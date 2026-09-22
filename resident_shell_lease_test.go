package tagent

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/require"
)

// C1 (resident-review-fixes 1.2): the R4 executor-shell rebuild must never
// re-register recovery retention on the SHARED durable store. A durable bus
// build arms (ArmRetentionFromInbox) and a mem_spill attach arms
// (ProtectAllPending) the un-acked originals held under the resident owner's
// lease. The shell's own artifacts are discarded (its bus is never acked) and
// releaseRetention only runs on the resident bus's Ack path — so any holder the
// shell adds is a permanent lease leak, and an Arm-failure leg keeps a BeginHold
// with no matching EndHold (forgetting barrier hangs forever). The gate
// `!mode.isExecutorShell()` on agentCfg.BusSpillDir / ets.SetMemSpill restores
// recovery registration to resident-owner-only.

// seedUnackedEnvelope writes one claimed+prepared but un-acked durable envelope
// into the inbox rooted at spillDir (spillDir/inbox-v2), whose prepared fact key
// is factKey and reserved receipt key is receiptKey. A later
// NewReliableEventBus(spillDir)+ArmRetentionFromInbox enumerates it and protects
// exactly those keys — mirroring the §2.8 restart-recovery owner setup.
func seedUnackedEnvelope(t *testing.T, spillDir string, factKey, receiptKey int64) {
	t.Helper()
	in, err := reliability.NewInbox(spillDir, 0)
	require.NoError(t, err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "leak-probe", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"e-leak","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	require.NoError(t, err)
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, tagentevent.FormatEventKey(receiptKey),
		[]json.RawMessage{json.RawMessage(`{"event_key":` + strconv.FormatInt(factKey, 10) + `}`)}))
	require.NoError(t, in.Close())
}

func TestResidentShellBuild_DoesNotDoubleArmSharedLease(t *testing.T) {
	root := t.TempDir()
	storeDir := filepath.Join(root, "store")
	busRoot := filepath.Join(root, "bus")
	spillRoot := filepath.Join(root, "spill")

	mc := MemoryConfig{Type: "localfile", Path: storeDir}
	// Pre-acquire the shared durable store so the test holds the SAME
	// *FileSegmentStore (and its §2.8 retention lease) that the resident build
	// will later borrow via the path registry — the lease is created in-process
	// by buildSharedResource and is not reachable after the fact except through
	// the shared instance.
	rawStore, _, preRelease, err := defaultResources.acquire("localfile", storeDir, fingerprintMemory(mc), func() (openedResource, error) {
		return openLocalFileStore(mc)
	})
	require.NoError(t, err)
	defer func() { _ = preRelease() }()
	fss, ok := rawStore.(*memory.FileSegmentStore)
	require.True(t, ok, "localfile shared store must be a *memory.FileSegmentStore")
	lease := fss.RetentionLease()
	require.NotNil(t, lease, "buildSharedResource must wire the retention lease")

	// One overdue un-acked envelope in the entry's durable inbox.
	pid := memory.PartitionIDFromName("tagent")
	now := time.Now().UnixMilli()
	factKey := memory.NewSnowflakeEventKey(pid, now-10*24*3600*1000) // overdue fact original
	receiptKey := memory.NewSnowflakeEventKey(pid, now)
	seedUnackedEnvelope(t, filepath.Join(busRoot, "tagent"), factKey, receiptKey)

	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "resident"},
				Memory:       mc,
			},
		},
		Reliability: ReliabilityConfig{
			BusSpillDir:        busRoot,
			MemSpillDir:        spillRoot,
			DegradationEnabled: true,
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")

	// 1) Resident cold-start build: the durable bus arms recovery retention on
	//    the shared store's lease — exactly one holder per material key.
	resident, err := buildAgent("tagent", cfg.Agents["tagent"], cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeResident)
	require.NoError(t, err)
	rc.entryMemStore = resident.MemStore()
	rc.entrySessionSvc = resident.SessionSvc()
	require.Equal(t, 1, lease.Holders(factKey), "resident arm protects the fact original")
	require.Equal(t, 1, lease.Holders(receiptKey), "resident arm protects the receipt original")

	// 2) Hot-reload executor-shell rebuild (structural change → new generation).
	//    The shell borrows the resident store; with the C1 gate it builds a
	//    volatile bus (no BusSpillDir) and no spill, so it adds NO holder.
	gen2 := cfg.Agents["tagent"]
	gen2.SystemPrompt = PromptConfig{Inline: "gen2"}
	_, err = buildAgent("tagent", gen2, cfg, rc, loader, make(map[string]*agent.TagentAgent), buildModeExecutorShell)
	require.NoError(t, err)

	// fail-before: removing the `!mode.isExecutorShell()` gate on BusSpillDir /
	// SetMemSpill makes the shell re-open the SAME durable inbox and re-arm the
	// SAME keys → these become 2 (permanent leak, forgetting never resumes).
	// With the gate: unchanged.
	require.Equal(t, 1, lease.Holders(factKey), "executor shell must add NO holder to the shared fact lease (C1)")
	require.Equal(t, 1, lease.Holders(receiptKey), "executor shell must add NO holder to the shared receipt lease (C1)")
}

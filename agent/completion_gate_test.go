package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/kv"
	"github.com/stretchr/testify/require"
)

// §5.10 — section-5 completion gate: the integration scenarios the spec names
// explicitly, discriminating against ANY harvest-of-projection residue:
//   receipt key BEFORE the compaction boundary (outside snapshot/tail scan),
//   crash after completion before receipt (the FROZEN stamp+attribution are
//   re-used, never re-stamped), and full CROSS-PROCESS recovery of the same
//   window through a real durable backend (localfile, per spec L144).

// TestGate_ReceiptKeyBeforeCompactionBoundary: an outstanding receipt key that
// lies BEFORE the snapshot anchor is invisible to the scan window — reconcile
// must still find it through the envelope's own reservation, re-submit only
// the frozen receipt, and never re-execute (spec Scenario「回执 key 早于压缩边
// 界」+「completion 后 receipt 前崩溃」: 复用冻结的完整 receipt 含原时间与归因).
func TestGate_ReceiptKeyBeforeCompactionBoundary(t *testing.T) {
	dir := t.TempDir()
	ta, healthy, path := completionOnlyEnvelope(t, dir)
	// The cold-start rebuild runs against an EMPTY projection with a real
	// compressor wired (the state a fresh process boots in).
	ta.contextManager.projection = compress.NewSessionProjection()
	ta.contextManager.contextCompressor = compress.NewContextCompressor(
		compress.NewSmartCompressor(compress.WithKeepRecentTasks(2)),
		healthy, compress.NewDefaultTokenCounter(), 60, 0.8, 2,
		compress.WithRecentFullCount(2))
	env := envelopeAt(t, path)
	frozen, ferr := decodeCompletion(env.Completion)
	require.NoError(t, ferr)
	receiptKey, kerr := tagentevent.ParseEventKey(frozen.ReceiptKey)
	require.NoError(t, kerr)

	// A compaction snapshot NEWER than the reserved receipt key: the rebuild
	// scan window starts at the anchor, so the receipt key is out of range.
	snapKey := memory.NewSnowflakeEventKey(1, time.Now().UnixMilli()+3600_000)
	require.Greater(t, snapKey, receiptKey, "the boundary must sit AFTER the outstanding receipt key")
	payload, perr := json.Marshal(&compress.CompactionPayload{
		SummaryRef:   memory.EventReference{EventKey: -1, EventType: tagentevent.TypeContextCompress},
		FullBoundary: 0,
	})
	require.NoError(t, perr)
	require.NoError(t, healthy.StoreEvent(snapKey, memory.FullEvent{
		EventKey: snapKey, PartitionID: 1, EventType: tagentevent.TypeContextCompressSummary,
		Timestamp: time.Now().UnixMilli(), Metadata: map[string]string{
			compress.CompactionMetaKey:        compress.CompactionGenV1,
			compress.CompactionPayloadMetaKey: string(payload),
		},
	}))

	ta.RebuildProjectionFromWAL() // scan runs — and cannot see the receipt key
	res := ta.contextManager.RecoveryResult()
	require.NotNil(t, res)
	require.Equal(t, "snapshot", res.Mode, "precondition: the rebuild anchored at the newer snapshot")

	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.ReceiptsAdded, "direct lookup finds the key outside the scan window and re-submits ONLY the receipt")

	stored, gerr := healthy.GetEvent(receiptKey)
	require.NoError(t, gerr)
	require.Equal(t, frozen.ReceiptFact.Timestamp, stored.Timestamp,
		"§5.10: the re-submitted receipt reuses the FROZEN original time, never a re-stamp")
	require.Equal(t, frozen.ReceiptFact.Metadata, stored.Metadata, "and the frozen attribution verbatim")
	for _, ref := range ta.contextManager.projection.GetAll() {
		require.NotEqual(t, receiptKey, ref.EventKey, "the scan/projection never harvested the out-of-window receipt")
	}
}

// TestGate_CrossProcessCompletionOnlyRecovery runs the Phase-A-died window
// through a REAL second process: the child freezes a durable completion on a
// localfile-backed chain + inbox and exits WITHOUT any cleanup; the parent,
// sharing those directories, must register protection, reconcile directly from
// the frozen bytes (no re-run, no re-stamp), clean up, and release exactly
// once (spec L144: cross-process acceptance uses localfile).
func TestGate_CrossProcessCompletionOnlyRecovery(t *testing.T) {
	const (
		childEnv = "TAGENT_XPROC_CHILD"
		dirEnv   = "TAGENT_XPROC_DIR"
		factEnv  = "TAGENT_XPROC_FACT"
		recEnv   = "TAGENT_XPROC_RECEIPT"
		frozenMs = int64(1700000123456)
	)
	if os.Getenv(childEnv) == "1" {
		xprocChildFreezeCompletionOnly(os.Getenv(dirEnv), frozenMs,
			mustI64(os.Getenv(factEnv)), mustI64(os.Getenv(recEnv)))
		os.Exit(0) // no Close, no ack, no receipt commit — the crash itself
	}

	root := t.TempDir()
	dirStore, dirInbox := root+"/store", root+"/inbox"
	inputKey := memory.NewSnowflakeEventKey(1, frozenMs-2000) // the OVERDUE original
	receiptKey := memory.NewSnowflakeEventKey(1, frozenMs-1000)
	cmd := exec.Command(os.Args[0], "-test.run", "^TestGate_CrossProcessCompletionOnlyRecovery$", "-test.v")
	cmd.Env = append(os.Environ(), childEnv+"=1", dirEnv+"="+root,
		factEnv+"="+fmt.Sprint(inputKey), recEnv+"="+fmt.Sprint(receiptKey))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child (Phase A) failed: %v\n%s", err, out)
	}

	// Fresh process: same durable backend, same inbox — nothing was graceful-closed.
	kvStore, err := kv.NewLocalFileKV(dirStore)
	require.NoError(t, err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, dirStore, 100)
	require.NoError(t, err)
	lease := memory.NewRetentionLease()
	store.SetRetentionLease(lease)
	bus, err := NewReliableEventBus(dirInbox)
	require.NoError(t, err)
	bus.SetRetentionGuard(store)
	require.NoError(t, bus.ArmRetentionFromInbox(), "protection is rebuilt from the on-disk envelope before forgetting")
	require.True(t, store.IsKeyProtected(inputKey), "the overdue input original is leased")
	cm := &ContextManager{partitionID: 1, memStore: store, projection: compress.NewSessionProjection()}
	ta := &TagentAgent{name: "xproc", persistentBus: bus, contextManager: cm}
	s, err := ta.ReconcileOutstanding()
	require.NoError(t, err)
	require.Equal(t, 1, s.ReceiptsAdded, "the frozen completion re-submits its own receipt — no model in this process")

	stored, gerr := store.GetEvent(receiptKey)
	require.NoError(t, gerr)
	require.Equal(t, tagentevent.TypeInboxReceipt, stored.EventType)
	require.Equal(t, frozenMs, stored.Timestamp, "the cross-process receipt carries the FROZEN time, not the recovery clock")
	require.Equal(t, "frozen-first", stored.Metadata["attribution"], "and the frozen attribution, re-used verbatim across the process boundary")
	require.Equal(t, int64(0), bus.DurablePending(), "cleaned up after the receipt+ack barrier")
	require.False(t, store.IsKeyProtected(inputKey), "released exactly once after the ack barrier")
	_, ierr := store.GetEvent(inputKey)
	require.NoError(t, ierr, "the ORIGINAL input fact is intact across the process boundary (no re-run needed, none happened)")
	require.Equal(t, 1, countReceipts(t, store), "exactly one receipt on the chain — no duplicate, no re-run")
}

// xprocChildFreezeCompletionOnly drives the real leaf protocol up to — but
// deliberately NOT past — Phase B, then returns so the caller exits(0) like a
// crashed process.
func xprocChildFreezeCompletionOnly(root string, frozenMs, factKey, receiptKey int64) {
	kvStore, err := kv.NewLocalFileKV(root + "/store")
	mustX(err)
	store, err := memory.NewFileSegmentStore(kvStore, nil, root+"/store", 100)
	mustX(err)
	mustX(store.StoreEvent(factKey, memory.FullEvent{
		EventKey: factKey, PartitionID: 1, EventType: tagentevent.TypeExternalInput,
		EventSummary: "xproc input", Content: "xproc input", Timestamp: frozenMs - 1000,
	}))
	in, err := reliability.NewInbox(root+"/inbox", 0)
	mustX(err)
	_, err = in.Enqueue(&reliability.Envelope{
		RequestID: "xproc-1", State: reliability.InboxStatePending,
		Messages: []reliability.MessageSlot{{Slot: 0, SourceEvent: json.RawMessage(`{"id":"x-1","type":"external_input","message":{"role":"user","content":"x"}}`)}},
	})
	mustX(err)
	_, path, err := in.ClaimNext()
	mustX(err)
	hexR := tagentevent.FormatEventKey(receiptKey)
	mustX(in.PrepareFacts(path, hexR, []json.RawMessage{json.RawMessage(fmt.Sprintf(`{"event_key":%d}`, factKey))}))
	fact, err := buildReceiptFact(hexR, 1, "xproc-1", "xproc", frozenMs, map[string]string{"attribution": "frozen-first"})
	mustX(err)
	raw, err := freezeCompletion(completion{
		CompletionVersion: completionVersion,
		RequestID:         "xproc-1",
		ReceiptKey:        hexR,
		CompletedAtMs:     frozenMs,
		BatchResult:       batchCompleted,
		Slots:             []completionSlot{{Slot: 0, SourceID: "x-1", Disposition: slotProcessed, FactKey: tagentevent.FormatEventKey(factKey)}},
		ReceiptFact:       fact,
	})
	mustX(err)
	mustX(in.RecordCompletion(path, raw)) // Phase A durable — Phase B never runs
}

func mustX(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "xproc child:", err)
		os.Exit(2)
	}
}

func mustI64(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		panic("xproc env: bad int64: " + s)
	}
	return n
}

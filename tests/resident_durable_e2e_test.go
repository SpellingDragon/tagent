package tagent_test

// §8.1 — the RESIDENT DURABLE integration base: inbox-v2 (reliable bus over a
// spill dir) + localfile fact store + the REAL runner/plugin stack, with a
// mock model standing in for the LLM. Every acceptance is reconciled across
// the five surfaces: original receipt (envelope lifecycle), facts, actual
// model requests, results (outputs + receipts), and cleanup (inbox drained).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// durableE2EAgent wires the full composition root with BOTH durable surfaces
// on: localfile store + reliable bus (inbox-v2 spill), entry name "tagent".
func durableE2EAgent(t *testing.T, storeDir, spillDir string, m *e2eModel) *agent.TagentAgent {
	t.Helper()
	ta, err := tagent.New(tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt:      tagent.PromptConfig{Inline: "durable e2e agent"},
				MaxTokens:         4000,
				CompressThreshold: 0.8,
				Memory:            tagent.MemoryConfig{Type: "localfile", Path: storeDir},
			},
		},
		Reliability: tagent.ReliabilityConfig{BusSpillDir: spillDir},
	}, tagent.WithModel(m))
	require.NoError(t, err)
	return ta
}

func TestResidentDurableE2E_FiveSurfaceReconciliation(t *testing.T) {
	root := t.TempDir()
	storeDir, spillDir := filepath.Join(root, "store"), filepath.Join(root, "spill")
	m := &e2eModel{}
	ta := durableE2EAgent(t, storeDir, spillDir, m)

	out, err := ta.StartLoop("du", "dur-session")
	require.NoError(t, err)
	var mu sync.Mutex
	var sources []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range out {
			mu.Lock()
			if v, ok := evt.StateDelta["trigger_source"]; ok {
				sources = append(sources, fmt.Sprintf("%s", v))
			}
			mu.Unlock()
		}
	}()

	// ---- acceptances: batch A+B as ONE unit; C arrives only AFTER the A+B
	// turn is provably in flight (barrier, not sleep: request #0 captured) ---
	msgA := model.Message{Role: model.RoleUser, Content: "dur-A first line"}
	msgB := model.Message{Role: model.RoleUser, Content: "dur-B second input"}
	msgC := model.Message{Role: model.RoleUser, Content: "dur-C later single"}

	reqAB, durableAB, err := ta.InjectEnvelope(context.Background(), "user", []model.Message{msgA, msgB})
	require.NoError(t, err)
	require.True(t, durableAB, "with BusSpillDir set, acceptance MUST be durable (inbox-v2)")
	require.NotEmpty(t, reqAB)
	waitReq(t, m, 0, "dur-A first line") // A+B are in the ACTUAL model request — the turn is executing
	require.True(t, m.contains(0, "dur-B second input"),
		"A+B must merge into ONE business turn (batch slots are not compacted away)")

	recC, err := ta.InjectMessageContext(context.Background(), "user", msgC)
	require.NoError(t, err)
	require.True(t, recC.Durable)
	require.NotEmpty(t, recC.RequestID)
	require.NotEqual(t, reqAB, recC.RequestID, "each acceptance keeps its own stable identity")

	// ---- surface: ACTUAL model requests — C forms the NEXT turn ------------
	waitReq(t, m, 1, "dur-C later single")

	// ---- surface: results reach the host with lineage ----------------------
	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		n := len(sources)
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("delivery: only %d/2 turn outputs observed", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	for i, s := range sources[:2] {
		require.Equal(t, "user", s, "output %d carries user lineage (sources=%v)", i, sources)
	}
	mu.Unlock()

	// ---- surface: facts — every source message stored verbatim ------------
	pid := memory.PartitionIDFromName("tagent")
	store := ta.MemStore()
	for _, body := range []string{"dur-A first line", "dur-B second input", "dur-C later single"} {
		refs, qerr := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Keyword: body, Limit: 10})
		require.NoError(t, qerr)
		require.NotEmpty(t, refs, "the ORIGINAL input %q must be recallable from the fact chain", body)
		full, gerr := store.GetEvent(refs[0].EventKey)
		require.NoError(t, gerr)
		require.Equal(t, tagentevent.TypeExternalInput, full.EventType)
		require.Contains(t, full.Content, body, "stored content is the original, not a digest")
	}

	// ---- surface: receipts — one per business turn, then CLEANUP ----------
	receiptCount := func() int {
		refs, qerr := store.QueryEvents(memory.QueryOptions{PartitionIDs: []int{pid}, Limit: 200})
		if qerr != nil {
			return -1
		}
		n := 0
		for _, r := range refs {
			if r.EventType == tagentevent.TypeInboxReceipt {
				n++
			}
		}
		return n
	}
	deadline = time.Now().Add(20 * time.Second)
	for receiptCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 inbox receipts (A+B turn, C turn), got %d", receiptCount())
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.Equal(t, 2, receiptCount(), "exactly one receipt per business turn — no per-envelope double receipt")

	// Cleanup end: the inbox-v2 envelope store is fully drained (every accepted
	// item ended its lifecycle as processed-cleaned, nothing silently lost).
	inbox, err := reliability.NewInbox(filepath.Join(spillDir, "tagent"), 0)
	require.NoError(t, err)
	outstanding, err := inbox.Outstanding()
	require.NoError(t, err)
	require.Empty(t, outstanding, "after ack every envelope is unlinked — accepted IDs are all in the processed-cleaned state")
	entries, err := os.ReadDir(filepath.Join(spillDir, "tagent"))
	require.NoError(t, err)
	envLeft := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			envLeft++
		}
	}
	require.Equal(t, 0, envLeft, "no envelope residue in the inbox directory")

	require.NoError(t, ta.Close())
	<-done
}

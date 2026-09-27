package agent

import (
	"fmt"
	"sync"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// §7.1 (D2 去隐式传参): external context must be assembled per-invocation and the
// legacy direct-Ingest API must keep its single-handoff semantics without leaking
// into a concurrent Run. These pin the seam that replaces the old shared
// `ta.pendingExternalEvents` read/write that two concurrent Runs used to race on
// (Run previously did `IngestExternalEvents(events)` then read+clear the field —
// unsynchronized across goroutines; the removed writes are the red, the guarded
// drain + per-call local assembly below is the green). The wrapper→Run end-to-end
// path stays covered by tool_agent_test.go.

// applyExternalContext is pure: it folds the given events into the message and
// touches no shared state; empty events leave the message untouched.
func TestApplyExternalContext_IsPureAndCompact(t *testing.T) {
	msg := model.NewUserMessage("do the thing")
	events := []memory.FullEvent{
		{EventType: "note", EventSummary: "first note"},
		{EventType: "note", EventSummary: "second note"},
	}
	out := applyExternalContext(msg, events)
	require.Contains(t, out.Content, "first note")
	require.Contains(t, out.Content, "second note")
	require.Contains(t, out.Content, "do the thing")

	// Empty → unchanged (no prelude header injected).
	untouched := applyExternalContext(model.NewUserMessage("plain"), nil)
	require.Equal(t, "plain", untouched.Content)

	// The caller's value is not mutated (message is passed by value).
	require.Equal(t, "do the thing", msg.Content)
}

// The direct-Ingest slot is a single handoff: one drain returns the events and
// clears the slot; a second drain returns nothing, so the events cannot be
// re-delivered to a later, unrelated Run.
func TestDrainPendingExternalEvents_SingleHandoff(t *testing.T) {
	ta := &TagentAgent{name: "sink"}
	ta.IngestExternalEvents([]memory.FullEvent{{EventSummary: "one"}})

	first := ta.drainPendingExternalEvents()
	require.Len(t, first, 1)
	require.Equal(t, "one", first[0].EventSummary)

	require.Nil(t, ta.drainPendingExternalEvents(), "second drain must be empty")
	require.Nil(t, ta.pendingExternalEvents, "slot cleared after handoff")
}

// Concurrent Ingest + drain must not tear the shared slot (§7.1 D2). Each drain
// observes a coherent snapshot or nothing, never a half-written slice. This is the
// regression guard for the removed unlocked `ta.pendingExternalEvents` access.
func TestDrainPendingExternalEvents_ConcurrentNoTear(t *testing.T) {
	ta := &TagentAgent{name: "sink"}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		i := i
		wg.Add(2)
		go func() {
			defer wg.Done()
			ta.IngestExternalEvents([]memory.FullEvent{{EventSummary: fmt.Sprint("ingest", i)}})
		}()
		go func() {
			defer wg.Done()
			got := ta.drainPendingExternalEvents()
			for _, e := range got {
				require.Contains(t, e.EventSummary, "ingest")
			}
		}()
	}
	wg.Wait()
}

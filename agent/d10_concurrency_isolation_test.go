package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSettleSinkRegistry_ConcurrentPerInvocationIsolation locks the I1 invariant
// (design line 169): concurrent delegations to the SAME callee must never cross
// receivers. After the S3m-c convergence the routing table is a per-invocation BUS
// binding — each invocation binds its OWN bus under its unique correlation handle,
// and route publishes strictly to the handle's bus. This drives many invocation ids
// through the real registry from concurrent goroutines (bind, note-spawn, route/
// deliver, reclaim-from-bus-to-quiesce), asserting every event a loop reclaims is
// its own (by provenance tag) and its barrier reaches quiescence independently — no
// foreign settle, no shared-counter corruption, no premature/never quiesce. Run
// under -race this also validates the registry's lock discipline (review W-3's
// "concurrent same-name" concern, at the routing layer where crossing would occur).
func TestSettleSinkRegistry_ConcurrentPerInvocationIsolation(t *testing.T) {
	r := newSettleSinkRegistry()

	const invocations = 8
	const perInvocation = 25

	var wg sync.WaitGroup
	results := make([][]string, invocations)

	for i := 0; i < invocations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("inv-%d", i)
			invBus := NewEventBus()
			r.bind(id, invBus)

			// Book this invocation's spawns (as countingSpawner would), concurrently
			// with the other invocations touching the same registry.
			for k := 0; k < perInvocation; k++ {
				r.noteSpawn(id)
			}

			// A producer for a DIFFERENT-looking handle must never land in our bus:
			// deliver our own settles (each tagged with our id) and reclaim them.
			var mine []string
			for k := 0; k < perInvocation; k++ {
				evtID := fmt.Sprintf("%s#%d", id, k)
				require.True(t, r.route(id, &AgentEvent{ID: evtID}), "publish to own bound bus")
				for _, e := range invBus.TryPull() {
					mine = append(mine, e.ID)
				}
			}
			// Final reclaim of anything buffered.
			for _, e := range invBus.TryPull() {
				mine = append(mine, e.ID)
			}

			// Quiescence is reached only once every one of OUR spawns was delivered
			// by OUR routes — independent of the other concurrent invocations.
			require.True(t, r.quiescent(id), "own barrier reaches 0 despite concurrent siblings")

			results[i] = mine
			r.unbind(id)
		}(i)
	}
	wg.Wait()

	for i := 0; i < invocations; i++ {
		id := fmt.Sprintf("inv-%d", i)
		for _, evtID := range results[i] {
			require.Truef(t, strings.HasPrefix(evtID, id+"#"),
				"I1: invocation %s reclaimed a foreign event %s — buses crossed", id, evtID)
		}
	}
}

// TestSettleSinkRegistry_ConcurrentSameNameDistinctHandles proves two delegations
// carrying DIFFERENT handles but the same callee do not merge: the barrier and bus
// binding are strictly per-handle, so a settle booked/published under one handle is
// never counted or delivered under the other. (The "same-name" case that a shared
// per-agent field — which S2m deliberately avoided — would have corrupted.)
func TestSettleSinkRegistry_ConcurrentSameNameDistinctHandles(t *testing.T) {
	r := newSettleSinkRegistry()
	busX := NewEventBus()
	busY := NewEventBus()
	r.bind("callee/X", busX)
	r.bind("callee/Y", busY)

	var wg sync.WaitGroup
	for _, id := range []string{"callee/X", "callee/Y"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				r.noteSpawn(id)
			}
			for k := 0; k < 50; k++ {
				require.True(t, r.route(id, &AgentEvent{ID: id}))
			}
		}(id)
	}
	wg.Wait()

	// Each handle's barrier independently quiesced (its 50 spawns matched by its own
	// 50 deliveries); neither borrowed the other's decrements.
	require.True(t, r.quiescent("callee/X"))
	require.True(t, r.quiescent("callee/Y"))
	require.Len(t, drainIDs(busX), 50, "X's bus holds exactly X's settles")
	require.Len(t, drainIDs(busY), 50, "Y's bus holds exactly Y's settles")
}

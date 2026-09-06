package governance

import (
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestGoalRegistry_RebuildFromEvents (5.2, design-report-closeout): goal
// declarations survive a restart — Declare/Resolve double-write governance
// events, and a fresh registry bound to the same store replays them
// (status included; seq aligned so new IDs never collide). Fail-before:
// GoalRegistry was memory-only, restart lost all goals and the goal gate
// silently reopened.
func TestGoalRegistry_RebuildFromEvents(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")

	reg := NewGoalRegistry()
	reg.BindStore(store, pid)
	id1 := reg.Declare("完成季度报告", "user", 0)
	id2 := reg.Declare("清理临时文件", "agent", 0)
	if !reg.Resolve(id1, GoalAchieved) {
		t.Fatal("resolve id1")
	}
	if !reg.HasActive() {
		t.Fatal("id2 still active")
	}

	// Simulated restart: fresh registry, same store.
	reg2 := NewGoalRegistry()
	reg2.BindStore(store, pid)
	if !reg2.HasActive() {
		t.Fatal("rebuild lost the active goal (restart must not reopen the gate)")
	}
	// Resolved status survives.
	if reg2.Resolve(id1, GoalAchieved) == false {
		// id1 must exist post-rebuild (resolved status retained).
		t.Fatalf("id1 missing after rebuild")
	}
	// seq aligned: new declare must not collide with g-1/g-2.
	id3 := reg2.Declare("新目标", "user", 0)
	if id3 == id1 || id3 == id2 {
		t.Fatalf("ID collision after rebuild: %s", id3)
	}
}

// TestGoalRegistry_NoStoreNoEvents: without BindStore, behavior is exactly
// the legacy in-memory registry (zero behavior change, no events written).
func TestGoalRegistry_NoStoreNoEvents(t *testing.T) {
	reg := NewGoalRegistry()
	id := reg.Declare("x", "user", 0)
	if id != "g-1" || !reg.HasActive() {
		t.Fatal("legacy in-memory behavior broken")
	}
	reg.Resolve(id, GoalAchieved)
	if reg.HasActive() {
		t.Fatal("resolve failed")
	}
}

// TestGoalRegistry_ConcurrentNoResurrection (8.7/8.8, review §8): concurrent
// Declare/Resolve churn must not reorder governance events — after a rebuild
// a RESOLVED goal must stay resolved. Fail-before: event key/ts were
// allocated outside the lock, so a declared event could land AFTER its
// resolved event and resurrect the goal as active.
func TestGoalRegistry_ConcurrentNoResurrection(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	reg := NewGoalRegistry()
	reg.BindStore(store, pid)

	const churn = 40
	ids := make(chan string, churn)
	var declareWG, resolveWG sync.WaitGroup

	// Resolver: resolve each id as soon as it is declared.
	resolveWG.Add(1)
	go func() {
		defer resolveWG.Done()
		for id := range ids {
			reg.Resolve(id, GoalAchieved)
		}
	}()

	// Concurrent churn declares + noise declares.
	for i := 0; i < churn; i++ {
		declareWG.Add(2)
		go func() {
			defer declareWG.Done()
			ids <- reg.Declare("churn", "agent", 0)
		}()
		go func() {
			defer declareWG.Done()
			reg.Declare("noise", "agent", 0)
		}()
	}
	declareWG.Wait()
	close(ids)
	resolveWG.Wait()

	// All churn goals were resolved → rebuild must show zero ACTIVE churn.
	reg2 := NewGoalRegistry()
	reg2.BindStore(store, pid)
	for _, g := range reg2.List() {
		if g.Statement == "churn" && g.Status == GoalActive {
			t.Fatalf("resurrection: churn goal %s revived as active after rebuild", g.ID)
		}
	}
}

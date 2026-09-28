// 本文件负责目标登记表的重建路径：从事件重放恢复、无存储时的退化，以及并发下已终结目标
// 不得复活。
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
package governance

import (
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestGoalRegistry_RebuildFromEvents 钉住 goal。
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

	reg2 := NewGoalRegistry()
	reg2.BindStore(store, pid)
	if !reg2.HasActive() {
		t.Fatal("rebuild lost the active goal (restart must not reopen the gate)")
	}
	if reg2.Resolve(id1, GoalAchieved) == false {
		t.Fatalf("id1 missing after rebuild")
	}
	id3 := reg2.Declare("新目标", "user", 0)
	if id3 == id1 || id3 == id2 {
		t.Fatalf("ID collision after rebuild: %s", id3)
	}
}

// TestGoalRegistry_NoStoreNoEvents 钉住 without BindStore, behavior is exactly。
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

// TestGoalRegistry_ConcurrentNoResurrection 钉住 concurrent。
func TestGoalRegistry_ConcurrentNoResurrection(t *testing.T) {
	store := memory.NewInMemoryStore()
	pid := memory.PartitionIDFromName("tagent")
	reg := NewGoalRegistry()
	reg.BindStore(store, pid)

	const churn = 40
	ids := make(chan string, churn)
	var declareWG, resolveWG sync.WaitGroup

	resolveWG.Add(1)
	go func() {
		defer resolveWG.Done()
		for id := range ids {
			reg.Resolve(id, GoalAchieved)
		}
	}()

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

	reg2 := NewGoalRegistry()
	reg2.BindStore(store, pid)
	activeChurn, resolvedChurn := 0, 0
	for _, g := range reg2.List() {
		if g.Statement != "churn" {
			continue
		}
		if g.Status == GoalActive {
			activeChurn++
		} else {
			resolvedChurn++
		}
	}
	if activeChurn != 0 || resolvedChurn != churn {
		t.Fatalf("rebuild incomplete: active=%d resolved=%d, want 0/%d", activeChurn, resolvedChurn, churn)
	}
}

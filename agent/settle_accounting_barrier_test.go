// 本文件负责派生计数的对账屏障：去重命中、被门阻止、内联结算三种情形都必须**作废记账**，
// 去重还要让调用环保持静默（不得留下一个永远不会被结算的计数）。
// 契约: docs/wiki/agent/task-lifecycle.md#spawn-dedup-origin
package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/agent/task"
	"github.com/stretchr/testify/require"
)

type reviewStubSpawner struct {
	res task.SpawnResult
}

func (s *reviewStubSpawner) Spawn(spec task.TaskSpec, detector task.SettleDetector) task.SpawnResult {
	if detector != nil {
		detector.Cancel()
	}
	return s.res
}

func TestCountingSpawnerVoidsBookingOnDedup(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	id := "review-inv-1"
	sinks.bind(id, bus)

	cs := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}, Deduped: true}},
		sinks: sinks,
		id:    id,
	}
	cs.Spawn(task.TaskSpec{}, nil)

	require.False(t, sinks.awaiting(id),
		"review P2 evidence: deduped spawn leaked a delivery booking — pending stuck >0, loop cannot quiesce")
}

func TestCountingSpawnerVoidsBookingOnBlock(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	id := "review-inv-2"
	sinks.bind(id, bus)

	cs := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Blocked: "disk degraded"}},
		sinks: sinks,
		id:    id,
	}
	cs.Spawn(task.TaskSpec{}, nil)

	require.False(t, sinks.awaiting(id),
		"review P2 evidence: blocked spawn leaked a delivery booking — pending stuck >0, loop cannot quiesce")
}

// TestCountingSpawnerVoidsBookingOnInlineSettle 钉住 is the CONTROL: the existing voidSpawn-on-Settled behavior is correct and must keep holding after any fix.
func TestCountingSpawnerVoidsBookingOnInlineSettle(t *testing.T) {
	sinks := newSettleSinkRegistry()
	bus := NewEventBus()
	id := "review-inv-3"
	sinks.bind(id, bus)

	cs := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}, Settled: true}},
		sinks: sinks,
		id:    id,
	}
	cs.Spawn(task.TaskSpec{}, nil)

	require.False(t, sinks.awaiting(id), "inline settle must void its booking (existing behavior)")
}

// TestDedupKeepsInvocationLoopQuiescent 钉住 去重形状下要走完整条屏障链：派生后台任务、同键第二次调用去重、首个任务结算送达。
// - 最后一条真实结算发到绑定总线之后，共享壳的退出判据（无等待且总线已排空）必须成立。
func TestDedupKeepsInvocationLoopQuiescent(t *testing.T) {
	sinks := newSettleSinkRegistry()
	invBus := NewEventBus()
	id := "review-inv-4"
	sinks.bind(id, invBus)

	bg := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}}},
		sinks: sinks, id: id,
	}
	bg.Spawn(task.TaskSpec{}, nil)
	require.True(t, sinks.awaiting(id), "background spawn must keep the tail alive")

	dup := &countingSpawner{
		inner: &reviewStubSpawner{res: task.SpawnResult{Task: &task.Task{}, Deduped: true}},
		sinks: sinks, id: id,
	}
	dup.Spawn(task.TaskSpec{}, nil)

	settled := &task.Task{Spec: task.TaskSpec{Origin: map[string]string{metaKeyInvocationID: id}}}
	deliverTaskSettled(sinks, nil, settled, &AgentEvent{ID: "settle-real", Source: SourceTask})

	require.False(t, sinks.awaiting(id), "after the last real settle is delivered the loop must be able to quiesce (P2-3 gate)")
	var sawSettle bool
	for _, e := range invBus.TryPull() {
		if e != nil && e.ID == "settle-real" {
			sawSettle = true
		}
	}
	require.True(t, sawSettle, "the settle must be on the bound bus (drained by the shell before exit)")
}

package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
)

// R4（resident-continuity-r2-r4 3.7）回归：cm.runner 可换缝。

// fakeRunner 记录自身身份的哑 runner（仅测缝语义）。
type fakeRunner struct {
	id string
}

func (f *fakeRunner) Run(ctx context.Context, userID, sessionID string, message model.Message, runOpts ...trpcagent.RunOption) (<-chan *event.Event, error) {
	return make(chan *event.Event, 1), nil
}

func (f *fakeRunner) Close() error { return nil }

var _ runner.Runner = (*fakeRunner)(nil)

// ⑤in-flight turn 用旧 runner 跑完 + 下一 turn 起新（drain-free turn 级）。
func TestSwapExecutor_DrainFreeTurnLevel(t *testing.T) {
	old := &fakeRunner{id: "gen1"}
	cm := &ContextManager{runner: old}

	// in-flight turn：先取引用（RunFlow 同款 per-turn RLock 即放）。
	inFlight := cm.currentRunner()
	cm.SwapExecutor(&fakeRunner{id: "gen2"})

	if inFlight == nil || inFlight.(*fakeRunner).id != "gen1" {
		t.Fatalf("in-flight turn must keep the old runner reference, got %v", inFlight)
	}
	if got := cm.currentRunner(); got.(*fakeRunner).id != "gen2" {
		t.Fatalf("next turn must see the new runner, got %v", got)
	}
}

// 并发断言（-race 重点）：并发 RunFlow 取引用 vs SwapExecutor 换入——无撕裂、
// 每次取到的引用自洽（old 或 new，绝不为 nil/半换）。
func TestSwapExecutor_ConcurrentSwapVsRead(t *testing.T) {
	cm := &ContextManager{runner: &fakeRunner{id: "gen0"}}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if r := cm.currentRunner(); r == nil {
						t.Error("runner reference must never be nil mid-swap")
						return
					}
				}
			}
		}()
	}
	for gen := 1; gen <= 50; gen++ {
		cm.SwapExecutor(&fakeRunner{id: "gen"})
	}
	close(stop)
	wg.Wait()
}

// ⑥org 常驻不变量：SwapExecutor 后 cm 本体/bus/projection/TaskManager 引用原封
// （状态⊥执行器——换的只是 runner 缝）。
func TestSwapExecutor_ResidentInvariants(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	proj := compress.NewSessionProjection()
	bus := NewReliableEventBus("")
	cm := &ContextManager{runner: &fakeRunner{id: "g1"}, taskController: tm, projection: proj, bus: bus}

	cm.SwapExecutor(&fakeRunner{id: "g2"})

	if cm.taskController != tm {
		t.Error("TaskManager reference must survive the swap (org-level resident)")
	}
	if cm.projection != proj {
		t.Error("projection must survive the swap (R1 continuity)")
	}
	if cm.bus != bus {
		t.Error("bus must survive the swap (resident loop)")
	}
}

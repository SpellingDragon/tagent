package agent

import (
	"context"
	"testing"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stretchr/testify/require"

	"github.com/SpellingDragon/tagent/agent/task"
)

// §4.2（spec task-registry-rebuild「恢复与显式重投按当前编排绑定」）：显式
// Resume/Relaunch 新建子 Run 时，目标必须按**当前有效代**解析——被后续代移除的
// 目标要明确拒绝，不得复活它当时的绑定，也不得静默改投。

// relaunchStubAgent is a named delegation target. Only Info().Name and Run()
// matter here: the accept path asserts on the SpawnResult key (spawn happens
// before the detector's run), and Run() returns an already-closed stream.
type relaunchStubAgent struct {
	trpcagent.Agent // 余下方法本测不调用；若被调用会立刻 panic 而非静默
	name            string
}

func (a relaunchStubAgent) Info() trpcagent.Info { return trpcagent.Info{Name: a.name} }

func (a relaunchStubAgent) Run(_ context.Context, _ *trpcagent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func relaunchWrapper(name string) *AgentToolWrapper {
	return NewAgentToolWrapper(relaunchStubAgent{name: name}, name, nil, nil)
}

// TestSubagentWrapper_FollowsEffectiveFace 钉住解析源本身：`SubagentWrapper` 认的是
// 已发布的面孔，因此换代、回滚式重发布都会立刻改变它认得谁——这正是「不另立第二
// 路由真源」的形状。
func TestSubagentWrapper_FollowsEffectiveFace(t *testing.T) {
	keep, drop := relaunchWrapper("keep"), relaunchWrapper("drop")
	cm := newTestContextManager("relaunch-face", &requestCapturingModel{resp: gateOKResp()},
		[]trpctool.Tool{keep, drop}, make(chan *event.Event, 32), nil)

	require.Same(t, drop, cm.SubagentWrapper("drop"), "baseline: both targets resolve")

	// 发布移除 drop 的一代。
	face := cm.ExecutorConfig()
	face.Tools = []trpctool.Tool{keep}
	cm.PublishExecutor(cm.NewExecutorCandidate(face), face)

	require.Same(t, keep, cm.SubagentWrapper("keep"))
	require.Nil(t, cm.SubagentWrapper("drop"),
		"a target removed by the effective generation must stop resolving — that is what makes a relaunch refuse instead of reviving it")

	// 回滚式重发布（换回含 drop 的面孔）必须立刻恢复可解析性：解析源没有惰性缓存。
	back := cm.ExecutorConfig()
	back.Tools = []trpctool.Tool{keep, drop}
	cm.PublishExecutor(cm.NewExecutorCandidate(back), back)
	require.Same(t, drop, cm.SubagentWrapper("drop"), "republishing the old topology restores resolvability")
}

// TestSubagentRedispatcher_RefusesTargetNotInEffectiveGeneration 是 §4.2 的拒绝面：
// 重投解析不到当前代目标时，返回明确错误、不产出 SpawnResult，且**不写任务板**；
// 当前代仍认得的名字照常投递。
func TestSubagentRedispatcher_RefusesTargetNotInEffectiveGeneration(t *testing.T) {
	tm := task.NewTaskManager(task.TaskManagerConfig{})
	live := relaunchWrapper("live")
	routed := map[string]*AgentToolWrapper{"live": live}
	// A nil lease is inert by contract: this resolver stands in for the resolution
	// rule, so the test exercises the refusal/delivery branches, not the pinning.
	redispatch := SubagentRedispatcher(func(_ context.Context, name string) (*AgentToolWrapper, *ExecLease, error) {
		return routed[name], nil, nil
	}, tm)

	res, err := redispatch(context.Background(), "retired", "resume work")
	require.Error(t, err, "a target absent from the effective generation must be refused")
	require.Contains(t, err.Error(), "EFFECTIVE orchestration generation",
		"and the message must say WHY (version selection), not just 'not found'")
	require.Contains(t, err.Error(), "retired", "naming the target so the operator can act")
	require.Nil(t, res.Task, "no execution may be created for a refused relaunch (task board untouched)")

	// 仍被当前代认得的目标必须真被投递（证明拒绝来自解析源，而非这条路径总是失败）。
	res2, err2 := redispatch(context.Background(), "live", "resume work")
	require.NoError(t, err2, "a still-routed target must relaunch normally")
	require.NotNil(t, res2.Task, "a still-routed target really gets a task")
	require.Equal(t, "live:resume work", res2.Task.Spec.Key, "spawned under the current binding")

	// 反证式收口：把名字从解析源里摘掉（模拟后续代移除了它），同一请求必须转为拒绝。
	delete(routed, "live")
	_, err3 := redispatch(context.Background(), "live", "resume work again")
	require.Error(t, err3, "removal from the effective face, not the record, decides the outcome")
}

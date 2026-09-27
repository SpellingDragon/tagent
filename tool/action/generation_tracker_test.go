package action

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 3.3「有状态工具」腿（evidence §5.51）：已纳管任务不因工具换代失监视。
//
// 组织侧的热更换代为每个 owner 装配**新的** ActionTool，而 TaskManager 的会话
// tracker 是 `actionTool.IsTrackedSession` 的**绑定方法值**——它捕获的是那一刻的
// 工具实例。本测钉住这条规则的物理前提：两台 ActionTool 的 monitor 互相独立，
// 因此「闭包仍指向换代前的那台」不是理论问题，而是一次确定的**假阴性**——
// 存活会话会被判为未跟踪，孤儿裁决（RetireOrphans）与 suspect→running 提升
// 都按这个判定行动，足以把仍在跑的纳管任务裁死。
//
// 换代后必须把 tracker 重挂到**当前代**的工具上（build_agent 的 wireAgent 早有
// 此注释：「executorOnly 热重建换代 ActionTool 后，旧闭包指向旧 monitor，须重接
// （幂等）」）。轮九十的去壳让已存在 owner 不再经 wireAgent，这条既有规则因此
// 在发布路径上失守——修复见 build_agent.go 的 activateOwnerGenerations。

func TestActionTool33_MonitorsArePerGeneration(t *testing.T) {
	prior := NewActionTool(WithOrphanCleanupDisabled())
	require.NotNil(t, prior.tmuxMonitor, "precondition: no monitor without tmux — the machine must provide it")
	defer func() { _ = prior.Close() }()

	live := NewActionTool(WithOrphanCleanupDisabled())
	require.NotNil(t, live.tmuxMonitor)
	defer func() { _ = live.Close() }()

	require.NotSame(t, prior.tmuxMonitor, live.tmuxMonitor,
		"两代工具各自持有 monitor——若它们共享同一台，重挂就无关紧要，本测的前提也就不成立")

	// A managed task started by the CURRENT generation is tracked there only.
	const sess = "tagent-33-generation-tracker"
	live.tmuxMonitor.AddSession(&TmuxSession{ID: sess})

	require.True(t, live.IsTrackedSession(sess), "the live generation tracks its session")
	require.False(t, prior.IsTrackedSession(sess),
		"the superseded generation does NOT: a tracker closure left on it answers false for a running session")

	// The exact harm shape the re-arm prevents: the manager holding the stale
	// method value cannot see the session at all.
	stale := prior.IsTrackedSession
	armed := live.IsTrackedSession
	require.False(t, stale(sess), "stale closure ⇒ 存活会话被判未跟踪（孤儿裁决会把它 retire）")
	require.True(t, armed(sess), "re-armed closure ⇒ 监视随换代继续")
}

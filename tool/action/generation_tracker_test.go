package action

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorsArePerGeneration(t *testing.T) {
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

	stale := prior.IsTrackedSession
	armed := live.IsTrackedSession
	require.False(t, stale(sess), "stale closure ⇒ 存活会话被判未跟踪（孤儿裁决会把它 retire）")
	require.True(t, armed(sess), "re-armed closure ⇒ 监视随换代继续")
}

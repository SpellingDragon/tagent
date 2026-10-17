// 契约: docs/wiki/tool/tool-architecture.md#resident-continuity
package action

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMonitorsArePerGeneration(t *testing.T) {
	prior := &ActionTool{tmuxMonitor: NewTmuxMonitor(), residentMetaDirOverride: t.TempDir()}
	live := &ActionTool{tmuxMonitor: NewTmuxMonitor(), residentMetaDirOverride: t.TempDir()}
	require.NotSame(t, prior.tmuxMonitor, live.tmuxMonitor)
	const sess = "tagent-33-generation-tracker"
	live.tmuxMonitor.AddSession(&TmuxSession{ID: sess})
	require.True(t, live.IsTrackedSession(sess))
	require.False(t, prior.IsTrackedSession(sess))
}

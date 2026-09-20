package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §5.8 — the recovery-inventory barrier at the bus boundary: registration runs
// INSIDE a hold (forgetting pauses for its duration, including late attaches on
// an already-running store), and an UNREADABLE inbox keeps the hold raised
// (explicit block) instead of releasing the gate on an incomplete view.

type barrierTrackGuard struct {
	begins, ends, arms int
	protected          map[int64]int
}

func (g *barrierTrackGuard) ProtectKey(k int64) { g.protected[k]++ }
func (g *barrierTrackGuard) ReleaseKey(k int64) { g.protected[k]-- }
func (g *barrierTrackGuard) ArmRetention()      { g.arms++ }
func (g *barrierTrackGuard) BeginHold()         { g.begins++ }
func (g *barrierTrackGuard) EndHold()           { g.ends++ }

func TestArmRetention_RunsUnderBarrierAndBlocksOnUnreadableInbox(t *testing.T) {
	dir := t.TempDir()
	bus, err := NewReliableEventBus(dir)
	require.NoError(t, err)
	g := &barrierTrackGuard{protected: map[int64]int{}}
	bus.SetRetentionGuard(g)

	// Success path: exactly one nested Begin/End pair around the inventory, arm lands.
	require.NoError(t, bus.ArmRetentionFromInbox())
	require.Equal(t, 1, g.begins)
	require.Equal(t, 1, g.ends, "a completed registration releases its barrier")
	require.Equal(t, 1, g.arms)

	// Inventory failure: the barrier is RAISED AND KEPT (begin without end) —
	// forgetting stays blocked on the incomplete view; no MarkReady is signaled;
	// the caller (agent build) refuses to open ingest.
	envDir := filepath.Join(dir, "inbox-v2")
	require.NoError(t, os.Chmod(envDir, 0o000))
	defer func() { _ = os.Chmod(envDir, 0o700) }()
	require.ErrorContains(t, bus.ArmRetentionFromInbox(), "read inbox dir")
	require.Equal(t, 2, g.begins)
	require.Equal(t, 1, g.ends, "the failed inventory KEEPS its hold (explicit §5.8 block, never silently released)")
	require.Equal(t, 1, g.arms, "an incomplete view never arms the scanner gate")
}

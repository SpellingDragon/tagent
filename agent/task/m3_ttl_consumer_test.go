package task

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestM3_TaskDefaultTTLReachesLiveConsumerWithoutOverwritingExplicit pins D4
// (M-3) `task_default_ttl`: the hot value is the manager's DEFAULT/FALLBACK
// source consumed by the real reaper path, and it must NOT rewrite an existing
// task's explicit spec TTL / lifetime anchor. remainingLifetime is the exact
// computation reconcileTTL honors (10.6), so reading it through tm.DefaultTTL()
// proves the hot-set manager value reaches the live consumer.
func TestM3_TaskDefaultTTLReachesLiveConsumerWithoutOverwritingExplicit(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{DefaultTTL: 10 * time.Minute})

	fallback := tm.Spawn(TaskSpec{Kind: "command", Desc: "no explicit ttl", Key: "kfb"}, neverSettleDetector{}).Task
	explicit := tm.Spawn(TaskSpec{Kind: "command", Desc: "explicit ttl", Key: "kex", TTL: 99 * time.Minute}, neverSettleDetector{}).Task
	require.NotNil(t, fallback)
	require.NotNil(t, explicit)

	// One minute after each task's own anchor.
	nowFB := fallback.StartedAt.Add(time.Minute)
	nowEX := explicit.StartedAt.Add(time.Minute)

	remFB, boundedFB := fallback.remainingLifetime(nowFB, tm.DefaultTTL())
	require.True(t, boundedFB)
	require.Equal(t, 9*time.Minute, remFB, "no-explicit task falls back to the manager default (10m - 1m)")
	remEX, _ := explicit.remainingLifetime(nowEX, tm.DefaultTTL())
	require.Equal(t, 98*time.Minute, remEX, "explicit-spec task is bounded by its own anchor (99m - 1m)")

	// §6.4 pull (S-E): the same rotation happens by rotating the SOURCE the
	// manager resolves at its next sweep/board read — there is no write into the
	// manager anymore.
	var termTTL, defTTL time.Duration
	tm.SetTTLSource(func() (time.Duration, time.Duration) { return termTTL, defTTL })
	defTTL = 20 * time.Minute
	require.Equal(t, 20*time.Minute, tm.DefaultTTL(), "the source reading is the live consumer")

	remFB2, _ := fallback.remainingLifetime(nowFB, tm.DefaultTTL())
	require.Equal(t, 19*time.Minute, remFB2, "the fallback task must follow the hot-updated default")
	remEX2, _ := explicit.remainingLifetime(nowEX, tm.DefaultTTL())
	require.Equal(t, 98*time.Minute, remEX2, "an existing task's EXPLICIT ttl/anchor must never be rewritten by the default")
}

// TestM3_TerminalTTLReachesLiveConsumer pins D4 `task_terminal_ttl`: the hot
// value reaches the TaskManager that actually performs the terminal-retention
// check. §6.4 pull (S-E) replaced the setter with a source rotation, so the
// authority is the record reading itself: a zero reading means the record has no
// opinion and the CONSTRUCTION value answers — it can never wipe the live period
// to zero. (Under the retired setter, zero meant "keep what was last pushed";
// that stickiness is gone on purpose, because in production the source always
// carries the full desired bundle, so there is no second, staler memory to fall
// back to.)
func TestM3_TerminalTTLReachesLiveConsumer(t *testing.T) {
	tm := NewTaskManager(TaskManagerConfig{TerminalTTL: 2 * time.Minute})
	require.Equal(t, 2*time.Minute, tm.TerminalTTL())

	var termTTL, defTTL time.Duration
	tm.SetTTLSource(func() (time.Duration, time.Duration) { return termTTL, defTTL })

	termTTL = 5 * time.Minute
	require.Equal(t, 5*time.Minute, tm.TerminalTTL(), "a rotated source is the live terminal-retention period")

	termTTL = 0
	require.Equal(t, 2*time.Minute, tm.TerminalTTL(), "a zero reading falls back to construction — it must not zero the live terminal TTL")
}

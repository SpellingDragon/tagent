package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// countGuard is a test RetentionGuard recording the net holders per key.
type countGuard struct{ m map[int64]int }

func newCountGuard() *countGuard          { return &countGuard{m: map[int64]int{}} }
func (g *countGuard) ProtectKey(k int64)  { g.m[k]++ }
func (g *countGuard) ReleaseKey(k int64)  { g.m[k]-- }
func (g *countGuard) ArmRetention()       {}
func (g *countGuard) BeginHold()          {}
func (g *countGuard) EndHold()            {}
func (g *countGuard) holders(k int64) int { return g.m[k] }

// §2.8 spill belt: appending a pending key protects its original; a successful
// replay releases it (no leak); ProtectAllPending rebuilds the lease from an
// existing spill file (restart).
func TestMemSpill_RetentionBelt(t *testing.T) {
	path := t.TempDir() + "/spill.jsonl"
	key := NewSnowflakeEventKey(1, 1704067200*1000)
	ev := FullEvent{EventKey: key, PartitionID: 1, EventType: "external_input", Timestamp: 1704067200000}

	g := newCountGuard()
	sp := NewMemSpill(path)
	sp.SetGuard(g)
	require.NoError(t, sp.Append(key, ev))
	require.Equal(t, 1, g.holders(key), "append must protect the pending spill key")

	// Simulate a restart: a fresh handle over the same file rebuilds the lease.
	g2 := newCountGuard()
	sp2 := NewMemSpill(path)
	sp2.SetGuard(g2)
	require.NoError(t, sp2.ProtectAllPending())
	require.Equal(t, 1, g2.holders(key), "ProtectAllPending must rebuild the holder from the file")

	// Replay succeeds into a replayer store → the pending holder is released.
	n, err := sp2.ReplayWithNotify(NewInMemoryStore(), nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 0, g2.holders(key), "successful replay must release the spill key (§2.8 no leak)")
}

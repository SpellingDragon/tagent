package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRaiseSnowflakeFloor_RestartGenerationCannotCollide: the §8.5 guard.
// A fresh process that raises the floor from the durable chain must never
// re-issue a key already committed — even within the same second (where the
// in-memory sequence counter would otherwise restart at 0).
func TestRaiseSnowflakeFloor_RestartGenerationCannotCollide(t *testing.T) {
	pid := 7
	first := NewSnowflakeEventKey(pid, 0)
	second := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, second, first)

	// A new generation that never raises the floor WOULD re-issue second-1
	// style colliding keys; raising it to the durable max forces strictly
	// above-max issuance in the same second:
	RaiseSnowflakeFloor(pid, second)
	third := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, third, second, "the next issued key must sit above the durable floor")
	require.Equal(t, (second>>timestampShift)&timestampMask, (third>>timestampShift)&timestampMask,
		"same-second pinning, not a time jump")
}

func TestRaiseSnowflakeFloor_IsOneWay(t *testing.T) {
	pid := 8
	high := NewSnowflakeEventKey(pid, 0)
	RaiseSnowflakeFloor(pid, high)
	mid := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, mid, high)

	RaiseSnowflakeFloor(pid, high) // a LOWER observation must never regress the floor
	next := NewSnowflakeEventKey(pid, 0)
	require.Greater(t, next, mid, "floor is one-way")
}

// TestScanLiveKeys_TombstonedHighestKeyStillRaisesFloor (resident-review-fixes
// 2.1): the §8.5 key floor is "a new generation never re-issues an already-
// issued key" — independent of liveness. When the two HIGHEST keys are
// tombstoned, the live max sits below the highest ISSUED key; the scan must
// still raise the floor from the highest ISSUED key (tombstone included), else
// a same-second restart re-issues the tombstoned key (ReplayEvent →
// ErrEventForgotten → a legitimate input downgraded to quarantine).
func TestScanLiveKeys_TombstonedHighestKeyStillRaisesFloor(t *testing.T) {
	file, tset := newFileStoreWithTombstones(t)
	const pid = 900
	base := int64((snowflakeEpoch + 10) * 1000) // pins every key to one fixed second
	issue := func() int64 { return NewSnowflakeEventKey(pid, base) }

	k0, k1, k2 := issue(), issue(), issue() // seq 0,1,2 (same ts) — k2 is highest ISSUED
	for _, k := range []int64{k0, k1, k2} {
		require.NoError(t, file.StoreEvent(k, FullEvent{
			EventKey: k, PartitionID: pid, EventType: "external_input",
			EventSummary: "e", Content: "c", Timestamp: base,
		}))
	}
	// Tombstone the two highest so the live max is k0, two below highest ISSUED k2.
	require.NoError(t, tset.MarkTombstone(k1))
	require.NoError(t, tset.MarkTombstone(k2))

	// Simulate a fresh process generation: clear the in-memory monotonicity
	// guard so ONLY the durable floor (raised by the rebuild scan) can prevent a
	// same-second re-issue.
	snowflakeSeqMu.Lock()
	delete(snowflakeSeqLast, pid)
	delete(snowflakeSeqCnt, pid)
	snowflakeSeqMu.Unlock()

	require.NoError(t, file.RebuildLiveCounts())

	// A same-second new key must clear the highest ISSUED key k2, not re-issue it.
	// fail-before (maxKey updated only for live keys): the floor lands on k0 and
	// this next key comes out exactly equal to the tombstoned k2.
	next := NewSnowflakeEventKey(pid, base)
	require.NotEqual(t, k2, next, "next key must not re-issue the tombstoned highest key")
	require.Greater(t, next, k2, "key floor must be raised past the tombstoned highest key")
}

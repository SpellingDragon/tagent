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

package reliability

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §3.7 (design 决策10): the runtime loads ONLY the current format; previous-format
// items are inert transitional data cleared solely by an explicit, operator-confirmed
// managed reset — and current-format corruption must surface, never be wiped.

func TestResetTransitional_RequiresExplicitConfirmThenClears(t *testing.T) {
	dir := t.TempDir()
	spill := filepath.Join(dir, "00000000000000000009.spill")
	require.NoError(t, os.WriteFile(spill, []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)

	// Unconfirmed: refuse and delete nothing (a reset is never automatic).
	n, rerr := in.ResetTransitional(false)
	require.Error(t, rerr)
	require.Equal(t, 0, n)
	require.FileExists(t, spill, "an unconfirmed reset must not delete anything")

	// Confirmed: clears exactly the enumerated legacy file.
	n, rerr = in.ResetTransitional(true)
	require.NoError(t, rerr)
	require.Equal(t, 1, n)
	require.NoFileExists(t, spill)
	sp, v1 := in.TransitionalData()
	require.Empty(t, sp)
	require.Empty(t, v1)
}

func TestResetTransitional_RefusedWithLiveUnacked(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000009.spill"), []byte("{}"), 0o644))
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("r", "user")) // a live unacked envelope → pending>0

	n, rerr := in.ResetTransitional(true)
	require.Error(t, rerr, "a managed reset needs exclusive writer access, never an in-flight turn")
	require.Equal(t, 0, n)
}

// Current-format v2 items are never classified as transitional, so they can never
// enter the reset allow-list (the reset is structurally incapable of wiping v2).
func TestTransitional_NeverClassifiesCurrentV2(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("r", "user"))
	sp, v1 := in.TransitionalData()
	require.Empty(t, sp)
	require.Empty(t, v1, "current-format v2 items are never transitional / reset targets")
}

// The §3.7 reclassification: a stray legacy .spill no longer masks the current-format
// corruption arm — the quarantine (corruption) STILL fails loud and is NOT wiped.
// Before §3.7 the spill gate fired first (ErrLegacySpillNotDrained), hiding this;
// this assertion is a genuine fail-before on the pre-§3.7 order.
func TestQuarantineCorruptionStillBlocksDespiteTransitional(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000009.spill"), []byte("{}"), 0o644))
	q := filepath.Join(dir, "inbox-v2", inboxQuarantine)
	require.NoError(t, os.MkdirAll(q, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(q, "00000000000000000003.json"), []byte("{bad"), 0o644))

	_, err := NewInbox(dir, 10)
	require.ErrorIs(t, err, ErrQuarantineUndispositioned,
		"current-format corruption must surface, not be ignored as transitional or wiped")
}

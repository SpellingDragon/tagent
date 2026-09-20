package reliability

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// §3.6 (spec L96/L110): removal is durable only after BOTH unlink and its directory
// sync. An ack whose dir-sync fails keeps capacity + the retention lease and opens
// an independent cleanup account; a later retry (or per-turn drain) completes the
// owed barrier and releases exactly once. The syncDirFunc seam (§3.2) drives the
// failure. Each fail-before is recorded in evidence.md.

// ackToReceipted walks one envelope through the lifecycle to the receipted state so
// Ack can be exercised.
func ackToReceipted(t *testing.T, in *Inbox) string {
	t.Helper()
	mustEnqueue(t, in, env("r-ack", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)
	require.NoError(t, in.PrepareFacts(path, "rk",
		[]json.RawMessage{json.RawMessage(`{"event_key":555,"event_summary":"s","prepared_version":1}`)}))
	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(path, ReceiptCredential{ReceiptKey: "rk"}))
	return path
}

func TestAck_UncertainDirSyncRetainsCapacityThenRetryCompletesBarrier(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	path := ackToReceipted(t, in)
	require.Equal(t, int64(1), in.Pending())

	orig := syncDirFunc
	defer func() { syncDirFunc = orig }()

	// Ack with a failing dir-sync: the unlink lands but the barrier is unconfirmed.
	syncDirFunc = func(string) error { return errors.New("dir sync boom") }
	aerr := in.Ack(path)
	require.Error(t, aerr, "an uncertain cleanup (sync failed) must not report success")
	_, statErr := os.Stat(path)
	require.True(t, os.IsNotExist(statErr), "the file was unlinked")
	require.Equal(t, int64(1), in.Pending(),
		"capacity must be RETAINED while the removal barrier is unconfirmed (§3.6, no premature release)")

	// Barrier now succeeds: the retry on the already-missing file must still
	// complete it and release capacity exactly once.
	syncDirFunc = orig
	require.NoError(t, in.Ack(path), "retry of an owed cleanup must complete the barrier")
	require.Equal(t, int64(0), in.Pending(), "capacity released once the barrier is durable")

	// A further ack is a no-op — never a second release.
	require.NoError(t, in.Ack(path))
	require.Equal(t, int64(0), in.Pending())
}

func TestAck_OwedBarrierStillFailingKeepsCapacityAndLease(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	path := ackToReceipted(t, in)

	orig := syncDirFunc
	defer func() { syncDirFunc = orig }()
	syncDirFunc = func(string) error { return errors.New("dir sync boom") }

	require.Error(t, in.Ack(path)) // uncertain → opens account, retains capacity
	require.Equal(t, int64(1), in.Pending())
	require.Len(t, in.DrainCleanups(), 0, // drain while sync still fails releases nothing
		"a drain that cannot sync must keep the account owed")
	require.Equal(t, int64(1), in.Pending())

	syncDirFunc = orig // barrier recoverable
	released := in.DrainCleanups()
	require.Len(t, released, 1, "drain completes the owed barrier and returns the protected material")
	require.Contains(t, released[0].FactKeys, int64(555),
		"the returned material must carry the frozen fact key so the caller releases the right lease (§2.8)")
	require.Equal(t, int64(0), in.Pending(), "capacity released once by the drain")
	require.Len(t, in.DrainCleanups(), 0, "no double release on a subsequent drain")
}

package reliability

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// §5.6 — the cleanup-account boundary: receipted items are never swept outside
// the Ack protocol, and the independent cleanup account is registered BEFORE
// the unlink, so a definitively failed remove leaves NO phantom owed entry.

// TestNextClaimable_ReturnsReceiptedNeverSweeps locks L96「nextClaimable 不私
// 自删除 receipted 项」: a receipted-but-unacked envelope must survive repeated
// claim scans UNCHANGED on disk and in capacity — deletion and the exactly-once
// capacity/index/retention release belong exclusively to Ack (whose caller
// pairs it with releaseRetention). Fail-before: with the pre-§2.8 sweep (leaf
// removing receipted items during the scan), the second ClaimNext finds the
// file gone and Pending already dropped without its barrier.
func TestNextClaimable_ReturnsReceiptedNeverSweeps(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("sweep-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)
	cred := reserveCredential(t, in, p)
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p, cred)) // receipted, NOT acked

	for i := 0; i < 3; i++ {
		envOut, pOut, cerr := in.ClaimNext()
		require.NoError(t, cerr)
		require.NotNil(t, envOut, "scan %d: the receipted item must be RETURNED, never swept", i)
		require.Equal(t, p, pOut)
		require.Equal(t, InboxStateReceipted, envOut.State, "returned unchanged, not re-claimed")
		require.FileExists(t, p, "scan %d: deletion may only happen through Ack", i)
	}
	require.Equal(t, int64(1), in.Pending(),
		"capacity stays held until an Ack completes the barrier (no silent release on scan)")

	require.NoError(t, in.Ack(p))
	require.NoFileExists(t, p)
	require.Equal(t, int64(0), in.Pending())
}

// TestAck_RemoveFailureLeavesNoPhantomAccount: §5.6 registers the cleanup
// account BEFORE the unlink; when the remove itself definitively fails the
// original is still on disk and the account MUST be cancelled — a phantom owed
// entry would let a later DrainCleanups finalize a barrier for a file that was
// never removed (releasing capacity and the lease while the envelope still
// exists → double release on the real Ack).
func TestAck_RemoveFailureLeavesNoPhantomAccount(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("phantom-1", "user"))
	_, p, err := in.ClaimNext()
	require.NoError(t, err)
	cred := reserveCredential(t, in, p)
	require.NoError(t, in.RecordCompletion(p, json.RawMessage(`{"result":"completed"}`)))
	require.NoError(t, in.RecordReceipt(p, cred))

	envDir := filepath.Dir(p)
	require.NoError(t, os.Chmod(envDir, 0o500)) // unlink inside a read-only dir fails
	defer func() { _ = os.Chmod(envDir, 0o700) }()
	_, rerr := os.CreateTemp(envDir, "probe")
	if rerr == nil {
		t.Skip("platform allows unlink from a read-only dir; cannot induce a remove failure")
	}

	require.ErrorContains(t, in.Ack(p), "ack remove")
	require.FileExists(t, p, "the untouched original stays")

	// No phantom account: a drain must finalize nothing (capacity/lease untouched).
	require.NoError(t, os.Chmod(envDir, 0o700))
	require.Empty(t, in.DrainCleanups(), "a failed remove cancelled the pre-registered account")
	require.Equal(t, int64(1), in.Pending(), "the envelope still counts until its real Ack")

	require.NoError(t, in.Ack(p))
	require.Equal(t, int64(0), in.Pending(), "capacity released exactly once, by the real barrier")
}

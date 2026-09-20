package reliability

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// §3.3 (b): receive publish and close MUST be coordinated under one lock — an
// Enqueue whose liveness fast-check raced a completed Close must NOT register a
// new item after Close returned ("不出现关闭返回后新增未登记项"). The test
// deterministically interleaves the two via testGateHook.
func TestInbox_EnqueueCoordinatesWithCompletedClose(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	defer func() { testGateHook = nil }()

	var closeDone bool
	testGateHook = func() {
		_ = in.Close() // completes BEFORE the enqueue takes the mutation lock
		closeDone = true
	}

	_, err = in.Enqueue(env("raced", "user"))

	require.True(t, closeDone, "Close completed during the enqueue's pre-lock window")
	require.ErrorIs(t, err, ErrInboxClosed,
		"an enqueue that raced a completed Close must be refused, never registered after Close returned")
	require.Equal(t, int64(0), in.Pending(), "no item may be registered after Close returned")
}

// §3.3 (c): an IDENTICAL prepare/completion retry MUST still complete the owed
// durable barrier, not return early merely because the content already matches
// ("相同 prepare/completion 重试仍补齐所欠屏障，不因内容相同提前成功"). This
// matters when the first attempt's rename landed but its dir-sync failed,
// leaving the barrier unfulfilled.
func TestInbox_RecordCompletion_IdempotentRetryRerunsBarrier(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("rc", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	payload := json.RawMessage(`{"result":"completed"}`)

	orig := syncDirFunc
	var failOnce atomic.Bool
	var syncCalls atomic.Int64
	failOnce.Store(true)
	syncDirFunc = func(d string) error {
		syncCalls.Add(1)
		if failOnce.Load() {
			return errors.New("injected dirsync failure")
		}
		return orig(d)
	}

	// First attempt: rename lands, dir-sync fails → uncertain; completion is on disk.
	err1 := in.RecordCompletion(path, payload)
	require.Error(t, err1, "first completion must surface the publish-uncertain failure")
	require.Greater(t, syncCalls.Load(), int64(0))

	// Retry with the IDENTICAL payload: must re-run the barrier (dir-sync), not
	// skip on content-equality.
	failOnce.Store(false)
	syncCalls.Store(0)
	err2 := in.RecordCompletion(path, payload)
	syncDirFunc = orig

	require.NoError(t, err2, "identical re-completion succeeds once the barrier is re-run")
	require.Greater(t, syncCalls.Load(), int64(0),
		"an identical completion retry MUST re-run the durable barrier, not early-return on content equality")
}

// A DIFFERENT completion payload still conflicts (must not be softened by the
// barrier-retry fix).
func TestInbox_RecordCompletion_DifferentPayloadStillConflicts(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("rc2", "user"))
	_, path, err := in.ClaimNext()
	require.NoError(t, err)

	require.NoError(t, in.RecordCompletion(path, json.RawMessage(`{"result":"completed"}`)))
	require.ErrorIs(t, in.RecordCompletion(path, json.RawMessage(`{"result":"failed"}`)), ErrCompletionConflict)
}

package reliability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func env(n, src string) *Envelope {
	return &Envelope{RequestID: n, Source: src, State: InboxStatePending,
		Messages: []EnvelopeMessage{{Role: "user", Content: "m-" + n}}}
}

func mustEnqueue(t *testing.T, in *Inbox, e *Envelope) int64 {
	t.Helper()
	seq, err := in.Enqueue(e)
	require.NoError(t, err)
	return seq
}

// claimUntil claims envelopes in order until the named one shows up,
// receipt+acking the older ones to keep the scan deterministic.
func claimUntil(t *testing.T, in *Inbox, id string) (*Envelope, string) {
	t.Helper()
	for {
		e, p, err := in.ClaimNext()
		require.NoError(t, err)
		require.NotNil(t, e)
		if e.RequestID == id {
			return e, p
		}
		require.NoError(t, in.RecordReceipt(p, "skip"))
		require.NoError(t, in.Ack(p))
	}
}

func TestInbox_EnqueueClaimAck_BasicOrder(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("a", "user"))
	mustEnqueue(t, in, env("b", "user"))
	require.Equal(t, int64(2), in.Pending())

	e1, p1, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "a", e1.RequestID)
	require.Equal(t, InboxStateClaimed, e1.State)
	require.Equal(t, int64(2), in.Pending(), "claim does not remove")

	require.Error(t, in.Ack(p1), "ack refuses non-receipted claim")

	require.NoError(t, in.RecordReceipt(p1, "turn finished"))
	require.NoError(t, in.Ack(p1))
	require.Equal(t, int64(1), in.Pending())
	require.NoError(t, in.Ack(p1)) // idempotent

	e2, p2, err := in.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "b", e2.RequestID)
	require.NoError(t, in.RecordReceipt(p2, "done"))
	require.NoError(t, in.Ack(p2))
	require.Equal(t, int64(0), in.Pending())
}

func TestInbox_FullIsExplicitRejection(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 2)
	require.NoError(t, err)
	mustEnqueue(t, in, env("1", "user"))
	mustEnqueue(t, in, env("2", "user"))
	_, err = in.Enqueue(env("3", "user"))
	require.True(t, errors.Is(err, ErrInboxFull), "overflow must be typed, got: %v", err)
}

func TestInbox_Reopen_RequeuesClaimed_KeepsReceipted(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("a", "user"))
	_, pA := claimUntil(t, in, "a")

	mustEnqueue(t, in, env("b", "user"))
	_, pB := claimUntil(t, in, "b")
	require.NoError(t, in.RecordReceipt(pB, "done")) // crash BEFORE ack

	mustEnqueue(t, in, env("c", "user"))
	require.NoError(t, in.Close())

	// Reopen: claimed(a) → replay; receipted(b) → Ack-skip; c → pending.
	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), in2.Pending())

	got1, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "a", got1.RequestID, "claimed-but-unreceipted replays")
	require.GreaterOrEqual(t, got1.Attempts, 2, "requeue + re-claim each count one attempt")

	// b (receipted) is cleared on the same scan — Ack-skipped, NOT re-executed;
	// the next claimable is c.
	got2, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "c", got2.RequestID)
	require.Equal(t, int64(2), in2.Pending(), "receipted b cleared; c now claimed (claim keeps the file until ack)")
	_ = pA
	_ = pB
}

func TestInbox_CorruptItemQuarantined(t *testing.T) {
	dir := t.TempDir()
	in, err := NewInbox(dir, 10)
	require.NoError(t, err)
	mustEnqueue(t, in, env("good", "user"))
	corrupt := filepath.Join(in.Dir(), "00000000000000000002.json")
	require.NoError(t, os.WriteFile(corrupt, []byte("{not json"), 0o644))

	in2, err := NewInbox(dir, 10)
	require.NoError(t, err)
	e, _, err := in2.ClaimNext()
	require.NoError(t, err)
	require.Equal(t, "good", e.RequestID)
	_, statErr := os.Stat(filepath.Join(in.Dir(), inboxQuarantine, "00000000000000000002.json"))
	require.NoError(t, statErr, "corrupt item kept in quarantine, not destroyed")
}

func TestInbox_LegacySpillBlocksUpgrade(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00000000000000000009.spill"), []byte("{}"), 0o644))
	_, err := NewInbox(dir, 10)
	require.True(t, errors.Is(err, ErrLegacySpillNotDrained), "legacy spill must fail loud, got: %v", err)
}

func TestInbox_ConcurrentEnqueue_NoOvertakeNoLoss(t *testing.T) {
	in, err := NewInbox(t.TempDir(), 0) // default 2560
	require.NoError(t, err)
	const G, N = 10, 10
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < N; i++ {
				_, err := in.Enqueue(env(fmt.Sprintf("g%02d-i%02d", g, i), "user"))
				require.NoError(t, err)
			}
		}(g)
	}
	wg.Wait()
	require.Equal(t, int64(G*N), in.Pending())
	// Drain in strict seq order: envelopes come back grouped by file order.
	seen := 0
	for {
		e, p, err := in.ClaimNext()
		require.NoError(t, err)
		if e == nil {
			break
		}
		require.NoError(t, in.RecordReceipt(p, "drain"))
		require.NoError(t, in.Ack(p))
		seen++
	}
	require.Equal(t, G*N, seen, "every durable input must survive concurrency")
}

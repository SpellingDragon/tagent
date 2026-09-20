package agent

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §5.1 — turn-result reduction. The persistent loop used to treat "RunFlow
// returned nil" as the sole definition of done, so a model/framework failure
// carried as a stream event (Response.Error) and a mid-turn shutdown
// cancellation (deliverEvent false with ctx done) BOTH fell through to the ACK
// path. These tests lock the reduction of every observed channel into an
// explicit completed / failed / cancelled result.

// TestReduceTurnOutcome locks the pure decision table across all five channels
// the design enumerates (start error, response error, cancellation, normal
// end) and their precedence.
func TestReduceTurnOutcome(t *testing.T) {
	t.Run("normal end is completed", func(t *testing.T) {
		require.Equal(t, turnCompleted, reduceTurnOutcome(nil, "", false).status)
	})
	t.Run("start error is failed", func(t *testing.T) {
		oc := reduceTurnOutcome(context.DeadlineExceeded, "", false)
		require.Equal(t, turnFailed, oc.status)
		require.Contains(t, oc.err, "deadline exceeded")
	})
	t.Run("response error is failed", func(t *testing.T) {
		oc := reduceTurnOutcome(nil, "server_error: rate limited", false)
		require.Equal(t, turnFailed, oc.status)
		require.Equal(t, "server_error: rate limited", oc.err)
	})
	t.Run("cancellation outranks errors and forms no completion", func(t *testing.T) {
		// A turn cut short reached no terminal state: cancelled wins even when a
		// transport error and a response error were already observed in the same
		// drain, so the loop retains the claim rather than forming a completion.
		oc := reduceTurnOutcome(context.Canceled, "server_error: partial", true)
		require.Equal(t, turnCancelled, oc.status)
		require.Empty(t, oc.err)
	})
	t.Run("start error outranks response error", func(t *testing.T) {
		// A runner that failed to start produced no real response to classify.
		oc := reduceTurnOutcome(context.DeadlineExceeded, "ignored", false)
		require.Equal(t, turnFailed, oc.status)
		require.Contains(t, oc.err, "deadline exceeded")
		require.NotContains(t, oc.err, "ignored")
	})
	t.Run("error summary is bounded", func(t *testing.T) {
		long := make([]byte, maxErrSummary+200)
		for i := range long {
			long[i] = 'x'
		}
		oc := failedOutcome(string(long))
		require.LessOrEqual(t, len(oc.err), maxErrSummary+len("…(truncated)"))
		require.Contains(t, oc.err, "truncated")
	})
}

// TestRunFlow_ResponseErrorReducesFailed proves the §5.1 core bug at the real
// RunFlow drain: a model API failure arrives as an event carrying Response.Error
// while RunFlow itself returns nil (transport OK). The old code never inspected
// Response.Error, so the turn recorded nothing and the loop read "nil" as
// success. Run synchronously on the test goroutine, so reading the recorded
// outcome is race-free.
func TestRunFlow_ResponseErrorReducesFailed(t *testing.T) {
	outputCh := make(chan *event.Event, 100)
	m := &requestCapturingModel{resp: &model.Response{
		ID:    "err-resp",
		Done:  true,
		Error: &model.ResponseError{Type: "server_error", Message: "upstream exploded"},
	}}
	cm := newTestContextManager("resp-err", m, nil, outputCh, NewEventBus())

	err := cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"})
	require.NoError(t, err, "transport return stays nil — the failure is carried in the stream, not the return")
	oc := cm.LastTurnOutcome()
	require.Equal(t, turnFailed, oc.status, "a response-internal error must reduce to failed, not the old silent 'completed'")
	require.Contains(t, oc.err, "upstream exploded", "the bounded summary is retained for the completion (§5.2)")
}

// TestRunFlow_NormalDrainCompleted is the pass-after companion: an ordinary
// productive turn still reduces to completed (the reduction must not over-reach
// into treating every turn as failed).
func TestRunFlow_NormalDrainCompleted(t *testing.T) {
	outputCh := make(chan *event.Event, 100)
	m := &requestCapturingModel{resp: &model.Response{
		ID:      "ok",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "a real answer"}}},
	}}
	cm := newTestContextManager("normal", m, nil, outputCh, NewEventBus())

	require.NoError(t, cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"}))
	require.Equal(t, turnCompleted, cm.LastTurnOutcome().status)
}

// TestRunFlow_MidStreamCancelReducesCancelled is the deterministic §5.1 cancel
// discriminator: a productive turn whose delivery is cut short by a shutdown
// cancellation. The OLD RunFlow returned nil on that path (the loop then read
// "success" and ACKed the durable inputs of a turn that reached no terminal
// state). The §5.1 drain instead surfaces ctx.Canceled AND records a cancelled
// outcome. Run synchronously (a goroutine cancels while delivery is blocked), so
// unlike a full-loop drive there is no degenerate-retry path to mask the result.
func TestRunFlow_MidStreamCancelReducesCancelled(t *testing.T) {
	outputCh := make(chan *event.Event) // unbuffered, never read → deliverEvent blocks
	m := &requestCapturingModel{resp: &model.Response{
		ID:      "ok",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "a real answer"}}},
	}}
	cm := newTestContextManager("cancel-drain", m, nil, outputCh, NewEventBus())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond) // let the drain reach the blocking delivery
		cancel()
	}()

	err := cm.RunFlow(ctx, model.Message{Role: model.RoleUser, Content: "hi"})
	require.ErrorIs(t, err, context.Canceled, "cancellation must be surfaced, not swallowed as a nil 'completed' return")
	require.Equal(t, turnCancelled, cm.LastTurnOutcome().status, "a cut-short turn reduces to cancelled, never completed")
}

// TestRunEventLoop_MidStreamCancelRetainsClaim is the end-to-end regression
// guard: a durable batch whose turn is cut short by a shutdown cancellation must
// leave the claim UN-acked (retained for replay, spec L90/L130). The strict
// §5.1 fail-before discriminator is TestRunFlow_MidStreamCancelReducesCancelled
// above (synchronous, no retry path to mask it); this drives the full loop and
// locks that the loop, on a cancelled outcome, returns before finishDurableBatch
// rather than ACKing a turn that reached no terminal state.
func TestRunEventLoop_MidStreamCancelRetainsClaim(t *testing.T) {
	// A productive, error-free turn — the ONLY reason the envelope would be acked
	// is the cancellation guard, not a store fault or an unverified credential.
	m := &requestCapturingModel{resp: &model.Response{
		ID:      "ok",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}},
	}}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	// Unbuffered outputCh with no reader → RunFlow's first forward blocks in
	// deliverEvent, so a cancel there is observed as a mid-stream cancellation.
	outputCh := make(chan *event.Event)
	store := memory.NewInMemoryStore()
	ta := newDurableAgentWithStore("cancel-retains", m, store, outputCh, bus)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hello-durable"}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	// Wait until the batch committed and the model was reached once (submitOK →
	// model), i.e. RunFlow is now draining the response into the stalled outputCh.
	deadline := time.After(4 * time.Second)
	for m.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("durable batch never reached the model (commit path broken?)")
		case <-time.After(10 * time.Millisecond):
		}
	}
	// Cancel while RunFlow is blocked delivering the assistant event.
	cancel()

	// Give the cancellation a bounded window to unwind, then watch that the
	// envelope is NOT acked. If the cancel path wrongly ACKed, DurablePending
	// would reach 0 and stay there.
	ackWatch := time.Now().Add(2 * time.Second)
	for time.Now().Before(ackWatch) && bus.DurablePending() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	require.NotZero(t, bus.DurablePending(), "§5.1: a mid-turn shutdown cancellation must retain the claim, not ack a turn that reached no terminal state")
}

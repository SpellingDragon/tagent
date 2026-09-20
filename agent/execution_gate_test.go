package agent

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/plugin"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// §4.5(A) tests: the execution gate MUST block the real model call when a durable turn
// installed an echo credential that is not Verified() — because the framework logs a
// plugin error and continues, the credential STATE (not the returned error) is the
// authority, and the gate enforces it at the actual model entry. It MUST pass through on a
// verified credential, on a rejected credential, and when no credential is present
// (volatile turn). The blocked model must not be invoked at all.

func gateOKResp() *model.Response {
	return &model.Response{ID: "r", Done: true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "ok"}}}}
}

func plainReq() *model.Request {
	return &model.Request{Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}}
}

func newGate(t *testing.T) (*executionGateModel, *requestCapturingModel, *ContextManager) {
	t.Helper()
	cm := newTestContextManager("gate", &loopMockModel{}, nil, nil, nil)
	inner := &requestCapturingModel{resp: gateOKResp()}
	return newExecutionGateModel(inner, cm), inner, cm
}

// A durable turn whose credential is not yet bound MUST be blocked (no inner call).
func TestExecutionGate_BlocksUnverifiedCredential(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"} // installed, not bound
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	_, err := g.GenerateContent(ctx, plainReq())
	require.ErrorIs(t, err, ErrExecutionCredentialUnverified)
	require.Equal(t, 0, inner.requestCount(), "blocked model must NOT be invoked")
}

// A bound credential (Verified) passes through to the inner model.
func TestExecutionGate_PassesVerifiedCredential(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	cred.Bind("inv-root") // bound → Verified()
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	_, err := g.GenerateContent(ctx, plainReq())
	require.NoError(t, err)
	require.Equal(t, 1, inner.requestCount(), "verified turn must reach the model")
}

// A rejected credential (bound but downgraded by a plugin error) is blocked.
func TestExecutionGate_BlocksRejectedCredential(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	cred.Bind("inv-root")
	cred.MarkRejected("plugin store error")
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	_, err := g.GenerateContent(ctx, plainReq())
	require.ErrorIs(t, err, ErrExecutionCredentialUnverified)
	require.Equal(t, 0, inner.requestCount(), "rejected turn must be blocked")
}

// No credential (volatile / non-durable turn) passes through unchanged.
func TestExecutionGate_NoCredential_Passes(t *testing.T) {
	g, inner, _ := newGate(t)
	_, err := g.GenerateContent(context.Background(), plainReq())
	require.NoError(t, err)
	require.Equal(t, 1, inner.requestCount())
}

// §4.5C: creating a lazy iterator must NOT consume the recovery notice or call the model;
// only starting iteration does. This is the "created-then-cancelled iterator is not a model
// call" guarantee the event-sourced-projection spec requires.
func TestExecutionGate_IteratorLazilyConsumesNotice(t *testing.T) {
	g, inner, cm := newGate(t)
	cm.recoveryMu.Lock()
	cm.recoveryNotice = "[recovery] lazy one-shot"
	cm.recoveryMu.Unlock()

	seq, err := g.GenerateContentIter(context.Background(), plainReq())
	require.NoError(t, err)

	// Creation touches neither the notice nor the model.
	cm.recoveryMu.Lock()
	still := cm.recoveryNotice
	cm.recoveryMu.Unlock()
	require.Equal(t, "[recovery] lazy one-shot", still, "creating the iterator must not consume the notice")
	require.Equal(t, 0, inner.requestCount(), "creating the iterator must not call the model")

	// Iteration is the actual model call → notice consumed + injected into the request.
	count := 0
	seq(func(*model.Response) bool { count++; return true })
	require.Equal(t, 1, count, "iterator yielded the response")
	cm.recoveryMu.Lock()
	cleared := cm.recoveryNotice
	cm.recoveryMu.Unlock()
	require.Equal(t, "", cleared, "iterating consumes the recovery notice")
	reqs := inner.snapshotRequests()
	require.NotEmpty(t, reqs)
	got := reqs[len(reqs)-1].Messages
	require.Equal(t, "[recovery] lazy one-shot", got[len(got)-1].Content, "notice injected at actual iteration")
}

// §4.5(A) for the iterator path: an unverified credential blocks iteration (yields
// nothing, model not called).
func TestExecutionGate_BlocksUnverifiedOnIterator(t *testing.T) {
	g, inner, _ := newGate(t)
	cred := &plugin.EchoCredential{MergedMessage: "hi"}
	ctx := plugin.WithEchoCredential(context.Background(), cred)

	seq, err := g.GenerateContentIter(ctx, plainReq())
	require.NoError(t, err) // lazy: creation succeeds
	called := false
	seq(func(*model.Response) bool { called = true; return true })
	require.False(t, called, "unverified iterator must yield nothing (blocked)")
	require.Equal(t, 0, inner.requestCount(), "blocked iterator must not call the model")
}

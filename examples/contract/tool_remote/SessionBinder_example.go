package remoteexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/tool/remote"
)

// errSessionUnbound is what the canonical session binder reports for a target it cannot
// attribute a session to.
var errSessionUnbound = errors.New("remote: the target carries no agent identity")

// localSessions is the canonical session binder: it resolves the session association of the
// explicit target from the local session the call runs under, and refuses to hand out a
// scope it cannot attribute.
type localSessions struct {
	localAgentID   string
	localSessionID string
}

// Bind returns the session scope to use for the explicit target.
func (b localSessions) Bind(_ context.Context, target remote.Target) (remote.SessionScope, error) {
	if target.AgentID == "" || b.localSessionID == "" {
		return remote.SessionScope{}, errSessionUnbound
	}
	return remote.SessionScope{
		AgentID:   b.localAgentID,
		SessionID: b.localSessionID,
		ContextID: b.localSessionID + "/" + target.AgentID,
	}, nil
}

var _ remote.SessionBinder = localSessions{}

// ExampleSessionBinder resolves the session one remote call runs under: the scope names the
// local agent and session the call is attributed to, while a target that carries no identity
// is refused rather than bound to a scope the caller cannot attribute.
//
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func ExampleSessionBinder() {
	binder := localSessions{localAgentID: "agent-1", localSessionID: "session-1"}
	scope, err := binder.Bind(context.Background(), remote.Target{AgentURL: "http://127.0.0.1:8080", AgentID: "peer"})
	if err != nil || scope.SessionID != "session-1" {
		return
	}
	if _, err := binder.Bind(context.Background(), remote.Target{AgentURL: "http://127.0.0.1:8080"}); err == nil {
		return
	}
}

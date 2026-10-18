package remoteexample

import (
	"context"
	"errors"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"

	"github.com/SpellingDragon/tagent/tool/remote"
)

// errCallUnaddressed and errCallUndelivered are what the canonical dispatcher reports for a
// call that names no identity and for one with nowhere to go.
var (
	errCallUnaddressed = errors.New("remote: the call names no invocation")
	errCallUndelivered = errors.New("remote: no delivery is wired behind the dispatcher")
)

// onceDispatcher is the canonical dispatcher: it sends one addressed request under the
// identity it was handed, keeps that identity and target across every retry it makes, and
// leaves one outcome for the call.
type onceDispatcher struct {
	deliver func(context.Context, remote.Target, a2aprotocol.SendMessageParams) (*a2aprotocol.MessageResult, error)
}

// Send delivers params to target under identity.
func (d onceDispatcher) Send(ctx context.Context, target remote.Target, identity remote.CallIdentity, params a2aprotocol.SendMessageParams, opts ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error) {
	if identity.InvocationID == "" || target.AgentURL == "" {
		return nil, errCallUnaddressed
	}
	if d.deliver == nil {
		return nil, errCallUndelivered
	}
	return d.deliver(ctx, target, params)
}

var _ remote.Dispatcher = onceDispatcher{}

// ExampleDispatcher sends one addressed call: the request goes out under the identity and
// target it was handed, a call that names no invocation is refused before anything is sent,
// and the call leaves one outcome instead of one outcome per retry.
//
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func ExampleDispatcher() {
	ctx := context.Background()
	target := remote.Target{AgentURL: "http://127.0.0.1:8080", AgentID: "peer"}
	identity := remote.CallIdentity{InvocationID: "inv-1", DecisionID: "dec-1", CallID: "call-1"}
	dispatcher := onceDispatcher{
		deliver: func(_ context.Context, got remote.Target, _ a2aprotocol.SendMessageParams) (*a2aprotocol.MessageResult, error) {
			if got != target {
				return nil, errCallUnaddressed
			}
			return &a2aprotocol.MessageResult{}, nil
		},
	}
	result, err := dispatcher.Send(ctx, target, identity, a2aprotocol.SendMessageParams{})
	if err != nil || result == nil {
		return
	}
	if _, err := dispatcher.Send(ctx, target, remote.CallIdentity{}, a2aprotocol.SendMessageParams{}); err == nil {
		return
	}
}

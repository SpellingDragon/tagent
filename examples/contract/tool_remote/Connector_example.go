package remoteexample

import (
	"context"
	"errors"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"

	"github.com/SpellingDragon/tagent/tool/remote"
)

// errTargetUnaddressed is what the canonical connector reports for a target that names no
// endpoint.
var errTargetUnaddressed = errors.New("remote: the target names no endpoint")

// addressedConnector is the canonical connector: it opens a client for the explicit target it
// is handed and never substitutes a default or the most recent one.
type addressedConnector struct{}

// Connect creates a client for the explicit target.
func (addressedConnector) Connect(_ context.Context, target remote.Target, opts ...a2aclient.Option) (*a2aclient.A2AClient, error) {
	if target.AgentURL == "" {
		return nil, errTargetUnaddressed
	}
	return a2aclient.NewA2AClient(target.AgentURL, opts...)
}

var _ remote.Connector = addressedConnector{}

// ExampleConnector opens one explicitly addressed connection: an endpoint the caller named
// gets a client, while a target that names none is refused instead of falling back to a
// default destination.
//
// 契约: docs/wiki/tool/tool-architecture.md#delegation-retry
func ExampleConnector() {
	connector := addressedConnector{}
	client, err := connector.Connect(context.Background(), remote.Target{AgentURL: "http://127.0.0.1:8080", AgentID: "peer"})
	if err != nil || client == nil {
		return
	}
	if _, err := connector.Connect(context.Background(), remote.Target{AgentID: "peer"}); err == nil {
		return
	}
}

package agent

import (
	"fmt"

	a2ago "trpc.group/trpc-go/trpc-a2a-go/server"
	a2aserver "trpc.group/trpc-go/trpc-agent-go/server/a2a"
)

// NewA2AServer creates an A2A server that exposes the given TagentAgent.
func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error) {
	if ta == nil {
		return nil, fmt.Errorf("tagent agent is required")
	}
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	srv, err := a2aserver.New(
		a2aserver.WithAgent(ta, true),
		a2aserver.WithHost(host),
	)
	if err != nil {
		return nil, fmt.Errorf("create A2A server: %w", err)
	}
	return srv, nil
}

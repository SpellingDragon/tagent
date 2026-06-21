// Package agent provides the A2A server wrapper for exposing TagentAgent remotely.
//
// TagentAgent already implements agent.Agent, so it can be directly used as the
// agent for an A2A server. The A2A server automatically maps incoming message
// metadata to Invocation.RunOptions.RuntimeState (server.go:377), so external_context
// passed by a remote parent agent's AgentToolWrapper arrives transparently.
//
// This file provides a convenience constructor that configures the A2A server
// with the TagentAgent and a host address. The server publishes an agent card
// at /.well-known/agent.json and accepts A2A protocol requests.

package agent

import (
	"fmt"

	a2ago "trpc.group/trpc-go/trpc-a2a-go/server"
	a2aserver "trpc.group/trpc-go/trpc-agent-go/server/a2a"
)

// NewA2AServer creates an A2A server that exposes the given TagentAgent via
// the A2A protocol. The server can be started with server.Start(host).
//
// TagentAgent implements agent.Agent, so no adapter is needed. The A2A server
// automatically converts A2A messages to Invocations and maps metadata to
// RuntimeState — TagentAgent.Run reads external_context from RuntimeState
// and injects it via the normal injectExternalContext mechanism.
//
// Usage:
//
//	srv, err := agent.NewA2AServer(ta, "0.0.0.0:8088")
//	if err != nil { ... }
//	go srv.Start("0.0.0.0:8088")
//
// The host parameter is used for the agent card URL. Supported formats:
//   - "localhost:8080" → "http://localhost:8080"
//   - "http://example.com" → used as-is
func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error) {
	if ta == nil {
		return nil, fmt.Errorf("tagent agent is required")
	}
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}

	srv, err := a2aserver.New(
		a2aserver.WithAgent(ta, true), // enableStreaming=true
		a2aserver.WithHost(host),
	)
	if err != nil {
		return nil, fmt.Errorf("create A2A server: %w", err)
	}

	return srv, nil
}

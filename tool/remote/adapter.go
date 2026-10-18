// 契约: docs/wiki/tool/tool-architecture.md#十七、委派调用的取消归属与重试形状
//
// Package remote adapts the locked trpc-a2a-go client to explicitly addressed
// remote agent calls: opening a connection to a named destination, sending one
// request under a fixed call identity, and carrying the session association of
// that call.
//
// The A2A wire protocol is not reimplemented here. Message encoding, JSON-RPC
// transport, agent cards and task types stay with the native
// trpc.group/trpc-go/trpc-a2a-go packages this package targets.
package remote

import (
	"context"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// Target is the explicit destination of a delegation, taken from the caller's
// declaration. A call without a resolvable target is refused; no default or
// most-recent destination is substituted.
type Target struct {
	// AgentURL is the base endpoint of the remote A2A agent.
	AgentURL string

	// AgentID names the remote agent inside the caller's declaration, and is
	// what call attribution reports.
	AgentID string
}

// CallIdentity is the identity one remote call keeps across every retry it
// makes: the owning invocation, the owning decision and the native model call
// that started it.
//
// Two attempts under one identity are one call, so a retry cannot be counted as
// a second one.
type CallIdentity struct {
	// InvocationID identifies the responsibility scope that owns the call.
	InvocationID string

	// DecisionID identifies the model decision that requested the call.
	DecisionID string

	// CallID is the native model call identifier the request carried.
	CallID string
}

// SessionScope is the session association of one remote call: the local agent
// and session that own it, plus the A2A context identifier carried on the wire
// for that pair.
type SessionScope struct {
	// AgentID is the local agent instance that owns the call.
	AgentID string

	// SessionID is the local session the call is attributed to.
	SessionID string

	// ContextID is the A2A context identifier used by this call on the wire.
	ContextID string
}

// Connector opens an explicitly addressed connection to a remote A2A agent.
//
// The redline list (2 rules):
// MUST create a client for the explicit target it is handed.
// MUST NOT resolve a target by default or reuse the most recent one.
type Connector interface {
	// Connect creates a client for the explicit target.
	Connect(ctx context.Context, target Target, opts ...a2aclient.Option) (*a2aclient.A2AClient, error)
}

// SessionBinder resolves the session association a remote call runs under.
//
// The redline list (2 rules):
// MUST resolve the session association of the explicit target.
// MUST NOT hand out a session scope the caller is not authorized to bind.
type SessionBinder interface {
	// Bind returns the session scope to use for the explicit target.
	Bind(ctx context.Context, target Target) (SessionScope, error)
}

// Dispatcher sends one addressed request and owns the retry shape of that call.
//
// The redline list (3 rules):
// MUST keep the same identity and the same target across every retry it performs.
// MUST leave one outcome for the call.
// MUST NOT record a retry as a second call or a second fact.
type Dispatcher interface {
	// Send delivers params to target under identity.
	//
	// Every retry the implementation performs keeps the same identity and the
	// same target, and leaves one outcome for the call.
	Send(ctx context.Context, target Target, identity CallIdentity, params a2aprotocol.SendMessageParams, opts ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error)
}

package remote

import (
	"context"
	"errors"
	"fmt"
	"sync"

	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	a2aprotocol "trpc.group/trpc-go/trpc-a2a-go/protocol"
)

var (
	// ErrTargetMissing reports a remote call that names no complete target: no
	// destination URL beside the agent name it belongs to.
	ErrTargetMissing = errors.New("remote: the call names no explicit target")
	// ErrTargetUndeclared reports a target outside the caller's declaration, so
	// the call is refused rather than dialled.
	ErrTargetUndeclared = errors.New("remote: the named target is not declared by the caller")
	// ErrCallIdentityMissing reports a remote call that carries no attribution
	// identity, so two attempts of it could not be recognized as one call.
	ErrCallIdentityMissing = errors.New("remote: the call carries no attribution identity")
	// ErrConnectorMissing reports a dispatcher constructed without a connector.
	ErrConnectorMissing = errors.New("remote: no connector is attached")
	// ErrCallTargetConflict reports one call identity naming two targets: a call
	// keeps the target it started on.
	ErrCallTargetConflict = errors.New("remote: the call identity is bound to another target")
)

// complete reports whether a target names both its destination and the agent it
// belongs to.
func (t Target) complete() bool {
	return t.AgentURL != "" && t.AgentID != ""
}

// Endpoints is the caller's declaration of the remote agents it may address:
// one explicit target per agent name. A name the declaration does not carry is
// refused, so no call resolves a destination of its own.
type Endpoints struct {
	declared map[string]Target
}

// NewEndpoints declares the remote agents one caller may address. Every entry
// names both an agent and its destination, and one agent name carries one
// destination.
func NewEndpoints(targets ...Target) (*Endpoints, error) {
	endpoints := &Endpoints{declared: make(map[string]Target, len(targets))}
	for _, target := range targets {
		if !target.complete() {
			return nil, ErrTargetMissing
		}
		if prior, ok := endpoints.declared[target.AgentID]; ok && prior != target {
			return nil, fmt.Errorf("remote: agent %q is declared with two destinations", target.AgentID)
		}
		endpoints.declared[target.AgentID] = target
	}
	return endpoints, nil
}

// Lookup returns the declared target of one agent name. A name the declaration
// does not carry is refused, so no call dials a destination of its own.
func (e *Endpoints) Lookup(agent string) (Target, error) {
	if e == nil {
		return Target{}, ErrTargetUndeclared
	}
	target, ok := e.declared[agent]
	if !ok {
		return Target{}, ErrTargetUndeclared
	}
	return target, nil
}

// Resolve returns the declared target the call names. A name the declaration
// does not carry, or one whose destination differs from the declaration, is
// refused by name.
func (e *Endpoints) Resolve(target Target) (Target, error) {
	declared, err := e.Lookup(target.AgentID)
	if err != nil || declared != target {
		return Target{}, ErrTargetUndeclared
	}
	return declared, nil
}

// RetryPolicy is the bounded retry shape of one remote call: how many attempts a
// call makes at most before its failure is reported.
type RetryPolicy struct {
	// Attempts is the number of attempts one call makes; a value below one is
	// read as one attempt.
	Attempts int
}

// NewRetryPolicy returns the retry shape of the given attempt count.
func NewRetryPolicy(attempts int) RetryPolicy {
	if attempts < 1 {
		attempts = 1
	}
	return RetryPolicy{Attempts: attempts}
}

// DefaultRetryPolicy is the retry shape of a dispatcher no policy was attached
// to: one attempt, so a call is never repeated behind the caller's back.
func DefaultRetryPolicy() RetryPolicy { return RetryPolicy{Attempts: 1} }

// CallRecord is the attribution of one remote call: the identity it runs under,
// the target it is bound to, and how many attempts it made. Two attempts under
// one identity share this record, so they are one call and never two.
type CallRecord struct {
	// Identity is the attribution identity of the call.
	Identity CallIdentity
	// Target is the target the call is bound to.
	Target Target
	// Attempts is the number of attempts the call made.
	Attempts int
	// Settled reports that the call left its outcome.
	Settled bool
}

// DispatcherOption configures a dispatcher at construction.
type DispatcherOption func(*A2ADispatcher)

// WithEndpoints attaches the caller's declaration of the agents it may address.
func WithEndpoints(endpoints *Endpoints) DispatcherOption {
	return func(d *A2ADispatcher) { d.endpoints = endpoints }
}

// WithRetryPolicy attaches the retry shape of the calls this dispatcher makes.
func WithRetryPolicy(policy RetryPolicy) DispatcherOption {
	return func(d *A2ADispatcher) { d.policy = NewRetryPolicy(policy.Attempts) }
}

// callState is the one outcome of one call identity, shared by every attempt
// and every repeat that names it.
type callState struct {
	record CallRecord
	target Target
	result *a2aprotocol.MessageResult
	err    error
	done   chan struct{}
}

// 契约: docs/wiki/tool/tool-architecture.md#十七、委派调用的取消归属与重试形状
//
// A2ADispatcher sends one explicitly addressed request under the caller's
// attribution identity and owns the retry shape of that call.
//
// Every attempt repeats the same identity and the same target, and a repeat of a
// call that already ran is answered with the outcome it left rather than sent a
// second time: one call identity carries one outcome.
type A2ADispatcher struct {
	connector Connector
	endpoints *Endpoints
	policy    RetryPolicy
	mu        sync.Mutex
	calls     map[CallIdentity]*callState
}

// NewA2ADispatcher returns the dispatcher over the given connector.
func NewA2ADispatcher(connector Connector, options ...DispatcherOption) (*A2ADispatcher, error) {
	if connector == nil {
		return nil, ErrConnectorMissing
	}
	dispatcher := &A2ADispatcher{
		connector: connector,
		policy:    DefaultRetryPolicy(),
		calls:     map[CallIdentity]*callState{},
	}
	for _, option := range options {
		option(dispatcher)
	}
	return dispatcher, nil
}

// Send delivers params to the explicit target under identity. The call is
// refused when it names no complete target, when the target is outside the
// caller's declaration, or when the identity carries no attribution; a repeat of
// an identity already bound to another target is refused as well.
func (d *A2ADispatcher) Send(ctx context.Context, target Target, identity CallIdentity, params a2aprotocol.SendMessageParams, opts ...a2aclient.RequestOption) (*a2aprotocol.MessageResult, error) {
	if !target.complete() {
		return nil, ErrTargetMissing
	}
	if identity.InvocationID == "" || identity.DecisionID == "" || identity.CallID == "" {
		return nil, ErrCallIdentityMissing
	}
	resolved, err := d.endpoints.Resolve(target)
	if err != nil {
		return nil, err
	}
	state, leader, err := d.claim(resolved, identity)
	if err != nil {
		return nil, err
	}
	if !leader {
		select {
		case <-state.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return state.result, state.err
	}
	result, sendErr := d.dispatch(ctx, resolved, identity, params, opts)
	d.mu.Lock()
	state.result, state.err = result, sendErr
	state.record.Settled = true
	d.mu.Unlock()
	close(state.done)
	return result, sendErr
}

// Call reports the attribution record of one call identity.
func (d *A2ADispatcher) Call(identity CallIdentity) (CallRecord, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	state, ok := d.calls[identity]
	if !ok {
		return CallRecord{}, false
	}
	return state.record, true
}

// claim reports the state one call identity runs under beside whether this
// caller drives it. An identity already bound to another target is refused.
func (d *A2ADispatcher) claim(target Target, identity CallIdentity) (*callState, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state, ok := d.calls[identity]; ok {
		if state.target != target {
			return nil, false, ErrCallTargetConflict
		}
		return state, false, nil
	}
	state := &callState{
		record: CallRecord{Identity: identity, Target: target},
		target: target,
		done:   make(chan struct{}),
	}
	d.calls[identity] = state
	return state, true, nil
}

// dispatch connects the target and makes the attempts one call is allowed,
// repeating the same identity and target on each of them.
func (d *A2ADispatcher) dispatch(ctx context.Context, target Target, identity CallIdentity, params a2aprotocol.SendMessageParams, opts []a2aclient.RequestOption) (*a2aprotocol.MessageResult, error) {
	client, err := d.connector.Connect(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("remote: connecting the call to %s: %w", target.AgentID, err)
	}
	var last error
	for attempt := 0; attempt < d.policy.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := client.SendMessage(ctx, params, opts...)
		d.recordAttempt(identity)
		if err == nil {
			return result, nil
		}
		last = err
	}
	return nil, fmt.Errorf("remote: the call to %s failed after %d attempts: %w", target.AgentID, d.policy.Attempts, last)
}

// recordAttempt counts one attempt on the record of the call identity.
func (d *A2ADispatcher) recordAttempt(identity CallIdentity) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if state, ok := d.calls[identity]; ok {
		state.record.Attempts++
	}
}

// 契约: docs/wiki/tool/tool-architecture.md#十七、委派调用的取消归属与重试形状
//
// NativeConnector opens the locked trpc-a2a-go client for one explicit target.
// The agent card, the transport and the message types stay with that client.
type NativeConnector struct{}

// Connect creates the client of one explicit target.
func (NativeConnector) Connect(ctx context.Context, target Target, opts ...a2aclient.Option) (*a2aclient.A2AClient, error) {
	if !target.complete() {
		return nil, ErrTargetMissing
	}
	return a2aclient.NewA2AClient(target.AgentURL, opts...)
}

var (
	_ Connector  = NativeConnector{}
	_ Dispatcher = (*A2ADispatcher)(nil)
)

// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// The sampling one sub-agent call names for the requests of its delegated turn:
// the temperature, thinking switch and reasoning effort the call declares flow
// onto every request that turn runs. The wrapper sits between the trpc agent
// builder and the model the assembly resolves, so a parameter declared at the
// call site reaches the provider even though the trpc model constructors accept
// no per-call sampling of their own.
//
// A parameter the call does not name is never written as a zero, and a value a
// request already carries is never overridden.
package subagent

import (
	"context"
	"strings"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// sampling is the sampling one sub-agent declaration names for its requests. A
// nil pointer declares no value and leaves the model default standing; a
// declared zero is the zero the call named.
type sampling struct {
	// temperature is the declared sampling temperature.
	temperature *float64
	// thinkingEnabled is the declared thinking switch.
	thinkingEnabled *bool
	// reasoningEffort is the declared reasoning effort; empty declares none.
	reasoningEffort string
}

// samplingOf reads the sampling one definition declares.
func samplingOf(d Definition) sampling {
	return sampling{
		temperature:     d.Temperature,
		thinkingEnabled: d.ThinkingEnabled,
		reasoningEffort: strings.TrimSpace(d.ReasoningEffort),
	}
}

// isZero reports whether the declaration names no sampling at all, so the model
// is built without a wrapper.
func (s sampling) isZero() bool {
	return s.temperature == nil && s.thinkingEnabled == nil && s.reasoningEffort == ""
}

// withSampling wraps one model with the sampling its sub-agent declaration
// names. A declaration naming none keeps the model as it stands, so an
// undeclared parameter adds no layer to the request path.
func withSampling(inner trpcmodel.Model, d Definition) trpcmodel.Model {
	params := samplingOf(d)
	if inner == nil || params.isZero() {
		return inner
	}
	return &sampledModel{inner: inner, params: params}
}

// sampledModel injects the declared sampling into the requests that name none
// and delegates the whole stream to the model it wraps.
type sampledModel struct {
	// inner is the model the delegated turn actually runs on.
	inner trpcmodel.Model
	// params is the sampling the call declared.
	params sampling
}

var (
	_ trpcmodel.Model     = (*sampledModel)(nil)
	_ trpcmodel.IterModel = (*sampledModel)(nil)
)

// Info reports the descriptor of the wrapped model, so a model reference still
// resolves by the provider's model name.
func (m *sampledModel) Info() trpcmodel.Info {
	return m.inner.Info()
}

// GenerateContent serves one request with the declared sampling filled in where
// the request names none.
func (m *sampledModel) GenerateContent(ctx context.Context, request *trpcmodel.Request) (<-chan *trpcmodel.Response, error) {
	return m.inner.GenerateContent(ctx, m.decorate(request))
}

// GenerateContentIter serves one request in the caller goroutine, keeping the
// iterator capability of the wrapped model rather than bridging it behind a
// channel.
func (m *sampledModel) GenerateContentIter(ctx context.Context, request *trpcmodel.Request) (trpcmodel.Seq[*trpcmodel.Response], error) {
	if iterator, ok := m.inner.(trpcmodel.IterModel); ok {
		return iterator.GenerateContentIter(ctx, m.decorate(request))
	}
	stream, err := m.GenerateContent(ctx, request)
	if err != nil || stream == nil {
		return nil, err
	}
	return func(yield func(*trpcmodel.Response) bool) {
		for response := range stream {
			if !yield(response) {
				return
			}
		}
	}, nil
}

// decorate returns a copy of the request with the declared sampling filled in
// where the request names none. The caller's request is never modified.
func (m *sampledModel) decorate(request *trpcmodel.Request) *trpcmodel.Request {
	if request == nil {
		return nil
	}
	out := *request
	if m.params.temperature != nil && out.Temperature == nil {
		value := *m.params.temperature
		out.Temperature = &value
	}
	if m.params.thinkingEnabled != nil && out.ThinkingEnabled == nil {
		value := *m.params.thinkingEnabled
		out.ThinkingEnabled = &value
	}
	if m.params.reasoningEffort != "" && out.ReasoningEffort == nil {
		value := m.params.reasoningEffort
		out.ReasoningEffort = &value
	}
	return &out
}

// cloneFloat64 copies one declared number; nil stays nil.
func cloneFloat64(in *float64) *float64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// cloneBool copies one declared switch; nil stays nil.
func cloneBool(in *bool) *bool {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

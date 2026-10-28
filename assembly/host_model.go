// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The model sampling of one declared agent: the temperature, thinking switch
// and reasoning effort a host document declares flow onto every request of that
// agent's model. The wrapper sits between the published binding and the model
// the provider registry built, so a parameter declared in the document reaches
// the provider even though the trpc model constructors accept no per-model
// sampling of their own.
//
// The wrapper fills a value only while the request itself names none: an
// undeclared parameter is never written as a zero, and a sampling a request
// already carries is never overridden.
package assembly

import (
	"context"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/model"
)

// hostModelParams is the sampling one agent declares for the requests of its
// model. A nil pointer declares no value and leaves the model default standing;
// a declared zero is the zero the document named.
type hostModelParams struct {
	Temperature     *float64
	ThinkingEnabled *bool
	ReasoningEffort string
}

// hostModelParamsOf reads the sampling one agent spec declares.
func hostModelParamsOf(spec config.AgentSpec) hostModelParams {
	return hostModelParams{
		Temperature:     spec.Temperature,
		ThinkingEnabled: spec.ThinkingEnabled,
		ReasoningEffort: strings.TrimSpace(spec.ReasoningEffort),
	}
}

// isZero reports whether the spec declares no sampling at all, so the model is
// installed without a wrapper.
func (p hostModelParams) isZero() bool {
	return p.Temperature == nil && p.ThinkingEnabled == nil && p.ReasoningEffort == ""
}

// withHostModelParams wraps one model with the sampling its agent spec declares.
// A spec that declares none keeps the model as it stands, so an undeclared
// parameter adds no layer to the request path.
func withHostModelParams(inner model.Model, spec config.AgentSpec) model.Model {
	params := hostModelParamsOf(spec)
	if inner == nil || params.isZero() {
		return inner
	}
	return &hostParameterizedModel{inner: inner, params: params}
}

// hostParameterizedModel injects the declared sampling into the requests that
// name none and delegates the whole stream to the model it wraps.
type hostParameterizedModel struct {
	inner  model.Model
	params hostModelParams
}

var (
	_ model.Model     = (*hostParameterizedModel)(nil)
	_ model.IterModel = (*hostParameterizedModel)(nil)
)

// Info reports the descriptor of the wrapped model, so a model reference still
// resolves by the provider's model name.
func (m *hostParameterizedModel) Info() model.Info {
	return m.inner.Info()
}

// GenerateContent serves one request with the declared sampling filled in where
// the request names none.
func (m *hostParameterizedModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	return m.inner.GenerateContent(ctx, m.decorate(request))
}

// GenerateContentIter serves one request in the caller goroutine, keeping the
// iterator capability of the wrapped model rather than bridging it behind a
// channel.
func (m *hostParameterizedModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error) {
	if iterator, ok := m.inner.(model.IterModel); ok {
		return iterator.GenerateContentIter(ctx, m.decorate(request))
	}
	stream, err := m.GenerateContent(ctx, request)
	if err != nil || stream == nil {
		return nil, err
	}
	return func(yield func(*model.Response) bool) {
		for response := range stream {
			if !yield(response) {
				return
			}
		}
	}, nil
}

// decorate returns a copy of the request with the declared sampling filled in
// where the request names none. The caller's request is never modified, so one
// request object reused across calls keeps the values it was built with.
func (m *hostParameterizedModel) decorate(request *model.Request) *model.Request {
	if request == nil {
		return nil
	}
	out := *request
	if m.params.Temperature != nil && out.Temperature == nil {
		value := *m.params.Temperature
		out.Temperature = &value
	}
	if m.params.ThinkingEnabled != nil && out.ThinkingEnabled == nil {
		value := *m.params.ThinkingEnabled
		out.ThinkingEnabled = &value
	}
	if m.params.ReasoningEffort != "" && out.ReasoningEffort == nil {
		value := m.params.ReasoningEffort
		out.ReasoningEffort = &value
	}
	return &out
}

// hostModelIdentitySeparator is the separator a rendered lease identity carries
// between the model name and the version: ModelIdentity.Name renders the two
// halves exactly so.
const hostModelIdentitySeparator = "@"

// hostModelVersion names one model reference as provider/model: the provider
// registry key the document resolves beside the model name it names. The
// version is what a model lease moves across, so the provider registry entry and
// the model name it serves are both part of the identity.
func hostModelVersion(provider, name string) string {
	provider, name = strings.TrimSpace(provider), strings.TrimSpace(name)
	if provider == "" || name == "" {
		return ""
	}
	return provider + "/" + name
}

// modelVersion reports the model version one declared agent runs against, and
// the empty version beside an agent the document resolves no model for.
func (d *hostDocument) modelVersion(id string) string {
	return hostModelVersion(d.modelRef(id))
}

// splitHostModelVersion splits one model version back onto its provider and
// model name. A provider key never carries a slash, while a model name may, so
// the first slash separates the two halves.
func splitHostModelVersion(version string) (string, string, error) {
	provider, name, ok := strings.Cut(strings.TrimSpace(version), "/")
	if !ok || provider == "" || name == "" {
		return "", "", fmt.Errorf("tagent: the model version %q names no provider/model", version)
	}
	return provider, name, nil
}

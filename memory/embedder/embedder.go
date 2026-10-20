// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
//
// Package embedder assembles the provider-backed embedding connection of the
// memory side from its declaration: the provider, the model, the environment
// variable holding the key, the vector width and the optional endpoint. The
// connection is validated at assembly time, so a host that runs is a host whose
// vectors belong to the vector space its store was built for.
package embedder

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/SpellingDragon/tagent/api/config"
	trpcembedder "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
)

// ErrEmbeddingDeclaration reports an embedding connection this package cannot
// assemble: an unknown provider, a connection missing its model, key variable
// or vector width, or an endpoint that is not an absolute http(s) URL.
var ErrEmbeddingDeclaration = errors.New("memory/embedder: the embedding declaration is invalid")

// ErrEmbeddingKey reports an embedding connection whose key variable names no
// value in the environment. The assembly refuses rather than send requests
// that are certain to be rejected, and names the variable that holds nothing.
var ErrEmbeddingKey = errors.New("memory/embedder: the embedding key is not set")

// ErrEmbeddingUnwired reports an embedder with no provider connection behind
// it: the zero value carries no vector source and answers no request.
var ErrEmbeddingUnwired = errors.New("memory/embedder: the embedding connection is not wired")

// endpointDefaults maps one provider to the endpoint its calls go to when the
// declaration names none. An empty default means the provider SDK's own
// endpoint, which is the OpenAI default.
var endpointDefaults = map[string]string{
	"openai": "",
	"zhipu":  "https://open.bigmodel.cn/api/paas/v4",
}

// Validate reports whether one embedding declaration names everything its
// provider needs: a known provider, a model, the variable holding the key, a
// positive vector width, and an endpoint that is absolute when the declaration
// overrides the provider default.
func Validate(spec config.EmbeddingSpec) error {
	provider := strings.TrimSpace(spec.Provider)
	if provider == "" {
		return fmt.Errorf("%w: the embedding names no provider", ErrEmbeddingDeclaration)
	}
	if _, known := endpointDefaults[provider]; !known {
		return fmt.Errorf("%w: embedding provider %q is not a known provider", ErrEmbeddingDeclaration, provider)
	}
	if strings.TrimSpace(spec.Model) == "" {
		return fmt.Errorf("%w: the embedding names no model", ErrEmbeddingDeclaration)
	}
	if strings.TrimSpace(spec.APIKeyEnv) == "" {
		return fmt.Errorf("%w: the embedding names no key variable", ErrEmbeddingDeclaration)
	}
	if spec.Dimensions <= 0 {
		return fmt.Errorf("%w: the embedding declares no vector width", ErrEmbeddingDeclaration)
	}
	endpoint := strings.TrimSpace(spec.APIEndpoint)
	if endpoint == "" {
		return nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: embedding endpoint %q is not an absolute http(s) URL", ErrEmbeddingDeclaration, endpoint)
	}
	return nil
}

// vectorSource is the provider call the adapter converts from: the embedder
// the provider SDK constructs, or a test double behind the same shape.
type vectorSource interface {
	GetEmbedding(ctx context.Context, text string) ([]float64, error)
}

// Embedder adapts a provider embedder to the memory seam: it resolves the key
// variable once at assembly, converts the provider's float64 vector into the
// float32 the seam stores, and refuses an empty vector or one whose width is
// not the declared width by name rather than storing it into a vector space it
// does not belong to.
type Embedder struct {
	provider   string
	model      string
	endpoint   string
	dimensions int
	source     vectorSource
}

// New assembles the embedding seam of one declared connection. The key is read
// at assembly and travels in the client, so a missing variable refuses the
// assembly by name before any request is prepared.
func New(spec config.EmbeddingSpec) (*Embedder, error) {
	if err := Validate(spec); err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(spec.Provider)
	model := strings.TrimSpace(spec.Model)
	key, err := embeddingKey(spec.APIKeyEnv)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimSpace(spec.APIEndpoint)
	if endpoint == "" {
		endpoint = endpointDefaults[provider]
	}
	opts := []trpcembedder.Option{
		trpcembedder.WithModel(model),
		trpcembedder.WithDimensions(spec.Dimensions),
		trpcembedder.WithAPIKey(key),
	}
	if endpoint != "" {
		opts = append(opts, trpcembedder.WithBaseURL(endpoint))
	}
	return &Embedder{
		provider:   provider,
		model:      model,
		endpoint:   endpoint,
		dimensions: spec.Dimensions,
		source:     trpcembedder.New(opts...),
	}, nil
}

// Connection reports the declared connection this embedder was assembled from,
// with the provider default endpoint filled in when the declaration named none.
func (e *Embedder) Connection() config.EmbeddingSpec {
	if e == nil {
		return config.EmbeddingSpec{}
	}
	return config.EmbeddingSpec{
		Provider:    e.provider,
		Model:       e.model,
		APIKeyEnv:   "",
		Dimensions:  e.dimensions,
		APIEndpoint: e.endpoint,
	}
}

// Embed returns the vector of text under the declared connection, honouring
// cancellation and answering a named failure when the provider fails, returns
// no vector, or returns a vector of another width.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if e == nil || e.source == nil {
		return nil, ErrEmbeddingUnwired
	}
	vector, err := e.source.GetEmbedding(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("memory/embedder: embed with %s/%s: %w", e.provider, e.model, err)
	}
	if len(vector) == 0 {
		return nil, fmt.Errorf("memory/embedder: embed with %s/%s: the provider returned no vector", e.provider, e.model)
	}
	if len(vector) != e.dimensions {
		return nil, fmt.Errorf("memory/embedder: embed with %s/%s: the provider returned %d dimensions, the connection declares %d",
			e.provider, e.model, len(vector), e.dimensions)
	}
	embedded := make([]float32, len(vector))
	for i, value := range vector {
		embedded[i] = float32(value)
	}
	return embedded, nil
}

// embeddingKey resolves the environment variable one connection names. An
// unset and an empty variable answer the same named refusal: an embedder
// holding an empty key would fail on its first request, not at assembly.
func embeddingKey(name string) (string, error) {
	variable := strings.TrimSpace(name)
	key := strings.TrimSpace(os.Getenv(variable))
	if key == "" {
		return "", fmt.Errorf("%w: %s is empty or unset", ErrEmbeddingKey, variable)
	}
	return key, nil
}

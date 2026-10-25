// embedder_test 钉住 语义嵌入装配的红线：声明面的 provider/model/宽度/端点就是装配出的连接，
// 缺 key 与半声明在装配期具名拒绝，向量宽度不符或空向量不被当作可存储的向量。
// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
package embedder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
)

// TestValidateRefusesHalfNamedConnections pins each named refusal of an incomplete declaration.
func TestValidateRefusesHalfNamedConnections(t *testing.T) {
	complete := config.EmbeddingSpec{
		Provider:   "zhipu",
		Model:      "embedding-3",
		APIKeyEnv:  "ZAI_API_KEY",
		Dimensions: 512,
	}
	if err := Validate(complete); err != nil {
		t.Fatalf("validate the complete connection: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*config.EmbeddingSpec)
	}{
		{name: "no provider", mutate: func(s *config.EmbeddingSpec) { s.Provider = " " }},
		{name: "unknown provider", mutate: func(s *config.EmbeddingSpec) { s.Provider = "other" }},
		{name: "no model", mutate: func(s *config.EmbeddingSpec) { s.Model = "" }},
		{name: "no key variable", mutate: func(s *config.EmbeddingSpec) { s.APIKeyEnv = "" }},
		{name: "no vector width", mutate: func(s *config.EmbeddingSpec) { s.Dimensions = 0 }},
		{name: "relative endpoint", mutate: func(s *config.EmbeddingSpec) { s.APIEndpoint = "/v1" }},
		{name: "non-http endpoint", mutate: func(s *config.EmbeddingSpec) { s.APIEndpoint = "ftp://example/v1" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := complete
			tc.mutate(&spec)
			if err := Validate(spec); !errors.Is(err, ErrEmbeddingDeclaration) {
				t.Fatalf("validate %+v: error %v, want ErrEmbeddingDeclaration", spec, err)
			}
		})
	}
}

// TestNewResolvesTheProviderDefaultEndpoint pins the connection a declaration assembles.
func TestNewResolvesTheProviderDefaultEndpoint(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "test-key")
	embedded, err := New(config.EmbeddingSpec{
		Provider:   "zhipu",
		Model:      "embedding-3",
		APIKeyEnv:  "ZAI_API_KEY",
		Dimensions: 512,
	})
	if err != nil {
		t.Fatalf("assemble the declared connection: %v", err)
	}
	connection := embedded.Connection()
	if connection.Provider != "zhipu" || connection.Model != "embedding-3" || connection.Dimensions != 512 {
		t.Fatalf("connection = %+v, want the declared zhipu embedding-3 binding", connection)
	}
	if want := "https://open.bigmodel.cn/api/paas/v4"; connection.APIEndpoint != want {
		t.Fatalf("endpoint = %q, want the zhipu default %q", connection.APIEndpoint, want)
	}
}

// TestNewRefusesWithoutTheKey pins the named refusal of an empty key variable.
func TestNewRefusesWithoutTheKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	_, err := New(config.EmbeddingSpec{
		Provider:   "zhipu",
		Model:      "embedding-3",
		APIKeyEnv:  "ZAI_API_KEY",
		Dimensions: 512,
	})
	if !errors.Is(err, ErrEmbeddingKey) {
		t.Fatalf("assemble beside an empty key: error %v, want ErrEmbeddingKey", err)
	}
	if !strings.Contains(err.Error(), "ZAI_API_KEY") {
		t.Fatalf("the refusal %q does not name the empty variable", err)
	}
}

// TestEmbedConvertsAndRefusesUnusableVectors pins the seam conversion and its refusals.
func TestEmbedConvertsAndRefusesUnusableVectors(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		vector []float64
		err    error
		want   []float32
		refuse bool
	}{
		{name: "converts", vector: []float64{0.5, -1}, want: []float32{0.5, -1}},
		{name: "empty", vector: nil, refuse: true},
		{name: "wrong width", vector: []float64{1, 2, 3}, refuse: true},
		{name: "provider failure", err: errors.New("provider down"), refuse: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			embedded := &Embedder{provider: "zhipu", model: "embedding-3", dimensions: 2,
				source: vectorSourceFunc(func(context.Context, string) ([]float64, error) {
					return tc.vector, tc.err
				})}
			got, err := embedded.Embed(ctx, "text")
			if tc.refuse {
				if err == nil {
					t.Fatalf("embed returned %v, want a named refusal", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("embed: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("vector = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("vector = %v, want %v", got, tc.want)
				}
			}
		})
	}

	var zero *Embedder
	if _, err := zero.Embed(ctx, "text"); !errors.Is(err, ErrEmbeddingUnwired) {
		t.Fatalf("embed beside no connection: error %v, want ErrEmbeddingUnwired", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := zeroEmbedded().Embed(cancelled, "text"); !errors.Is(err, context.Canceled) {
		t.Fatalf("embed beside a cancelled context: error %v, want context.Canceled", err)
	}
}

// vectorSourceFunc adapts one function to the provider call seam.
type vectorSourceFunc func(context.Context, string) ([]float64, error)

// GetEmbedding answers the vector the function returns.
func (f vectorSourceFunc) GetEmbedding(ctx context.Context, text string) ([]float64, error) {
	return f(ctx, text)
}

// zeroEmbedded returns an embedder carrying a source, so the cancellation
// check is the context's answer rather than the unwired refusal.
func zeroEmbedded() *Embedder {
	return &Embedder{dimensions: 1, source: vectorSourceFunc(func(context.Context, string) ([]float64, error) {
		return []float64{1}, nil
	})}
}

// TestEmbedRefusesEmptyTextByName 验证空文本在请求前被具名拒绝。
func TestEmbedRefusesEmptyTextByName(t *testing.T) {
	e := &Embedder{}
	if _, err := e.Embed(context.Background(), ""); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("embed of empty text: err = %v, want ErrEmptyText", err)
	}
}

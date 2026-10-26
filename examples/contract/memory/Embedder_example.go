package memoryexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/memory"
)

// errEmptyText is the named failure an embedder reports instead of a zero vector.
var errEmptyText = errors.New("memoryexample: embedder was handed empty text")

// sizedEmbedder is the canonical embedding seam: it generates the vector of one text and
// reports a named failure rather than a zero vector.
type sizedEmbedder struct{}

// Embed returns the embedding vector of text, or a named failure.
func (sizedEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if text == "" {
		return nil, errEmptyText
	}
	return []float32{float32(len(text)), 1}, nil
}

var _ memory.Embedder = sizedEmbedder{}

// ExampleEmbedder embeds one text under the binding that borrowed the seam and reports
// that empty text is refused by name instead of answered with a zero vector.
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
func ExampleEmbedder() {
	ctx := context.Background()
	var embedder memory.Embedder = sizedEmbedder{}

	vector, err := embedder.Embed(ctx, "hello")
	if err != nil || len(vector) == 0 {
		return
	}
	if _, err := embedder.Embed(ctx, ""); !errors.Is(err, errEmptyText) {
		return
	}
	_ = vector[0]
}

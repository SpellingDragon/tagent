// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
//
// Skeleton compile-time assertions for the provider-backed embedder: the
// assembly entry, the declaration check, the memory seam method and the named
// refusals keep exactly these shapes.
package embedder

import (
	"context"

	"github.com/SpellingDragon/tagent/api/config"
)

var (
	_ func(config.EmbeddingSpec) error              = Validate
	_ func(config.EmbeddingSpec) (*Embedder, error) = New

	_ func(*Embedder, context.Context, string) ([]float32, error) = (*Embedder).Embed
	_ func(*Embedder) config.EmbeddingSpec                        = (*Embedder).Connection

	_ error = ErrEmbeddingDeclaration
	_ error = ErrEmbeddingKey
	_ error = ErrEmbeddingUnwired
)

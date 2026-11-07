// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
//
// Embedder generates the embedding of one query or one candidate's original
// text on demand. No runtime vector table is retained between calls.
package memory

import "context"

// Embedder generates one text vector under the deployment's binding.
//
// The redline list (2 rules):
// MUST return a named failure rather than pretend an empty vector is an embedding.
// MUST NOT retain a runtime vector table or precompute on startup or commit.
type Embedder interface {
	// Embed returns the embedding vector of text, or a named failure.
	Embed(ctx context.Context, text string) ([]float32, error)
}

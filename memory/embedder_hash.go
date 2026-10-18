// 契约: docs/wiki/memory/memory-architecture.md#embedder-contract
//
// The deterministic embedding stand-in of the memory side: it maps text to a
// fixed-dimension unit vector with no endpoint behind it, so the vector leg of
// a retrieval runs to completion offline and a runtime without an embedding
// configuration still exercises the whole retrieval mechanism end to end. The
// vectors it carries are content fingerprints and not semantic measurements:
// identical text always yields identical vectors and text sharing more
// character n-grams scores a higher cosine. That is the mechanism guarantee a
// retrieval test stands on, and no claim of semantic recall quality.
package memory

import (
	"context"
	"hash/fnv"
	"math"
)

// DefaultHashDimensions is the vector width a HashEmbedder runs at when it
// names none.
const DefaultHashDimensions = 128

// HashEmbedder is the Embedder that hashes character n-grams into a
// fixed-dimension unit vector. The mapping is deterministic and honours
// cancellation, and the zero value is usable and runs at DefaultHashDimensions.
type HashEmbedder struct {
	dimensions int
}

// NewHashEmbedder returns a hash embedder of the given vector width, or one at
// DefaultHashDimensions when the width names none.
func NewHashEmbedder(dimensions int) *HashEmbedder {
	if dimensions <= 0 {
		dimensions = DefaultHashDimensions
	}
	return &HashEmbedder{dimensions: dimensions}
}

var _ Embedder = (*HashEmbedder)(nil)

// Embed returns the deterministic embedding vector of text: a unit vector over
// the character n-gram fingerprint of the text, identical for identical text.
func (e *HashEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dimensions := e.dimensions
	if dimensions <= 0 {
		dimensions = DefaultHashDimensions
	}
	vector := make([]float32, dimensions)
	runes := []rune(text)
	for _, width := range []int{2, 3} {
		for start := 0; start+width <= len(runes); start++ {
			hash := fnv.New32a()
			_, _ = hash.Write([]byte(string(runes[start : start+width])))
			digest := hash.Sum32()
			sign := float32(1)
			if digest&0x80000000 != 0 {
				sign = -1
			}
			vector[digest%uint32(dimensions)] += sign
		}
	}
	norm := float64(0)
	for _, component := range vector {
		norm += float64(component) * float64(component)
	}
	if norm == 0 {
		vector[0] = 1
		return vector, nil
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range vector {
		vector[i] *= scale
	}
	return vector, nil
}

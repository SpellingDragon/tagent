package embedder

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/SpellingDragon/tagent/memory"
)

// MockEmbedder 用文本哈希把内容映射到固定维度的确定性伪向量，实现 memory.Embedder。
// 相同文本必得相同向量，共享词元越多余弦越高；仅用于验证机制（融合、过滤、降级），
// 不承诺真实语义质量——以它通过的测试不能推断线上召回效果。零值实例仍可用。
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder
type MockEmbedder struct {
	dim int
}

var _ memory.Embedder = (*MockEmbedder)(nil)

// NewMockEmbedder 创建确定性 mock 嵌入器（dim<=0 时取 64）。
func NewMockEmbedder(dim int) *MockEmbedder {
	if dim <= 0 {
		dim = 64
	}
	return &MockEmbedder{dim: dim}
}

// Dimension 返回构造时确定的维度。
func (m *MockEmbedder) Dimension() int { return m.dim }

// ModelID 返回含维度的标识，使换维度后的旧向量在重建时被跳过。
func (m *MockEmbedder) ModelID() string {
	return fmt.Sprintf("mock-embed-%d", m.dim)
}

// Embed 对每条文本做词元哈希袋（bag-of-token-hashes）→ L2 归一化向量。
// 共享词元产生重叠维度，故余弦相似度随词元重叠单调——足以驱动 RRF 机制测试。
func (m *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = m.embedOne(t)
	}
	return out, nil
}

func (m *MockEmbedder) embedOne(text string) []float32 {
	dim := m.dim
	if dim <= 0 {
		dim = 64
	}
	vec := make([]float32, dim)
	start := 0
	for i := 0; i <= len(text); i++ {
		if i == len(text) || isDelim(text[i]) {
			if i > start {
				tok := text[start:i]
				h := fnv.New32a()
				_, _ = h.Write([]byte(tok))
				sum := h.Sum32()
				vec[sum%uint32(dim)] += 1.0
				vec[(sum>>7)%uint32(dim)] += 0.5
			}
			start = i + 1
		}
	}
	l2normalize(vec)
	return vec
}

// isDelim 判断字节是否为词元分隔符：ASCII 字母数字与 UTF-8 多字节序列（中文等）
// 都算词元内容，按字节聚合而不切开。
func isDelim(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c >= 0x80:
		return false
	default:
		return true
	}
}

// l2normalize 原地 L2 归一化（零向量保持不变，避免除零）。
func l2normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
}

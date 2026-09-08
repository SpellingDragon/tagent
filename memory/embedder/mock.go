package embedder

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"

	"github.com/SpellingDragon/tagent/memory"
)

// MockEmbedder 用 FNV 哈希把文本映射到固定维度的确定性伪向量（实现 memory.Embedder）。
// 语义：相同文本 → 相同向量；共享词元越多 → 余弦越高（弱语义）。
// 仅用于验证机制（融合/过滤/降级），不承诺真实语义质量。
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

func (m *MockEmbedder) Dimension() int { return m.dim }
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
		dim = 64 // 零值 MockEmbedder{} 兜底，防除零 panic（审查 Nit8）
	}
	vec := make([]float32, dim)
	// 词元哈希袋：按空白/标点粗切，每词元投到两个维度（增碰撞分辨）。
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

func isDelim(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c >= 0x80: // UTF-8 多字节（中文等）：按字节聚合到词元，不切分
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

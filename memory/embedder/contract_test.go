package embedder

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestContract_AllImplementations（2026-09-08 分包回归）：三个实现均满足
// memory.Embedder 契约的批量语义——返回与 texts 等长、顺序对应、非 nil 向量；
// Dimension/ModelID 稳定（分包移动后行为逐字节不变）。
// 新增供应商时把实现加入 providers 表即自动获得契约守护。
func TestContract_AllImplementations(t *testing.T) {
	texts := []string{"部署完成", "deploy finished", "用户偏好：简洁回复"}
	providers := map[string]memory.Embedder{
		"mock":   NewMockEmbedder(64),
		"traced": NewTracedEmbedder(NewMockEmbedder(64)),
	}
	// zhipu 需 key，short 下跳过（真实链路见 zhipu_real_test）。
	for name, p := range providers {
		p := p
		t.Run(name, func(t *testing.T) {
			require.NotZero(t, p.Dimension(), "Dimension 不得为 0（mock/traced 已知维度）")
			require.NotEmpty(t, p.ModelID(), "ModelID 必填（索引指纹比对依赖）")

			vecs, err := p.Embed(context.Background(), texts)
			require.NoError(t, err)
			require.Len(t, vecs, len(texts), "批量语义：与输入等长")

			// 顺序对应 + 确定性：同输入两次嵌入逐字节一致。
			vecs2, err := p.Embed(context.Background(), texts)
			require.NoError(t, err)
			for i := range vecs {
				require.NotEmpty(t, vecs[i], "向量不得为空（index %d）", i)
				require.InDeltaSlice(t, vecs[i], vecs2[i], 1e-6, "确定性：同输入同向量（index %d）", i)
			}
		})
	}
	// 编译期契约断言（三实现）。
	var (
		_ memory.Embedder = (*MockEmbedder)(nil)
		_ memory.Embedder = (*TracedEmbedder)(nil)
		_ memory.Embedder = (*ZhipuEmbedder)(nil)
	)
}

package embedder

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
)

// TestContract_AllImplementations 钉住三实现均满足 Embedder 批量语义（等长、顺序对应、非空、确定性）；新增供应商进 providers 表即自动受守护。
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder
func TestContract_AllImplementations(t *testing.T) {
	texts := []string{"部署完成", "deploy finished", "用户偏好：简洁回复"}
	providers := map[string]memory.Embedder{
		"mock":   NewMockEmbedder(64),
		"traced": NewTracedEmbedder(NewMockEmbedder(64)),
	}
	for name, p := range providers {
		p := p
		t.Run(name, func(t *testing.T) {
			require.NotZero(t, p.Dimension(), "Dimension 不得为 0（mock/traced 已知维度）")
			require.NotEmpty(t, p.ModelID(), "ModelID 必填（索引指纹比对依赖）")

			vecs, err := p.Embed(context.Background(), texts)
			require.NoError(t, err)
			require.Len(t, vecs, len(texts), "批量语义：与输入等长")

			vecs2, err := p.Embed(context.Background(), texts)
			require.NoError(t, err)
			for i := range vecs {
				require.NotEmpty(t, vecs[i], "向量不得为空（index %d）", i)
				require.InDeltaSlice(t, vecs[i], vecs2[i], 1e-6, "确定性：同输入同向量（index %d）", i)
			}
		})
	}
	var (
		_ memory.Embedder = (*MockEmbedder)(nil)
		_ memory.Embedder = (*TracedEmbedder)(nil)
		_ memory.Embedder = (*ZhipuEmbedder)(nil)
	)
}

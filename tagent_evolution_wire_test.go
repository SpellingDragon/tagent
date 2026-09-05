package tagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// fakeModel 是满足 model.Model 的最小替身（New 构建期不调用 GenerateContent，仅运行期）。
type fakeModel struct{}

func (fakeModel) GenerateContent(context.Context, *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response)
	close(ch)
	return ch, nil
}

func (fakeModel) Info() model.Info { return model.Info{} }

// minimalConfig 构造最小单 agent 配置（inline 提示词 + 无工具 + 内存 store），避开
// DefaultConfig 的多 agent 依赖（knowledge 需 SkillRepo、recall 需分区等）。
func minimalConfig(evoDir string, evoEnabled bool) Config {
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				SystemPrompt: PromptConfig{Inline: "你是 tagent 测试代理"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	cfg.ApplyDefaults()
	cfg.Evolution.Enabled = evoEnabled
	cfg.Evolution.Dir = evoDir
	return cfg
}

// TestNew_EvolutionEnabled_WiresBaseline 验证 evolution 接线：启用时 New 成功构建，且 entry
// agent 的静态系统提示词被 InitBaseline 落入 bundle 存储（证明 VersionedSource/refine 接线执行）。
func TestNew_EvolutionEnabled_WiresBaseline(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	evoDir := filepath.Join(t.TempDir(), "evolution")

	a, err := New(minimalConfig(evoDir, true), WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)

	// InitBaseline 应已在 evoDir 写入 bundle（bundles/ 子目录 + active 指针）——
	// 证明 entry agent 的 evolution 接线路径确实执行（非空转）。
	entries, err := os.ReadDir(evoDir)
	require.NoError(t, err, "evolution 目录应被创建")
	require.NotEmpty(t, entries, "InitBaseline 应写入 bundle 文件")

	bundles, err := os.ReadDir(filepath.Join(evoDir, "bundles"))
	require.NoError(t, err, "bundles 子目录应存在")
	require.NotEmpty(t, bundles, "应有基线 bundle 落盘")
}

// TestNew_EvolutionDisabled_NoSideEffect 验证配置门控：默认关闭时 New 成功且不创建 evolution
// 目录（现状零行为变化）。
func TestNew_EvolutionDisabled_NoSideEffect(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	evoDir := filepath.Join(t.TempDir(), "evolution")

	cfg := minimalConfig(evoDir, false)
	require.False(t, cfg.Evolution.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)

	// 关闭时不应创建 evolution 目录（接线未激活）。
	_, statErr := os.Stat(evoDir)
	require.True(t, os.IsNotExist(statErr), "evolution 关闭时不应创建 bundle 目录")
}

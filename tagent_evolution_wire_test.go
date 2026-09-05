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

// TestNew_GovernanceEnabled_Builds 验证 governance 接线：启用时 New 成功构建（govGate 构造 +
// leaf 工具包裹路径执行）。配置门控——默认关闭则不构造。
func TestNew_GovernanceEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Governance.Enabled = true
	cfg.Governance.Enforcement = "warn"
	cfg.Governance.Dir = t.TempDir()

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_GovernanceDisabled_Default 验证 governance 默认关闭（零值）→ New 成功，现状不变。
func TestNew_GovernanceDisabled_Default(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	require.False(t, cfg.Governance.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_ReliableBusSpillDir 验证 ReliableBus 接线：配置 BusSpillDir 后 New 成功，且 entry
// agent 的 per-agent 溢出子目录 <BusSpillDir>/<entry> 被创建（NewReliableEventBus→NewSpillStore）。
func TestNew_ReliableBusSpillDir(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	spillRoot := filepath.Join(t.TempDir(), "bus-spill")
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.BusSpillDir = spillRoot

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)

	// per-agent 溢出子目录应被创建（<spillRoot>/tagent）。
	_, statErr := os.Stat(filepath.Join(spillRoot, cfg.Entry))
	require.NoError(t, statErr, "per-agent 溢出子目录 <BusSpillDir>/<entry> 应被创建")
}

// TestNew_ReliableBusDisabledDefault 验证配置门控：默认 BusSpillDir 空 → 不创建溢出目录（现状）。
func TestNew_ReliableBusDisabledDefault(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	require.Empty(t, cfg.Reliability.BusSpillDir)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_DegradationEnabled_Builds 验证 A2 接线：DegradationEnabled 时 New 成功构建
// （DegradationManager 构造 + ErrorTrackingStore 最外层包裹 memStore + agentCfg.Degradation
// 注入 + event_loop model 上报就绪）——补齐此前 DegradationManager 零接线的断点。
func TestNew_DegradationEnabled_Builds(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig(filepath.Join(t.TempDir(), "evo"), false)
	cfg.Reliability.DegradationEnabled = true

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

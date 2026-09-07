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
func minimalConfig(_ string, evoEnabled bool) Config {
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
	return cfg
}

// TestNew_EvolutionEnabled_GitNativeSmoke 验证 git-native evolution 接线：启用时 New 成功
// 构造 GitEvolution 装配单元（refine 工具注册/章 provider/Stop closer 在 buildAgent 路径；
// git 仓自检 Warn 不阻断）。深度行为在 evolution 包 tempdir git 仓测试覆盖（2.4/3.3）。
func TestNew_EvolutionEnabled_GitNativeSmoke(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	a, err := New(minimalConfig("", true), WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
}

// TestNew_EvolutionDisabled_NoSideEffect 验证配置门控：默认关闭时 New 成功（现状零行为变化）。
func TestNew_EvolutionDisabled_NoSideEffect(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())
	cfg := minimalConfig("", false)
	require.False(t, cfg.Evolution.Enabled)

	a, err := New(cfg, WithModel(fakeModel{}))
	require.NoError(t, err)
	require.NotNil(t, a)
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

// TestResolveMemoryStore_FileSamePathShared 是 M-1（四审）回归：type: file 同 path 必须返回
// 同一实例（与 memory/localfile 同构）——否则跨 agent read_namespaces 下 InMemRelationStore
// 内存图分歧（recall 因果链断链）+ 双 Compactor 基于独立视图并发覆盖同一 KV 键。
func TestResolveMemoryStore_FileSamePathShared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-mem")
	s1, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	s2, err := resolveMemoryStore(MemoryConfig{Type: "file", Path: path})
	require.NoError(t, err)
	require.Same(t, s1, s2, "file 后端同 path 必须共享同一实例（M-1：防因果链断链/双 Compactor 并发覆盖）")
}

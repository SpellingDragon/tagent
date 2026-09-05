package tagent

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ============================================================================
// Config Validation tests
// ============================================================================

func TestValidate_AgentReferences(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		wantErr     bool
		errContains string
	}{
		{
			name: "valid agent tool reference",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "knowledge", DescriptionFile: "desc.md"},
						},
					},
					"knowledge": {},
				},
			},
			wantErr: false,
		},
		{
			name: "unknown agent reference",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "nonexistent", DescriptionFile: "desc.md"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "unknown agent",
		},
		{
			name: "plain tool with valid id",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "exec"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "duplicate tool id",
			cfg: Config{
				Entry: "tagent",
				Agents: map[string]AgentConfig{
					"tagent": {
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "exec"},
							{Kind: ToolKindTool, ID: "exec"},
						},
					},
				},
			},
			wantErr:     true,
			errContains: "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.ApplyDefaults()
			err := tt.cfg.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidate_ArchitectureHierarchy(t *testing.T) {
	cfg := &Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {
				Tools: []ToolRef{
					{Kind: ToolKindAgent, AgentID: "action", DescriptionFile: "action_tool_desc.md"},
				},
			},
			"action": {
				Tools: []ToolRef{
					{Kind: ToolKindTool, ID: "read_file"},
					{Kind: ToolKindTool, ID: "save_file"},
					{Kind: ToolKindTool, ID: "list_file"},
					{Kind: ToolKindTool, ID: "search_file"},
					{Kind: ToolKindTool, ID: "search_content"},
					{Kind: ToolKindTool, ID: "read_multiple_files"},
					{Kind: ToolKindTool, ID: "replace_content"},
					{Kind: ToolKindTool, ID: "exec", DescriptionFile: "exec_tool_desc.md"},
				},
			},
		},
	}
	cfg.ApplyDefaults()
	err := cfg.Validate()
	require.NoError(t, err, "valid architecture should not produce error")
}

// ============================================================================
// DefaultConfig tests
// ============================================================================

func TestDefaultConfig_KnowledgeTools(t *testing.T) {
	cfg := DefaultConfig()

	knowledgeAgent, ok := cfg.Agents["knowledge"]
	require.True(t, ok, "knowledge agent should exist in DefaultConfig")

	expectedTools := []string{
		"skill_search", "skill_load", "mcp_discover",
		"web_search", "duckduckgo_search", "memory_query",
	}

	require.Len(t, knowledgeAgent.Tools, len(expectedTools),
		"knowledge agent should have %d tool refs", len(expectedTools))

	for i, expectedID := range expectedTools {
		tr := knowledgeAgent.Tools[i]
		assert.Equal(t, ToolKindTool, tr.Kind, "tool[%d] should be kind=tool", i)
		assert.Equal(t, expectedID, tr.ID, "tool[%d] should have id=%q", i, expectedID)
	}
}

func TestDefaultConfig_RecallTools(t *testing.T) {
	cfg := DefaultConfig()

	recallAgent, ok := cfg.Agents["recall"]
	require.True(t, ok, "recall agent should exist in DefaultConfig")

	expectedTools := []string{
		"recall_query", "recall_get", "recall_recent", "recall_trace",
	}

	require.Len(t, recallAgent.Tools, len(expectedTools),
		"recall agent should have %d tool refs", len(expectedTools))

	for i, expectedID := range expectedTools {
		tr := recallAgent.Tools[i]
		assert.Equal(t, ToolKindTool, tr.Kind, "tool[%d] should be kind=tool", i)
		assert.Equal(t, expectedID, tr.ID, "tool[%d] should have id=%q", i, expectedID)
	}
}

func TestDefaultConfig_TagentTools(t *testing.T) {
	cfg := DefaultConfig()

	tagentAgent, ok := cfg.Agents["tagent"]
	require.True(t, ok, "tagent agent should exist in DefaultConfig")

	// tagent should have 3 tools: knowledge (agent), recall (agent), exec (tool)
	require.Len(t, tagentAgent.Tools, 3, "tagent should have 3 tools")

	assert.Equal(t, ToolKindAgent, tagentAgent.Tools[0].Kind)
	assert.Equal(t, "knowledge", tagentAgent.Tools[0].AgentID)

	assert.Equal(t, ToolKindAgent, tagentAgent.Tools[1].Kind)
	assert.Equal(t, "recall", tagentAgent.Tools[1].AgentID)

	assert.Equal(t, ToolKindTool, tagentAgent.Tools[2].Kind)
	assert.Equal(t, "exec", tagentAgent.Tools[2].ID)
}

func TestDefaultConfig_MeditationConfig(t *testing.T) {
	cfg := DefaultConfig()

	tagentAgent := cfg.Agents["tagent"]
	assert.False(t, tagentAgent.Meditation.Enabled,
		"DefaultConfig should not enable meditation by default")
}

// TestDefaultConfigBuildable 永久看住「配置-注册表漂移」类 BUG（报告 D4 §4.5.3）：
// DefaultConfig 必须通过 ApplyDefaults + Validate + ValidateToolAccess 全链路，
// 即 New(DefaultConfig()) 可构建。此前 DefaultConfig 引用 id:"action" 而注册表
// 注册为 "exec"（registry.go），ValidateToolAccess 会失败——本测试锁死该回归。
func TestDefaultConfigBuildable(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	cfg := DefaultConfig()
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig Validate 失败: %v", err)
	}
	if err := GetRegistry().ValidateToolAccess(&cfg); err != nil {
		t.Fatalf("DefaultConfig 工具引用与注册表漂移（id:\"action\" vs \"exec\" 类 BUG）: %v", err)
	}
}

// ============================================================================
// ToolRegistry tests
// ============================================================================

func TestToolRegistry_RegisterAndQuery(t *testing.T) {
	registry := GetRegistry()
	require.NotNil(t, registry)

	err := RegisterBuiltinTools()
	require.NoError(t, err)

	// Verify all registered plain tools
	plainTools := []string{
		"exec",
		"read_file", "save_file", "list_file", "search_file",
		"search_content", "read_multiple_files", "replace_content",
		"skill_search", "skill_load", "mcp_discover",
		"web_search", "duckduckgo_search", "memory_query",
		"recall_query", "recall_get", "recall_recent", "recall_trace",
	}

	for _, id := range plainTools {
		factory, ok := registry.GetPlainToolFactory(id)
		assert.True(t, ok, "plain tool %q should be registered", id)
		assert.NotNil(t, factory, "factory for %q should not be nil", id)
	}
}

func TestToolRegistry_ValidateToolAccess(t *testing.T) {
	// Ensure builtins are registered
	RegisterBuiltinTools()

	registry := GetRegistry()

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid tool references",
			cfg: Config{
				Agents: map[string]AgentConfig{
					"test": {
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "exec"},
							{Kind: ToolKindTool, ID: "skill_search"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "unregistered tool",
			cfg: Config{
				Agents: map[string]AgentConfig{
					"test": {
						Tools: []ToolRef{
							{Kind: ToolKindTool, ID: "nonexistent_tool"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "agent kind tools skip registry check",
			cfg: Config{
				Agents: map[string]AgentConfig{
					"test": {
						Tools: []ToolRef{
							{Kind: ToolKindAgent, AgentID: "some_agent"},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.ValidateToolAccess(&tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRegisterBuiltinTools_Idempotent(t *testing.T) {
	// First call should succeed
	err := RegisterBuiltinTools()
	require.NoError(t, err)

	// Second call should also succeed (idempotent via sync.Once)
	err = RegisterBuiltinTools()
	require.NoError(t, err)

	// Verify tools are still accessible after multiple calls
	registry := GetRegistry()
	_, ok := registry.GetPlainToolFactory("exec")
	assert.True(t, ok, "exec should still be registered after idempotent call")
}

// ============================================================================
// MeditationConfig parsing test
// ============================================================================

func TestMeditationConfig_Fields(t *testing.T) {
	mc := MeditationConfig{
		Enabled:    true,
		Interval:   "30m",
		MinGap:     "2h",
		PromptFile: "meditation.md",
	}

	assert.True(t, mc.Enabled)
	assert.Equal(t, "30m", mc.Interval)
	assert.Equal(t, "2h", mc.MinGap)
	assert.Equal(t, "meditation.md", mc.PromptFile)
}

func TestLoadConfig_ExampleYAML(t *testing.T) {
	cfg, err := LoadConfig("examples/wechat-bot/tagent.yaml")
	require.NoError(t, err)
	require.NotNil(t, cfg)

	actionCfg, ok := cfg.Agents["action"]
	require.True(t, ok, "action agent should exist")

	var fileToolIDs []string
	for _, tr := range actionCfg.Tools {
		if tr.Kind == ToolKindTool {
			fileToolIDs = append(fileToolIDs, tr.ID)
		}
	}
	assert.Contains(t, fileToolIDs, "read_file")
	assert.Contains(t, fileToolIDs, "save_file")
	assert.NotContains(t, cfg.Agents, "read")
	assert.NotContains(t, cfg.Agents, "write")
}

// TestAgentConfig_TaskTerminalTTL: the task_terminal_ttl YAML field parses as
// a duration string and flows to agent.TagentConfig (bounds the resume_task
// window for terminal subagent tasks).
func TestAgentConfig_TaskTerminalTTL(t *testing.T) {
	var acfg AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte("task_terminal_ttl: \"30m\"\n"), &acfg))
	assert.Equal(t, "30m", acfg.TaskTerminalTTL)

	ttl, err := time.ParseDuration(acfg.TaskTerminalTTL)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Minute, ttl)

	// Unset stays empty → buildAgent leaves TagentConfig.TaskTerminalTTL zero
	// → task package default (2m).
	var empty AgentConfig
	require.NoError(t, yaml.Unmarshal([]byte("max_tokens: 1000\n"), &empty))
	assert.Empty(t, empty.TaskTerminalTTL)
}

// TestResolveLifecycleConfig (D11): YAML-declared lifecycle fields override
// the built-in defaults; unset fields fall back; negative global TTL disables
// TTL-based forgetting entirely.
func TestResolveLifecycleConfig(t *testing.T) {
	// Nil → pure defaults.
	cfg := resolveLifecycleConfig(nil)
	assert.Equal(t, 7, cfg.GlobalTTLDays)
	assert.Equal(t, 0, cfg.MaxEventsPerPartition)

	// Full override.
	ttl, maxEv := 30, 50000
	cfg = resolveLifecycleConfig(&LifecycleConfig{
		GlobalTTLDays:         &ttl,
		TypeTTL:               map[string]int{"thinking_plan": 1},
		CheckInterval:         "15m",
		MaxEventsPerPartition: &maxEv,
	})
	assert.Equal(t, 30, cfg.GlobalTTLDays)
	assert.Equal(t, 1, cfg.TypeTTL["thinking_plan"])
	assert.Equal(t, 30, cfg.TypeTTL["external_input"], "unspecified types keep default table")
	assert.Equal(t, 50000, cfg.MaxEventsPerPartition)
	assert.Equal(t, int64(15*60), int64(cfg.CheckInterval.Seconds()))

	// Negative global TTL disables TTL; invalid interval falls back.
	off := -1
	cfg = resolveLifecycleConfig(&LifecycleConfig{
		GlobalTTLDays: &off,
		CheckInterval: "not-a-duration",
	})
	assert.Equal(t, -1, cfg.GlobalTTLDays)
	assert.Equal(t, int64(3600), int64(cfg.CheckInterval.Seconds()), "invalid interval keeps 1h default")
}

// TestLifecycleConfigYAML: the lifecycle block parses from YAML into
// MemoryConfig (field names are the contract users write in tagent.yaml).
func TestLifecycleConfigYAML(t *testing.T) {
	yamlSrc := `
entry: a
model: m
providers:
  p:
    api_endpoint: "http://x"
agents:
  a:
    memory:
      type: localfile
      path: /tmp/x
      lifecycle:
        global_ttl_days: 30
        type_ttl:
          thinking_plan: 1
        check_interval: "15m"
        max_events_per_partition: 50000
`
	tmp := t.TempDir()
	path := tmp + "/tagent.yaml"
	require.NoError(t, os.WriteFile(path, []byte(yamlSrc), 0o644))
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	lc := cfg.Agents["a"].Memory.Lifecycle
	require.NotNil(t, lc)
	require.NotNil(t, lc.GlobalTTLDays)
	assert.Equal(t, 30, *lc.GlobalTTLDays)
	assert.Equal(t, 1, lc.TypeTTL["thinking_plan"])
	assert.Equal(t, "15m", lc.CheckInterval)
	require.NotNil(t, lc.MaxEventsPerPartition)
	assert.Equal(t, 50000, *lc.MaxEventsPerPartition)
}

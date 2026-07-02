package tagent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// tagent should have 3 tools: knowledge (agent), recall (agent), action (tool)
	require.Len(t, tagentAgent.Tools, 3, "tagent should have 3 tools")

	assert.Equal(t, ToolKindAgent, tagentAgent.Tools[0].Kind)
	assert.Equal(t, "knowledge", tagentAgent.Tools[0].AgentID)

	assert.Equal(t, ToolKindAgent, tagentAgent.Tools[1].Kind)
	assert.Equal(t, "recall", tagentAgent.Tools[1].AgentID)

	assert.Equal(t, ToolKindTool, tagentAgent.Tools[2].Kind)
	assert.Equal(t, "action", tagentAgent.Tools[2].ID)
}

func TestDefaultConfig_MeditationConfig(t *testing.T) {
	cfg := DefaultConfig()

	tagentAgent := cfg.Agents["tagent"]
	assert.False(t, tagentAgent.Meditation.Enabled,
		"DefaultConfig should not enable meditation by default")
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

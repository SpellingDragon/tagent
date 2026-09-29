package tagent

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/tool/action"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockSkillRepo is a minimal SkillRepository implementation for testing.
type mockSkillRepo struct{}

func (m *mockSkillRepo) Summaries() []skill.Summary { return nil }
func (m *mockSkillRepo) Get(name string) (*skill.Skill, error) {
	return nil, errors.New("skill not found")
}

// mockToolSet is a minimal trpctool.ToolSet implementation for testing.
type mockToolSet struct{}

func (m *mockToolSet) Tools(_ context.Context) []trpctool.Tool { return nil }
func (m *mockToolSet) Close() error                            { return nil }
func (m *mockToolSet) Name() string                            { return "mock" }

// mockCallableTool is a minimal CallableTool for factory results.
type mockCallableTool struct {
	name string
}

func (m *mockCallableTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: m.name}
}
func (m *mockCallableTool) Call(_ context.Context, _ []byte) (any, error) { return nil, nil }

// TestBuildPlainToolRef_InjectRuntimeDependencies verifies that buildPlainToolRef
// correctly injects MemStore, SkillRepo, MCPToolSets, and ReadPartitionIDs into
// the PlainToolFactoryConfig passed to the registered factory.
//
// 契约: docs/wiki/tool/tool-architecture.md#tool-registry
func TestBuildPlainToolRef_InjectRuntimeDependencies(t *testing.T) {
	var captured agent.PlainToolFactoryConfig

	agent.RegisterPlainTool("test_inject", func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		captured = cfg
		return &mockCallableTool{name: cfg.ID}, nil
	})

	memStore := memory.NewInMemoryStore()
	skillRepo := &mockSkillRepo{}
	mcpSets := []trpctool.ToolSet{&mockToolSet{}}
	readPartitionIDs := []int{1, 2, 3}

	rc := &runtimeConfig{
		skillRepo:   skillRepo,
		mcpToolSets: mcpSets,
	}

	tr := ToolRef{
		Kind:        ToolKindTool,
		ID:          "test_inject",
		Description: "injection test tool",
		Properties:  map[string]any{"key": "value"},
	}

	callable, isAction, err := buildPlainToolRef(tr, "", "", rc, memStore, readPartitionIDs, "desc", nil, 0)
	require.NoError(t, err)
	require.NotNil(t, callable)
	assert.False(t, isAction)

	assert.Equal(t, "test_inject", captured.ID)
	assert.Equal(t, "desc", captured.Description)
	assert.Equal(t, map[string]any{"key": "value"}, captured.Properties)
	assert.Equal(t, memStore, captured.MemStore)
	assert.Equal(t, skillRepo, captured.SkillRepo)
	assert.Equal(t, mcpSets, captured.MCPToolSets)
	assert.Equal(t, readPartitionIDs, captured.ReadPartitionIDs)
}

// TestBuildPlainToolRef_UnregisteredID verifies that buildPlainToolRef returns
// an error when the plain tool id is not registered.
func TestBuildPlainToolRef_UnregisteredID(t *testing.T) {
	memStore := memory.NewInMemoryStore()
	rc := &runtimeConfig{}

	tr := ToolRef{
		Kind: ToolKindTool,
		ID:   "not_registered_ever",
	}

	callable, _, err := buildPlainToolRef(tr, "", "", rc, memStore, nil, "desc", nil, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plain tool factory registered")
	assert.Nil(t, callable)
}

// TestBuildPlainToolRef_ActionToolIsMarked verifies that buildPlainToolRef returns
// isAction=true when the factory produces an *action.ActionTool.
func TestBuildPlainToolRef_ActionToolIsMarked(t *testing.T) {
	// The builtin "exec" factory is registered by RegisterBuiltinTools.
	require.NoError(t, RegisterBuiltinTools())

	memStore := memory.NewInMemoryStore()
	rc := &runtimeConfig{}

	tr := ToolRef{
		Kind:        ToolKindTool,
		ID:          "exec",
		Description: "execute commands",
	}

	callable, isAction, err := buildPlainToolRef(tr, "", "", rc, memStore, nil, "exec tool", nil, 0)
	require.NoError(t, err)
	require.NotNil(t, callable)
	assert.True(t, isAction)
}

func TestActionFactory_Properties(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]any
		wantErr    bool
	}{
		{
			name:       "empty properties",
			properties: nil,
			wantErr:    false,
		},
		{
			name: "work_dir only",
			properties: map[string]any{
				"work_dir": "/tmp/tagent-workspace",
			},
			wantErr: false,
		},
		{
			name: "run_as_user only",
			properties: map[string]any{
				"run_as_user": "tagent-runner",
			},
			wantErr: false,
		},
		{
			name: "run_as_group only",
			properties: map[string]any{
				"run_as_group": "tagent-runner",
			},
			wantErr: false,
		},
		{
			name: "all properties",
			properties: map[string]any{
				"work_dir":     "/tmp/tagent-workspace",
				"run_as_user":  "tagent-runner",
				"run_as_group": "tagent-runner",
			},
			wantErr: false,
		},
		{
			name: "non-string property values ignored",
			properties: map[string]any{
				"work_dir": 123,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callable, err := actionFactory(agent.PlainToolFactoryConfig{
				ID:         "exec",
				Properties: tt.properties,
			})

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, callable)

			// Verify it is an ActionTool.
			at, ok := callable.(*action.ActionTool)
			require.True(t, ok, "factory should return *action.ActionTool")
			assert.NotNil(t, at)
		})
	}
}

func TestActionFactory_ReturnsCallableTool(t *testing.T) {
	callable, err := actionFactory(agent.PlainToolFactoryConfig{ID: "exec"})
	require.NoError(t, err)
	require.NotNil(t, callable)

	// Declaration should be non-nil.
	decl := callable.Declaration()
	require.NotNil(t, decl)
	assert.Equal(t, "action", decl.Name)
}

// factoryMockModel satisfies model.Model for config-driven builds.
type factoryMockModel struct{}

func (m *factoryMockModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true}
	close(ch)
	return ch, nil
}
func (m *factoryMockModel) Info() model.Info { return model.Info{Name: "factory-mock-model"} }

// TestBuildAgent_ProtectsBuiltinAgentNames verifies that all builtin agent names
// are built via the config-driven path even when a ToolAgentFactory is registered
// for them.
func TestBuildAgent_ProtectsBuiltinAgentNames(t *testing.T) {
	// Register a factory that would produce an agent named "factory-built".
	factoryRegistered := false
	agent.RegisterToolAgent("*", func(cfg agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		factoryRegistered = true
		return nil, assert.AnError
	})

	cfg := Config{
		Agents: map[string]AgentConfig{
			"knowledge": {
				SystemPrompt: PromptConfig{Inline: "knowledge agent prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
			"recall": {
				SystemPrompt: PromptConfig{Inline: "recall agent prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
			"action": {
				SystemPrompt: PromptConfig{Inline: "action agent prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	for name := range builtinAgentNames {
		factoryRegistered = false
		acfg := cfg.Agents[name]
		ta, err := buildAgent(name, acfg, cfg, rc, loader, cache, buildModeResident)
		require.NoError(t, err, "building builtin agent %q should succeed via config-driven path", name)
		require.NotNil(t, ta)
		assert.False(t, factoryRegistered, "builtin agent %q should not use ToolAgentFactory", name)
		assert.Equal(t, name, ta.Info().Name, "builtin agent %q should retain its config-driven identity", name)
	}
}

// TestBuildAgent_AllowsCustomAgentFactory verifies that non-builtin agent names
// can still be built via a registered ToolAgentFactory — and that the factory's
// chosen identity survives: the migrated (round-91) contract delivers a
// DECLARATION whose Name the org respects verbatim, exactly as the old contract
// used to use the returned instance verbatim.
func TestBuildAgent_AllowsCustomAgentFactory(t *testing.T) {
	customName := "custom_agent"
	agent.RegisterToolAgent(customName, func(cfg agent.ToolAgentFactoryConfig) (*agent.TagentConfig, error) {
		// What a factory does now: describe the agent; the org constructs it.
		return &agent.TagentConfig{
			Name:         "factory-built",
			Model:        cfg.Model,
			SystemPrompt: "factory-built prompt",
		}, nil
	})

	cfg := Config{
		Agents: map[string]AgentConfig{
			customName: {
				SystemPrompt: PromptConfig{Inline: "custom agent prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
		},
	}
	rc := &runtimeConfig{model: &factoryMockModel{}}
	loader := prompt.NewLoader("")
	cache := make(map[string]*agent.TagentAgent)

	ta, err := buildAgent(customName, cfg.Agents[customName], cfg, rc, loader, cache, buildModeResident)
	require.NoError(t, err)
	require.NotNil(t, ta)
	assert.Equal(t, "factory-built", ta.Info().Name, "custom agent should use ToolAgentFactory")
}

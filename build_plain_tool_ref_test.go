package tagent

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	callable, isAction, err := buildPlainToolRef(tr, rc, memStore, readPartitionIDs, "desc")
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

	callable, _, err := buildPlainToolRef(tr, rc, memStore, nil, "desc")
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

	callable, isAction, err := buildPlainToolRef(tr, rc, memStore, nil, "exec tool")
	require.NoError(t, err)
	require.NotNil(t, callable)
	assert.True(t, isAction)
}

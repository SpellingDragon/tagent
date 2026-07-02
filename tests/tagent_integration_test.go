package tagent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpcskill "trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ---------------------------------------------------------------------------
// Local test mocks
// ---------------------------------------------------------------------------

type intTestMockModel struct{}

func (m *intTestMockModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true}
	close(ch)
	return ch, nil
}

func (m *intTestMockModel) Info() model.Info { return model.Info{Name: "int-test-mock-model"} }

type intTestMockSkillRepo struct{}

func (m *intTestMockSkillRepo) Summaries() []trpcskill.Summary { return nil }
func (m *intTestMockSkillRepo) Get(name string) (*trpcskill.Skill, error) {
	return nil, errors.New("skill not found")
}

type intTestMockToolSet struct{}

func (m *intTestMockToolSet) Tools(_ context.Context) []trpctool.Tool { return nil }
func (m *intTestMockToolSet) Close() error                            { return nil }
func (m *intTestMockToolSet) Name() string                            { return "mock" }

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newIntTestConfig() tagent.Config {
	return tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt: tagent.PromptConfig{Inline: "You are the entry agent."},
				Memory:       tagent.MemoryConfig{Type: "memory"},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindAgent, AgentID: "knowledge", Description: "knowledge tool"},
					{Kind: tagent.ToolKindAgent, AgentID: "recall", Description: "recall tool"},
					{Kind: tagent.ToolKindTool, ID: "exec", Description: "action tool"},
				},
			},
			"knowledge": {
				SystemPrompt: tagent.PromptConfig{Inline: "You are the knowledge agent."},
				Memory:       tagent.MemoryConfig{Type: "memory"},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindTool, ID: "skill_search", Description: "search skills"},
					{Kind: tagent.ToolKindTool, ID: "skill_load", Description: "load a skill"},
					{Kind: tagent.ToolKindTool, ID: "mcp_discover", Description: "discover mcp tools"},
					{Kind: tagent.ToolKindTool, ID: "web_search", Description: "search the web"},
					{Kind: tagent.ToolKindTool, ID: "duckduckgo_search", Description: "search with duckduckgo"},
					{Kind: tagent.ToolKindTool, ID: "memory_query", Description: "query memory"},
				},
			},
			"recall": {
				SystemPrompt: tagent.PromptConfig{Inline: "You are the recall agent."},
				Memory:       tagent.MemoryConfig{Type: "memory"},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindTool, ID: "recall_query", Description: "query recall"},
					{Kind: tagent.ToolKindTool, ID: "recall_get", Description: "get recall"},
					{Kind: tagent.ToolKindTool, ID: "recall_recent", Description: "recent recall"},
					{Kind: tagent.ToolKindTool, ID: "recall_trace", Description: "trace recall"},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Integration tests (black-box, using exported API + TestingBuildAgent)
// ---------------------------------------------------------------------------

func TestTagentNew_Success(t *testing.T) {
	require.NoError(t, tagent.RegisterBuiltinTools())

	cfg := newIntTestConfig()
	mockModel := &intTestMockModel{}

	entryAgent, err := tagent.New(
		cfg,
		tagent.WithModel(mockModel),
		tagent.WithSkillRepo(&intTestMockSkillRepo{}),
		tagent.WithMCPToolSets([]trpctool.ToolSet{&intTestMockToolSet{}}),
	)
	require.NoError(t, err)
	require.NotNil(t, entryAgent)

	tools := entryAgent.Tools()
	require.GreaterOrEqual(t, len(tools), 3)

	names := make(map[string]bool)
	for _, tool := range tools {
		decl := tool.Declaration()
		if decl != nil {
			names[decl.Name] = true
		}
	}

	assert.True(t, names["knowledge"], "entry agent should have knowledge tool")
	assert.True(t, names["recall"], "entry agent should have recall tool")
	assert.True(t, names["action"], "entry agent should have action tool")
}

func TestTagentNew_KnowledgeAgentHasSixPlainTools(t *testing.T) {
	cfg := newIntTestConfig()
	require.NoError(t, tagent.RegisterBuiltinTools())

	knowledgeCfg := cfg.Agents["knowledge"]
	agentCache := make(map[string]*agent.TagentAgent)
	loader := prompt.NewLoader("")

	knowledgeAgent, err := tagent.TestingBuildAgent(
		"knowledge", knowledgeCfg, cfg,
		&intTestMockModel{},
		&intTestMockSkillRepo{},
		[]trpctool.ToolSet{&intTestMockToolSet{}},
		loader, agentCache,
	)
	require.NoError(t, err)
	require.NotNil(t, knowledgeAgent)

	decls := knowledgeAgent.Tools()
	require.Len(t, decls, 6, "knowledge agent should have 6 plain tools")
}

func TestTagentNew_RecallAgentHasFourPlainTools(t *testing.T) {
	cfg := newIntTestConfig()
	require.NoError(t, tagent.RegisterBuiltinTools())

	recallCfg := cfg.Agents["recall"]
	agentCache := make(map[string]*agent.TagentAgent)
	loader := prompt.NewLoader("")

	recallAgent, err := tagent.TestingBuildAgent(
		"recall", recallCfg, cfg,
		&intTestMockModel{},
		nil, nil,
		loader, agentCache,
	)
	require.NoError(t, err)
	require.NotNil(t, recallAgent)

	decls := recallAgent.Tools()
	require.Len(t, decls, 4, "recall agent should have 4 plain tools")
}

func TestTagentNew_UnregisteredToolReturnsError(t *testing.T) {
	cfg := newIntTestConfig()
	cfg.Agents["tagent"] = tagent.AgentConfig{
		SystemPrompt: tagent.PromptConfig{Inline: "You are the entry agent."},
		Memory:       tagent.MemoryConfig{Type: "memory"},
		Tools: []tagent.ToolRef{
			{Kind: tagent.ToolKindTool, ID: "definitely_not_registered"},
		},
	}

	entryAgent, err := tagent.New(cfg, tagent.WithModel(&intTestMockModel{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool access validation")
	assert.Nil(t, entryAgent)
}

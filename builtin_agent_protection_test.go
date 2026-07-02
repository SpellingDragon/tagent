package tagent

import (
	"context"
	"testing"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// factoryMockAgent is a lightweight agent.Agent for ToolAgentFactory tests.
type factoryMockAgent struct {
	name string
}

func (m *factoryMockAgent) Run(_ context.Context, _ *trpcagent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}

func (m *factoryMockAgent) Tools() []trpctool.Tool { return nil }
func (m *factoryMockAgent) Info() trpcagent.Info {
	return trpcagent.Info{Name: m.name, Description: "factory-built mock agent"}
}
func (m *factoryMockAgent) SubAgents() []trpcagent.Agent          { return nil }
func (m *factoryMockAgent) FindSubAgent(_ string) trpcagent.Agent { return nil }

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
	agent.RegisterToolAgent("*", func(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
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
			"speak": {
				SystemPrompt: PromptConfig{Inline: "speak agent prompt"},
				Memory:       MemoryConfig{Type: "memory"},
			},
			"draw": {
				SystemPrompt: PromptConfig{Inline: "draw agent prompt"},
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
		ta, err := buildAgent(name, acfg, cfg, rc, loader, cache)
		require.NoError(t, err, "building builtin agent %q should succeed via config-driven path", name)
		require.NotNil(t, ta)
		assert.False(t, factoryRegistered, "builtin agent %q should not use ToolAgentFactory", name)
		assert.Equal(t, name, ta.Info().Name, "builtin agent %q should retain its config-driven identity", name)
	}
}

// TestBuildAgent_AllowsCustomAgentFactory verifies that non-builtin agent names
// can still be built via a registered ToolAgentFactory.
func TestBuildAgent_AllowsCustomAgentFactory(t *testing.T) {
	customName := "custom_agent"
	agent.RegisterToolAgent(customName, func(cfg agent.ToolAgentFactoryConfig) (*agent.TagentAgent, error) {
		// Return a TagentAgent built from a minimal config so it satisfies the type.
		return agent.NewTagentAgent(&agent.TagentConfig{
			Name:         "factory-built",
			Model:        cfg.Model,
			SystemPrompt: "factory-built prompt",
		})
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

	ta, err := buildAgent(customName, cfg.Agents[customName], cfg, rc, loader, cache)
	require.NoError(t, err)
	require.NotNil(t, ta)
	assert.Equal(t, "factory-built", ta.Info().Name, "custom agent should use ToolAgentFactory")
}

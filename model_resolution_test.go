package tagent

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// stubModel is a minimal model.Model for testing resolveAgentModel.
type stubModel struct{ name string }

func (m *stubModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	return nil, nil
}
func (m *stubModel) Info() model.Info { return model.Info{Name: m.name} }

func TestResolveAgentModel_OverridesTakePrecedence(t *testing.T) {
	overrideModel := &stubModel{name: "override"}
	rc := &runtimeConfig{
		model: &stubModel{name: "default"},
		modelOverrides: map[string]model.Model{
			"tagent": overrideModel,
		},
	}

	cfg := Config{
		Provider: "openai",
		Agents: map[string]AgentConfig{
			"tagent": {Model: "glm-5"},
		},
	}

	got := rc.resolveAgentModel("tagent", cfg.Agents["tagent"], cfg)
	assert.Equal(t, "override", got.Info().Name)
}

func TestResolveAgentModel_NoModelField_UsesParent(t *testing.T) {
	parentModel := &stubModel{name: "parent"}
	rc := &runtimeConfig{
		model: parentModel,
	}

	cfg := Config{
		Provider: "openai",
		Agents: map[string]AgentConfig{
			"recall": {}, // no Model field
		},
	}

	got := rc.resolveAgentModel("recall", cfg.Agents["recall"], cfg)
	assert.Equal(t, "parent", got.Info().Name)
}

func TestResolveAgentModel_ResolvesFromProvider(t *testing.T) {
	// Set a dummy API key so the provider can create a model.
	os.Setenv("TEST_API_KEY", "test-key-123")
	defer os.Unsetenv("TEST_API_KEY")

	parentModel := &stubModel{name: "parent"}
	rc := &runtimeConfig{
		model: parentModel,
	}

	cfg := Config{
		Provider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIEndpoint: "https://api.example.com/v1",
				APIKeyEnv:   "TEST_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"knowledge": {Model: "gpt-4"},
		},
	}

	got := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	require.NotNil(t, got)
	// Should NOT be the parent model — it should be resolved via provider.
	assert.NotEqual(t, "parent", got.Info().Name)
}

func TestResolveAgentModel_CachesResolvedModels(t *testing.T) {
	os.Setenv("TEST_API_KEY", "test-key-456")
	defer os.Unsetenv("TEST_API_KEY")

	rc := &runtimeConfig{
		model: &stubModel{name: "parent"},
	}

	cfg := Config{
		Provider: "openai",
		Providers: map[string]ProviderConfig{
			"openai": {
				APIEndpoint: "https://api.example.com/v1",
				APIKeyEnv:   "TEST_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"knowledge": {Model: "gpt-4"},
			"recall":    {Model: "gpt-4"}, // same model → should reuse cached instance
		},
	}

	got1 := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	got2 := rc.resolveAgentModel("recall", cfg.Agents["recall"], cfg)

	// Both should resolve to the same cached instance.
	assert.Same(t, got1, got2)
}

func TestResolveAgentModel_AgentProviderOverridesGlobal(t *testing.T) {
	os.Setenv("TEST_KEY_A", "key-a")
	os.Setenv("TEST_KEY_B", "key-b")
	defer os.Unsetenv("TEST_KEY_A")
	defer os.Unsetenv("TEST_KEY_B")

	rc := &runtimeConfig{
		model: &stubModel{name: "parent"},
	}

	cfg := Config{
		Provider: "openai", // global default
		Providers: map[string]ProviderConfig{
			"openai": {
				APIEndpoint: "https://api-a.example.com/v1",
				APIKeyEnv:   "TEST_KEY_A",
			},
			"anthropic": {
				APIEndpoint: "https://api-b.example.com",
				APIKeyEnv:   "TEST_KEY_B",
			},
		},
		Agents: map[string]AgentConfig{
			"knowledge": {
				Model:    "claude-3",
				Provider: "anthropic", // override global provider
			},
		},
	}

	got := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	require.NotNil(t, got)
	// Should use anthropic provider, not openai.
	assert.NotEqual(t, "parent", got.Info().Name)
}

func TestResolveAgentModel_FallsBackOnProviderError(t *testing.T) {
	rc := &runtimeConfig{
		model: &stubModel{name: "parent"},
	}

	cfg := Config{
		Provider: "nonexistent_provider",
		Agents: map[string]AgentConfig{
			"knowledge": {Model: "some-model"},
		},
	}

	got := rc.resolveAgentModel("knowledge", cfg.Agents["knowledge"], cfg)
	// Should fall back to parent model on error.
	assert.Equal(t, "parent", got.Info().Name)
}

// ============================================================================
// Config Provider fields tests
// ============================================================================

func TestConfig_ApplyDefaults_SetsProvider(t *testing.T) {
	cfg := Config{
		Entry: "tagent",
		Agents: map[string]AgentConfig{
			"tagent": {},
		},
	}
	cfg.ApplyDefaults()
	assert.Equal(t, "openai", cfg.Provider)
}

func TestConfig_ProviderConfigParsing(t *testing.T) {
	yamlData := `
provider: anthropic
providers:
  anthropic:
    api_endpoint: "https://api.anthropic.com"
    api_key_env: "ANTHROPIC_API_KEY"
  openai:
    api_endpoint: "https://api.openai.com/v1"
    api_key_env: "OPENAI_API_KEY"
agents:
  tagent:
    provider: anthropic
    model: claude-3
  knowledge:
    model: gpt-4
entry: tagent
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	cfg.ApplyDefaults()
	assert.Equal(t, "anthropic", cfg.Provider)
	assert.Len(t, cfg.Providers, 2)
	assert.Equal(t, "https://api.anthropic.com", cfg.Providers["anthropic"].APIEndpoint)
	assert.Equal(t, "ANTHROPIC_API_KEY", cfg.Providers["anthropic"].APIKeyEnv)
	assert.Equal(t, "anthropic", cfg.Agents["tagent"].Provider)
	assert.Equal(t, "", cfg.Agents["knowledge"].Provider) // falls back to global
}

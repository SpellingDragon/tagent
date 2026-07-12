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

// TestTencentProvider_Hy3Model verifies that the tencent provider (OpenAI-compatible)
// can successfully call the hy3 model. This test requires TENCENT_API_KEY env var.
func TestTencentProvider_Hy3Model(t *testing.T) {
	apiKey := os.Getenv("TENCENT_API_KEY")
	if apiKey == "" {
		t.Skip("TENCENT_API_KEY not set, skipping integration test")
	}

	cfg := Config{
		Provider: "tencent",
		Providers: map[string]ProviderConfig{
			"tencent": {
				Provider:    "openai", // tencent uses OpenAI-compatible protocol
				APIEndpoint: "https://tokenhub.tencentmaas.com/v1",
				APIKeyEnv:   "TENCENT_API_KEY",
			},
		},
		Agents: map[string]AgentConfig{
			"test": {
				Provider: "tencent",
				Model:    "hy3",
			},
		},
	}
	cfg.ApplyDefaults()

	rc := &runtimeConfig{}
	resolvedModel := rc.resolveAgentModel("test", cfg.Agents["test"], cfg)
	require.NotNil(t, resolvedModel, "model should be resolved")

	// Test actual model call
	ctx := context.Background()
	req := &model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "你好，请用一句话介绍自己"},
		},
	}

	respCh, err := resolvedModel.GenerateContent(ctx, req)
	require.NoError(t, err, "GenerateContent should not error")

	var fullContent string
	for resp := range respCh {
		if resp != nil && len(resp.Choices) > 0 {
			fullContent += resp.Choices[0].Message.Content
		}
	}

	assert.NotEmpty(t, fullContent, "response content should not be empty")
	t.Logf("hy3 model response: %s", fullContent)
}

func TestConfig_ProviderConfigWithProtocolField(t *testing.T) {
	yamlData := `
provider: zhipu
providers:
  zhipu:
    provider: openai           # 智谱 GLM 使用 OpenAI 兼容协议
    api_endpoint: "https://open.bigmodel.cn/api/paas/v4"
    api_key_env: "ZAI_API_KEY"
  deepseek:
    provider: openai           # DeepSeek 也使用 OpenAI 兼容协议
    api_endpoint: "https://api.deepseek.com/v1"
    api_key_env: "DEEPSEEK_API_KEY"
  anthropic:
    provider: anthropic        # Anthropic 使用原生协议
    api_endpoint: "https://api.anthropic.com"
    api_key_env: "ANTHROPIC_API_KEY"
agents:
  tagent:
    provider: zhipu
    model: glm-5
  knowledge:
    provider: deepseek
    model: deepseek-chat
  action:
    provider: anthropic
    model: claude-3
entry: tagent
`
	var cfg Config
	err := yaml.Unmarshal([]byte(yamlData), &cfg)
	require.NoError(t, err)
	cfg.ApplyDefaults()

	// Verify provider field parsing
	assert.Equal(t, "zhipu", cfg.Provider)
	assert.Len(t, cfg.Providers, 3)

	// Verify zhipu provider (OpenAI-compatible)
	assert.Equal(t, "openai", cfg.Providers["zhipu"].Provider)
	assert.Equal(t, "https://open.bigmodel.cn/api/paas/v4", cfg.Providers["zhipu"].APIEndpoint)
	assert.Equal(t, "ZAI_API_KEY", cfg.Providers["zhipu"].APIKeyEnv)

	// Verify deepseek provider (OpenAI-compatible)
	assert.Equal(t, "openai", cfg.Providers["deepseek"].Provider)
	assert.Equal(t, "https://api.deepseek.com/v1", cfg.Providers["deepseek"].APIEndpoint)
	assert.Equal(t, "DEEPSEEK_API_KEY", cfg.Providers["deepseek"].APIKeyEnv)

	// Verify anthropic provider (native protocol)
	assert.Equal(t, "anthropic", cfg.Providers["anthropic"].Provider)
	assert.Equal(t, "https://api.anthropic.com", cfg.Providers["anthropic"].APIEndpoint)
	assert.Equal(t, "ANTHROPIC_API_KEY", cfg.Providers["anthropic"].APIKeyEnv)

	// Verify agent provider references
	assert.Equal(t, "zhipu", cfg.Agents["tagent"].Provider)
	assert.Equal(t, "deepseek", cfg.Agents["knowledge"].Provider)
	assert.Equal(t, "anthropic", cfg.Agents["action"].Provider)
}

package tagent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/memory"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
)

// wiringTool is a declaration-only tool for wiring tests.
type wiringTool struct{ name string }

func (w *wiringTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: w.name, Description: "wiring tool"}
}

// wiringToolSet is a ToolSet exposing fixed tools.
type wiringToolSet struct {
	name  string
	tools []trpctool.Tool
}

func (m *wiringToolSet) Tools(_ context.Context) []trpctool.Tool { return m.tools }
func (m *wiringToolSet) Close() error                            { return nil }
func (m *wiringToolSet) Name() string                            { return m.name }

// TestBuildPlainToolRef_MCPCallInjectsRegistry verifies buildPlainToolRef
// wires rc.mcpRegistry into the mcp_call factory.
func TestBuildPlainToolRef_MCPCallInjectsRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })
	reg.Add("mock", &wiringToolSet{name: "mock"})

	rc := &runtimeConfig{mcpRegistry: reg}
	tr := ToolRef{Kind: ToolKindTool, ID: "mcp_call"}

	callable, isAction, err := buildPlainToolRef(tr, "", rc, memory.NewInMemoryStore(), nil, "mcp gateway", nil)
	require.NoError(t, err)
	require.NotNil(t, callable)
	assert.False(t, isAction)

	ct, ok := callable.(trpctool.CallableTool)
	require.True(t, ok, "mcp_call must be callable")
	res, err := ct.Call(context.Background(), []byte(`{"server":"nope","tool":"x"}`))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), "unknown MCP server")
	assert.Contains(t, string(b), "mock", "error must list registry servers, proving injection")
}

// TestBuildPlainToolRef_MCPCallWithoutRegistry verifies the factory still
// succeeds when no registry was wired (empty stub behavior).
func TestBuildPlainToolRef_MCPCallWithoutRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	rc := &runtimeConfig{}
	tr := ToolRef{Kind: ToolKindTool, ID: "mcp_call"}

	callable, _, err := buildPlainToolRef(tr, "", rc, memory.NewInMemoryStore(), nil, "mcp gateway", nil)
	require.NoError(t, err)

	ct, ok := callable.(trpctool.CallableTool)
	require.True(t, ok, "mcp_call must be callable")
	res, err := ct.Call(context.Background(), []byte(`{"server":"a","tool":"b"}`))
	require.NoError(t, err)
	b, _ := json.Marshal(res)
	assert.Contains(t, string(b), "no MCP servers are registered")
}

// TestMCPDiscoverFactory_PrefersRegistry verifies the discover factory
// consumes the injected live registry.
func TestMCPDiscoverFactory_PrefersRegistry(t *testing.T) {
	require.NoError(t, RegisterBuiltinTools())

	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })

	factory, ok := agent.GetPlainToolFactory("mcp_discover")
	require.True(t, ok)
	ct, err := factory(agent.PlainToolFactoryConfig{ID: "mcp_discover", MCPRegistry: reg})
	require.NoError(t, err)

	// Registered AFTER factory creation — must still be discoverable (live reads).
	reg.Add("web-search-prime", &wiringToolSet{
		name:  "web-search-prime",
		tools: []trpctool.Tool{&wiringTool{name: "webSearchPrime"}},
	})

	res, err := ct.Call(context.Background(), []byte(`{"query":"webSearchPrime"}`))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	assert.Contains(t, string(b), `mcp_call(server=\"web-search-prime\", tool=\"webSearchPrime\"`)
	assert.Contains(t, string(b), "mcp:web-search-prime")
}

// TestLoadConfig_MCPServers verifies YAML parsing + ConfigPath recording.
func TestLoadConfig_MCPServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tagent.yaml")
	yaml := `
entry: tagent
agents:
  tagent:
    system_prompt:
      inline: "hi"
mcp_servers:
  web-search-prime:
    transport: streamable-http
    url: https://open.bigmodel.cn/api/mcp/web_search_prime/mcp
    api_key_env: ZAI_API_KEY
    timeout: 30s
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)

	sc, ok := cfg.MCPServers["web-search-prime"]
	require.True(t, ok)
	assert.Equal(t, "streamable-http", sc.Transport)
	assert.Equal(t, "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp", sc.URL)
	assert.Equal(t, "ZAI_API_KEY", sc.APIKeyEnv)
	assert.Equal(t, "30s", sc.Timeout)
	assert.True(t, filepath.IsAbs(cfg.ConfigPath), "ConfigPath must be recorded (absolute)")
}

// TestConfigValidate_MCPServers covers the three validation failures.
func TestConfigValidate_MCPServers(t *testing.T) {
	base := func() Config {
		return Config{
			Entry: "tagent",
			Agents: map[string]AgentConfig{
				"tagent": {SystemPrompt: PromptConfig{Inline: "hi"}},
			},
		}
	}

	cases := []struct {
		name    string
		server  MCPServerConfig
		wantErr string
	}{
		{"sse missing url", MCPServerConfig{Transport: "sse"}, "requires url"},
		{"stdio missing command", MCPServerConfig{Transport: "stdio"}, "requires command"},
		{"unsupported transport", MCPServerConfig{Transport: "websocket", URL: "https://x"}, "unsupported transport"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg.MCPServers = map[string]MCPServerConfig{"bad": tc.server}
			cfg.ApplyDefaults()
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Contains(t, err.Error(), "bad")
		})
	}

	// Valid declaration passes.
	cfg := base()
	cfg.MCPServers = map[string]MCPServerConfig{
		"ok": {Transport: "streamable-http", URL: "https://example.com/mcp"},
	}
	cfg.ApplyDefaults()
	require.NoError(t, cfg.Validate())
}

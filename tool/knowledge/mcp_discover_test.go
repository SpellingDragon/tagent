package knowledge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
)

// discoverFakeTool is a declaration-only tool for discovery tests.
type discoverFakeTool struct {
	name string
	desc string
}

func (f *discoverFakeTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        f.name,
		Description: f.desc,
		InputSchema: &tool.Schema{
			Type:       "object",
			Properties: map[string]*tool.Schema{"search_query": {Type: "string"}},
		},
	}
}

// discoverFakeToolSet is a minimal ToolSet for discovery tests.
type discoverFakeToolSet struct {
	name  string
	tools []tool.Tool
}

func (m *discoverFakeToolSet) Tools(_ context.Context) []tool.Tool { return m.tools }
func (m *discoverFakeToolSet) Close() error                        { return nil }
func (m *discoverFakeToolSet) Name() string                        { return m.name }

func discoverCall(t *testing.T, discoverTool tool.Tool, query string) mcpDiscoverResult {
	t.Helper()
	ct, ok := discoverTool.(tool.CallableTool)
	require.True(t, ok)
	res, err := ct.Call(context.Background(), []byte(`{"query":"`+query+`"}`))
	require.NoError(t, err)
	out, ok := res.(mcpDiscoverResult)
	require.True(t, ok)
	return out
}

// TestMCPDiscover_Registry_LiveAddRemove verifies runtime registry
// mutations are visible on the NEXT discover call without any rebuild.
func TestMCPDiscover_Registry_LiveAddRemove(t *testing.T) {
	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })

	discover := NewMCPDiscoverToolWithRegistry(reg)

	// Empty registry → empty result, no error.
	out := discoverCall(t, discover, "webSearchPrime")
	assert.Equal(t, 0, out.Count)

	// Runtime-registered server becomes discoverable immediately.
	reg.Add("web-search-prime", &discoverFakeToolSet{
		name:  "web-search-prime",
		tools: []tool.Tool{&discoverFakeTool{name: "webSearchPrime", desc: "Search the web"}},
	})
	out = discoverCall(t, discover, "webSearchPrime")
	require.Equal(t, 1, out.Count)
	assert.Equal(t, "webSearchPrime", out.Tools[0].Name)
	assert.Equal(t, "mcp:web-search-prime", out.Tools[0].Source)

	// Removed server no longer appears.
	reg.Remove("web-search-prime")
	out = discoverCall(t, discover, "webSearchPrime")
	assert.Equal(t, 0, out.Count)
}

// TestMCPDiscover_TruthfulInvocationGuidance verifies the content carries
// the real mcp_call invocation + input schema and no exec-based lie.
func TestMCPDiscover_TruthfulInvocationGuidance(t *testing.T) {
	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })
	reg.Add("web-search-prime", &discoverFakeToolSet{
		name:  "web-search-prime",
		tools: []tool.Tool{&discoverFakeTool{name: "webSearchPrime", desc: "Search the web"}},
	})

	out := discoverCall(t, NewMCPDiscoverToolWithRegistry(reg), "search")
	require.Equal(t, 1, out.Count)

	content := out.Tools[0].Description
	assert.Contains(t, content, `mcp_call(server="web-search-prime", tool="webSearchPrime"`)
	assert.Contains(t, content, "Input Schema:")
	assert.Contains(t, content, "search_query")
	assert.NotContains(t, content, `command(mode="exec"`)
}

// TestMCPDiscover_NaturalLanguageQuery verifies token-AND fallback: a
// space-separated natural query matches underscore-named tools and
// reordered description words (the shape LLMs actually issue).
func TestMCPDiscover_NaturalLanguageQuery(t *testing.T) {
	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })
	reg.Add("web-search-prime", &discoverFakeToolSet{
		name: "web-search-prime",
		tools: []tool.Tool{&discoverFakeTool{
			name: "web_search_prime",
			desc: "Search web information, returns results including web page title, URL, summary",
		}},
	})
	discover := NewMCPDiscoverToolWithRegistry(reg)

	// Natural language query (space-separated, reversed word order vs desc).
	out := discoverCall(t, discover, "web search")
	require.Equal(t, 1, out.Count, "token-AND fallback must match natural query")
	assert.Equal(t, "web_search_prime", out.Tools[0].Name)

	// Unrelated query still misses.
	out = discoverCall(t, discover, "database migration")
	assert.Equal(t, 0, out.Count)
}

// TestMCPDiscover_OneEmptyServerDoesNotBlockOthers approximates a failing
// server (trpc swallows connection errors and yields no tools) alongside a
// healthy one.
func TestMCPDiscover_OneEmptyServerDoesNotBlockOthers(t *testing.T) {
	reg := toolmcp.NewRegistry()
	t.Cleanup(func() { _ = reg.Close() })
	reg.Add("dead", &discoverFakeToolSet{name: "dead"}) // no tools (unreachable)
	reg.Add("alive", &discoverFakeToolSet{
		name:  "alive",
		tools: []tool.Tool{&discoverFakeTool{name: "ping", desc: "ping tool"}},
	})

	out := discoverCall(t, NewMCPDiscoverToolWithRegistry(reg), "ping")
	require.Equal(t, 1, out.Count)
	assert.Equal(t, "mcp:alive", out.Tools[0].Source)
}

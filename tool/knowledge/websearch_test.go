package knowledge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWebSearchTool_Declaration verifies the web_search tool's Declaration.
func TestNewWebSearchTool_Declaration(t *testing.T) {
	searchTool := NewWebSearchTool()
	require.NotNil(t, searchTool)

	// Verify it satisfies tool.CallableTool interface
	callable, ok := searchTool.(tool.CallableTool)
	require.True(t, ok, "NewWebSearchTool should return a tool.CallableTool")

	decl := callable.Declaration()
	require.NotNil(t, decl, "Declaration should not be nil")

	// Basic identity
	assert.Equal(t, "web_search", decl.Name)
	assert.Contains(t, decl.Description, "Search the web")
	assert.Contains(t, decl.Description, "Zhipu")
	assert.Equal(t, "object", decl.InputSchema.Type)
}

// TestNewWebSearchTool_InputSchema_QueryParameter verifies the input schema
// includes the required "query" parameter.
func TestNewWebSearchTool_InputSchema_QueryParameter(t *testing.T) {
	searchTool := NewWebSearchTool().(tool.CallableTool)
	decl := searchTool.Declaration()

	// Check that InputSchema has "query" property
	require.NotNil(t, decl.InputSchema)
	querySchema, ok := decl.InputSchema.Properties["query"]
	require.True(t, ok, "input schema should have 'query' property")
	assert.Equal(t, "string", querySchema.Type)
	assert.Contains(t, querySchema.Description, "search query")

	// "query" should be in the required list
	assert.Contains(t, decl.InputSchema.Required, "query",
		"query should be a required parameter")
}

// TestNewWebSearchTool_InputSchema_NoExtraParams verifies the input schema
// only contains expected parameters (query only).
func TestNewWebSearchTool_InputSchema_NoExtraParams(t *testing.T) {
	searchTool := NewWebSearchTool().(tool.CallableTool)
	decl := searchTool.Declaration()

	// Only "query" should be in properties
	assert.Len(t, decl.InputSchema.Properties, 1,
		"input schema should only have 'query' property")
}

// TestWebSearch_ZhipuCall verifies the tool posts a well-formed request to
// the Zhipu Web Search API (Bearer auth + expected body fields) and maps the
// search_result array into SearchResult entries.
func TestWebSearch_ZhipuCall(t *testing.T) {
	var gotAuth string
	var gotBody zhipuSearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "test-id",
			"request_id": "test-id",
			"search_result": [
				{"title": "页面一", "link": "https://a.example/1", "content": "摘要一", "media": "媒体甲", "publish_date": "2025-05-23"},
				{"title": "页面二", "link": "https://b.example/2", "content": "摘要二", "media": "", "publish_date": ""}
			]
		}`))
	}))
	defer srv.Close()

	t.Setenv("TEST_ZAI_KEY", "sk-test")
	cfg := DefaultWebSearchConfig()
	cfg.Endpoint = srv.URL
	cfg.APIKeyEnv = "TEST_ZAI_KEY"

	wt := &webSearchTool{cfg: cfg, httpClient: srv.Client()}
	resp, err := wt.search(context.Background(), searchRequest{Query: "测试查询"})
	require.NoError(t, err)

	// Request assertions.
	assert.Equal(t, "Bearer sk-test", gotAuth)
	assert.Equal(t, "测试查询", gotBody.SearchQuery)
	assert.Equal(t, "search_std", gotBody.SearchEngine)
	assert.Equal(t, 10, gotBody.Count)

	// Response mapping assertions.
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "页面一", resp.Results[0].Title)
	assert.Equal(t, "https://a.example/1", resp.Results[0].URL)
	assert.Equal(t, "媒体甲", resp.Results[0].Source)
	assert.Contains(t, resp.Results[0].Description, "摘要一")
	assert.Contains(t, resp.Results[0].Description, "2025-05-23")
	// Empty media falls back to "zhipu".
	assert.Equal(t, "zhipu", resp.Results[1].Source)
	assert.Equal(t, "zhipu", resp.Engine)
}

// TestWebSearch_MissingAPIKey verifies a missing API key yields an informative
// message rather than an error.
func TestWebSearch_MissingAPIKey(t *testing.T) {
	cfg := DefaultWebSearchConfig()
	cfg.APIKeyEnv = "DEFINITELY_UNSET_ENV_VAR_XYZ"
	wt := &webSearchTool{cfg: cfg, httpClient: http.DefaultClient}
	resp, err := wt.search(context.Background(), searchRequest{Query: "anything"})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
	assert.Contains(t, resp.Message, "DEFINITELY_UNSET_ENV_VAR_XYZ")
}

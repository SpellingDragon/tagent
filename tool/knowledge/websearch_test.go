package knowledge

import (
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
	assert.Contains(t, decl.Description, "multiple search engines")
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

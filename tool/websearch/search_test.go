package websearch

import (
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewTool_Declaration verifies the web_search tool's Declaration.
func TestNewTool_Declaration(t *testing.T) {
	searchTool := NewTool()
	require.NotNil(t, searchTool)

	// Verify it satisfies tool.CallableTool interface
	callable, ok := searchTool.(tool.CallableTool)
	require.True(t, ok, "NewTool should return a tool.CallableTool")

	decl := callable.Declaration()
	require.NotNil(t, decl, "Declaration should not be nil")

	// Basic identity
	assert.Equal(t, "web_search", decl.Name)
	assert.Contains(t, decl.Description, "Search the web")
	assert.Contains(t, decl.Description, "multiple search engines")
	assert.Equal(t, "object", decl.InputSchema.Type)
}

// TestNewTool_InputSchema_QueryParameter verifies the input schema
// includes the required "query" parameter.
func TestNewTool_InputSchema_QueryParameter(t *testing.T) {
	searchTool := NewTool().(tool.CallableTool)
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

// TestNewTool_InputSchema_NoExtraParams verifies the input schema
// only contains expected parameters (query only).
func TestNewTool_InputSchema_NoExtraParams(t *testing.T) {
	searchTool := NewTool().(tool.CallableTool)
	decl := searchTool.Declaration()

	// Only "query" should be in properties
	assert.Len(t, decl.InputSchema.Properties, 1,
		"input schema should only have 'query' property")
}

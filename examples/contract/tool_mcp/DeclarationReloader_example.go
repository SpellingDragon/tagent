package mcpexample

import (
	"context"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/tool/mcp"
)

// gatewayDeclarations is the canonical declaration reloader: it re-issues the declarations of
// the built-in gateway tools in the order they are handed to a request, and answers no tool
// the gateway does not serve.
type gatewayDeclarations struct{}

// ReloadDeclarations returns the declarations of the built-in gateway tools in request order.
func (gatewayDeclarations) ReloadDeclarations(_ context.Context) []*trpctool.Declaration {
	args := &trpctool.Schema{
		Type:     "object",
		Required: []string{mcp.MCPCallArgServer, mcp.MCPCallArgTool},
		Properties: map[string]*trpctool.Schema{
			mcp.MCPCallArgServer: {Type: "string"},
			mcp.MCPCallArgTool:   {Type: "string"},
			mcp.MCPCallArgArgs:   {Type: "object"},
		},
	}
	return []*trpctool.Declaration{
		{Name: mcp.ToolNameMCPCall, Description: "call one tool on a registered MCP server", InputSchema: args},
		{Name: mcp.ToolNameMCPDiscover, Description: "list the tools a registered MCP server serves", InputSchema: &trpctool.Schema{Type: "object"}},
	}
}

var _ mcp.DeclarationReloader = gatewayDeclarations{}

// ExampleDeclarationReloader re-issues the gateway declarations: the two built-in tool names
// come back in request order and each carries its input schema, so a request is handed the
// same declarations whether or not a server connection changed.
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func ExampleDeclarationReloader() {
	declarations := gatewayDeclarations{}.ReloadDeclarations(context.Background())
	if len(declarations) != 2 {
		return
	}
	if declarations[0].Name != mcp.ToolNameMCPCall || declarations[1].Name != mcp.ToolNameMCPDiscover {
		return
	}
	_ = declarations[0].InputSchema.Required
}

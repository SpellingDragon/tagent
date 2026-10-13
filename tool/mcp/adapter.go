// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// Package mcp adapts the locked tRPC-Agent-Go MCP tool set to the runtime: it
// registers live server connections, reads discovery from the generation a
// request borrowed, and declares the fixed gateway tools the model sees.
//
// The MCP wire protocol is not reimplemented here. Connection setup, session
// initialization and tool listing stay with the native
// trpc.group/trpc-go/trpc-agent-go/tool/mcp types this package registers and
// hands out.
package mcp

import (
	"context"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// ToolNameMCPCall and ToolNameMCPDiscover name the built-in gateway tools, and
// the MCPCallArg keys name the three arguments of the call gateway. Registry
// content changes none of them, so an agent's declared tool prefix stays
// byte-identical as servers are added or removed.
const (
	// ToolNameMCPCall is the fixed name of the built-in MCP call gateway tool.
	ToolNameMCPCall = "mcp_call"

	// ToolNameMCPDiscover is the fixed name of the built-in MCP discovery tool.
	ToolNameMCPDiscover = "mcp_discover"

	// MCPCallArgServer is the argument key naming the target MCP server.
	MCPCallArgServer = "server"

	// MCPCallArgTool is the argument key naming the tool on that server.
	MCPCallArgTool = "tool"

	// MCPCallArgArgs is the argument key carrying the remote tool arguments.
	MCPCallArgArgs = "args"
)

// MCPCallInputSchemaJSON is the canonical JSON Schema of the mcp_call gateway
// declaration: an object with exactly the three required keys server, tool and
// args, where args carries the argument object forwarded to the remote tool.
//
// A schema constant, not a function of the registry: a call reaches whatever
// the registry holds at call time while the declaration the model was given
// stays the same value.
const MCPCallInputSchemaJSON = `{
  "type": "object",
  "properties": {
    "server": {"type": "string"},
    "tool": {"type": "string"},
    "args": {"type": "object"}
  },
  "required": ["server", "tool", "args"]
}`

// Registry is the concurrent-safe map from server name to live native tool set
// that discovery and the gateway read.
//
// Add and Remove move live connections only; neither changes any agent's
// declared tool set.
type Registry interface {
	// Get returns the tool set registered under name and reports whether it is
	// present.
	Get(ctx context.Context, name string) (trpctool.ToolSet, bool)

	// List returns every registered tool set.
	List(ctx context.Context) []trpctool.ToolSet

	// Names returns the registered server names in ascending order.
	Names(ctx context.Context) []string

	// Add registers a live tool set under name.
	Add(ctx context.Context, name string, toolset trpctool.ToolSet) error

	// Remove unregisters name and releases its tool set.
	Remove(ctx context.Context, name string) error
}

// Connector builds one native MCP tool set from its connection declaration and
// brings its session up before the registry hands the tool set out.
type Connector interface {
	// Connect creates and initializes the native tool set described by config.
	Connect(ctx context.Context, config trpcmcp.ConnectionConfig, opts ...trpcmcp.ToolSetOption) (*trpcmcp.ToolSet, error)
}

// Discovery resolves the tools reachable through one registry and re-lists a
// live tool set.
type Discovery interface {
	// Discover returns the tools reachable through registry as read for the
	// current request, and carries the input schema of every tool it returns.
	Discover(ctx context.Context, registry Registry) ([]trpctool.Tool, error)

	// Refresh re-lists the tools of one live tool set.
	Refresh(ctx context.Context, toolset trpctool.ToolSet) error
}

// DeclarationReloader re-issues the declarations of the built-in gateway tools.
type DeclarationReloader interface {
	// ReloadDeclarations returns the declarations of the built-in gateway tools
	// in the order they are handed to a request.
	ReloadDeclarations(ctx context.Context) []*trpctool.Declaration
}

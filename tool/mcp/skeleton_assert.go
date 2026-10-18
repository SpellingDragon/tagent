package mcp

import (
	"context"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// Skeleton compile-time assertions for the phase 1 adapter declarations in
// adapter.go.
//
// The phase 2 implementation types that will satisfy these interfaces do not
// exist yet, so there is no `var _ Iface = (*Impl)(nil)` to write. The
// assertions below pin what the skeleton proves on its own: every declared
// method keeps its exact shape, every declared name is an interface type, and
// the native MCP tool set still satisfies the native interface the registry
// hands out. A renamed method, a changed parameter or a changed result stops the
// method shape assertions from compiling.
var (
	_ func(Registry, context.Context, string) (trpctool.ToolSet, bool)                                               = Registry.Get
	_ func(Registry, context.Context) []trpctool.ToolSet                                                             = Registry.List
	_ func(Registry, context.Context) []string                                                                       = Registry.Names
	_ func(Registry, context.Context, string, trpctool.ToolSet) error                                                = Registry.Add
	_ func(Registry, context.Context, string) error                                                                  = Registry.Remove
	_ func(Discovery, context.Context, Registry) ([]trpctool.Tool, error)                                            = Discovery.Discover
	_ func(Discovery, context.Context, trpctool.ToolSet) error                                                       = Discovery.Refresh
	_ func(DeclarationReloader, context.Context) []*trpctool.Declaration                                             = DeclarationReloader.ReloadDeclarations
	_ func(Connector, context.Context, trpcmcp.ConnectionConfig, ...trpcmcp.ToolSetOption) (*trpcmcp.ToolSet, error) = Connector.Connect
)

// Interface kinds, plus the native implementation relationship the registry
// depends on.
var (
	_ Registry            = (Registry)(nil)
	_ Connector           = (Connector)(nil)
	_ Discovery           = (Discovery)(nil)
	_ DeclarationReloader = (DeclarationReloader)(nil)
	_ trpctool.ToolSet    = (*trpcmcp.ToolSet)(nil)
)

// Phase 2 implementation relationships: the live registry, the native
// connector, the registry discovery and the fixed gateway tools satisfy the
// declared faces, and the discovered tool and the named discovery failures
// keep their own shapes. A changed face stops this block from compiling.
var (
	_ Registry              = (*ServerRegistry)(nil)
	_ Connector             = (*NativeConnector)(nil)
	_ Discovery             = (*RegistryDiscovery)(nil)
	_ DeclarationReloader   = (*Gateways)(nil)
	_ trpctool.CallableTool = (*CallTool)(nil)
	_ trpctool.CallableTool = (*DiscoverTool)(nil)
	_ trpctool.Tool         = (*DiscoveredTool)(nil)
	_ error                 = (*ServerFailures)(nil)
)

// Gateway identifiers must stay compile-time constants: a variable or a
// computed value stops this block from compiling.
const (
	_ = ToolNameMCPCall
	_ = ToolNameMCPDiscover
	_ = MCPCallArgServer
	_ = MCPCallArgTool
	_ = MCPCallArgArgs
	_ = MCPCallInputSchemaJSON
)

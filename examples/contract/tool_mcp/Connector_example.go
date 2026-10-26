package mcpexample

import (
	"context"
	"errors"
	"strings"

	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"

	"github.com/SpellingDragon/tagent/tool/mcp"
)

// errTransportUnknown is what the canonical connector reports for a declaration whose
// transport names no native kind.
var errTransportUnknown = errors.New("mcp: the declaration names no known transport")

// nativeConnector is the canonical connector: it builds one native tool set from its
// connection declaration and brings that set's session up before it is handed out.
type nativeConnector struct{}

// Connect creates the native tool set described by config and initializes its session.
func (nativeConnector) Connect(ctx context.Context, config trpcmcp.ConnectionConfig, opts ...trpcmcp.ToolSetOption) (*trpcmcp.ToolSet, error) {
	switch strings.ToLower(strings.TrimSpace(config.Transport)) {
	case "stdio", "sse", "streamable":
	default:
		return nil, errTransportUnknown
	}
	toolset := trpcmcp.NewMCPToolSet(config, opts...)
	if err := toolset.Init(ctx); err != nil {
		return nil, errors.Join(err, toolset.Close())
	}
	return toolset, nil
}

var _ mcp.Connector = nativeConnector{}

// ExampleConnector builds one native tool set from its declaration: a declaration naming no
// known transport is refused before anything is dialled, so the registry is never handed a
// set whose session was never up.
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func ExampleConnector() {
	connector := nativeConnector{}
	toolset, err := connector.Connect(context.Background(), trpcmcp.ConnectionConfig{})
	if err == nil || toolset != nil {
		return
	}
}

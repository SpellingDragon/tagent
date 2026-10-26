package mcpexample

import (
	"context"
	"errors"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/tool/mcp"
)

// errDiscoverySchema and errDiscoverySet are what the canonical discovery reports for a
// tool that carries no input schema and for a refresh of no live set.
var (
	errDiscoverySchema = errors.New("mcp: the tool carries no input schema")
	errDiscoverySet    = errors.New("mcp: no live tool set to refresh")
)

// registryDiscovery is the canonical discovery: it resolves the tools reachable through the
// registry it was handed, carrying every input schema, and re-lists a live tool set instead
// of reusing a listing taken earlier.
type registryDiscovery struct{}

// Discover returns the tools reachable through registry, carrying the input schema of each.
func (registryDiscovery) Discover(ctx context.Context, registry mcp.Registry) ([]trpctool.Tool, error) {
	var tools []trpctool.Tool
	for _, set := range registry.List(ctx) {
		if set == nil {
			continue
		}
		for _, one := range set.Tools(ctx) {
			if one == nil || one.Declaration() == nil || one.Declaration().InputSchema == nil {
				return nil, errDiscoverySchema
			}
			tools = append(tools, one)
		}
	}
	return tools, nil
}

// Refresh re-lists the tools of one live tool set.
func (registryDiscovery) Refresh(ctx context.Context, toolset trpctool.ToolSet) error {
	if toolset == nil {
		return errDiscoverySet
	}
	_ = toolset.Tools(ctx)
	return nil
}

var _ mcp.Discovery = registryDiscovery{}

// ExampleDiscovery resolves the tools reachable through one registry: the call is handed the
// registry and reads the tools from it, while a refresh of no live set is refused instead of
// answered from a listing taken earlier.
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func ExampleDiscovery() {
	ctx := context.Background()
	registry := &liveRegistry{}
	tools, err := registryDiscovery{}.Discover(ctx, registry)
	if err != nil || len(tools) != 0 {
		return
	}
	if err := (registryDiscovery{}).Refresh(ctx, nil); err == nil {
		return
	}
}

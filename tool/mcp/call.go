package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// mustParseSchema turns one gateway schema constant into the shared schema
// value every declaration hands out. Parsed once: the declared schema is a
// constant of the package, never a function of what the registry holds.
func mustParseSchema(doc string) *trpctool.Schema {
	var schema trpctool.Schema
	if err := json.Unmarshal([]byte(doc), &schema); err != nil {
		panic("mcp: the gateway schema constant does not parse: " + err.Error())
	}
	return &schema
}

// callInputSchema is the fixed input schema of the call gateway, parsed once
// from MCPCallInputSchemaJSON.
var callInputSchema = mustParseSchema(MCPCallInputSchemaJSON)

// normalizeRegistry folds a typed-nil registry — a nil pointer held by a
// non-nil interface — onto the nil interface, so "no registry is injected"
// can never be mistaken for a live table at call time.
func normalizeRegistry(registry Registry) Registry {
	if registry == nil {
		return nil
	}
	value := reflect.ValueOf(registry)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Interface, reflect.Slice, reflect.Func:
		if value.IsNil() {
			return nil
		}
	}
	return registry
}

// callResult is the self-correcting payload of a gateway failure: every
// failure of the call side comes back as a result carrying its named state and
// the material the model needs to correct itself — the available server list,
// the target server's tool list, or the target tool's input schema.
type callResult struct {
	OK          bool             `json:"ok"`
	Server      string           `json:"server,omitempty"`
	Tool        string           `json:"tool,omitempty"`
	Error       string           `json:"error,omitempty"`
	Servers     []string         `json:"servers,omitempty"`
	Tools       []string         `json:"tools,omitempty"`
	InputSchema *trpctool.Schema `json:"input_schema,omitempty"`
	Hint        string           `json:"hint,omitempty"`
}

// resultJSON packs one gateway result as the JSON text the model reads. A
// failure is still a result — the Go error beside it stays nil — so the turn
// carries the correction material instead of dying on a transport error.
func resultJSON(v any) (any, error) {
	text, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("mcp: the result does not marshal: %v", err), nil
	}
	return string(text), nil
}

// callArgs are the three fixed arguments of the call gateway.
type callArgs struct {
	Server string         `json:"server"`
	Tool   string         `json:"tool"`
	Args   map[string]any `json:"args"`
}

// parseCallArgs decodes the gateway arguments. An empty argument object is
// accepted here and refused below with the schema guidance, so a call that
// forgot everything is told what to send instead of how the JSON broke.
func parseCallArgs(jsonArgs []byte) (callArgs, error) {
	var args callArgs
	trimmed := bytes.TrimSpace(jsonArgs)
	if len(trimmed) == 0 {
		return args, nil
	}
	err := json.Unmarshal(trimmed, &args)
	return args, err
}

// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// CallTool is the fixed mcp_call gateway: one plain tool with the constant
// server/tool/args declaration whose calls reach whatever the injected live
// registry holds at call time. The declaration never reads the registry, so
// runtime registration changes the reachable tool surface and never the tool
// set an agent declared. Every failure comes back as a named result carrying
// the material to correct the next call, and never as a Go error.
type CallTool struct {
	registry    Registry
	declaration *trpctool.Declaration
}

// NewCallTool builds the call gateway over the injected live registry. A
// missing registry must still build: it changes what a call answers, never
// whether the tool exists.
func NewCallTool(registry Registry) *CallTool {
	return &CallTool{
		registry: normalizeRegistry(registry),
		declaration: &trpctool.Declaration{
			Name:        ToolNameMCPCall,
			Description: "call one tool on a registered MCP server: name the server and the tool, and pass the tool arguments object under args; the servers reachable at call time come from the live registry",
			InputSchema: callInputSchema,
		},
	}
}

// Declaration returns the fixed declaration of the call gateway: the same
// name, description and schema whatever the registry holds.
func (t *CallTool) Declaration() *trpctool.Declaration {
	return t.declaration
}

// Call routes one call to the named tool of the named server through the live
// registry. The wire protocol stays with the locked framework tool set the
// registry hands out. Failures answer as named results: an empty table names
// both registration sources, an unknown server echoes the registered names, an
// unknown tool echoes that server's tool list, and a failed inner call carries
// the target tool's input schema.
//
// - A successful call returns the native result untouched: the gateway adds no envelope of its own to a call that answered.
func (t *CallTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	args, err := parseCallArgs(jsonArgs)
	if err != nil {
		return resultJSON(callResult{
			Error:       fmt.Sprintf("mcp: the %s arguments do not parse: %v", ToolNameMCPCall, err),
			InputSchema: t.declaration.InputSchema,
		})
	}
	if args.Server == "" || args.Tool == "" {
		return resultJSON(callResult{
			Server:      args.Server,
			Tool:        args.Tool,
			Error:       fmt.Sprintf("mcp: %s needs both the %s and the %s argument", ToolNameMCPCall, MCPCallArgServer, MCPCallArgTool),
			Servers:     t.registryNames(ctx),
			InputSchema: t.declaration.InputSchema,
		})
	}
	if names := t.registryNames(ctx); len(names) == 0 {
		return resultJSON(callResult{
			Server: args.Server,
			Tool:   args.Tool,
			Error:  "mcp: no MCP server is registered, so no tool can be called",
			Hint:   "register a server first: declare one in the configuration file or register one at runtime",
		})
	}
	toolset, release, ok := borrowServer(ctx, t.registry, args.Server)
	if !ok {
		return resultJSON(callResult{
			Server:  args.Server,
			Tool:    args.Tool,
			Error:   fmt.Sprintf("mcp: no server is registered under the name %q", args.Server),
			Servers: t.registryNames(ctx),
		})
	}
	defer release()

	tools := toolset.Tools(ctx)
	if len(tools) == 0 {
		return resultJSON(callResult{
			Server: args.Server,
			Tool:   args.Tool,
			Error: fmt.Sprintf("mcp: server %q is registered but listed no tools — MCP dependency failure, its connection may be down",
				args.Server),
		})
	}
	names := make([]string, 0, len(tools))
	var target trpctool.Tool
	for _, candidate := range tools {
		decl := candidate.Declaration()
		if decl == nil {
			continue
		}
		names = append(names, decl.Name)
		if decl.Name == args.Tool {
			target = candidate
		}
	}
	if target == nil {
		return resultJSON(callResult{
			Server: args.Server,
			Tool:   args.Tool,
			Error:  fmt.Sprintf("mcp: server %q offers no tool named %q", args.Server, args.Tool),
			Tools:  names,
		})
	}
	called, ok := target.(trpctool.CallableTool)
	if !ok {
		return resultJSON(callResult{
			Server: args.Server,
			Tool:   args.Tool,
			Error:  fmt.Sprintf("mcp: server %q offers tool %q without a call face — MCP dependency failure", args.Server, args.Tool),
			Tools:  names,
		})
	}
	rawArgs := []byte("{}")
	if args.Args != nil {
		if rawArgs, err = json.Marshal(args.Args); err != nil {
			return resultJSON(callResult{
				Server:      args.Server,
				Tool:        args.Tool,
				Error:       fmt.Sprintf("mcp: the %s arguments for %q do not marshal: %v", MCPCallArgArgs, args.Tool, err),
				InputSchema: target.Declaration().InputSchema,
			})
		}
	}
	result, callErr := called.Call(ctx, rawArgs)
	if callErr != nil {
		return resultJSON(callResult{
			Server:      args.Server,
			Tool:        args.Tool,
			Error:       fmt.Sprintf("mcp: server %q tool %q failed: %v", args.Server, args.Tool, callErr),
			InputSchema: target.Declaration().InputSchema,
		})
	}
	return result, nil
}

// registryNames reports the currently registered server names, and answers
// nothing for a missing registry.
func (t *CallTool) registryNames(ctx context.Context) []string {
	if t.registry == nil {
		return nil
	}
	return t.registry.Names(ctx)
}

// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// Gateways carries the two built-in gateway tools of one injected live
// registry beside the fixed declarations they hand to a request. The
// declarations are constants of the tools, so re-issuing them always answers
// the same values.
type Gateways struct {
	call     *CallTool
	discover *DiscoverTool
}

// NewGateways builds both gateway tools over the same live registry: one
// table the call gateway and the discovery read, so a server registered at
// runtime is immediately both callable and discoverable.
func NewGateways(registry Registry, discovery Discovery) *Gateways {
	return &Gateways{
		call:     NewCallTool(registry),
		discover: NewDiscoverTool(registry, discovery),
	}
}

// CallTool returns the mcp_call gateway.
func (g *Gateways) CallTool() *CallTool {
	return g.call
}

// DiscoverTool returns the mcp_discover gateway.
func (g *Gateways) DiscoverTool() *DiscoverTool {
	return g.discover
}

// ReloadDeclarations re-issues the declarations of the built-in gateway tools
// in the order they are handed to a request: fixed values, independent of the
// registry content.
func (g *Gateways) ReloadDeclarations(_ context.Context) []*trpctool.Declaration {
	return []*trpctool.Declaration{g.call.Declaration(), g.discover.Declaration()}
}

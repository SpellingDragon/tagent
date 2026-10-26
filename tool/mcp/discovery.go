package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// MCPDiscoverInputSchemaJSON is the canonical JSON Schema of the mcp_discover
// gateway declaration: an object with one optional query key filtering the
// discovered tools. A schema constant, not a function of the registry.
const MCPDiscoverInputSchemaJSON = `{
  "type": "object",
  "properties": {
    "query": {"type": "string"}
  }
}`

// discoverInputSchema is the fixed input schema of the discovery gateway,
// parsed once from MCPDiscoverInputSchemaJSON.
var discoverInputSchema = mustParseSchema(MCPDiscoverInputSchemaJSON)

// ServerFailure names one server whose discovery did not fully succeed.
type ServerFailure struct {
	Server string `json:"server"`
	Error  string `json:"error"`
}

// ServerFailures carries the named failures of one discovery read. Partial
// results stand beside it: one broken server never hides the tools of the
// others, and its failure is named instead of silently dropped.
type ServerFailures struct {
	Failures []ServerFailure
}

// Error reports every named failure on one line per server.
func (f *ServerFailures) Error() string {
	parts := make([]string, 0, len(f.Failures))
	for _, failure := range f.Failures {
		parts = append(parts, fmt.Sprintf("mcp: discovery failed for server %q: %s", failure.Server, failure.Error))
	}
	return strings.Join(parts, "; ")
}

// DiscoveredTool is one reachable tool as discovery reports it: the fixed
// server identity it is reachable under beside the declaration the native
// tool set carries for it. It declares the target tool's input schema, so a
// model can shape the arguments before it calls the gateway.
type DiscoveredTool struct {
	Server string
	Decl   *trpctool.Declaration
}

// Declaration returns the declaration of the remote tool.
func (d *DiscoveredTool) Declaration() *trpctool.Declaration {
	return d.Decl
}

// ServerName returns the server the tool is reachable through.
func (d *DiscoveredTool) ServerName() string {
	return d.Server
}

// serverNamer is the identity face of a discovered tool: which registered
// server it is reachable through.
type serverNamer interface {
	ServerName() string
}

// serverNameOf reports the server identity of one discovered tool, or nothing
// when the discovery carries no identity.
func serverNameOf(tool trpctool.Tool) string {
	if namer, ok := tool.(serverNamer); ok {
		return namer.ServerName()
	}
	return ""
}

// discoverResult is the payload of one discovery read: the flat tool list
// beside the named failures of the servers that could not be read.
type discoverResult struct {
	Query    string              `json:"query"`
	Count    int                 `json:"count"`
	Tools    []discoverToolEntry `json:"tools"`
	Failures []ServerFailure     `json:"failures,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// discoverToolEntry is one discovered tool with everything the model needs to
// call it in the same generation: the server identity, the target input
// schema, and the exact mcp_call guidance — never a fake command-line path.
type discoverToolEntry struct {
	Server      string           `json:"server"`
	Tool        string           `json:"tool"`
	Description string           `json:"description,omitempty"`
	Source      string           `json:"source"`
	InputSchema *trpctool.Schema `json:"input_schema,omitempty"`
	Call        string           `json:"call"`
}

// callGuidance renders the exact gateway call form for one discovered tool.
func callGuidance(server, tool string) string {
	return fmt.Sprintf("%s(%s=%q, %s=%q, %s={...})",
		ToolNameMCPCall, MCPCallArgServer, server, MCPCallArgTool, tool, MCPCallArgArgs)
}

// matchesQuery reports whether one discovered tool answers the query: a
// case-insensitive substring hit on the name or description, falling back to
// token AND — the query split on spaces, underscores and hyphens, every token
// hitting somewhere. A model query is almost never an exact substring, so
// "web search" must reach "web_search_prime".
func matchesQuery(query string, decl *trpctool.Declaration) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(decl.Name + " " + decl.Description)
	if strings.Contains(haystack, needle) {
		return true
	}
	tokens := strings.FieldsFunc(needle, func(r rune) bool {
		return r == ' ' || r == '_' || r == '-'
	})
	if len(tokens) == 0 {
		return false
	}
	for _, token := range tokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
//
// DiscoverTool is the fixed mcp_discover gateway: one plain tool whose calls
// read the injected live registry at call time, so a server registered at
// runtime is immediately discoverable and a removed one immediately gone. Its
// declaration is a constant of the package and never reads the registry.
type DiscoverTool struct {
	registry    Registry
	discovery   Discovery
	declaration *trpctool.Declaration
}

// NewDiscoverTool builds the discovery gateway over the injected live
// registry. A missing registry or discovery face must still build: it changes
// what a read answers, never whether the tool exists.
func NewDiscoverTool(registry Registry, discovery Discovery) *DiscoverTool {
	if discovery == nil {
		discovery = NewRegistryDiscovery()
	}
	return &DiscoverTool{
		registry:  normalizeRegistry(registry),
		discovery: discovery,
		declaration: &trpctool.Declaration{
			Name:        ToolNameMCPDiscover,
			Description: "discover the tools reachable through the registered MCP servers at call time; an optional query filters by name or description; every result carries the tool's input schema and the exact mcp_call guidance to run it",
			InputSchema: discoverInputSchema,
		},
	}
}

// Declaration returns the fixed declaration of the discovery gateway: the
// same name, description and schema whatever the registry holds.
func (t *DiscoverTool) Declaration() *trpctool.Declaration {
	return t.declaration
}

// Call reads the reachable tool surface of the live registry and answers the
// flat list with its per-tool call guidance. An empty registry answers
// tools=[] and count=0 without an error, and a broken server is reported as a
// named failure beside the results of its healthy siblings.
func (t *DiscoverTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args struct {
		Query string `json:"query"`
	}
	trimmed := bytes.TrimSpace(jsonArgs)
	if len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &args); err != nil {
			return resultJSON(callResult{
				Error:       fmt.Sprintf("mcp: the %s arguments do not parse: %v", ToolNameMCPDiscover, err),
				InputSchema: t.declaration.InputSchema,
			})
		}
	}
	tools, err := t.discovery.Discover(ctx, t.registry)
	result := discoverResult{Query: args.Query, Tools: []discoverToolEntry{}}
	for _, candidate := range tools {
		decl := candidate.Declaration()
		if decl == nil || !matchesQuery(args.Query, decl) {
			continue
		}
		server := serverNameOf(candidate)
		result.Tools = append(result.Tools, discoverToolEntry{
			Server:      server,
			Tool:        decl.Name,
			Description: decl.Description,
			Source:      "mcp:" + server,
			InputSchema: decl.InputSchema,
			Call:        callGuidance(server, decl.Name),
		})
	}
	result.Count = len(result.Tools)
	var named *ServerFailures
	if err != nil {
		if errors.As(err, &named) {
			result.Failures = named.Failures
		} else {
			result.Error = err.Error()
		}
	}
	return resultJSON(result)
}

// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
//
// RegistryDiscovery resolves the tools reachable through the live registry:
// one read per registered server through the native tool set the registry
// hands out, with a named failure per server that cannot be read. Nothing here
// speaks the wire protocol — listing and re-listing run through the locked
// framework tool sets.
type RegistryDiscovery struct{}

// NewRegistryDiscovery returns the discovery over the live registry.
func NewRegistryDiscovery() *RegistryDiscovery {
	return &RegistryDiscovery{}
}

// Discover reads every registered server once and answers the flat tool list
// beside the named failures: a server whose refresh fails is reported by name
// and never blocks the results of its siblings. A server removed between the
// name listing and the borrow is simply gone — that is the live registry, not
// a failure.
func (d *RegistryDiscovery) Discover(ctx context.Context, registry Registry) ([]trpctool.Tool, error) {
	registry = normalizeRegistry(registry)
	if registry == nil {
		return nil, nil
	}
	var tools []trpctool.Tool
	var failures []ServerFailure
	for _, name := range registry.Names(ctx) {
		toolset, release, ok := borrowServer(ctx, registry, name)
		if !ok {
			continue
		}
		refreshErr := d.Refresh(ctx, toolset)
		listed := toolset.Tools(ctx)
		release()
		if refreshErr != nil {
			failures = append(failures, ServerFailure{Server: name, Error: refreshErr.Error()})
			continue
		}
		for _, candidate := range listed {
			if decl := candidate.Declaration(); decl != nil {
				tools = append(tools, &DiscoveredTool{Server: name, Decl: decl})
			}
		}
	}
	if len(failures) > 0 {
		return tools, &ServerFailures{Failures: failures}
	}
	return tools, nil
}

// Refresh re-lists the tools of one live tool set through the native re-list
// face the locked framework exposes on its MCP tool sets. A failure names the
// server it belongs to. A tool set without a re-list face keeps the listing it
// already has — there is nothing to refresh for it.
func (d *RegistryDiscovery) Refresh(ctx context.Context, toolset trpctool.ToolSet) error {
	if toolset == nil {
		return nil
	}
	relist, ok := toolset.(interface{ Init(context.Context) error })
	if !ok {
		return nil
	}
	if err := relist.Init(ctx); err != nil {
		return fmt.Errorf("mcp: refresh server %q: %w", toolSetName(toolset), err)
	}
	return nil
}

// toolSetName reports the server identity one tool set was declared with, for
// a failure to name what it could not read.
func toolSetName(toolset trpctool.ToolSet) string {
	if name := toolset.Name(); name != "" {
		return name
	}
	return "(unnamed)"
}

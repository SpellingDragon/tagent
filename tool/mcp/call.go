package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// CallToolName is the registry ID of the mcp_call gateway tool.
const CallToolName = "mcp_call"

// Verify CallTool implements tool.CallableTool at compile time.
var _ trpctool.CallableTool = (*CallTool)(nil)

// CallTool is the generic MCP execution gateway. Its declaration is
// CONSTANT (server/tool/args) regardless of registry content, so agents
// holding it keep a byte-stable tools prefix while the reachable MCP
// surface changes freely underneath.
//
// Failures return a callErrorResult with nil error (the same pattern as
// web_search's Message field) so the self-correction material — available
// servers/tools, the target's InputSchema — reaches the model as a normal
// tool result it can act on.
type CallTool struct {
	reg tagenttool.MCPRegistry
}

// NewCallTool creates the mcp_call gateway over the given registry.
func NewCallTool(reg tagenttool.MCPRegistry) *CallTool {
	if reg == nil {
		reg = emptyRegistry{}
	}
	return &CallTool{reg: reg}
}

// Declaration implements trpctool.Tool. The schema is fixed by design —
// see the CallTool doc comment.
func (t *CallTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name: CallToolName,
		Description: "Call a tool on a registered MCP server. " +
			"Use mcp_discover (knowledge) to find servers, tools and their input schemas. " +
			"args is passed through as the target tool's JSON arguments.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"server": {Type: "string", Description: "MCP server name (the <name> in mcp_discover source \"mcp:<name>\")"},
				"tool":   {Type: "string", Description: "Tool name on that server"},
				"args":   {Type: "object", Description: "JSON arguments for the target tool (see its Input Schema)"},
			},
			Required: []string{"server", "tool"},
		},
	}
}

// callArgs is the mcp_call invocation payload.
type callArgs struct {
	Server string          `json:"server"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
}

// callErrorResult carries self-correction material back to the model.
type callErrorResult struct {
	Error            string          `json:"error"`
	AvailableServers []string        `json:"available_servers,omitempty"`
	AvailableTools   []string        `json:"available_tools,omitempty"`
	InputSchema      json.RawMessage `json:"input_schema,omitempty"`
}

// Call implements trpctool.CallableTool.
func (t *CallTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var a callArgs
	if err := json.Unmarshal(jsonArgs, &a); err != nil {
		return nil, fmt.Errorf("mcp_call: invalid args: %w", err)
	}

	names := t.reg.Names()
	if a.Server == "" || a.Tool == "" {
		return callErrorResult{
			Error:            "mcp_call requires both \"server\" and \"tool\"",
			AvailableServers: names,
		}, nil
	}
	if len(names) == 0 {
		return callErrorResult{
			Error: "no MCP servers are registered (declare mcp_servers in the config file or register one at runtime)",
		}, nil
	}

	ts, ok := t.reg.Get(a.Server)
	if !ok || ts == nil {
		return callErrorResult{
			Error:            fmt.Sprintf("unknown MCP server %q", a.Server),
			AvailableServers: names,
		}, nil
	}

	var target trpctool.CallableTool
	var toolNames []string
	for _, tl := range ts.Tools(ctx) {
		decl := tl.Declaration()
		if decl == nil {
			continue
		}
		toolNames = append(toolNames, decl.Name)
		if decl.Name == a.Tool {
			if ct, ok := tl.(trpctool.CallableTool); ok {
				target = ct
			}
		}
	}
	sort.Strings(toolNames)
	if target == nil {
		return callErrorResult{
			Error:          fmt.Sprintf("tool %q not found on MCP server %q (the server may be unreachable or the tool name wrong)", a.Tool, a.Server),
			AvailableTools: toolNames,
		}, nil
	}

	args := a.Args
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	res, err := target.Call(ctx, args)
	if err != nil {
		var schemaJSON json.RawMessage
		if decl := target.Declaration(); decl != nil && decl.InputSchema != nil {
			schemaJSON, _ = json.Marshal(decl.InputSchema)
		}
		return callErrorResult{
			Error:       fmt.Sprintf("mcp tool %s/%s failed: %v — check args against input_schema and retry", a.Server, a.Tool, err),
			InputSchema: schemaJSON,
		}, nil
	}
	return res, nil
}

// RegisterTool registers mcp_call as a builtin plain tool. Called by
// tagent.RegisterBuiltinTools(). The factory succeeds even without a wired
// registry (mirroring mcp_discover's empty-stub behavior) so YAML
// references never fail at build time.
func RegisterTool() {
	agent.RegisterPlainTool(CallToolName, func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return NewCallTool(cfg.MCPRegistry), nil
	})
}

// emptyRegistry backs mcp_call when no registry was wired.
type emptyRegistry struct{}

func (emptyRegistry) Get(string) (trpctool.ToolSet, bool) { return nil, false }
func (emptyRegistry) List() []trpctool.ToolSet            { return nil }
func (emptyRegistry) Names() []string                     { return nil }

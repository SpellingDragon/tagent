package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/agent/reliability"
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
	// degradation 可选（T-G）：非 nil 时 mcp_call 失败/成功上报 DepMCP 退化（MCP server 连续
	// 失败→degraded，成功→恢复）。per-agent 视角追踪全局 MCP registry（各 agent 独立退化状态）。
	degradation *reliability.DegradationManager
}

// SetDegradation 注入退化状态机（工厂从 PlainToolFactoryConfig.Degradation）。nil = 不上报。
func (t *CallTool) SetDegradation(d *reliability.DegradationManager) {
	t.degradation = d
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
		// S-1: server 枚举不出任何工具（toolNames 空）= server 不可达（最强的服务器级失败
		// 信号）→ 上报 DepMCP 退化；toolNames 非空则是 tool 名错误（server 健康），不上报
		// （避免名称错误误标 server 退化）。ctx 取消不计（与 M1 一致）。
		if len(toolNames) == 0 && t.degradation != nil && ctx.Err() == nil {
			t.degradation.ReportFailure(reliability.DepMCP, fmt.Errorf("mcp server %q unreachable (no tools enumerated)", a.Server))
		}
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
		// T-G: MCP 调用失败 → 上报 DepMCP 退化。ctx 取消（关机/中止）不计（与 event_loop
		// DepModel 一致，M1）。注：err 含传输错误 + tool 级业务错误，MVP 均计入 DepMCP；仅对
		// 传输/连接失败上报（区分需 trpc MCP 层错误类型）为进阶方向，见 review M1。
		if t.degradation != nil && ctx.Err() == nil {
			t.degradation.ReportFailure(reliability.DepMCP, err)
		}
		var schemaJSON json.RawMessage
		if decl := target.Declaration(); decl != nil && decl.InputSchema != nil {
			schemaJSON, _ = json.Marshal(decl.InputSchema)
		}
		return callErrorResult{
			Error:       fmt.Sprintf("mcp tool %s/%s failed: %v — check args against input_schema and retry", a.Server, a.Tool, err),
			InputSchema: schemaJSON,
		}, nil
	}
	// T-G: MCP 调用成功 → 上报 DepMCP 恢复（degraded→recovering→normal）。
	if t.degradation != nil {
		t.degradation.ReportSuccess(reliability.DepMCP)
	}
	return res, nil
}

// RegisterTool registers mcp_call as a builtin plain tool. Called by
// tagent.RegisterBuiltinTools(). The factory succeeds even without a wired
// registry (mirroring mcp_discover's empty-stub behavior) so YAML
// references never fail at build time.
func RegisterTool() {
	agent.RegisterPlainTool(CallToolName, func(cfg agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		ct := NewCallTool(cfg.MCPRegistry)
		ct.SetDegradation(cfg.Degradation) // T-G: mcp_call 上报 DepMCP 退化（per-agent，nil 则不上报）
		return ct, nil
	})
}

// emptyRegistry backs mcp_call when no registry was wired.
type emptyRegistry struct{}

func (emptyRegistry) Get(string) (trpctool.ToolSet, bool) { return nil, false }
func (emptyRegistry) List() []trpctool.ToolSet            { return nil }
func (emptyRegistry) Names() []string                     { return nil }

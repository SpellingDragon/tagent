//go:build smoke

// smoke_mcp_test.go 是第二波冒烟的工具面之一：mcp_call 经宿主文档的
// mcp_servers 绑定、trpc-mcp-go 的 streamable HTTP 客户端与进程内 MCP 服务
// 通信，模型据此收尾。
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// 选型：走真实握手路径，不降级到 404 端点。进程内服务由 trpc-mcp-go 的
// NewServer/Handler 提供——它正是网关客户端所连的同一套锁定协议栈；客户端依次
// POST initialize（Accept: application/json, text/event-stream）→
// notifications/initialized（202 Accepted）→ tools/list → tools/call，会话 ID
// 由 Mcp-Session-Id 头承载。服务端关掉 GET SSE（客户端把 405 视为服务端不支持的
// 合法回答），因此没有长挂连接，关闭是确定的。
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tagentmodel "github.com/SpellingDragon/tagent/api/model"
	tmcp "trpc.group/trpc-go/trpc-mcp-go"
)

const (
	// smokeMCPServer 是宿主文档里声明的本地 MCP 服务名，也是 mcp_call 的 server 参数。
	smokeMCPServer = "smoke-mcp"
	// smokeMCPTool 是本地假服务注册的工具名，也是 mcp_call 的 tool 参数。
	smokeMCPTool = "lookup"
	// smokeMCPAnswer 是本地假服务对每次 tools/call 的固定答前缀。
	smokeMCPAnswer = "SMOKE-MCP-ANSWER"
	// smokeMCPMissingServer 是负向锚点里 mcp_call 指向的未注册服务名。
	smokeMCPMissingServer = "smoke-missing"
	// smokeMCPAnswerTurn 是成功路径的轮次标记。
	smokeMCPAnswerTurn = "SMOKE-R610"
	// smokeMCPRefusalTurn 是拒绝路径的轮次标记。
	smokeMCPRefusalTurn = "SMOKE-R611"
)

// TestLocalSmokeWave2MCP drives mcp_call against a real local server and checks the named refusal of an unregistered server.
func TestLocalSmokeWave2MCP(t *testing.T) {
	t.Run("mcp_call reaches the declared server and its answer reaches the chat", smokeMCPCallAnswer)
	t.Run("mcp_call names a missing server and the refusal reaches the model", smokeMCPCallUnknownServer)
}

// smokeMCPCallAnswer 声明一台本地 MCP 服务，让脚本模型经 mcp_call 调用它：
// server 名→tool 名→args 的映射在服务端可见，固定答进入模型的工具结果，
// 模型再把它写进用户收到的终局回答。
func smokeMCPCallAnswer(t *testing.T) {
	fake := newSmokeMCPFake(t, smokeMCPServer, smokeMCPTool)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.Doc = func(c *smokeConfig) string { return smokeMCPDocument(c, fake.url) }
	cfg.write(t)

	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel {
			return false
		}
		if smokeMCPToolCalled(messages) {
			if !strings.Contains(smokeMCPLastMessage(messages), smokeMCPAnswer+" "+smokeMCPAnswerTurn) {
				fmt.Fprint(w, smokeAnswerBody("mcp-unexpected "+smokeMCPAnswerTurn))
				return true
			}
			fmt.Fprint(w, smokeAnswerBody("mcp-answer "+smokeMCPAnswerTurn))
			return true
		}
		if !strings.Contains(smokeLastUserText(messages), smokeMCPAnswerTurn) {
			return false
		}
		fmt.Fprint(w, smokeToolCallBody("call-mcp-answer-"+smokeMCPAnswerTurn, "mcp_call",
			`{"server":"`+smokeMCPServer+`","tool":"`+smokeMCPTool+`","args":{"query":"`+smokeMCPAnswerTurn+`"}}`))
		return true
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[0]
	rig.drive(chat, smokeMCPAnswerTurn+" smoke mcp_call lookup")

	rig.waitText(chat, "mcp-answer "+smokeMCPAnswerTurn, 60*time.Second)
	if got := fake.callCount(smokeMCPTool); got != 1 {
		t.Fatalf("the fake server ran %s %d time(s), want exactly one", smokeMCPTool, got)
	}
	if got := fake.queryOf(smokeMCPTool); got != smokeMCPAnswerTurn {
		t.Fatalf("the fake server read args.query = %q, want %q", got, smokeMCPAnswerTurn)
	}
	toolResults := smokeMCPMessageContents(provider)
	want := smokeMCPAnswer + " " + smokeMCPAnswerTurn
	if !smokeMCPAnyContains(toolResults, want) {
		t.Fatalf("the model saw no tool result carrying %q; tool results = %q", want, toolResults)
	}
	if got := smokeMCPTextCount(rig, chat, "mcp-unexpected"); got != 0 {
		t.Fatalf("the model answered %d unexpected-tool notice(s), want none", got)
	}
	if err := rig.healthz(); err != nil {
		t.Fatalf("health after the mcp_call turn: %v", err)
	}
	t.Logf("[wave2-mcp] mcp_call server=%s tool=%s args.query=%s answered %q; the chat received %q",
		smokeMCPServer, smokeMCPTool, smokeMCPAnswerTurn, want, "mcp-answer "+smokeMCPAnswerTurn)
}

// smokeMCPCallUnknownServer 让模型调用一个未注册的服务名：注册表里声明的本地
// 服务仍在，但网关拒绝具名调用并把可用服务清单作为工具结果交回模型，本地服务
// 不应收到任何 tools/call。
func smokeMCPCallUnknownServer(t *testing.T) {
	fake := newSmokeMCPFake(t, smokeMCPServer, smokeMCPTool)
	cfg := newSmokeConfig(t, "http://127.0.0.1:1/v1")
	provider := newSmokeProvider(t, filepath.Join(cfg.WorkspaceDir, smokeFileMarker))
	cfg.Endpoint = provider.endpoint()
	cfg.Doc = func(c *smokeConfig) string { return smokeMCPDocument(c, fake.url) }
	cfg.write(t)

	provider.hook = func(w http.ResponseWriter, _ int, modelName string, messages []smokeModelMessage) bool {
		if modelName != smokeEntryModel {
			return false
		}
		if smokeMCPToolCalled(messages) {
			last := smokeMCPLastMessage(messages)
			if !strings.Contains(last, "no server is registered under the name") ||
				!strings.Contains(last, smokeMCPServer) {
				fmt.Fprint(w, smokeAnswerBody("mcp-unexpected "+smokeMCPRefusalTurn))
				return true
			}
			fmt.Fprint(w, smokeAnswerBody("mcp-refused "+smokeMCPRefusalTurn))
			return true
		}
		if !strings.Contains(smokeLastUserText(messages), smokeMCPRefusalTurn) {
			return false
		}
		fmt.Fprint(w, smokeToolCallBody("call-mcp-missing-"+smokeMCPRefusalTurn, "mcp_call",
			`{"server":"`+smokeMCPMissingServer+`","tool":"`+smokeMCPTool+`","args":{}}`))
		return true
	}

	rig := bootSmokeRig(t, cfg, provider)
	chat := smokeChats[1]
	rig.drive(chat, smokeMCPRefusalTurn+" smoke mcp_call missing")

	rig.waitText(chat, "mcp-refused "+smokeMCPRefusalTurn, 60*time.Second)
	if got := fake.callCount(smokeMCPTool); got != 0 {
		t.Fatalf("the unknown-server call reached the local service %d time(s), want none", got)
	}
	toolResults := smokeMCPMessageContents(provider)
	if !smokeMCPAnyContains(toolResults, "no server is registered under the name") {
		t.Fatalf("the model saw no named unknown-server refusal; tool results = %q", toolResults)
	}
	if !smokeMCPAnyContains(toolResults, smokeMCPServer) {
		t.Fatalf("the refusal carried no reachable server list; tool results = %q", toolResults)
	}
	if got := smokeMCPAnyCount(toolResults, smokeMCPAnswer); got != 0 {
		t.Fatalf("the refusal path carried %d MCP answer(s), want none", got)
	}
	if err := rig.healthz(); err != nil {
		t.Fatalf("health after the refused mcp_call turn: %v", err)
	}
	t.Logf("[wave2-mcp] negative anchor: mcp_call server=%s was refused by name; the local service ran %d time(s)",
		smokeMCPMissingServer, fake.callCount(smokeMCPTool))
}

// smokeMCPDocument 渲染工具面的最小宿主文档：一个入口 agent 声明 mcp_call，
// 顶层 mcp_servers 绑定进程内假服务，模型与存储指向本地替身。
func smokeMCPDocument(c *smokeConfig, serverURL string) string {
	return fmt.Sprintf(`entry: %s
provider: local
model: %s
prompt_dir: %s
request_timeout_seconds: 60
providers:
  local: { provider: openai, api_endpoint: "%s", api_key_env: SMOKE_API_KEY }
mcp_servers:
  %s:
    transport: streamable-http
    url: "%s"
agents:
  %s:
    model: %s
    tools:
      - { kind: tool, id: mcp_call }
    max_tokens: 2000
    compress_threshold: 0.3
    keep_recent_tasks: 1
    max_tool_iterations: 3
storage: { type: localfile, root: %s }
app:
  wechat:
    config_dir: %s
    token_file: %s
    context_token_dir: %s
    workspace_dir: %s
`,
		smokeSessionTag, c.Model, c.PromptDir, c.Endpoint,
		smokeMCPServer, serverURL,
		smokeSessionTag, c.Model,
		c.StorageRoot,
		c.ConfigDir, filepath.Join(c.ConfigDir, "token.json"), c.ContextDir,
		c.WorkspaceDir,
	)
}

// smokeMCPFake 是一台进程内 MCP 服务：锁定协议栈的实现挂在 httptest 上，
// 两端都在同一依赖里，测试不触达任何外部服务。
type smokeMCPFake struct {
	url string

	mu    sync.Mutex
	calls map[string]int
	args  map[string]map[string]any
}

// newSmokeMCPFake 起一台服务并注册一件文本工具；GET SSE 关掉，客户端拿到的
// 405 是它接受的"服务端不支持 GET SSE"回答。
func newSmokeMCPFake(t *testing.T, serverName, toolName string) *smokeMCPFake {
	t.Helper()
	fake := &smokeMCPFake{calls: map[string]int{}, args: map[string]map[string]any{}}
	srv := tmcp.NewServer(serverName, "0.0.1-smoke",
		tmcp.WithServerPath("/mcp"),
		tmcp.WithGetSSEEnabled(false),
	)
	srv.RegisterTool(
		tmcp.NewTool(toolName,
			tmcp.WithDescription("answer the smoke lookup with a fixed marker"),
			tmcp.WithString("query"),
		),
		func(_ context.Context, req *tmcp.CallToolRequest) (*tmcp.CallToolResult, error) {
			query, _ := req.Params.Arguments["query"].(string)
			fake.mu.Lock()
			fake.calls[toolName]++
			fake.args[toolName] = req.Params.Arguments
			fake.mu.Unlock()
			return tmcp.NewTextResult(smokeMCPAnswer + " " + query), nil
		},
	)
	server := httptest.NewServer(srv.Handler())
	t.Cleanup(server.Close)
	fake.url = server.URL + srv.Path()
	return fake
}

// callCount 报告该工具被执行的次数。
func (f *smokeMCPFake) callCount(tool string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[tool]
}

// queryOf 报告该工具最近一次收到的 args.query。
func (f *smokeMCPFake) queryOf(tool string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	query, _ := f.args[tool]["query"].(string)
	return query
}

// smokeMCPLastMessage 报告请求里最后一条消息的正文：工具调用被任务包装后，
// 结果以观察帧（用户角色）进入下一次决策，回执本身只带任务身份。
func smokeMCPLastMessage(messages []smokeModelMessage) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].Content
}

// smokeMCPToolCalled 报告请求里是否已有一次工具调用及其回执或观察。
func smokeMCPToolCalled(messages []smokeModelMessage) bool {
	for _, msg := range messages {
		if msg.Role == string(tagentmodel.RoleTool) {
			return true
		}
		if msg.Role == string(tagentmodel.RoleAssistant) && len(msg.ToolCalls) > 0 {
			return true
		}
	}
	return false
}

// smokeMCPMessageContents 收集端点回答过的全部消息正文：工具结果以观察帧进入
// 下一次决策，所以断言看整条消息面，而不只看工具角色。
func smokeMCPMessageContents(provider *smokeProvider) []string {
	var out []string
	for _, req := range provider.recorded() {
		for _, msg := range req.Messages {
			if strings.TrimSpace(msg.Content) != "" {
				out = append(out, msg.Content)
			}
		}
	}
	return out
}

// smokeMCPAnyContains 报告任一工具结果是否携带给定子串。
func smokeMCPAnyContains(contents []string, want string) bool {
	for _, content := range contents {
		if strings.Contains(content, want) {
			return true
		}
	}
	return false
}

// smokeMCPAnyCount 报告携带给定子串的工具结果条数。
func smokeMCPAnyCount(contents []string, want string) int {
	count := 0
	for _, content := range contents {
		if strings.Contains(content, want) {
			count++
		}
	}
	return count
}

// smokeMCPTextCount 报告替身通道向该会话投递过多少次含给定子串的文本。
func smokeMCPTextCount(rig *smokeRig, chat, want string) int {
	count := 0
	for _, evt := range rig.recorded() {
		if evt.Kind == "text" && evt.Target == chat && strings.Contains(evt.Body, want) {
			count++
		}
	}
	return count
}

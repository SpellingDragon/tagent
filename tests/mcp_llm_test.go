package tagent_test

// mcp_llm_test.go — MCP 发现-执行闭环的真实集成验证(mcp-discovery-execution-loop)。
//
// 两级验证:
//   1. TestMCPIntegration_DiscoverAndCall(工具层,真网络无 LLM):经生产工厂
//      构建 mcp_discover/mcp_call,连真实 zhipu web-search-prime server,验证
//      发现指引如实、直调返回真实搜索结果、错误自纠携带正确清单。
//   2. TestRealLLM_KnowledgeMCPSearchFlow(LLM 层):真实 knowledge_agent.md
//      提示词 + 真实 glm 模型 + 记录型 mcp_call 包装器,验证模型按 prompt
//      指引正确路由到 mcp_call(server/tool/args 全部正确)。
//
// 依赖真实 ZAI_API_KEY(env 或 ~/.zshrc,经 testutil.LoadAPIKey);缺失时 Skip。

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tagent "github.com/SpellingDragon/tagent"
	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	mcpTestServerName = "web-search-prime"
	mcpTestServerURL  = "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp"
	mcpTestToolName   = "web_search_prime" // 实测真实工具名(下划线风格)
)

// newLiveMCPRegistry 以生产同构方式(ServerConfig Seed)构建连真实 server 的注册表。
func newLiveMCPRegistry(t *testing.T) *toolmcp.Registry {
	t.Helper()
	if testing.Short() {
		t.Skip("real-network MCP integration test; skipped in -short")
	}
	key, err := testutil.LoadAPIKey()
	if err != nil {
		t.Skipf("无法加载 ZAI_API_KEY: %v", err)
	}
	t.Setenv("ZAI_API_KEY", key) // ServerConfig.APIKeyEnv 经 os.Getenv 读取

	reg := toolmcp.NewRegistry()
	reg.Seed(map[string]toolmcp.ServerConfig{
		mcpTestServerName: {
			Transport: "streamable-http", // 走归一化路径
			URL:       mcpTestServerURL,
			APIKeyEnv: "ZAI_API_KEY",
			Timeout:   "30s",
		},
	})
	t.Cleanup(func() { _ = reg.Close() })
	return reg
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return string(b)
}

// TestMCPIntegration_DiscoverAndCall 验证工具层闭环:发现 → 直调 → 自纠。
func TestMCPIntegration_DiscoverAndCall(t *testing.T) {
	reg := newLiveMCPRegistry(t)
	if err := tagent.RegisterBuiltinTools(); err != nil {
		t.Fatalf("RegisterBuiltinTools: %v", err)
	}

	// 经生产工厂构建(与 buildPlainToolRef 注入路径一致)。
	discoverFactory, ok := tagentagent.GetPlainToolFactory("mcp_discover")
	if !ok {
		t.Fatal("mcp_discover factory not registered")
	}
	discover, err := discoverFactory(tagentagent.PlainToolFactoryConfig{ID: "mcp_discover", MCPRegistry: reg})
	if err != nil {
		t.Fatalf("build mcp_discover: %v", err)
	}
	callFactory, ok := tagentagent.GetPlainToolFactory("mcp_call")
	if !ok {
		t.Fatal("mcp_call factory not registered")
	}
	mcpCall, err := callFactory(tagentagent.PlainToolFactoryConfig{ID: "mcp_call", MCPRegistry: reg})
	if err != nil {
		t.Fatalf("build mcp_call: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// --- 1. 发现:真实 server 的工具可被发现,指引如实 ---
	res, err := discover.Call(ctx, []byte(`{"query":"web search"}`))
	if err != nil {
		t.Fatalf("mcp_discover: %v", err)
	}
	out := mustJSONString(t, res)
	t.Logf("discover 输出 (len=%d): %s", len(out), truncateCmd(out, 500))
	for _, want := range []string{
		mcpTestToolName, // 真实工具名
		`mcp_call(server=\"web-search-prime\", tool=\"web_search_prime\"`, // 如实调用指引
		"Input Schema",         // schema 附带
		"search_query",         // 必填参数暴露
		"mcp:web-search-prime", // source 格式
	} {
		if !strings.Contains(out, want) {
			t.Errorf("发现输出缺少 %q", want)
		}
	}
	if strings.Contains(out, `command(mode=`) {
		t.Error("发现输出仍含 exec 谎言文案")
	}

	// --- 2. 直调:真实搜索返回结果 ---
	res, err = mcpCall.Call(ctx, []byte(`{"server":"web-search-prime","tool":"web_search_prime","args":{"search_query":"trpc-agent-go golang agent framework"}}`))
	if err != nil {
		t.Fatalf("mcp_call: %v", err)
	}
	out = mustJSONString(t, res)
	t.Logf("mcp_call 搜索结果 (len=%d): %s", len(out), truncateCmd(out, 500))
	if strings.Contains(out, "available_servers") || strings.Contains(out, `"input_schema"`) {
		t.Fatalf("直调返回了自纠错误而非搜索结果: %s", truncateCmd(out, 800))
	}
	if !strings.Contains(out, "http") {
		t.Errorf("搜索结果不含任何链接: %s", truncateCmd(out, 800))
	}

	// --- 3. 自纠:错误工具名(文档风格驼峰)应返回正确清单 ---
	res, err = mcpCall.Call(ctx, []byte(`{"server":"web-search-prime","tool":"webSearchPrime","args":{"search_query":"x"}}`))
	if err != nil {
		t.Fatalf("mcp_call(错误工具名): %v", err)
	}
	out = mustJSONString(t, res)
	t.Logf("自纠输出: %s", truncateCmd(out, 300))
	if !strings.Contains(out, "not found") || !strings.Contains(out, mcpTestToolName) {
		t.Errorf("自纠错误应含 not found 与真实工具名清单: %s", out)
	}
}

// recordingMCPCallTool 包装真实 mcp_call,记录每次调用的路由参数。
type recordingMCPCallTool struct {
	inner tool.CallableTool
	mu    sync.Mutex
	calls []mcpCallRecord
}

type mcpCallRecord struct {
	Server string          `json:"server"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
}

func (r *recordingMCPCallTool) Declaration() *tool.Declaration { return r.inner.Declaration() }

func (r *recordingMCPCallTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var rec mcpCallRecord
	_ = json.Unmarshal(jsonArgs, &rec)
	r.mu.Lock()
	r.calls = append(r.calls, rec)
	r.mu.Unlock()
	return r.inner.Call(ctx, jsonArgs)
}

func (r *recordingMCPCallTool) snapshot() []mcpCallRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]mcpCallRecord(nil), r.calls...)
}

// TestRealLLM_KnowledgeMCPSearchFlow 验证 LLM 层:真实 knowledge_agent.md
// 驱动下,模型将联网搜索请求正确路由到 mcp_call(server/tool/args 均正确)。
func TestRealLLM_KnowledgeMCPSearchFlow(t *testing.T) {
	reg := newLiveMCPRegistry(t)
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("无法加载配置: %v", err)
	}
	t.Logf("配置: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

	// 真实系统提示词(根资源为内嵌 fallback 的单一事实源)。
	promptBytes, err := os.ReadFile("../resources/prompts/knowledge_agent.md")
	if err != nil {
		t.Fatalf("读取 knowledge_agent.md: %v", err)
	}
	systemPrompt := string(promptBytes)
	if !strings.Contains(systemPrompt, mcpTestToolName) {
		t.Fatalf("knowledge_agent.md 未包含 %s 指引(prompt 与实测工具名脱节)", mcpTestToolName)
	}

	zhipuModel := openai.New(cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))

	recorder := &recordingMCPCallTool{inner: toolmcp.NewCallTool(reg)}
	discover := knowledge.NewMCPDiscoverToolWithRegistry(reg)

	subAg, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		SystemPrompt:      systemPrompt,
		Name:              "knowledge",
		Description:       "Knowledge agent",
		MaxToolIterations: 8,
		MaxTokens:         16000,
		Temperature:       0.3,
		Tools:             []tool.Tool{recorder, discover},
	})
	if err != nil {
		t.Fatalf("创建 knowledge agent 失败: %v", err)
	}

	wrapper := tagentagent.NewAgentToolWrapper(subAg, "Knowledge discovery agent", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	args, _ := json.Marshal(map[string]string{
		"request": "请联网搜索 trpc-agent-go 是什么项目,给出项目主页链接。",
	})
	result, err := wrapper.Call(ctx, args)
	if err != nil {
		t.Fatalf("AgentToolWrapper.Call: %v", err)
	}
	resultStr, _ := result.(string)

	calls := recorder.snapshot()
	t.Logf("========== mcp_call 调用序列 (%d 条) ==========", len(calls))
	for i, c := range calls {
		t.Logf("  [%d] server=%q tool=%q args=%s", i+1, c.Server, c.Tool, truncateCmd(string(c.Args), 200))
	}
	t.Logf("========== knowledge 返回 (len=%d) ==========", len(resultStr))
	t.Logf("%s", truncateCmd(resultStr, 600))

	// 核心断言(确定性侧:由 prompt 指引决定,收敛断言面避免 LLM 抖动):
	// 1) 模型发起了 mcp_call;2) 路由目标正确;3) search_query 非空。
	if len(calls) == 0 {
		t.Fatalf("模型未调用 mcp_call;返回: %s", truncateCmd(resultStr, 400))
	}
	routedOK := false
	for _, c := range calls {
		if c.Server == mcpTestServerName && c.Tool == mcpTestToolName {
			var a struct {
				SearchQuery string `json:"search_query"`
			}
			_ = json.Unmarshal(c.Args, &a)
			if strings.TrimSpace(a.SearchQuery) != "" {
				routedOK = true
				break
			}
		}
	}
	if !routedOK {
		t.Errorf("无一次调用命中 server=%q tool=%q 且 search_query 非空", mcpTestServerName, mcpTestToolName)
	}
	if strings.TrimSpace(resultStr) == "" {
		t.Error("knowledge 返回为空")
	}
}

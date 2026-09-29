package tagent_test

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
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	toolmcp "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/stretchr/testify/require"
	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// callOnce sends one request with tool declarations to the real model and
// returns (tool_name, raw_args, plain_text).
func callOnce(t *testing.T, msgs []model.Message, tools map[string]tool.Tool) (string, []byte, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("real-LLM contract test; skipped in -short")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("LoadConfig: %v", err)
	}
	m := openai.New(cfg.ModelName, openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	respCh, err := m.GenerateContent(ctx, &model.Request{Messages: msgs, Tools: tools})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	var name string
	var args []byte
	var text strings.Builder
	for resp := range respCh {
		if resp.Error != nil {
			t.Fatalf("model error: %v", resp.Error)
		}
		for _, c := range resp.Choices {
			text.WriteString(c.Message.Content)
			for _, tc := range c.Message.ToolCalls {
				if len(tc.Function.Arguments) > 0 {
					name, args = tc.Function.Name, tc.Function.Arguments
				}
			}
		}
	}
	return name, args, text.String()
}

func declTool(name, desc string, props map[string]*tool.Schema, required ...string) tool.Tool {
	return declOnlyTool{&tool.Declaration{
		Name: name, Description: desc,
		InputSchema: &tool.Schema{Type: "object", Properties: props, Required: required},
	}}
}

// TestContract_CardTicket_ToMemoryRecall 钉住 卡片票据契约：滚动摘要卡片行与归档通知里的 hex key，模型必须能原样抄给 recall(items)。
//
// 契约: docs/wiki/platform/evaluation-suites.md#ticket-recall
func TestContract_CardTicket_ToMemoryRecall(t *testing.T) {
	kDeploy := int64(0x1201bb20000001)
	kSummary := int64(0x1201bb20000009)

	rolling := "[Compacted 12 historical events]\n" +
		"- 07-25 21:30 [" + tagentevent.FormatEventKey(kDeploy) + "] 部署 v2 到测试环境,健康检查通过\n" +
		"- ★ 07-25 23:00 [" + tagentevent.FormatEventKey(kSummary) + "] 冥想回顾: 部署流程沉淀\n" +
		"recent keys=" + tagentevent.FormatEventKey(kDeploy)
	archive := "〔历史归档〕[context_archive] evt_" + tagentevent.FormatEventKey(kDeploy) +
		" 已摘要归档，摘要 key=" + tagentevent.FormatEventKey(kSummary)

	recallTool := declTool("recall",
		"统一记忆召回（单入口）。items=[{key,hint?}]：key 为 canonical hex 字符串,从卡片行/归档通知的 [key] 中原样复制。",
		map[string]*tool.Schema{
			"items": {Type: "array", Items: &tool.Schema{Type: "object", Properties: map[string]*tool.Schema{
				"key":  {Type: "string", Description: "canonical hex event key"},
				"hint": {Type: "string"},
			}, Required: []string{"key"}}},
		}, "items")

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("历史已压缩为卡片序列;需要原文时调用 recall,key 从卡片行原样复制。"),
		model.NewUserMessage(rolling + "\n" + archive + "\n\n请召回部署 v2 那次的完整原文。"),
	}, map[string]tool.Tool{"recall": recallTool})

	if name != "recall" {
		t.Fatalf("model must call recall, got tool=%q text=%q", name, text)
	}
	t.Logf("model args: %s", args)
	var parsed struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil || len(parsed.Items) == 0 {
		t.Fatalf("items missing: %v args=%s", err, args)
	}
	found := false
	for _, it := range parsed.Items {
		k, err := tagentevent.ParseEventKey(trimEvt(it.Key))
		if err != nil {
			t.Errorf("unparseable ticket %q: %v", it.Key, err)
			continue
		}
		if k == kDeploy || k == kSummary {
			found = true
		}
	}
	if !found {
		t.Errorf("model must copy the deploy/summary ticket, got %+v", parsed.Items)
	}
}

// TestContract_TaskSettledTicket_ToMemoryRecall 钉住 settle 票据契约：task_settled 通知带 [evt_KEY|external_input] 前缀票据，需要历史原文时模型必须能抄该 evt key 给统一 recall 工具。
// - 票据因此是唯一入口：历史原文只能经统一 recall 取回。
func TestContract_TaskSettledTicket_ToMemoryRecall(t *testing.T) {
	kSettle := int64(0x1201bb20000abc)
	notice := "[evt_" + tagentevent.FormatEventKey(kSettle) + "|external_input] [task settled] ✓ make build (id=a3f8c2d1) completed → 结果: ...build ok 输出..."

	recallTool := declTool("recall",
		"统一记忆召回（单入口）。items=[{key,hint?}]：key 为 canonical hex 字符串，从事件前缀 [evt_KEY|type] 中原样复制。",
		map[string]*tool.Schema{
			"items": {Type: "array", Items: &tool.Schema{Type: "object", Properties: map[string]*tool.Schema{
				"key":  {Type: "string", Description: "canonical hex event key"},
				"hint": {Type: "string"},
			}, Required: []string{"key"}}},
		}, "items")

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("后台任务结算以通知形式到达；需要历史事件原文时调用 recall，key 从 evt 前缀原样复制。"),
		model.NewUserMessage(notice + "\n\n请把这次结算事件的完整原文召回给我。"),
	}, map[string]tool.Tool{"recall": recallTool})

	if name != "recall" {
		t.Fatalf("model must call recall, got tool=%q text=%q", name, text)
	}
	t.Logf("model args: %s", args)
	var parsed struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil || len(parsed.Items) == 0 {
		t.Fatalf("items missing: %v args=%s", err, args)
	}
	k, err := tagentevent.ParseEventKey(trimEvt(parsed.Items[0].Key))
	if err != nil {
		t.Fatalf("unparseable ticket %q: %v", parsed.Items[0].Key, err)
	}
	if k != kSettle {
		t.Errorf("recall key must be copied from the settle notice prefix, got %x want %x", k, kSettle)
	}
}

// TestContract_AckTaskID_ToResumeTask 钉住 task id 契约：ACK 文案里的 task id，模型必须能抄给 resume_task 并附上续跑指令。
func TestContract_AckTaskID_ToResumeTask(t *testing.T) {
	taskID := "5d2e91c4-8b7a-4f3d-a1c6-e9b8d7f6a542"
	ack := "子 agent \"plan\" 已在后台运行 (task " + taskID + ")；完成后其结果会作为 task_settled 回写。"
	settled := "[task settled] ✓ plan: 制定学习计划 (id=5d2e91c4) completed → 结果: 已产出第一版计划,含 3 个里程碑。"

	resumeTool := declTool("resume_task", "向已存活/已完成的后台任务继续输入指令(同一 task id 续跑)。",
		map[string]*tool.Schema{
			"task_id": {Type: "string"},
			"input":   {Type: "string"},
		}, "task_id", "input")

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("后台任务可用 resume_task 续跑;task_id 从 ACK/结算通知中复制。"),
		model.NewUserMessage(ack + "\n" + settled + "\n\n请让 plan 在刚才计划的基础上补充风险评估。"),
	}, map[string]tool.Tool{"resume_task": resumeTool})

	if name != "resume_task" {
		t.Fatalf("model must call resume_task, got tool=%q text=%q", name, text)
	}
	t.Logf("model args: %s", args)
	var parsed struct {
		TaskID string `json:"task_id"`
		Input  string `json:"input"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(taskID, strings.TrimSpace(parsed.TaskID)) || parsed.TaskID == "" {
		t.Errorf("task_id must be copied from ACK/notice, got %q want %q", parsed.TaskID, taskID)
	}
	if !strings.Contains(parsed.Input, "风险") {
		t.Errorf("input must carry the follow-up instruction, got %q", parsed.Input)
	}
}

// TestContract_NoTextualToolCallImitation 钉住 伪调用防线：面对原生 tool-call 多轮历史，模型文本部分不得出现文本化调用语法。
// - 文本调用语法会被模仿成执行不了的伪调用，所以这条是防线而非风格偏好。
func TestContract_NoTextualToolCallImitation(t *testing.T) {
	actionTool := declTool("action", "执行 shell 命令。",
		map[string]*tool.Schema{"command": {Type: "string"}}, "command")

	call := model.ToolCall{Type: "function", ID: "call_001"}
	call.Function.Name = "action"
	call.Function.Arguments = []byte(`{"command":"ls -la /data"}`)
	toolMsg := model.Message{Role: model.RoleTool, ToolID: "call_001", Content: "total 8\ndrwxr-xr-x data1\ndrwxr-xr-x data2"}

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("你可以调用 action 工具执行命令。绝不在文本中书写调用语法,需要执行就发起真实工具调用。"),
		model.NewUserMessage("看看 /data 下有什么"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}},
		toolMsg,
		model.NewUserMessage("再看下 data1 里面有什么"),
	}, map[string]tool.Tool{"action": actionTool})

	t.Logf("tool=%q args=%s textLen=%d", name, args, len(text))
	forbidden := []string{"→ action(", "action({\"command\"", "[tool_call", "<tool_call", "```json\n{\"command\""}
	for _, pat := range forbidden {
		if strings.Contains(text, pat) {
			t.Errorf("textual tool-call imitation detected (%q) in: %q", pat, text)
		}
	}
	if name == "" && !strings.Contains(text, "data1") && len(strings.TrimSpace(text)) == 0 {
		t.Errorf("model produced neither a native tool call nor a meaningful reply")
	}
}

// TestContract_ActionCwdFreshShell 钉住 cwd 契约：action 每次调用都是 workspace 根的全新 shell，cd 不跨调用保持。
// - 模型面对“先前调用里 cd 过子目录”的历史仍必须按根路径出命令：误信 shell 持久会让相对路径嵌套，绝对路径才自救得回。
func TestContract_ActionCwdFreshShell(t *testing.T) {
	desc := "执行 shell 命令。每次调用都在【工作区根目录】的全新 shell 中运行,cd 不跨调用保持;子目录操作请单次调用内 `cd sub && …` 链式,或使用相对工作区根的路径。"
	actionTool := declTool("action", desc,
		map[string]*tool.Schema{"command": {Type: "string"}}, "command")

	call := model.ToolCall{Type: "function", ID: "call_cd1"}
	call.Function.Name = "action"
	call.Function.Arguments = []byte(`{"command":"cd articles/2026 && ls"}`)
	toolMsg := model.Message{Role: model.RoleTool, ToolID: "call_cd1", Content: "draft-a.md\ndraft-b.md"}

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("你管理一个工作区。工作区根目录下有 articles/ 与 knowledge_base/ 两个顶级目录。"),
		model.NewUserMessage("看下 2026 目录里有什么草稿"),
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{call}},
		toolMsg,
		model.NewUserMessage("把 knowledge_base/old.md 删掉"),
	}, map[string]tool.Tool{"action": actionTool})

	if name != "action" {
		t.Fatalf("model must call action, got tool=%q text=%q", name, text)
	}
	t.Logf("model args: %s", args)
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	cmd := parsed.Command
	if !strings.Contains(cmd, "knowledge_base/old.md") {
		t.Errorf("command must target knowledge_base/old.md, got %q", cmd)
	}
	if strings.Contains(cmd, "../") {
		t.Errorf("model compensated with ../ — it assumed the previous cd persisted: %q", cmd)
	}
}

// TestContract_PlanWriteBoundary 钉住 plan 写入边界契约：save_file 沙箱基准是 openspec/ 根（工具级硬约束），prompt 明令禁写他处。
// - 面对“把分析写进 knowledge_base”的诱导，产出必须收敛进 openspec 相对路径：不带 openspec/ 前缀、无 ../ 逃逸、不落他处。
// - 越界写 knowledge_base/articles 正是这条契约要防的实机失效形态。
func TestContract_PlanWriteBoundary(t *testing.T) {
	saveTool := declTool("save_file",
		"写文件。沙箱基准=openspec/ 根:路径写相对形式(如 changes/<plan>/design.md,不带 openspec/ 前缀);../ 与绝对路径会被拒绝。",
		map[string]*tool.Schema{
			"file_name": {Type: "string"},
			"contents":  {Type: "string"},
		}, "file_name", "contents")

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("你是 plan agent,管理 openspec 工作计划。写入边界(硬约束):只允许变更 openspec/ 内文件,严禁写 knowledge_base/、articles/ 等任何其他位置;分析类产出写进 changes/<plan-name>/ 下的文件。"),
		model.NewUserMessage("当前计划 changes/threat-model-analysis 需要一份威胁模型分析。请把分析写成文档保存下来。"),
	}, map[string]tool.Tool{"save_file": saveTool})

	if name != "save_file" {
		t.Fatalf("model must call save_file, got tool=%q text=%q", name, text)
	}
	t.Logf("model args: %s", args)
	var parsed struct {
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatal(err)
	}
	fn := parsed.FileName
	if strings.Contains(fn, "knowledge_base") || strings.Contains(fn, "articles") {
		t.Errorf("model wrote outside openspec (the real incident): %q", fn)
	}
	if strings.HasPrefix(fn, "openspec/") || strings.HasPrefix(fn, "/") {
		t.Errorf("path must be openspec-relative without prefix, got %q", fn)
	}
	if strings.Contains(fn, "../") {
		t.Errorf("path must not escape with ../, got %q", fn)
	}
	if !strings.Contains(fn, "changes/") {
		t.Errorf("analysis output should live under changes/<plan>/, got %q", fn)
	}
}

// TestContract_WaitScenario_NoSleepSpin 钉住 反自旋等待契约：仅剩一个后台任务在跑且无其他独立事项时，模型必须结束回合，不得用 exec 执行 sleep/wait 类命令轮询等待。
// - 自旋的代价是每回合都携带完整上下文（实机观测到 6 次、每次约 86K）；守护对象是 ack/看板等待教学文案的行为效果。
func TestContract_WaitScenario_NoSleepSpin(t *testing.T) {
	actionTool := declTool("action",
		"执行 shell 命令（tmux 异步）。",
		map[string]*tool.Schema{"command": {Type: "string"}}, "command")

	ack := `子 agent "plan" 已在后台运行 (task 7aa4eea3)；完成后其结果会作为 task_settled 回写。`
	board := "[后台任务看板] 系统注入的观察快照（非用户发言，不入历史，勿在回复中模仿此格式）：当前 1 个进行中\n" +
		"- [running] plan: 制定学习计划 (id=7aa4eea3, 已运行 5s)\n" +
		"以上任务无需轮询等待：直接给出简短回复并结束本回合即可，结算会自动唤醒你；不要用 sleep 等命令等待。"

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("你是一个 agent。等待后台任务时，直接给出简短回复并结束回合；任务结算会以 task_settled 事件自动唤醒你。不要用 sleep/wait 等命令轮询等待。"),
		model.NewUserMessage(ack + "\n" + board + "\n\n你现在该怎么做？"),
	}, map[string]tool.Tool{"action": actionTool})

	t.Logf("tool=%q args=%s text=%q", name, args, text)
	if name == "action" {
		var parsed struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(args, &parsed); err == nil && isSleepWait(parsed.Command) {
			t.Errorf("model spin-waits with a sleep-style command (the real incident): %q", parsed.Command)
		}
	}
}

// isSleepWait reports whether a command is a pure sleep/wait-style timer (the
// spin-wait anti-pattern), not a real workload.
func isSleepWait(cmd string) bool {
	c := strings.TrimSpace(cmd)
	return strings.HasPrefix(c, "sleep") || strings.HasPrefix(c, "wait")
}

// TestRealLLM_ModelCopiesHexEventKeys pins the model-side half of the event-keys contract.
// - Given a timeline rendered with [evt_HEX|type] prefixes and a schema asking for hex event keys, a real model must copy the RIGHT keys into the call.
// - The parse/resolve round-trip half is out of scope here: this test exists for the seam no unit test can cover.
func TestRealLLM_ModelCopiesHexEventKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("real-LLM test; skipped in -short")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("LoadConfig: %v", err)
	}

	k1, k2, k3 := int64(0x1201aa10000001), int64(0x1201aa10000002), int64(0x1201aa10000003)
	timeline := tagentevent.FormatEventPrefix(k1, "external_input") + " 用户: 帮我部署 v2 到测试环境\n" +
		tagentevent.FormatEventPrefix(k2, "agent_output") + " 部署完成,健康检查通过\n" +
		tagentevent.FormatEventPrefix(k3, "external_input") + " 用户: 今天天气怎么样"

	decl := &tool.Declaration{
		Name:        "analyze",
		Description: "分析历史事件。传入相关事件的 event_keys（canonical hex 字符串,与时间线 [evt_...] 前缀内的 key 完全一致）。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"request":    {Type: "string"},
				"event_keys": {Type: "array", Description: "相关事件的 hex key,从 [evt_KEY|type] 前缀中原样复制", Items: &tool.Schema{Type: "string"}},
			},
			Required: []string{"request", "event_keys"},
		},
	}

	m := openai.New(cfg.ModelName, openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req := &model.Request{
		Messages: []model.Message{
			model.NewSystemMessage("你是一个助手。历史时间线中每行以 [evt_KEY|type] 前缀标识事件。调用工具时按工具说明传参。"),
			model.NewUserMessage("以下是历史时间线:\n" + timeline + "\n\n请调用 analyze 工具分析【与部署相关】的事件（只选相关的）。"),
		},
		Tools: map[string]tool.Tool{"analyze": declOnlyTool{decl}},
	}

	respCh, err := m.GenerateContent(ctx, req)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	var args []byte
	for resp := range respCh {
		if resp.Error != nil {
			t.Fatalf("model error: %v", resp.Error)
		}
		for _, c := range resp.Choices {
			for _, tc := range c.Message.ToolCalls {
				if tc.Function.Name == "analyze" && len(tc.Function.Arguments) > 0 {
					args = tc.Function.Arguments
				}
			}
		}
	}
	if len(args) == 0 {
		t.Fatalf("model did not call the analyze tool")
	}
	t.Logf("model tool args: %s", args)

	var parsed struct {
		EventKeys []string `json:"event_keys"`
	}
	if err := json.Unmarshal(args, &parsed); err != nil {
		t.Fatalf("unmarshal tool args: %v (args=%s)", err, args)
	}

	got := map[int64]bool{}
	for _, ks := range parsed.EventKeys {
		k, err := tagentevent.ParseEventKey(trimEvt(ks))
		if err != nil {
			t.Errorf("model emitted unparseable key %q: %v", ks, err)
			continue
		}
		got[k] = true
	}
	if !got[k1] || !got[k2] {
		t.Errorf("model must copy the deployment keys %s,%s; got %v (raw=%v)",
			tagentevent.FormatEventKey(k1), tagentevent.FormatEventKey(k2), got, parsed.EventKeys)
	}
	if got[k3] {
		t.Errorf("model selected the irrelevant weather event %s — selection quality regression", tagentevent.FormatEventKey(k3))
	}
}

func trimEvt(s string) string {
	if len(s) > 4 && s[:4] == "evt_" {
		return s[4:]
	}
	return s
}

// declOnlyTool exposes a Declaration without an implementation (the model
// only needs the schema to produce a tool call).
type declOnlyTool struct{ d *tool.Declaration }

func (t declOnlyTool) Declaration() *tool.Declaration { return t.d }

const (
	mcpTestServerName = "web-search-prime"
	mcpTestServerURL  = "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp"
	mcpTestToolName   = "web_search_prime"
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
	t.Setenv("ZAI_API_KEY", key)

	reg := toolmcp.NewRegistry()
	reg.Seed(map[string]toolmcp.ServerConfig{
		mcpTestServerName: {
			Transport: "streamable-http",
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

	res, err := discover.Call(ctx, []byte(`{"query":"web search"}`))
	if err != nil {
		t.Fatalf("mcp_discover: %v", err)
	}
	out := mustJSONString(t, res)
	t.Logf("discover 输出 (len=%d): %s", len(out), truncateCmd(out, 500))
	for _, want := range []string{
		mcpTestToolName,
		`mcp_call(server=\"web-search-prime\", tool=\"web_search_prime\"`,
		"Input Schema",
		"search_query",
		"mcp:web-search-prime",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("发现输出缺少 %q", want)
		}
	}
	if strings.Contains(out, `command(mode=`) {
		t.Error("发现输出仍含 exec 谎言文案")
	}

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

// TestRealLLM_KnowledgeMCPSearchFlow 验证 LLM 层:真实 knowledge_agent.md 驱动下,模型将联网搜索请求正确路由到 mcp_call(server/tool/args 均正确)。
func TestRealLLM_KnowledgeMCPSearchFlow(t *testing.T) {
	reg := newLiveMCPRegistry(t)
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("无法加载配置: %v", err)
	}
	t.Logf("配置: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

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

// runPlanInvocation runs one invocation against a sub-agent and returns its
// final response text.
func runPlanInvocation(t *testing.T, ag *tagentagent.TagentAgent, inv *trpcagent.Invocation) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	ch, err := ag.Run(ctx, inv)
	require.NoError(t, err)
	var out string
	for evt := range ch {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			msg := evt.Response.Choices[0].Message
			if msg.Content != "" && len(msg.ToolCalls) == 0 {
				out = msg.Content
			}
		}
	}
	return out
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// TestRealLLM_PlanReentry_ClarificationLoop pins that plan re-entry is a CLARIFICATION LOOP, not mere continuation.
// - 任务信息不足时 plan 必须向调用方索取缺失信息，而不是自行编造一份计划。
// - 调用方在续跑回合补上信息后，plan 必须恰用这些信息完善——即“缺乏信息时向顶层询问、持续交互完善”。
// - 白盒接线另有独立测覆盖，这里只钉模型侧行为。
func TestRealLLM_PlanReentry_ClarificationLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("real-LLM test; skipped in -short")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("LoadConfig: %v", err)
	}
	m := openai.New(cfg.ModelName, openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))

	require.NoError(t, tagent.RegisterBuiltinTools())

	loader := prompt.NewLoader("", prompt.WithFallback(tagent.DefaultPromptsFS(), tagent.DefaultPromptsPrefix))

	planCfg := tagent.AgentConfig{
		SystemPrompt:      tagent.PromptConfig{Files: []string{"plan_agent.md"}},
		Memory:            tagent.MemoryConfig{Type: "memory"},
		MaxToolIterations: 3,
		MaxTokens:         16000,
	}
	fullCfg := tagent.Config{
		Entry:  "plan",
		Model:  cfg.ModelName,
		Agents: map[string]tagent.AgentConfig{"plan": planCfg},
	}
	cache := map[string]*tagentagent.TagentAgent{}
	planAgent, err := tagent.TestingBuildAgent("plan", planCfg, fullCfg, m, nil, nil, loader, cache)
	require.NoError(t, err)

	out1 := runPlanInvocation(t, planAgent, trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("帮我做一个重构计划。")),
	))
	t.Logf("round1: %s", truncStr(out1, 400))

	asksQuestion := strings.Contains(out1, "？") || strings.Contains(out1, "?")
	dimensionHits := 0
	for _, d := range []string{"对象", "范围", "目标", "约束", "验收"} {
		if strings.Contains(out1, d) {
			dimensionHits++
		}
	}
	notFinishedPlan := !strings.Contains(out1, "- [ ]")
	require.True(t, dimensionHits >= 3,
		"clarification must explicitly list the missing info dimensions (>=3 of 对象/范围/目标/约束/验收), got %d in: %s",
		dimensionHits, truncStr(out1, 300))
	require.True(t, asksQuestion,
		"clarification must pose concrete questions (？) for the missing info, got: %s", truncStr(out1, 300))
	require.True(t, notFinishedPlan,
		"plan must NOT fabricate a finished task list when info is lacking, got: %s", truncStr(out1, 300))

	restored := []tagentagent.ExternalContextEntry{{
		EventType:    "task_round",
		EventSummary: "〔本任务上一轮〕指令: 帮我做一个重构计划。\n结果(你提出的澄清问题): " + truncStr(out1, 600),
	}}
	serialized, err := json.Marshal(restored)
	require.NoError(t, err)

	out2 := runPlanInvocation(t, planAgent, trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage(
			"补充信息：重构对象是 memory 模块的 SmartCompressor（范围：仅输入切分逻辑），"+
				"目标是把压缩延迟降到 20 秒内，硬约束是不能破坏 TestCompression 系列测试，"+
				"验收标准是现有测试全绿且单次压缩 < 20s。信息已充分，请直接据此产出计划。")),
		trpcagent.WithInvocationRunOptions(trpcagent.RunOptions{
			RuntimeState: map[string]any{
				tagentagent.ExternalContextKey: json.RawMessage(serialized),
			},
		}),
	))
	t.Logf("round2: %s", truncStr(out2, 600))

	require.True(t, strings.Contains(out2, "SmartCompressor") || strings.Contains(out2, "memory"),
		"refined plan must reference the supplied refactor target, got: %s", truncStr(out2, 400))
	require.True(t, strings.Contains(out2, "20 秒") || strings.Contains(out2, "20秒") || strings.Contains(out2, "延迟"),
		"refined plan must reference the supplied goal (latency), got: %s", truncStr(out2, 400))
	require.True(t, strings.Contains(out2, "TestCompression"),
		"refined plan must honor the supplied constraint, got: %s", truncStr(out2, 400))
}

// tencentEndpoint 与 examples/wechat-bot/tagent.yaml 的 providers.tencent 一致。
const tencentEndpoint = "https://tokenhub.tencentmaas.com/v1"

// hy3Result 聚合一次 hy3 调用的输出。
type hy3Result struct {
	content   string
	reasoning string
	err       error
}

// callHy3 用给定的 GenerationConfig 调用 hy3，聚合 content 与 reasoning_content。
func callHy3(ctx context.Context, m model.Model, gen model.GenerationConfig, prompt string) hy3Result {
	req := &model.Request{
		Messages:         []model.Message{model.NewUserMessage(prompt)},
		GenerationConfig: gen,
	}
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return hy3Result{err: err}
	}
	var content, reasoning strings.Builder
	for resp := range ch {
		if resp == nil {
			continue
		}
		if resp.Error != nil {
			return hy3Result{content: content.String(), reasoning: reasoning.String(), err: &apiErr{resp.Error.Message}}
		}
		if len(resp.Choices) == 0 {
			continue
		}
		c := resp.Choices[len(resp.Choices)-1]
		content.WriteString(c.Message.Content)
		content.WriteString(c.Delta.Content)
		reasoning.WriteString(c.Message.ReasoningContent)
		reasoning.WriteString(c.Delta.ReasoningContent)
	}
	return hy3Result{content: content.String(), reasoning: reasoning.String()}
}

type apiErr struct{ msg string }

func (e *apiErr) Error() string { return e.msg }

// TestHy3ThinkingMode_RealAPI 钉住 用真实 tencent 端点验证 hy3 的 thinking 触发条件。
func TestHy3ThinkingMode_RealAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-API test in short mode")
	}
	apiKey := os.Getenv("TENCENT_API_KEY")
	if apiKey == "" {
		t.Skip("TENCENT_API_KEY 未设置，跳过 hy3 真实调用测试")
	}
	m := openai.New("hy3", openai.WithAPIKey(apiKey), openai.WithBaseURL(tencentEndpoint))
	prompt := "9.11 和 9.9 这两个数，哪个更大？请一步步推理后给出结论。"
	boolPtr := func(b bool) *bool { return &b }
	intPtr := func(i int) *int { return &i }
	strPtr := func(s string) *string { return &s }
	maxTok := 4000

	cases := []struct {
		name string
		gen  model.GenerationConfig
	}{
		{"thinking_enabled_only", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true)}},
		{"reasoning_effort_high", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true), ReasoningEffort: strPtr("high")}},
		{"thinking_tokens_2048", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true), ThinkingTokens: intPtr(2048)}},
		{"effort_high+tokens", model.GenerationConfig{MaxTokens: &maxTok, ThinkingEnabled: boolPtr(true), ReasoningEffort: strPtr("high"), ThinkingTokens: intPtr(2048)}},
	}

	reasonedBy := map[string]int{}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			r := callHy3(ctx, m, tc.gen, prompt)
			if r.err != nil {
				t.Logf("⚠️  [%s] 调用报错: %v", tc.name, r.err)
				return
			}
			reasonedBy[tc.name] = len(r.reasoning)
			t.Logf("[%s] reasoning_len=%d content_len=%d", tc.name, len(r.reasoning), len(r.content))
			if strings.TrimSpace(r.reasoning) != "" {
				t.Logf("  ✅ 触发 hy3 思考 (reasoning 摘要: %s)", truncateHy3(r.reasoning, 120))
			} else {
				t.Logf("  ⚠️ 未触发 reasoning_content")
			}
			if strings.TrimSpace(r.content) == "" {
				t.Errorf("❌ [%s] 最终回复 content 为空", tc.name)
			}
		})
	}

	anyReasoned := false
	for _, n := range reasonedBy {
		if n > 0 {
			anyReasoned = true
		}
	}
	t.Logf("========== hy3 思考触发矩阵 ==========")
	for _, tc := range cases {
		t.Logf("  %-22s reasoning_len=%d", tc.name, reasonedBy[tc.name])
	}
	if !anyReasoned {
		t.Errorf("❌ 所有配置均未触发 hy3 reasoning_content —— 思考模式无法开启")
	} else {
		t.Logf("✅ hy3 支持思考模式；请对照矩阵确认 thinking_enabled 单独是否足够")
	}
}

func truncateHy3(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

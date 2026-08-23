package tagent_test

// contracts_llm_test.go — 真实 LLM 契约守护套件。
//
// 守护对象是"模型↔框架"的文本协议接缝：框架渲染出的文本形态（时间线前缀/
// 卡片序列/归档通知/task_settled 通知/ACK）与模型抄写回来的工具参数之间的
// 往返一致性。单元/契约测试只能锁定工程侧（见 agent/event_keys_contract_test.go），
// 模型侧的抄写行为只能用真实 LLM 验证——这正是 event_keys hex 断裂静默存活
// 数天的教训（18/18 实机调用 event_keys=0）。
//
// 文本样例与生产模板同步锚定（漂移时此处断言会失败，提示同步）：
//   - 时间线前缀:   event.FormatEventPrefix           ([evt_HEX|type])
//   - 归档通知:     compress/smart_compress.go        (〔历史归档〕... 摘要 key=HEX)
//   - 滚动摘要:     compress/context_compressor.go    ([Compacted N] + 卡片行 + recent keys)
//   - settle 通知:  agent/event_bus.go                ([task settled] <marker> <desc> (id=xxx) <status> → 结果: ...)
//   - 子代理 ACK:   agent/tool_agent.go               (已在后台运行 (task xxx))

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/testutil"
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

// ---------------------------------------------------------------------------
// C2+C4 卡片票据契约：滚动摘要卡片行/归档通知里的 hex key，模型必须能
// 原样抄给 recall(items)。
// ---------------------------------------------------------------------------
func TestContract_CardTicket_ToMemoryRecall(t *testing.T) {
	kDeploy := int64(0x1201bb20000001)
	kSummary := int64(0x1201bb20000009)

	// 与 buildRetainedRefs / L3 归档通知的生产形态同步。
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

// ---------------------------------------------------------------------------
// C3 settle 票据契约（通知→召回，stable-context-compaction D1 修订/D7）：
// task_settled 通知渲染形态带 [evt_KEY|external_input] 前缀票据，需要历史
// 原文时模型必须能抄 evt key 给统一 recall 工具（get_task_result 已退役）。
// ---------------------------------------------------------------------------
func TestContract_TaskSettledTicket_ToMemoryRecall(t *testing.T) {
	kSettle := int64(0x1201bb20000abc)
	// 与 agent/event_bus.go newTaskSettledEvent 单行轨迹模板 + resolveRef 前缀同步：
	// 小结果全文内联（大结果为尾部+转储文件票据，此处验证票据可抄）。
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

// ---------------------------------------------------------------------------
// C7 task id 契约（ACK→重入）：ACK 文案里的 task id，模型必须能抄给
// resume_task 并附上续跑指令。
// ---------------------------------------------------------------------------
func TestContract_AckTaskID_ToResumeTask(t *testing.T) {
	taskID := "5d2e91c4-8b7a-4f3d-a1c6-e9b8d7f6a542"
	// 与 agent/tool_agent.go 子代理 ACK 模板同步（票据化，无工具名教学）。
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

// ---------------------------------------------------------------------------
// C5 伪调用防线：面对原生 tool-call 多轮历史，模型的文本部分不得出现
// 文本化调用语法（当初实机两次踩坑：文本调用语法会被模仿成执行不了的伪调用）。
// ---------------------------------------------------------------------------
func TestContract_NoTextualToolCallImitation(t *testing.T) {
	actionTool := declTool("action", "执行 shell 命令。",
		map[string]*tool.Schema{"command": {Type: "string"}}, "command")

	// 原生形态的多轮历史：assistant 带 ToolCalls，tool 带配对结果。
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
	// 期待：发起真实 ToolCall（首选）或纯文本回复；文本部分绝不含伪调用语法。
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

// ---------------------------------------------------------------------------
// C8 cwd 契约：action 每次调用都是 workspace 根的全新 shell（cd 不跨调用
// 保持）。模型面对"上一轮 cd 过子目录"的历史，仍必须按根路径出命令——
// 实机事故：模型误信 shell 持久，相对路径嵌套导致误删失败后靠绝对路径自救。
// ---------------------------------------------------------------------------
func TestContract_ActionCwdFreshShell(t *testing.T) {
	desc := "执行 shell 命令。每次调用都在【工作区根目录】的全新 shell 中运行,cd 不跨调用保持;子目录操作请单次调用内 `cd sub && …` 链式,或使用相对工作区根的路径。"
	actionTool := declTool("action", desc,
		map[string]*tool.Schema{"command": {Type: "string"}}, "command")

	// 历史：上一轮在子目录里干过活（迷航诱因）。
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
	// 迷航特征：用 ../ 补偿"还在 articles/2026"的错误认知。
	if strings.Contains(cmd, "../") {
		t.Errorf("model compensated with ../ — it assumed the previous cd persisted: %q", cmd)
	}
}

// ---------------------------------------------------------------------------
// C9 plan 写入边界契约：save_file 沙箱基准=openspec/ 根（工具级硬约束），
// prompt 明令禁写他处。面对"把分析写进 knowledge_base"的诱导，模型必须
// 把产出收敛进 openspec 相对路径（不带 openspec/ 前缀、无 ../ 逃逸、
// 不落他处）——实机事故：plan 越界写 knowledge_base/articles。
// ---------------------------------------------------------------------------
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
	// 收敛进 openspec 相对路径：不带 openspec/ 前缀、在 changes/ 下。
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

// ---------------------------------------------------------------------------
// C10 反自旋等待契约（context-efficiency-and-trajectory）：仅剩一个后台任务
// 在跑、无其他独立事项时，模型必须结束回合（简短回复），不得用 exec 执行
// sleep/wait 类命令轮询等待——实机事故：模型发明 exec(sleep N) 自旋 6 轮，
// 每轮携带 ~86K 上下文。守护对象是 ack/看板等待教学文案的行为效果。
// ---------------------------------------------------------------------------
func TestContract_WaitScenario_NoSleepSpin(t *testing.T) {
	actionTool := declTool("action",
		"执行 shell 命令（tmux 异步）。",
		map[string]*tool.Schema{"command": {Type: "string"}}, "command")

	// 与 agent/tool_agent.go 子代理 ACK + agent/task/task_board.go 看板指引同步。
	ack := `子 agent "plan" 已在后台运行 (task 7aa4eea3)；完成后其结果会作为 task_settled 回写。`
	board := "[后台任务看板] 系统注入的观察快照（非用户发言，不入历史，勿在回复中模仿此格式）：当前 1 个进行中\n" +
		"- [running] plan: 制定学习计划 (id=7aa4eea3, 已运行 5s)\n" +
		"以上任务无需轮询等待：直接给出简短回复并结束本回合即可，结算会自动唤醒你；不要用 sleep 等命令等待。"

	name, args, text := callOnce(t, []model.Message{
		model.NewSystemMessage("你是一个 agent。等待后台任务时，直接给出简短回复并结束回合；任务结算会以 task_settled 事件自动唤醒你。不要用 sleep/wait 等命令轮询等待。"),
		model.NewUserMessage(ack + "\n" + board + "\n\n你现在该怎么做？"),
	}, map[string]tool.Tool{"action": actionTool})

	t.Logf("tool=%q args=%s text=%q", name, args, text)
	// 反自旋核心断言：模型不得发起 sleep/wait 式等待命令。结束回合（无工具
	// 调用、给出简短文本）是正确行为。
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

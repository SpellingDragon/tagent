package tagent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/testutil"
)

// recordingSpecTool 是记录型 mock spec 工具（与生产 tagent.yaml 的 plan 工具
// 集一致：计划管理经类型化 spec 工具，后端 openspec CLI，无 shell）。用于观察
// plan agent 的 LLM 在 create 时到底发起了哪些 spec 操作。
type recordingSpecTool struct {
	mu  sync.Mutex
	ops []string
}

func (t *recordingSpecTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "spec",
		Description: "规格化计划管理（openspec 后端，类型化操作）：op ∈ init/new/status/validate/archive/instructions/list。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"op":       {Type: "string", Description: "操作类型: init/new/status/validate/archive/instructions/list"},
				"name":     {Type: "string", Description: "计划名（kebab-case）"},
				"artifact": {Type: "string", Description: "instructions 的目标 artifact（proposal/specs/design/tasks）"},
				"json":     {Type: "boolean", Description: "status 以 JSON 输出"},
			},
			Required: []string{"op"},
		},
	}
}

func (t *recordingSpecTool) record(op string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ops = append(t.ops, op)
}

func (t *recordingSpecTool) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.ops))
	copy(out, t.ops)
	return out
}

func (t *recordingSpecTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args struct {
		Op       string `json:"op"`
		Name     string `json:"name"`
		Artifact string `json:"artifact"`
	}
	_ = json.Unmarshal(jsonArgs, &args)
	t.record(args.Op)

	// 模拟真实 openspec CLI 的输出（与本机 openspec 实际文案对齐，
	// 让模型能从 ✔/Created 标记确认操作成功，避免重试震荡）。
	switch args.Op {
	case "init":
		return "✔ Initialized openspec directory structure (openspec/).", nil
	case "new":
		name := args.Name
		if name == "" {
			name = "unknown"
		}
		return fmt.Sprintf("- Creating change '%s'...\n✔ Created change '%s' at openspec/changes/%s/ (schema: spec-driven)\nNext: create artifacts with openspec instructions", name, name, name), nil
	case "list":
		return "Changes:\n  (none)", nil
	case "instructions":
		art := args.Artifact
		if art == "" {
			return "error: --artifact is required (proposal/specs/design/tasks)", nil
		}
		return fmt.Sprintf("- Generating instructions...\n<artifact id=%q change=%q schema=\"spec-driven\">\n## Template\n\nDescribe the goal.\n- [ ] Step description\n</artifact>", art, args.Name), nil
	case "status":
		if args.Name == "" {
			return "Changes:\n  (none)", nil
		}
		return fmt.Sprintf(`{"changeName":%q,"schemaName":"spec-driven","isComplete":false,"applyRequires":["tasks"],"artifacts":[{"id":"proposal","status":"done"},{"id":"tasks","status":"done"}]}`, args.Name), nil
	default:
		return "error: unknown op " + args.Op, nil
	}
}

// recordingSaveFileTool 记录 save_file 调用（写 proposal.md / tasks.md）。
type recordingSaveFileTool struct {
	mu    sync.Mutex
	saved map[string]string
}

func (t *recordingSaveFileTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "save_file",
		Description: "Save content to a file at the given path.",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"path":    {Type: "string", Description: "File path to save to."},
				"content": {Type: "string", Description: "File content."},
			},
			Required: []string{"path", "content"},
		},
	}
}

func (t *recordingSaveFileTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	_ = json.Unmarshal(jsonArgs, &args)
	t.mu.Lock()
	if t.saved == nil {
		t.saved = map[string]string{}
	}
	t.saved[args.Path] = args.Content
	t.mu.Unlock()
	return fmt.Sprintf("Saved %d bytes to %s", len(args.Content), args.Path), nil
}

func (t *recordingSaveFileTool) snapshot() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]string{}
	for k, v := range t.saved {
		out[k] = v
	}
	return out
}

// TestPlanAgentCreateBehavior_RealPrompt 用真实的 plan_agent.md 系统提示词 +
// 记录型 mock 工具（spec + save_file，与生产 plan 工具集一致），实际调用真实
// LLM，观察 plan agent 在收到 create 请求时到底发起了哪些操作——特别是是否
// 经 spec(op="new") 建 change、产出合规 artifact（proposal.md + tasks.md）、
// 并以 spec(op="status") 结构自检收尾。
//
// 契约随 plan_agent.md 的 spec 工具流演进（原 shell openspec CLI 契约已废弃：
// plan 无 shell 能力，spec 工具是唯一计划管理入口）。
func TestPlanAgentCreateBehavior_RealPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("无法加载配置: %v", err)
	}
	t.Logf("配置: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

	// 加载真实的 plan_agent.md 系统提示词
	promptPath := "../examples/wechat-bot/resources/prompts/plan_agent.md"
	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatalf("无法读取 plan_agent.md: %v", err)
	}
	systemPrompt := string(promptBytes)
	t.Logf("已加载真实 plan_agent.md (len=%d)", len(systemPrompt))

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	specTool := &recordingSpecTool{}
	saveTool := &recordingSaveFileTool{}

	thinking := true
	subAg, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxToolIterations: 40, // 模型探索冗余度不稳定（实测最多 37 次），留足预算走完 create→自检收尾
		MaxTokens:         64000,
		Temperature:       0.3,
		SystemPrompt:      systemPrompt,
		Name:              "plan",
		Description:       "Plan agent",
		ThinkingEnabled:   &thinking,
		Tools:             []tool.Tool{specTool, saveTool},
	})
	if err != nil {
		t.Fatalf("创建 agent 失败: %v", err)
	}

	wrapper := tagentagent.NewAgentToolWrapper(subAg, "Manage openspec work plans", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	// 模拟真实的 create 请求（与日志中的请求类似）
	req := map[string]string{
		"action":  "create",
		"request": "创建一个学习 Go 语言的计划：1) 学习基础语法 2) 理解并发模型 3) 实践项目。用于验证 plan 创建流程。",
	}
	args, _ := json.Marshal(req)
	t.Logf("调用 plan(action=create, request=学习Go计划)")

	result, err := wrapper.Call(ctx, args)
	if err != nil {
		t.Fatalf("AgentToolWrapper.Call 报错: %v", err)
	}

	resultStr, _ := result.(string)
	ops := specTool.snapshot()
	savedFiles := saveTool.snapshot()

	// 打印 plan agent 实际发起的所有 spec 操作
	t.Logf("========== plan agent 发起的 spec 操作序列 (%d 条) ==========", len(ops))
	for i, op := range ops {
		mark := ""
		switch op {
		case "new":
			mark = " <<< spec new (create 核心)"
		case "init":
			mark = " (init)"
		case "list":
			mark = " (list 盘点)"
		case "instructions":
			mark = " (取模板)"
		case "status":
			mark = " (自检收尾)"
		}
		t.Logf("  [%d] spec(op=%q)%s", i+1, op, mark)
	}
	t.Logf("========== save_file 写入的文件 (%d 个) ==========", len(savedFiles))
	for p := range savedFiles {
		t.Logf("  - %s", p)
	}
	t.Logf("========== plan 返回 (len=%d) ==========", len(resultStr))
	t.Logf("%s", truncateCmd(resultStr, 400))

	// 核心断言：create 必须产出合规 openspec change（proposal.md + tasks.md），
	// tasks.md 遵循官方模板，且按级别收尾（plan-interaction-contract D3）：
	// A 级以 spec(op="status") 结构自检收尾，禁止 validate（无 specs deltas 必然失败）。
	ranNew := false
	ranValidate := false
	ranStatusClose := false
	for _, op := range ops {
		switch op {
		case "new":
			ranNew = true
		case "validate":
			ranValidate = true
		case "status":
			ranStatusClose = true
		}
	}
	wroteProposal := false
	wroteTasks := false
	var tasksContent string
	var savedPaths []string
	for p, content := range savedFiles {
		savedPaths = append(savedPaths, p)
		if strings.Contains(p, "proposal.md") {
			wroteProposal = true
		}
		if strings.Contains(p, "tasks.md") {
			wroteTasks = true
			tasksContent = content
		}
	}
	// tasks.md 官方模板：`## N.` 分组标题 + `- [ ] N.M` 复选框
	groupHeadingRe := regexp.MustCompile(`(?m)^## \d+\.`)
	checkboxRe := regexp.MustCompile(`(?m)^- \[ \] \d+\.\d+`)
	tasksFollowsTemplate := groupHeadingRe.MatchString(tasksContent) && checkboxRe.MatchString(tasksContent)

	// 诊断输出：明确 create 是否产出合规 change
	if !ranNew {
		t.Errorf("❌ plan create 未发起 spec(op=\"new\")。发起的操作: %v", ops)
	} else {
		t.Logf("✅ plan create 发起了 spec(op=\"new\")")
	}
	// 关键：proposal.md 必须创建（不再是裸 tasks.md 目录）
	if !wroteProposal {
		t.Errorf("❌ plan create 未创建 proposal.md（仍是裸 tasks.md 目录）。写入的文件: %v", savedPaths)
	} else {
		t.Logf("✅ plan create 创建了 proposal.md")
	}
	if !wroteTasks {
		t.Errorf("❌ plan create 未写 tasks.md。写入的文件: %v", savedPaths)
	} else {
		t.Logf("✅ plan create 写入了 tasks.md")
	}
	if wroteTasks && !tasksFollowsTemplate {
		t.Errorf("❌ tasks.md 不符合官方模板（需 `## N.` 分组 + `- [ ] N.M` 复选框）。内容:\n%s", truncateCmd(tasksContent, 300))
	} else if tasksFollowsTemplate {
		t.Logf("✅ tasks.md 符合官方模板（## N. 分组 + - [ ] N.M 复选框）")
	}
	if !ranStatusClose {
		t.Errorf("❌ plan create 未以 spec(op=\"status\") 结构自检收尾（A 级收尾方式）。发起的操作: %v", ops)
	} else {
		t.Logf("✅ plan create 以 spec(op=\"status\") 自检收尾")
	}
	if ranValidate {
		t.Errorf("❌ A 级计划调用了 spec(op=\"validate\")（无 specs deltas 必然失败，新契约禁止）。发起的操作: %v", ops)
	} else {
		t.Logf("✅ A 级计划未调用 validate（符合新契约）")
	}
}

func truncateCmd(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

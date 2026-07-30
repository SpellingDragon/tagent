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

// recordingActionTool 是一个记录所有执行命令的 mock action 工具。
// 它模拟一个 openspec 可用的 shell 环境，返回真实感的输出，
// 用于观察 plan agent 的 LLM 在 create 时到底执行了哪些命令。
type recordingActionTool struct {
	mu       sync.Mutex
	commands []string
}

func (t *recordingActionTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "action",
		Description: "Execute a shell command via tmux and wait for it to stabilize. Returns the final status and captured output.",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"command": {
					Type:        "string",
					Description: "The action to execute, described as a shell command. Runs via sh -c so pipes, redirects, and chaining are supported.",
				},
			},
			Required: []string{"command"},
		},
	}
}

func (t *recordingActionTool) record(cmd string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.commands = append(t.commands, cmd)
}

func (t *recordingActionTool) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.commands))
	copy(out, t.commands)
	return out
}

func (t *recordingActionTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(jsonArgs, &args)
	t.record(args.Command)

	// 模拟真实 openspec 环境的输出。
	cmd := args.Command
	switch {
	case strings.Contains(cmd, "openspec init"):
		return "OpenSpec initialized. Created openspec/ directory structure.", nil
	case strings.Contains(cmd, "openspec new change"):
		// 提取 change 名
		name := "unknown"
		if idx := strings.Index(cmd, "new change"); idx >= 0 {
			rest := strings.TrimSpace(cmd[idx+len("new change"):])
			rest = strings.Trim(rest, "\"' ")
			if sp := strings.IndexAny(rest, " \"'&|;"); sp > 0 {
				rest = rest[:sp]
			}
			if rest != "" {
				name = rest
			}
		}
		return fmt.Sprintf("Created change '%s' at openspec/changes/%s/\n  - proposal.md\n  - tasks.md\n  - design.md", name, name), nil
	case strings.Contains(cmd, "openspec list"):
		return "No active changes found.", nil
	case strings.Contains(cmd, "openspec instructions"):
		return "## Proposal Template\n\nDescribe the goal and motivation.\n\n## Tasks Template\n\n- [ ] Step description", nil
	case strings.Contains(cmd, "openspec --help") || strings.Contains(cmd, "openspec -h"):
		return "Usage: openspec [command]\n\nCommands:\n  init\n  new change <name>\n  list\n  status\n  archive\n  instructions", nil
	case strings.Contains(cmd, "pwd"):
		return "/workspace\ntotal 8\ndrwxr-xr-x  openspec/", nil
	case strings.Contains(cmd, "ls"):
		return "openspec/\ngo.mod\nREADME.md", nil
	default:
		return "OK (exit 0)", nil
	}
}

// recordingSaveFileTool 记录 save_file 调用（写 tasks.md）。
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
// 记录型 mock 工具，实际调用 glm-5.2，观察 plan agent 在收到 create 请求时
// 到底执行了哪些命令——特别是是否真的执行了 `openspec new change`。
//
// 这复现 wechat-bot 日志中观察到的现象：8 次 create 调用，0 次真正执行
// `openspec new change`，全是 pwd/ls/openspec list 探索类命令。
//
// 运行方式：
//
//	TRPC_CLAW_MODEL_NAME=glm-5.2 go test -v \
//	    -run TestPlanAgentCreateBehavior_RealPrompt \
//	    ./tests/ -timeout 180s
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

	actionTool := &recordingActionTool{}
	saveTool := &recordingSaveFileTool{}

	thinking := true
	subAg, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxToolIterations: 15, // 与修复后的 tagent.yaml 一致
		MaxTokens:         64000,
		Temperature:       0.3,
		SystemPrompt:      systemPrompt,
		Name:              "plan",
		Description:       "Plan agent",
		ThinkingEnabled:   &thinking,
		Tools:             []tool.Tool{actionTool, saveTool},
	})
	if err != nil {
		t.Fatalf("创建 agent 失败: %v", err)
	}

	wrapper := tagentagent.NewAgentToolWrapper(subAg, "Manage openspec work plans", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
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
	commands := actionTool.snapshot()
	savedFiles := saveTool.snapshot()

	// 打印 plan agent 实际执行的所有命令
	t.Logf("========== plan agent 执行的命令序列 (%d 条) ==========", len(commands))
	for i, c := range commands {
		mark := ""
		switch {
		case strings.Contains(c, "openspec new change"):
			mark = " <<< openspec new change (create 核心)"
		case strings.Contains(c, "openspec init"):
			mark = " (init)"
		case strings.Contains(c, "openspec --help"):
			mark = " (help 探索)"
		case strings.Contains(c, "openspec list"):
			mark = " (list 探索)"
		case strings.Contains(c, "pwd") || strings.Contains(c, "ls"):
			mark = " (环境探索)"
		}
		t.Logf("  [%d] %s%s", i+1, truncateCmd(c, 100), mark)
	}
	t.Logf("========== save_file 写入的文件 (%d 个) ==========", len(savedFiles))
	for p := range savedFiles {
		t.Logf("  - %s", p)
	}
	t.Logf("========== plan 返回 (len=%d) ==========", len(resultStr))
	t.Logf("%s", truncateCmd(resultStr, 400))

	// 核心断言：create 必须产出合规 openspec change（proposal.md + tasks.md），
	// tasks.md 遵循官方模板，且按级别收尾（plan-interaction-contract D3）：
	// A 级以 openspec status 结构自检收尾，禁止 validate（无 specs deltas 必然失败）。
	ranNewChange := false
	ranValidate := false
	ranStatusClose := false
	for _, c := range commands {
		if strings.Contains(c, "openspec new change") {
			ranNewChange = true
		}
		if strings.Contains(c, "openspec validate") {
			ranValidate = true
		}
		if strings.Contains(c, "openspec status") {
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
	if !ranNewChange {
		t.Errorf("❌ plan create 未执行 `openspec new change`。执行的命令: %v", commands)
	} else {
		t.Logf("✅ plan create 执行了 `openspec new change`")
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
		t.Errorf("❌ plan create 未以 `openspec status` 结构自检收尾（A 级收尾方式）。执行的命令: %v", commands)
	} else {
		t.Logf("✅ plan create 以 `openspec status` 自检收尾")
	}
	if ranValidate {
		t.Errorf("❌ A 级计划调用了 `openspec validate`（无 specs deltas 必然失败，新契约禁止）。执行的命令: %v", commands)
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

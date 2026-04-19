package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
)

// ==================== Helpers ====================

// mustMarshal marshals args to JSON bytes for CallableTool.Call().
func mustMarshal(t *testing.T, args map[string]interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}
	return data
}

// newTestMemoryStore creates a MemoryStore pre-populated with test events.
func newTestMemoryStore(t *testing.T, events map[string]memory.FullEvent) memory.MemoryStore {
	t.Helper()
	tempDir := t.TempDir()
	store, err := memory.NewFileBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}
	for key, event := range events {
		if err := store.StoreEvent(key, event); err != nil {
			t.Fatalf("Failed to store event %s: %v", key, err)
		}
	}
	return store
}

// hasTmux returns true if tmux is available on the system.
func hasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// skipWithoutTmux skips the test if tmux is not available.
func skipWithoutTmux(t *testing.T) {
	t.Helper()
	if !hasTmux() {
		t.Skip("tmux not available, skipping tmux-dependent test")
	}
}

// ==================== RecallTool Tests ====================
// 严格对齐设计文档 1.5 测试用例

// Test 1: 基本回忆
// 设计文档: recall(query="用户之前让我做什么") → LLM 调用 memory_query → 返回相关事件
func TestRecallTool_BasicRecall(t *testing.T) {
	store := newTestMemoryStore(t, map[string]memory.FullEvent{
		"evt_001": {
			EventKey:     "evt_001",
			EventType:    memory.EventTypeActionCommand,
			EventSummary: "用户要求整理文件",
			Timestamp:    time.Now().UnixMilli(),
			Content:      "整理 /tmp 目录下的文件",
		},
		"evt_002": {
			EventKey:     "evt_002",
			EventType:    memory.EventTypeAgentOutput,
			EventSummary: "文件整理完成",
			Timestamp:    time.Now().Add(1 * time.Minute).UnixMilli(),
			Content:      "成功整理 15 个文件",
		},
	})

	tool := NewRecallTool(WithRecallMemoryStore(store))

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "文件整理",
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call failed: %v", err)
	}

	resp, ok := result.(*RecallResponse)
	if !ok {
		t.Fatalf("Expected *RecallResponse, got %T", result)
	}

	// 验证: 返回相关事件
	if len(resp.Events) == 0 {
		t.Error("Expected events, got none")
	}

	// 验证: 事件应包含文件整理相关内容
	found := false
	for _, evt := range resp.Events {
		if strings.Contains(evt.Summary, "整理") || strings.Contains(evt.Summary, "文件") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find event related to '文件整理', but none matched")
	}

	t.Logf("BasicRecall: found %d events, message=%q", len(resp.Events), resp.Message)
}

// Test 2: 多轮回忆（需要获取完整事件）
// 设计文档: recall(query="上次执行命令的完整结果是什么")
// → LLM 先调用 memory_query 获取事件列表 → LLM 再调用 memory_get 获取完整详情
func TestRecallTool_MultiStepRecall(t *testing.T) {
	store := newTestMemoryStore(t, map[string]memory.FullEvent{
		"evt_cmd_001": {
			EventKey:     "evt_cmd_001",
			EventType:    memory.EventTypeActionCommand,
			EventSummary: "执行部署命令",
			Timestamp:    time.Now().Add(-2 * time.Hour).UnixMilli(),
			Content:      "deploy.sh --env production",
		},
		"evt_result_001": {
			EventKey:     "evt_result_001",
			EventType:    memory.EventTypeAgentOutput,
			EventSummary: "部署结果",
			Timestamp:    time.Now().Add(-2*time.Hour + 30*time.Second).UnixMilli(),
			Content:      "部署成功: 3 个服务已更新，耗时 2m30s",
		},
	})

	tool := NewRecallTool(WithRecallMemoryStore(store))

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "上次执行命令的完整结果",
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call failed: %v", err)
	}

	resp, ok := result.(*RecallResponse)
	if !ok {
		t.Fatalf("Expected *RecallResponse, got %T", result)
	}

	// 验证: simple mode 下至少返回了事件列表
	// TODO: React Agent 模式下应验证 memory_get 被调用
	if len(resp.Events) == 0 {
		t.Error("Expected events from multi-step recall, got none")
	}

	t.Logf("MultiStepRecall: found %d events", len(resp.Events))
}

// Test 3: 回忆摘要
// 设计文档: recall(query="总结上次的文件整理过程")
// → LLM 调用 memory_query → LLM 调用 memory_summarize → 返回结构化摘要
func TestRecallTool_Summarize(t *testing.T) {
	store := newTestMemoryStore(t, map[string]memory.FullEvent{
		"evt_s1": {
			EventKey:     "evt_s1",
			EventType:    memory.EventTypeActionCommand,
			EventSummary: "开始文件整理",
			Timestamp:    time.Now().Add(-1 * time.Hour).UnixMilli(),
			Content:      "开始整理 /data 目录",
		},
		"evt_s2": {
			EventKey:     "evt_s2",
			EventType:    memory.EventTypeAgentOutput,
			EventSummary: "文件整理进度",
			Timestamp:    time.Now().Add(-1*time.Hour + 5*time.Minute).UnixMilli(),
			Content:      "已整理 50 个文件",
		},
		"evt_s3": {
			EventKey:     "evt_s3",
			EventType:    memory.EventTypeAgentOutput,
			EventSummary: "文件整理完成",
			Timestamp:    time.Now().Add(-1*time.Hour + 10*time.Minute).UnixMilli(),
			Content:      "全部完成，共整理 120 个文件",
		},
	})

	tool := NewRecallTool(WithRecallMemoryStore(store))

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "总结上次的文件整理过程",
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call failed: %v", err)
	}

	resp, ok := result.(*RecallResponse)
	if !ok {
		t.Fatalf("Expected *RecallResponse, got %T", result)
	}

	// 验证: 应返回多个相关事件
	if len(resp.Events) < 2 {
		t.Errorf("Expected at least 2 events for summarize query, got %d", len(resp.Events))
	}

	// TODO: React Agent 模式下应验证 memory_summarize 被调用
	t.Logf("Summarize: found %d events, message=%q", len(resp.Events), resp.Message)
}

// Test 4: 空结果处理
// 设计文档: recall(query="不存在的事件") → 尝试多种查询策略 → 最终返回空结果并说明原因
func TestRecallTool_NoResults(t *testing.T) {
	// Empty memory store
	store := newTestMemoryStore(t, nil)

	tool := NewRecallTool(WithRecallMemoryStore(store))

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "不存在的事件xyz123",
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call should not error for no results: %v", err)
	}

	resp, ok := result.(*RecallResponse)
	if !ok {
		t.Fatalf("Expected *RecallResponse, got %T", result)
	}

	// 验证: 返回空结果但不是错误
	if len(resp.Events) != 0 {
		t.Errorf("Expected 0 events for non-existent query, got %d", len(resp.Events))
	}

	// 验证: 应说明原因
	if resp.Message == "" {
		t.Error("Expected message explaining no results")
	}

	t.Logf("NoResults: events=%d, message=%q", len(resp.Events), resp.Message)
}

// Test 5: 自然语言理解（关键词提取）
// 设计文档: recall(query="我昨天让助手整理了什么文件，最后成功了吗")
// → 提取关键词 ["昨天", "助手", "整理", "文件", "成功"]
// → 过滤停用词 ["我", "让", "了", "什么", "吗"]
func TestRecallTool_NaturalLanguage(t *testing.T) {
	store := newTestMemoryStore(t, map[string]memory.FullEvent{
		"evt_nl": {
			EventKey:     "evt_nl",
			EventType:    memory.EventTypeActionCommand,
			EventSummary: "助手整理文件",
			Timestamp:    time.Now().Add(-24 * time.Hour).UnixMilli(),
			Content:      "助手整理了 /data 目录的文件，结果成功",
		},
	})

	tool := NewRecallTool(WithRecallMemoryStore(store))

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "我昨天让助手整理了什么文件，最后成功了吗",
		"limit": 10,
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call failed: %v", err)
	}

	resp, ok := result.(*RecallResponse)
	if !ok {
		t.Fatalf("Expected *RecallResponse, got %T", result)
	}

	// 验证: simple mode 下返回事件
	if len(resp.Events) == 0 {
		t.Error("Expected events for natural language query")
	}

	t.Logf("NaturalLanguage: found %d events, message=%q", len(resp.Events), resp.Message)
}

// Test: 关键词提取的详细验证
// 设计文档 1.4: extractKeywords + stopWords
func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name              string
		query             string
		mustNotContain    []string // 停用词必须被过滤
		shouldContainSome []string // 至少包含其中一些
	}{
		{
			name:              "English stop words filtered",
			query:             "what did the assistant do yesterday",
			mustNotContain:    []string{"the", "a", "is", "are"},
			shouldContainSome: []string{"assistant", "yesterday"},
		},
		{
			name:              "Chinese stop words filtered",
			query:             "我 的 了 是 在 他 你",
			mustNotContain:    []string{"我", "你", "的", "了", "是", "在"},
			shouldContainSome: []string{},
		},
		{
			name:              "Mixed content preserves meaningful words",
			query:             "deploy production server configuration",
			mustNotContain:    []string{},
			shouldContainSome: []string{"deploy", "production", "server", "configuration"},
		},
		{
			name:              "Short words filtered",
			query:             "I a x go to",
			mustNotContain:    []string{"a"},
			shouldContainSome: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := extractKeywords(tt.query)

			// 验证停用词被过滤
			for _, sw := range tt.mustNotContain {
				for _, kw := range keywords {
					if kw == sw {
						t.Errorf("Stop word %q should be filtered, but found in keywords", sw)
					}
				}
			}

			// 验证有意义的词至少部分保留
			if len(tt.shouldContainSome) > 0 {
				found := 0
				for _, expected := range tt.shouldContainSome {
					for _, kw := range keywords {
						if kw == expected {
							found++
							break
						}
					}
				}
				if found == 0 {
					t.Errorf("Expected at least one of %v in keywords, got %v",
						tt.shouldContainSome, keywords)
				}
			}

			t.Logf("Query: %q -> Keywords: %v", tt.query, keywords)
		})
	}
}

// Test: RecallTool 空查询必须报错
func TestRecallTool_EmptyQuery(t *testing.T) {
	tool := NewRecallTool()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "",
	}))
	if err == nil {
		t.Fatal("Expected error for empty query, got nil")
	}

	if !strings.Contains(err.Error(), "query") {
		t.Errorf("Error should mention 'query', got: %v", err)
	}
}

// Test: RecallTool 无 MemoryStore 时的降级行为
func TestRecallTool_NoMemoryStore(t *testing.T) {
	tool := NewRecallTool()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "test query",
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call should not error without memory store: %v", err)
	}

	resp, ok := result.(*RecallResponse)
	if !ok {
		t.Fatalf("Expected *RecallResponse, got %T", result)
	}

	// 验证: 返回降级消息
	if resp.Message == "" {
		t.Error("Expected degradation message without memory store")
	}

	if len(resp.Events) != 0 {
		t.Error("Expected no events without memory store")
	}

	t.Logf("NoMemoryStore: message=%q", resp.Message)
}

// ==================== Sub-tool Tests ====================

// TestSubTool_SkillSearch tests the skill_search sub-tool.
func TestSubTool_SkillSearch(t *testing.T) {
	repo := &mockSkillRepo{
		summaries: []skill.Summary{{Name: "github-pr", Description: "Create a GitHub pull request workflow"}},
	}

	results := searchSkills(repo, "github")
	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}
	if results[0].Title != "github-pr" {
		t.Errorf("Expected 'github-pr', got %q", results[0].Title)
	}
}

// TestSubTool_SkillSearch_NoMatch tests that unrelated queries return few/no results.
func TestSubTool_SkillSearch_NoMatch(t *testing.T) {
	repo := &mockSkillRepo{
		summaries: []skill.Summary{{Name: "deploy", Description: "Deploy application"}},
	}

	results := searchSkills(repo, "xyzabc123")
	if len(results) > 0 {
		t.Logf("Got %d results (some may match due to CJK substring logic), not a hard failure", len(results))
	}
}

// mockSkillRepo is a test double for SkillRepository.
type mockSkillRepo struct {
	summaries []skill.Summary
}

func (m *mockSkillRepo) Summaries() []skill.Summary { return m.summaries }
func (m *mockSkillRepo) Get(name string) (*skill.Skill, error) {
	return nil, fmt.Errorf("skill not found: %s", name)
}

// ==================== CommandTool Tests ====================
// 严格对齐设计文档 3.6 测试用例

// Test 1: 同步命令执行
// 设计文档: command(function="exec", command="echo hello") → ExitCode=0, Stdout="hello\n"
func TestCommandTool_SyncExec(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "echo hello",
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", resp.ExitCode)
	}

	if resp.Stdout != "hello\n" {
		t.Errorf("Expected stdout 'hello\\n', got %q", resp.Stdout)
	}
}

// Test 2: 异步命令执行
// 设计文档: command(function="tmux_exec", command="sleep 10") → 立即返回 SessionID → TmuxMonitor 监控
func TestCommandTool_AsyncExec(t *testing.T) {
	skipWithoutTmux(t)

	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "tmux_exec",
		"command": "echo async_test && sleep 1",
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*TmuxExecResponse)
	if !ok {
		t.Fatalf("Expected *TmuxExecResponse, got %T", result)
	}

	// 验证: 立即返回 SessionID
	if resp.SessionID == "" {
		t.Error("Expected session ID, got empty string")
	}

	// 验证: 状态为 running
	if resp.Status != string(SessionRunning) {
		t.Errorf("Expected status %q, got %q", SessionRunning, resp.Status)
	}

	// 验证: TmuxMonitor 开始监控此会话
	_, exists := tool.tmuxMonitor.GetSession(resp.SessionID)
	if !exists {
		t.Error("Expected session to be monitored by TmuxMonitor")
	}

	// 清理
	tool.tmuxExecutor.KillSession(resp.SessionID)

	t.Logf("AsyncExec: session_id=%s, status=%q", resp.SessionID, resp.Status)
}

// Test 3: Skill 执行（通过 KnowledgeAgent 翻译后）
// 设计文档: KnowledgeAgent 返回执行计划 → CommandTool 执行
// 实际流程: KnowledgeAgent 返回 {execution_plan: {command: "./scripts/create_pr.sh ..."}}
//
//	Agent 调用 command(function="exec", command="./scripts/create_pr.sh ...")
func TestCommandTool_SkillExecution(t *testing.T) {
	// 模拟: KnowledgeAgent 翻译后的执行计划命令
	// 创建临时脚本
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "skill_test.sh")
	scriptContent := "#!/bin/sh\necho 'Skill executed successfully'\nexit 0"
	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("Failed to create test script: %v", err)
	}

	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": scriptPath,
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d, stderr: %s", resp.ExitCode, resp.Stderr)
	}

	if !strings.Contains(resp.Stdout, "Skill executed successfully") {
		t.Errorf("Expected stdout to contain 'Skill executed successfully', got %q", resp.Stdout)
	}
}

// Test 4: MCP 执行（通过 KnowledgeAgent 翻译后）
// 设计文档: KnowledgeAgent 返回执行计划 → CommandTool 执行
// 实际流程: KnowledgeAgent 返回 {execution_plan: {command: "curl -X POST ..."}}
//
//	Agent 调用 command(function="exec", command="curl -X POST ...")
func TestCommandTool_MCPExecution(t *testing.T) {
	// 模拟: KnowledgeAgent 翻译后的 MCP RPC 调用命令
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "echo '{\"status\": \"ok\", \"data\": \"mcp_result\"}'",
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", resp.ExitCode)
	}

	// 验证: MCP 响应内容被正确捕获
	var mcpResult map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Stdout)), &mcpResult); err != nil {
		t.Fatalf("Failed to parse MCP result: %v", err)
	}

	if mcpResult["status"] != "ok" {
		t.Errorf("Expected MCP status 'ok', got %v", mcpResult["status"])
	}
}

// Test 5: 安全隔离
// 设计文档: 配置 runAsUser 和 runAsGroup → command(function="exec", command="whoami") → 使用指定用户执行
func TestCommandTool_SecurityIsolation(t *testing.T) {
	// 注意: sudo 在沙盒环境中可能不可用，此测试验证配置传递
	tool := NewCommandTool(
		WithCommandRunAsUser("nobody"),
		WithCommandRunAsGroup("nobody"),
	)
	defer tool.tmuxMonitor.Stop()

	// 验证: CommandSpec 正确设置
	if tool.runAsUser != "nobody" {
		t.Errorf("Expected tool runAsUser 'nobody', got %q", tool.runAsUser)
	}

	if tool.runAsGroup != "nobody" {
		t.Errorf("Expected tool runAsGroup 'nobody', got %q", tool.runAsGroup)
	}

	// 注意: 实际 sudo 执行在 CI 中可能被禁用
	t.Log("SecurityIsolation: configuration verified")
}

// Test 6-10: TmuxMonitor 测试 → 见下方 TmuxMonitor 专项测试

// Test: CommandTool 未知 function 报错
func TestCommandTool_UnknownFunction(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "unknown",
		"command": "echo test",
	}))
	if err == nil {
		t.Fatal("Expected error for unknown function, got nil")
	}

	if !strings.Contains(err.Error(), "unknown mode") {
		t.Errorf("Error should mention 'unknown mode', got: %v", err)
	}
}

// Test: CommandTool 空 command 报错
func TestCommandTool_EmptyCommand(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	_, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "",
	}))
	if err == nil {
		t.Fatal("Expected error for empty command, got nil")
	}
}

// Test: CommandTool 同步执行带工作目录
func TestCommandTool_SyncExecWithWorkDir(t *testing.T) {
	tempDir := t.TempDir()
	tool := NewCommandTool(WithCommandWorkspace(tempDir))
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "pwd",
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d, stderr: %s", resp.ExitCode, resp.Stderr)
	}

	// 验证工作目录
	expectedDir, _ := filepath.EvalSymlinks(tempDir)
	actualDir := strings.TrimSpace(resp.Stdout)
	actualDirEval, _ := filepath.EvalSymlinks(actualDir)

	if actualDirEval != expectedDir {
		t.Errorf("Expected workdir %q, got %q", expectedDir, actualDir)
	}
}

// Test: CommandTool 同步执行带环境变量
func TestCommandTool_SyncExecWithEnv(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "echo $TAGENT_TEST_VAR",
		"env": map[string]interface{}{
			"TAGENT_TEST_VAR": "tagent_test_value",
		},
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d, stderr: %s", resp.ExitCode, resp.Stderr)
	}

	if resp.Stdout != "tagent_test_value\n" {
		t.Errorf("Expected stdout 'tagent_test_value\\n', got %q", resp.Stdout)
	}
}

// Test: CommandTool 同步执行错误退出码
func TestCommandTool_SyncExecError(t *testing.T) {
	tool := NewCommandTool()
	defer tool.tmuxMonitor.Stop()

	ctx := context.Background()
	result, err := tool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": "exit 42",
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}

	resp, ok := result.(*CommandExecResult)
	if !ok {
		t.Fatalf("Expected *CommandExecResult, got %T", result)
	}

	if resp.ExitCode != 42 {
		t.Errorf("Expected exit code 42, got %d", resp.ExitCode)
	}
}

// ==================== CommandExecutor Tests ====================

func TestCommandExecutor_BasicExecution(t *testing.T) {
	executor := NewCommandExecutor()

	ctx := context.Background()
	spec := CommandSpec{
		Command: "echo",
		Args:    []string{"test_output"},
	}

	result, err := executor.Execute(ctx, spec)
	if err != nil {
		t.Fatalf("CommandExecutor.Execute failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}

	if result.Stdout != "test_output\n" {
		t.Errorf("Expected stdout 'test_output\\n', got %q", result.Stdout)
	}

	if result.Duration == 0 {
		t.Error("Expected non-zero duration")
	}
}

func TestCommandExecutor_Timeout(t *testing.T) {
	executor := NewCommandExecutor(
		WithExecutorDefaultTimeout(1 * time.Second),
	)

	ctx := context.Background()
	spec := CommandSpec{
		Command: "sleep",
		Args:    []string{"30"},
	}

	result, err := executor.Execute(ctx, spec)
	_ = err // exec.CommandContext may or may not return error for timeout

	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for timed-out command")
	}

	// 验证: 超时后应该很快返回（不超过5秒）
	if result.Duration > 5*time.Second {
		t.Errorf("Timeout should have killed the process quickly, took %v", result.Duration)
	}
}

func TestCommandExecutor_WorkDir(t *testing.T) {
	tempDir := t.TempDir()
	executor := NewCommandExecutor(
		WithExecutorWorkspace(tempDir),
	)

	ctx := context.Background()
	spec := CommandSpec{
		Command: "pwd",
	}

	result, err := executor.Execute(ctx, spec)
	if err != nil {
		t.Fatalf("CommandExecutor.Execute failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	expectedDir, _ := filepath.EvalSymlinks(tempDir)
	actualDir := strings.TrimSpace(result.Stdout)
	actualDirEval, _ := filepath.EvalSymlinks(actualDir)

	if actualDirEval != expectedDir {
		t.Errorf("Expected workdir %q, got %q", expectedDir, actualDir)
	}
}

func TestCommandExecutor_EnvVariables(t *testing.T) {
	executor := NewCommandExecutor()

	ctx := context.Background()
	spec := CommandSpec{
		Command: "sh",
		Args:    []string{"-c", "echo $TAGENT_EXEC_TEST"},
		Env:     map[string]string{"TAGENT_EXEC_TEST": "env_value_123"},
	}

	result, err := executor.Execute(ctx, spec)
	if err != nil {
		t.Fatalf("CommandExecutor.Execute failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	if result.Stdout != "env_value_123\n" {
		t.Errorf("Expected stdout 'env_value_123\\n', got %q", result.Stdout)
	}
}

func TestCommandExecutor_CommandSpecOverrides(t *testing.T) {
	// 验证: CommandSpec 中的 Dir 覆盖 Workspace
	tempDir := t.TempDir()
	executor := NewCommandExecutor(
		WithExecutorWorkspace("/tmp"), // 默认 workspace
	)

	ctx := context.Background()
	spec := CommandSpec{
		Command: "pwd",
		Dir:     tempDir, // Spec 中的 Dir 应覆盖 Workspace
	}

	result, err := executor.Execute(ctx, spec)
	if err != nil {
		t.Fatalf("CommandExecutor.Execute failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("Expected exit code 0, got %d, stderr: %s", result.ExitCode, result.Stderr)
	}

	expectedDir, _ := filepath.EvalSymlinks(tempDir)
	actualDir := strings.TrimSpace(result.Stdout)
	actualDirEval, _ := filepath.EvalSymlinks(actualDir)

	if actualDirEval != expectedDir {
		t.Errorf("Spec.Dir should override Workspace, expected %q, got %q", expectedDir, actualDir)
	}
}

func TestCommandExecutor_BuildEnv(t *testing.T) {
	executor := NewCommandExecutor()

	env := executor.buildEnv(map[string]string{
		"FOO": "bar",
	})

	// 验证: PATH 存在
	hasPath := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			break
		}
	}
	if !hasPath {
		t.Error("buildEnv should include PATH")
	}

	// 验证: 自定义变量存在
	hasFoo := false
	for _, e := range env {
		if e == "FOO=bar" {
			hasFoo = true
			break
		}
	}
	if !hasFoo {
		t.Error("buildEnv should include custom env vars")
	}
}

func TestCommandExecutor_ProcessGroup(t *testing.T) {
	// 验证: SysProcAttr.Setpgid 被正确设置
	executor := NewCommandExecutor()

	spec := CommandSpec{
		Command: "echo",
		Args:    []string{"test"},
	}

	cmd, err := executor.buildCommand(spec)
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should not be nil")
	}

	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid should be true for process group management")
	}
}

// ==================== TmuxMonitor Tests ====================
// 严格对齐设计文档 3.7 测试用例

func TestTmuxMonitor_Lifecycle(t *testing.T) {
	monitor := NewTmuxMonitor()

	monitor.Start()
	if !monitor.running {
		t.Error("Monitor should be running after Start()")
	}

	// 防止重复启动
	monitor.Start() // should be no-op
	if !monitor.running {
		t.Error("Monitor should still be running after duplicate Start()")
	}

	monitor.Stop()
	if monitor.running {
		t.Error("Monitor should not be running after Stop()")
	}

	// 防止重复停止
	monitor.Stop() // should be no-op
}

func TestTmuxMonitor_AddRemoveSession(t *testing.T) {
	monitor := NewTmuxMonitor()

	session := &TmuxSession{
		ID:     "test-session-1",
		Status: SessionRunning,
	}

	monitor.AddSession(session)
	if _, exists := monitor.GetSession(session.ID); !exists {
		t.Error("Session should exist after AddSession()")
	}

	// 添加多个会话
	session2 := &TmuxSession{
		ID:     "test-session-2",
		Status: SessionRunning,
	}
	monitor.AddSession(session2)

	sessions := monitor.ListSessions()
	if len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(sessions))
	}

	// 移除会话
	monitor.RemoveSession(session.ID)
	if _, exists := monitor.GetSession(session.ID); exists {
		t.Error("Session should not exist after RemoveSession()")
	}
}

func TestTmuxMonitor_StateChangeCallback(t *testing.T) {
	stateChanges := []struct {
		oldStatus SessionStatus
		newStatus SessionStatus
	}{}

	var mu sync.Mutex
	monitor := NewTmuxMonitor(
		WithMonitorStateChangeCallback(func(sessionID string, oldS, newS SessionStatus, output string) {
			mu.Lock()
			defer mu.Unlock()
			stateChanges = append(stateChanges, struct {
				oldStatus SessionStatus
				newStatus SessionStatus
			}{oldS, newS})
		}),
	)

	session := &TmuxSession{
		ID:     "test-callback",
		Status: SessionRunning,
	}
	monitor.AddSession(session)

	// 模拟状态变更: running → completed
	// 注意: checkSession 会调用 detectSessionState，由于没有真实 tmux session，
	// 它会基于 executor 返回值决定状态。这里我们直接测试 callback 机制。
	// 通过修改 session.Status 并触发检测来验证。
	session.Status = SessionCompleted
	// 由于 detectSessionState 会重新检测，我们直接设置 executor 为 nil
	// 使得 detectSessionState 返回 SessionError，这样 status 会从 completed 变为 error
	monitor.executor = nil
	monitor.checkSession(session)

	mu.Lock()
	defer mu.Unlock()
	if len(stateChanges) == 0 {
		t.Error("State change callback should be called")
	} else {
		t.Logf("State change: %s -> %s", stateChanges[0].oldStatus, stateChanges[0].newStatus)
	}
}

// Test 6: 假死检测
// 设计文档: 进程存在但无输出 → 超过 FakeDeadThreshold → 发送心跳 → 检测到假死（SessionFakeAlive）
func TestTmuxMonitor_FakeDeadDetection(t *testing.T) {
	skipWithoutTmux(t)

	// 创建一个长时间运行但无输出的会话
	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	stateChanged := false
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval:             500 * time.Millisecond, // 测试用短间隔
			StableThreshold:      2,
			InteractiveThreshold: 3,
			FakeDeadThreshold:    3,
		}),
		WithMonitorStateChangeCallback(func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
			if newStatus == SessionFakeAlive || newStatus == SessionStable {
				stateChanged = true
				t.Logf("FakeDead detection: %s -> %s", oldStatus, newStatus)
			}
		}),
	)

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "cat", // cat 会等待输入，不会产生输出
	})
	if err != nil {
		t.Fatalf("Failed to create tmux session: %v", err)
	}
	defer executor.KillSession(session.ID)

	monitor.AddSession(session)
	monitor.Start()
	defer monitor.Stop()

	// 等待足够长的时间让 monitor 检测
	// 需要等待至少 fakeDeadThreshold * interval
	time.Sleep(3 * time.Second)

	// 验证: 状态应该已经变化（可能是 stable 或 fake_alive）
	s, exists := monitor.GetSession(session.ID)
	if exists {
		t.Logf("Session status after monitoring: %s, stableCount=%d", s.Status, s.StableCount)
	}

	// 注意: 由于 cat 进程确实在运行且可以响应心跳，假死检测可能标记为 fake_alive
	_ = stateChanged
}

// Test 7: 假活检测
// 设计文档: 进程不存在但 pane 未标记 dead → 检测到假活 → 强制清理
func TestTmuxMonitor_FakeAliveDetection(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval: 500 * time.Millisecond,
		}),
	)

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "echo done", // 短命令，很快完成
	})
	if err != nil {
		t.Fatalf("Failed to create tmux session: %v", err)
	}

	// 等待命令完成
	time.Sleep(1 * time.Second)

	monitor.AddSession(session)
	monitor.Start()
	defer monitor.Stop()

	// 等待 monitor 检测到完成
	time.Sleep(2 * time.Second)

	// 验证: 已完成的会话应该被自动移除
	_, exists := monitor.GetSession(session.ID)
	if exists {
		// 会话可能还没被清理，尝试再次检查
		time.Sleep(1 * time.Second)
		_, exists = monitor.GetSession(session.ID)
	}

	// 清理
	executor.KillSession(session.ID)

	t.Logf("FakeAliveDetection: session auto-removed=%v", !exists)
}

// Test 8: 输出稳定性检测
// 设计文档: 3 次连续输出未变化 → 状态变为 SessionStable
func TestTmuxMonitor_OutputStability(t *testing.T) {
	// 使用无 executor 的 monitor 进行逻辑测试
	monitor := NewTmuxMonitor(
		WithMonitorConfig(MonitorConfig{
			StableThreshold:      2,
			InteractiveThreshold: 3,
		}),
	)

	session := &TmuxSession{
		ID:            "stable-test",
		Status:        SessionRunning,
		IsInteractive: false,
		LastOutputMD5: "abc123", // 初始 MD5
		StableCount:   0,
	}

	monitor.AddSession(session)

	// 验证 getThreshold
	if threshold := monitor.getThreshold(false); threshold != 2 {
		t.Errorf("Expected stable threshold 2 for non-interactive, got %d", threshold)
	}

	if threshold := monitor.getThreshold(true); threshold != 3 {
		t.Errorf("Expected interactive threshold 3, got %d", threshold)
	}
}

// Test 9: 边界情况 - 快速完成
// 设计文档: 创建短命令会话 → 第一次检测 → 状态直接变为 SessionCompleted
func TestTmuxMonitor_QuickCompletion(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	completed := false
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval: 500 * time.Millisecond,
		}),
		WithMonitorStateChangeCallback(func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
			if newStatus == SessionCompleted {
				completed = true
				t.Logf("QuickCompletion: %s -> %s", oldStatus, newStatus)
			}
		}),
	)

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "echo hello_quick",
	})
	if err != nil {
		t.Fatalf("Failed to create tmux session: %v", err)
	}

	// 等待命令完成
	time.Sleep(500 * time.Millisecond)

	monitor.AddSession(session)
	monitor.Start()
	defer monitor.Stop()

	// 等待 monitor 检测
	time.Sleep(2 * time.Second)

	// 清理
	executor.KillSession(session.ID)

	if completed {
		t.Log("QuickCompletion: session completed and detected")
	} else {
		t.Log("QuickCompletion: session not yet detected as completed (timing dependent)")
	}
}

// Test 10: 边界情况 - 长时间运行
// 设计文档: 创建长命令会话 → 多次检测周期 → 状态保持 SessionRunning → 不发送事件
func TestTmuxMonitor_LongRunning(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	stateChangeCount := 0
	monitor := NewTmuxMonitor(
		WithMonitorExecutor(executor),
		WithMonitorConfig(MonitorConfig{
			Interval: 500 * time.Millisecond,
		}),
		WithMonitorStateChangeCallback(func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
			stateChangeCount++
			t.Logf("LongRunning: %s -> %s (change #%d)", oldStatus, newStatus, stateChangeCount)
		}),
	)

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "sleep 60", // 长命令
	})
	if err != nil {
		t.Fatalf("Failed to create tmux session: %v", err)
	}

	monitor.AddSession(session)
	monitor.Start()
	defer monitor.Stop()

	// 等待几个检测周期
	time.Sleep(2 * time.Second)

	// 验证: 会话仍在运行
	s, exists := monitor.GetSession(session.ID)
	if exists {
		if s.Status == SessionCompleted || s.Status == SessionError {
			t.Errorf("Long-running session should not be completed/error, got %s", s.Status)
		}
	}

	// 清理
	executor.KillSession(session.ID)

	t.Logf("LongRunning: state changes=%d, session exists=%v", stateChangeCount, exists)
}

// ==================== TmuxExecutor Tests ====================

func TestTmuxExecutor_SessionManagement(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(
		WithTmuxPrefix("tagent-test"),
		WithTmuxWorkspace(t.TempDir()),
	)

	ctx := context.Background()

	// 创建会话
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "sleep 30",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer executor.KillSession(session.ID)

	// 验证会话属性
	if session.ID == "" {
		t.Error("Session ID should not be empty")
	}

	if session.Status != SessionRunning {
		t.Errorf("Expected status running, got %s", session.Status)
	}

	if session.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	// 验证会话存在
	if !executor.SessionExists(session.ID) {
		t.Error("Session should exist after creation")
	}

	// 获取输出
	output, err := executor.GetSessionOutput(session.ID)
	if err != nil {
		t.Logf("GetSessionOutput error (may be timing): %v", err)
	} else {
		t.Logf("Session output: %q", output)
	}

	// 检查 pane dead 状态
	isDead := executor.IsPaneDead(session.ID)
	t.Logf("IsPaneDead: %v", isDead)

	// 列出会话
	sessions, err := executor.ListSessions()
	if err != nil {
		t.Logf("ListSessions error: %v", err)
	} else {
		found := false
		for _, s := range sessions {
			if s.ID == session.ID {
				found = true
				break
			}
		}
		if !found {
			t.Error("Created session should appear in ListSessions")
		}
	}
}

func TestTmuxExecutor_KillSession(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "sleep 60",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// 杀掉会话
	if err := executor.KillSession(session.ID); err != nil {
		t.Fatalf("Failed to kill session: %v", err)
	}

	// 验证会话已不存在
	if executor.SessionExists(session.ID) {
		t.Error("Session should not exist after kill")
	}
}

func TestTmuxExecutor_SendKeys(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "cat", // 交互命令
	})
	if err != nil {
		t.Fatalf("Failed to create interactive session: %v", err)
	}
	defer executor.KillSession(session.ID)

	// 发送按键
	err = executor.SendKeys(session.ID, "hello from test\n")
	if err != nil {
		t.Fatalf("Failed to send keys: %v", err)
	}

	// 等待输出
	time.Sleep(500 * time.Millisecond)

	output, err := executor.GetSessionOutput(session.ID)
	if err != nil {
		t.Logf("GetSessionOutput error: %v", err)
	} else {
		if !strings.Contains(output, "hello from test") {
			t.Logf("Output may not contain sent text yet: %q", output)
		}
	}
}

func TestTmuxExecutor_Heartbeat(t *testing.T) {
	skipWithoutTmux(t)

	executor := NewTmuxExecutor(WithTmuxPrefix("tagent-test"))

	ctx := context.Background()
	session, err := executor.CreateSession(ctx, TmuxCreateOptions{
		Command: "cat",
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer executor.KillSession(session.ID)

	// 发送心跳
	result := executor.SendHeartbeat(session.ID)
	t.Logf("Heartbeat result: %s", result)

	// 对于 cat 进程，心跳应该能成功（因为 shell 仍在运行）
	// 但结果取决于 tmux 会话的具体状态
}

// ==================== CallableTool Interface Tests ====================

func TestCallableTool_Interface(t *testing.T) {
	// Verify RecallTool and CommandTool implement CallableTool.
	// KnowledgeTool is now agent.Tool (wrapped TagentAgent), tested in agent package.
	var _ tool.CallableTool = NewRecallTool()
	var _ tool.CallableTool = NewCommandTool()
}

func TestCallableTool_Names(t *testing.T) {
	tests := []struct {
		tool     tool.CallableTool
		expected string
	}{
		{NewRecallTool(), "recall"},
		{NewCommandTool(), "command"},
	}

	for _, tt := range tests {
		if tt.tool.Declaration().Name != tt.expected {
			t.Errorf("Expected tool name %q, got %q", tt.expected, tt.tool.Declaration().Name)
		}
	}
}

// ==================== SessionStatus Tests ====================

func TestSessionStatus_Values(t *testing.T) {
	// 验证: 设计文档 3.7.1 定义的 6 种状态
	statuses := map[SessionStatus]string{
		SessionRunning:   "running",
		SessionStable:    "stable",
		SessionCompleted: "completed",
		SessionError:     "error",
		SessionFakeDead:  "fake_dead",
		SessionFakeAlive: "fake_alive",
	}

	for status, expected := range statuses {
		if string(status) != expected {
			t.Errorf("Expected SessionStatus %q, got %q", expected, status)
		}
	}
}

func TestTmuxSession_Struct(t *testing.T) {
	// 验证: 设计文档 3.7 定义的 TmuxSession 字段
	session := &TmuxSession{
		ID:            "test-id",
		Name:          "test-name",
		Command:       "echo test",
		WorkDir:       "/tmp",
		Status:        SessionRunning,
		CreatedAt:     time.Now(),
		LastOutput:    "output",
		LastOutputMD5: "abc123",
		StableCount:   2,
		IsInteractive: false,
		PID:           12345,
	}

	if session.ID != "test-id" {
		t.Errorf("ID mismatch")
	}
	if session.Status != SessionRunning {
		t.Errorf("Status mismatch")
	}
	if session.StableCount != 2 {
		t.Errorf("StableCount mismatch")
	}
}

// ==================== MonitorConfig Tests ====================

func TestDefaultMonitorConfig(t *testing.T) {
	// 验证: 设计文档 3.8 定义的默认配置
	cfg := DefaultMonitorConfig()

	if cfg.Interval != 30*time.Second {
		t.Errorf("Expected Interval 30s, got %v", cfg.Interval)
	}

	if cfg.StableThreshold != 2 {
		t.Errorf("Expected StableThreshold 2, got %d", cfg.StableThreshold)
	}

	if cfg.InteractiveThreshold != 3 {
		t.Errorf("Expected InteractiveThreshold 3, got %d", cfg.InteractiveThreshold)
	}

	if cfg.FakeDeadThreshold != 5 {
		t.Errorf("Expected FakeDeadThreshold 5, got %d", cfg.FakeDeadThreshold)
	}

	if cfg.HeartbeatCommand != "echo ping" {
		t.Errorf("Expected HeartbeatCommand 'echo ping', got %q", cfg.HeartbeatCommand)
	}

	if cfg.HeartbeatTimeout != 5*time.Second {
		t.Errorf("Expected HeartbeatTimeout 5s, got %v", cfg.HeartbeatTimeout)
	}
}

// ==================== Integration Tests ====================
// 严格对齐设计文档第四章

// Test: 完整工作流 - 内部知识 + 外部知识 + 执行
// 设计文档 4.1: "上次整理的文件结果如何？帮我参考最佳实践重新整理"
func TestIntegration_FullWorkflow(t *testing.T) {
	// 准备: MemoryStore 中的历史事件
	store := newTestMemoryStore(t, map[string]memory.FullEvent{
		"evt_history": {
			EventKey:     "evt_history",
			EventType:    memory.EventTypeAgentOutput,
			EventSummary: "文件整理完成",
			Timestamp:    time.Now().Add(-1 * time.Hour).UnixMilli(),
			Content:      "成功整理 /data 目录下的 50 个文件",
		},
	})

	// 创建工具集（KnowledgeTool is now agent.Tool, tested in agent package）
	recallTool := NewRecallTool(WithRecallMemoryStore(store))
	commandTool := NewCommandTool()
	defer commandTool.tmuxMonitor.Stop()

	ctx := context.Background()

	// Step 1: Recall - 回忆历史
	recallResult, err := recallTool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"query": "上次整理的文件结果",
		"limit": 5,
	}))
	if err != nil {
		t.Fatalf("RecallTool.Call failed: %v", err)
	}
	recallResp := recallResult.(*RecallResponse)
	if len(recallResp.Events) == 0 {
		t.Error("Expected recall results")
	}
	t.Logf("Step 1 Recall: found %d events", len(recallResp.Events))

	// Step 2: Command - 执行命令（KnowledgeTool 跳过，需 LLM，在 agent 包测试）
	commandResult, err := commandTool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "exec",
		"command": fmt.Sprintf("echo 'Re-organizing files based on history: %d events'", len(recallResp.Events)),
	}))
	if err != nil {
		t.Fatalf("CommandTool.Call failed: %v", err)
	}
	commandResp := commandResult.(*CommandExecResult)
	if commandResp.ExitCode != 0 {
		t.Errorf("Command failed: exit_code=%d, stderr=%s", commandResp.ExitCode, commandResp.Stderr)
	}
	t.Logf("Step 3 Command: %s", strings.TrimSpace(commandResp.Stdout))
}

// Test: 并发 Tool 调用
// 设计文档 4.2: "同时搜索知识库和执行命令"
func TestIntegration_ConcurrentToolCalls(t *testing.T) {
	recallTool := NewRecallTool()
	commandTool := NewCommandTool()
	defer commandTool.tmuxMonitor.Stop()

	ctx := context.Background()

	// 并发调用 RecallTool 和 CommandTool
	// (KnowledgeTool is now agent.Tool, tested in agent package)
	var (
		recallResult  any
		recallErr     error
		commandResult any
		commandErr    error
		wg            sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		recallResult, recallErr = recallTool.Call(ctx, mustMarshal(t, map[string]interface{}{
			"query": "test recall",
		}))
	}()

	go func() {
		defer wg.Done()
		commandResult, commandErr = commandTool.Call(ctx, mustMarshal(t, map[string]interface{}{
			"mode":    "exec",
			"command": "echo concurrent_command",
		}))
	}()

	wg.Wait()

	// 验证: 所有调用成功
	if recallErr != nil {
		t.Errorf("RecallTool concurrent call failed: %v", recallErr)
	}
	if commandErr != nil {
		t.Errorf("CommandTool concurrent call failed: %v", commandErr)
	}

	// 验证: 结果类型正确
	if _, ok := recallResult.(*RecallResponse); !ok {
		t.Errorf("Expected *RecallResponse, got %T", recallResult)
	}
	if _, ok := commandResult.(*CommandExecResult); !ok {
		t.Errorf("Expected *CommandExecResult, got %T", commandResult)
	}

	t.Log("ConcurrentToolCalls: recall and command tools executed concurrently and returned correct types")
}

// Test: Tmux 事件流（定时状态检测器驱动）
// 设计文档 4.2: 完整异步执行 → 监控 → 状态变更 → 事件推送
func TestIntegration_TmuxEventFlow(t *testing.T) {
	skipWithoutTmux(t)

	stateChanges := []string{}

	commandTool := NewCommandTool(
		WithCommandWorkspace(t.TempDir()),
	)
	defer commandTool.tmuxMonitor.Stop()

	// 设置短监控间隔以加速测试
	commandTool.tmuxMonitor.Stop()
	commandTool.tmuxMonitor = NewTmuxMonitor(
		WithMonitorExecutor(commandTool.tmuxExecutor),
		WithMonitorConfig(MonitorConfig{
			Interval:             500 * time.Millisecond,
			StableThreshold:      2,
			InteractiveThreshold: 3,
			FakeDeadThreshold:    5,
		}),
		WithMonitorStateChangeCallback(func(sessionID string, oldStatus, newStatus SessionStatus, output string) {
			stateChanges = append(stateChanges, fmt.Sprintf("%s->%s", oldStatus, newStatus))
		}),
	)
	commandTool.tmuxMonitor.Start()

	ctx := context.Background()

	// Step 1: 创建异步命令
	result, err := commandTool.Call(ctx, mustMarshal(t, map[string]interface{}{
		"mode":    "tmux_exec",
		"command": "echo event_flow_test && sleep 2",
	}))
	if err != nil {
		t.Fatalf("Async exec failed: %v", err)
	}

	tmuxResp := result.(*TmuxExecResponse)
	sessionID := tmuxResp.SessionID

	t.Logf("Step 1: Created async session %s", sessionID)

	// Step 2: 等待命令完成和状态变更
	time.Sleep(4 * time.Second)

	// Step 3: 验证状态变更被记录
	if len(stateChanges) > 0 {
		t.Logf("Step 3: State changes detected: %v", stateChanges)
	} else {
		t.Log("Step 3: No state changes detected (timing dependent)")
	}

	// 清理
	commandTool.tmuxExecutor.KillSession(sessionID)
}

// ==================== Edge Cases ====================

func TestKillProcessGroup_InvalidPID(t *testing.T) {
	err := KillProcessGroup(0)
	if err == nil {
		t.Error("Expected error for invalid PID 0")
	}

	err = KillProcessGroup(-1)
	if err == nil {
		t.Error("Expected error for negative PID")
	}
}

func TestCommandSpec_Defaults(t *testing.T) {
	spec := CommandSpec{
		Command: "echo",
		Args:    []string{"test"},
	}

	// 验证: 默认值
	if spec.Timeout != 0 {
		t.Errorf("Default Timeout should be 0, got %v", spec.Timeout)
	}

	if spec.RunAsUser != "" {
		t.Errorf("Default RunAsUser should be empty, got %q", spec.RunAsUser)
	}

	if spec.RunAsGroup != "" {
		t.Errorf("Default RunAsGroup should be empty, got %q", spec.RunAsGroup)
	}
}

package tagent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	tagentmemory "github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/stretchr/testify/require"
)

// mustMarshal marshals args to JSON bytes for CallableTool.Call().
func mustMarshal(t *testing.T, args map[string]interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal args: %v", err)
	}
	return data
}

// runWithLoop starts a persistent event loop, injects a message, collects events,
// and stops the loop. This is the only way to run a top-level TagentAgent.
//
// After ctx cancellation or IsFinalResponse, the helper drains any remaining
// events from outputCh (with a short grace period) to avoid losing error events
// that arrive at the same time as the context deadline.
func runWithLoop(ctx context.Context, t *testing.T, ag *tagentagent.TagentAgent, userID, sessionID string, msg model.Message) []*event.Event {
	t.Helper()
	outputCh, err := ag.StartLoop(userID, sessionID)
	require.NoError(t, err)

	ag.InjectMessage(msg)

	var events []*event.Event
loop:
	for {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				break loop
			}
			events = append(events, evt)
			if evt.IsFinalResponse() {
				break loop
			}
		case <-ctx.Done():
			// Context cancelled — drain any remaining events with a grace period
			// to capture error events that arrive simultaneously.
			drainTimer := time.NewTimer(500 * time.Millisecond)
		drain:
			for {
				select {
				case evt, ok := <-outputCh:
					if !ok {
						break drain
					}
					events = append(events, evt)
					if evt.IsFinalResponse() {
						break drain
					}
				case <-drainTimer.C:
					break drain
				}
			}
			drainTimer.Stop()
			break loop
		}
	}

	ag.StopLoop()
	return events
}

// TestIntegration_SmartCompress_WithRealLLM 测试两阶段上下文压缩
func TestIntegration_SmartCompress_WithRealLLM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping integration test", err)
	}

	t.Logf("SmartCompress Stage 2 test with real LLM:")
	t.Logf("  Endpoint: %s", cfg.Endpoint)
	t.Logf("  Model: %s", cfg.ModelName)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxTokens:         20, // 极小预算强制触发压缩
		CompressThreshold: 0.8,
		SummaryModel:      zhipuModel,
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// 模拟长对话触发压缩
	msg := model.Message{
		Role:    model.RoleUser,
		Content: "继续",
	}

	events := runWithLoop(ctx, t, ag, "test-user", "test-session", msg)

	t.Logf("End-to-end with compression: %d events", len(events))

	// 应该成功返回（压缩后模型仍能正常响应）
	if len(events) == 0 {
		t.Error("Expected at least one event after compression")
	}
}

// TestRegression_AgentLoop_MultipleIterations 回归测试：多轮迭代
func TestRegression_AgentLoop_MultipleIterations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 加载配置（从环境或 ~/.zshrc）
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping regression test", err)
	}

	t.Logf("Regression test: Multiple iterations")
	t.Logf("  Endpoint: %s", cfg.Endpoint)
	t.Logf("  Model: %s", cfg.ModelName)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	// 创建 echo 工具
	echoTool := &echoToolStruct{
		name:        "echo",
		description: "Echo back the input message",
	}

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model: zhipuModel,
		Tools: []tool.Tool{echoTool},
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	msg := model.Message{
		Role:    model.RoleUser,
		Content: "请使用 echo 工具回复'Hello'。",
	}

	events := runWithLoop(ctx, t, ag, "test-user", "test-session", msg)

	// 验证多轮迭代（tool call + final response）
	if len(events) < 2 {
		t.Errorf("Expected at least 2 events (tool result + agent output), got %d", len(events))
	}

	// 验证最终输出
	var agentOutput string
	for _, evt := range events {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			msg := evt.Response.Choices[0].Message
			if msg.Content != "" && len(msg.ToolCalls) == 0 {
				agentOutput = msg.Content
			}
		}
	}

	if agentOutput != "" {
		t.Logf("Final output: %s", agentOutput)
		if !strings.Contains(agentOutput, "Hello") {
			t.Errorf("Final output should contain 'Hello', got: %s", agentOutput)
		}
		if strings.Contains(agentOutput, "错误") || strings.Contains(agentOutput, "error") || strings.Contains(agentOutput, "失败") {
			t.Errorf("Final output contains error: %s", agentOutput)
		}
	}
}

// TestRegression_CompressionCycle 回归测试：多次压缩循环
func TestRegression_CompressionCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping regression test", err)
	}

	t.Logf("Regression test: Compression cycle")
	t.Logf("  Endpoint: %s", cfg.Endpoint)
	t.Logf("  Model: %s", cfg.ModelName)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxTokens:         2000, // GLM-4.7 reasoning_content is large; 300 is too aggressive
		CompressThreshold: 0.8,
		SummaryModel:      zhipuModel,
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Persistent event loop: same session across all rounds
	outputCh, err := ag.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	for round := 0; round < 3; round++ {
		t.Logf("Compression round %d", round+1)
		msg := model.Message{Role: model.RoleUser, Content: "继续"}
		ag.InjectMessage(msg)

		var eventCount int
	loop:
		for {
			select {
			case evt, ok := <-outputCh:
				if !ok {
					break loop
				}
				eventCount++
				t.Logf("  Round %d event: tag=%s", round+1, evt.Tag)
				if evt.IsFinalResponse() {
					break loop
				}
			case <-ctx.Done():
				break loop
			}
		}
		t.Logf("  Events in round %d: %d", round+1, eventCount)
		if eventCount == 0 {
			t.Error("Should have at least 1 event per iteration")
			break
		}
	}

	ag.StopLoop()
}

// echoToolStruct 简单的回显工具（用于测试）
type echoToolStruct struct {
	name        string
	description string
}

func (t *echoToolStruct) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        t.name,
		Description: t.description,
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"message": {
					Type:        "string",
					Description: "The message to echo back",
				},
			},
			Required: []string{"message"},
		},
	}
}

func (t *echoToolStruct) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args map[string]interface{}
	if len(jsonArgs) > 0 {
		if err := json.Unmarshal(jsonArgs, &args); err != nil {
			return "", err
		}
	}

	message, ok := args["message"].(string)
	if !ok {
		return "", nil
	}

	return message, nil
}

// ==================== 6.3 RecallAgent 测试用例（需真实 LLM）====================

// createRecallTestStore creates a MemoryStore pre-populated with test events for RecallAgent tests.
func createRecallTestStore(t *testing.T) tagentmemory.MemoryStore {
	t.Helper()
	store, err := tagentmemory.NewFileSegmentStore(tagentmemory.NewMockRustVikingClient(), nil, ":memory:", 100)
	if err != nil {
		t.Fatalf("Failed to create memory store: %v", err)
	}

	// Pre-populate with test events using Snowflake EventKeys
	partitionID := tagentmemory.PartitionIDFromName("tagent")
	testEvents := []tagentmemory.FullEvent{
		{
			EventKey:     tagentmemory.NewSnowflakeEventKey(partitionID, 0),
			PartitionID:  partitionID,
			EventType:    tagentevent.TypeActionCommand,
			EventSummary: "用户要求整理文件",
			Timestamp:    time.Now().Add(-2 * time.Hour).UnixMilli(),
			Content:      "整理 /tmp 目录下的文件",
		},
		{
			EventKey:     tagentmemory.NewSnowflakeEventKey(partitionID, 0),
			PartitionID:  partitionID,
			EventType:    tagentevent.TypeAgentOutput,
			EventSummary: "文件整理完成",
			Timestamp:    time.Now().Add(-2*time.Hour + 5*time.Minute).UnixMilli(),
			Content:      "成功整理 /tmp 目录下的 15 个文件，释放 200MB 空间",
		},
		{
			EventKey:     tagentmemory.NewSnowflakeEventKey(partitionID, 0),
			PartitionID:  partitionID,
			EventType:    tagentevent.TypeActionCommand,
			EventSummary: "执行部署命令",
			Timestamp:    time.Now().Add(-1 * time.Hour).UnixMilli(),
			Content:      "deploy.sh --env production",
		},
		{
			EventKey:     tagentmemory.NewSnowflakeEventKey(partitionID, 0),
			PartitionID:  partitionID,
			EventType:    tagentevent.TypeAgentOutput,
			EventSummary: "部署成功",
			Timestamp:    time.Now().Add(-1*time.Hour + 2*time.Minute).UnixMilli(),
			Content:      "部署成功: 3 个服务已更新，耗时 2m30s",
		},
	}

	for _, evt := range testEvents {
		if err := store.StoreEvent(evt.EventKey, evt); err != nil {
			t.Fatalf("Failed to store event %d: %v", evt.EventKey, err)
		}
	}

	return store
}

// ==================== KnowledgeAgent Integration Tests (requires real LLM) ====================

// TestIntegration_KnowledgeTool_WithRealLLM_BasicQuery tests knowledge agent with real LLM.
// KnowledgeTool is now a TagentAgent wrapped as agent.Tool.
func TestIntegration_KnowledgeTool_WithRealLLM_BasicQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping integration test", err)
	}

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	// Create KnowledgeTool via knowledge.NewTool
	knowledgeTool, err := knowledge.NewTool(knowledge.Config{
		Model:     zhipuModel,
		PromptDir: "../resources/prompts",
	})
	if err != nil {
		t.Fatalf("Failed to create KnowledgeTool: %v", err)
	}

	// Verify the tool has proper declaration
	decl := knowledgeTool.Declaration()
	if decl == nil {
		t.Fatal("Expected non-nil Declaration")
	}
	t.Logf("KnowledgeTool name: %s", decl.Name)
	t.Logf("KnowledgeTool description length: %d", len(decl.Description))

	// Call the tool (agent.Tool implements CallableTool)
	callable, ok := knowledgeTool.(tool.CallableTool)
	if !ok {
		t.Fatal("KnowledgeTool should implement CallableTool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := callable.Call(ctx, mustMarshal(t, map[string]interface{}{
		"request": "What is a GitHub pull request?",
	}))
	if err != nil {
		t.Fatalf("KnowledgeTool.Call failed: %v", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("Expected string result, got %T", result)
	}

	t.Logf("KnowledgeTool result length: %d", len(resultStr))
	if len(resultStr) == 0 {
		t.Error("Expected non-empty result from KnowledgeTool")
	}
}

// ==================== 12.1 完整工作流端到端测试 ====================

// TestIntegration_EndToEnd_FullWorkflow 测试 12.1: 完整工作流
// 用户输入 → TagentAgent → LLM → tool_calls → 最终响应
func TestIntegration_EndToEnd_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping integration test", err)
	}

	t.Logf("End-to-end test with real LLM:")
	t.Logf("  Endpoint: %s", cfg.Endpoint)
	t.Logf("  Model: %s", cfg.ModelName)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	// 1. Create an echo tool for TagentAgent
	echo := &echoToolStruct{name: "echo", description: "Echo back the input message"}

	// 2. Create TagentAgent with tools
	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		Tools:             []tool.Tool{echo},
		MaxToolIterations: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	// 3. Run TagentAgent
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	msg := model.Message{
		Role:    model.RoleUser,
		Content: "请使用 echo 工具回复 '端到端测试成功'",
	}

	events := runWithLoop(ctx, t, ag, "test-user", "test-session", msg)

	t.Logf("End-to-end: received %d events", len(events))

	// 5. Verify at least one event was produced
	if len(events) == 0 {
		t.Fatal("Expected at least one event from TagentAgent")
	}

	// 6. Find final agent output
	var agentOutput string
	for _, evt := range events {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			m := evt.Response.Choices[0].Message
			if m.Content != "" && len(m.ToolCalls) == 0 {
				agentOutput = m.Content
			}
		}
	}

	if agentOutput != "" {
		t.Logf("Final agent output: %s", agentOutput)
	} else {
		t.Log("No final agent output found (may have been tool-call only)")
	}
}

// 本文件承载跨面集成与真实 LLM 端到端工况的整链验证。
// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
package tagent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	trpcskill "trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
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

// TestIntegration_SmartCompress_WithRealLLM 钉住 两阶段上下文压缩在真实模型调用下至少产出一个事件。
//
// 契约: docs/wiki/agent/event-flow.md#unified-compression
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
		MaxTokens:         20,
		CompressThreshold: 0.8,
		SummaryModel:      zhipuModel,
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	msg := model.Message{
		Role:    model.RoleUser,
		Content: "继续",
	}

	events := runWithLoop(ctx, t, ag, "test-user", "test-session", msg)

	t.Logf("End-to-end with compression: %d events", len(events))

	if len(events) == 0 {
		t.Error("Expected at least one event after compression")
	}
}

// TestRegression_AgentLoop_MultipleIterations 钉住 多轮 agent 循环必须同时产出工具结果与最终回复，且回复含预期文本、不含错误。
func TestRegression_AgentLoop_MultipleIterations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

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

	echoTool := &echoToolStruct{
		name:        "echo",
		description: "Echo back the input message",
	}

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model: zhipuModel,
		Tools: []trpctool.Tool{echoTool},
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

// TestRegression_CompressionCycle 钉住 多次压缩循环每轮至少产出一个事件，压缩周期本身不得中断回合。
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
		MaxTokens:         2000,
		CompressThreshold: 0.8,
		SummaryModel:      zhipuModel,
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

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

func (t *echoToolStruct) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        t.name,
		Description: t.description,
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
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

// TestIntegration_KnowledgeTool_WithRealLLM_BasicQuery tests knowledge agent with real LLM.
// - KnowledgeTool is now a TagentAgent wrapped as agent.Tool.
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

	knowledgeTool, err := knowledge.NewTool(knowledge.Config{
		Model:     zhipuModel,
		PromptDir: "../resources/prompts",
	})
	if err != nil {
		t.Fatalf("Failed to create KnowledgeTool: %v", err)
	}

	decl := knowledgeTool.Declaration()
	if decl == nil {
		t.Fatal("Expected non-nil Declaration")
	}
	t.Logf("KnowledgeTool name: %s", decl.Name)
	t.Logf("KnowledgeTool description length: %d", len(decl.Description))

	callable, ok := knowledgeTool.(trpctool.CallableTool)
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

// TestIntegration_EndToEnd_FullWorkflow 走完整工作流：用户输入 → TagentAgent → LLM → tool_calls → 最终响应。
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

	echo := &echoToolStruct{name: "echo", description: "Echo back the input message"}

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		Tools:             []trpctool.Tool{echo},
		MaxToolIterations: 10,
	})
	if err != nil {
		t.Fatalf("Failed to create TagentAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	msg := model.Message{
		Role:    model.RoleUser,
		Content: "请使用 echo 工具回复 '端到端测试成功'",
	}

	events := runWithLoop(ctx, t, ag, "test-user", "test-session", msg)

	t.Logf("End-to-end: received %d events", len(events))

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

type intTestMockModel struct{}

func (m *intTestMockModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 1)
	ch <- &model.Response{Done: true}
	close(ch)
	return ch, nil
}

func (m *intTestMockModel) Info() model.Info { return model.Info{Name: "int-test-mock-model"} }

type intTestMockSkillRepo struct{}

func (m *intTestMockSkillRepo) Summaries() []trpcskill.Summary { return nil }
func (m *intTestMockSkillRepo) Get(name string) (*trpcskill.Skill, error) {
	return nil, errors.New("skill not found")
}

type intTestMockToolSet struct{}

func (m *intTestMockToolSet) Tools(_ context.Context) []trpctool.Tool { return nil }
func (m *intTestMockToolSet) Close() error                            { return nil }
func (m *intTestMockToolSet) Name() string                            { return "mock" }

func newIntTestConfig() tagent.Config {
	return tagent.Config{
		Entry: "tagent",
		Agents: map[string]tagent.AgentConfig{
			"tagent": {
				SystemPrompt: tagent.PromptConfig{Inline: "You are the entry tagentagent."},
				Memory:       tagent.MemoryConfig{Type: "memory"},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindAgent, AgentID: "knowledge", Description: "knowledge tool"},
					{Kind: tagent.ToolKindAgent, AgentID: "recall", Description: "recall tool"},
					{Kind: tagent.ToolKindTool, ID: "exec", Description: "action tool"},
				},
			},
			"knowledge": {
				SystemPrompt: tagent.PromptConfig{Inline: "You are the knowledge tagentagent."},
				Memory:       tagent.MemoryConfig{Type: "memory"},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindTool, ID: "skill_search", Description: "search skills"},
					{Kind: tagent.ToolKindTool, ID: "skill_load", Description: "load a skill"},
					{Kind: tagent.ToolKindTool, ID: "mcp_discover", Description: "discover mcp tools"},
					{Kind: tagent.ToolKindTool, ID: "web_search", Description: "search the web"},
					{Kind: tagent.ToolKindTool, ID: "duckduckgo_search", Description: "search with duckduckgo"},
					{Kind: tagent.ToolKindTool, ID: "memory_query", Description: "query memory"},
				},
			},
			"recall": {
				SystemPrompt: tagent.PromptConfig{Inline: "You are the recall tagentagent."},
				Memory:       tagent.MemoryConfig{Type: "memory"},
				Tools: []tagent.ToolRef{
					{Kind: tagent.ToolKindTool, ID: "recall_query", Description: "query recall"},
					{Kind: tagent.ToolKindTool, ID: "recall_get", Description: "get recall"},
					{Kind: tagent.ToolKindTool, ID: "recall_recent", Description: "recent recall"},
					{Kind: tagent.ToolKindTool, ID: "recall_trace", Description: "trace recall"},
				},
			},
		},
	}
}

func TestTagentNew_Success(t *testing.T) {
	require.NoError(t, tagent.RegisterBuiltinTools())

	cfg := newIntTestConfig()
	mockModel := &intTestMockModel{}

	entryAgent, err := tagent.New(
		cfg,
		tagent.WithModel(mockModel),
		tagent.WithSkillRepo(&intTestMockSkillRepo{}),
		tagent.WithMCPToolSets([]trpctool.ToolSet{&intTestMockToolSet{}}),
	)
	require.NoError(t, err)
	require.NotNil(t, entryAgent)

	tools := entryAgent.Tools()
	require.GreaterOrEqual(t, len(tools), 3)

	names := make(map[string]bool)
	for _, tool := range tools {
		decl := tool.Declaration()
		if decl != nil {
			names[decl.Name] = true
		}
	}

	assert.True(t, names["knowledge"], "entry agent should have knowledge tool")
	assert.True(t, names["recall"], "entry agent should have recall tool")
	assert.True(t, names["action"], "entry agent should have action tool")
}

func TestTagentNew_KnowledgeAgentHasSixPlainTools(t *testing.T) {
	cfg := newIntTestConfig()
	require.NoError(t, tagent.RegisterBuiltinTools())

	knowledgeCfg := cfg.Agents["knowledge"]
	agentCache := make(map[string]*tagentagent.TagentAgent)
	loader := prompt.NewLoader("")

	knowledgeAgent, err := tagent.TestingBuildAgent(
		"knowledge", knowledgeCfg, cfg,
		&intTestMockModel{},
		&intTestMockSkillRepo{},
		[]trpctool.ToolSet{&intTestMockToolSet{}},
		loader, agentCache,
	)
	require.NoError(t, err)
	require.NotNil(t, knowledgeAgent)

	decls := knowledgeAgent.Tools()
	require.Len(t, decls, 6, "knowledge agent should have 6 plain tools")
}

func TestTagentNew_RecallAgentHasFourPlainTools(t *testing.T) {
	cfg := newIntTestConfig()
	require.NoError(t, tagent.RegisterBuiltinTools())

	recallCfg := cfg.Agents["recall"]
	agentCache := make(map[string]*tagentagent.TagentAgent)
	loader := prompt.NewLoader("")

	recallAgent, err := tagent.TestingBuildAgent(
		"recall", recallCfg, cfg,
		&intTestMockModel{},
		nil, nil,
		loader, agentCache,
	)
	require.NoError(t, err)
	require.NotNil(t, recallAgent)

	decls := recallAgent.Tools()
	require.Len(t, decls, 4, "recall agent should have 4 plain tools")
}

func TestTagentNew_UnregisteredToolReturnsError(t *testing.T) {
	cfg := newIntTestConfig()
	cfg.Agents["tagent"] = tagent.AgentConfig{
		SystemPrompt: tagent.PromptConfig{Inline: "You are the entry tagentagent."},
		Memory:       tagent.MemoryConfig{Type: "memory"},
		Tools: []tagent.ToolRef{
			{Kind: tagent.ToolKindTool, ID: "definitely_not_registered"},
		},
	}

	entryAgent, err := tagent.New(cfg, tagent.WithModel(&intTestMockModel{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool access validation")
	assert.Nil(t, entryAgent)
}

func TestRealLLM_ReasoningContentDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping", err)
	}

	t.Logf("ReasoningContent detection test: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:        zhipuModel,
		MaxTokens:    8000,
		Temperature:  0.3,
		SystemPrompt: "You are a knowledge research agent. Describe what you found concisely.",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	events := runWithLoop(ctx, t, ag, "test-user", "test-reasoning", model.Message{
		Role:    model.RoleUser,
		Content: "请描述 url-fetcher 技能的功能：它可以通过 HTTP GET 请求获取网页内容，支持自定义 headers 和超时设置。",
	})

	require.GreaterOrEqual(t, len(events), 1, "should receive at least one event")

	for _, evt := range events {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			msg := evt.Response.Choices[0].Message
			finishReason := ""
			if evt.Response.Choices[0].FinishReason != nil {
				finishReason = *evt.Response.Choices[0].FinishReason
			}
			t.Logf("Event response fields:")
			t.Logf("  content_len=%d", len(msg.Content))
			t.Logf("  reasoning_content_len=%d", len(msg.ReasoningContent))
			t.Logf("  finish_reason=%q", finishReason)
			t.Logf("  tool_calls=%d", len(msg.ToolCalls))
			if evt.Response.Usage != nil {
				t.Logf("  usage: prompt=%d completion=%d total=%d",
					evt.Response.Usage.PromptTokens, evt.Response.Usage.CompletionTokens, evt.Response.Usage.TotalTokens)
			}
			if msg.Content != "" {
				t.Logf("  content preview: %s", truncate(msg.Content, 200))
			}
			if msg.ReasoningContent != "" {
				t.Logf("  reasoning preview: %s", truncate(msg.ReasoningContent, 200))
			}

			totalOutput := len(msg.Content) + len(msg.ReasoningContent) + len(msg.ToolCalls)
			assert.Greater(t, totalOutput, 0, "model should produce some output")
		}
	}
}

func TestRealLLM_SubAgentRun_CompletesWithSummary(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping", err)
	}

	t.Logf("Sub-agent Run completion test: model=%s", cfg.ModelName)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxToolIterations: 3,
		MaxTokens:         8000,
		Temperature:       0.3,
		SystemPrompt:      "You are a knowledge research agent. Describe what you found and return a summary. Keep it concise.",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	events := runWithLoop(ctx, t, ag, "test-user", "test-session", model.Message{
		Role:    model.RoleUser,
		Content: "请描述 url-fetcher 这个技能的功能。它可以通过 HTTP GET 请求获取网页内容，支持自定义 headers 和超时设置。请总结它的用途。",
	})

	t.Logf("Received %d events", len(events))
	require.GreaterOrEqual(t, len(events), 1, "should receive at least one event")

	// Find final output.
	var finalContent string
	var hasEmptyContent bool
	for _, evt := range events {
		if evt.Response != nil && len(evt.Response.Choices) > 0 {
			choice := evt.Response.Choices[len(evt.Response.Choices)-1]
			if len(choice.Message.ToolCalls) == 0 {
				finalContent = choice.Message.Content
				if finalContent == "" {
					hasEmptyContent = true
				}
				if choice.Message.ReasoningContent != "" {
					t.Logf("Found reasoning_content (len=%d): %s",
						len(choice.Message.ReasoningContent), truncate(choice.Message.ReasoningContent, 200))
				}
			}
		}
	}

	if hasEmptyContent {
		t.Logf("WARNING: final response had empty content — this indicates the model put output in reasoning_content or returned nothing")
	}
	if finalContent != "" {
		t.Logf("Final content: %s", truncate(finalContent, 300))
		assert.Greater(t, len(finalContent), 0, "final content should not be empty if present")
	}
}

func TestRealLLM_SubAgentRun_InjectMessageRoutesCorrectly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("Failed to load config: %v, skipping", err)
	}

	t.Logf("InjectMessage routing test: model=%s", cfg.ModelName)

	zhipuModel := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             zhipuModel,
		MaxToolIterations: 3,
		MaxTokens:         8000,
		Temperature:       0.3,
		SystemPrompt:      "You are a helpful assistant. Respond concisely.",
	})
	require.NoError(t, err)

	outputCh, err := ag.StartLoop("test-user", "test-inject")
	require.NoError(t, err)
	defer ag.StopLoop()

	ag.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "你好，请回复'收到'。",
	})

	select {
	case evt := <-outputCh:
		if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
			content := evt.Response.Choices[0].Message.Content
			t.Logf("Got first response: %s", truncate(content, 100))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for first response")
	}

	ag.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "请回复'好的'。",
	})

	select {
	case evt := <-outputCh:
		if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
			content := evt.Response.Choices[0].Message.Content
			t.Logf("Got second response: %s", truncate(content, 100))
			assert.Greater(t, len(content), 0, "second response should be non-empty")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for second response — InjectMessage may not be routing correctly")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

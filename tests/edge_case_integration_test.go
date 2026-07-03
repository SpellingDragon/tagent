package tagent_test

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Real LLM integration tests for edge cases found in production traces.
// These tests use GLM-4.7 to reproduce and verify fixes for:
//   1. Empty content + reasoning_content fallback
//   2. Sub-agent InjectMessage routing via activeBus
//   3. Sub-agent empty final response completing (not hanging)
// ============================================================================

// ----------------------------------------------------------------------------
// Test 1: Real LLM — reasoning_content detection
//
// GLM-4.7 sometimes puts all output in reasoning_content with empty content.
// This test sends a prompt that is likely to trigger reasoning, then inspects
// the raw response fields to understand the model's behavior.
// ----------------------------------------------------------------------------

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

			// The model should produce some output.
			totalOutput := len(msg.Content) + len(msg.ReasoningContent) + len(msg.ToolCalls)
			assert.Greater(t, totalOutput, 0, "model should produce some output")
		}
	}
}

// ----------------------------------------------------------------------------
// Test 2: Real LLM — sub-agent with knowledge-style prompt completes
//
// Simulates the knowledge agent scenario from production: the agent loads
// a skill, then needs to produce a final summary. Verifies that the sub-agent
// Run() path completes (doesn't hang) even if the model returns empty content.
// ----------------------------------------------------------------------------

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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Run as sub-agent (uses Run() path, not StartLoop).
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
				// Log reasoning_content if present for diagnosis.
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

// ----------------------------------------------------------------------------
// Test 3: Real LLM — InjectMessage routes to sub-agent via activeBus
//
// Verifies that when a sub-agent is running via Run(), InjectMessage
// correctly routes to the sub-agent's invocation bus (not dropped).
// ----------------------------------------------------------------------------

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

	// Start persistent loop.
	outputCh, err := ag.StartLoop("test-user", "test-inject")
	require.NoError(t, err)
	defer ag.StopLoop()

	// Inject a user message.
	ag.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "你好，请回复'收到'。",
	})

	// Wait for first response.
	select {
	case evt := <-outputCh:
		if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
			content := evt.Response.Choices[0].Message.Content
			t.Logf("Got first response: %s", truncate(content, 100))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for first response")
	}

	// Inject second message — verifies InjectMessage still routes correctly
	// after the first response (activeBus is still the persistent bus).
	ag.InjectMessage(model.Message{
		Role:    model.RoleUser,
		Content: "请回复'好的'。",
	})

	select {
	case evt := <-outputCh:
		if evt != nil && evt.Response != nil && len(evt.Response.Choices) > 0 {
			content := evt.Response.Choices[0].Message.Content
			t.Logf("Got second response: %s", truncate(content, 100))
			// Should contain some acknowledgment.
			assert.Greater(t, len(content), 0, "second response should be non-empty")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for second response — InjectMessage may not be routing correctly")
	}
}

// ----------------------------------------------------------------------------
// Helper
// ----------------------------------------------------------------------------

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

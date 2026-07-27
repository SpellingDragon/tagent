package tagent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	trpcagent "trpc.group/trpc-go/trpc-agent-go/agent"

	tagent "github.com/SpellingDragon/tagent"
	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/prompt"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/stretchr/testify/require"
)

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

// TestRealLLM_PlanReentry_RestoresPriorContext verifies the plan re-entry
// capability end-to-end with a real model: a resumed plan agent receives its
// prior round as restored external context and continues coherently — it
// retrieves a task established in round 1 even though the round-2 instruction
// never names it. This is the model-side complement to the white-box wiring
// test (agent/subagent_resume_test.go): together they prove re-entry both
// delivers the context AND that the model uses it.
func TestRealLLM_PlanReentry_RestoresPriorContext(t *testing.T) {
	if testing.Short() {
		t.Skip("real-LLM test; skipped in -short")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("LoadConfig: %v", err)
	}
	m := openai.New(cfg.ModelName, openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))

	require.NoError(t, tagent.RegisterBuiltinTools())

	planCfg := tagent.AgentConfig{
		SystemPrompt:      tagent.PromptConfig{Inline: "你是 plan agent，负责管理 openspec 工作计划的创建、更新与归档。你只产出计划与规格文档，回答简洁。"},
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
	planAgent, err := tagent.TestingBuildAgent("plan", planCfg, fullCfg, m, nil, nil, prompt.NewLoader(""), cache)
	require.NoError(t, err)

	const distinctive = "校验85条事实清单"

	// Round 1: establish a plan whose first task has a distinctive name.
	out1 := runPlanInvocation(t, planAgent, trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage(
			"建立一个 openspec 工作计划，第 1 个任务必须命名为『"+distinctive+"』。只需回复确认并写出这个任务名。")),
	))
	t.Logf("round1: %s", truncStr(out1, 300))
	require.Contains(t, out1, distinctive, "round 1 must establish the distinctive task")

	// Round 2 (re-entry): the resume instruction does NOT name the task — the
	// agent can only produce it by reading the restored prior-round context.
	// This mirrors exactly what subagentResume injects (task_round entry).
	restored := []tagentagent.ExternalContextEntry{{
		EventType:    "task_round",
		EventSummary: "〔本任务上一轮〕指令: 建立一个 openspec 工作计划，第 1 个任务命名为『" + distinctive + "』\n结果: " + truncStr(out1, 800),
	}}
	serialized, err := json.Marshal(restored)
	require.NoError(t, err)

	out2 := runPlanInvocation(t, planAgent, trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage(
			"你上一轮已经建立了一个计划。请先说出该计划第 1 个任务的准确名称，再把它细化为 3 个子步骤。")),
		trpcagent.WithInvocationRunOptions(trpcagent.RunOptions{
			RuntimeState: map[string]any{
				tagentagent.ExternalContextKey: json.RawMessage(serialized),
			},
		}),
	))
	t.Logf("round2: %s", truncStr(out2, 500))

	// Coherent continuation: the distinctive task name — absent from the
	// round-2 instruction — must be retrieved from the restored context.
	require.True(t, strings.Contains(out2, distinctive),
		"resumed plan agent must retrieve the prior task from restored context (re-entry); got: %s", truncStr(out2, 400))
}

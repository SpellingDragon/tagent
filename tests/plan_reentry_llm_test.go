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

// TestRealLLM_PlanReentry_ClarificationLoop verifies the plan re-entry
// capability is a CLARIFICATION LOOP, not mere continuation: when the task is
// underspecified, plan must ASK the caller for the missing information (not
// fabricate a plan); when the caller supplies it via a resumed round, plan
// refines using exactly that information. This is the "缺乏信息时向顶层询问、
// 持续交互完善" behavior — the model-side complement to the white-box wiring
// test (agent/subagent_resume_test.go).
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

	planCfg := tagent.AgentConfig{
		SystemPrompt: tagent.PromptConfig{Inline: "你是 plan agent，负责制定 openspec 工作计划。\n" +
			"制定计划需要足够信息：对象/范围/目标/约束/验收标准。\n" +
			"信息不足时，不要臆测补全，向调用方提出具体的澄清问题（明确缺哪些信息）；\n" +
			"调用方经后续输入补充后，你据此完善计划，可多轮迭代直至信息充分再产出正式计划。\n" +
			"识别充分性：当对象/目标/约束等关键信息已明确时即视为充分，直接产出计划，不要反复追问次要细节。回答简洁。"},
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

	// Round 1: an UNDERSPECIFIED task — no object/scope/goal/constraints.
	// plan must ask clarifying questions, not fabricate a finished plan.
	out1 := runPlanInvocation(t, planAgent, trpcagent.NewInvocation(
		trpcagent.WithInvocationMessage(model.NewUserMessage("帮我做一个重构计划。")),
	))
	t.Logf("round1: %s", truncStr(out1, 400))

	asksQuestion := strings.Contains(out1, "？") || strings.Contains(out1, "?")
	asksSpecifics := strings.Contains(out1, "重构") &&
		(strings.Contains(out1, "哪个") || strings.Contains(out1, "什么") ||
			strings.Contains(out1, "对象") || strings.Contains(out1, "范围") ||
			strings.Contains(out1, "目标") || strings.Contains(out1, "模块") ||
			strings.Contains(out1, "约束"))
	notFinishedPlan := !strings.Contains(out1, "- [ ]")
	require.True(t, asksQuestion && asksSpecifics,
		"underspecified task must trigger clarifying questions, got: %s", truncStr(out1, 300))
	require.True(t, notFinishedPlan,
		"plan must NOT fabricate a finished task list when info is lacking, got: %s", truncStr(out1, 300))

	// Round 2 (re-entry): the caller supplies the missing information. plan
	// must refine using EXACTLY these specifics (distinctive markers below).
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

	// plan incorporated the supplied specifics (distinctive markers that only
	// came from the round-2 answer) — proof it refined from the clarification.
	require.True(t, strings.Contains(out2, "SmartCompressor") || strings.Contains(out2, "memory"),
		"refined plan must reference the supplied refactor target, got: %s", truncStr(out2, 400))
	require.True(t, strings.Contains(out2, "20 秒") || strings.Contains(out2, "20秒") || strings.Contains(out2, "延迟"),
		"refined plan must reference the supplied goal (latency), got: %s", truncStr(out2, 400))
	require.True(t, strings.Contains(out2, "TestCompression"),
		"refined plan must honor the supplied constraint, got: %s", truncStr(out2, 400))
}

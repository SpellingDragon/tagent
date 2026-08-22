package tagent_test

import (
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/testutil"
	"github.com/SpellingDragon/tagent/tool/action"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
)

// TestRealLLM_AsyncTask_EndToEnd exercises the full async loop with a real model
// and real tmux (Phase 4 / task 5.1):
//
//	user asks to run a long command → ActionTool spawns it → sync-wait window
//	elapses → ack (background) → command finishes → task_settled event → the
//	persistent loop reclaims it into a new turn → the LLM sees the settle result
//	and reports it back.
//
// Success signal: the command's unique marker (printed by the long command)
// surfaces in the assistant's output — proving the settle result flowed back
// through task_settled → reclaim turn → LLM, with no hang or empty reply.
func TestRealLLM_AsyncTask_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-LLM integration test in short mode")
	}
	if !action.IsTmuxAvailable() {
		t.Skip("tmux not available; skipping async e2e")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("failed to load config: %v, skipping", err)
	}
	t.Logf("async e2e: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

	llm := openai.New(
		cfg.ModelName,
		openai.WithAPIKey(cfg.APIKey),
		openai.WithBaseURL(cfg.Endpoint),
	)

	actionTool := action.NewActionTool(action.WithActionWorkspace(t.TempDir()))
	defer actionTool.Close()

	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             llm,
		MaxTokens:         8000,
		Temperature:       0.3,
		MaxToolIterations: 10,
		SystemPrompt: "你是一个能执行 shell 命令的助手。用 action 工具执行用户要求的命令。" +
			"长命令会在后台运行并立即返回“已在后台运行”，稍后你会收到一条 [task settled] 通知，" +
			"里面带有该任务的最终结果/输出。收到 [task settled] 后，请把其中的结果原样、简洁地告诉用户。" +
			"你也可以用 list_tasks 查询任务。",
		Tools: []tool.Tool{
			actionTool,
			tasktool.NewListTasksTool(),
			tasktool.NewCancelTaskTool(),
		},
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	outputCh, err := ag.StartLoop("u-async", "s-async-e2e")
	if err != nil {
		t.Fatalf("StartLoop: %v", err)
	}

	const marker = "BUILD_DONE_7788"
	// sleep 16 > sync_wait (10s) → dispatched async (ack ~10s), settles ~16-19s
	// later (monitor poll 3s) → task_settled → reclaim turn ~18-20s.
	start := time.Now()
	ag.InjectMessage(model.NewUserMessage(
		"请用 action 工具在后台运行这个命令：sleep 16 && echo " + marker +
			" —— 命令完成后，把它打印出来的内容告诉我。"))

	// Collect events across BOTH the ack turn and the later reclaim turn.
	// The marker string appears in the user message and may be restated by the
	// LLM in the early ack turn, so it only counts as a genuine settle-reclaim
	// signal once enough time has passed for the command to actually finish
	// (settleGate) AND when it comes from an assistant message.
	const settleGate = 13 * time.Second
	deadline := time.After(75 * time.Second)
	var assistantOut []string
	sawMarker := false
loop:
	for {
		select {
		case evt, ok := <-outputCh:
			if !ok {
				break loop
			}
			if evt.Response == nil || len(evt.Response.Choices) == 0 {
				continue
			}
			msg := evt.Response.Choices[0].Message
			if msg.Role != model.RoleAssistant || msg.Content == "" {
				continue
			}
			elapsed := time.Since(start)
			assistantOut = append(assistantOut, msg.Content)
			t.Logf("[assistant @%.0fs] %s", elapsed.Seconds(), truncate(msg.Content, 240))
			if strings.Contains(msg.Content, marker) && elapsed > settleGate {
				sawMarker = true
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	if len(assistantOut) == 0 {
		t.Fatalf("agent produced no assistant output — possible hang/empty reply")
	}
	if !sawMarker {
		t.Errorf("marker %q never surfaced from a post-settle (>%.0fs) assistant turn — the settle result did not flow back through the reclaim turn.\nassistant outputs:\n%s",
			marker, settleGate.Seconds(), strings.Join(assistantOut, "\n---\n"))
	}
}

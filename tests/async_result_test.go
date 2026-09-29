package tagent_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tagentagent "github.com/SpellingDragon/tagent/agent"
	tagentTask "github.com/SpellingDragon/tagent/agent/task"
	"github.com/SpellingDragon/tagent/testutil"
	tasktool "github.com/SpellingDragon/tagent/tool/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// slowOpArgs is the (empty-ish) input for the slow background test tool.
type slowOpArgs struct {
	Label string `json:"label" description:"a short label for the operation"`
}

type slowOpResult struct {
	Status string `json:"status"`
}

// newSlowBackgroundTool returns a tool that delegates to the task layer: it
// spawns a generic task that runs past the dense phase (so it detaches to the
// background), then settles with a unique marker. This exercises the full
// path WITHOUT tmux:
//
//	tool call → spawn (background) → ack → settle → task_settled event → the
//	persistent loop reclaims it into a new turn → the LLM reports the result.
//
// It mirrors ActionTool's async pattern but with a deterministic timer instead
// of a tmux command, so it runs anywhere a real LLM is reachable.
func newSlowBackgroundTool(marker string) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args slowOpArgs) (slowOpResult, error) {
			spawner, ok := tagentTask.TaskSpawnerFromContext(ctx)
			if !ok {
				return slowOpResult{Status: "SLOW RESULT: " + marker}, nil
			}
			detector := tagentTask.NewFuncSettleDetector(context.Background(),
				func(runCtx context.Context) (string, error) {
					select {
					case <-time.After(6 * time.Second):
						return "SLOW RESULT: " + marker, nil
					case <-runCtx.Done():
						return "", runCtx.Err()
					}
				},
				2*time.Second,
			)
			res := spawner.Spawn(tagentTask.TaskSpec{Kind: "generic", Desc: "slow op " + args.Label}, detector)
			if res.Settled {
				return slowOpResult{Status: res.Signal.Output}, nil
			}
			return slowOpResult{Status: "已在后台运行 (task " + res.Task.ID + ")，完成后会有 [task settled] 通知"}, nil
		},
		function.WithName("slow_op"),
		function.WithDescription("在后台执行一个耗时操作。会立即返回“已在后台运行”，稍后你会收到一条 [task settled] 通知，里面带最终结果。"),
	)
}

// TestRealLLM_AsyncResultDelivery_EndToEnd is the end-to-end integration test
// that unit tests structurally cannot provide (and whose absence let the
// board-injection bug ship): it runs a REAL agent + REAL LLM through the full
// assembled loop and asserts a background task's settle result flows back to
// the user via the reclaim turn — with no hang and no empty reply. No tmux
// required (the slow work is a timer-based generic task).
func TestRealLLM_AsyncResultDelivery_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-LLM integration test in short mode")
	}
	cfg, err := testutil.LoadConfig()
	if err != nil {
		t.Skipf("failed to load config: %v, skipping", err)
	}
	t.Logf("async-result-delivery e2e: model=%s endpoint=%s", cfg.ModelName, cfg.Endpoint)

	llm := openai.New(cfg.ModelName, openai.WithAPIKey(cfg.APIKey), openai.WithBaseURL(cfg.Endpoint))

	const marker = "ASYNC_DELIVERED_9911"
	ag, err := tagentagent.NewTagentAgent(&tagentagent.TagentConfig{
		Model:             llm,
		MaxTokens:         8000,
		Temperature:       0.3,
		MaxToolIterations: 10,
		SystemPrompt: "你是一个助手。当用户要求执行耗时操作时，用 slow_op 工具。" +
			"它会立即返回“已在后台运行”，稍后你会收到一条 [task settled] 通知，里面带最终结果。" +
			"收到 [task settled] 后，把其中的结果原样、简洁地告诉用户。",
		Tools: []tool.Tool{
			newSlowBackgroundTool(marker),
			tasktool.NewListTasksTool(),
		},
	})
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}
	defer ag.Close()

	outputCh, err := ag.StartLoop("u-ard", "s-ard-e2e")
	if err != nil {
		t.Fatalf("StartLoop: %v", err)
	}

	start := time.Now()
	ag.InjectMessage(model.NewUserMessage(
		"请用 slow_op 在后台执行一个耗时操作（label=build），完成后把它返回的结果告诉我。"))

	// The marker only exists in the settle result (not in the user message or
	// the ack), so any assistant message containing it proves the settle flowed
	// back through task_settled → reclaim turn → delivery. settleGate is a lower
	// bound: the task settles ~6s in.
	const settleGate = 4 * time.Second
	deadline := time.After(120 * time.Second)
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
			t.Logf("[assistant @%.0fs] %s", elapsed.Seconds(), truncate(msg.Content, 200))
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
		t.Errorf("marker %q never surfaced from a post-settle (>%.0fs) turn — the background settle result did not flow back through the reclaim turn.\nassistant outputs:\n%s",
			marker, settleGate.Seconds(), strings.Join(assistantOut, "\n---\n"))
	}
}

// asyncMockModel is a mock model that simulates async tool execution
type asyncMockModel struct {
	callCount int
	mu        sync.Mutex
}

func (m *asyncMockModel) GenerateContent(_ context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.callCount++
	count := m.callCount
	m.mu.Unlock()

	ch := make(chan *model.Response, 1)

	if count == 1 {
		ch <- &model.Response{
			Done: true,
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "I'll run the command asynchronously",
						ToolCalls: []model.ToolCall{
							{
								ID:   "call_123",
								Type: "function",
								Function: model.FunctionDefinitionParam{
									Name:      "action",
									Arguments: []byte(`{"command":"echo hello","async":true}`),
								},
							},
						},
					},
				},
			},
		}
	} else {
		ch <- &model.Response{
			Done: true,
			Choices: []model.Choice{
				{
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "Command completed successfully",
					},
				},
			},
		}
	}

	close(ch)
	return ch, nil
}

func (m *asyncMockModel) Info() model.Info {
	return model.Info{Name: "async-mock-model"}
}

// TestAsyncResultRouting verifies that async tool results are correctly
// routed to the original user who triggered the command.
func TestAsyncResultRouting(t *testing.T) {
	cfg := &tagentagent.TagentConfig{
		Model:     &asyncMockModel{},
		MaxTokens: 4096,
	}
	ta, err := tagentagent.NewTagentAgent(cfg)
	require.NoError(t, err)

	outputCh, err := ta.StartLoop("test-user", "test-session")
	require.NoError(t, err)

	// Collect output events
	var mu sync.Mutex
	receivedEvents := make(map[string][]*event.Event)

	go func() {
		for evt := range outputCh {
			if evt == nil {
				continue
			}
			chatID := ""
			if evt.StateDelta != nil {
				if cid, ok := evt.StateDelta["meta_chat_id"]; ok {
					chatID = string(cid)
				}
			}
			if chatID != "" {
				mu.Lock()
				receivedEvents[chatID] = append(receivedEvents[chatID], evt)
				mu.Unlock()
			}
		}
	}()

	userA := "user_A_789"
	ta.InjectMessageWithMetadata("user", model.Message{
		Role:    model.RoleUser,
		Content: "Please run: echo hello",
	}, map[string]string{
		"chat_id":   userA,
		"user_name": "Alice",
	})

	time.Sleep(500 * time.Millisecond)

	ta.InjectMessageWithMetadata("async_result", model.Message{
		Role:    model.RoleUser,
		Content: "[action_tool_result] Command completed: echo hello\nOutput: hello",
	}, map[string]string{
		"chat_id":   userA,
		"user_name": "Alice",
	})

	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()

	eventsA := receivedEvents[userA]
	assert.NotEmpty(t, eventsA, "User A should receive at least one event")

	for _, evt := range eventsA {
		if evt.StateDelta != nil {
			if cid, ok := evt.StateDelta["meta_chat_id"]; ok {
				assert.Equal(t, userA, string(cid), "All events should have chat_id=A")
			}
		}
	}

	hasFinalResponse := false
	for _, evt := range eventsA {
		if evt.StateDelta != nil {
			if trigger, ok := evt.StateDelta["trigger_source"]; ok {
				if string(trigger) == "async_result" {
					hasFinalResponse = true
					break
				}
			}
		}
	}
	assert.True(t, hasFinalResponse, "Should receive a response triggered by async_result")
}

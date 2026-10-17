// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	upagent "trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestTaskObservationConsumesWithoutModel checks both transport forms and producer immutability.
func TestTaskObservationConsumesWithoutModel(t *testing.T) {
	for _, durable := range []bool{false, true} {
		t.Run(map[bool]string{false: "volatile", true: "durable"}[durable], func(t *testing.T) {
			bus := NewEventBus()
			if durable {
				var err error
				bus, err = NewReliableEventBus(t.TempDir())
				require.NoError(t, err)
				defer bus.CloseDurable()
			}
			store := memory.NewInMemoryStore()
			m := &requestCapturingModel{resp: finalTextResponse("unused", "unused")}
			ta := newDurableAgentWithStore("observation", m, store, make(chan *event.Event, 20), bus)
			defer ta.contextManager.Close()
			evt := NewExternalInputEvent(SourceTask, model.NewUserMessage("[task settled] ∞ command stable → "+strings.Repeat("quiet ", 500)))
			evt.Metadata["settle_notice"] = "true"
			evt.Metadata[tagentevent.MetaKeySettleWake] = "observe"
			evt.Metadata["task_id"] = "job"
			evt.Metadata[tagentevent.MetaKeyObservationReceiptKey] = "producer-baggage"
			original, err := json.Marshal(evt.Metadata)
			require.NoError(t, err)
			_, err = bus.PublishContext(context.Background(), evt)
			require.NoError(t, err)
			batch := bus.TryPull()
			require.Len(t, batch, 1)
			require.Equal(t, turnContinue, ta.processTurn(context.Background(), ta.contextManager, batch))
			require.Zero(t, m.requestCount(), "quiet observation has a formal consumption without a model call")
			after, err := json.Marshal(batch[0].Metadata)
			require.NoError(t, err)
			require.Equal(t, string(original), string(after), "consumer state stays outside producer metadata")
			fact, err := taskInputFact(batch[0])
			require.NoError(t, err)
			require.Equal(t, "observed", compress.VerifiedInputDisposition(store, fact))
			refs := ta.contextManager.projection.GetAll()
			require.Len(t, refs, 1)
			require.Equal(t, tagentevent.TypeSettleFold, refs[0].EventType)
			recalled, err := store.GetEvent(fact.EventKey)
			require.NoError(t, err)
			require.Equal(t, evt.Message.Content, recalled.Content)
			snapshot, err := tagentevent.DecodeSourceSnapshot(recalled.Metadata[tagentevent.MetaKeySourceSnapshot])
			require.NoError(t, err)
			encoded, err := json.Marshal(snapshot.Metadata)
			require.NoError(t, err)
			require.Equal(t, string(original), string(encoded))
		})
	}
}

type taskWakeRetryStore struct {
	*memory.InMemoryStore
	attempts int
	keys     []int64
}

func (s *taskWakeRetryStore) ReplayEvent(key int64, fact memory.FullEvent) (memory.ReplayResult, memory.FullEvent, error) {
	if fact.Metadata[tagentevent.MetaKeyInboxRequestID] != "" && fact.EventType == tagentevent.TypeExternalInput {
		s.attempts++
		s.keys = append(s.keys, key)
		if s.attempts <= 4 {
			return memory.ReplayNew, fact, errors.New("temporary durable write failure")
		}
	}
	return s.InMemoryStore.ReplayEvent(key, fact)
}

// TestMixedTaskBatchRetainsVolatileDuringDurableRetry checks the frozen batch survives exhausted backoff.
func TestMixedTaskBatchRetainsVolatileDuringDurableRetry(t *testing.T) {
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	defer bus.CloseDurable()
	store := &taskWakeRetryStore{InMemoryStore: memory.NewInMemoryStore()}
	m := &requestCapturingModel{resp: finalTextResponse("done", "processed both")}
	ta := newDurableAgentWithStore("mixed-retry", m, store, make(chan *event.Event, 20), bus)
	defer ta.contextManager.Close()
	_, err = bus.PublishContext(context.Background(), NewExternalInputEvent("user", model.NewUserMessage("durable user decision")))
	require.NoError(t, err)
	batch := bus.TryPull()
	require.Len(t, batch, 1)
	observation := NewExternalInputEvent(SourceTask, model.NewUserMessage("[task settled] ∞ observation kept across retry"))
	observation.Metadata[tagentevent.MetaKeySettleWake] = "observe"
	observation.Metadata["settle_notice"] = "true"
	batch = append(batch, observation)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	require.Equal(t, turnContinue, ta.processTurn(ctx, ta.contextManager, batch))
	require.Equal(t, 5, store.attempts)
	for _, key := range store.keys {
		require.Equal(t, store.keys[0], key)
	}
	require.Equal(t, 1, m.requestCount())
	require.True(t, observation.volatileCommitted)
	fact, err := taskInputFact(observation)
	require.NoError(t, err)
	require.Equal(t, "observed", compress.VerifiedInputDisposition(store, fact))
	require.Empty(t, bus.TryPull(), "the current batch finishes without requeueing or dropping its volatile member")
}

// TestSubagentActiveTurnBudgetStopsIncompleteTurn checks model timeouts remain interruptions.
func TestSubagentActiveTurnBudgetStopsIncompleteTurn(t *testing.T) {
	m := &requestCapturingModel{}
	store := memory.NewInMemoryStore()
	bus := NewEventBus()
	ta := newDurableAgentWithStore("active-budget", m, store, make(chan *event.Event, 20), bus)
	defer ta.contextManager.Close()
	ctx := WithSubagentTurnBudget(context.Background(), 30*time.Millisecond)
	result := ta.processTurn(ctx, ta.contextManager, []*AgentEvent{NewExternalInputEvent("user", model.NewUserMessage("wait for model"))})
	require.Equal(t, turnStop, result)
	require.Equal(t, turnCancelled, ta.contextManager.lastBatchOutcome.status)
	require.NoError(t, ctx.Err(), "only the active RunFlow receives the deadline")
	require.Equal(t, 1, m.requestCount())
}

// TestInvocationWaitChangeWakesPull checks an empty bus closes when its last wait is voided.
func TestInvocationWaitChangeWakesPull(t *testing.T) {
	r := newSettleSinkRegistry()
	bus := NewEventBus()
	r.bind("inv", bus)
	r.noteSpawn("inv")
	ta := &TagentAgent{name: "wait-change", settleSinks: r}
	done := make(chan turnDisposition, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { done <- ta.runAgentLoop(ctx, bus, &ContextManager{}, loopSpec{invocationID: "inv"}) }()
	r.voidSpawn("inv")
	select {
	case disposition := <-done:
		require.Equal(t, turnContinue, disposition)
	case <-ctx.Done():
		t.Fatal("a changed wait left Pull blocked")
	}
}

type taskWakeRound struct {
	task     *task.Task
	detector *task.ManualDetector
}

type taskWakeChainProbe struct {
	mu      sync.Mutex
	current *task.Task
	resume  bool
	rounds  chan taskWakeRound
}

func (p *taskWakeChainProbe) Declaration() *tool.Declaration {
	return (&e2eProbeTool{}).Declaration()
}

func (p *taskWakeChainProbe) Call(ctx context.Context, _ []byte) (any, error) {
	controller, ok := task.TaskControllerFromContext(ctx)
	if !ok {
		return nil, errors.New("task controller unavailable")
	}
	p.mu.Lock()
	current := p.current
	p.mu.Unlock()
	if p.resume && current != nil {
		result, err := controller.Resume(ctx, current.ID, "next input")
		return result.Settled, err
	}
	detector := task.NewManualDetector()
	detector.FireDetach()
	spec := task.TaskSpec{Kind: "command", Mode: "oneshot", WaitFor: task.WaitExit, Desc: "chained task"}
	if p.resume {
		spec.Mode, spec.WaitFor = "interactive", task.WaitInput
		spec.ResumeFn = func(context.Context, string) (task.SettleDetector, error) {
			p.mu.Lock()
			current := p.current
			p.mu.Unlock()
			next := task.NewManualDetector()
			next.FireDetach()
			p.rounds <- taskWakeRound{task: current, detector: next}
			return next, nil
		}
	}
	result := controller.Spawn(spec, detector)
	p.mu.Lock()
	p.current = result.Task
	p.mu.Unlock()
	p.rounds <- taskWakeRound{task: result.Task, detector: detector}
	return "background adopted", nil
}

// TestRunContinuationKeepsAttributionAcrossTaskRounds checks chained spawn and interactive resume.
func TestRunContinuationKeepsAttributionAcrossTaskRounds(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "spawn", true: "resume"}[resume], func(t *testing.T) {
			probe := &taskWakeChainProbe{resume: resume, rounds: make(chan taskWakeRound, 4)}
			m := &recordingSequenceModel{responses: []*model.Response{
				toolCallResponse("first", "first"), finalTextResponse("first-ack", "first background adopted"),
				toolCallResponse("second", "second"), finalTextResponse("second-ack", "second background adopted"),
				finalTextResponse("complete", "all rounds consumed"),
			}}
			ta, err := NewTagentAgent(&TagentConfig{Name: "task-chain", Model: m, SystemPrompt: "test", MemoryStore: memory.NewInMemoryStore(), Tools: []tool.Tool{probe}, MaxToolIterations: 5})
			require.NoError(t, err)
			defer ta.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			output, err := ta.Run(WithSubagentTurnBudget(ctx, 100*time.Millisecond), &upagent.Invocation{InvocationID: "chain-inv", Message: model.NewUserMessage("run chained tasks")})
			require.NoError(t, err)
			nextSettleAnswer(t, output, "first background adopted")
			first := <-probe.rounds
			defer first.detector.Done()
			select {
			case <-time.After(150 * time.Millisecond):
			case <-ctx.Done():
				t.Fatal("the initial task turn did not finish")
			}
			kind := task.SettleCompleted
			if resume {
				kind = task.SettleInput
			}
			first.detector.Emit(task.SettleSignal{Kind: kind, Output: "first task result"})
			nextSettleAnswer(t, output, "second background adopted")
			var second taskWakeRound
			select {
			case second = <-probe.rounds:
			case <-ctx.Done():
				t.Fatal("the continuation did not adopt its next task round")
			}
			defer second.detector.Done()
			require.True(t, ta.settleSinks.awaiting("chain-inv"), "the second round is booked under the original invocation")
			second.detector.Emit(task.SettleSignal{Kind: kind, Output: "second task result"})
			nextSettleAnswer(t, output, "all rounds consumed")
			for {
				select {
				case _, open := <-output:
					if !open {
						return
					}
				case <-ctx.Done():
					t.Fatal("the final consumed result did not quiesce")
				}
			}
		})
	}
}

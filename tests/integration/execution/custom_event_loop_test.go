package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

const (
	// customEventType is an event type the runtime carries no frozen table entry
	// for, so the case reads the extension point rather than a built-in type.
	customEventType = "meditation_tick"
	// customEventText is the payload the custom event arrives with.
	customEventText = "the host admitted one tick of its own"
	// customToolName is the tool the consumption calls.
	customToolName = "tick_tool"
	// customToolOutput is what that tool answers.
	customToolOutput = "tick answered"
)

// errCustomEventNoResult is the named refusal of a consumed custom event whose
// consumption produced nothing to deliver.
var errCustomEventNoResult = errors.New("execution: the consumed custom event answered no result")

// TestCustomEventConsumedByLoop drives one custom event type through the host loop.
// - a custom type admitted into an invocation's ready input is consumed as its own message, beside no type whitelist
// - the consumption calls the model and a tool, and its settled result backflows through the delivery face
// - a consumption producing nothing is refused by name instead of delivered silently
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestCustomEventConsumedByLoop(t *testing.T) {
	owner := event.Address{AgentID: "agent-host", SessionID: "session-custom-event"}

	t.Run("a custom event type is consumed and its result is delivered", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		tool := &convTool{name: customToolName, output: customToolOutput}
		var roundMu sync.Mutex
		round := 0
		provider := &customEventModel{name: "model-host", answer: func(req *model.Request) *model.Response {
			roundMu.Lock()
			round++
			current := round
			roundMu.Unlock()
			if current == 1 {
				if len(req.Messages) != 1 || req.Messages[0].Role != model.RoleUser || req.Messages[0].Content != customEventText {
					t.Errorf("first request messages = %+v, want the custom event consumed as its own message", req.Messages)
				}
				if req.Tools[customToolName] == nil {
					t.Errorf("first request tools = %v, want the tool the consumption calls", req.Tools)
				}
				return convToolTurn(convCall("call-tick", customToolName, `{"tick":"09:00"}`))
			}
			settled := observationOf(req.Messages, "call-tick")
			if settled == nil || !strings.Contains(settled.Content, customToolOutput) {
				t.Errorf("second request messages = %+v, want the settled tool result backflowed into the next decision", req.Messages)
			}
			return convFinalTurn("tick consumed")
		}}
		rig := newCustomEventRig(t, provider, []trpctool.Tool{tool}, owner)
		result, err := rig.consume(ctx, provider, customEventOf(owner))
		if err != nil {
			t.Fatalf("consume the custom event: %v", err)
		}
		if result != "tick consumed" {
			t.Fatalf("consumed result = %q, want the answer the loop produced", result)
		}
		if delivered := awaitDelivered(t, rig.delivered); delivered != "tick consumed" {
			t.Fatalf("delivered result = %q, want the consumed result handed to the delivery face", delivered)
		}
		notice := awaitNotice(t, rig.arrived)
		if notice.Kind != task.NoticeSettled || notice.TaskID == "" || notice.Event == "" {
			t.Fatalf("mailbox notice = %+v, want the settled notification of the admitted tool call", notice)
		}
		if got := tool.calls(); got != 1 {
			t.Fatalf("tool executions = %d, want the consumption's one call", got)
		}
		requests := provider.recorded()
		if len(requests) != 2 {
			t.Fatalf("model requests = %d, want one per decision", len(requests))
		}
	})

	t.Run("a consumption producing nothing is refused by name", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &customEventModel{name: "model-host", answer: func(*model.Request) *model.Response {
			return convFinalTurn("")
		}}
		rig := newCustomEventRig(t, provider, nil, owner)
		result, err := rig.consume(ctx, provider, customEventOf(owner))
		if !errors.Is(err, errCustomEventNoResult) {
			t.Fatalf("consume = (%q, %v), want the named refusal of an empty consumption", result, err)
		}
		if result != "" {
			t.Fatalf("consumed result = %q, want no result beside the refusal", result)
		}
		if got := len(provider.recorded()); got != 1 {
			t.Fatalf("model requests = %d, want the custom event consumed by one decision", got)
		}
		select {
		case delivered := <-rig.delivered:
			t.Fatalf("delivered result = %q, want no delivery beside an empty consumption", delivered)
		default:
		}
		select {
		case notice := <-rig.arrived:
			t.Fatalf("mailbox notice = %+v, want no notification beside an empty consumption", notice)
		default:
		}
	})
}

// customEventRig is the host loop one custom event is admitted to: the binding
// holder the loop borrows, the task manager whose mailbox is the delivery face,
// and the consumer that drives the loop and hands its result on.
type customEventRig struct {
	instance     event.InstanceID
	invocationID event.InvocationID
	owner        event.Address
	bindings     *state.Bindings
	manager      *task.MemoryManager
	loop         *agent.Loop
	arrived      chan task.Notice
	delivered    chan string

	mu    sync.Mutex
	facts map[event.EventID]model.Message
	seq   uint64
}

// newCustomEventRig builds the host loop over the given provider and tools,
// with every tool registered through the task wrapper of the rig.
func newCustomEventRig(t *testing.T, provider model.Model, tools []trpctool.Tool, owner event.Address) *customEventRig {
	t.Helper()
	rig := &customEventRig{
		instance:     event.InstanceID("instance-custom-event"),
		invocationID: event.InvocationID("inv-custom-event"),
		owner:        owner,
		bindings:     state.NewBindings(),
		arrived:      make(chan task.Notice, 4),
		delivered:    make(chan string, 4),
		facts:        map[event.EventID]model.Message{},
	}
	admission := task.NewMemoryAdmission()
	registry := task.NewMemoryRegistry(task.NewMemoryCommit(nil), admission)
	rig.manager = task.NewMemoryManager(registry, nil, task.WithEnvelopeDeliverer(rig.envelope))
	wrapped := make([]trpctool.Tool, 0, len(tools))
	for _, tool := range tools {
		callable, ok := tool.(trpctool.CallableTool)
		if !ok {
			t.Fatalf("tool %T carries no native call surface", tool)
		}
		wrapped = append(wrapped, task.Wrap(rig.manager, callable,
			task.WithResultRecorder(rig.record), task.WithMailbox(rig.mail)))
	}
	binding := state.AgentBinding{
		Model:        provider,
		Tools:        wrapped,
		PromptSource: state.PromptSource{Version: "prompt-custom-event-v1"},
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: &config.Declaration{
		Entry:  string(owner.AgentID),
		Agents: []config.AgentSpec{{ID: string(owner.AgentID), Model: provider.Info().Name}},
	}}, map[event.AgentID]state.AgentBinding{owner.AgentID: binding}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	if err := rig.bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	rig.loop = agent.NewLoop(rig.bindings, state.NewOccupancy(), rig.instance, rig.resolve)
	return rig
}

// consume drives the host loop over one custom event and hands the consumed
// result to the delivery face: an empty result is refused by name instead of
// being delivered as nothing.
func (r *customEventRig) consume(ctx context.Context, provider *customEventModel, ready event.Event) (string, error) {
	turn, err := r.loop.Drive(ctx, agent.Invocation{
		InvocationID: r.invocationID,
		Address:      r.owner,
		Instance:     r.instance,
		Ready:        []event.Event{ready},
	})
	if err != nil {
		return "", err
	}
	if turn.Terminal != model.KindCompleted {
		return "", fmt.Errorf("%w: the turn ended %v", errCustomEventNoResult, turn.Terminal)
	}
	result := strings.TrimSpace(provider.reply())
	if result == "" {
		return "", errCustomEventNoResult
	}
	r.delivered <- result
	return result, nil
}

// envelope reports the acceptance of every notification the manager offers for
// delivery, so the delivery face of this rig books the outcomes it is handed.
func (r *customEventRig) envelope(context.Context, task.NoticeEnvelope) (task.DeliveryAcceptance, error) {
	return task.DeliveryAccepted, nil
}

// mail admits one settled notification into the mailbox of this rig and hands it
// back into the invocation that issued the call.
func (r *customEventRig) mail(ctx context.Context, notice task.Notice) error {
	r.loop.Backflow(r.invocationID, notice)
	r.arrived <- notice
	return nil
}

// record stores the result of one wrapped call as the fact its notification
// names, so the backflow carries the settled body into the next decision.
func (r *customEventRig) record(_ context.Context, result task.Result) (event.EventID, error) {
	content := ""
	switch out := result.Output.(type) {
	case nil:
	case string:
		content = out
	default:
		content = fmt.Sprintf("%v", out)
	}
	if result.Err != nil {
		content = result.Err.Error()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	id := event.EventID(fmt.Sprintf("fact-custom-event-%d", r.seq))
	r.facts[id] = model.Message{
		Role:     model.RoleTool,
		ToolID:   result.CallID,
		ToolName: result.Tool,
		Content:  content,
	}
	return id, nil
}

// resolve returns the message one recorded fact carries.
func (r *customEventRig) resolve(id event.EventID) (model.Message, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	msg, ok := r.facts[id]
	return msg, ok
}

// customEventOf builds one event of a type the runtime carries no frozen entry
// for, addressed to the given session.
func customEventOf(owner event.Address) event.Event {
	return event.Event{
		Key:       event.EventID("ev-custom-event"),
		Type:      customEventType,
		Timestamp: time.Now(),
		Message:   model.Message{Role: model.RoleUser, Content: customEventText},
		Owner:     owner,
		Source:    owner,
		Target:    owner,
	}
}

// observationOf returns the settled observation of one call: the message
// carrying the call identity outside the tool role, so the admission receipt is
// never read as the settled body.
func observationOf(messages []model.Message, callID string) *model.Message {
	for i := range messages {
		if messages[i].ToolID == callID && messages[i].Role != model.RoleTool {
			return &messages[i]
		}
	}
	return nil
}

// customEventModel is the scripted provider of the custom event case: it records
// every request and every answer it produced.
type customEventModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
	replies  []string
}

// GenerateContent records the request and streams the scripted response.
func (m *customEventModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	response := answer(req)
	m.mu.Lock()
	if len(response.Choices) > 0 {
		m.replies = append(m.replies, response.Choices[0].Message.Content)
	} else {
		m.replies = append(m.replies, "")
	}
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- response
	close(ch)
	return ch, nil
}

// Info reports the scripted provider's descriptor.
func (m *customEventModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// recorded reports the native requests the scripted provider was driven with.
func (m *customEventModel) recorded() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// reply reports the last answer the scripted provider produced: the result one
// consumption handed to the delivery face.
func (m *customEventModel) reply() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.replies) == 0 {
		return ""
	}
	return m.replies[len(m.replies)-1]
}

// awaitDelivered waits for one consumed result to reach the delivery face.
func awaitDelivered(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case delivered := <-ch:
		return delivered
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the delivered result")
		return ""
	}
}

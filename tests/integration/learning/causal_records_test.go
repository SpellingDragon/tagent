package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCausalTrainingRecords drives the runtime's one evidence face — the event loop's capture admission — and the record pipeline attached to it:
// - the loop admits one attempt per observable attempt, so every decision's outcome is replayable, the last one and the intermediate ones alike
// - the request view material of every decision is fixed at admission: the model, the prompt version and the tools the request saw
// - the causal chain one decision reduced is the request the loop handed the model, read from the turn itself
// - a failed attempt keeps its error text beside the shared terminal
// - every record line carries its schema version and never the v2 capture shape
// - a record round-trips through its versioned line without losing fields
// - two concurrent drives keep their attempts independent under the race
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-recorder
func TestCausalTrainingRecords(t *testing.T) {
	t.Run("the production points write episode, decision and attempt records over a real loop", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			if len(req.Messages) == 1 {
				return learnToolTurn(learnCall("call-1", "echo-a", `{"x":1}`))
			}
			return learnFinalTurn("settled")
		}}
		h := newHarness(t, provider, []*learnTool{{name: "echo-a", output: "out-a"}}, "inv-learn-1")
		turn := h.drive(ctx, "ep-learn-1", "learn me")

		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		records := h.records.snapshot()
		if len(records.attempts) != 2 {
			t.Fatalf("attempt records = %d, want one per observable model attempt", len(records.attempts))
		}
		for i, attempt := range records.attempts {
			if attempt.EpisodeID != "ep-learn-1" {
				t.Fatalf("attempt %d episode = %q, want the episode boundary the ready input named", i, attempt.EpisodeID)
			}
		}

		views := h.evidence.views()
		if len(views) != 2 {
			t.Fatalf("fixed request views = %d, want one per decision boundary", len(views))
		}
		for i, view := range views {
			if view.decisionID != turn.Decisions[i].DecisionID {
				t.Fatalf("view %d decision = %q, want the real decision identity %q", i, view.decisionID, turn.Decisions[i].DecisionID)
			}
			if view.model != "model-a" {
				t.Fatalf("view %d model = %q, want the model the request ran on", i, view.model)
			}
			if view.promptVersion != "prompt-v1" {
				t.Fatalf("view %d prompt version = %q, want the version the request ran with", i, view.promptVersion)
			}
			if len(view.tools) != 1 || view.tools[0] != "echo-a" {
				t.Fatalf("view %d visible tools = %v, want the tools the request actually saw", i, view.tools)
			}
		}
		if second := turn.Decisions[1].Request.Messages; !carriesToolOutput(second, "out-a") {
			t.Fatalf("second decision request = %+v, want the settled tool observation of the first round", second)
		}

		for i, attempt := range records.attempts {
			if attempt.Schema != rl.SchemaCurrent {
				t.Fatalf("attempt %d schema = %q, want %q", i, attempt.Schema, rl.SchemaCurrent)
			}
			if attempt.ID == "" || attempt.DecisionID != turn.Decisions[i].DecisionID {
				t.Fatalf("attempt %d identity = (%q, %q), want one attempt bound to decision %q", i, attempt.ID, attempt.DecisionID, turn.Decisions[i].DecisionID)
			}
			if attempt.EpisodeID != "ep-learn-1" {
				t.Fatalf("attempt %d episode = %q, want the episode it belongs to", i, attempt.EpisodeID)
			}
			if attempt.Terminal != turn.Decisions[i].Terminal || attempt.Terminal != model.KindCompleted {
				t.Fatalf("attempt %d terminal = %v, want the shared terminal %v", i, attempt.Terminal, turn.Decisions[i].Terminal)
			}
			if attempt.Usage.TotalTokens != 15 {
				t.Fatalf("attempt %d usage = %+v, want the usage the reduction accumulated", i, attempt.Usage)
			}
			if attempt.Error != "" {
				t.Fatalf("attempt %d error = %q, want empty for an attempt that observed no error", i, attempt.Error)
			}
			if attempt.Logprobs != rl.LogprobsUnknown {
				t.Fatalf("attempt %d logprobs = %v, want unknown when nothing was observed", i, attempt.Logprobs)
			}
			if attempt.Timestamp.IsZero() {
				t.Fatalf("attempt %d timestamp = %v, want the time the record was written", i, attempt.Timestamp)
			}
		}
		first := records.attempts[0].Response
		if len(first.Choices) != 1 || len(first.Choices[0].Message.ToolCalls) != 1 ||
			first.Choices[0].Message.ToolCalls[0].ID != "call-1" ||
			first.Choices[0].Message.ToolCalls[0].Function.Name != "echo-a" ||
			string(first.Choices[0].Message.ToolCalls[0].Function.Arguments) != `{"x":1}` {
			t.Fatalf("first attempt response = %+v, want the native tool call kept byte-identical", first)
		}
		second := records.attempts[1].Response
		if len(second.Choices) != 1 || second.Choices[0].Message.Content != "settled" {
			t.Fatalf("second attempt response = %+v, want the native final content kept as it stood", second)
		}
	})

	t.Run("a failed attempt keeps its error text beside the shared terminal", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			return learnFailTurn("provider refused")
		}}
		h := newHarness(t, provider, nil, "inv-learn-1")
		turn := h.drive(ctx, "ep-learn-fail", "fail me")

		if turn.Terminal != model.KindFailed {
			t.Fatalf("turn terminal = %v, want the failed terminal the reduction derived", turn.Terminal)
		}
		records := h.records.snapshot()
		if len(records.attempts) != 1 {
			t.Fatalf("attempt records = %d, want one per observable attempt of the failed decision", len(records.attempts))
		}
		attempt := records.attempts[0]
		if attempt.Terminal != model.KindFailed || attempt.Terminal != turn.Decisions[0].Terminal {
			t.Fatalf("attempt terminal = %v, want the shared failed terminal", attempt.Terminal)
		}
		if !strings.Contains(attempt.Error, "provider refused") {
			t.Fatalf("attempt error = %q, want the observed error text kept", attempt.Error)
		}
	})

	t.Run("every record line carries its version and never the legacy v2 shape", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			return learnFinalTurn("done")
		}}
		h := newHarness(t, provider, nil, "inv-learn-1")
		turn := h.drive(ctx, "ep-learn-version", "run")
		records := h.records.snapshot()
		if len(records.attempts) != 1 {
			t.Fatalf("attempt records = %d, want the one attempt the drive admitted", len(records.attempts))
		}
		episode := rl.EpisodeRecord{
			Schema:    rl.SchemaCurrent,
			ID:        "ep-learn-version",
			Sessions:  []event.SessionID{h.session},
			Lineage:   rl.LineageUnknown,
			Timestamp: time.Unix(1700000000, 0).UTC(),
		}
		decision := rl.DecisionRecord{
			Schema:       rl.SchemaCurrent,
			ID:           turn.Decisions[0].DecisionID,
			InvocationID: h.invocationID,
			EpisodeID:    "ep-learn-version",
			Timestamp:    time.Unix(1700000000, 0).UTC(),
		}

		lines := make([][]byte, 0, 3)
		for _, line := range []rl.Line{
			rl.NewEpisodeLine(episode),
			rl.NewDecisionLine(decision),
			rl.NewAttemptLine(records.attempts[0]),
		} {
			data, err := rl.MarshalLine(line)
			if err != nil {
				t.Fatalf("marshal the %s line: %v", line.Kind, err)
			}
			text := string(data)
			if !strings.Contains(text, `"schema":"`+string(rl.SchemaCurrent)+`"`) {
				t.Fatalf("line = %s, want the declared schema version on the wire", text)
			}
			if !strings.Contains(text, `"kind":"`+string(line.Kind)+`"`) {
				t.Fatalf("line = %s, want the record kind on the wire", text)
			}
			for _, legacy := range []string{"schema_version", "capture_scope", "run_id", "llm_call"} {
				if strings.Contains(text, `"`+legacy+`"`) {
					t.Fatalf("line = %s, want no legacy capture field %q on a current schema line", text, legacy)
				}
			}
			lines = append(lines, data)
		}

		parsed, err := rl.ParseLine(lines[0])
		if err != nil {
			t.Fatalf("parse the episode line back: %v", err)
		}
		if parsed.Schema != rl.SchemaCurrent || parsed.Kind != rl.RecordKindEpisode ||
			parsed.Episode == nil || parsed.Episode.ID != "ep-learn-version" {
			t.Fatalf("parsed episode line = %+v, want the versioned record read back", parsed)
		}

		if _, err := rl.MarshalLine(rl.Line{Kind: rl.RecordKindEpisode, Episode: &rl.EpisodeRecord{}}); !errors.Is(err, rl.ErrSchemaMissing) {
			t.Fatalf("marshal an unversioned line error = %v, want ErrSchemaMissing", err)
		}
		legacy := []byte(`{"schema_version":2,"capture_scope":"sdk_request","run_id":"r-1","call_id":"c-1","llm_call":{}}`)
		if _, err := rl.ParseLine(legacy); !errors.Is(err, rl.ErrLegacySchemaLine) {
			t.Fatalf("parse a legacy v2 row error = %v, want ErrLegacySchemaLine with its migration route", err)
		}
		masquerade := []byte(`{"schema":"` + string(rl.SchemaCurrent) + `","kind":"attempt","schema_version":2,"call_id":"c-1"}`)
		if _, err := rl.ParseLine(masquerade); !errors.Is(err, rl.ErrSchemaMisuse) {
			t.Fatalf("parse a masquerading row error = %v, want ErrSchemaMisuse", err)
		}
		future := []byte(`{"schema":"tagent.record.v2","kind":"episode","episode":{}}`)
		if _, err := rl.ParseLine(future); !errors.Is(err, rl.ErrSchemaUnknown) {
			t.Fatalf("parse a future version line error = %v, want ErrSchemaUnknown", err)
		}
	})

	t.Run("a record round-trips through its versioned line without losing fields", func(t *testing.T) {
		record := rl.AttemptRecord{
			Schema:     rl.SchemaCurrent,
			ID:         "attempt-a1",
			DecisionID: "decision-d1",
			EpisodeID:  "ep-1",
			Response: model.Response{
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
				Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
			Terminal:       model.KindCompleted,
			Usage:          model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			Logprobs:       rl.LogprobsPresent,
			LogprobsSource: "provider-x",
			Timestamp:      time.Unix(1700000000, 0).UTC(),
		}
		data, err := rl.MarshalLine(rl.NewAttemptLine(record))
		if err != nil {
			t.Fatalf("marshal the attempt line: %v", err)
		}
		parsed, err := rl.ParseLine(data)
		if err != nil {
			t.Fatalf("parse the attempt line back: %v", err)
		}
		got := parsed.Attempt
		if got == nil {
			t.Fatalf("parsed attempt = nil, want the attempt payload read back")
		}
		if got.ID != record.ID || got.DecisionID != record.DecisionID || got.EpisodeID != record.EpisodeID {
			t.Fatalf("parsed attempt identity = (%q, %q, %q), want the identities kept", got.ID, got.DecisionID, got.EpisodeID)
		}
		if got.Terminal != record.Terminal || got.Usage.TotalTokens != 15 {
			t.Fatalf("parsed attempt = (terminal %v, usage %+v), want the shared terminal and usage kept", got.Terminal, got.Usage)
		}
		if got.Logprobs != rl.LogprobsPresent || got.LogprobsSource != "provider-x" {
			t.Fatalf("parsed attempt logprobs = (%v, %q), want the observed state and its source kept", got.Logprobs, got.LogprobsSource)
		}
		if len(got.Response.Choices) != 1 || got.Response.Choices[0].Message.Content != "hello" {
			t.Fatalf("parsed attempt response = %+v, want the native response kept as it stood", got.Response)
		}

		decision := rl.DecisionRecord{
			Schema:       rl.SchemaCurrent,
			ID:           "decision-d1",
			InvocationID: "inv-1",
			EpisodeID:    "ep-1",
			InputEvents:  []event.EventID{"ev-1", "fact-1"},
			ModelView: rl.ModelViewRef{
				Model:         "model-a",
				PromptVersion: "prompt-v1",
				VisibleTools:  []rl.VersionedName{{Name: "echo-a"}},
			},
			Timestamp: time.Unix(1700000000, 0).UTC(),
		}
		data, err = rl.MarshalLine(rl.NewDecisionLine(decision))
		if err != nil {
			t.Fatalf("marshal the decision line: %v", err)
		}
		parsed, err = rl.ParseLine(data)
		if err != nil {
			t.Fatalf("parse the decision line back: %v", err)
		}
		if parsed.Decision == nil || len(parsed.Decision.InputEvents) != 2 ||
			parsed.Decision.InputEvents[0] != "ev-1" || parsed.Decision.InputEvents[1] != "fact-1" {
			t.Fatalf("parsed decision input events = %v, want the reduced input events kept in order", parsed.Decision.InputEvents)
		}
	})

	t.Run("two concurrent drives keep their attempts independent under the race", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			return learnFinalTurn("done")
		}}
		first := newHarness(t, provider, nil, "inv-concurrent-a")
		second := newHarness(t, provider, nil, "inv-concurrent-b")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			first.drive(ctx, "ep-concurrent-a", "run a")
		}()
		go func() {
			defer wg.Done()
			second.drive(ctx, "ep-concurrent-b", "run b")
		}()
		wg.Wait()

		all := collectorState{}
		for _, h := range []*harness{first, second} {
			all.attempts = append(all.attempts, h.records.snapshot().attempts...)
		}
		if len(all.attempts) != 2 {
			t.Fatalf("attempt records = %d, want one per drive", len(all.attempts))
		}
		if all.attempts[0].EpisodeID == all.attempts[1].EpisodeID {
			t.Fatalf("attempt episodes = (%q, %q), want each drive to keep its own episode boundary", all.attempts[0].EpisodeID, all.attempts[1].EpisodeID)
		}
		seen := map[event.AttemptID]bool{}
		for _, attempt := range all.attempts {
			if seen[attempt.ID] {
				t.Fatalf("attempt id %q written twice, want every attempt kept independent", attempt.ID)
			}
			seen[attempt.ID] = true
		}
	})
}

// carriesToolOutput reports whether one request transcript carries the settled
// observation of a tool call in the lane that observation was delivered through,
// so the causal chain one decision reduced is read from the request the loop
// actually handed the model rather than from a stored list of identities.
func carriesToolOutput(messages []model.Message, output string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Content, output) {
			return true
		}
	}
	return false
}

// collectorState is the snapshot of everything the record collector received.
type collectorState struct {
	episodes  []rl.EpisodeRecord
	decisions []rl.DecisionRecord
	attempts  []rl.AttemptRecord
}

// collector is the training record production face of the test: it keeps every
// record it is handed, in arrival order, and edits none of them.
type collector struct {
	mu    sync.Mutex
	state collectorState
}

// RecordEpisode keeps one episode record.
func (c *collector) RecordEpisode(ctx context.Context, record rl.EpisodeRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.episodes = append(c.state.episodes, record)
	return nil
}

// RecordDecision keeps one decision record.
func (c *collector) RecordDecision(ctx context.Context, record rl.DecisionRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.decisions = append(c.state.decisions, record)
	return nil
}

// RecordAttempt keeps one attempt record.
func (c *collector) RecordAttempt(ctx context.Context, record rl.AttemptRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.attempts = append(c.state.attempts, record)
	return nil
}

// snapshot reports the records collected so far.
func (c *collector) snapshot() collectorState {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := collectorState{
		episodes:  append([]rl.EpisodeRecord(nil), c.state.episodes...),
		decisions: append([]rl.DecisionRecord(nil), c.state.decisions...),
		attempts:  append([]rl.AttemptRecord(nil), c.state.attempts...),
	}
	return out
}

// factTable maps one recorded result fact to the native tool message it carries.
type factTable struct {
	mu    sync.Mutex
	facts map[event.EventID]model.Message
}

// put records one result fact beside its native tool message.
func (f *factTable) put(id event.EventID, msg model.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.facts == nil {
		f.facts = map[event.EventID]model.Message{}
	}
	f.facts[id] = msg
}

// resolve reports the native tool message one recorded result fact carries.
func (f *factTable) resolve(id event.EventID) (model.Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msg, ok := f.facts[id]
	return msg, ok
}

// harness is one real runtime assembly of the learning records: the persistent
// event loop over the state owner, the task wrapper over the memory manager and
// the record collector at the production points.
type harness struct {
	loop         *agent.Loop
	records      *collector
	evidence     *evidenceCapture
	facts        *factTable
	session      event.SessionID
	invocationID event.InvocationID
}

// newHarness assembles the real loop and task layer around one scripted model
// and its tools, with the record collector attached at the production points.
func newHarness(t *testing.T, provider *learnModel, tools []*learnTool, invocationID event.InvocationID) *harness {
	t.Helper()
	h := &harness{
		records:      &collector{},
		facts:        &factTable{},
		session:      event.SessionID("session-learn-1"),
		invocationID: invocationID,
	}
	commit := task.NewMemoryCommit(nil)
	registry := task.NewMemoryRegistry(commit, task.NewMemoryAdmission())
	manager := task.NewMemoryManager(registry, func(ctx context.Context, notice task.Notice) (task.DeliveryAcceptance, error) {
		return task.DeliveryAccepted, nil
	})
	wrapped := make([]trpctool.Tool, 0, len(tools))
	for _, tool := range tools {
		w := task.Wrap(manager, tool,
			task.WithResultRecorder(func(ctx context.Context, result task.Result) (event.EventID, error) {
				id := event.EventID("fact-" + result.CallID)
				content := ""
				if result.Output != nil {
					content = fmt.Sprint(result.Output)
				}
				h.facts.put(id, model.Message{Role: model.RoleTool, ToolName: result.Tool, Content: content})
				return id, nil
			}),
			task.WithMailbox(func(ctx context.Context, notice task.Notice) error {
				h.loop.Backflow(h.invocationID, notice)
				return nil
			}),
		)
		wrapped = append(wrapped, w)
	}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: provider.name}},
	}
	binding := state.AgentBinding{
		Model:        provider,
		Tools:        wrapped,
		PromptSource: state.PromptSource{Version: "prompt-v1"},
	}
	plan, err := state.NewStatePlan(state.OrgPlan{Declaration: decl}, map[event.AgentID]state.AgentBinding{"agent-a": binding}, nil)
	if err != nil {
		t.Fatalf("build the state plan: %v", err)
	}
	bindings := state.NewBindings()
	if err := bindings.Hold(plan); err != nil {
		t.Fatalf("hold the state plan: %v", err)
	}
	h.loop = agent.NewLoop(bindings, state.NewOccupancy(), "instance-learn", h.facts.resolve)
	h.evidence = &evidenceCapture{records: h.records}
	h.loop.SetCapture(h.evidence)
	return h
}

// evidenceView is the request view material the runtime fixed for one decision.
type evidenceView struct {
	decisionID    event.DecisionID
	model         string
	promptVersion string
	tools         []string
}

// evidenceCapture is the loop's one evidence face in the test: it keeps the view
// each decision's request was fixed on and writes the attempt record of every
// admitted attempt through the record collector, exactly as the run's own
// adapter does. Nothing is derived here: the attempt record is built through the
// single observation face AttemptOf from the outcome the reduction produced.
type evidenceCapture struct {
	records *collector

	mu     sync.Mutex
	fixed  []evidenceView
	sealed int
}

// Snapshot keeps the view material one decision's request was fixed on.
func (c *evidenceCapture) Snapshot(_ context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools, _ []string, _ model.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fixed = append(c.fixed, evidenceView{decisionID: decisionID, model: modelName, promptVersion: promptVersion, tools: append([]string(nil), visibleTools...)})
	return nil
}

// Record writes the attempt record of one admitted attempt.
func (c *evidenceCapture) Record(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error {
	return c.records.RecordAttempt(ctx, rl.AttemptOf(attemptID, decisionID, episodeID, outcome))
}

// Seal counts one closed turn.
func (c *evidenceCapture) Seal(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sealed++
	return nil
}

// views reports the request views fixed so far, in decision order.
func (c *evidenceCapture) views() []evidenceView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]evidenceView(nil), c.fixed...)
}

// drive runs one invocation of the harness to its terminal over the named
// episode boundary.
func (h *harness) drive(ctx context.Context, episode event.EpisodeID, input string) agent.Turn {
	turn, err := h.loop.Drive(ctx, agent.Invocation{
		InvocationID: h.invocationID,
		Address:      event.Address{AgentID: "agent-a", SessionID: h.session},
		Instance:     "instance-learn",
		Ready: []event.Event{
			{
				Key:       "ev-input-1",
				Type:      "user_input",
				Timestamp: time.Now(),
				EpisodeID: episode,
				Message:   model.Message{Role: model.RoleUser, Content: input},
			},
		},
	})
	if err != nil {
		panic(fmt.Sprintf("drive the invocation: %v", err))
	}
	return turn
}

// learnModel is the scripted provider stand-in: it records every native request
// it is driven with and answers each with the scripted response the sub-case
// names. It never reaches a real endpoint.
type learnModel struct {
	mu       sync.Mutex
	name     string
	answer   func(req *model.Request) *model.Response
	requests []model.Request
}

// GenerateContent records the native request and streams the scripted response
// with the usage the reduction is expected to accumulate.
func (m *learnModel) GenerateContent(ctx context.Context, req *model.Request) (<-chan *model.Response, error) {
	m.mu.Lock()
	m.requests = append(m.requests, *req)
	answer := m.answer
	m.mu.Unlock()
	ch := make(chan *model.Response, 1)
	ch <- answer(req)
	close(ch)
	return ch, nil
}

// Info reports the scripted provider's descriptor.
func (m *learnModel) Info() model.Info {
	return model.Info{Name: m.name}
}

// learnTool is the native tool stand-in: it executes once per call and answers
// with its output.
type learnTool struct {
	name   string
	output string
}

// Declaration returns the declaration naming the fake tool.
func (t *learnTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the fake tool once and reports its output.
func (t *learnTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return t.output, nil
}

// learnCall builds one native tool call of a scripted assistant message.
func learnCall(id, name, args string) model.ToolCall {
	return model.ToolCall{
		ID:   id,
		Type: "function",
		Function: model.FunctionDefinitionParam{
			Name:      name,
			Arguments: []byte(args),
		},
	}
}

// learnToolTurn builds the scripted terminal response carrying native tool
// calls and the usage of the attempt.
func learnToolTurn(calls ...model.ToolCall) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, ToolCalls: calls}}},
		Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		Done:    true,
	}
}

// learnFinalTurn builds the scripted terminal response carrying the final
// answer and the usage of the attempt.
func learnFinalTurn(content string) *model.Response {
	return &model.Response{
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}},
		Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		Done:    true,
	}
}

// learnFailTurn builds the scripted terminal response carrying a provider
// error.
func learnFailTurn(message string) *model.Response {
	return &model.Response{
		Done:  true,
		Error: &model.ResponseError{Message: message, Type: model.ErrorTypeAPIError},
	}
}

// fileDigest reports the SHA-256 of one file's bytes.
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCausalTrainingRecords drives the runtime's real production points — the event loop and the task wrapper:
// - the production points write one episode, one decision per decision boundary and one attempt per observable attempt
// - a failed attempt keeps its error text beside the shared terminal
// - every record line carries its schema version and never the v2 capture shape
// - a record round-trips through its versioned line without losing fields
// - the minimal real production fields are kept: the native response, usage, error text, input events, model view and timestamps
// - two concurrent drives keep their attempts independent under the race
// - the v2 JSONL import is read-only and marks unknown lineage and unknown fields explicitly instead of guessing
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
		if len(records.episodes) != 1 {
			t.Fatalf("episode records = %d, want exactly one at the production point", len(records.episodes))
		}
		episode := records.episodes[0]
		if episode.Schema != rl.SchemaCurrent {
			t.Fatalf("episode schema = %q, want %q", episode.Schema, rl.SchemaCurrent)
		}
		if episode.ID != "ep-learn-1" {
			t.Fatalf("episode id = %q, want the application's own episode boundary", episode.ID)
		}
		if len(episode.Sessions) != 1 || episode.Sessions[0] != "session-learn-1" {
			t.Fatalf("episode sessions = %v, want the session the drive ran on", episode.Sessions)
		}
		if episode.Lineage != rl.LineageUnknown || episode.ParentID != "" || episode.RootID != "" {
			t.Fatalf("episode lineage = (%v, parent %q, root %q), want an explicit unknown lineage with no guessed parent", episode.Lineage, episode.ParentID, episode.RootID)
		}
		if episode.Timestamp.IsZero() {
			t.Fatalf("episode timestamp = %v, want the time the record was written", episode.Timestamp)
		}

		if len(records.decisions) != 2 {
			t.Fatalf("decision records = %d, want one per real decision boundary", len(records.decisions))
		}
		for i, decision := range records.decisions {
			if decision.Schema != rl.SchemaCurrent {
				t.Fatalf("decision %d schema = %q, want %q", i, decision.Schema, rl.SchemaCurrent)
			}
			if decision.ID != turn.Decisions[i].DecisionID {
				t.Fatalf("decision %d id = %q, want the real decision identity %q", i, decision.ID, turn.Decisions[i].DecisionID)
			}
			if decision.InvocationID != "inv-learn-1" || decision.EpisodeID != "ep-learn-1" {
				t.Fatalf("decision %d scope = (%q, %q), want the invocation and episode it belongs to", i, decision.InvocationID, decision.EpisodeID)
			}
			if decision.Timestamp.IsZero() {
				t.Fatalf("decision %d timestamp = %v, want the time the record was written", i, decision.Timestamp)
			}
			view := decision.ModelView
			if view.Model != "model-a" {
				t.Fatalf("decision %d model view model = %q, want the model the request ran on", i, view.Model)
			}
			if view.PromptVersion != "prompt-v1" {
				t.Fatalf("decision %d prompt version = %q, want the version the request ran with", i, view.PromptVersion)
			}
			if len(view.VisibleTools) != 1 || view.VisibleTools[0].Name != "echo-a" {
				t.Fatalf("decision %d visible tools = %v, want the tools the request actually saw", i, view.VisibleTools)
			}
			if view.VisibleTools[0].Version != "" {
				t.Fatalf("decision %d tool version = %q, want an unversioned member recorded as unversioned", i, view.VisibleTools[0].Version)
			}
		}
		if len(records.decisions[0].InputEvents) != 1 || records.decisions[0].InputEvents[0] != "ev-input-1" {
			t.Fatalf("first decision input events = %v, want the stored ready input it reduced", records.decisions[0].InputEvents)
		}
		if len(records.decisions[1].InputEvents) < 2 || records.decisions[1].InputEvents[0] != "ev-input-1" ||
			records.decisions[1].InputEvents[1] != "fact-call-1" {
			t.Fatalf("second decision input events = %v, want the ready input beside the settled fact fact-call-1", records.decisions[1].InputEvents)
		}

		if len(records.attempts) != 2 {
			t.Fatalf("attempt records = %d, want one per observable model attempt", len(records.attempts))
		}
		for i, attempt := range records.attempts {
			if attempt.Schema != rl.SchemaCurrent {
				t.Fatalf("attempt %d schema = %q, want %q", i, attempt.Schema, rl.SchemaCurrent)
			}
			if attempt.ID == "" || attempt.DecisionID != records.decisions[i].ID {
				t.Fatalf("attempt %d identity = (%q, %q), want one attempt bound to decision %q", i, attempt.ID, attempt.DecisionID, records.decisions[i].ID)
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
		if len(records.attempts) != 1 || len(records.decisions) != 1 {
			t.Fatalf("records = (%d decisions, %d attempts), want one of each at the production points", len(records.decisions), len(records.attempts))
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
		h.drive(ctx, "ep-learn-version", "run")
		records := h.records.snapshot()
		if len(records.episodes) != 1 || len(records.decisions) != 1 || len(records.attempts) != 1 {
			t.Fatalf("records = (%d, %d, %d), want one of each kind", len(records.episodes), len(records.decisions), len(records.attempts))
		}

		lines := make([][]byte, 0, 3)
		for _, line := range []rl.Line{
			rl.NewEpisodeLine(records.episodes[0]),
			rl.NewDecisionLine(records.decisions[0]),
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
			state := h.records.snapshot()
			all.episodes = append(all.episodes, state.episodes...)
			all.decisions = append(all.decisions, state.decisions...)
			all.attempts = append(all.attempts, state.attempts...)
		}
		if len(all.episodes) != 2 || len(all.decisions) != 2 || len(all.attempts) != 2 {
			t.Fatalf("records = (%d episodes, %d decisions, %d attempts), want one set per drive", len(all.episodes), len(all.decisions), len(all.attempts))
		}
		if all.episodes[0].ID == all.episodes[1].ID {
			t.Fatalf("episode ids = (%q, %q), want each drive to keep its own episode", all.episodes[0].ID, all.episodes[1].ID)
		}
		seen := map[event.AttemptID]bool{}
		for _, attempt := range all.attempts {
			if seen[attempt.ID] {
				t.Fatalf("attempt id %q written twice, want every attempt kept independent", attempt.ID)
			}
			seen[attempt.ID] = true
		}
	})

	t.Run("the legacy import reads old JSONL read-only and marks unknown lineage explicitly", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "legacy.jsonl")
		current, err := rl.MarshalLine(rl.NewEpisodeLine(rl.EpisodeRecord{ID: "ep-current"}))
		if err != nil {
			t.Fatalf("marshal the current schema line: %v", err)
		}
		lines := []string{
			`{"timestamp":1700000000,"session_id":"s-1","user_id":"u-1","batch_index":0,"llm_call":{"request":{"messages":[]},"response":{"choices":[]}},"metadata":{"model_endpoint":"http://x"}}`,
			`{"schema_version":2,"capture_scope":"sdk_request","run_id":"r-1","call_id":"c-1","response_id":"resp-1","request_digest":"d1","binding_status":"bound","missing_reasons":[],"owner":{"agent_name":"a"},"llm_call":{"request":{},"response":{}},"tool_calls":[],"tool_results":[],"metadata":{}}`,
			`{"schema_version":2,"call_id":"c-2","carrier_pigeon":"coo"}`,
			`{"llm_call":{"request":{"messages":[]}}}`,
			`{"schema_version":3,"call_id":"c-3"}`,
			`{"schema_version":"2.5","call_id":"c-4"}`,
			`{not json`,
			string(current),
		}
		body := strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the legacy source: %v", err)
		}
		before := fileDigest(t, path)

		result, err := rl.ImportFile(path)
		if err != nil {
			t.Fatalf("import the legacy source: %v", err)
		}
		if after := fileDigest(t, path); after != before {
			t.Fatalf("source digest = %s after import, want %s: the import must never write back", after, before)
		}
		if len(result.Records) != 1 || result.Records[0].Episode == nil || result.Records[0].Episode.ID != "ep-current" {
			t.Fatalf("imported records = %+v, want exactly the current schema line", result.Records)
		}
		if len(result.Legacy) != 4 {
			t.Fatalf("legacy records = %d, want the four legacy rows kept read-only", len(result.Legacy))
		}
		wantSchemas := []rl.SchemaVersion{rl.SchemaLegacyV1, rl.SchemaLegacyV2, rl.SchemaLegacyV2, rl.SchemaLegacyV1}
		wantLines := []int{1, 2, 3, 4}
		for i, legacy := range result.Legacy {
			if legacy.Schema != wantSchemas[i] || legacy.Line != wantLines[i] {
				t.Fatalf("legacy %d = (%q at line %d), want (%q at line %d)", i, legacy.Schema, legacy.Line, wantSchemas[i], wantLines[i])
			}
			if legacy.Lineage != rl.LineageUnknown {
				t.Fatalf("legacy %d lineage = %v, want an explicit unknown lineage", i, legacy.Lineage)
			}
			if len(legacy.Fields) == 0 || legacy.Raw == "" {
				t.Fatalf("legacy %d = %+v, want the raw fields kept as they stood", i, legacy)
			}
		}
		extra := result.Legacy[2]
		if len(extra.UnknownFields) != 1 || extra.UnknownFields[0] != "carrier_pigeon" {
			t.Fatalf("legacy unknown fields = %v, want the unknown field named explicitly", extra.UnknownFields)
		}
		if _, kept := extra.Fields["carrier_pigeon"]; !kept {
			t.Fatalf("legacy fields = %v, want the unknown field kept in the row", extra.Fields)
		}
		if len(result.Unknown) != 3 {
			t.Fatalf("gaps = %+v, want three explicit gaps", result.Unknown)
		}
		wantReasons := []string{rl.ImportReasonUnknownSchemaVersion, rl.ImportReasonUnknownSchemaVersion, rl.ImportReasonParseError}
		wantGapLines := []int{5, 6, 7}
		for i, gap := range result.Unknown {
			if gap.Reason != wantReasons[i] || gap.Line != wantGapLines[i] {
				t.Fatalf("gap %d = (%q at line %d), want (%q at line %d)", i, gap.Reason, gap.Line, wantReasons[i], wantGapLines[i])
			}
			if gap.Raw == "" {
				t.Fatalf("gap %d = %+v, want the raw bytes of the unreadable line kept", i, gap)
			}
		}
	})
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
	h.loop.SetRecorder(rl.NewRecorderAdapter(h.records))
	return h
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

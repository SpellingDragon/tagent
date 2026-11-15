package assembly

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestFoldWindowCarriesTheSummaryBudget pins the summary budget the fold window
// carries: a declaration naming only its model window takes the declaration
// default, and a declared budget passes through as it stands, so the fold reads
// the policy value instead of a constant.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFoldWindowCarriesTheSummaryBudget(t *testing.T) {
	undeclared := foldWindowOf(config.AgentSpec{ReplayWindow: 100})
	if undeclared.SummaryBudgetBytes != config.DefaultSummaryBudgetBytes {
		t.Fatalf("summary budget = %d, want the declaration default %d", undeclared.SummaryBudgetBytes, config.DefaultSummaryBudgetBytes)
	}
	declared := foldWindowOf(config.AgentSpec{ReplayWindow: 100, SummaryBudgetBytes: 512})
	if declared.SummaryBudgetBytes != 512 {
		t.Fatalf("summary budget = %d, want the declared 512", declared.SummaryBudgetBytes)
	}
}

// TestBandSummaryPromptStatesTheDeclaredBudget pins the instruction a band
// summary is asked under: the byte bound it states is the declared budget, so no
// fixed literal bound stands beside a declaration that names another one.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestBandSummaryPromptStatesTheDeclaredBudget(t *testing.T) {
	instruction := bandSummaryInstruction(512)
	if !strings.Contains(instruction, "within 512 bytes") {
		t.Fatalf("instruction = %q, want it to state the declared 512-byte bound", instruction)
	}
	if strings.Contains(instruction, "2048") {
		t.Fatalf("instruction = %q, want no fixed literal bound beside the declared one", instruction)
	}
}

// TestCompressionSummaryCutsToTheDeclaredBudget pins the cut the consumer applies
// to one model answer: the recorded text answers the declared budget, not the
// policy default.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestCompressionSummaryCutsToTheDeclaredBudget(t *testing.T) {
	ctx := context.Background()
	gists := &fakeSummaryGist{answer: func(string) (string, error) {
		return strings.Repeat("[11-06 12:00|gen:?] one settled decision\n", 200), nil
	}}
	consumer := NewCompressionConsumer(nil).withSummaryGist(gists)
	got := consumer.summarizeProduct(ctx, "session-gist", mechanicalSummary(t), 512)
	if len(got.Entry.Content) > 512 {
		t.Fatalf("summary carries %d bytes, want at most the declared 512", len(got.Entry.Content))
	}
	if greedy := len(strings.Repeat("[11-06 12:00|gen:?] one settled decision\n", 200)); greedy <= 512 {
		t.Fatalf("the scripted answer carries %d bytes, want it longer than the bound the cut applies", greedy)
	}
	if len(gists.budgets) != 1 || gists.budgets[0] != 512 {
		t.Fatalf("the gist face was asked under %v, want the declared 512-byte budget", gists.budgets)
	}
}

// TestBandSummaryGistReadsTheLease pins the model face a band summary asks: an
// agent holding a model lease is served by the lease's version in effect, never
// by the model the host contributed beside it.
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestBandSummaryGistReadsTheLease(t *testing.T) {
	ctx := context.Background()
	leased := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the leased answer"}}}, Done: true}
	}}
	contributed := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the contributed answer"}}}, Done: true}
	}}
	lease, err := rl.NewModelLease(leased, "model-a@v1")
	if err != nil {
		t.Fatalf("build the model lease: %v", err)
	}
	run := windowChainRun(t, ctx, WithModelLease("agent-a", lease))
	installRunModel(t, run, contributed)
	session := event.SessionID("session-gist-lease")
	if err := run.attachScopedReplay(event.Address{AgentID: "agent-a", SessionID: session}, FoldWindow{MaxTokens: 100}, replay.NewReplayProjection()); err != nil {
		t.Fatalf("attach the session projection: %v", err)
	}

	gist, err := (bandSummaries{run: run}).SummaryGist(ctx, session, "[11-06 12:00|gen:?] external_input: ask", replay.DefaultAgeSummaryTextLimit)
	if err != nil {
		t.Fatalf("SummaryGist: %v", err)
	}
	if gist != "the leased answer" {
		t.Fatalf("gist = %q, want the leased version's answer: the summary asks the model face the agent's own requests ask", gist)
	}
	if quoted := contributed.requests; len(quoted) != 0 {
		t.Fatalf("the contributed model was asked %d time(s), want the lease to serve the summary", len(quoted))
	}
}

// TestBandSummaryGistCarriesTheDeclaredSampling pins the sampling a band summary
// request carries: the temperature and the output limit the agent declares reach
// the provider although the request itself names none.
//
// 契约: docs/wiki/rl/rl-architecture.md#swappable-model
func TestBandSummaryGistCarriesTheDeclaredSampling(t *testing.T) {
	ctx := context.Background()
	temperature := 0.25
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "the declared answer"}}}, Done: true}
	}}
	run, err := Assemble(ctx, &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:           "agent-a",
			Model:        "model-a",
			ReplayWindow: 100,
			Temperature:  &temperature,
			MaxTokens:    777,
		}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}},
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	session := event.SessionID("session-gist-sampling")
	if err := run.attachScopedReplay(event.Address{AgentID: "agent-a", SessionID: session}, FoldWindow{MaxTokens: 100}, replay.NewReplayProjection()); err != nil {
		t.Fatalf("attach the session projection: %v", err)
	}
	if _, err := (bandSummaries{run: run}).SummaryGist(ctx, session, "[11-06 12:00|gen:?] external_input: ask", replay.DefaultAgeSummaryTextLimit); err != nil {
		t.Fatalf("SummaryGist: %v", err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.requests) != 1 {
		t.Fatalf("the provider saw %d request(s), want the one band summary request", len(provider.requests))
	}
	asked := provider.requests[0]
	if asked.Temperature == nil || *asked.Temperature != temperature {
		t.Fatalf("summary request temperature = %v, want the declared %v", asked.Temperature, temperature)
	}
	if asked.MaxTokens == nil || *asked.MaxTokens != 777 {
		t.Fatalf("summary request max tokens = %v, want the declared 777", asked.MaxTokens)
	}
}

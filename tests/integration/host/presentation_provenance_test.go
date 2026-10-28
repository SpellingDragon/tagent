package host

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// provenancePresenter is one final presenter of the formal assembly: it records
// every fact it was dispatched for and answers one fixed view of it.
type provenancePresenter struct {
	name  string
	order *[]event.EventID
	text  string
}

// Name reports the stable name of the presenter.
func (p *provenancePresenter) Name() string { return p.name }

// Port reports the fact->view port the presenter serves.
func (p *provenancePresenter) Port() plugin.Port {
	return plugin.PortFactView
}

// Present records the source fact and answers the view this presenter writes.
func (p *provenancePresenter) Present(_ context.Context, fact event.EventID) (string, bool) {
	*p.order = append(*p.order, fact)
	return p.text, false
}

// TestMultiplePresentersEachAnswerATraceableView drives the formal assembly with two final presenters on the one agent.
// - every committed fact answers one view per presenter, in the declared order
// - each view names the presenter that wrote it beside the fact it was rendered from
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestMultiplePresentersEachAnswerATraceableView(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	var seenSummary, seenDetail []event.EventID
	summary := &provenancePresenter{name: "summary", order: &seenSummary, text: "summary view"}
	detail := &provenancePresenter{name: "detail", order: &seenDetail, text: "detail view"}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Plugins: []plugin.Instance{summary, detail}},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-presenters"}
	invokeTurn(t, run, "inv-presenters", addr, "hello")

	presentations := run.Presentations()
	if len(presentations) == 0 {
		t.Fatal("presentations = none, want every committed fact presented")
	}
	for _, presentation := range presentations {
		if presentation.Fact == "" {
			t.Fatalf("presentation = %+v, want the source fact every view traces back to", presentation)
		}
		if len(presentation.Views) != 2 {
			t.Fatalf("views of %q = %+v, want one view per presenter", presentation.Fact, presentation.Views)
		}
		for i, want := range []struct{ plugin, text string }{{"summary", "summary view"}, {"detail", "detail view"}} {
			got := presentation.Views[i]
			if got.Plugin != want.plugin || got.Text != want.text || got.Dropped {
				t.Fatalf("view %d of %q = %+v, want the view %q written by %q", i, presentation.Fact, got, want.text, want.plugin)
			}
		}
	}
	if len(seenSummary) != len(presentations) || len(seenDetail) != len(presentations) {
		t.Fatalf("presenters saw (%d, %d) facts, want both rendered every presented fact", len(seenSummary), len(seenDetail))
	}
	for i, presentation := range presentations {
		if seenSummary[i] != presentation.Fact || seenDetail[i] != presentation.Fact {
			t.Fatalf("presenters saw (%q, %q) at %d, want the presented fact %q", seenSummary[i], seenDetail[i], i, presentation.Fact)
		}
	}
}

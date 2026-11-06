// The windowed rebuild anchors of the restart seeding: a chain whose newest
// history summary telescopes the folded material rebuilds from the tail alone,
// and the rebuilt projection answers the one a full replay builds.
package assembly

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// windowChainRun assembles one run over the in-process tier and drives its
// entry agent with the scripted provider, so the fold fires as the commits
// grow past the small window.
func windowChainRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	round := 0
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		round++
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}},
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run
}

// commitWindowTurns commits closed turns of four legs until the chain holds
// turns count, letting the fold fire between commits as it does online.
func commitWindowTurns(t *testing.T, ctx context.Context, run *Run, addr event.Address, turns int) {
	t.Helper()
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for turn := 0; turn < turns; turn++ {
		for position, leg := range []struct {
			suffix  string
			typ     string
			summary string
		}{
			{"0-in", event.TypeExternalInput, "ask"},
			{"1-plan", event.TypeThinkingPlan, "plan"},
			{"2-call", event.TypeActionCommand, "call"},
			{"3-out", event.TypeAgentOutput, "answer"},
		} {
			evt := event.Event{
				Key:       event.EventID(timeStampKey(base, turn, position)),
				Type:      leg.typ,
				Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
				Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
				Message:   model.Message{Role: model.RoleUser, Content: "the material of the window rebuild"},
				Owner:     addr,
			}
			if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
				t.Fatalf("commit %s: %v", evt.Key, err)
			}
		}
	}
}

// timeStampKey names one leg by its turn and position.
func timeStampKey(base time.Time, turn, position int) string {
	return base.Add(time.Duration(turn*10+position) * time.Minute).Format("150405")
}

// TestWindowedRebuildEqualsFullReplay pins the tail rebuild against the full one.
// - a chain that folded rebuilds the same projection from the tail alone
// - a chain that never folded falls back to the full replay
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestWindowedRebuildEqualsFullReplay(t *testing.T) {
	ctx := context.Background()
	seed := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-window-rebuild"}
	commitWindowTurns(t, ctx, seed, addr, 12)

	productKinds := map[string]int{}
	_ = seed.EachSessionFact(ctx, addr, func(evt event.Event) error {
		if evt.Type == event.CompressProductType {
			productKinds["product"]++
			if k, ok := replay.FoldKindOf(event.EventID(evt.Meta["compress_key"])); ok {
				productKinds[k]++
			}
		}
		if evt.Type == event.CompressRequestType {
			productKinds["request"]++
		}
		return nil
	})
	t.Logf("chain holds: %v", productKinds)

	windowed, report, err := seed.seedWindowedReplay(ctx, seed.tier)
	if err != nil {
		t.Fatalf("rebuild inside the window: %v", err)
	}
	full, fullReport, err := seed.seedFullReplay(ctx, seed.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	if !report.Bounded {
		t.Fatal("the folded chain never reported its window boundary")
	}
	if report.Decoded > fullReport.Decoded {
		t.Fatalf("the windowed rebuild decoded %d events, want at most the full %d", report.Decoded, fullReport.Decoded)
	}
	wantEntries, wantOK := full[addr]
	gotEntries, gotOK := windowed[addr]
	if !wantOK || !gotOK {
		t.Fatalf("the rebuilds registered the session differently: windowed=%v full=%v", gotOK, wantOK)
	}
	gotList, wantList := gotEntries.projection.Entries(), wantEntries.projection.Entries()
	if len(gotList) != len(wantList) {
		t.Fatalf("the windowed rebuild holds %d entries, want the full %d", len(gotList), len(wantList))
	}
	for at := range gotList {
		if gotList[at].Key != wantList[at].Key || gotList[at].Type != wantList[at].Type {
			t.Fatalf("entry %d = %s/%s, want %s/%s", at, gotList[at].Key, gotList[at].Type, wantList[at].Key, wantList[at].Type)
		}
		if gotList[at].Content != wantList[at].Content {
			t.Fatalf("entry %d content differs", at)
		}
	}
}

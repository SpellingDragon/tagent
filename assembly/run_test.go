package assembly

import (
	"time"

	"context"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/model"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// windowChainRun assembles one run over the in-process tier with a small replay
// window, so the fold fires as the commits grow past it.
func windowChainRun(t *testing.T, ctx context.Context) *Run {
	t.Helper()
	round := 0
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		round++
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 60}, {ID: "agent-b", Model: "model-a", MaxTokens: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}},
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
			typ     string
			summary string
		}{
			{event.TypeExternalInput, "ask"},
			{event.TypeThinkingPlan, "plan"},
			{event.TypeActionCommand, "call"},
			{event.TypeAgentOutput, "answer"},
		} {
			evt := event.Event{
				Key:       event.EventID(timeStampKey(base, addr, turn, position)),
				Type:      leg.typ,
				Timestamp: base.Add(time.Duration(turn*10+position) * time.Minute),
				Meta:      map[string]string{event.MetaKeyEventSummary: leg.summary},
				Message:   model.Message{Role: model.RoleUser, Content: "the material of the landing walk"},
				Owner:     addr,
			}
			if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
				t.Fatalf("commit %s: %v", evt.Key, err)
			}
		}
	}
}

// timeStampKey names one leg by its chain, turn and position.
func timeStampKey(base time.Time, addr event.Address, turn, position int) string {
	return string(addr.AgentID) + base.Add(time.Duration(turn*10+position)*time.Minute).Format("150405")
}

// TestLandedSeqRidesTheWalk pins the restart landing sequence.
// - the single rebuild walk answers the last slot every partition committed
// - the map equals an independent newest-first read of the store
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func TestLandedSeqRidesTheWalk(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-walk-a"}
	other := event.Address{AgentID: "agent-b", SessionID: "session-walk-b"}
	commitWindowTurns(t, ctx, run, addr, 3)
	commitWindowTurns(t, ctx, run, other, 2)

	landed, err := run.seedReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the chains: %v", err)
	}
	want := map[int]int64{}
	if err := run.tier.Segments.EachEventNewestFirst(ctx, func(stored ledger.StoredEvent) error {
		if stored.Seq > want[stored.PartitionID] {
			want[stored.PartitionID] = stored.Seq
		}
		return nil
	}); err != nil {
		t.Fatalf("read the store newest first: %v", err)
	}
	if len(landed) != len(want) {
		t.Fatalf("the walk answered %d partitions, want the %d the store holds", len(landed), len(want))
	}
	for partition, seq := range want {
		if landed[partition] != seq {
			t.Fatalf("partition %d lands after seq %d, want %d", partition, landed[partition], seq)
		}
	}
}

// TestAnchoredRebuildEqualsFullWalk pins the fold anchor theorem.
//   - the newest product fact of each session carries the post-batch survivor
//     list, and rebuilding from the anchor answers the full walk entry by entry
//   - the anchor path decodes fewer bodies than the full walk on a folded chain
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildEqualsFullWalk(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-anchor"}
	commitWindowTurns(t, ctx, run, addr, 12)

	full, _, fullReport, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	anchored, anchoredReport, err := run.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchors: %v", err)
	}
	if anchoredReport.Anchors == 0 {
		t.Fatal("the folded chain minted no anchor the rebuild could use")
	}
	if anchoredReport.Decoded >= fullReport.Decoded {
		t.Fatalf("the anchored rebuild decoded %d bodies, want fewer than the full walk %d", anchoredReport.Decoded, fullReport.Decoded)
	}
	want, carried := full[addr]
	if !carried {
		t.Fatal("the full walk registered no projection for the session")
	}
	got, carried := anchored[addr]
	if !carried {
		t.Fatal("the anchored rebuild registered no projection for the session")
	}
	wantList, gotList := want.projection.Entries(), got.projection.Entries()
	if len(gotList) != len(wantList) {
		t.Fatalf("the anchored rebuild holds %d entries, want the full walk %d", len(gotList), len(wantList))
	}
	for at := range gotList {
		if gotList[at].Key != wantList[at].Key || gotList[at].Type != wantList[at].Type || gotList[at].Content != wantList[at].Content {
			t.Fatalf("entry %d = %s/%s, want %s/%s", at, gotList[at].Key, gotList[at].Type, wantList[at].Key, wantList[at].Type)
		}
	}
}

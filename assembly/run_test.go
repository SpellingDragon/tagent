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
// - the newest product fact of each session carries the post-batch survivors
// - rebuilding from the anchor answers the full walk entry by entry
// - the anchor path decodes fewer bodies than the full walk
// - the index answers the same landed slots as the full walk
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildEqualsFullWalk(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-anchor"}
	commitWindowTurns(t, ctx, run, addr, 12)

	full, fullLanded, fullReport, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	anchored, anchoredReport, err := run.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchors: %v", err)
	}
	t.Logf("full decoded=%d anchored decoded=%d anchors=%d", fullReport.Decoded, anchoredReport.Decoded, anchoredReport.Anchors)
	if anchoredReport.Anchors == 0 {
		t.Fatal("the folded chain minted no anchor the rebuild could use")
	}
	if anchoredReport.Decoded >= fullReport.Decoded {
		t.Fatalf("the anchored rebuild decoded %d bodies, want fewer than the full walk %d", anchoredReport.Decoded, fullReport.Decoded)
	}
	if len(anchoredReport.Landed) != len(fullLanded) {
		t.Fatalf("the index answered %d landed partitions, want the %d the full walk holds", len(anchoredReport.Landed), len(fullLanded))
	}
	for partition, seq := range fullLanded {
		if anchoredReport.Landed[partition] != seq {
			t.Fatalf("partition %d lands after seq %d, want %d", partition, anchoredReport.Landed[partition], seq)
		}
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
		got, want := gotList[at], wantList[at]
		if got.Key != want.Key || got.Type != want.Type || got.Content != want.Content || got.Summary != want.Summary || !got.Time.Equal(want.Time) || got.StatedRole() != want.StatedRole() || got.Generation() != want.Generation() {
			t.Fatalf("entry %d = %s/%s/%q/%s, want %s/%s/%q/%s", at, got.Key, got.Type, got.Summary, got.Time, want.Key, want.Type, want.Summary, want.Time)
		}
	}
}

// TestAnchorRefusesBlankType pins the type-based discovery rule.
// - an index reporting no frozen event type finds no anchor by key spelling
// - the rebuild falls back to the full walk of every chain
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchorRefusesBlankType(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-blank-type"}
	commitWindowTurns(t, ctx, run, addr, 12)

	full, _, fullReport, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	seeds, report, err := run.seedAnchoredReplayBlankTypes(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild with no typed index: %v", err)
	}
	if report.Anchors != 0 {
		t.Fatalf("the rebuild found %d anchors with no frozen event type, want none", report.Anchors)
	}
	if report.Decoded < fullReport.Decoded {
		t.Fatalf("the rebuild decoded %d bodies, want the full-walk fallback %d", report.Decoded, fullReport.Decoded)
	}
	if report.Fallbacks == 0 {
		t.Fatal("the rebuild reported no full-walk fallback")
	}
	if len(seeds) != len(full) {
		t.Fatalf("the fallback registered %d chains, want the %d the full walk holds", len(seeds), len(full))
	}
}

// TestAnchorUnreadablePayloadFallsBack pins the corrupt-anchor rule.
// - a product whose survivor payload cannot be decoded is refused by name
// - the chain is rebuilt from every committed fact, not invented material
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchorUnreadablePayloadFallsBack(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-corrupt-anchor"}
	commitWindowTurns(t, ctx, run, addr, 12)

	full, _, _, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	seeds, report, err := run.seedAnchoredReplayCorrupt(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild with a corrupt anchor: %v", err)
	}
	if report.Anchors != 0 {
		t.Fatalf("the rebuild accepted %d corrupt anchor(s), want none", report.Anchors)
	}
	if report.Fallbacks == 0 {
		t.Fatal("the rebuild reported no full-walk fallback after refusing the anchor")
	}
	if len(seeds) != len(full) {
		t.Fatalf("the fallback registered %d chains, want the %d the full walk holds", len(seeds), len(full))
	}
	want, carried := full[addr]
	if !carried {
		t.Fatal("the full walk registered no projection for the session")
	}
	got, carried := seeds[addr]
	if !carried {
		t.Fatal("the fallback registered no projection for the session")
	}
	wantList, gotList := want.projection.Entries(), got.projection.Entries()
	if len(gotList) != len(wantList) {
		t.Fatalf("the fallback holds %d entries, want the full walk %d", len(gotList), len(wantList))
	}
	for at := range gotList {
		if gotList[at].Key != wantList[at].Key || gotList[at].Type != wantList[at].Type || gotList[at].Content != wantList[at].Content {
			t.Fatalf("entry %d = %s/%s, want %s/%s", at, gotList[at].Key, gotList[at].Type, wantList[at].Key, wantList[at].Type)
		}
	}
}

// seedAnchoredReplayBlankTypes runs the anchored rebuild over an index whose
// frozen event types are all blank, so no product can be found by spelling.
func (r *Run) seedAnchoredReplayBlankTypes(ctx context.Context, tier *ledger.Tier) (map[event.Address]*sessionSeed, seedReport, error) {
	return r.seedAnchoredReplayWith(ctx, tier, anchoredSeam{mutateRefs: func(refs []anchorRef) {
		for i := range refs {
			refs[i].typ = ""
		}
	}})
}

// seedAnchoredReplayCorrupt runs the anchored rebuild over a chain whose newest
// product carries an unreadable survivor payload: the anchor is refused by name
// and the chain falls back to its full replay, with every fact still in place.
func (r *Run) seedAnchoredReplayCorrupt(ctx context.Context, tier *ledger.Tier) (map[event.Address]*sessionSeed, seedReport, error) {
	return r.seedAnchoredReplayWith(ctx, tier, anchoredSeam{mutateAnchor: func(evt event.Event) event.Event {
		evt.Meta[metaKeyCompressSurvivors] = "{not-a-payload"
		return evt
	}})
}

// TestMechanismFactsStayOutOfTheQueryFace pins the wording rule.
// - a compression mechanism fact carries an internal summary as recovery metadata
// - the query face refuses it by frozen type, so it never reaches the model view
// - a business fact still lands with its own text and type
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestMechanismFactsStayOutOfTheQueryFace(t *testing.T) {
	for _, typ := range []string{event.CompressProductType, event.CompressRequestType} {
		evt := event.Event{
			Key:     event.EventID("cmpout-" + typ),
			Type:    typ,
			Meta:    map[string]string{event.MetaKeyEventSummary: "compress product -hist-internal"},
			Message: model.Message{Content: "internal wording"},
			Owner:   event.Address{AgentID: "agent-a", SessionID: "session-wording"},
		}
		if _, held := eventRecordOf(evt); held {
			t.Fatalf("the %s fact leaked its mechanism wording into the query face", typ)
		}
	}
	business := event.Event{
		Key:     "134000",
		Type:    event.TypeExternalInput,
		Meta:    map[string]string{event.MetaKeyEventSummary: "ask"},
		Message: model.Message{Role: model.RoleUser, Content: "the material of the landing walk"},
		Owner:   event.Address{AgentID: "agent-a", SessionID: "session-wording"},
	}
	record, held := eventRecordOf(business)
	if !held {
		t.Fatal("a business fact was refused by the query face")
	}
	if record.Content != "the material of the landing walk" || record.Type != event.TypeExternalInput {
		t.Fatalf("the business record = %q/%s, want the fact's own text and type", record.Content, record.Type)
	}
}

// TestNeverFoldedSessionFallsBack pins the no-anchor rule.
// - a chain whose window never folds mints no product and finds no anchor
// - the rebuild replays every committed fact and equals the full walk
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestNeverFoldedSessionFallsBack(t *testing.T) {
	ctx := context.Background()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 100000}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}},
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	if err != nil {
		t.Fatalf("assemble the wide-window run: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-never-folded"}
	commitWindowTurns(t, ctx, run, addr, 3)

	full, _, _, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	seeds, report, err := run.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild the never-folded chain: %v", err)
	}
	if report.Anchors != 0 {
		t.Fatalf("the rebuild found %d anchor(s) on a chain that never folded", report.Anchors)
	}
	if report.Fallbacks != 1 {
		t.Fatalf("the rebuild reported %d fallback(s), want one per never-folded chain", report.Fallbacks)
	}
	want, carried := full[addr]
	if !carried {
		t.Fatal("the full walk registered no projection for the session")
	}
	got, carried := seeds[addr]
	if !carried {
		t.Fatal("the rebuild registered no projection for the session")
	}
	wantList, gotList := want.projection.Entries(), got.projection.Entries()
	if len(gotList) != len(wantList) {
		t.Fatalf("the rebuild holds %d entries, want the full walk %d", len(gotList), len(wantList))
	}
	for at := range gotList {
		if gotList[at].Key != wantList[at].Key || gotList[at].Type != wantList[at].Type || gotList[at].Content != wantList[at].Content {
			t.Fatalf("entry %d = %s/%s, want %s/%s", at, gotList[at].Key, gotList[at].Type, wantList[at].Key, wantList[at].Type)
		}
	}
}

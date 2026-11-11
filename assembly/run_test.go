package assembly

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"context"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/model"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// windowChainRun assembles one run with a small replay window, so the fold
// fires as the commits grow past it. Later options override the memory default.
func windowChainRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	round := 0
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		round++
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	options := append([]Option{
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
	}, opts...)
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 60}, {ID: "agent-b", Model: "model-a", MaxTokens: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, options...)
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
	run.stopCheckpointFiler()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-walk-a"}
	other := event.Address{AgentID: "agent-b", SessionID: "session-walk-b"}
	commitWindowTurns(t, ctx, run, addr, 3)
	commitWindowTurns(t, ctx, run, other, 2)

	landed, err := run.seedReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the chains: %v", err)
	}
	want := map[int]int64{}
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(_ event.EventID, partition int, seq int64, _ string, _ event.SessionID) error {
		if seq > want[partition] {
			want[partition] = seq
		}
		return nil
	}); err != nil {
		t.Fatalf("read the store index newest first: %v", err)
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
// - the checkpoint filer ends before the walk that arms it, so the chain the two reads compare stands still
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildEqualsFullWalk(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	run.stopCheckpointFiler()
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
	if gotRendered, wantRendered := replay.RenderProjection(gotList), replay.RenderProjection(wantList); !bytes.Equal(gotRendered, wantRendered) {
		t.Fatalf("the anchored rebuild renders %d bytes, want the full walk %d", len(gotRendered), len(wantRendered))
	}
}

// TestAnchorRefusesBlankType pins the type-based discovery rule.
// - an index reporting no frozen event type finds no anchor by key spelling
// - the rebuild falls back to the full walk of every chain
// - a chain the framework checkpoint states is rebuilt from it, so the recovered chains sit beside the ones the checkpoint covered
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
	seeds, report, err := run.seedAnchoredReplay(ctx, blankTypedTier(t, ctx, run.tier))
	if err != nil {
		t.Fatalf("rebuild with no typed index: %v", err)
	}
	if report.Anchors != 0 {
		t.Fatalf("the rebuild found %d anchors with no frozen event type, want none", report.Anchors)
	}
	if report.Decoded < fullReport.Decoded {
		t.Fatalf("the rebuild decoded %d bodies, want the full-walk fallback %d", report.Decoded, fullReport.Decoded)
	}
	if report.Fallbacks+report.Checkpoints == 0 {
		t.Fatal("the rebuild reported no checkpoint for a chain whose anchor was refused")
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
	if _, err := run.commitFactChecked(ctx, corruptAnchorEvent(addr)); err != nil {
		t.Fatalf("commit the corrupt anchor: %v", err)
	}
	seeds, report, err := run.seedAnchoredReplay(ctx, run.tier)
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

// blankTypedTier answers the events of one tier under an index that carries no
// frozen event type, the index state the type-based discovery rule is judged
// against: every body is re-committed byte for byte at the position it holds, so
// nothing but its type could name an anchor.
func blankTypedTier(t *testing.T, ctx context.Context, source *ledger.Tier) *ledger.Tier {
	t.Helper()
	opened, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the tier whose index carries no frozen type: %v", err)
	}
	type position struct {
		key       event.EventID
		partition int
		seq       int64
		session   event.SessionID
	}
	var positions []position
	if err := source.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, _ string, session event.SessionID) error {
		positions = append(positions, position{key: key, partition: partition, seq: seq, session: session})
		return nil
	}); err != nil {
		t.Fatalf("walk the source index: %v", err)
	}
	for _, at := range positions {
		stored, held, err := source.Segments.GetEvent(ctx, at.key)
		if err != nil {
			t.Fatalf("read the stored event %s: %v", at.key, err)
		}
		if !held {
			t.Fatalf("the index names %s but the tier holds no body for it", at.key)
		}
		record := ledger.SegmentRecord{
			PartitionID: stored.PartitionID,
			WindowTS:    stored.WindowTS,
			Seq:         stored.Seq,
			EventKey:    stored.Key,
			EventTime:   stored.WindowTS,
			Body:        stored.Body,
		}.WithChain("", at.session)
		if err := opened.Segments.StoreEvent(ctx, record); err != nil {
			t.Fatalf("re-commit the event %s under no frozen type: %v", at.key, err)
		}
	}
	return opened
}

// corruptAnchorEvent renders the product one chain files with a survivor payload
// no reader can decode: the fact keeps every field of its product and answers no
// anchor, so the chain falls back to its full replay.
func corruptAnchorEvent(addr event.Address) event.Event {
	request := event.CompressRequestFor("assembly.test", "the corrupt anchor", "the fixture states a survivor payload no reader decodes")
	product := event.CompressProductFor(request.ID, compressionProducer, event.EventID("-corrupt-anchor"), nil, "the corrupt summary")
	evt := compressProductEvent(addr.SessionID, addr.AgentID, product, time.Now().UTC())
	evt.Meta[metaKeyCompressSurvivors] = "{not-a-payload"
	return evt
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
	if report.Fallbacks+report.Checkpoints != 1 {
		t.Fatalf("the rebuild reported %d fallback(s) beside %d checkpoint(s), want one checkpoint for the never-folded chain", report.Fallbacks, report.Checkpoints)
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

// TestAnchoredRebuildReplaysTheTail pins the post-anchor replay rule.
// - facts committed after the newest anchor are replayed in commit order
// - the anchored projection still equals the full walk field by field
// - facts land through commitOnly, which skips the fold observer
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildReplaysTheTail(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-tail"}
	commitWindowTurns(t, ctx, run, addr, 12)

	base := time.Date(2026, 11, 6, 14, 0, 0, 0, time.UTC)
	for at := 0; at < 3; at++ {
		evt := event.Event{
			Key:       event.EventID("tail-" + string(rune('a'+at))),
			Type:      event.TypeExternalInput,
			Timestamp: base.Add(time.Duration(at) * time.Minute),
			Meta:      map[string]string{event.MetaKeyEventSummary: "tail ask"},
			Message:   model.Message{Role: model.RoleUser, Content: "a fact committed after the fold"},
			Owner:     addr,
		}
		if _, err := run.commitOnly(ctx, addr, evt); err != nil {
			t.Fatalf("commit the tail fact %s: %v", evt.Key, err)
		}
	}

	full, _, fullReport, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	anchored, anchoredReport, err := run.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchor: %v", err)
	}
	if anchoredReport.Anchors != 1 {
		t.Fatalf("the rebuild found %d anchor(s), want one", anchoredReport.Anchors)
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
		t.Fatal("the rebuild registered no projection for the session")
	}
	wantList, gotList := want.projection.Entries(), got.projection.Entries()
	if len(gotList) != len(wantList) {
		t.Fatalf("the rebuild holds %d entries, want the full walk %d", len(gotList), len(wantList))
	}
	for at := range gotList {
		got, want := gotList[at], wantList[at]
		if got.Key != want.Key || got.Type != want.Type || got.Content != want.Content || got.Summary != want.Summary || !got.Time.Equal(want.Time) {
			t.Fatalf("entry %d = %s/%s, want %s/%s", at, got.Key, got.Type, want.Key, want.Type)
		}
	}
	if gotRendered, wantRendered := replay.RenderProjection(gotList), replay.RenderProjection(wantList); !bytes.Equal(gotRendered, wantRendered) {
		t.Fatalf("the rebuild renders %d bytes, want the full walk %d", len(gotRendered), len(wantRendered))
	}
}

// TestAnchoredRebuildKeepsAgentChainsApart pins the ChainKey isolation rule.
// - two agents that share one session id are rebuilt as two chains
// - the anchored seeds match the full walk key for key
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildKeepsAgentChainsApart(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	shared := event.SessionID("session-shared")
	a := event.Address{AgentID: "agent-a", SessionID: shared}
	b := event.Address{AgentID: "agent-b", SessionID: shared}
	commitWindowTurns(t, ctx, run, a, 12)
	commitWindowTurns(t, ctx, run, b, 3)

	full, _, _, err := run.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	anchored, _, err := run.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchors: %v", err)
	}
	if len(full) != 2 {
		t.Fatalf("the full walk registered %d chains, want the two agent chains", len(full))
	}
	if len(anchored) != len(full) {
		t.Fatalf("the rebuild registered %d chains, want the %d the full walk holds", len(anchored), len(full))
	}
	for addr, want := range full {
		got, carried := anchored[addr]
		if !carried {
			t.Fatalf("the rebuild dropped the chain %v", addr)
		}
		wantList, gotList := want.projection.Entries(), got.projection.Entries()
		if len(gotList) != len(wantList) {
			t.Fatalf("chain %v rebuild holds %d entries, want the full walk %d", addr, len(gotList), len(wantList))
		}
		for at := range gotList {
			if gotList[at].Key != wantList[at].Key || gotList[at].Type != wantList[at].Type || gotList[at].Content != wantList[at].Content {
				t.Fatalf("chain %v entry %d = %s/%s, want %s/%s", addr, at, gotList[at].Key, gotList[at].Type, wantList[at].Key, wantList[at].Type)
			}
		}
	}
}

// TestAnchoredRebuildOnAFreshRun pins the cold-boot shape.
// - a fresh run seeded from the same tier answers the same projection
// - the anchored ledger holds only the survivors and the tail, not the full chain
// - two fresh runs share one in-process tier: one full walk, one anchored
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildOnAFreshRun(t *testing.T) {
	ctx := context.Background()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	newRun := func() *Run {
		t.Helper()
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
	first := newRun()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-fresh"}
	commitWindowTurns(t, ctx, first, addr, 12)

	fullRun := newRun()
	fullRun.tier = first.tier
	anchoredRun := newRun()
	anchoredRun.tier = first.tier

	full, _, fullReport, err := fullRun.seedFullReplay(ctx, first.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	anchored, anchoredReport, err := anchoredRun.seedAnchoredReplay(ctx, first.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchor: %v", err)
	}
	if anchoredReport.Anchors != 1 {
		t.Fatalf("the fresh rebuild found %d anchor(s), want one", anchoredReport.Anchors)
	}
	if anchoredReport.Decoded >= fullReport.Decoded {
		t.Fatalf("the fresh rebuild decoded %d bodies, want fewer than the full walk %d", anchoredReport.Decoded, fullReport.Decoded)
	}
	want, carried := full[addr]
	if !carried {
		t.Fatal("the full walk registered no projection for the session")
	}
	got, carried := anchored[addr]
	if !carried {
		t.Fatal("the fresh rebuild registered no projection for the session")
	}
	if gotRendered, wantRendered := replay.RenderProjection(got.projection.Entries()), replay.RenderProjection(want.projection.Entries()); !bytes.Equal(gotRendered, wantRendered) {
		t.Fatalf("the fresh rebuild renders %d bytes, want the full walk %d", len(gotRendered), len(wantRendered))
	}
	anchoredRun.mu.Lock()
	seeded := len(anchoredRun.factOrder)
	anchoredRun.mu.Unlock()
	if seeded >= fullReport.Decoded {
		t.Fatalf("the anchored ledger seeded %d facts, want a sparse subset of the %d the full walk decoded", seeded, fullReport.Decoded)
	}
	if seeded == 0 {
		t.Fatal("the anchored ledger seeded no facts at all")
	}
}

// TestFullReplayUsesCommitOrder pins partition sequence over event timestamps.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestFullReplayUsesCommitOrder(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-commit-order"}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for _, item := range []struct {
		key event.EventID
		at  time.Time
	}{
		{key: "later-commit", at: at.Add(2 * time.Hour)},
		{key: "earlier-commit", at: at.Add(time.Hour)},
	} {
		evt := event.Event{Key: item.key, Type: event.TypeExternalInput, Timestamp: item.at, Meta: map[string]string{event.MetaKeyEventSummary: "ask"}, Message: model.Message{Role: model.RoleUser, Content: "ordered"}, Owner: addr}
		if _, err := run.CommitToSession(ctx, addr, evt); err != nil {
			t.Fatalf("commit %s: %v", item.key, err)
		}
	}
	fresh := windowChainRun(t, ctx)
	seeds, _, _, err := fresh.seedFullReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("replay the out-of-order timestamps: %v", err)
	}
	entries := seeds[addr].projection.Entries()
	if len(entries) != 2 || entries[0].Key != "later-commit" || entries[1].Key != "earlier-commit" {
		t.Fatalf("the full replay ordered %v by event time instead of commit sequence", entries)
	}
}

// TestAnchoredRebuildKeepsTheInterleavedFact pins the snapshot rule.
// - an interleaved event lands between the snapshot and the product commit
// - the rebuild replays it instead of silently losing it
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestAnchoredRebuildKeepsTheInterleavedFact(t *testing.T) {
	ctx := context.Background()
	run := windowChainRun(t, ctx)
	tier := run.tier
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	addr := event.Address{AgentID: "agent-a", SessionID: "session-interleaved"}

	store := func(key event.EventID, typ string, content string, seq int64, body []byte) {
		t.Helper()
		if err := tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: 1,
			WindowTS:    at.UnixMilli(),
			Seq:         seq,
			EventKey:    key,
			EventTime:   at.UnixMilli(),
			Body:        body,
		}.WithChain(typ, addr.SessionID)); err != nil {
			t.Fatalf("store %s: %v", key, err)
		}
	}
	encode := func(evt event.Event) []byte {
		t.Helper()
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", evt.Key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", evt.Key, err)
		}
		return encoded
	}
	mapped := func(key string, seq int64) event.Event {
		return event.Event{
			Key:       event.EventID(key),
			Type:      event.TypeExternalInput,
			Timestamp: at.Add(time.Duration(seq) * time.Minute),
			Meta:      map[string]string{event.MetaKeyEventSummary: "ask"},
			Message:   model.Message{Role: model.RoleUser, Content: "material of " + key},
			Owner:     addr,
		}
	}

	// Four facts are in the checkpoint snapshot (seq 1..4). An interleaved
	// fact lands at seq 5, and the product commits at seq 6.
	var survivors []replay.ProjectionEntry
	for seq := int64(1); seq <= 4; seq++ {
		key := event.EventID("survivor-" + string(rune('a'+seq-1)))
		evt := mapped(string(key), seq)
		store(key, evt.Type, evt.Message.Content, seq, encode(evt))
		entry, ok := mappedEntryOf(evt)
		if !ok {
			t.Fatalf("the survivor %s does not map", key)
		}
		survivors = append(survivors, entry)
	}
	interleaved := mapped("interleaved", 5)
	store(interleaved.Key, interleaved.Type, interleaved.Message.Content, 5, encode(interleaved))

	product := event.CompressProductFor("cmp-request", compressionProducer, "-hist-checkpoint", nil, "history summary").
		WithCheckpoint(anchorCheckpointOf(survivors))
	productEvent := compressProductEvent(addr.SessionID, addr.AgentID, product, at.Add(6*time.Minute))
	store(productEvent.Key, productEvent.Type, "", 6, encode(productEvent))

	seeds, report, err := run.seedAnchoredReplay(ctx, tier)
	if err != nil {
		t.Fatalf("rebuild from the anchor: %v", err)
	}
	if report.Anchors != 1 {
		t.Fatalf("the rebuild found %d anchor(s), want one", report.Anchors)
	}
	seed, carried := seeds[addr]
	if !carried {
		t.Fatalf("the rebuild registered no projection for %v", addr)
	}
	found := false
	for _, entry := range seed.projection.Entries() {
		if entry.Key == interleaved.Key {
			found = true
			if entry.Content != interleaved.Message.Content {
				t.Fatalf("the interleaved fact carries %q, want %q", entry.Content, interleaved.Message.Content)
			}
		}
	}
	if !found {
		t.Fatal("the rebuild silently lost the fact committed between the snapshot and the product commit")
	}
}

// TestMechanismFactNeverMapsToARequest pins the projection seam rule.
// - a compress.product fact carrying message content is refused by frozen type
// - a business fact carrying the same content still maps
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestMechanismFactNeverMapsToARequest(t *testing.T) {
	for _, typ := range []string{event.CompressProductType, event.CompressRequestType} {
		evt := event.Event{
			Key:     event.EventID("cmpout-" + typ),
			Type:    typ,
			Meta:    map[string]string{event.MetaKeyEventSummary: "compress product -hist-internal"},
			Message: model.Message{Role: model.RoleUser, Content: "internal wording"},
			Owner:   event.Address{AgentID: "agent-a", SessionID: "session-map"},
		}
		if _, ok := mappedEntryOf(evt); ok {
			t.Fatalf("the %s fact mapped into a request despite its non-empty message", typ)
		}
	}
	business := event.Event{
		Key:     "134000",
		Type:    event.TypeExternalInput,
		Meta:    map[string]string{event.MetaKeyEventSummary: "ask"},
		Message: model.Message{Role: model.RoleUser, Content: "internal wording"},
		Owner:   event.Address{AgentID: "agent-a", SessionID: "session-map"},
	}
	entry, ok := mappedEntryOf(business)
	if !ok || entry.Content != "internal wording" {
		t.Fatal("a business fact was refused by the projection seam")
	}
}

// faultyEngine is the port decorator these checks inject failures through: it
// wraps an opened atomic store and refuses a read, refuses a commit or drops the
// puts the check names, so the injection sits at the store seam instead of in a
// file layout and runs unchanged over any engine.
type faultyEngine struct {
	inner ledger.AtomicStore
	// failRead answers the failure of one read, or nil to let it through.
	failRead func(fetch ledger.Fetch) error
	// failCommit answers the failure of one commit, or nil to let it through.
	failCommit func(batch ledger.Batch) error
	// dropPut reports whether one put is dropped from the batch before the
	// wrapped engine sees it.
	dropPut func(key string) bool
}

// Read passes the read through the injected fault to the wrapped engine.
func (s *faultyEngine) Read(ctx context.Context, fetch ledger.Fetch) ([]ledger.Pair, error) {
	if s.failRead != nil {
		if err := s.failRead(fetch); err != nil {
			return nil, err
		}
	}
	return s.inner.Read(ctx, fetch)
}

// Keys enumerates the wrapped engine's keys.
func (s *faultyEngine) Keys(ctx context.Context, prefix string) ([]string, error) {
	return s.inner.Keys(ctx, prefix)
}

// Namespaces enumerates the wrapped engine's namespaces.
func (s *faultyEngine) Namespaces(ctx context.Context) ([]string, error) {
	return s.inner.Namespaces(ctx)
}

// Commit passes the batch through the injected faults to the wrapped engine,
// dropping the puts the check names.
func (s *faultyEngine) Commit(ctx context.Context, batch ledger.Batch) error {
	if s.failCommit != nil {
		if err := s.failCommit(batch); err != nil {
			return err
		}
	}
	if s.dropPut == nil {
		return s.inner.Commit(ctx, batch)
	}
	kept := ledger.Batch{Deletes: batch.Deletes}
	for _, put := range batch.Puts {
		if s.dropPut(put.Key) {
			continue
		}
		kept.Puts = append(kept.Puts, put)
	}
	return s.inner.Commit(ctx, kept)
}

// engineOverRoot opens the journal engine over root and hands back the
// decorator a check injects through, beside the config that opens a tier over
// it.
func engineOverRoot(t *testing.T, root string) (*faultyEngine, ledger.StorageConfig) {
	t.Helper()
	cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}
	inner, err := ledger.OpenAtomicStore(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open the storage engine over %s: %v", root, err)
	}
	faulty := &faultyEngine{inner: inner}
	cfg.Engine = faulty
	return faulty, cfg
}

// TestBootDoesNotReadOriginalShards pins boot isolation from original bodies.
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestBootDoesNotReadOriginalShards(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	faulty, cfg := engineOverRoot(t, root)
	run := windowChainRun(t, ctx, WithStorage(cfg))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-original-lazy"}
	commitWindowTurns(t, ctx, run, addr, 1)
	key := event.EventID(timeStampKey(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), addr, 0, 0))
	if got := run.tier.Originals.Get(ctx, key); got.Outcome != ledger.RecallFound {
		t.Fatalf("the seed original was not committed: %+v", got)
	}
	faulty.failRead = func(fetch ledger.Fetch) error {
		if strings.HasPrefix(fetch.Key, "blob:") || strings.HasPrefix(fetch.Prefix, "blob:") || fetch.Key == "" && fetch.Prefix == "" {
			return fmt.Errorf("%w: injected on the original body read", ledger.ErrSnapshotCorrupt)
		}
		return nil
	}
	reopened := windowChainRun(t, ctx, WithStorage(cfg))
	if got := reopened.tier.Originals.Get(ctx, key); got.Outcome != ledger.RecallFailed || !errors.Is(got.Err, ledger.ErrSnapshotCorrupt) {
		t.Fatalf("the first original read after boot = %+v, want the named corrupt-snapshot failure", got)
	}
}

// TestBootOverJournalTier pins the journal tier at the production boot entry.
//   - A run assembled over the journal tier commits its session chain, its original bodies and its durable task facts
//   - a second assembly over the same root reads the chain back
//   - The projection the reopened run serves is the projection a full walk of the same chain rebuilds, entry for entry
//   - nothing the fold summarised is dropped by the replay the boot runs
//
// 契约: docs/wiki/memory/memory-architecture.md#store-instance-sharing
func TestBootOverJournalTier(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := func() Option {
		return WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-journal-tier"}
	first := windowChainRun(t, ctx, storage())
	commitWindowTurns(t, ctx, first, addr, 1)
	key := event.EventID(timeStampKey(time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC), addr, 0, 0))
	if got := first.tier.Originals.Get(ctx, key); got.Outcome != ledger.RecallFound {
		t.Fatalf("the journal tier did not commit the seed original: %+v", got)
	}
	committed := len(first.SessionFacts(addr))
	if committed == 0 {
		t.Fatal("the journal tier committed no session fact")
	}

	restarted := windowChainRun(t, ctx, storage())
	if got := restarted.tier.Originals.Get(ctx, key); got.Outcome != ledger.RecallFound {
		t.Fatalf("the reopened journal tier answered the seed original as %+v, want the committed body", got)
	}
	if len(restarted.facts) == 0 {
		t.Fatal("the reopened journal tier restored no session fact")
	}
	served, carried := restarted.replays[addr]
	if !carried {
		t.Fatalf("the reopened run registered no sequence for %v", addr)
	}
	full := windowChainRun(t, ctx, storage())
	fullSeeds, _, _, err := full.seedFullReplay(ctx, full.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	want, carried := fullSeeds[addr]
	if !carried {
		t.Fatalf("the full walk registered no projection for %v", addr)
	}
	gotRendered := replay.RenderProjection(served.projection.Entries())
	wantRendered := replay.RenderProjection(want.projection.Entries())
	if !bytes.Equal(gotRendered, wantRendered) {
		t.Fatalf("the reopened run renders %q, want the full walk of the same chain %q", gotRendered, wantRendered)
	}
	if len(served.projection.Entries()) == 0 {
		t.Fatal("the reopened run served an empty projection beside a committed chain")
	}
}

// TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice pins the boot restore of a log no cadence has covered with a checkpoint yet.
// - positive: the notification a stopped process left owed in the durable account is seated in the mailbox of the assembled run
// - the boot read no checkpoint, so the restore is the empty cut rather than a filed one
// - negative: skipping the framework restore of the empty cut leaves the owed obligation unseated
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	owedAddr := event.Address{AgentID: "agent-a", SessionID: "session-young-owed"}
	owedTask := event.TaskID("task-young-owed")
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("open the storage tier: %v", err)
	}
	account, err := state.NewDurableCommit(ledger.TierFactLog(tier), nil)
	if err != nil {
		t.Fatalf("open the durable account: %v", err)
	}
	if _, err := account.Commit(ctx, task.CommitRecord{
		MutationID: "young-owed:0",
		TaskID:     owedTask,
		Status:     task.StatusRunning,
		Outbox: []task.Envelope{{
			Notice: task.Notice{
				TaskID: owedTask,
				Round:  1,
				Kind:   task.NoticeProgress,
				Seq:    1,
				Event:  "fact-young-owed",
				Text:   "the worker reports progress the stopped process never delivered",
			},
			Target:         owedAddr,
			Source:         owedAddr,
			TargetInstance: "instance-young",
		}},
	}); err != nil {
		t.Fatalf("commit the owed notification: %v", err)
	}

	reopened := windowChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageJournal, Root: root}))
	if key, _ := reopened.CheckpointFiled(); key != "" {
		t.Fatalf("the young log carries the checkpoint %s, want a boot whose tier states no checkpoint", key)
	}
	seats := reopened.store.Unconsumed()
	if len(seats) != 1 || seats[0].Notice.TaskID != owedTask {
		t.Fatalf("the booted mailbox seats %+v, want the owed notification the account still owes: skipping the framework restore of the empty cut leaves it unseated", seats)
	}
	if got := seats[0].Address; got != owedAddr {
		t.Fatalf("the restored seat routes to %v, want the frozen routing %v", got, owedAddr)
	}
}

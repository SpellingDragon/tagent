package assembly

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"context"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
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
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}, {ID: "agent-b", Model: "model-a", ReplayWindow: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, options...)
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run
}

// endRun ends the storage view one fixture booted, so the restart it compares
// against opens the root beside no earlier writer: the tier gives the root's write
// rights up in its close, and a view left open refuses every later writer.
//   - The closing checkpoint a clean Run.Close owes is not filed, because the fixture compares two processes over one root and a cut this process alone would state is state neither of them committed
//   - The checkpoint filer stops first, so the view is not closed under a cadence that is still writing
func endRun(t *testing.T, run *Run) {
	t.Helper()
	run.stopCheckpointFiler()
	if err := run.tier.Close(); err != nil {
		t.Fatalf("end the storage view the fixture booted: %v", err)
	}
}

// commitWindowTurns commits closed turns of four legs until the chain holds
// turns count, letting the fold fire between commits as it does online.
func commitWindowTurns(t *testing.T, ctx context.Context, run *Run, addr event.Address, turns int) {
	t.Helper()
	commitWindowTurnsFrom(t, ctx, run, addr, 0, turns)
}

// commitWindowTurnsFrom commits turns count closed turns of four legs from the
// turn the caller names, so a fixture commits material past a cut without
// re-committing the identities the chain already holds: a turn is one key per leg,
// and the same turn committed twice is a repeat the chain files no position of.
func commitWindowTurnsFrom(t *testing.T, ctx context.Context, run *Run, addr event.Address, from, turns int) {
	t.Helper()
	base := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	for turn := from; turn < from+turns; turn++ {
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
// - beside no checkpoint the anchor bounds the projection read alone, so the bodies the seam decodes equal the full walk
// - only a cut vouches covered material, and the saving rides a checkpoint a real boot restores
// - the saving anchors: TestDefaultBootRebuildsFromTheAnchor, TestSymptomAnchorSessionBootCostFlatAcrossGenerations
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
	if anchoredReport.Decoded != fullReport.Decoded {
		t.Fatalf("the anchored rebuild decoded %d bodies beside the %d the full walk holds, want the same read: no checkpoint vouches the anchor's range, so the seam skips nothing", anchoredReport.Decoded, fullReport.Decoded)
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
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 100000}},
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
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", ReplayWindow: 60}},
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

// TestEachSessionFactUsesIndexCommitOrderForLegacyArchivedEvents pins the durable session read against old index values that carry no commit rank.
//   - interleaved partitions commit in an order partition/window/sequence cannot express
//   - the archived bodies remain readable through the identity locator
//   - reversing EachIndexNewestFirst gives the exact stream order
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestEachSessionFactUsesIndexCommitOrderForLegacyArchivedEvents(t *testing.T) {
	ctx := context.Background()
	root, archiveRoot := t.TempDir(), t.TempDir()
	cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true}
	cfg.ArchiveRoot = archiveRoot
	cfg.HotLogMaxBytes = 1
	writer, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("open the archive-backed writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })
	addr := event.Address{AgentID: "agent-a", SessionID: "session-legacy-archive-order"}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	type position struct {
		key       event.EventID
		partition int
		seq       int64
		content   string
	}
	positions := []position{
		{key: "legacy-high-first", partition: 20, seq: 1, content: "first"},
		{key: "legacy-low-first", partition: 10, seq: 1, content: "second"},
		{key: "legacy-high-second", partition: 20, seq: 2, content: "third"},
		{key: "legacy-low-second", partition: 10, seq: 2, content: "fourth"},
	}
	contents := make(map[event.EventID]string, len(positions))
	for _, item := range positions {
		evt := event.Event{
			Key:       item.key,
			Type:      event.TypeExternalInput,
			Timestamp: at,
			Message:   model.Message{Role: model.RoleUser, Content: item.content},
			Owner:     addr,
		}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", item.key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", item.key, err)
		}
		if err := writer.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: item.partition,
			WindowTS:    at.UnixMilli(),
			Seq:         item.seq,
			EventKey:    item.key,
			EventTime:   at.UnixMilli(),
			Body:        encoded,
		}.WithChain(evt.Type, addr.SessionID)); err != nil {
			t.Fatalf("store %s: %v", item.key, err)
		}
		contents[item.key] = item.content
	}
	for _, cut := range []position{{partition: 20, seq: 2}, {partition: 10, seq: 2}} {
		if err := writer.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{
			Key:   fmt.Sprintf("checkpoint:%d:%d", cut.partition, cut.seq),
			Value: []byte("cut"),
		}}}); err != nil {
			t.Fatalf("commit the archive cut: %v", err)
		}
	}
	archived, err := os.ReadDir(filepath.Join(archiveRoot, "segments"))
	if err != nil || len(archived) == 0 {
		t.Fatalf("read the archived segments: count=%d err=%v", len(archived), err)
	}
	for _, item := range positions {
		if err := writer.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{
			Key:   fmt.Sprintf("%d:idx:%s", item.partition, item.key),
			Value: []byte(fmt.Sprintf("%d:%d", at.UnixMilli(), item.seq)),
		}}}); err != nil {
			t.Fatalf("write the legacy index for %s: %v", item.key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the archive writer: %v", err)
	}
	cfg.WriteRights = false
	reader, err := ledger.OpenTier(ctx, cfg)
	if err != nil {
		t.Fatalf("reopen the archive reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	var newest []event.EventID
	if err := reader.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, _ int, _ int64, _ string, _ event.SessionID) error {
		newest = append(newest, key)
		return nil
	}); err != nil {
		t.Fatalf("walk the true commit order: %v", err)
	}
	want := make([]event.EventID, len(newest))
	for i := range newest {
		want[i] = newest[len(newest)-1-i]
	}
	if len(want) != len(positions) {
		t.Fatalf("the index names %d events, want %d", len(want), len(positions))
	}
	for i, item := range positions {
		if want[i] != item.key {
			t.Fatalf("the fixture commit order at %d = %s, want %s", i, want[i], item.key)
		}
	}

	run := &Run{tier: reader}
	var got []event.EventID
	if err := run.EachSessionFact(ctx, addr, func(evt event.Event) error {
		got = append(got, evt.Key)
		if evt.Message.Content != contents[evt.Key] {
			return fmt.Errorf("event %s carries body %q, want %q", evt.Key, evt.Message.Content, contents[evt.Key])
		}
		return nil
	}); err != nil {
		t.Fatalf("read the legacy archived chain: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("the session read returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the session read returned %v, want index commit order %v", got, want)
		}
	}
}

// TestEachSessionFactReadsAPartitionTheSeatUnderstates pins the replay against a seat that lost a partition.
//   - the seat states partition 10 alone beside committed entries the index files under partition 20, so the seat-wide identity read answers absence
//   - the full walk reads each body at the partition its index entry named, and answers the true commit order rather than a missing body
//   - every body it answers is the one its own commit landed
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestEachSessionFactReadsAPartitionTheSeatUnderstates(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writer, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the journal writer: %v", err)
	}
	t.Cleanup(func() { writer.Close() })
	addr := event.Address{AgentID: "agent-a", SessionID: "session-seat-understates"}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	type position struct {
		key       event.EventID
		partition int
		seq       int64
		content   string
	}
	positions := []position{
		{key: "seat-high-first", partition: 20, seq: 1, content: "first"},
		{key: "seat-low-first", partition: 10, seq: 1, content: "second"},
		{key: "seat-high-second", partition: 20, seq: 2, content: "third"},
		{key: "seat-low-second", partition: 10, seq: 2, content: "fourth"},
	}
	contents := make(map[event.EventID]string, len(positions))
	for _, item := range positions {
		evt := event.Event{
			Key:       item.key,
			Type:      event.TypeExternalInput,
			Timestamp: at,
			Message:   model.Message{Role: model.RoleUser, Content: item.content},
			Owner:     addr,
		}
		body, err := memory.BodyFromEvent(evt, "test", "test")
		if err != nil {
			t.Fatalf("encode %s: %v", item.key, err)
		}
		encoded, err := body.Encode()
		if err != nil {
			t.Fatalf("marshal %s: %v", item.key, err)
		}
		if err := writer.Segments.StoreEvent(ctx, ledger.SegmentRecord{
			PartitionID: item.partition,
			WindowTS:    at.UnixMilli(),
			Seq:         item.seq,
			EventKey:    item.key,
			EventTime:   at.UnixMilli(),
			Body:        encoded,
		}.WithChain(evt.Type, addr.SessionID)); err != nil {
			t.Fatalf("store %s: %v", item.key, err)
		}
		contents[item.key] = item.content
	}
	if err := writer.Store.Commit(ctx, ledger.Batch{Puts: []ledger.Pair{{
		Key:   "store:seat",
		Value: []byte(fmt.Sprintf(`{"count":%d,"commitSeq":%d,"partitions":[10]}`, len(positions), len(positions))),
	}}}); err != nil {
		t.Fatalf("state the seat without partition 20: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close the journal writer: %v", err)
	}
	reader, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root})
	if err != nil {
		t.Fatalf("reopen the journal reader: %v", err)
	}
	t.Cleanup(func() { reader.Close() })
	if _, held, err := reader.Segments.GetEventInPartition(ctx, "seat-high-first", 20); err != nil || !held {
		t.Fatalf("the entry of partition 20 = held=%t err=%v, want the index entry and body its commit landed", held, err)
	}
	if _, held, err := reader.Segments.GetEvent(ctx, "seat-high-first"); err != nil || held {
		t.Fatalf("the seat-wide read of partition 20 = held=%t err=%v, want absence beside the partition the seat dropped", held, err)
	}

	run := &Run{tier: reader}
	var got []event.EventID
	if err := run.EachSessionFact(ctx, addr, func(evt event.Event) error {
		got = append(got, evt.Key)
		if evt.Message.Content != contents[evt.Key] {
			return fmt.Errorf("event %s carries body %q, want %q", evt.Key, evt.Message.Content, contents[evt.Key])
		}
		return nil
	}); err != nil {
		t.Fatalf("read the chain of the understated seat: %v", err)
	}
	want := make([]event.EventID, len(positions))
	for i, item := range positions {
		want[i] = item.key
	}
	if len(got) != len(want) {
		t.Fatalf("the session read returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the session read returned %v, want the commit order %v", got, want)
		}
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
	cfg := ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true}
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
	endRun(t, first)

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
	endRun(t, restarted)
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
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageJournal, Root: root, WriteRights: true})
	if err != nil {
		t.Fatalf("open the storage tier: %v", err)
	}
	account := state.NewEmptyDurableCommit(ledger.TierFactLog(tier), nil)
	if err := account.Seat(ctx, nil, nil); err != nil {
		t.Fatalf("seat the durable account of the log: %v", err)
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
	if err := tier.Close(); err != nil {
		t.Fatalf("close the seeding tier: %v", err)
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

// TestReplayRefNamesAMissingBody pins the answer of the anchored rebuild's per-slot walk when the store it walks cannot answer one body.
//   - a fact the walk names but the store answers no body for is refused with ErrSegmentBodyMissing carrying that key
//   - it is never returned as an empty product beside a nil error
//   - the refusal is the same named error the sibling recovery reads answer (eachCommittedEvent, eachFactPastCut, EachEvent)
//   - so the two paths of one store cannot disagree about one missing body
//   - the branch the walk takes is the one a derived index publishes fewer entries than the commit order names
//   - today the load publishes every entry, so the walk reaches it only through a store that answers no body for the named fact, which is what this test drives
//
// 契约: docs/wiki/memory/memory-architecture.md#read-paths
func TestReplayRefNamesAMissingBody(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	run := noTrimChainRun(t, ctx, WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	addr := event.Address{AgentID: "agent-a", SessionID: "session-replay-ref-missing"}
	partition := replayRefFixturePartition(t, ctx, run, addr)
	missing := event.EventID("session-replay-ref-missing-fact")
	report := seedReport{}
	seed := &sessionSeed{}
	product, stated, agent, err := run.replayRef(ctx, run.tier, missing, partition, seed, &report)
	if err == nil {
		t.Fatalf("the walk answered the missing fact %s as (%+v, %t, %s, nil), want the missing body refused by name",
			missing, product, stated, agent)
	}
	if !errors.Is(err, ledger.ErrSegmentBodyMissing) {
		t.Fatalf("the walk refused the missing fact with %v, want ErrSegmentBodyMissing", err)
	}
	if !strings.Contains(err.Error(), string(missing)) {
		t.Fatalf("the refusal = %v, want the key %s named", err, missing)
	}
	if report.Decoded != 0 {
		t.Fatalf("the walk decoded %d fact(s) of a body it never read, want none counted", report.Decoded)
	}
}

// pipelineRuler is the Governor these checks contribute through WithPlugins: one
// declared call->ruling face that records every pass it was asked and answers the
// decision its check scripted.
type pipelineRuler struct {
	name     string
	mu       sync.Mutex
	asked    []plugin.CallPass
	decision func(plugin.CallPass) plugin.Decision
}

// Name returns the stable name of this instance.
func (g *pipelineRuler) Name() string { return g.name }

// Port reports the port this instance serves.
func (g *pipelineRuler) Port() plugin.Port { return plugin.PortCallRuling }

// Judge records the pass and answers the scripted ruling over it.
func (g *pipelineRuler) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, pass)
	if g.decision != nil {
		return g.decision(pass), nil
	}
	return plugin.Decision{Disposition: plugin.DispositionAllow}, nil
}

// ruled reports the passes the face was asked to rule, in the order it was asked.
func (g *pipelineRuler) ruled() []plugin.CallPass {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]plugin.CallPass(nil), g.asked...)
}

// pipelineRuledTool is the native tool the declared ruler rules: it counts its
// executions, so a check tells a call that ran from one the ruling refused.
type pipelineRuledTool struct {
	name  string
	mu    sync.Mutex
	calls int
}

// Declaration returns the declaration naming the ruled tool.
func (t *pipelineRuledTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{Name: t.name}
}

// Call executes the ruled tool once and reports its output.
func (t *pipelineRuledTool) Call(context.Context, []byte) (any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	return "ruled-output", nil
}

// executions reports how many times the tool ran.
func (t *pipelineRuledTool) executions() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

// pipelineRuledRun assembles one run over a scripted provider whose first
// decision calls the ruled tool and whose second settles the turn, with the
// options the check attaches.
func pipelineRuledRun(t *testing.T, ctx context.Context, opts ...Option) (*Run, *pipelineRuledTool, *snapshotTestModel) {
	t.Helper()
	tool := &pipelineRuledTool{name: "pipeline-ruled"}
	round := 0
	var mu sync.Mutex
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		mu.Lock()
		defer mu.Unlock()
		round++
		if round == 1 {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{
					Role: model.RoleAssistant,
					ToolCalls: []model.ToolCall{{
						ID:   "call-pipeline",
						Type: "function",
						Function: model.FunctionDefinitionParam{
							Name:      tool.name,
							Arguments: []byte(`{}`),
						},
					}},
				}}},
				Done: true,
			}
		}
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
			Done:    true,
		}
	}}
	options := append([]Option{WithGovernance(snapshotTestGovernance{})}, opts...)
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{tool}},
	}, options...)
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run, tool, provider
}

// pipelineRuledInvoke drives the one invocation that calls the ruled tool through
// the formal entry of the assembled run and answers the turn it served.
func pipelineRuledInvoke(t *testing.T, ctx context.Context, run *Run, invocation string) {
	t.Helper()
	turn, err := run.Invoke(ctx, InvokeRequest{
		InvocationID:  event.InvocationID(invocation),
		Address:       event.Address{AgentID: "agent-a", SessionID: event.SessionID("session-" + invocation)},
		TriggerSource: "user",
		Input:         []model.Message{{Role: model.RoleUser, Content: "drive the ruled call"}},
	})
	if err != nil {
		t.Fatalf("invoke the assembled run: %v", err)
	}
	if turn.Terminal != model.KindCompleted {
		t.Fatalf("terminal of the turn = %q, want the completed answer", turn.Terminal)
	}
}

// TestGovernorDeclaredInThePipelineRulesTheCall pins the ruling face a declared pipeline carries.
//   - the Governor a WithPlugins contribution publishes is asked to rule the real call, once, over the final call of the Before pass
//   - the call it allows executes once, and the policy WithPolicy attached is never asked beside the declaration
//   - the same Governor refusing refuses the call before execution: the tool runs zero times, the model reads the reason and rule
//   - consumer: the public Assemble entry, the Run.Invoke turn it serves, the admission wrapper every registered tool goes through
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestGovernorDeclaredInThePipelineRulesTheCall(t *testing.T) {
	t.Run("the declared governor rules the call and the assembly policy is not consulted", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ruler := &pipelineRuler{name: "pipeline-ruler"}
		poison := &pipelineRuler{name: "assembly-policy", decision: func(plugin.CallPass) plugin.Decision {
			return plugin.Decision{Disposition: plugin.DispositionAllow, Reason: "the assembly policy ruled a call beside a declared governor"}
		}}
		run, tool, provider := pipelineRuledRun(t, ctx,
			WithPlugins(func(*Run) ([]plugin.Instance, error) {
				return []plugin.Instance{ruler}, nil
			}),
			WithPolicy(poison),
		)
		pipelineRuledInvoke(t, ctx, run, "inv-governor-declared-allow")

		asked := ruler.ruled()
		if len(asked) != 1 {
			t.Fatalf("the declared governor was asked %d time(s), want the one ruling of the call", len(asked))
		}
		if got := asked[0].Final.Function.Name; got != tool.name {
			t.Fatalf("the declared governor ruled the call of %q, want the final call of %q", got, tool.name)
		}
		if got := tool.executions(); got != 1 {
			t.Fatalf("the allowed call executed %d time(s), want the one execution of the ruling that allowed it", got)
		}
		if got := len(poison.ruled()); got != 0 {
			t.Fatalf("the assembly policy was asked %d time(s), want none beside the governor the pipeline declared", got)
		}
		if len(provider.requests) < 2 {
			t.Fatalf("the turn was driven %d time(s), want the decision that called the tool beside the one that settled it", len(provider.requests))
		}
		receipt := provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content
		if !strings.Contains(receipt, "ruled-output") {
			t.Fatalf("the receipt the model read = %q, want the output of the call the declared ruling allowed", receipt)
		}
	})

	t.Run("the same governor refuses the call before any execution", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ruler := &pipelineRuler{name: "pipeline-ruler", decision: func(pass plugin.CallPass) plugin.Decision {
			return plugin.Decision{
				Disposition: plugin.DispositionAllow,
				Rule:        "declared-no-call",
				Reason:      "the declared ruler refuses this call",
				Denial:      &plugin.Denial{CallID: pass.Final.ID, Tool: pass.Final.Function.Name},
			}
		}}
		run, tool, provider := pipelineRuledRun(t, ctx,
			WithPlugins(func(*Run) ([]plugin.Instance, error) {
				return []plugin.Instance{ruler}, nil
			}),
		)
		pipelineRuledInvoke(t, ctx, run, "inv-governor-declared-refuse")

		if got := len(ruler.ruled()); got != 1 {
			t.Fatalf("the declared governor was asked %d time(s), want the one ruling of the refused call", got)
		}
		if got := tool.executions(); got != 0 {
			t.Fatalf("the refused call executed %d time(s), want no execution of a call the declared governor refused", got)
		}
		if len(provider.requests) < 2 {
			t.Fatalf("the turn was driven %d time(s), want the refusal read back to the model", len(provider.requests))
		}
		receipt := provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content
		if !strings.Contains(receipt, "the declared ruler refuses this call") || !strings.Contains(receipt, "declared-no-call") {
			t.Fatalf("the receipt the model read = %q, want the reason and the rule of the declared refusal", receipt)
		}
	})
}

// observationPortRuler is a face that declares the fact->observation port while
// implementing Judge as well: the declaration is the routing, so the call->ruling
// face it never declared is never asked. It answers the observation duty of the
// port it declares and refuses every call it is asked to rule, so a pipeline
// reading the method instead of the port rules the call through a face declared
// elsewhere.
type observationPortRuler struct {
	name  string
	mu    sync.Mutex
	asked []plugin.CallPass
}

// Name returns the stable name of this instance.
func (g *observationPortRuler) Name() string { return g.name }

// Port reports the port this instance serves: the observation port, never the
// ruling one.
func (g *observationPortRuler) Port() plugin.Port { return plugin.PortFactObservation }

// ObserveCommit answers the observation duty of the port this face declares.
func (g *observationPortRuler) ObserveCommit(context.Context, event.EventID) error {
	return nil
}

// Judge records the pass and refuses the call: a pipeline asking this face to
// rule is one that read the method instead of the declared port.
func (g *observationPortRuler) Judge(pass plugin.CallPass) (plugin.Decision, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, pass)
	return plugin.Decision{
		Disposition: plugin.DispositionAllow,
		Rule:        "declared-on-another-port",
		Reason:      "a face declaring another port ruled this call",
		Denial:      &plugin.Denial{CallID: pass.Final.ID, Tool: pass.Final.Function.Name},
	}, nil
}

// ruled reports the passes this face was asked to rule, in the order it was asked.
func (g *observationPortRuler) ruled() []plugin.CallPass {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]plugin.CallPass(nil), g.asked...)
}

// TestGovernorDeclaredOnAnotherPortDoesNotRule pins the declaration a ruling face is read by.
//   - negative: a WithPlugins instance implementing Judge while declaring the fact->observation port is published in the pipeline and asked to rule zero calls
//   - positive: the policy WithPolicy attached rules that same call instead, once, and the call executes once
//   - the instance is published in the agent's pipeline, so the zero rulings are the port's and not a contribution that never landed
//   - consumer: the public Assemble entry, the Run.Invoke turn it serves, the admission wrapper every registered tool goes through
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestGovernorDeclaredOnAnotherPortDoesNotRule(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	misdeclared := &observationPortRuler{name: "observation-port-ruler"}
	policy := &pipelineRuler{name: "assembly-policy"}
	run, tool, provider := pipelineRuledRun(t, ctx,
		WithPlugins(func(*Run) ([]plugin.Instance, error) {
			return []plugin.Instance{misdeclared}, nil
		}),
		WithPolicy(policy),
	)
	pipelineRuledInvoke(t, ctx, run, "inv-governor-other-port")

	run.mu.Lock()
	published := run.current.Agents[event.AgentID("agent-a")].Plugins
	run.mu.Unlock()
	mounted := false
	for _, instance := range published {
		if instance.Name() == misdeclared.name {
			mounted = true
		}
	}
	if !mounted {
		t.Fatalf("the pipeline publishes the instances %v, want the contributed %s among them: the check must rule out a contribution that never landed", namesOfInstances(published), misdeclared.name)
	}
	if got := len(misdeclared.ruled()); got != 0 {
		t.Fatalf("the face declaring fact->observation was asked to rule %d call(s), want none beside the port it declares", got)
	}
	if got := len(policy.ruled()); got != 1 {
		t.Fatalf("the policy WithPolicy attached was asked %d time(s), want the one ruling of a pipeline declaring no governor", got)
	}
	if got := tool.executions(); got != 1 {
		t.Fatalf("the call executed %d time(s), want the one execution of the call the attached policy allowed", got)
	}
	if len(provider.requests) < 2 {
		t.Fatalf("the turn was driven %d time(s), want the decision that called the tool beside the one that settled it", len(provider.requests))
	}
	receipt := provider.requests[1].Messages[len(provider.requests[1].Messages)-1].Content
	if !strings.Contains(receipt, "ruled-output") {
		t.Fatalf("the receipt the model read = %q, want the output of the call the attached policy allowed", receipt)
	}
}

// namesOfInstances reports the names of one published pipeline, in the published
// order, so a refusal can state what it saw.
func namesOfInstances(instances []plugin.Instance) []string {
	names := make([]string, 0, len(instances))
	for _, instance := range instances {
		names = append(names, instance.Name())
	}
	return names
}

// TestMemoryDeclarationRefusesHalfAnArchiveConfig pins the four incomplete archive declarations the load refuses by name.
// - a byte budget beside no archive_root, and an archive_root beside no byte budget, are each refused
// - an archive_root beside no storage tier of its own is refused
// - a retention window beside no archive_root is refused
// - every refusal is the memory declaration one and names agents.user.memory
// - a complete archive declaration beside a declared tier is accepted, and the tier carries all three fields
// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
func TestMemoryDeclarationRefusesHalfAnArchiveConfig(t *testing.T) {
	tier := func(mutate func(*config.MemorySpec)) *config.Declaration {
		spec := config.MemorySpec{Type: "localfile", Path: "/srv/hot"}
		mutate(&spec)
		return &config.Declaration{Agents: []config.AgentSpec{{ID: "user", Memory: spec}}}
	}
	archiveOnly := config.MemorySpec{}
	archiveOnly.ArchiveRoot = "/srv/archive"
	archiveOnly.HotLogMaxBytes = 1 << 20
	cases := []struct {
		name    string
		decl    *config.Declaration
		refusal string
	}{
		{
			name:    "byte budget beside no archive root",
			decl:    tier(func(s *config.MemorySpec) { s.HotLogMaxBytes = 1 << 20 }),
			refusal: "hot_log_max_bytes requires archive_root",
		},
		{
			name:    "archive root beside no byte budget",
			decl:    tier(func(s *config.MemorySpec) { s.ArchiveRoot = "/srv/archive" }),
			refusal: "archive_root requires hot_log_max_bytes above zero",
		},
		{
			name:    "archive root beside no storage tier",
			decl:    &config.Declaration{Agents: []config.AgentSpec{{ID: "user", Memory: archiveOnly}}},
			refusal: "archive_root requires the storage tier's own type and path",
		},
		{
			name:    "retention window beside no archive root",
			decl:    tier(func(s *config.MemorySpec) { s.ArchiveMaxAge = 24 * time.Hour }),
			refusal: "archive_max_age requires archive_root",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := MemoryPlanOf(tc.decl, "", "")
			if !errors.Is(err, ErrMemoryDeclaration) {
				t.Fatalf("MemoryPlanOf over %s = %v, want the memory declaration refusal", tc.name, err)
			}
			if plan != nil {
				t.Fatalf("MemoryPlanOf over %s returned a plan beside the refusal: %+v", tc.name, plan)
			}
			if !strings.Contains(err.Error(), "agents.user.memory") {
				t.Fatalf("refusal %q does not name the field path agents.user.memory", err)
			}
			if !strings.Contains(err.Error(), tc.refusal) {
				t.Fatalf("refusal %q does not name %q", err, tc.refusal)
			}
		})
	}

	complete := tier(func(s *config.MemorySpec) {
		s.ArchiveRoot = "/srv/archive"
		s.HotLogMaxBytes = 1 << 20
		s.ArchiveMaxAge = 24 * time.Hour
	})
	plan, err := MemoryPlanOf(complete, "", "")
	if err != nil {
		t.Fatalf("MemoryPlanOf over a complete archive declaration = %v, want the tier built", err)
	}
	built := plan.Storage()
	if built == nil {
		t.Fatalf("the plan carries no storage tier over a declared archive face")
	}
	if built.Type != "localfile" || built.Root != "/srv/hot" {
		t.Fatalf("tier = %+v, want the declared type and root", *built)
	}
	if built.ArchiveRoot != "/srv/archive" || built.HotLogMaxBytes != 1<<20 || built.ArchiveMaxAge != 24*time.Hour {
		t.Fatalf("tier archive face = %+v, want the three declared fields carried through the bridge", *built)
	}
}

// TestArchiveConfigOnlyUnderAgentMemory pins where the three archive fields may be declared.
// - each of the three keys is refused as an unknown field under the top-level storage section
// - a key outside the three is refused under the memory section too, so inlining keeps that section a whitelist
// - the agent memory section reads all three, and one plan carries them beside a top-level tier naming the same type and root
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestArchiveConfigOnlyUnderAgentMemory(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		name string
		line string
	}{
		{name: "archive root", line: "  archive_root: /srv/archive\n"},
		{name: "hot log budget", line: "  hot_log_max_bytes: 1048576\n"},
		{name: "retention window", line: "  archive_max_age: 24h\n"},
	} {
		t.Run("top level "+tc.name, func(t *testing.T) {
			path := filepath.Join(root, strings.ReplaceAll(tc.name, " ", "-")+".yaml")
			document := "entry: user\nagents:\n  user: {}\nstorage:\n  type: localfile\n  root: /srv/hot\n" + tc.line
			if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
				t.Fatalf("write the host document: %v", err)
			}
			_, err := loadHostDocument(path)
			if !errors.Is(err, config.ErrUnknownConfigKey) {
				t.Fatalf("loadHostDocument over %s at the top level = %v, want the unknown key refusal", tc.name, err)
			}
			if !strings.Contains(err.Error(), "storage.") {
				t.Fatalf("refusal %q does not name the storage section the key landed in", err)
			}
		})
	}

	typo := filepath.Join(root, "agent-memory-typo.yaml")
	const misspelled = `entry: user
agents:
  user:
    memory:
      type: localfile
      path: /srv/hot
      archive_roots: /srv/archive
`
	if err := os.WriteFile(typo, []byte(misspelled), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	if _, err := loadHostDocument(typo); !errors.Is(err, config.ErrUnknownConfigKey) ||
		!strings.Contains(err.Error(), "agents.user.memory.archive_roots") {
		t.Fatalf("loadHostDocument over a key outside the inlined archive face = %v, want the unknown key refusal at agents.user.memory.archive_roots", err)
	}

	path := filepath.Join(root, "agent-memory.yaml")
	const declared = `entry: user
agents:
  user:
    memory:
      type: localfile
      path: /srv/hot
      archive_root: /srv/archive
      hot_log_max_bytes: 1048576
      archive_max_age: 24h
storage:
  type: localfile
  root: /srv/hot
`
	if err := os.WriteFile(path, []byte(declared), 0o644); err != nil {
		t.Fatalf("write the host document: %v", err)
	}
	doc, err := loadHostDocument(path)
	if err != nil {
		t.Fatalf("loadHostDocument over the archive face under agents.user.memory = %v, want it read", err)
	}
	decl, err := doc.Declaration()
	if err != nil {
		t.Fatalf("Declaration over the loaded document = %v", err)
	}
	entry := decl.Agents[0].Memory
	if entry.ArchiveRoot != "/srv/archive" || entry.HotLogMaxBytes != 1048576 || entry.ArchiveMaxAge != 24*time.Hour {
		t.Fatalf("memory = %+v, want the three fields read from agents.user.memory", entry)
	}
	plan, err := MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil {
		t.Fatalf("MemoryPlanOf beside a top-level tier naming the same type and root = %v, want the plan built", err)
	}
	built := plan.Storage()
	if built == nil {
		t.Fatalf("the plan carries no storage tier over the declared agent archive face")
	}
	if built.ArchiveRoot != "/srv/archive" || built.HotLogMaxBytes != 1048576 || built.ArchiveMaxAge != 24*time.Hour {
		t.Fatalf("tier archive face = %+v, want the agent declaration kept beside the top-level tier", *built)
	}
}

// TestMovedArchiveFieldIsNotTheSameTier pins that the archive face is part of the restart judgement.
// - a generation naming the same type and root but another archive field is not the tier this process opened
// - an unchanged archive face is the same tier
// 契约: docs/wiki/platform/org-hot-reload.md#restart-required-dimensions
func TestMovedArchiveFieldIsNotTheSameTier(t *testing.T) {
	storage := ledger.StorageConfig{Type: "localfile", Root: "/srv/hot"}
	storage.ArchiveRoot = "/srv/archive"
	storage.HotLogMaxBytes = 1 << 20
	storage.ArchiveMaxAge = 24 * time.Hour
	opened := &MemoryPlan{hasStore: true, storage: storage}
	if !sameMemoryTier(opened, opened, false) {
		t.Fatalf("sameMemoryTier over one unchanged plan = false, want the tier this process opened")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ledger.StorageConfig)
	}{
		{name: "archive root", mutate: func(c *ledger.StorageConfig) { c.ArchiveRoot = "/srv/other" }},
		{name: "hot log budget", mutate: func(c *ledger.StorageConfig) { c.HotLogMaxBytes = 1 << 21 }},
		{name: "retention window", mutate: func(c *ledger.StorageConfig) { c.ArchiveMaxAge = 48 * time.Hour }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moved := &MemoryPlan{hasStore: true, storage: opened.storage}
			tc.mutate(&moved.storage)
			if sameMemoryTier(opened, moved, false) {
				t.Fatalf("sameMemoryTier over a moved %s = true, want the restart-required verdict", tc.name)
			}
		})
	}
}

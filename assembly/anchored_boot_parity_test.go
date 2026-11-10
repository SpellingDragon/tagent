// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// noWindowBootRun assembles one run whose agent declares no model window, so the
// resident bound of a rebuilt chain is the no-window bound.
func noWindowBootRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}}}, append([]Option{WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory})}, opts...)...)
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run
}

// noTrimNoWindowRun is noWindowBootRun beside the seed trim off, so the live
// ledger holds exactly the facts the boot decoded and the decode depth of a boot
// is readable.
func noTrimNoWindowRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	options := append([]Option{
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
	}, opts...)
	run, err := assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}}, false, options...)
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run
}

// TestAnchoredSurvivorsFollowTheResidentBound pins the survivor trim.
// - a checkpoint naming more survivors than the resident bound rebuilds the bound, not the payload
// - the rebuilt projection holds the newest survivors in payload order, with their text read back from the chain (negative: the oldest are gone)
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestAnchoredSurvivorsFollowTheResidentBound(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-survivor-bound"}
	run := noWindowBootRun(t, ctx)
	total := 2*noWindowResidentFacts + 10
	survivors := make([]anchorEntry, 0, total)
	for at := 0; at < total; at++ {
		moment := time.Date(2026, 11, 6, 12, at%60, at/60, 0, time.UTC)
		survivor := anchorEntry{
			Key:     event.EventID(fmt.Sprintf("%s-survivor-%d", addr.SessionID, at)),
			Type:    event.TypeExternalInput,
			Summary: fmt.Sprintf("the survivor %d", at),
			Time:    moment.UnixMilli(),
		}
		if _, err := run.commitFactChecked(ctx, event.Event{
			Key:       survivor.Key,
			Type:      survivor.Type,
			Timestamp: moment,
			Meta:      map[string]string{event.MetaKeyEventSummary: survivor.Summary},
			Message:   model.Message{Role: model.RoleUser, Content: fmt.Sprintf("the survivor text %d", at)},
			Owner:     addr, Source: addr, Target: addr,
		}); err != nil {
			t.Fatalf("commit the survivor %d: %v", at, err)
		}
		survivors = append(survivors, survivor)
	}
	payload, err := json.Marshal(survivors)
	if err != nil {
		t.Fatalf("encode the survivor payload: %v", err)
	}
	request := event.CompressRequestFor("assembly.test", "the survivor bound", "the fixture judges more survivors than the bound")
	product := event.CompressProductFor(request.ID, compressionProducer, event.EventID("-summary-survivor-bound"), nil, "the folded summary").WithCheckpoint(payload)
	if _, err := run.commitFactChecked(ctx, compressProductEvent(addr.SessionID, addr.AgentID, product, time.Now().UTC())); err != nil {
		t.Fatalf("commit the anchor payload: %v", err)
	}

	seeds, report, err := run.seedAnchoredReplay(ctx, run.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchor: %v", err)
	}
	if report.Anchors != 1 {
		t.Fatalf("the rebuild used %d anchor(s), want the one the chain carries", report.Anchors)
	}
	seed, carried := seeds[addr]
	if !carried {
		t.Fatal("the anchored rebuild registered no projection for the chain")
	}
	entries := seed.projection.Entries()
	if len(entries) != noWindowResidentFacts {
		t.Fatalf("the rebuilt projection holds %d entries, want the resident bound %d", len(entries), noWindowResidentFacts)
	}
	newest := survivors[len(survivors)-1]
	last := entries[len(entries)-1]
	if last.Key != newest.Key || last.Content != fmt.Sprintf("the survivor text %d", total-1) {
		t.Fatalf("the rebuilt projection ends at %s reading %q, want the newest survivor %s reading the text the chain states", last.Key, last.Content, newest.Key)
	}
	for _, entry := range entries {
		if entry.Key == survivors[0].Key {
			t.Fatalf("the rebuilt projection still holds the oldest survivor %s, want the bound to keep the newest", entry.Key)
		}
	}
}

// counterlessProductOf renders one product fact for the parity fixture.
func counterlessProductOf(product event.CompressProduct) event.Event {
	return compressProductEvent("", "", product, time.Now().UTC())
}

// TestAnchoredBootMatchesFullBoot pins the faces a restart answers.
//   - the anchored rebuild lands the slots the full walk lands
//   - the two rebuilds render the same projection bytes entry for entry
//   - the two consumers answer the same judgments (negative: the sets differ)
//   - no answered judgment is executed again and no held judgment is lost
//   - the anchored rebuild notes only the products it decodes, so the judgment it records is never one a chain product already answers
//   - the fallback rebuild answers the same judgments as the full walk
//   - neither rebuild states a compaction payload or a recovery hint
//   - either carrier serves the rebuild (the newest checkpoint when the store holds one, else the newest per-chain anchor), covered material staying unread
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestAnchoredBootMatchesFullBoot(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-anchored-boot"}
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
	source := windowChainRun(t, ctx, storage)
	commitWindowTurns(t, ctx, source, addr, 12)
	if count := countFoldProducts(t, ctx, source); count == 0 {
		t.Fatal("the fixture folded nothing, so the anchored boot proves nothing")
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

	products := countFoldProducts(t, ctx, source)
	full := windowChainRun(t, ctx, storage)
	resetCompression(full)
	fullSeeds, fullLanded, fullReport, err := full.seedFullReplay(ctx, full.tier)
	if err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	anchored := windowChainRun(t, ctx, storage)
	resetCompression(anchored)
	anchoredSeeds, anchoredReport, err := anchored.seedAnchoredReplay(ctx, anchored.tier)
	if err != nil {
		t.Fatalf("rebuild from the anchors: %v", err)
	}
	fallback := windowChainRun(t, ctx, storage)
	resetCompression(fallback)
	fallbackSeeds, fallbackReport, err := fallback.seedAnchoredReplay(ctx, blankTypedTier(t, ctx, fallback.tier))
	if err != nil {
		t.Fatalf("rebuild with no typed index: %v", err)
	}
	if fallbackReport.Anchors != 0 {
		t.Fatalf("the fallback rebuild used %d anchor(s), want the full walk of every chain", fallbackReport.Anchors)
	}
	fallbackSeed, carried := fallbackSeeds[addr]
	if !carried {
		t.Fatal("the fallback rebuild registered no projection for the chain")
	}
	if anchoredReport.Anchors == 0 && anchoredReport.Checkpoints == 0 {
		t.Fatal("the folded chain minted no carrier the rebuild could use")
	}
	if anchoredReport.Decoded >= fullReport.Decoded {
		t.Fatalf("the anchored rebuild decoded %d bodies, want fewer than the full walk %d", anchoredReport.Decoded, fullReport.Decoded)
	}
	if len(anchoredReport.Landed) != len(fullLanded) {
		t.Fatalf("the anchored rebuild landed %d partitions, want the %d the full walk lands", len(anchoredReport.Landed), len(fullLanded))
	}
	for partition, seq := range fullLanded {
		if anchoredReport.Landed[partition] != seq {
			t.Fatalf("partition %d lands after seq %d in the anchored rebuild, want %d", partition, anchoredReport.Landed[partition], seq)
		}
	}
	want, carried := fullSeeds[addr]
	if !carried {
		t.Fatal("the full walk registered no projection for the chain")
	}
	got, carried := anchoredSeeds[addr]
	if !carried {
		t.Fatal("the anchored rebuild registered no projection for the chain")
	}
	gotRendered, wantRendered := replay.RenderProjection(got.projection.Entries()), replay.RenderProjection(want.projection.Entries())
	if !bytes.Equal(gotRendered, wantRendered) {
		t.Fatalf("the anchored rebuild renders %d bytes, want the full walk %d", len(gotRendered), len(wantRendered))
	}
	for name, projection := range map[string]*replay.ReplayProjection{"anchored": got.projection, "fallback": fallbackSeed.projection, "full": want.projection} {
		if projection.Compaction() != nil {
			t.Fatalf("the %s rebuild states a compaction payload, want none from a chain replay", name)
		}
		if projection.RecoveryHint() != nil {
			t.Fatalf("the %s rebuild states a recovery hint, want none from a chain replay", name)
		}
	}

	if products == 0 {
		t.Fatal("the fixture folded nothing, so the judgment parity proves nothing")
	}
	if got := answeredCount(t, full); got != products {
		t.Fatalf("the full walk answers %d judgments, want the %d products the chain holds", got, products)
	}
	answeredOnChain := productsByRequest(t, ctx, anchored)
	if got := answeredCount(t, anchored); got == 0 {
		t.Fatal("the anchored rebuild notes no answered judgment, want the products it decoded noted")
	}
	for _, request := range anchored.compress.pending(addr.SessionID) {
		if answeredOnChain[request.ID] != 0 {
			t.Fatalf("the anchored rebuild holds the judgment %s pending beside %d product(s) answering it, want an answered judgment never executed again", request.ID, answeredOnChain[request.ID])
		}
	}
	if got := answeredCount(t, fallback); got != products {
		t.Fatalf("the fallback rebuild answers %d judgments, want the %d products the chain holds", got, products)
	}
}

// resetCompression replaces the compression consumer of one run with an empty
// one over the same session face, so the accounting a rebuild makes is what the
// read answers instead of the accounting the assembly already made.
func resetCompression(run *Run) {
	run.compress = NewCompressionConsumer(run.compress.sessions)
}

// answeredCount reports how many judgments one consumer holds as answered.
func answeredCount(t *testing.T, run *Run) int {
	t.Helper()
	run.compress.mu.Lock()
	defer run.compress.mu.Unlock()
	return len(run.compress.answered)
}

// chainIndexSize counts the facts one tier indexes, the denominator the boot
// decode count is measured against.
func chainIndexSize(t *testing.T, ctx context.Context, run *Run) int {
	t.Helper()
	count := 0
	if err := run.tier.Segments.EachIndexNewestFirst(ctx, func(event.EventID, int, int64, string, event.SessionID) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("count the indexed facts: %v", err)
	}
	return count
}

// noTrimChainRun assembles one run of the folding fixture beside the seed trim
// off, so the live ledger holds exactly the facts the rebuild decoded and the
// decode depth of a boot is readable.
func noTrimChainRun(t *testing.T, ctx context.Context, opts ...Option) *Run {
	t.Helper()
	provider := &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}
	options := append([]Option{
		WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}),
	}, opts...)
	run, err := assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a", MaxTokens: 60}, {ID: "agent-b", Model: "model-a", MaxTokens: 60}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: provider}, "agent-b": {Model: provider}}, false, options...)
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run
}

// TestDefaultBootRebuildsFromTheAnchor pins the default anchor boot.
//   - a folded chain boots from its anchor, so the decoded facts stay below the chain
//   - the projection the default boot serves is the projection the full walk rebuilds
//   - the same measure answers every indexed fact on a full walk (negative: the reduction is the anchor's, not a dropped chain)
//   - a chain that never folded still boots every fact of it
//   - an unfolded chain reads back the checkpoint the source filed, so the boot decodes that and the facts past its cut, never more
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestDefaultBootRebuildsFromTheAnchor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-default-anchor"}
	source := noTrimChainRun(t, ctx, storage)
	source.stopCheckpointFiler()
	commitWindowTurns(t, ctx, source, addr, 12)
	if count := countFoldProducts(t, ctx, source); count == 0 {
		t.Fatal("the fixture folded nothing, so the boot proves nothing")
	}
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}
	indexed := chainIndexSize(t, ctx, source)

	booted := noTrimChainRun(t, ctx, storage)
	decoded := booted.CommittedFacts()
	if decoded >= indexed {
		t.Fatalf("the default boot decoded %d of the %d indexed facts, want the anchor to leave the folded material unread", decoded, indexed)
	}
	served, carried := booted.replays[addr]
	if !carried {
		t.Fatalf("the default boot registered no sequence for %v", addr)
	}
	if served.projection.Len() == 0 {
		t.Fatal("the default boot served an empty projection beside a folded chain")
	}
	full := noTrimChainRun(t, ctx, storage)
	if _, _, _, err := full.seedFullReplay(ctx, full.tier); err != nil {
		t.Fatalf("replay the whole chain: %v", err)
	}
	if whole := full.CommittedFacts(); whole != indexed {
		t.Fatalf("the full walk decoded %d facts beside the %d the chain indexes and the %d the boot decoded, want one ledger of one chain", whole, indexed, decoded)
	}
	fullSeeds, _, _, err := full.seedFullReplay(ctx, full.tier)
	if err != nil {
		t.Fatalf("replay the whole chain again: %v", err)
	}
	want, carried := fullSeeds[addr]
	if !carried {
		t.Fatalf("the full walk registered no projection for %v", addr)
	}
	if got, want := replay.RenderProjection(served.projection.Entries()), replay.RenderProjection(want.projection.Entries()); !bytes.Equal(got, want) {
		t.Fatalf("the default boot renders %d bytes, want the full walk %d", len(got), len(want))
	}

	plainRoot := t.TempDir()
	plainStorage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: plainRoot})
	plain := noTrimChainRun(t, ctx, plainStorage)
	commitWindowFact(t, ctx, plain, addr, windowEvent(addr, "p1-in", "inv-plain", "the one fact of an unfolded chain", event.TypeExternalInput))
	if _, err := plain.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the unfolded process: %v", err)
	}
	plainBooted := noTrimChainRun(t, ctx, plainStorage)
	if got, want := plainBooted.CommittedFacts(), chainIndexSize(t, ctx, plainBooted); got > want {
		t.Fatalf("the boot over an unfolded chain decoded %d of %d facts, want no more than the chain holds", got, want)
	}
}

// TestTheRestoredProjectionReadsOnlyPastItsSurvivors pins the read of a boot the projection slice of a carrier rebuilds beside no fold product.
//   - the boot reads the survivor bodies and the facts committed past the newest of them, never the chain the survivors cover
//   - the covered history grows without growing one read of the boot
//   - the covered filler states no content, so the projection the run presents never holds it and the restart has no reason to read it
//   - the projection the boot serves is the projection the full walk rebuilds
//
// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
func TestTheRestoredProjectionReadsOnlyPastItsSurvivors(t *testing.T) {
	reads := map[int]int{}
	indexed := map[int]int{}
	for _, filler := range []int{40, 400} {
		ctx := context.Background()
		storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: t.TempDir()})
		addr := event.Address{AgentID: "agent-a", SessionID: event.SessionID(fmt.Sprintf("session-restored-bound-%d", filler))}
		source := noTrimNoWindowRun(t, ctx, storage)
		for index := 0; index < filler; index++ {
			if _, err := source.commitFactChecked(ctx, event.Event{
				Type:      event.TypeFeedback,
				Timestamp: time.Now(),
				Owner:     addr, Source: addr, Target: addr,
				Meta: map[string]string{event.MetaKeyEventSummary: fmt.Sprintf("covered filler %d", index)},
			}); err != nil {
				t.Fatalf("commit covered filler %d: %v", index, err)
			}
		}
		commitWindowTurns(t, ctx, source, addr, 2)
		if count := countFoldProducts(t, ctx, source); count != 0 {
			t.Fatalf("the fixture filed %d product(s), want the carrier of a chain that never folded", count)
		}
		if _, filed, err := source.Checkpoint(ctx); err != nil || !filed {
			t.Fatalf("file the carrier of the chain = (%t, %v), want a filed fact", filed, err)
		}
		indexed[filler] = chainIndexSize(t, ctx, source)
		if indexed[filler] <= filler {
			t.Fatalf("the chain indexes %d fact(s) beside %d covered fact(s), want the covered history on the chain", indexed[filler], filler)
		}
		if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(5 * time.Second)}); err != nil {
			t.Fatalf("close the source process: %v", err)
		}

		booted := noTrimNoWindowRun(t, ctx, storage)
		if filed, _ := booted.CheckpointFiled(); filed == "" {
			t.Fatal("the boot restored no carrier, so the bound proves nothing")
		}
		reads[filler] = booted.CommittedFacts()

		full := noTrimNoWindowRun(t, ctx, storage)
		fullSeeds, _, _, err := full.seedFullReplay(ctx, full.tier)
		if err != nil {
			t.Fatalf("replay the whole chain: %v", err)
		}
		want, carried := fullSeeds[addr]
		if !carried {
			t.Fatalf("the full walk registered no projection for %v", addr)
		}
		served, carried := booted.replays[addr]
		if !carried {
			t.Fatalf("the boot registered no projection for %v", addr)
		}
		if got, want := replay.RenderProjection(served.projection.Entries()), replay.RenderProjection(want.projection.Entries()); !bytes.Equal(got, want) {
			t.Fatalf("the boot beside %d covered fact(s) renders %d bytes, want the full walk %d", filler, len(got), len(want))
		}
		if reads[filler] >= indexed[filler] {
			t.Fatalf("the boot beside %d covered fact(s) read %d of the %d the chain indexes, want the carrier to leave the covered material unread",
				filler, reads[filler], indexed[filler])
		}
	}
	if reads[400] != reads[40] {
		t.Fatalf("the boot read %d bodies beside 400 covered facts and %d beside 40, want a read that follows the survivors rather than the covered history",
			reads[400], reads[40])
	}
}

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
func noWindowBootRun(t *testing.T, ctx context.Context, addr event.Address) *Run {
	t.Helper()
	run, err := Assemble(ctx, &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}, map[event.AgentID]NativeAgent{"agent-a": {Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
		return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: factWindowAnswer}}}, Done: true}
	}}}}, WithGovernance(snapshotTestGovernance{}),
		WithStorage(ledger.StorageConfig{Type: ledger.StorageMemory}))
	if err != nil {
		t.Fatalf("assemble the run: %v", err)
	}
	return run
}

// TestAnchoredSurvivorsFollowTheResidentBound pins the survivor trim.
// - a checkpoint naming more survivors than the resident bound rebuilds the bound, not the payload
// - the rebuilt projection holds the newest survivors in payload order (negative: the oldest are gone)
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestAnchoredSurvivorsFollowTheResidentBound(t *testing.T) {
	ctx := context.Background()
	addr := event.Address{AgentID: "agent-a", SessionID: "session-survivor-bound"}
	run := noWindowBootRun(t, ctx, addr)
	survivors := make([]anchorEntry, 0, 2*noWindowResidentFacts+10)
	for at := 0; at < 2*noWindowResidentFacts+10; at++ {
		survivors = append(survivors, anchorEntry{
			Key:     event.EventID(fmt.Sprintf("-%d-survivor", at)),
			Type:    replay.ProjectionTypeSegmentFold,
			Summary: "a folded line",
			Content: fmt.Sprintf("the folded text %d", at),
			Time:    time.Date(2026, 11, 6, 12, at%60, 0, 0, time.UTC).UnixMilli(),
		})
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
	if last := entries[len(entries)-1]; last.Key != newest.Key || last.Content != newest.Content {
		t.Fatalf("the rebuilt projection ends at %s, want the newest survivor %s", last.Key, newest.Key)
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
	fallbackSeeds, fallbackReport, err := fallback.seedAnchoredReplayBlankTypes(ctx, fallback.tier)
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
	if anchoredReport.Anchors == 0 {
		t.Fatal("the folded chain minted no anchor the rebuild could use")
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
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func TestDefaultBootRebuildsFromTheAnchor(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	storage := WithStorage(ledger.StorageConfig{Type: ledger.StorageLocalFile, Root: root})
	addr := event.Address{AgentID: "agent-a", SessionID: "session-default-anchor"}
	source := noTrimChainRun(t, ctx, storage)
	commitWindowTurns(t, ctx, source, addr, 12)
	if count := countFoldProducts(t, ctx, source); count == 0 {
		t.Fatal("the fixture folded nothing, so the boot proves nothing")
	}
	indexed := chainIndexSize(t, ctx, source)
	if _, err := source.Close(ctx, agent.CloseRequest{Timeout: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatalf("close the source process: %v", err)
	}

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
	if got, want := plainBooted.CommittedFacts(), chainIndexSize(t, ctx, plainBooted); got != want {
		t.Fatalf("the boot over an unfolded chain decoded %d of %d facts, want the full walk", got, want)
	}
}

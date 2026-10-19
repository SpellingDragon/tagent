package history

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestCompressionProductsAreEquivalent pins the compression products to their inputs, whichever path minted them.
// - the same material mints the same artifact whether a pass is triggered directly or consumed as a request
// - the artifact identity follows the refs it carries, not the arrival order of the cards or the pass moment
// - one entry settles on one ticket decision: same key, mode and preview, and one settle identity
// - the rendered ticket reaches the original through the segment ticket the decision carries
// - one projection folds to one compaction line and one fold identity, whichever path folded it
// - the fold identity is content-addressed over the payload, not over the moment it was committed
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressionProductsAreEquivalent(t *testing.T) {
	t.Run("artifact 同一材料同一身份: one material mints one artifact", func(t *testing.T) {
		ctx := context.Background()
		direct := newCompressionCuration(t)
		directReport, err := direct.Curate(ctx, compressionNow, compressionCards())
		if err != nil {
			t.Fatalf("Curate: %v", err)
		}
		if !directReport.Triggered || directReport.Artifact == nil {
			t.Fatalf("direct pass = triggered %v artifact %v, want the over-limit pass to condense", directReport.Triggered, directReport.Artifact)
		}

		requests := memory.NewCompressRequests()
		var products []meditation.MeditationArtifact
		consumer := newCompressionCuration(t,
			meditation.WithCompressRequests(requests),
			meditation.WithMeditationCards(compressionCardSource()),
			meditation.WithMeditationProducts(func(_ context.Context, artifact meditation.MeditationArtifact) error {
				products = append(products, artifact)
				return nil
			}),
		)
		if _, filed, err := consumer.Judge(ctx, compressionNow, compressionCards()); err != nil || !filed {
			t.Fatalf("Judge = (filed %v, %v), want the over-limit sequence to file its request", filed, err)
		}
		tick, err := consumer.Tick(ctx, compressionNow)
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if len(tick.Failures) != 0 || len(tick.Read) != 1 || len(tick.Reports) != 1 || len(products) != 1 {
			t.Fatalf("tick = (failures %v, read %d, reports %d, products %d), want one consumed request", tick.Failures, len(tick.Read), len(tick.Reports), len(products))
		}
		consumedReport := tick.Reports[0]
		if !reflect.DeepEqual(*directReport.Artifact, *consumedReport.Artifact) {
			t.Fatalf("artifacts = (direct %+v, consumed %+v), want one product for one material", *directReport.Artifact, *consumedReport.Artifact)
		}
		if !reflect.DeepEqual(*directReport.Artifact, products[0]) {
			t.Fatalf("committed product = %+v, want the artifact the pass minted %+v", products[0], *directReport.Artifact)
		}
		if !reflect.DeepEqual(directReport.Fates, consumedReport.Fates) || !reflect.DeepEqual(directReport.Evicted, consumedReport.Evicted) {
			t.Fatalf("fates = (direct %v, consumed %v) evicted %v, want one fate per card on both paths", directReport.Fates, consumedReport.Fates, directReport.Evicted)
		}

		artifact := *directReport.Artifact
		if artifact.Type != meditation.MeditationArtifactType {
			t.Fatalf("artifact type = %q, want the frozen %q", artifact.Type, meditation.MeditationArtifactType)
		}
		if artifact.Summary != "需求原文 / 实现摘要" {
			t.Fatalf("artifact summary = %q, want the deterministic condensation of the overflow run", artifact.Summary)
		}
		if !reflect.DeepEqual(artifact.Refs, []event.EventID{"cc-2", "cc-1"}) {
			t.Fatalf("artifact refs = %v, want the kept cards in value order", artifact.Refs)
		}
		if !strings.HasPrefix(string(artifact.Key), "cur-") {
			t.Fatalf("artifact key = %q, want the content-addressed identity the condensation answers", artifact.Key)
		}

		shuffled := newCompressionCuration(t)
		shuffledReport, err := shuffled.Curate(ctx, compressionNow, compressionCardsReversed())
		if err != nil {
			t.Fatalf("Curate(cards reversed): %v", err)
		}
		if shuffledReport.Artifact == nil || shuffledReport.Artifact.Key != artifact.Key || !reflect.DeepEqual(shuffledReport.Artifact.Refs, artifact.Refs) {
			t.Fatalf("artifact of the reversed arrival order = %+v, want the identity %q of the same material", shuffledReport.Artifact, artifact.Key)
		}

		later := newCompressionCuration(t)
		laterReport, err := later.Curate(ctx, compressionNow.Add(time.Minute), compressionCards())
		if err != nil {
			t.Fatalf("Curate(a minute later): %v", err)
		}
		if laterReport.Artifact == nil || laterReport.Artifact.CreatedAt.Equal(artifact.CreatedAt) || laterReport.Artifact.Key != artifact.Key {
			t.Fatalf("artifact a minute later = %+v, want the same identity %q under another minting moment", laterReport.Artifact, artifact.Key)
		}
	})

	t.Run("ticket 同一输入同一呈现: one entry settles on one ticket", func(t *testing.T) {
		ctx := context.Background()
		first, firstEntries := assembleCompression(t, ctx)
		second, secondEntries := assembleCompression(t, ctx)

		decision, recorded := first.Decision("cc-1")
		preview := strings.Repeat("A", 128) + "…"
		if !recorded || decision.Key != "cc-1" || decision.Mode != replay.PresentationTicket || decision.Preview != preview {
			t.Fatalf("ticket decision = %+v (%v), want a ticket over cc-1 with the bounded preview", decision, recorded)
		}
		if len(firstEntries) != 3 || firstEntries[0].Key != "cc-1" {
			t.Fatalf("assembled entries = %+v, want the large entry presented first and the entries past the budget skipped", firstEntries)
		}
		wantTicket := preview + "\n[ticket cc-1: full text retrievable by segment]"
		if firstEntries[0].Content != wantTicket {
			t.Fatalf("presented ticket = %q, want the preview and the segment ticket %q", firstEntries[0].Content, wantTicket)
		}
		if len(first.Settles()) != len(firstEntries) {
			t.Fatalf("settle facts = %+v, want one per settled entry and none for the skipped ones", first.Settles())
		}

		otherDecision, _ := second.Decision("cc-1")
		if otherDecision != decision {
			t.Fatalf("ticket decisions = (%+v, %+v), want one decision for one input", decision, otherDecision)
		}
		if len(secondEntries) != len(firstEntries) || !reflect.DeepEqual(secondEntries, firstEntries) {
			t.Fatalf("second assembly = %+v, want the entries the first assembly presented %+v", secondEntries, firstEntries)
		}

		settle := settleOf(t, first.Settles(), "cc-1")
		if settle.Entry != "cc-1" || settle.Mode != decision.Mode || settle.Preview != decision.Preview {
			t.Fatalf("settle fact = %+v, want the entry, mode and preview of the ticket decision", settle)
		}
		identity := replay.ReplaySettleFor(decision)
		if settle.ID != identity.ID || settle.FactKey() != string(settle.ID) || settle.FactType() != replay.ReplaySettleType {
			t.Fatalf("settle identity = (%q, %q, %q), want the decision identity read as %q", settle.FactKey(), settle.FactType(), settle.ID, replay.ReplaySettleType)
		}
		if settleOf(t, second.Settles(), "cc-1").ID != identity.ID {
			t.Fatal("one ticket decision settled under two identities")
		}
		if len(first.Settles()) != len(second.Settles()) {
			t.Fatalf("settle facts = (%d, %d), want one per settled entry on both assemblies", len(first.Settles()), len(second.Settles()))
		}
	})

	t.Run("fold 同一投影同一折叠线: one projection folds to one line", func(t *testing.T) {
		ctx := context.Background()
		session := replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("s-compression")}
		chain := replay.NewMemoryEventChain()
		consumerProjection := newCompressionProjection(t)
		requests := memory.NewCompressRequests()
		var folds []replay.CompactionFact
		consumer := newCompressionCuration(t,
			meditation.WithCompressRequests(requests),
			meditation.WithMeditationCards(compressionCardSource()),
			meditation.WithMeditationProducts(func(context.Context, meditation.MeditationArtifact) error { return nil }),
			meditation.WithMeditationPreFold(
				func(context.Context) (*replay.ReplayProjection, error) { return consumerProjection, nil },
				func(ctx context.Context, payload replay.CompactionFact) error {
					fact, err := meditation.CompactionChainFact(session, payload, compressionNow)
					if err != nil {
						return err
					}
					if err := chain.Commit(ctx, fact); err != nil {
						return err
					}
					folds = append(folds, payload)
					return nil
				},
			),
		)
		if _, filed, err := consumer.Judge(ctx, compressionNow, compressionCards()); err != nil || !filed {
			t.Fatalf("Judge = (filed %v, %v), want the over-limit sequence to file its request", filed, err)
		}
		tick, err := consumer.Tick(ctx, compressionNow)
		if err != nil {
			t.Fatalf("Tick: %v", err)
		}
		if len(tick.Failures) != 0 || len(tick.Folds) != 1 || len(folds) != 1 {
			t.Fatalf("folds = (failures %v, tick %d, sink %d), want one pre-fold of the projection", tick.Failures, len(tick.Folds), len(folds))
		}

		directProjection := newCompressionProjection(t)
		direct, err := meditation.Prefold(ctx, directProjection)
		if err != nil {
			t.Fatalf("Prefold: %v", err)
		}
		if !reflect.DeepEqual(direct, tick.Folds[0]) || !reflect.DeepEqual(direct, folds[0]) {
			t.Fatalf("fold payloads = (direct %+v, consumed %+v), want one payload for one projection", direct, tick.Folds[0])
		}
		if direct.Summary.Content != "compaction: 需求原文; 实现摘要; 验证命令; 收尾记录; tool chain" {
			t.Fatalf("fold line = %q, want the summaries of the folded entries in order", direct.Summary.Content)
		}
		if direct.Summary.Type != replay.CompactionEventType || direct.FullBoundary != "cc-4" {
			t.Fatalf("fold summary = %+v boundary = %q, want the compaction type and the last keyed entry", direct.Summary, direct.FullBoundary)
		}
		if refs := compressionRefKeys(direct.Refs); !reflect.DeepEqual(refs, []event.EventID{"cc-3", "-ref-1-4"}) {
			t.Fatalf("fold refs = %v, want the synthetic ref and the retained tool fact in payload order", refs)
		}

		identity := meditation.CompactionFactKey(direct)
		if meditation.CompactionFactKey(tick.Folds[0]) != identity {
			t.Fatal("one fold payload answered two identities")
		}
		if _, held, err := chain.Fact(ctx, identity); err != nil || !held {
			t.Fatalf("fold fact = (held %v, %v), want the compaction fact committed under the fold identity", held, err)
		}
		later, err := meditation.CompactionChainFact(session, direct, compressionNow.Add(time.Hour))
		if err != nil {
			t.Fatalf("CompactionChainFact: %v", err)
		}
		if later.Key != identity || later.Time.Equal(compressionNow) {
			t.Fatalf("fold fact a hour later = (%q, %v), want the identity %q under another commit moment", later.Key, later.Time, identity)
		}
		entries := consumerProjection.Entries()
		if len(entries) == 0 || entries[0].Type != replay.CompactionEventType || entries[0].Content != direct.Summary.Content {
			t.Fatalf("folded entries = %+v, want the compaction line read first", entries)
		}
	})
}

// compressionNow is the pass moment every compression product is minted at.
var compressionNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

// compressionCards is the card sequence every compression path is driven with: four cards over a bound of two.
func compressionCards() []meditation.MeditationCard {
	return []meditation.MeditationCard{
		{Key: "cc-1", Type: event.TypeExternalInput, Summary: "需求原文", RecordedAt: compressionNow.Add(-72 * time.Hour)},
		{Key: "cc-2", Type: event.TypeAgentOutput, Summary: "实现摘要", RecordedAt: compressionNow.Add(-48 * time.Hour), Links: 1},
		{Key: "cc-3", Type: event.TypeActionCommand, Summary: "验证命令", RecordedAt: compressionNow.Add(-2 * time.Hour), Links: 3},
		{Key: "cc-4", Type: event.TypeAgentOutput, Summary: "收尾记录", RecordedAt: compressionNow.Add(-time.Hour)},
	}
}

// compressionCardsReversed is the same material arriving in the opposite order: the arrival order names nothing about the material.
func compressionCardsReversed() []meditation.MeditationCard {
	cards := compressionCards()
	reversed := make([]meditation.MeditationCard, 0, len(cards))
	for i := len(cards) - 1; i >= 0; i-- {
		reversed = append(reversed, cards[i])
	}
	return reversed
}

// compressionCardSource is the static card source a consumer tick curates.
func compressionCardSource() meditation.MeditationCards {
	return func(context.Context) ([]meditation.MeditationCard, error) { return compressionCards(), nil }
}

// compressionEntries is the projection of the same material: one large entry that settles on a ticket, one keyless tool chain and two short entries.
func compressionEntries() []replay.ProjectionEntry {
	return []replay.ProjectionEntry{
		{Key: "cc-1", Type: event.TypeExternalInput, Summary: "需求原文", Content: strings.Repeat("A", 600), Time: compressionNow.Add(-72 * time.Hour)},
		{Key: "cc-2", Type: event.TypeAgentOutput, Summary: "实现摘要", Content: "实现摘要正文", Time: compressionNow.Add(-48 * time.Hour)},
		{Key: "cc-3", Type: event.TypeActionCommand, Summary: "验证命令", Content: "验证命令正文", Time: compressionNow.Add(-2 * time.Hour)},
		{Key: "cc-4", Type: event.TypeAgentOutput, Summary: "收尾记录", Content: "收尾记录正文", Time: compressionNow.Add(-time.Hour)},
		{Type: event.TypeActionCommand, Summary: "tool chain", Content: "工具链 grep → sed", Time: compressionNow.Add(-30 * time.Minute)},
	}
}

// newCompressionProjection returns a projection of the compression material, appended in payload order.
func newCompressionProjection(t *testing.T) *replay.ReplayProjection {
	t.Helper()
	ctx := context.Background()
	projection := replay.NewReplayProjection()
	for _, entry := range compressionEntries() {
		projection.Append(ctx, entry)
	}
	return projection
}

// assembleCompression assembles one fresh projection under the ticket budget and answers it with its settle facts.
func assembleCompression(t *testing.T, ctx context.Context) (*replay.ReplayProjection, []replay.ProjectionEntry) {
	t.Helper()
	projection := newCompressionProjection(t)
	entries, err := projection.AssembleBudget(ctx, 55)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}
	return projection, entries
}

// newCompressionCuration returns a curation engine over its own retention ledger and lifecycle, so two paths never share a pass.
func newCompressionCuration(t *testing.T, options ...meditation.MeditationOption) *meditation.Meditation {
	t.Helper()
	store := ledger.NewMemoryLifecycleStore(ledger.LifecycleCapabilities{})
	retention := ledger.NewRetention(ledger.NewMemoryGuard())
	lifecycle, err := ledger.NewLifecycle(ledger.LifecycleConfig{GlobalTTLDays: -1}, retention, store, ledger.LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	curation, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 2, Keep: 2, MinScore: 1}, retention, lifecycle, nil, options...)
	if err != nil {
		t.Fatalf("open the curation engine: %v", err)
	}
	return curation
}

// settleOf returns the settle fact one entry settled under.
func settleOf(t *testing.T, settles []replay.ReplaySettle, entry event.EventID) replay.ReplaySettle {
	t.Helper()
	for _, settle := range settles {
		if settle.Entry == entry {
			return settle
		}
	}
	t.Fatalf("settle facts = %+v, want one for %q", settles, entry)
	return replay.ReplaySettle{}
}

// compressionRefKeys lists the keys of one fold payload in payload order.
func compressionRefKeys(refs []replay.CompactionRef) []event.EventID {
	keys := make([]event.EventID, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key)
	}
	return keys
}

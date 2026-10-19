package capabilities

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

// curStack is the real curation stack one case drives: the shared guard, the
// retention ledger over it, the lifecycle owning the one tombstone channel,
// and the original store the facts live in.
type curStack struct {
	retention      *ledger.Retention
	lifecycle      *ledger.Lifecycle
	lifecycleStore *ledger.MemoryLifecycleStore
	originals      *ledger.MemoryOriginalStore
}

// newCurStack wires the real curation stack: the retention ledger over the
// shared guard, the lifecycle over the reference lifecycle store, and the
// in-memory original store. The recovery inventory is complete before any pass
// runs, so every destructive step is armed exactly as a finished inventory
// arms it.
func newCurStack(t *testing.T) *curStack {
	t.Helper()
	retention := ledger.NewRetention(ledger.NewMemoryGuard())
	caps := ledger.LifecycleCapabilities{
		DurableRecovery: true,
		Protection:      true,
		Reclamation:     true,
	}
	lifecycleStore := ledger.NewMemoryLifecycleStore(caps)
	lifecycle, err := ledger.NewLifecycle(ledger.LifecycleConfig{}, retention, lifecycleStore, caps)
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	return &curStack{
		retention:      retention,
		lifecycle:      lifecycle,
		lifecycleStore: lifecycleStore,
		originals:      ledger.NewMemoryOriginalStore(),
	}
}

// curOwnerOf is the address every curation original is registered under.
func curOwnerOf() event.Address {
	return event.Address{AgentID: "agent-curation", SessionID: "session-curation"}
}

// curOriginalOf is the committed original text behind one card key.
func curOriginalOf(key event.EventID) []byte {
	return []byte("original-bytes-of-" + string(key) + "\n")
}

// curRefOf is the reference metadata one card original is registered under.
func curRefOf(key event.EventID) ledger.OriginalRef {
	return ledger.OriginalRef{
		Key:             key,
		Owner:           curOwnerOf(),
		Format:          "text/plain",
		RetentionPolicy: "keep",
	}
}

// seedCur commits every card original under its reference and files its key
// under the lifecycle enumeration, then returns the committed state the pass
// must leave untouched.
func seedCur(t *testing.T, stack *curStack, cards []meditation.MeditationCard) map[event.EventID]curCommitted {
	t.Helper()
	ctx := context.Background()
	committed := map[event.EventID]curCommitted{}
	for _, card := range cards {
		body := curOriginalOf(card.Key)
		if err := stack.originals.Put(ctx, curRefOf(card.Key), body); err != nil {
			t.Fatalf("Put(%s): %v", card.Key, err)
		}
		stack.lifecycleStore.Put(ledger.StoredKey{
			Key:        card.Key,
			Type:       card.Type,
			Partition:  1,
			RecordedAt: card.RecordedAt,
		})
		committed[card.Key] = curCommitted{body: body, refs: stack.originals.References(card.Key)}
	}
	return committed
}

// curCommitted is the committed state of one original: its bytes and its
// reference metadata, captured before the pass so any rewrite is visible.
type curCommitted struct {
	body []byte
	refs []ledger.OriginalRef
}

// assertCurUntouched checks that every original still reads back byte for
// byte under unchanged reference metadata: the pass rewrites, overwrites and
// migrates nothing.
func assertCurUntouched(t *testing.T, stack *curStack, committed map[event.EventID]curCommitted) {
	t.Helper()
	ctx := context.Background()
	for key, want := range committed {
		result := stack.originals.Get(ctx, key)
		if result.Outcome != ledger.RecallFound {
			t.Fatalf("Get(%s) outcome = %v, want RecallFound: curation never removes an original here", key, result.Outcome)
		}
		if !reflect.DeepEqual(result.Body, want.body) {
			t.Fatalf("Get(%s) = %q, want the committed original byte for byte: curation rewrites no original fact", key, result.Body)
		}
		if got := stack.originals.References(key); !reflect.DeepEqual(got, want.refs) {
			t.Fatalf("References(%s) = %+v, want %+v: curation rewrites no reference metadata", key, got, want.refs)
		}
	}
}

// curFailCondenser is the condensation that always fails: the seat an LLM
// curation would take, answering with a named failure.
func curFailCondenser(context.Context, meditation.MeditationRun, time.Time) (string, error) {
	return "", errors.New("condense call refused")
}

// TestMemoryCuration drives the memory curation pass over the real retention ledger and pins the curation contract onto real sub-cases.
//   - 整理不改写原文: every original byte and reference metadata stays untouched and the output is only a new condensed artifact
//   - 超限整理且 recall 链不断: the condensed result keeps the key references and recall refills by key byte for byte
//   - 整理失败降级不塌: a failing condensation sinks into the count line with its failure recorded and the pass continues
//   - 留存/淘汰/引用保护三通道互斥: every card lands in exactly one fate, eviction travels the tombstone channel alone and referenced originals refuse deletion
//   - the pass triggers only over the sequence bound and stays deterministic
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestMemoryCuration(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	curCards := func() []meditation.MeditationCard {
		return []meditation.MeditationCard{
			{Key: "card-1", Type: event.TypeExternalInput, Summary: "迁移计划上文", RecordedAt: now.Add(-72 * time.Hour), Links: 0},
			{Key: "card-2", Type: event.TypeAgentOutput, Summary: "灰度演练记录", RecordedAt: now.Add(-60 * time.Hour), Links: 1},
			{Key: "card-3", Type: event.TypeThinkingPlan, Summary: "复盘记录", RecordedAt: now.Add(-48 * time.Hour), Links: 3},
			{Key: "card-4", Type: event.TypeExternalInput, Summary: "登录改版", RecordedAt: now.Add(-4 * time.Hour), Links: 0},
			{Key: "card-5", Type: event.TypeActionCommand, Summary: "次序清单", RecordedAt: now.Add(-3 * time.Hour), Links: 2},
			{Key: "card-6", Type: event.TypeAgentOutput, Summary: "回填索引", RecordedAt: now.Add(-2 * time.Hour), Links: 0},
		}
	}

	t.Run("整理不改写原文: a curation pass rewrites no original fact", func(t *testing.T) {
		stack := newCurStack(t)
		cards := curCards()
		committed := seedCur(t, stack, cards)
		engine, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 3, Keep: 2, MinScore: 6}, stack.retention, stack.lifecycle, nil)
		if err != nil {
			t.Fatalf("NewMeditation: %v", err)
		}
		report, err := engine.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate: %v", err)
		}
		if !report.Triggered || report.Artifact == nil {
			t.Fatalf("report = triggered %v artifact %v, want the over-limit pass to condense", report.Triggered, report.Artifact)
		}
		assertCurUntouched(t, stack, committed)

		artifact := *report.Artifact
		if artifact.Type != meditation.MeditationArtifactType {
			t.Fatalf("artifact type = %q, want %q", artifact.Type, meditation.MeditationArtifactType)
		}
		for _, card := range cards {
			if artifact.Key == card.Key {
				t.Fatalf("artifact key = %s, want a new reference that is never a rewritten fact", artifact.Key)
			}
		}
		if len(artifact.Refs) == 0 {
			t.Fatal("artifact carries no key reference, want the condensed result to relate to the facts it condenses")
		}
		for _, ref := range artifact.Refs {
			result := stack.originals.Get(ctx, ref)
			if result.Outcome != ledger.RecallFound || !reflect.DeepEqual(result.Body, curOriginalOf(ref)) {
				t.Fatalf("recall of artifact ref %s = %v, want the committed original byte for byte: the artifact stays traceable to the facts", ref, result.Outcome)
			}
		}
	})

	t.Run("超限整理且 recall 链不断: the condensed result keeps key references and recall refills by key", func(t *testing.T) {
		stack := newCurStack(t)
		cards := curCards()
		committed := seedCur(t, stack, cards)
		engine, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 3, Keep: 2, MinScore: 6}, stack.retention, stack.lifecycle, nil)
		if err != nil {
			t.Fatalf("NewMeditation: %v", err)
		}
		report, err := engine.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate: %v", err)
		}
		artifact := *report.Artifact
		wantRefs := []event.EventID{"card-3", "card-2"}
		if !reflect.DeepEqual(artifact.Refs, wantRefs) {
			t.Fatalf("artifact refs = %v, want %v: the key cards survive as references in value order", artifact.Refs, wantRefs)
		}
		for _, ref := range wantRefs {
			if report.Fates[ref] != meditation.MeditationProtect {
				t.Fatalf("fate of %s = %v, want protect: the referenced original is never deleted", ref, report.Fates[ref])
			}
		}
		if report.Fates["card-1"] != meditation.MeditationEvict {
			t.Fatalf("fate of card-1 = %v, want evict: the unprotected low-value overflow card is evicted", report.Fates["card-1"])
		}
		if !reflect.DeepEqual(report.Evicted, []event.EventID{"card-1"}) {
			t.Fatalf("evicted = %v, want [card-1]", report.Evicted)
		}

		result := stack.originals.Get(ctx, "card-1")
		if result.Outcome != ledger.RecallFound || !reflect.DeepEqual(result.Body, committed["card-1"].body) {
			t.Fatalf("recall of the evicted card-1 = %v, want the committed original until reclamation", result.Outcome)
		}
		if _, marked := stack.lifecycleStore.Tombstoned("card-1"); !marked {
			t.Fatal("evicted card-1 carries no tombstone, want the eviction on the tombstone channel")
		}

		destroyed, err := stack.lifecycle.Reclaim(ctx, now)
		if err != nil {
			t.Fatalf("Reclaim: %v", err)
		}
		if destroyed != 1 {
			t.Fatalf("Reclaim destroyed %d keys, want exactly the evicted one", destroyed)
		}
		for _, ref := range wantRefs {
			result := stack.originals.Get(ctx, ref)
			if result.Outcome != ledger.RecallFound || !reflect.DeepEqual(result.Body, committed[ref].body) {
				t.Fatalf("recall of referenced %s after reclamation = %v, want the committed original byte for byte", ref, result.Outcome)
			}
			if _, marked := stack.lifecycleStore.Tombstoned(ref); marked {
				t.Fatalf("referenced %s carries a tombstone, want none: the referenced original is never deleted", ref)
			}
		}
		assertCurUntouched(t, stack, committed)
	})

	t.Run("整理失败降级不塌: a failing condensation sinks and rewrites nothing", func(t *testing.T) {
		stack := newCurStack(t)
		cards := curCards()
		committed := seedCur(t, stack, cards)
		engine, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 3, Keep: 2, MinScore: 6}, stack.retention, stack.lifecycle, curFailCondenser)
		if err != nil {
			t.Fatalf("NewMeditation: %v", err)
		}
		report, err := engine.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate = %v, want the failed condensation sunk instead of failing the caller", err)
		}
		if len(report.Failures) != 1 {
			t.Fatalf("sink account = %d failures, want the condensation failure sunk exactly once", len(report.Failures))
		}
		if report.Artifact == nil {
			t.Fatal("degraded pass produced no artifact, want the engineering fallback")
		}
		if report.Artifact.Sunk != 3 || report.Artifact.Summary != "(earlier 3 items)" {
			t.Fatalf("degraded artifact = sunk %d summary %q, want the overflow run sunk into the count line", report.Artifact.Sunk, report.Artifact.Summary)
		}
		if !reflect.DeepEqual(report.Artifact.Refs, []event.EventID{"card-3", "card-2"}) {
			t.Fatalf("degraded artifact refs = %v, want the key references kept even in degradation", report.Artifact.Refs)
		}
		assertCurUntouched(t, stack, committed)
		for _, card := range cards {
			result := stack.originals.Get(ctx, card.Key)
			if result.Outcome != ledger.RecallFound || !reflect.DeepEqual(result.Body, committed[card.Key].body) {
				t.Fatalf("recall of %s after the degraded pass = %v, want the committed original byte for byte", card.Key, result.Outcome)
			}
		}
		healthy, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 3, Keep: 2, MinScore: 6}, stack.retention, stack.lifecycle, nil)
		if err != nil {
			t.Fatalf("NewMeditation(after failure): %v", err)
		}
		again, err := healthy.Curate(ctx, now, cards)
		if err != nil || again.Artifact == nil || len(again.Failures) != 0 {
			t.Fatalf("second pass = (%v, %+v), want the main flow unaffected by the sunk failure", err, again.Failures)
		}
	})

	t.Run("留存/淘汰/引用保护三通道互斥且淘汰只走墓碑通道", func(t *testing.T) {
		stack := newCurStack(t)
		cards := curCards()
		committed := seedCur(t, stack, cards)
		shared := ledger.HolderID("shared:outbox-obligation")
		if _, err := stack.retention.Register(ctx, "card-6", shared, ledger.RetentionRecord{
			Kind:       ledger.RetentionUnconfirmed,
			Type:       event.TypeAgentOutput,
			RecordedAt: now.Add(-2 * time.Hour),
		}); err != nil {
			t.Fatalf("Hold(card-6): %v", err)
		}
		engine, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 3, Keep: 1, MinScore: 6}, stack.retention, stack.lifecycle, nil)
		if err != nil {
			t.Fatalf("NewMeditation: %v", err)
		}
		report, err := engine.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate: %v", err)
		}

		if len(report.Fates) != len(cards) {
			t.Fatalf("fates cover %d cards, want %d: every card is filed under exactly one channel", len(report.Fates), len(cards))
		}
		counts := map[meditation.MeditationFate]int{}
		for key, fate := range report.Fates {
			switch fate {
			case meditation.MeditationRetain, meditation.MeditationEvict, meditation.MeditationProtect:
				counts[fate]++
			default:
				t.Fatalf("fate of %s = %q, want one of retain/evict/protect: no fourth channel exists", key, fate)
			}
		}
		if counts[meditation.MeditationRetain] == 0 || counts[meditation.MeditationEvict] == 0 || counts[meditation.MeditationProtect] == 0 {
			t.Fatalf("fate counts = %v, want every channel exercised", counts)
		}
		if report.Fates["card-6"] != meditation.MeditationProtect {
			t.Fatalf("fate of card-6 = %v, want protect: a standing hold keeps the original out of every eviction", report.Fates["card-6"])
		}

		for _, card := range cards {
			_, marked := stack.lifecycleStore.Tombstoned(card.Key)
			if marked != (report.Fates[card.Key] == meditation.MeditationEvict) {
				t.Fatalf("tombstone of %s = %v under fate %v, want the evicted set and the tombstoned set identical", card.Key, marked, report.Fates[card.Key])
			}
		}
		for _, key := range report.Evicted {
			result := stack.originals.Get(ctx, key)
			if result.Outcome != ledger.RecallFound || !reflect.DeepEqual(result.Body, committed[key].body) {
				t.Fatalf("recall of evicted %s = %v, want the bytes untouched until reclamation: eviction rewrites nothing", key, result.Outcome)
			}
		}

		if err := stack.lifecycle.Delete(ctx, "card-6", now); !errors.Is(err, ledger.ErrProtected) {
			t.Fatalf("Delete(card-6) = %v, want the named refusal ErrProtected", err)
		}
		for _, ref := range report.Artifact.Refs {
			if err := stack.lifecycle.Delete(ctx, ref, now); !errors.Is(err, ledger.ErrProtected) {
				t.Fatalf("Delete(referenced %s) = %v, want the named refusal ErrProtected", ref, err)
			}
		}
		assertCurUntouched(t, stack, committed)
	})

	t.Run("the pass triggers only over the bound and stays deterministic", func(t *testing.T) {
		stack := newCurStack(t)
		cards := curCards()
		seedCur(t, stack, cards)
		engine, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 6, Keep: 2, MinScore: 6}, stack.retention, stack.lifecycle, nil)
		if err != nil {
			t.Fatalf("NewMeditation: %v", err)
		}
		within, err := engine.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate(within bound): %v", err)
		}
		if within.Triggered || within.Artifact != nil || len(within.Evicted) != 0 {
			t.Fatalf("within-bound pass = triggered %v artifact %v evicted %v, want no curation at all", within.Triggered, within.Artifact, within.Evicted)
		}
		for _, card := range cards {
			if within.Fates[card.Key] != meditation.MeditationRetain {
				t.Fatalf("within-bound fate of %s = %v, want retain", card.Key, within.Fates[card.Key])
			}
		}

		over, err := meditation.NewMeditation(meditation.MeditationPolicy{MaxCards: 3, Keep: 2, MinScore: 6}, stack.retention, stack.lifecycle, nil)
		if err != nil {
			t.Fatalf("NewMeditation(over bound): %v", err)
		}
		first, err := over.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate(first): %v", err)
		}
		second, err := over.Curate(ctx, now, cards)
		if err != nil {
			t.Fatalf("Curate(second): %v", err)
		}
		if !reflect.DeepEqual(first.Fates, second.Fates) {
			t.Fatalf("two identical passes disagree on fates: %v vs %v, want the deterministic verdict", first.Fates, second.Fates)
		}
		if first.Artifact.Key != second.Artifact.Key || first.Artifact.Summary != second.Artifact.Summary {
			t.Fatalf("two identical passes disagree on the artifact: %v vs %v, want one content-addressed identity", first.Artifact, second.Artifact)
		}
		for _, card := range cards {
			left := meditation.AssessValue(card, now)
			right := meditation.AssessValue(card, now)
			if left != right {
				t.Fatalf("AssessValue(%s) = %+v then %+v, want a pure deterministic assessment", card.Key, left, right)
			}
		}
	})
}

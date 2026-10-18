package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// curationNow is the pass time every curation invariant judges age against.
var curationNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// TestCurationPassInvariants pins one pass: each card takes one fate, kept tickets keep their first stamp, and the artifact is a new reference over its refs.
// - a sequence within the bound retains every card and produces no artifact
// - every card of a triggered sequence carries exactly one of retain, evict or protect
// - only the oldest cards beyond the bound are judged for eviction, and each evicted card was tombstoned through the lifecycle
// - a card another holder protects is never evicted, even below the value floor
// - each kept ticket reads back under its first registration time and keeps its frozen type
// - a re-run never re-stamps a registered ticket with the newer time the card carries
// - the artifact identity is content-addressed over its refs, so the same condensation mints the same identity
// - the card sequence handed in is left byte for byte as it arrived
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestCurationPassInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a sequence within the bound retains every card and produces no artifact", func(t *testing.T) {
		curation, _, store := newCurationUnderTest(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		report, err := curation.Curate(ctx, curationNow, curationCards()[:2])
		if err != nil {
			t.Fatalf("curate a sequence within the bound: %v", err)
		}
		if report.Triggered || report.Artifact != nil {
			t.Fatalf("an untriggered pass = (triggered %v, artifact %v), want no curation at all", report.Triggered, report.Artifact)
		}
		if len(report.Fates) != 2 {
			t.Fatalf("fates of an untriggered sequence = %v, want one fate per card", report.Fates)
		}
		for key, fate := range report.Fates {
			if fate != CurationRetain {
				t.Fatalf("fate of %q = %q, want retain", key, fate)
			}
		}
		if len(store.tombstones) != 0 {
			t.Fatalf("tombstones of an untriggered pass = %v, want none", store.tombstones)
		}
	})

	t.Run("every card of a triggered sequence carries exactly one fate", func(t *testing.T) {
		curation, retention, store := newCurationUnderTest(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		holdTicket(t, ctx, retention, "aa02")
		report, err := curation.Curate(ctx, curationNow, curationCards())
		if err != nil {
			t.Fatalf("curate an over-limit sequence: %v", err)
		}
		if !report.Triggered || report.Artifact == nil {
			t.Fatalf("an over-limit pass = (triggered %v, artifact %v), want a condensation", report.Triggered, report.Artifact)
		}
		if len(report.Fates) != 6 {
			t.Fatalf("fates = %v, want one fate for each of the six cards", report.Fates)
		}
		for key, fate := range report.Fates {
			switch fate {
			case CurationRetain, CurationEvict, CurationProtect:
			default:
				t.Fatalf("fate of %q = %q, want one of the three channels", key, fate)
			}
		}
		if got := report.Fates["aa05"]; got != CurationRetain {
			t.Fatalf("fate of a card inside the sequence bound = %q, want retain", got)
		}
		if got := report.Fates["aa02"]; got != CurationProtect {
			t.Fatalf("fate of a card another holder protects = %q, want protect", got)
		}
		if len(report.Evicted) != 1 || report.Evicted[0] != "aa01" {
			t.Fatalf("evicted = %v, want only the unprotected oldest card", report.Evicted)
		}
		if _, marked := store.Tombstoned("aa01"); !marked {
			t.Fatal("the evicted card was not tombstoned through the lifecycle")
		}
		for _, key := range []event.EventID{"aa02", "aa03", "aa04"} {
			if _, marked := store.Tombstoned(key); marked {
				t.Fatalf("a protected or kept card %q was tombstoned", key)
			}
		}
		if len(store.tombstones) != 1 {
			t.Fatalf("tombstones = %v, want exactly the evicted card", store.tombstones)
		}
	})

	t.Run("each kept ticket reads back under its first registration", func(t *testing.T) {
		curation, retention, _ := newCurationUnderTest(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		holdTicket(t, ctx, retention, "aa02")
		report, err := curation.Curate(ctx, curationNow, curationCards())
		if err != nil {
			t.Fatalf("curate an over-limit sequence: %v", err)
		}
		artifact := report.Artifact
		if artifact == nil {
			t.Fatal("the over-limit pass produced no artifact")
		}
		if len(artifact.Refs) != 2 || artifact.Refs[0] != "aa03" || artifact.Refs[1] != "aa04" {
			t.Fatalf("artifact refs = %v, want the kept tickets in value order", artifact.Refs)
		}
		if artifact.Key != curationArtifactKey(artifact.Refs) {
			t.Fatalf("artifact key = %q, want the content-addressed identity of its refs", artifact.Key)
		}
		if artifact.Type != CurationArtifactType || artifact.Sunk != 0 {
			t.Fatalf("artifact = %+v, want a whole condensed card with nothing sunk", artifact)
		}
		for _, ref := range artifact.Refs {
			held, err := retention.Held(ctx, ref)
			if err != nil || !held {
				t.Fatalf("reference %q = (held %v, %v), want it held against eviction", ref, held, err)
			}
			record, registered := retention.Record(ref)
			if !registered {
				t.Fatalf("reference %q carries no registration", ref)
			}
			want := cardByKey(t, ref)
			if !record.RecordedAt.Equal(want.RecordedAt) || record.Type != want.Type {
				t.Fatalf("registration of %q = (%s, %s), want the recorded time and frozen type of the ticket", ref, record.RecordedAt, record.Type)
			}
		}
		if held, err := retention.Held(ctx, "aa01"); err != nil || held {
			t.Fatalf("evicted ticket held = (held %v, %v), want it unprotected", held, err)
		}
	})

	t.Run("a re-run never re-stamps a registered ticket", func(t *testing.T) {
		curation, retention, _ := newCurationUnderTest(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		holdTicket(t, ctx, retention, "aa02")
		if _, err := curation.Curate(ctx, curationNow, curationCards()); err != nil {
			t.Fatalf("first curation pass: %v", err)
		}
		restamped := curationCards()
		restamped[2].RecordedAt = curationNow.Add(-time.Minute)
		if _, err := curation.Curate(ctx, curationNow, restamped); err != nil {
			t.Fatalf("second curation pass: %v", err)
		}
		record, registered := retention.Record("aa03")
		if !registered {
			t.Fatal("the kept ticket lost its registration")
		}
		if !record.RecordedAt.Equal(cardByKey(t, "aa03").RecordedAt) {
			t.Fatalf("registered time after a re-run = %s, want the first registration time", record.RecordedAt)
		}
	})

	t.Run("the same condensation mints the same artifact identity", func(t *testing.T) {
		left := curationArtifactKey([]event.EventID{"aa03", "aa04"})
		right := curationArtifactKey([]event.EventID{"aa04", "aa03"})
		if left != right {
			t.Fatalf("artifact identities %q and %q differ for the same refs", left, right)
		}
		curation, retention, _ := newCurationUnderTest(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		holdTicket(t, ctx, retention, "aa02")
		first, err := curation.Curate(ctx, curationNow, curationCards())
		if err != nil {
			t.Fatalf("first curation pass: %v", err)
		}
		second, err := curation.Curate(ctx, curationNow, curationCards())
		if err != nil {
			t.Fatalf("second curation pass: %v", err)
		}
		if first.Artifact == nil || second.Artifact == nil || first.Artifact.Key != second.Artifact.Key {
			t.Fatalf("artifact identities of two identical passes = (%v, %v), want one identity", first.Artifact, second.Artifact)
		}
	})

	t.Run("the card sequence handed in is left as it arrived", func(t *testing.T) {
		curation, retention, _ := newCurationUnderTest(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		holdTicket(t, ctx, retention, "aa02")
		cards := curationCards()
		before := curationCards()
		if _, err := curation.Curate(ctx, curationNow, cards); err != nil {
			t.Fatalf("curate an over-limit sequence: %v", err)
		}
		if len(cards) != len(before) {
			t.Fatalf("cards after curation = %d, want the %d handed in", len(cards), len(before))
		}
		for i := range cards {
			if cards[i].Key != before[i].Key || cards[i].Type != before[i].Type ||
				cards[i].Summary != before[i].Summary || cards[i].Links != before[i].Links ||
				!cards[i].RecordedAt.Equal(before[i].RecordedAt) {
				t.Fatalf("card %d after curation = %+v, want %+v", i, cards[i], before[i])
			}
		}
	})
}

// TestCurationFallbackInvariants pins a failing condensation: the overflow run becomes a count line, its holdings stay held, and the pass reports no error.
// - a failing condensation sinks the whole overflow run into the count line
// - the failure is recorded in the sink account and the pass still returns no error
// - the references the failed pass keeps stay held against eviction
// - an artifact with sunk lines reports the count it collapsed instead of an intact summary
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestCurationFallbackInvariants(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("condenser endpoint unreachable")
	lifecycleStore := NewMemoryLifecycleStore(LifecycleCapabilities{})
	retention := NewRetention(NewMemoryGuard())
	lifecycle, err := NewLifecycle(LifecycleConfig{GlobalTTLDays: -1}, retention, lifecycleStore, LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	curation, err := NewCuration(CurationPolicy{MaxCards: 1, MinScore: 10}, retention, lifecycle,
		func(context.Context, CurationRun, time.Time) (string, error) { return "", failure })
	if err != nil {
		t.Fatalf("open the curation engine: %v", err)
	}
	cards := []CurationCard{
		{Key: "bb01", Type: "tool.result", Summary: "probe one", RecordedAt: curationNow.Add(-72 * time.Hour)},
		{Key: "bb02", Type: "agent.output", Summary: "probe two", Links: 2, RecordedAt: curationNow.Add(-60 * time.Hour)},
		{Key: "bb03", Type: "external.input", Summary: "probe three", RecordedAt: curationNow.Add(-time.Hour)},
	}
	report, err := curation.Curate(ctx, curationNow, cards)
	if err != nil {
		t.Fatalf("curate under a failing condensation: %v", err)
	}
	artifact := report.Artifact
	if artifact == nil {
		t.Fatal("the degraded pass produced no artifact")
	}
	if artifact.Sunk != 2 || artifact.Summary != "(earlier 2 items)" {
		t.Fatalf("degraded artifact = (sunk %d, summary %q), want the whole overflow run sunk into its count line", artifact.Sunk, artifact.Summary)
	}
	if len(report.Failures) != 1 || !errors.Is(report.Failures[0], failure) {
		t.Fatalf("sink account = %v, want the condensation failure recorded", report.Failures)
	}
	if len(artifact.Refs) != 1 || artifact.Refs[0] != "bb02" {
		t.Fatalf("degraded artifact refs = %v, want the value-floor card kept", artifact.Refs)
	}
	held, err := retention.Held(ctx, "bb02")
	if err != nil || !held {
		t.Fatalf("kept reference under a degraded pass = (held %v, %v), want it still protected", held, err)
	}
	if report.Fates["bb01"] != CurationEvict || report.Fates["bb03"] != CurationRetain {
		t.Fatalf("fates under a degraded pass = %v, want the unprotected overflow evicted and the window retained", report.Fates)
	}
}

// TestCurationRefusesAmbiguousCards pins the refusal of a card without a ticket or a sequence repeating one, leaving no fate, artifact or tombstone behind.
// - a card without an event key is refused by name
// - a sequence repeating one ticket is refused by name
// - a refused sequence leaves no fate, no artifact and no tombstone behind
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestCurationRefusesAmbiguousCards(t *testing.T) {
	ctx := context.Background()
	curation, _, store := newCurationUnderTest(t, CurationPolicy{MaxCards: 1, MinScore: 10})
	for name, cards := range map[string][]CurationCard{
		"card without a ticket": {{Key: "", RecordedAt: curationNow}},
		"sequence repeating one ticket": {
			{Key: "cc01", RecordedAt: curationNow.Add(-48 * time.Hour)},
			{Key: "cc01", RecordedAt: curationNow.Add(-time.Hour)},
		},
	} {
		report, err := curation.Curate(ctx, curationNow, cards)
		if !errors.Is(err, ErrCurationCardKey) {
			t.Fatalf("%s = %v, want ErrCurationCardKey", name, err)
		}
		if report.Triggered || report.Artifact != nil || len(report.Fates) != 0 {
			t.Fatalf("%s left a report behind: %+v", name, report)
		}
	}
	if len(store.tombstones) != 0 {
		t.Fatalf("tombstones after refused sequences = %v, want none", store.tombstones)
	}
}

// newCurationUnderTest opens a curation engine over an in-memory lifecycle whose
// TTL window is exempt, so only the curation pass files fates.
func newCurationUnderTest(t *testing.T, policy CurationPolicy) (*Curation, *Retention, *MemoryLifecycleStore) {
	t.Helper()
	store := NewMemoryLifecycleStore(LifecycleCapabilities{})
	retention := NewRetention(NewMemoryGuard())
	lifecycle, err := NewLifecycle(LifecycleConfig{GlobalTTLDays: -1}, retention, store, LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	curation, err := NewCuration(policy, retention, lifecycle, nil)
	if err != nil {
		t.Fatalf("open the curation engine: %v", err)
	}
	return curation, retention, store
}

// holdTicket registers one standing hold on a ticket under a holder beside the
// curation pass, as a recovery resource does.
func holdTicket(t *testing.T, ctx context.Context, retention *Retention, key event.EventID) {
	t.Helper()
	if _, err := retention.Register(ctx, key, "recovery:inventory", RetentionRecord{
		Kind:       RetentionRecovery,
		Type:       cardByKey(t, key).Type,
		RecordedAt: cardByKey(t, key).RecordedAt,
	}); err != nil {
		t.Fatalf("hold ticket %q: %v", key, err)
	}
}

// curationCards returns the six-card sequence the curation invariants fold: two
// oldest cards below the value floor, two value cards beyond the bound, and a
// two-card window inside it.
func curationCards() []CurationCard {
	return []CurationCard{
		{Key: "aa01", Type: "tool.result", Summary: "oldest probe", RecordedAt: curationNow.Add(-72 * time.Hour)},
		{Key: "aa02", Type: "agent.output", Summary: "held probe", RecordedAt: curationNow.Add(-60 * time.Hour)},
		{Key: "aa03", Type: "agent.output", Summary: "linked probe", Links: 2, RecordedAt: curationNow.Add(-48 * time.Hour)},
		{Key: "aa04", Type: "external.input", Summary: "skeleton probe", Links: 1, RecordedAt: curationNow.Add(-36 * time.Hour)},
		{Key: "aa05", Type: "external.input", Summary: "recent probe", RecordedAt: curationNow.Add(-2 * time.Hour)},
		{Key: "aa06", Type: "tool.result", Summary: "newest probe", RecordedAt: curationNow.Add(-time.Hour)},
	}
}

// cardByKey returns the card of the invariant sequence one ticket names.
func cardByKey(t *testing.T, key event.EventID) CurationCard {
	t.Helper()
	for _, card := range curationCards() {
		if card.Key == key {
			return card
		}
	}
	t.Fatalf("card %q is not part of the invariant sequence", key)
	return CurationCard{}
}

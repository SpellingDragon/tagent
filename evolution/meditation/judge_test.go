// 契约: docs/FACT-VOCABULARY.md#families
//
// TestBoundJudgmentFilesARequest pins the meditation producer side of the
// compress request family: one over-limit card sequence files exactly one
// request, and a sequence within the bound files none.
// - a sequence within the bound files no request and an over-limit one files exactly one
// - the filed request names the meditation producer and the bound reason at the judgment moment
// - the sequence identity follows the material it judges
// - a sequence no fate could be filed over is refused by name and files nothing
package meditation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

func TestBoundJudgmentFilesARequest(t *testing.T) {
	ctx := context.Background()

	t.Run("the bound judgment files a request only over the bound", func(t *testing.T) {
		meditation, log := newJudgingMeditation(t, MeditationPolicy{MaxCards: 2, MinScore: 10})
		if request, filed, err := meditation.Judge(ctx, meditationNow, meditationCards()[:2]); err != nil || filed || request.ID != "" {
			t.Fatalf("judgment of a sequence within the bound = (%+v, %v, %v), want no request at all", request, filed, err)
		}
		request, filed, err := meditation.Judge(ctx, meditationNow, meditationCards())
		if err != nil || !filed {
			t.Fatalf("judgment of an over-limit sequence = (%+v, %v, %v), want the request it files", request, filed, err)
		}
		if request.Producer != memory.CompressProducerMeditation || request.Reason != memory.CompressReasonMeditation {
			t.Fatalf("request = %+v, want the meditation producer and the bound reason", request)
		}
		if !request.At.Equal(meditationNow) {
			t.Fatalf("request filing moment = %s, want the judgment moment %s", request.At, meditationNow)
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 1 || pending[0].ID != request.ID {
			t.Fatalf("pending after the bound judgment = (%v, %v), want exactly the filed request", pending, err)
		}
	})

	t.Run("the sequence identity follows the material it judges", func(t *testing.T) {
		meditation, _ := newJudgingMeditation(t, MeditationPolicy{MaxCards: 2, MinScore: 10})
		left, _, err := meditation.Judge(ctx, meditationNow, meditationCards())
		if err != nil {
			t.Fatalf("judge the sequence: %v", err)
		}
		changed := meditationCards()
		changed[0].Key = "aa07"
		right, _, err := meditation.Judge(ctx, meditationNow, changed)
		if err != nil {
			t.Fatalf("judge the changed sequence: %v", err)
		}
		if left.Subject == right.Subject {
			t.Fatalf("subjects of two different sequences = %q, want one identity per body of material", left.Subject)
		}
		if left.Subject != string(meditationSequenceKey(meditationCards())) {
			t.Fatalf("subject = %q, want the content-addressed identity of the sequence", left.Subject)
		}
	})

	t.Run("a sequence no fate could be filed over files no request", func(t *testing.T) {
		meditation, log := newJudgingMeditation(t, MeditationPolicy{MaxCards: 1, MinScore: 10})
		repeated := []MeditationCard{
			{Key: "cc01", RecordedAt: meditationNow.Add(-48 * time.Hour)},
			{Key: "cc01", RecordedAt: meditationNow.Add(-time.Hour)},
		}
		if _, _, err := meditation.Judge(ctx, meditationNow, repeated); !errors.Is(err, ErrMeditationCardKey) {
			t.Fatalf("judgment of a sequence repeating one ticket = %v, want %v", err, ErrMeditationCardKey)
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending after a refused judgment = (%v, %v), want none", pending, err)
		}
	})
}

// newJudgingMeditation opens a meditation engine whose bound judgment files into
// a request log, over an in-memory lifecycle whose TTL window is exempt.
func newJudgingMeditation(t *testing.T, policy MeditationPolicy) (*Meditation, *memory.CompressRequests) {
	t.Helper()
	store := ledger.NewMemoryLifecycleStore(ledger.LifecycleCapabilities{})
	retention := ledger.NewRetention(ledger.NewMemoryGuard())
	lifecycle, err := ledger.NewLifecycle(ledger.LifecycleConfig{GlobalTTLDays: -1}, retention, store, ledger.LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	requests := memory.NewCompressRequests()
	meditation, err := NewMeditation(policy, retention, lifecycle, nil, WithCompressRequests(requests))
	if err != nil {
		t.Fatalf("open the meditation engine: %v", err)
	}
	return meditation, requests
}

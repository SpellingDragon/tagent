package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// TestCompressionRequestIsAFact pins the compress request family: who judged, why, what, and one request kept on record per judgment.
// - a request answers its content-addressed identity as its fact key and the frozen compress.request type
// - the identity is derived from producer, subject and reason, so a different judgment answers a different key
// - a judgment naming no producer, subject or reason is refused by name
// - a repeated filing answers the recorded request and keeps its first filing moment
// - a sequence within the bound files no request and an over-limit one files exactly one
// - the sequence identity follows the material it judges
// - a retirement of a request that does not await consumption is refused by name
//
// 契约: docs/FACT-VOCABULARY.md#families
func TestCompressionRequestIsAFact(t *testing.T) {
	ctx := context.Background()

	t.Run("a request answers its identity and its frozen type", func(t *testing.T) {
		request := CompressRequestFor(CompressProducerCuration, "seq-1", CompressReasonCuration)
		var fact event.Fact = request
		if got := fact.FactType(); got != "compress.request" {
			t.Fatalf("FactType() = %q, want the frozen compress.request type", got)
		}
		if got, want := fact.FactKey(), string(request.ID); got != want {
			t.Fatalf("FactKey() = %q, want the request identity %q", got, want)
		}
		if request.ID == "" {
			t.Fatal("the judgment carries no identity, want a content-addressed key")
		}
		if request.Producer != CompressProducerCuration || request.Reason != CompressReasonCuration || request.Subject != "seq-1" {
			t.Fatalf("request = %+v, want the producer, the reason and the judged subject", request)
		}
	})

	t.Run("the identity follows the judgment it names", func(t *testing.T) {
		base := CompressRequestFor(CompressProducerCuration, "seq-1", CompressReasonCuration)
		if repeat := CompressRequestFor(CompressProducerCuration, "seq-1", CompressReasonCuration); repeat.ID != base.ID {
			t.Fatalf("identities of one judgment = (%q, %q), want one key", base.ID, repeat.ID)
		}
		for name, other := range map[string]CompressRequest{
			"another producer": CompressRequestFor("workspace.tool-overflow", "seq-1", CompressReasonCuration),
			"another subject":  CompressRequestFor(CompressProducerCuration, "seq-2", CompressReasonCuration),
			"another reason":   CompressRequestFor(CompressProducerCuration, "seq-1", "another ground"),
		} {
			if other.ID == base.ID {
				t.Fatalf("%s answers the identity of the base judgment %q, want its own key", name, base.ID)
			}
		}
	})

	t.Run("a judgment naming nothing is refused by name", func(t *testing.T) {
		log := NewCompressRequests()
		for name, request := range map[string]CompressRequest{
			"no producer": {Subject: "seq-1", Reason: "a ground"},
			"no subject":  {Producer: CompressProducerCuration, Reason: "a ground"},
			"no reason":   {Producer: CompressProducerCuration, Subject: "seq-1"},
		} {
			if _, err := log.Produce(ctx, request); !errors.Is(err, ErrCompressRequestUnnamed) {
				t.Fatalf("%s = %v, want %v", name, err, ErrCompressRequestUnnamed)
			}
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending after refused judgments = (%v, %v), want none", pending, err)
		}
	})

	t.Run("a repeated filing keeps the recorded request", func(t *testing.T) {
		log := NewCompressRequests()
		first, err := log.Produce(ctx, CompressRequestFor(CompressProducerCuration, "seq-1", CompressReasonCuration))
		if err != nil {
			t.Fatalf("file the first judgment: %v", err)
		}
		if first.At.IsZero() {
			t.Fatal("the filed request carries no filing moment, want the moment it was filed")
		}
		repeat := CompressRequestFor(CompressProducerCuration, "seq-1", CompressReasonCuration)
		repeat.At = curationNow
		second, err := log.Produce(ctx, repeat)
		if err != nil {
			t.Fatalf("file the repeated judgment: %v", err)
		}
		if second.ID != first.ID || !second.At.Equal(first.At) {
			t.Fatalf("repeated filing = (%q, %s), want the recorded (%q, %s): one judgment keeps one filing", second.ID, second.At, first.ID, first.At)
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending after a repeated filing = (%v, %v), want the one request", pending, err)
		}
	})

	t.Run("the bound judgment files a request only over the bound", func(t *testing.T) {
		curation, log := newJudgingCuration(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		if request, filed, err := curation.Judge(ctx, curationNow, curationCards()[:2]); err != nil || filed || request.ID != "" {
			t.Fatalf("judgment of a sequence within the bound = (%+v, %v, %v), want no request at all", request, filed, err)
		}
		request, filed, err := curation.Judge(ctx, curationNow, curationCards())
		if err != nil || !filed {
			t.Fatalf("judgment of an over-limit sequence = (%+v, %v, %v), want the request it files", request, filed, err)
		}
		if request.Producer != CompressProducerCuration || request.Reason != CompressReasonCuration {
			t.Fatalf("request = %+v, want the curation producer and the bound reason", request)
		}
		if !request.At.Equal(curationNow) {
			t.Fatalf("request filing moment = %s, want the judgment moment %s", request.At, curationNow)
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 1 || pending[0].ID != request.ID {
			t.Fatalf("pending after the bound judgment = (%v, %v), want exactly the filed request", pending, err)
		}
	})

	t.Run("the sequence identity follows the material it judges", func(t *testing.T) {
		curation, _ := newJudgingCuration(t, CurationPolicy{MaxCards: 2, MinScore: 10})
		left, _, err := curation.Judge(ctx, curationNow, curationCards())
		if err != nil {
			t.Fatalf("judge the sequence: %v", err)
		}
		changed := curationCards()
		changed[0].Key = "aa07"
		right, _, err := curation.Judge(ctx, curationNow, changed)
		if err != nil {
			t.Fatalf("judge the changed sequence: %v", err)
		}
		if left.Subject == right.Subject {
			t.Fatalf("subjects of two different sequences = %q, want one identity per body of material", left.Subject)
		}
		if left.Subject != string(curationSequenceKey(curationCards())) {
			t.Fatalf("subject = %q, want the content-addressed identity of the sequence", left.Subject)
		}
	})

	t.Run("a sequence no fate could be filed over files no request", func(t *testing.T) {
		curation, log := newJudgingCuration(t, CurationPolicy{MaxCards: 1, MinScore: 10})
		repeated := []CurationCard{
			{Key: "cc01", RecordedAt: curationNow.Add(-48 * time.Hour)},
			{Key: "cc01", RecordedAt: curationNow.Add(-time.Hour)},
		}
		if _, _, err := curation.Judge(ctx, curationNow, repeated); !errors.Is(err, ErrCurationCardKey) {
			t.Fatalf("judgment of a sequence repeating one ticket = %v, want %v", err, ErrCurationCardKey)
		}
		pending, err := log.Pending(ctx)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending after a refused judgment = (%v, %v), want none", pending, err)
		}
	})

	t.Run("a retirement of a request that does not await consumption is refused", func(t *testing.T) {
		log := NewCompressRequests()
		request, err := log.Produce(ctx, CompressRequestFor(CompressProducerCuration, "seq-1", CompressReasonCuration))
		if err != nil {
			t.Fatalf("file the judgment: %v", err)
		}
		if err := log.Consume(ctx, request.ID); err != nil {
			t.Fatalf("retire the pending request: %v", err)
		}
		if err := log.Consume(ctx, request.ID); !errors.Is(err, ErrCompressRequestNotPending) {
			t.Fatalf("repeated retirement = %v, want %v", err, ErrCompressRequestNotPending)
		}
	})
}

// newJudgingCuration opens a curation engine whose bound judgment files into a
// request log, over an in-memory lifecycle whose TTL window is exempt.
func newJudgingCuration(t *testing.T, policy CurationPolicy) (*Curation, *CompressRequests) {
	t.Helper()
	store := NewMemoryLifecycleStore(LifecycleCapabilities{})
	retention := NewRetention(NewMemoryGuard())
	lifecycle, err := NewLifecycle(LifecycleConfig{GlobalTTLDays: -1}, retention, store, LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()
	requests := NewCompressRequests()
	curation, err := NewCuration(policy, retention, lifecycle, nil, WithCompressRequests(requests))
	if err != nil {
		t.Fatalf("open the curation engine: %v", err)
	}
	return curation, requests
}

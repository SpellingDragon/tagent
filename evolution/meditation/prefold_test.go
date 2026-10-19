package meditation

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
)

// TestFoldIsPrecomputedNotInline pins the fold to consumption: the tick folds and files the fold fact, the assembly only reads.
// - the consuming tick folds the projection once and commits one compaction fact into the state
// - the folded projection presents its compaction line first, readable at the request boundary
// - the assembly leaves the fact chain and the projection entries exactly as they stood
// - a folded projection answers a second pre-fold with the payload already filed
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestFoldIsPrecomputedNotInline(t *testing.T) {
	ctx := context.Background()
	session := replay.ChainKey{PartitionID: 1, SessionID: event.SessionID("s-fold")}
	at := func(step int) time.Time { return meditationNow.Add(time.Duration(step) * time.Minute) }

	store := ledger.NewMemoryLifecycleStore(ledger.LifecycleCapabilities{})
	retention := ledger.NewRetention(ledger.NewMemoryGuard())
	lifecycle, err := ledger.NewLifecycle(ledger.LifecycleConfig{GlobalTTLDays: -1}, retention, store, ledger.LifecycleCapabilities{})
	if err != nil {
		t.Fatalf("open the lifecycle: %v", err)
	}
	lifecycle.RecoveryComplete()

	projection := replay.NewReplayProjection()
	run := []replay.ProjectionEntry{
		{Key: "aa03", Type: event.TypeAgentOutput, Summary: "linked probe", Content: "第一段正文", Time: at(1)},
		{Key: "aa04", Type: event.TypeActionCommand, Summary: "tool call", Content: "grep -n TODO src", Time: at(2)},
		{Key: "aa05", Type: event.TypeAgentOutput, Summary: "recent probe", Content: "第二段正文", Time: at(3)},
		{Type: event.TypeActionCommand, Summary: "tool chain", Content: "- 工具链: grep → sed", Time: at(4)},
	}
	chain := replay.NewMemoryEventChain()
	for _, entry := range run {
		projection.Append(ctx, entry)
		if entry.Key == "" {
			continue
		}
		if err := chain.Commit(ctx, replay.ChainFact{Key: entry.Key, Chain: session, Type: entry.Type, Summary: entry.Summary, Content: entry.Content, Time: entry.Time}); err != nil {
			t.Fatalf("commit source fact %q: %v", entry.Key, err)
		}
	}

	requests := memory.NewCompressRequests()
	var folds []replay.CompactionFact
	meditation, err := NewMeditation(MeditationPolicy{MaxCards: 2, MinScore: 10}, retention, lifecycle, nil,
		WithCompressRequests(requests),
		WithMeditationCards(func(context.Context) ([]MeditationCard, error) { return meditationCards(), nil }),
		WithMeditationProducts(func(context.Context, MeditationArtifact) error { return nil }),
		WithMeditationPreFold(
			func(context.Context) (*replay.ReplayProjection, error) { return projection, nil },
			func(ctx context.Context, payload replay.CompactionFact) error {
				fact, err := CompactionChainFact(session, payload, meditationNow)
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
	if err != nil {
		t.Fatalf("open the meditation engine: %v", err)
	}
	if _, filed, err := meditation.Judge(ctx, meditationNow, meditationCards()); err != nil || !filed {
		t.Fatalf("judge the over-limit sequence = (filed %v, %v), want the request it files", filed, err)
	}
	tick, err := meditation.Tick(ctx, meditationNow)
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(tick.Failures) != 0 {
		t.Fatalf("tick failures = %v, want none", tick.Failures)
	}
	if len(tick.Folds) != 1 || len(folds) != 1 {
		t.Fatalf("fold facts = (tick %d, sink %d), want one pre-fold of the projection", len(tick.Folds), len(folds))
	}
	payload := projection.Compaction()
	if payload == nil || !reflect.DeepEqual(*payload, tick.Folds[0]) {
		t.Fatalf("compaction payload = %+v, want the payload the tick filed", payload)
	}
	if payload.FullBoundary != "aa05" {
		t.Fatalf("fold boundary = %q, want the last keyed entry of the folded run", payload.FullBoundary)
	}
	if _, held, err := chain.Fact(ctx, CompactionFactKey(tick.Folds[0])); err != nil || !held {
		t.Fatalf("fold fact = (held %v, %v), want the compaction fact committed into the state", held, err)
	}
	entries := projection.Entries()
	if len(entries) != 3 || entries[0].Type != replay.CompactionEventType || !strings.HasPrefix(entries[0].Content, "compaction: ") {
		t.Fatalf("folded entries = %+v, want the compaction line first", entries)
	}

	before := scanAll(t, ctx, chain)
	folded := make([]replay.ProjectionEntry, len(entries))
	copy(folded, entries)
	assembled, err := projection.AssembleBudget(ctx, 1000)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}
	after := scanAll(t, ctx, chain)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("fact chain after assembly = %+v, want it exactly as it stood", after)
	}
	if !reflect.DeepEqual(projection.Entries(), folded) {
		t.Fatalf("projection entries after assembly = %+v, want the folded result unchanged", projection.Entries())
	}
	if len(assembled) != len(entries) || assembled[0].Type != replay.CompactionEventType {
		t.Fatalf("assembled entries = %+v, want the folded projection read as it stands", assembled)
	}

	again, err := Prefold(ctx, projection)
	if err != nil {
		t.Fatalf("Prefold of a folded projection: %v", err)
	}
	if !reflect.DeepEqual(again, *payload) {
		t.Fatalf("second pre-fold = %+v, want the payload already filed", again)
	}
}

// scanAll reads every fact of one chain in write order, so a comparison sees the
// chain as it stands rather than one page of it.
func scanAll(t *testing.T, ctx context.Context, chain replay.EventChain) []replay.ChainFact {
	t.Helper()
	facts, err := chain.ScanFrom(ctx, "", 1024)
	if err != nil {
		t.Fatalf("scan the chain: %v", err)
	}
	return facts
}

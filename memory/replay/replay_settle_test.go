package replay

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/event"
)

// settleNow is the pass time the settle invariants judge age against.
var settleNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// TestSettleLandsAsReplaySettle pins the settled presentation as a fact: one settle fact per decision, none for a skipped entry.
// - a degraded presentation lands one replay-settle fact with its entry, ticket mode and preview
// - the identity is content-addressed over the decision, so one decision answers one identity
// - a repeated assembly follows the recorded decision and adds no second fact
// - a skipped entry records no decision and lands no fact
// - the fact chain behind the projection is unchanged by the assembly
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
func TestSettleLandsAsReplaySettle(t *testing.T) {
	ctx := context.Background()
	session := ChainKey{PartitionID: 1, SessionID: event.SessionID("s-settle")}
	chain := NewMemoryEventChain()
	projection := NewReplayProjection()
	large := ChainFact{
		Key:     "big-1",
		Chain:   session,
		Type:    event.TypeAgentOutput,
		Summary: "big result",
		Content: strings.Repeat("x", 1800),
		Time:    settleNow,
	}
	skipped := ChainFact{
		Key:     "skip-1",
		Chain:   session,
		Type:    event.TypeAgentOutput,
		Summary: strings.Repeat("s", 300),
		Content: strings.Repeat("y", 1800),
		Time:    settleNow.Add(time.Minute),
	}
	for _, fact := range []ChainFact{large, skipped} {
		if err := chain.Commit(ctx, fact); err != nil {
			t.Fatalf("commit %q: %v", fact.Key, err)
		}
		projection.Append(ctx, ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time})
	}
	before := scanAll(t, ctx, chain)

	assembled, err := projection.AssembleBudget(ctx, 60)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}
	if len(assembled) != 1 || assembled[0].Key != "big-1" {
		t.Fatalf("assembled entries = %+v, want the large entry presented and the other skipped", assembled)
	}
	decision, recorded := projection.Decision("big-1")
	if !recorded || decision.Mode != PresentationTicket {
		t.Fatalf("recorded decision = %+v (%v), want a ticket settlement", decision, recorded)
	}
	settles := projection.Settles()
	if len(settles) != 1 {
		t.Fatalf("settle facts = %+v, want exactly the settled entry", settles)
	}
	settle := settles[0]
	var fact event.Fact = settle
	if fact.FactType() != ReplaySettleType || fact.FactKey() != ReplaySettleFor(decision).FactKey() {
		t.Fatalf("settle fact = (%q, %q), want the decision identity read as %q", fact.FactKey(), fact.FactType(), ReplaySettleType)
	}
	if settle.Entry != "big-1" || settle.Mode != PresentationTicket || settle.Preview != decision.Preview || settle.At.IsZero() {
		t.Fatalf("settle fact = %+v, want the entry, mode, preview and moment of the decision", settle)
	}
	if settle.ID != ReplaySettleFor(decision).ID {
		t.Fatal("the landed settle fact and the decision it states answer different identities")
	}
	summary := ReplaySettleFor(PresentationDecision{Key: "big-1", Mode: PresentationSummary, Preview: decision.Preview})
	if summary.FactKey() == settle.FactKey() {
		t.Fatal("a summary settlement shares the identity of a ticket settlement")
	}
	if _, recorded := projection.Decision("skip-1"); recorded {
		t.Fatal("the skipped entry recorded a decision")
	}

	again, err := projection.AssembleBudget(ctx, 60)
	if err != nil {
		t.Fatalf("second AssembleBudget: %v", err)
	}
	if len(again) != 1 || again[0].Content != assembled[0].Content {
		t.Fatalf("second assembly = %+v, want the recorded settlement followed", again)
	}
	if len(projection.Settles()) != 1 {
		t.Fatalf("settle facts after the second assembly = %+v, want no second one", projection.Settles())
	}
	for _, recorded := range projection.Settles() {
		if recorded.Entry == "skip-1" {
			t.Fatalf("settle facts = %+v, want none for the skipped entry", projection.Settles())
		}
	}
	after := scanAll(t, ctx, chain)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("fact chain after assembly = %+v, want it exactly as it stood", after)
	}
}

// scanAll reads every fact of one chain in write order, so a comparison sees the
// chain as it stands rather than one page of it.
func scanAll(t *testing.T, ctx context.Context, chain EventChain) []ChainFact {
	t.Helper()
	facts, err := chain.ScanFrom(ctx, "", 1024)
	if err != nil {
		t.Fatalf("scan the chain: %v", err)
	}
	return facts
}

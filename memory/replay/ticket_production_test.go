// 契约: docs/GLOSSARY.md
//
// The ticket the projection writes: the middle-band fold states the event key of
// every entry it consumes, and the one ticket form the recall surface parses is
// written by TicketOf alone.
package replay

import (
	"context"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
)

// ticketsIn answers the tickets one product text carries, in the order they
// stand, and fails the read when a ticket is cut short: a half ticket is an
// address the model cannot hand back.
func ticketsIn(t *testing.T, text string) []event.EventID {
	t.Helper()
	var keys []event.EventID
	rest := text
	for {
		at := strings.Index(rest, "[evt_")
		if at < 0 {
			return keys
		}
		rest = rest[at+len("[evt_"):]
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			t.Fatalf("product text = %q, want every ticket closed by %q", text, "]")
		}
		token := rest[:end]
		key, typ, found := strings.Cut(token, "|")
		if !found || key == "" || typ == "" {
			t.Fatalf("product text = %q carries the ticket [evt_%s], want the event key beside its frozen type", text, token)
		}
		keys = append(keys, event.EventID(key))
		rest = rest[end:]
	}
}

// TestSegmentFoldCarriesTheTicketOfEveryFoldedEntry pins the middle-band ticket production.
//   - every line of a segment-fold product opens with that entry's provenance anchor
//   - the anchor is followed by the ticket of the entry the line stands for, key and type taken from the entry itself
//   - the ticket keys are exactly the folded entries' keys, in the order the fold consumed them
//   - the one-line read the line always carried stays behind the ticket
func TestSegmentFoldCarriesTheTicketOfEveryFoldedEntry(t *testing.T) {
	ctx := context.Background()
	entries := append(ageTurn("t1", 0, 2), ageTurn("t2", 1, 2)...)
	projection := ageProjection(t, entries)

	report, err := projection.FoldByAge(ctx, AgeFoldPlan{KeepRecent: 1, VolumeThreshold: 600})
	if err != nil {
		t.Fatalf("FoldByAge: %v", err)
	}
	if len(report.Products) != 1 {
		t.Fatalf("the fold minted %d product(s), want the one segment fold of the turn that left the window", len(report.Products))
	}
	product := report.Products[0]

	var folded []ProjectionEntry
	for _, entry := range entries {
		if strings.HasPrefix(string(entry.Key), "t1-") && isProcessDetail(entry) {
			folded = append(folded, entry)
		}
	}
	if len(folded) == 0 {
		t.Fatal("the fold consumed no process detail, so the ticket production has nothing to state")
	}
	lines := strings.Split(product.Content, "; ")
	if len(lines) != len(folded) {
		t.Fatalf("the product carries %d line(s), want the %d folded entries: %q", len(lines), len(folded), product.Content)
	}
	for i, entry := range folded {
		anchor := provenancePrefixOf(entry)
		if !strings.HasPrefix(lines[i], anchor) {
			t.Fatalf("line %d = %q, want it to open with the provenance anchor %q", i, lines[i], anchor)
		}
		want := anchor + "[evt_" + string(entry.Key) + "|" + entry.Type + "] " + entry.Type + ": " + entry.Summary
		if lines[i] != want {
			t.Fatalf("line %d = %q, want the ticket of the folded entry beside its one-line read %q", i, lines[i], want)
		}
	}

	want := make([]event.EventID, 0, len(folded))
	for _, entry := range folded {
		want = append(want, entry.Key)
	}
	got := ticketsIn(t, product.Content)
	if len(got) != len(want) {
		t.Fatalf("the product carries %d ticket(s) %v, want the %d folded keys %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ticket %d = %s, want the folded key %s: the ticket keys are the folded entries' keys in fold order (%v vs %v)", i, got[i], want[i], got, want)
		}
	}
}

// TestSegmentFoldNamesNoSyntheticKey pins the negative side of the ticket production.
//   - a product of an earlier fold is never folded a second time, so no line states a synthetic key
//   - an entry carrying no fact key yields its one-line read with no ticket: an empty address is not written
//   - the product text carries no [evt_-seg-…] or [evt_-hist-…] ticket, so no line promises a read that answers missing
func TestSegmentFoldNamesNoSyntheticKey(t *testing.T) {
	at := ageBase
	entries := []ProjectionEntry{
		{Key: "e-1-plan", Type: event.TypeThinkingPlan, Summary: "plan e1", Content: "the plan", Time: at},
		{Key: "", Type: event.TypeActionCommand, Summary: "bare", Content: "the bare call", Time: at.Add(1)},
		{Key: "-seg-abc123", Type: event.TypeActionCommand, Summary: "an earlier product", Content: "the folded lines", Time: at.Add(2)},
	}
	folded, product := foldProcessDetail(entries, true)
	if product == nil {
		t.Fatal("the fold minted no product over the process detail it read")
	}
	if folded != 2 {
		t.Fatalf("the fold consumed %d entr(ies), want the two carrying a fact key beside the bare entry: a synthetic key names a product and is never folded again", folded)
	}
	lines := strings.Split(product.Content, "; ")
	if len(lines) != 2 {
		t.Fatalf("the product carries %d line(s) %q, want one line per folded entry", len(lines), product.Content)
	}
	if !strings.Contains(lines[0], "[evt_e-1-plan|"+event.TypeThinkingPlan+"]") {
		t.Fatalf("line 0 = %q, want the ticket of the entry's own fact key", lines[0])
	}
	if strings.Contains(lines[1], "[evt_") {
		t.Fatalf("line 1 = %q, want the one-line read of an entry carrying no fact key, with no ticket", lines[1])
	}
	for _, form := range []string{"[evt_-seg-", "[evt_-hist-"} {
		if strings.Contains(product.Content, form) {
			t.Fatalf("product content = %q carries the synthetic-key ticket %q: a fold product is not an address", product.Content, form)
		}
	}
}

// TestTicketTextIsTheRecallTicketOfTheEntry pins the one ticket form.
//   - the ticket of a fact key is [evt_<key>|<type>], the form the recall surface parses
//   - the empty key and the synthetic key of a fold product answer no ticket: the address would answer missing
//   - the presented view the budget degrades to a shortened entry ends with the same ticket the fold writes
func TestTicketTextIsTheRecallTicketOfTheEntry(t *testing.T) {
	if got, want := TicketOf("02-1-plan", event.TypeActionCommand), "[evt_02-1-plan|action_command]"; got != want {
		t.Fatalf("TicketOf = %q, want %q: one form, written by one producer", got, want)
	}
	for _, key := range []event.EventID{"", "-seg-abc123", "-hist-abc123", "-recovery"} {
		if got := TicketOf(key, event.TypeActionCommand); got != "" {
			t.Fatalf("TicketOf(%q) = %q, want no ticket: a key naming no committed fact is an address that answers missing", key, got)
		}
	}

	entry := ProjectionEntry{Key: "e-9", Type: event.TypeThinkingPlan, Summary: "plan", Content: strings.Repeat("过程细节。", 40), Time: ageBase}
	view, ok := presentWithin(entry, entry.Content, 500)
	if !ok {
		t.Fatal("the budget presented nothing for an entry it had room to shorten")
	}
	if !strings.HasSuffix(view.Content, "\n"+TicketOf(entry.Key, entry.Type)) {
		t.Fatalf("the shortened view = %q, want it to end with the ticket the fold writes for the same entry", view.Content)
	}
}

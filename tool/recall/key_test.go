// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
//
// The two shapes parseKey must refuse to turn into a recallable key: the ticket
// form the fold wrote before the event key became the address, and a whole
// presented line carrying a provenance anchor but no ticket. Both answer missing
// rather than guessing a key out of the material.
package recall

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// keyReadFixture wires one exact read over a table holding the facts a caller may
// reach: the named keys are readable, so a shape that resolves to one of them is
// served and a shape that resolves to nothing answers missing.
func keyReadFixture(t *testing.T, keys ...event.EventID) *getTool {
	t.Helper()
	ctx := context.Background()
	tier, err := ledger.OpenTier(ctx, ledger.StorageConfig{Type: ledger.StorageMemory})
	if err != nil {
		t.Fatalf("open the memory tier: %v", err)
	}
	t.Cleanup(func() { tier.Segments.Close() })
	originals := tier.Originals
	scoped, ok := originals.(ledger.ScopedOriginalStore)
	if !ok {
		t.Fatalf("the memory tier holds %T, want the scoped original read face", originals)
	}
	events := map[event.EventID]memory.EventReference{}
	refs := map[event.EventID]ledger.OriginalRef{}
	partition := memory.PartitionIDFromName("agent-a")
	for _, key := range keys {
		ref := ledger.OriginalRef{Key: key, Format: "text"}
		if err := originals.Put(ctx, ref, []byte("the stored text of "+string(key)+"\n")); err != nil {
			t.Fatalf("commit the original of %s: %v", key, err)
		}
		refs[key] = ref
		events[key] = memory.EventReference{Key: key, PartitionID: partition, Type: event.TypeActionCommand, Summary: "call"}
	}
	return &getTool{base: newBase(Reads{
		Table:     tableStub{events: events, refs: refs},
		Originals: scoped,
		Trace:     traceStub{},
	}, []string{"agent-a"})}
}

// keyReadAnswer runs one call through the exact read and answers what it said.
func keyReadAnswer(t *testing.T, tool *getTool, arg string) getAnswer {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"key": arg})
	if err != nil {
		t.Fatalf("encode the call: %v", err)
	}
	answered, err := tool.Call(context.Background(), raw)
	if err != nil {
		t.Fatalf("the read of %q refused with %v, want the named reason", arg, err)
	}
	answer := getAnswer{}
	if err := json.Unmarshal([]byte(answered.(string)), &answer); err != nil {
		t.Fatalf("decode the answer of %q: %v", arg, err)
	}
	return answer
}

// TestParseKeyRefusesTheLegacyTicketForm pins the ticket form the fold does not write.
//   - the bracketed [ticket <key>: full text retrievable by segment] argument answers missing, not the key it names
//   - the same key handed back in the ticket form the fold writes is served, so the refusal is the shape and not the fixture
func TestParseKeyRefusesTheLegacyTicketForm(t *testing.T) {
	tool := keyReadFixture(t, "seg-abc")
	legacy := "[ticket seg-abc: full text retrievable by segment]"
	answer := keyReadAnswer(t, tool, legacy)
	if answer.Found || answer.Reason != reasonMissing {
		t.Fatalf("the legacy ticket form answered found=%t reason=%q key=%s, want missing: the old form names no address the surface parses", answer.Found, answer.Reason, answer.Key)
	}
	if answer.Key == "seg-abc" {
		t.Fatalf("the legacy ticket form resolved to the held key %s, want the old form to reach no key at all", answer.Key)
	}
	served := keyReadAnswer(t, tool, "[evt_seg-abc|action_command]")
	if !served.Found || served.Body == "" {
		t.Fatalf("the ticket form the fold writes answered found=%t body=%q, want the stored text: the fixture holds the key", served.Found, served.Body)
	}
	if served.Key != "seg-abc" {
		t.Fatalf("the ticket form resolved to %s, want the key it names", served.Key)
	}
}

// TestParseKeyRefusesAnAnchorWithoutATicket pins the whole-line read that carries no ticket.
//   - a presented line carrying only the provenance anchor answers missing instead of treating the anchor's separator as a key
//   - the same line with the entry's ticket in it resolves through the ticket, so an anchor line is readable exactly when it carries one
func TestParseKeyRefusesAnAnchorWithoutATicket(t *testing.T) {
	tool := keyReadFixture(t, "e-1-cmd")
	bare := "[10-01 10:00|gen:2] action_command: call e1a"
	answer := keyReadAnswer(t, tool, bare)
	if answer.Found || answer.Reason != reasonMissing {
		t.Fatalf("the anchor-only line answered found=%t reason=%q key=%s, want missing: the line carries no ticket to read the key from", answer.Found, answer.Reason, answer.Key)
	}
	if answer.Key == "e-1-cmd" {
		t.Fatalf("the anchor-only line resolved to the held key %s, want no key guessed out of the anchor", answer.Key)
	}
	ticketed := "[10-01 10:00|gen:2] " + "[evt_e-1-cmd|action_command]" + " action_command: call e1a"
	served := keyReadAnswer(t, tool, ticketed)
	if !served.Found || served.Key != "e-1-cmd" {
		t.Fatalf("the ticketed line answered found=%t key=%s, want the key its ticket names: the whole-line read goes through the ticket", served.Found, served.Key)
	}
}

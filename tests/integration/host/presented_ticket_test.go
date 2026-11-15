// The presented-ticket checks of the host: a projected entry reaches the model
// with the ticket of the fact it was minted from in front of its material, so a
// check that reads the material of a request peels the ticket off, a check that
// knows the fact key asserts the ticket literally, and a check that requires the
// address to be answerable requires the key to name a fact the session committed.
package host

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/assembly"
)

// presentedTicket spells the ticket one entry reaches the model as: the recall
// prefix, the fact key and the frozen type. The form is written out here rather
// than asked of the production writer, so a change to the ticket's spelling
// fails these checks instead of moving along with them.
func presentedTicket(key event.EventID, typ string) string {
	return "[evt_" + string(key) + "|" + typ + "]"
}

// splitPresentedTicket requires one presented message to open with a well-formed
// ticket and answers the fact key the ticket names, the frozen type beside it and
// the material behind the ticket.
func splitPresentedTicket(t *testing.T, content string) (event.EventID, string, string) {
	t.Helper()
	if !strings.HasPrefix(content, "[evt_") {
		t.Fatalf("presented content = %q, want it to open with the ticket of the fact it presents", content)
	}
	at := strings.Index(content, "] ")
	if at < 0 {
		t.Fatalf("presented content = %q, want a ticket closed by %q", content, "] ")
	}
	token := content[len("[evt_"):at]
	bar := strings.IndexByte(token, '|')
	if bar <= 0 || bar == len(token)-1 {
		t.Fatalf("presented ticket [evt_%s] of %q = %q, want the fact key beside the frozen type", token, content, content)
	}
	return event.EventID(token[:bar]), token[bar+1:], content[at+2:]
}

// withoutPresentedTicket answers the material one presented message carries with
// the ticket that addresses it peeled off: a consumption check counts the
// material a request carries, and the address in front of it is not material. A
// message opening with no ticket is answered as it stands.
func withoutPresentedTicket(content string) string {
	if !strings.HasPrefix(content, "[evt_") {
		return content
	}
	at := strings.Index(content, "] ")
	if at < 0 {
		return content
	}
	return content[at+2:]
}

// requireTicketNamesACommittedFact requires a presented ticket's key to name a
// fact the session committed: the address travels with the material only when the
// recall surface can answer it, so a key no fact carries is an address that
// answers missing.
func requireTicketNamesACommittedFact(t *testing.T, run *assembly.Run, addr event.Address, key event.EventID) {
	t.Helper()
	for _, fact := range run.SessionFacts(addr) {
		if fact.Key == key {
			return
		}
	}
	t.Fatalf("the presented ticket names %s, which session %s committed as no fact: the address answers missing", key, addr.SessionID)
}

// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
//
// The presented-ticket reading of the history anchors: a projected entry reaches
// the model with the ticket of the fact it was minted from in front of its
// material, so a check that reads the material peels the ticket off and a check
// that knows the fact key asserts the ticket literally.
package history

import (
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// presentedTicket spells the ticket one entry reaches the model as: the recall
// prefix, the fact key and the frozen type. The form is written out here rather
// than asked of the production writer, so a change to the ticket's spelling fails
// these checks instead of moving along with them.
func presentedTicket(key event.EventID, typ string) string {
	return "[evt_" + string(key) + "|" + typ + "]"
}

// withoutPresentedTicket answers the material one presented message carries with
// the ticket that addresses it peeled off: these anchors read the material a
// request carries, and the address in front of it is not material. A message
// opening with no ticket is answered as it stands.
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

// presentedTicketType answers the type the ticket a presented message opens with
// names, and whether the message opens with a ticket at all: an anchor that does
// not hold the fact key still reads the kind the address claims.
func presentedTicketType(content string) (string, bool) {
	at := strings.Index(content, "] ")
	if !strings.HasPrefix(content, "[evt_") || at < 0 {
		return "", false
	}
	bar := strings.LastIndex(content[:at], "|")
	if bar < 0 {
		return "", false
	}
	return content[bar+1 : at], true
}

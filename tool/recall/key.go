// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
//
// Ticket parsing of the recall surface: the three ways a caller spells one
// event key all resolve to the same canonical identity.
package recall

import (
	"strings"

	"github.com/SpellingDragon/tagent/api/event"
)

// keyTicketPrefix is the ticket prefix the projection writes in front of an
// event key, and the one a caller may hand back unchanged.
const keyTicketPrefix = "evt_"

// parseKey resolves the one key an argument names: the bracketed ticket
// [evt_KEY|type], the bare ticket evt_KEY and the raw key all resolve to the
// same event, and the type beside a ticket is dropped because the ticket names
// the event rather than the type. A whole presented line — a provenance anchor, a
// ticket and the one-line read the fold wrote — resolves through the ticket it
// carries, so a caller that copies the line out of the material reaches the same
// event; an argument carrying no ticket keeps the three spellings above and is
// never guessed from the anchor it carries.
func parseKey(raw string) (event.EventID, error) {
	text := strings.TrimSpace(raw)
	if key, found := ticketKeyIn(text); found {
		return key, nil
	}
	text = strings.TrimPrefix(text, "[")
	if cut, _, found := strings.Cut(text, "|"); found {
		text = cut
	}
	text = strings.TrimSuffix(strings.TrimSpace(text), "]")
	text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), keyTicketPrefix))
	if text == "" {
		return "", ErrRecallKey
	}
	return event.EventID(text), nil
}

// ticketKeyIn answers the key of the first ticket one argument carries, and
// whether it carries one: the ticket opens with the recall prefix and closes with
// the bracket that ends it, so a line stating an anchor, a ticket and a read is
// read through the ticket and never through the anchor's own separator.
func ticketKeyIn(text string) (event.EventID, bool) {
	at := strings.Index(text, keyTicketPrefix)
	if at < 0 {
		return "", false
	}
	rest := text[at+len(keyTicketPrefix):]
	end := strings.IndexByte(rest, ']')
	if end < 0 {
		return "", false
	}
	key, _, _ := strings.Cut(rest[:end], "|")
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	return event.EventID(key), true
}

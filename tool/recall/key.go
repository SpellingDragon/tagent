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
// the event rather than the type.
func parseKey(raw string) (event.EventID, error) {
	text := strings.TrimSpace(raw)
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

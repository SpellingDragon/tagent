// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
//
// The observation-table port of the recall surface: the references a time,
// type and keyword query matches, the reference of one landed event and the
// original reference that event was landed with.
package recall

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// Table is the observation-table read face the recall tools consume: the event
// references one declarative query matches, the reference of one event the
// table has landed, and the original reference that event was landed with.
//
// The redline list (3 rules):
// MUST answer over the events the table has already landed and never over a store behind it.
// MUST answer a key the table holds no event for as not held.
// MUST NOT widen the granted partition set: an event outside it takes no part in an answer.
type Table interface {
	// QueryEvents returns the references matching opts, totally ordered and
	// paged on the declarative terms of the memory read face.
	QueryEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error)
	// Event returns the reference of the landed event under key and reports
	// whether the table holds it.
	Event(key event.EventID) (memory.EventReference, bool)
	// Reference returns the original reference key was landed with and reports
	// whether the table holds one.
	Reference(key event.EventID) (ledger.OriginalRef, bool)
}

package ledger

import "github.com/SpellingDragon/tagent/api/event"

// FactKey returns the EventID owning the original. The reference scope (owner,
// format and retention policy) keeps references registered side by side; every
// EventID owns an independent body, even when another event has identical bytes.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r OriginalRef) FactKey() string { return string(r.Key) }

// FactType returns the frozen type a reference ticket is read as.
func (r OriginalRef) FactType() string { return "memory.original" }

// The reference ticket family satisfies the fact category contract.
var _ event.Fact = OriginalRef{}

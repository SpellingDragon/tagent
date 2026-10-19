package ledger

import "github.com/SpellingDragon/tagent/event"

// FactKey returns the reference's identity as a fact key: the canonical hex key
// of the original blob it names. The deduplication scope (owner, format and
// retention policy) keeps references registered side by side and never
// re-identifies the blob, so two tickets of byte-identical content answer the
// same key.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r OriginalRef) FactKey() string { return string(r.Key) }

// FactType returns the frozen type a reference ticket is read as.
func (r OriginalRef) FactType() string { return "memory.original" }

// The reference ticket family satisfies the fact category contract.
var _ event.Fact = OriginalRef{}

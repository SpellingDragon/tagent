package rl

import "github.com/SpellingDragon/tagent/api/event"

// FactKey returns the attempt's identity as a fact key: the attempt ID the record
// is stored under. A retry hidden below the decision boundary is not a second
// attempt, so the key of one attempt stays the ID it was recorded with.
//
// 契约: docs/FACT-VOCABULARY.md#families
func (r AttemptRecord) FactKey() string { return string(r.ID) }

// FactType returns the record kind an attempt line is read as; the record family
// keeps one frozen type per record kind.
func (r AttemptRecord) FactType() string { return "rl.attempt" }

// The attempt record kind of the training observation family satisfies the fact
// category contract.
var _ event.Fact = AttemptRecord{}

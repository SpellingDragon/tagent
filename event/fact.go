package event

// Fact is the fact category contract every fact family satisfies: the identity it
// is quoted by and the frozen type it is read as. Both questions are asked of the
// fact as it stands, because a fact is immutable once produced — neither answer is
// ever re-derived from the store or from the consumer that reads it.
//
// The identity is a plain string rather than EventID: it is a spelling the
// vocabulary registers, so a family living in a package that must not import this
// one still states it. The families, the package each of them lives in and the
// mechanism that locks each identity are registered in the fact vocabulary page;
// the event family below is the canonical one.
//
// 契约: docs/FACT-VOCABULARY.md#fact-category
type Fact interface {
	// FactKey is the identity the fact is quoted by: one fact answers the same
	// key every time it is asked, and a retry keeps the original identity.
	FactKey() string
	// FactType is the frozen type the fact is read as.
	FactType() string
}

// FactKey returns the event's canonical key as its fact identity, spelt as the
// hex string the store holds the event under; Type and Timestamp describe that
// fact and never re-identify it.
func (e Event) FactKey() string { return string(e.Key) }

// FactType returns the event's frozen type, one of the Type constants of this
// package.
func (e Event) FactType() string { return e.Type }

// The event family satisfies the fact category contract.
var _ Fact = Event{}

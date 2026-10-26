package eventexample

import (
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// factCatalog is the canonical consumer of the fact category: it files each fact under the
// identity it is quoted by and refuses a second fact that quotes an identity already on
// record, so one identity never answers two frozen types.
type factCatalog struct {
	kinds map[string]string
}

// errIdentityTaken is the named refusal a catalog reports instead of overwriting the type
// one identity already answered.
var errIdentityTaken = errors.New("eventexample: the fact identity is already on record")

func newFactCatalog() *factCatalog {
	return &factCatalog{kinds: map[string]string{}}
}

// File records the identity and the frozen type of one fact.
func (c *factCatalog) File(fact event.Fact) error {
	key := fact.FactKey()
	if _, taken := c.kinds[key]; taken {
		return errIdentityTaken
	}
	c.kinds[key] = fact.FactType()
	return nil
}

// Kind returns the frozen type filed under one identity.
func (c *factCatalog) Kind(key string) (string, bool) {
	kind, ok := c.kinds[key]
	return kind, ok
}

// ExampleFact files one event family fact and one compress request family fact, reports
// that a restatement on a non-identity field keeps the identity and type it was filed
// under, and reports that a second fact quoting the same identity is refused.
//
// 契约: docs/FACT-VOCABULARY.md#fact-category
func ExampleFact() {
	catalog := newFactCatalog()
	stamp := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	produced := event.Event{Key: "evt-1", Type: event.TypeAgentOutput, Timestamp: stamp}
	judged := event.CompressRequestFor("memory.curation", "seq-1", "the card sequence exceeds the curation bound")

	if err := catalog.File(produced); err != nil {
		return
	}
	if err := catalog.File(judged); err != nil {
		return
	}

	restated := produced
	restated.Timestamp = stamp.Add(time.Hour)
	if restated.FactKey() != produced.FactKey() || restated.FactType() != produced.FactType() {
		return
	}
	kind, ok := catalog.Kind(produced.FactKey())
	if !ok || kind != event.TypeAgentOutput {
		return
	}
	if err := catalog.File(judged); !errors.Is(err, errIdentityTaken) {
		return
	}
}

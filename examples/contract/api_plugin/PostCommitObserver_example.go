package pluginexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// auditObserver is the canonical post-commit observer: it reads one fact whose canonical
// commit already happened and reports what it observed to the audit side.
type auditObserver struct{}

// Name reports the stable name of the element.
func (auditObserver) Name() string { return "audit-observer" }

// Port reports the fact->observation port the element serves.
func (auditObserver) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// ObserveCommit observes one committed fact as a read-only duty: a failure returns an
// error, which the After pass records as a visible gap beside this element's name.
func (auditObserver) ObserveCommit(_ context.Context, id event.EventID) error {
	if id == "" {
		return errors.New("audit: the observation needs a committed fact")
	}
	return nil
}

var (
	_ plugin.Instance           = auditObserver{}
	_ plugin.PostCommitObserver = auditObserver{}
)

// ExamplePostCommitObserver observes one committed fact through the After pass: the fact
// is read after its canonical commit, and a failed observation returns as a named gap
// that never rolls the committed fact back.
//
// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
func ExamplePostCommitObserver() {
	pipeline, err := plugin.NewFlow([]plugin.Instance{auditObserver{}})
	if err != nil {
		return
	}
	presentation := pipeline.After(context.Background(), event.EventID("fact-1"))
	_ = presentation.Fact
}

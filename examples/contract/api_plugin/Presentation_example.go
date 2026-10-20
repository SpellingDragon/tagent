package pluginexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// brokenAudit is the observer whose observation fails: the failure is recorded as one
// named gap beside the plugin name, and the committed fact stays as it was committed.
type brokenAudit struct{}

// Name reports the stable name of the element.
func (brokenAudit) Name() string { return "broken-audit" }

// Port reports the fact->observation port the element serves.
func (brokenAudit) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

// ObserveCommit fails on every committed fact, so the After pass records a gap.
func (brokenAudit) ObserveCommit(_ context.Context, id event.EventID) error {
	if id == "" {
		return errors.New("broken-audit: the observation needs a committed fact")
	}
	return errors.New("broken-audit: the audit sink is unreachable")
}

var (
	_ plugin.Instance           = brokenAudit{}
	_ plugin.PostCommitObserver = brokenAudit{}
)

// ExamplePresentation drives one committed fact through the whole After pass: the fact
// identity is the event.EventID of the canonical commit, the rendered views sit beside
// the visible gaps the failed observers left, and the fact itself is never withheld or
// retracted by either outcome.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExamplePresentation() {
	pipeline, err := plugin.NewFlow([]plugin.Instance{brokenAudit{}, presentText{}})
	if err != nil {
		return
	}
	fact := event.EventID("fact-1")
	presentation := pipeline.After(context.Background(), fact)
	if presentation.Fact != fact {
		return
	}
	if len(presentation.Gaps) != 1 || len(presentation.Views) != 1 {
		return
	}
	if presentation.Gaps[0].Plugin != "broken-audit" {
		return
	}
	if presentation.Views[0].Plugin != "present-text" || presentation.Views[0].Dropped {
		return
	}
	_ = presentation.Gaps[0].Err
	_ = presentation.Views[0].Text
}

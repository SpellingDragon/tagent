// 契约: docs/EXTENSIONS.md#pipeline-phases
//
// Package view holds the presentation vocabulary of one committed fact: the
// After pass outcome a run reads out, the rendered views its final presenters
// produced and the visible gaps its failed observers left. The vocabulary is a
// projection of a fact, never a second fact: it names what a host reads beside
// the committed fact and carries no write face, so presenting, dropping and
// failing to observe all leave the committed fact as it was committed.
//
// The package depends on event alone, for the identity of the fact it presents.
package view

import (
	"github.com/SpellingDragon/tagent/api/event"
)

// ObservationGap is the visible gap one failed post-commit observation leaves
// behind: the plugin that failed, the committed fact it failed to observe and
// the error it failed with. The gap is the record of the failure; the committed
// fact is untouched by it.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
type ObservationGap struct {
	// Plugin is the stable name of the plugin whose observation failed.
	Plugin string

	// Fact is the committed fact the observation failed on.
	Fact event.EventID

	// Err is the error the observation failed with.
	Err error
}

// RenderedView is the final presentation one presenter rendered for a fact: the
// rendered text beside whether the channel dropped it. A dropped view drops the
// presentation only; the committed fact stays.
type RenderedView struct {
	// Plugin is the stable name of the presenter that rendered the view.
	Plugin string

	// Text is the presentation text the presenter rendered.
	Text string

	// Dropped reports whether the channel dropped this presentation.
	Dropped bool
}

// Presentation is the After pass outcome of one committed fact: the views the
// final presenters rendered beside the visible gaps the failed observers left.
// It never carries an error and never withholds the fact: the committed fact is
// already durable when the pass runs, and no outcome of this pass may retract
// it.
type Presentation struct {
	// Fact is the committed fact the pass read.
	Fact event.EventID

	// Views holds the rendered presentations, in pipeline order.
	Views []RenderedView

	// Gaps holds the visible observation gaps, in pipeline order.
	Gaps []ObservationGap
}

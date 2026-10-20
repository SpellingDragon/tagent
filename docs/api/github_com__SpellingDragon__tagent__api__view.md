package view // import "github.com/SpellingDragon/tagent/api/view"

Package view holds the presentation vocabulary of one committed fact: the After
pass outcome a run reads out, the rendered views its final presenters produced
and the visible gaps its failed observers left. The vocabulary is a projection
of a fact, never a second fact: it names what a host reads beside the committed
fact and carries no write face, so presenting, dropping and failing to observe
all leave the committed fact as it was committed.

The package depends on event alone, for the identity of the fact it presents.

TYPES

type ObservationGap struct {
	// Plugin is the stable name of the plugin whose observation failed:
	// provenance, tracing back to the one element that left this gap.
	Plugin string

	// Fact is the committed fact the observation failed on: provenance,
	// tracing back to the source the gap was left on.
	Fact event.EventID

	// Err is the error the observation failed with.
	Err error
}
    ObservationGap is the visible gap one failed post-commit observation leaves
    behind: the plugin that failed, the committed fact it failed to observe
    and the error it failed with. The gap is the record of the failure; the
    committed fact is untouched by it, and the gap's provenance is the plugin
    beside the fact it names.

type Presentation struct {
	// Fact is the committed fact the pass read: provenance, the source every
	// view and gap of this presentation traces back to.
	Fact event.EventID

	// Views holds the rendered presentations, in pipeline order.
	Views []RenderedView

	// Gaps holds the visible observation gaps, in pipeline order.
	Gaps []ObservationGap
}
    Presentation is the After pass outcome of one committed fact: the views the
    final presenters rendered beside the visible gaps the failed observers left.
    It never carries an error and never withholds the fact: the committed fact
    is already durable when the pass runs, and no outcome of this pass may
    retract it.

type RenderedView struct {
	// Plugin is the stable name of the presenter that rendered the view:
	// provenance, tracing back to the one element that wrote the text.
	Plugin string

	// Text is the presentation text the presenter rendered.
	Text string

	// Dropped reports whether the channel dropped this presentation.
	Dropped bool
}
    RenderedView is the final presentation one presenter rendered for a fact:
    the rendered text beside whether the channel dropped it. The view is a
    product of the presenter that wrote it and of the fact it was rendered
    from, so its provenance is the plugin name here beside the fact of the
    presentation that carries it. A dropped view drops the presentation only;
    the committed fact stays.

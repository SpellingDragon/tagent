package plugin

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/view"
)

// After runs the one consuming pass over one fact whose canonical commit already
// happened: every fact->observation face reads the fact first, then every
// fact->view face renders it. The pass runs after the commit and never before it,
// so a refused call and a call whose commit failed present nothing at all, and it
// carries no error return: a failure here is a gap or a dropped view, never a
// reason to touch the committed fact. A fact answers one view, several, or none —
// each view stands in the order the presenters were declared, names the presenter
// that wrote it beside the fact it was rendered from, and a presenter that drops
// its text drops its own view only.
//
// The redline list (3 rules):
// MUST answer an observer-only failure as a named gap and dispatch the later faces.
// MUST report a driver failure beside ErrObservationObligationRetained, and never as a delivered observation or a log line alone.
// MUST NOT retry, absorb or clear a driver's obligation here: the obligation stays in the driver's own account.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func (p Pipeline) After(ctx context.Context, fact event.EventID) view.Presentation {
	out := view.Presentation{Fact: fact}
	for _, face := range p.observers {
		started := time.Now()
		err := face.impl.ObserveCommit(ctx, fact)
		observeFace(PortFactObservation, faceObserve, face.name, faceResult(err), "fact="+string(fact), started)
		if err != nil {
			out.Gaps = append(out.Gaps, face.gap(fact, err))
		}
	}
	for _, face := range p.presenters {
		started := time.Now()
		text, dropped := face.impl.Present(ctx, fact)
		observeFace(PortFactView, facePresent, face.name, facePresentation(dropped), "fact="+string(fact), started)
		out.Views = append(out.Views, view.RenderedView{Plugin: face.name, Text: text, Dropped: dropped})
	}
	return out
}

// facePresentation spells the outcome one presenting face reported: the text it
// rendered, or the drop it declared instead of a silent empty view.
func facePresentation(dropped bool) string {
	if dropped {
		return "dropped"
	}
	return "rendered"
}

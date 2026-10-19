package view_test

import (
	"errors"
	"testing"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/view"
)

// TestPresentationIsOneView pins the presentation as one view of a committed fact.
// - a dropped view keeps its rendered text and plugin name: the drop drops the presentation only
// - a failed observation is a named gap beside the fact, never a retraction of it
// - views and gaps stay in creation order and the fact identity is carried untouched
//
// 契约: docs/EXTENSIONS.md#pipeline-phases
func TestPresentationIsOneView(t *testing.T) {
	fact := event.EventID("event-1")
	failure := errors.New("the observation failed")
	presentation := view.Presentation{
		Fact:  fact,
		Views: []view.RenderedView{{Plugin: "dropping", Text: "rendered", Dropped: true}, {Plugin: "presenting", Text: "shown"}},
		Gaps:  []view.ObservationGap{{Plugin: "broken", Fact: fact, Err: failure}},
	}

	if presentation.Fact != fact {
		t.Fatalf("presentation fact = %q, want the committed fact %q", presentation.Fact, fact)
	}

	dropped := presentation.Views[0]
	if !dropped.Dropped || dropped.Text != "rendered" || dropped.Plugin != "dropping" {
		t.Fatalf("dropped view = %+v, want the drop reported beside its text and plugin", dropped)
	}
	if got := presentation.Views[1]; got.Dropped || got.Text != "shown" || got.Plugin != "presenting" {
		t.Fatalf("second view = %+v, want pipeline order kept after the dropped view", got)
	}

	gap := presentation.Gaps[0]
	if gap.Plugin != "broken" || gap.Fact != fact || !errors.Is(gap.Err, failure) {
		t.Fatalf("gap = %+v, want the plugin, fact and cause named", gap)
	}

	empty := view.Presentation{Fact: fact}
	if len(empty.Views) != 0 || len(empty.Gaps) != 0 || empty.Fact != fact {
		t.Fatalf("empty presentation = %+v, want the fact identity alone", empty)
	}
}

package pluginexample

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/plugin"
)

// presentText is the canonical presenter: it renders the presentation of one fact for its
// channel and reports whether the channel dropped it.
type presentText struct{}

// Name reports the stable name of the element.
func (presentText) Name() string { return "present-text" }

// Phase reports the one phase the element serves.
func (presentText) Phase() plugin.Phase { return plugin.PhaseFinalPresentation }

// Present renders the presentation of one committed fact and reports it beside whether
// the channel dropped it; a drop drops the presentation only.
func (presentText) Present(_ context.Context, id event.EventID) (string, bool) {
	if id == "" {
		return "", true
	}
	return "presented " + string(id), false
}

var (
	_ plugin.Plugin         = presentText{}
	_ plugin.FinalPresenter = presentText{}
)

// ExampleFinalPresenter renders one committed fact through the After pass: the rendered
// view reaches the presentation read-out beside the drop flag, and the committed fact the
// view was rendered from stays.
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func ExampleFinalPresenter() {
	pipeline, err := plugin.NewPipeline([]plugin.Plugin{presentText{}})
	if err != nil {
		return
	}
	presentation := pipeline.After(context.Background(), event.EventID("fact-1"))
	for _, view := range presentation.Views {
		_ = view.Text
	}
}

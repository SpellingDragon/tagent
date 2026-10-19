package memoryqueryexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/query"
)

// oneShotSource is the canonical reading source: it freezes one gathering of the owners
// beside the per-source watermarks read at that same moment.
type oneShotSource struct {
	order []query.ObjectRef
}

// Freeze takes one immutable gathering of the owners and answers with its frozen detail
// source beside one watermark per source read at that gathering. The position is carried
// in the text form the source itself names: it is opaque, never parsed, and never
// compared with a position of another source.
func (s oneShotSource) Freeze(context.Context) (query.DetailSource, []query.SourceWatermark, error) {
	return freezeDetails(s.order), []query.SourceWatermark{
		{Source: "inventory", Position: query.SourcePosition("revision-7"), LagState: query.WatermarkCaughtUp},
	}, nil
}

var _ query.ReadingSource = oneShotSource{}

// ExampleReadingSource freezes one reading and reports that the pages of that revision
// describe the same content beside the position of the source they were read from.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleReadingSource() {
	ctx := context.Background()
	source := oneShotSource{order: []query.ObjectRef{
		{Kind: query.ObjectAgent, Address: event.Address{AgentID: "assistant", SessionID: "session-1"}},
	}}

	details, watermarks, err := source.Freeze(ctx)
	if err != nil {
		return
	}
	if len(watermarks) != 1 {
		return
	}
	if _, ok := details.DetailOf(source.order[0]); !ok {
		return
	}
}

package queryexample

import (
	"context"
	"errors"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/query"
)

// ExampleDetail_refusal drives both named refusals of a detail read through the real
// projection: an object the caller is not authorized to read and an object the bounded
// snapshot does not hold each refuse by name instead of answering an empty detail.
//
// 契约: docs/wiki/agent/agent-architecture.md#core-components
func ExampleDetail_refusal() {
	ctx := context.Background()
	projection := assembledProjection()
	filter := scopedFilter()

	snapshot, cursor, err := projection.Snapshot(ctx, filter)
	if err != nil {
		return
	}

	outside := query.ObjectRef{Kind: query.ObjectAgent, Address: event.Address{AgentID: "intruder"}}
	if _, err := projection.Detail(snapshot.Revision, cursor, filter, outside); !errors.Is(err, query.ErrDetailOutsideScope) {
		return
	}

	absent := query.ObjectRef{Kind: query.ObjectTask, TaskID: "task-404"}
	if _, err := projection.Detail(snapshot.Revision, cursor, filter, absent); !errors.Is(err, query.ErrDetailAbsent) {
		return
	}
}

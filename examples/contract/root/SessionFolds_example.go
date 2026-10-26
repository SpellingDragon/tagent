// Package rootexample shows how a host satisfies the SessionFolds port.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
package rootexample

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/replay"
)

type demoFolds struct {
	projections map[event.SessionID]*replay.ReplayProjection
	windows     map[event.SessionID]tagent.FoldWindow
	owners      map[event.EventID]event.SessionID
	filed       []event.CompressRequest
}

func (f *demoFolds) SessionOf(_ context.Context, id event.EventID) (event.SessionID, bool) {
	session, held := f.owners[id]
	return session, held
}

func (f *demoFolds) ProjectionOf(_ context.Context, session event.SessionID) (*replay.ReplayProjection, tagent.FoldWindow, bool) {
	projection, held := f.projections[session]
	if !held {
		return nil, tagent.FoldWindow{}, false
	}
	return projection, f.windows[session], true
}

func (f *demoFolds) FileCompressRequest(_ context.Context, _ event.SessionID, request event.CompressRequest) error {
	f.filed = append(f.filed, request)
	return nil
}

// ExampleSessionFolds demonstrates the face a host fills for the age fold
// plugin: the session of one commit, the projection and window that session
// registered, and the filing of the judgment a fold answered — the plugin holds
// no chain and no ledger of its own.
func ExampleSessionFolds() {
	folds := &demoFolds{
		projections: map[event.SessionID]*replay.ReplayProjection{"chat-a": replay.NewReplayProjection()},
		windows:     map[event.SessionID]tagent.FoldWindow{"chat-a": {MaxTokens: 1000}},
		owners:      map[event.EventID]event.SessionID{"evt-1": "chat-a"},
	}
	var face tagent.SessionFolds = folds
	plugin := tagent.NewAgeFoldPlugin(face)
	_ = plugin.ObserveCommit(context.Background(), "evt-1")
	fmt.Println(plugin.Name())
}

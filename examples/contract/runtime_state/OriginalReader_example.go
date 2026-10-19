package stateexample

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// archiveOriginals is the canonical read face of the migrated originals: the bytes one key
// holds and whether the tier holds any under it.
type archiveOriginals struct {
	bodies map[event.EventID][]byte
}

// GetOriginal returns the bytes stored under key and whether the tier holds any under it.
func (a *archiveOriginals) GetOriginal(_ context.Context, key event.EventID) ([]byte, bool, error) {
	body, ok := a.bodies[key]
	if !ok {
		return nil, false, nil
	}
	return append([]byte(nil), body...), true, nil
}

var _ state.OriginalReader = (*archiveOriginals)(nil)

// ExampleOriginalReader reads back the bytes one migrated key holds and reports an absent
// key as not held rather than as an empty original.
//
// 契约: docs/wiki/reliability/durable-delivery.md#canonical-fact-resolution
func ExampleOriginalReader() {
	ctx := context.Background()
	reader := &archiveOriginals{bodies: map[event.EventID][]byte{"event-1": []byte("original text")}}

	body, held, err := reader.GetOriginal(ctx, "event-1")
	if err != nil || !held || string(body) != "original text" {
		return
	}
	if _, held, err := reader.GetOriginal(ctx, "event-2"); err != nil || held {
		return
	}
}

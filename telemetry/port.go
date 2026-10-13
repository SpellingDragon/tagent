// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// Package telemetry is the runtime's observation bypass: it carries the
// outbound ports through which an already committed fact is observed for
// tracing and audit, and the bounded recorder that hands those observations to
// a writer.
//
// Nothing here owns runtime state. The ports consume committed facts, never
// block the caller that produced one, and never withhold a fact from a caller
// because an observer is slow or broken; with observation switched off, the
// committed facts stay available to every reader.
package telemetry

import (
	"context"

	"github.com/SpellingDragon/tagent/event"
)

// Observer is the post-commit observation entry: the single callback a runtime
// invokes for each fact that has committed successfully.
//
// Observe runs after the fact is durable, so a slow or failing observer can
// never hold back that fact or reorder it against other committed facts. A
// failure is recorded as a gap in the observation record and reported to the
// caller; the committed fact is never rolled back, and the observed work is
// never re-run to satisfy an observer.
type Observer interface {
	// Observe reports one committed fact to the observation side.
	//
	// The fact is passed unchanged and is never rewritten here. A returned
	// error records a missing observation, not an undone fact.
	Observe(ctx context.Context, evt event.Event) error
}

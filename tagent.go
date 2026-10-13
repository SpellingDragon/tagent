// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Package tagent is the composition root of the runtime and its only public
// entry: the single point where a pure organization declaration becomes a live
// runtime, and the only dependency-injection site of the process.
//
// The root establishes the shared faces before any candidate binding is prepared
// and assembles the execution face on top of them, and it keeps no second copy of
// the live scene: the effective bindings belong to the state owner, the
// obligations it admits belong to the task layer, and the readings it serves are
// derived from the facts those owners already hold. This is the phase 1 skeleton:
// it declares the public face and holds no behaviour.
package tagent

import (
	"context"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
)

// Runtime is the assembled runtime the host drives: the shared state face, the
// task face and the execution face of one process, wired once by Build from the
// cross-boundary ports Deps supplies — the memory original store, the telemetry
// observer and the ordered plugin pipeline among them.
//
// The runtime holds these faces rather than copies of them, so every entry below
// advances the one live scene, and its fields stay private because Build is the
// only way to obtain a runtime. This is the phase 1 skeleton: the entries are
// frozen stubs and hold no behaviour.
type Runtime struct {
	// store is the shared state face, established before any candidate binding
	// and never part of a candidate's revocable resources.
	store state.Store
	// manager is the task admission and settlement face, injected as the store's
	// notification outlet.
	manager task.Manager
	// agent is the execution face, assembled over the two shared faces.
	agent agent.Agent
}

// CloseSession closes one session's own side across the whole runtime: new input
// for the named address is refused, while the obligations already accepted for
// that site stay open for a recoverable path instead of being dropped.
//
// A drain that did not converge comes back as an error rather than a clean close,
// and one session's close never cancels another invocation of the same session.
func (r *Runtime) CloseSession(ctx context.Context, addr event.Address) error {
	return errNotImplemented
}

// Shutdown closes the runtime for new work and drains what it already holds: the
// shared faces refuse new input, the accepted obligations settle, and the
// temporary resources the runtime owns stop for real.
//
// A deadline that expires before the drain converged comes back as an error that
// states what was still open, so a truncated shutdown is never reported as a
// clean one.
func (r *Runtime) Shutdown(ctx context.Context) error {
	return errNotImplemented
}

// Query serves one bounded, immutable reading of the runtime through the
// read-only projection, filtered by the authorization scope the caller already
// holds.
//
// The reading grants no authorization of its own and derives every count from
// the detail entries it returns, so a host page and a training reader see the
// same numbers from the same entries.
func (r *Runtime) Query(filter query.ListFilter) (query.Snapshot, error) {
	return query.Snapshot{}, errNotImplemented
}

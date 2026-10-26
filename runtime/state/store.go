// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
//
// Package state is the runtime's state owner: it establishes the agent and
// session sites an explicit address names, admits a prepared plan as the next
// effective binding generation, closes a session's own side, and accepts the
// notifications a task intends to deliver.
//
// The package holds the effective bindings and the session sites, and an
// executor borrows a binding for one request rather than keeping a second
// writable source of its own. The store, the borrowing face, the send seam and
// the durable commit carrier are the behaviour behind the ports.
package state

import (
	"context"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// Store is the state owner's shared face: the composition root establishes the
// agent and session sites here, admits one prepared plan as the next effective
// binding generation, closes a session's own side, and forwards the
// notifications a task intends to deliver.
//
// The redline list (4 rules):
// MUST establish the shared face before any candidate binding is prepared.
// MUST discard a candidate whose baseline was advanced during its preparation.
// MUST NOT overwrite a newer publication with an expired candidate, or derive a new generation from byte-identical content.
// MUST NOT default an address to a most-recent session or drop the accepted obligations of a closed site.
type Store interface {
	// Establish creates the session site for one explicit (agent, session)
	// address on the given runtime instance and returns it. The address is never
	// defaulted to a most-recent session, and rebuilding an agent under the same
	// name yields a different instance.
	Establish(ctx context.Context, addr event.Address, instance event.InstanceID) (*Session, error)

	// Publish admits a fully prepared plan as the next effective binding
	// generation and returns the publication record. A plan whose baseline was
	// already advanced is discarded instead of overwriting the newer
	// publication, and reloading byte-identical content yields no new
	// generation.
	Publish(ctx context.Context, plan StatePlan) (Publication, error)

	// CloseSession closes the session's own side: new input is refused while the
	// accepted obligations of the site are kept for a recoverable path.
	CloseSession(ctx context.Context, addr event.Address) error

	// AcceptNotice accepts one notification into the mailbox of the target
	// session. Acceptance is idempotent by the notification's stable identity,
	// so a replayed delivery records no second fact, and acceptance never waits
	// for a slow model decision.
	AcceptNotice(ctx context.Context, notice task.Notice) error
}

package evolution

import (
	"context"
	"time"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#refine
//
// Refine is the git-native half of the self-improvement channel: an edit already
// takes effect when it lands on disk, and this surface records, lists and safely
// reverts such an edit through the host's git.
type Refine interface {
	// Register records the explicitly listed controlled files as one improvement:
	// it stages exactly those paths and commits them under the improvement
	// marker, leaving other working-tree edits and anything the user already
	// staged untouched. A path outside the controlled set refuses the whole
	// registration before git is touched.
	Register(ctx context.Context, paths []string, note string) (RefineRecord, error)

	// Status lists the improvement ledger together with the current state of each
	// evaluation window.
	Status(ctx context.Context) ([]RefineRecord, error)

	// Rollback reverts one recorded improvement, named by its full commit or an
	// unambiguous prefix. A commit that does not carry the improvement marker is
	// refused, so a user's own commit is never reverted. Reverting is a terminal
	// act of the agent, never of the framework.
	Rollback(ctx context.Context, sha string) error
}

// RefineRecord is one entry of the improvement ledger: the controlled paths the
// improvement changed and the note its author attached.
//
// The note states the pain point the improvement answers, the artifact it
// produced and the benefit it expects, so a ledger entry explains itself without
// its commit message.
type RefineRecord struct {
	// Paths lists the explicitly recorded controlled paths of the improvement.
	Paths []string
	// Note is the author's pain-point, artifact and expected-benefit note.
	Note string
}

// EvidenceWindow is the one-shot evidence window of one improvement version.
//
// Its origin is the activation moment, the moment a real request first consumed
// that version, and the version it names is the one actually consumed. A git
// registration time is a correlation only and never the window's origin, because
// a file takes effect when it lands on disk and registration may follow the
// first real consumption.
type EvidenceWindow struct {
	// Version names the improvement version whose real consumption opened the
	// window.
	Version string
	// ActivatedAt is the activation moment at which a real request first consumed
	// that version.
	ActivatedAt time.Time
}

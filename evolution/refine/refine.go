package refine

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/evolution"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#refine
//
// Refine is the git-native half of the self-improvement channel: an edit already
// takes effect when it lands on disk, and this surface records, lists and safely
// reverts such an edit through the host's git.
// The redline list (4 rules):
// MUST stage exactly the listed controlled paths and commit them under the improvement marker.
// MUST refuse a path outside the controlled set before git is touched.
// MUST NOT revert a commit that does not carry the improvement marker.
// MUST NOT touch a working-tree edit outside the listed paths.
type Refine interface {
	// Register records the explicitly listed controlled files as one improvement:
	// it stages exactly those paths and commits them under the improvement
	// marker, leaving other working-tree edits and anything the user already
	// staged untouched. A path outside the controlled set refuses the whole
	// registration before git is touched.
	Register(ctx context.Context, paths []string, note string) (RefineRecord, error)

	// Status lists the improvement ledger together with the current state of each
	// evaluation window and the post-hoc judgment recorded for each entry.
	Status(ctx context.Context) ([]RefineRecord, error)

	// Rollback reverts one recorded improvement, named by its full commit or an
	// unambiguous prefix. A commit that does not carry the improvement marker is
	// refused, so a user's own commit is never reverted. Reverting is a terminal
	// act of the agent, never of the framework.
	Rollback(ctx context.Context, sha string) error
}

// RefineRecord is one entry of the improvement ledger: the controlled paths the improvement changed,
// the note its author attached, and the read face — registration SHA, gate report, activation window and
// post-hoc report — through which the entry traces back to the evidence it rests on.
//
// - The note states the pain point the improvement answers, the artifact it produced and the benefit it expects, so a ledger entry explains itself without its commit message.
// - The entry carries its basis GateReport, and through that report's EvidenceRefs and the window's activation evidence a reader resolves what the improvement was judged against and when it took effect.
// - The registration SHA is a git correlation only: it links feedback to the consumed version and never opens or anchors the window, whose origin is the activation moment.
// - A zero record carries no judgment: an empty window means none has opened and an empty PosteriorReport means none has run, and neither is read as healthy.
type RefineRecord struct {
	// Paths lists the explicitly recorded controlled paths of the improvement.
	Paths []string
	// Note is the author's pain-point, artifact and expected-benefit note.
	Note string
	// SHA is the registration commit carrying the improvement marker. It is a
	// git correlation only and never the window's anchor.
	SHA string
	// CommittedAt is the moment the registration commit landed, read back from
	// git. It orders the ledger and marks the point after which a controlled file
	// is an artifact not yet registered; it is a git fact only and never the
	// anchor of the evaluation window, whose origin is the activation moment.
	CommittedAt time.Time
	// GateBasis is the structural gate report the improvement rests on, kept so
	// the ledger entry traces back through its EvidenceRefs to the evidence
	// behind the judgment. Its zero value means no gate report backs the entry
	// and is never read as a passing gate.
	GateBasis evolution.GateReport
	// Window is the one-shot evidence window of the improvement version, opened
	// at the activation moment a real request first consumed that version.
	Window evolution.EvidenceWindow
	// Posterior is the post-hoc evaluation report of the improvement version
	// after its window produced a judgment.
	Posterior evolution.PosteriorReport
}

// ImprovementMarker is the line-anchored marker an improvement registration
// commit's subject opens with. Generation, listing and rollback verification
// share this one constant, and its line anchoring keeps the reverts git writes
// as `Revert "[self-improve] …"` out of the ledger, so a rollback is never
// re-read as a fresh improvement.
const ImprovementMarker = "[self-improve]"

// Improvement is one successful registration the refine surface hands to its
// recorder: the registration commit carrying the improvement marker, the
// controlled paths it committed and the author's note.
//
// It is the payload of the improvement event a registration writes through the
// injected ImprovementRecorder, and its SHA is a git correlation only: it links
// the event back to the committed version and never opens or anchors an
// evaluation window.
type Improvement struct {
	// SHA is the registration commit carrying the improvement marker.
	SHA string
	// Paths lists the controlled paths the registration committed.
	Paths []string
	// Note is the author's pain-point, artifact and expected-benefit note.
	Note string
}

// ImprovementRecorder records one successful registration as a governance
// improvement event. It is the port through which the refine surface reaches
// the governance event face without importing governance, keeping the layering
// line: evolution never depends on governance and writes governance-class
// events straight through this injected port. A nil recorder records nothing,
// and a recorder failure never retracts the commit that already landed; the
// registration reports the unrecorded event by name instead.
type ImprovementRecorder func(ctx context.Context, improvement Improvement) error

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
	GateBasis GateReport
	// Window is the one-shot evidence window of the improvement version, opened
	// at the activation moment a real request first consumed that version.
	Window EvidenceWindow
	// Posterior is the post-hoc evaluation report of the improvement version
	// after its window produced a judgment.
	Posterior PosteriorReport
}

// EvidenceWindow is the one-shot evidence window of one improvement version.
//
// Its origin is the activation moment, the moment a real request first consumed
// that version, and the version it names is the one actually consumed. A git
// registration time is a correlation only and never the window's origin, because
// a file takes effect when it lands on disk and registration may follow the
// first real consumption. The window is carried by its RefineRecord together
// with the post-hoc report the window produced, so a window always connects
// back to its result. A window that cannot show activation evidence is judged
// insufficient with the missing evidence named, and the zero value never means
// the window is open.
type EvidenceWindow struct {
	// Version names the improvement version whose real consumption opened the
	// window.
	Version string
	// ActivatedAt is the activation moment at which a real request first consumed
	// that version.
	ActivatedAt time.Time
	// ActivationEvidence names the evidence that proves the activation moment:
	// the recorded first real consumption of Version. Its zero value means no
	// activation evidence is on record and is never read as an opened window.
	ActivationEvidence EvidenceRef
}

// PosteriorVerdict is the post-hoc evaluation state of one improvement version after real requests
// consumed it, judged over its evidence window: healthy, degraded, insufficient or not-due.
//
// - healthy: the window reached its judgment point with enough decidable evidence and that evidence shows no degradation.
// - degraded: the same threshold was met but the evidence shows degradation, which yields a rollback proposal.
// - insufficient: an attempted judgment aborted because the decidable evidence fell below the threshold or a source was missing or unreachable; the missing evidence is named.
// - not-due: the window has not reached its judgment point yet, so no judgment is owed.
// - The states are mutually exclusive and never blurred: insufficient never mixes with degraded (an aborted judgment asserts no degradation), not-due is never read as healthy, and insufficient is never folded into healthy.
// - This vocabulary is the post-hoc half only and never reused for the structural gate's pre-publication dispositions (GateVerdict); a verdict not produced yet is the empty PosteriorReport, never a fifth verdict, and that empty value is never read as healthy.
type PosteriorVerdict string

const (
	// PosteriorHealthy reports a due window whose decidable evidence met the
	// threshold and showed no degradation.
	PosteriorHealthy PosteriorVerdict = "healthy"

	// PosteriorDegraded reports a due window whose decidable evidence met the
	// threshold and showed degradation; it yields a rollback proposal.
	PosteriorDegraded PosteriorVerdict = "degraded"

	// PosteriorInsufficient reports an attempted judgment that aborted because
	// the decidable evidence was below threshold or an evidence source was
	// missing or unreachable; the missing evidence is named. It asserts no
	// degradation and is never read as healthy.
	PosteriorInsufficient PosteriorVerdict = "insufficient"

	// PosteriorNotDue reports a window that has not reached its judgment point
	// yet; it is never read as healthy and yields no rollback proposal.
	PosteriorNotDue PosteriorVerdict = "not-due"
)

// PosteriorReport is the post-hoc evaluation report of one improvement version:
// the four-state verdict, the named reason behind it and the evidence it rests
// on.
//
// The report is kept apart from the structural GateReport: the structural
// verdicts judge a candidate declaration before publication, the post-hoc
// states judge an improvement version after real consumption, and neither
// vocabulary is ever substituted for the other. When the verdict is
// insufficient, Reason names the missing evidence and the judgment aborts
// instead of falling back to healthy. Source and statistics fields are declared
// together with their real reviewer or guardrail consumer, so this report
// carries no speculative evaluation table. The zero report means no evaluation
// has run yet; its empty verdict is not a fifth state and is never read as
// healthy.
type PosteriorReport struct {
	// Verdict is the post-hoc state of the improvement version, exactly one of
	// healthy, degraded, insufficient or not-due.
	Verdict PosteriorVerdict
	// Reason is the named reason behind the verdict: the named missing evidence
	// for insufficient, the named ground for degraded.
	Reason string
	// EvidenceRefs names the evidence the verdict rests on.
	EvidenceRefs []EvidenceRef
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

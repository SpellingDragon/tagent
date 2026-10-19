package refine // import "github.com/SpellingDragon/tagent/evolution/refine"

Skeleton compile-time assertions for the git-native refinement surface:
the three operations of the channel, the ledger entry it answers with and the
improvement its recorder takes. A renamed field or a widened signature stops
this file from compiling.

CONSTANTS

const ImprovementMarker = "[self-improve]"
    ImprovementMarker is the line-anchored marker an improvement registration
    commit's subject opens with. Generation, listing and rollback verification
    share this one constant, and its line anchoring keeps the reverts git writes
    as `Revert "[self-improve] …"` out of the ledger, so a rollback is never
    re-read as a fresh improvement.

VARIABLES

var (
	// ErrNotGitRepository reports a refine operation whose run directory is not
	// a git work tree. The file edit itself already took effect on disk, so the
	// registration degrades to a plain named failure and never blocks the edit.
	ErrNotGitRepository = errors.New("evolution: refine needs a git repository (改进登记需 git 仓)")

	// ErrGitOperationInProgress reports a refine mutation refused because a git
	// revert or merge is still open in the repository. Registering during an
	// open revert would commit through it and swallow the sequencer state, so
	// the registration stops before any git command mutates the repository.
	ErrGitOperationInProgress = errors.New("evolution: refine refuses to mutate while a git revert or merge is open")

	// ErrImprovementUnrecorded reports a registration whose commit landed but
	// whose improvement event could not be recorded. The commit stays, because
	// its SHA is a correlation only; the unrecorded event is reported by name.
	ErrImprovementUnrecorded = errors.New("evolution: the registration committed but its improvement event was not recorded")

	// ErrImprovementUnknown reports a rollback naming a commit that does not
	// resolve to any commit in the repository.
	ErrImprovementUnknown = errors.New("evolution: no commit resolves to the named improvement")

	// ErrCommitNotImprovement reports a rollback naming a commit that carries no
	// improvement marker. A user's own commit is never reverted, so the rollback
	// is refused before git is touched.
	ErrCommitNotImprovement = errors.New("evolution: only a commit registered under the improvement marker may be rolled back")

	// ErrRevertConflict reports a rollback whose revert stopped on a conflict.
	// The git output and the disposition guidance travel back to the agent, and
	// the repository keeps the open revert for the agent to resolve or abort.
	ErrRevertConflict = errors.New("evolution: the revert left a conflict for the agent to resolve")
)
    ErrNotGitRepository and the other named failures of the git-native refine
    surface are reachable through errors.Is over the Unwrap chain rather than
    rendered text, so a caller reads the refusal it needs without matching
    messages.

var ErrNoRegisterPaths = errors.New("evolution: refine register needs at least one controlled path")
    ErrNoRegisterPaths reports a registration that names no controlled path to
    record. A registration with no explicit pathspec has nothing to stage and
    commit and is refused before git is touched.

var ErrPathOutOfScope = errors.New("evolution: refine register refuses a path outside the controlled set")
    ErrPathOutOfScope reports a registration naming a path outside the
    controlled path set. The whole registration is refused before any git
    command runs, so an out-of-scope path never reaches `git add` or `git
    commit` and the repository keeps the state it had.

FUNCTIONS

func DefaultProtectedPaths() []string

    DefaultProtectedPaths returns the controlled path patterns the refine
    surface guards by default: the prompt and skill trees. A registration may
    record only the files under these patterns, so an edit to anything else is
    refused before git is touched, and the file itself still took effect on disk
    the moment it was written.

TYPES

type GitRefine struct {
	// Has unexported fields.
}

    GitRefine is the git-native refinement surface over one controlled
    repository: it records an explicit set of controlled files as one
    improvement commit, lists the improvement ledger, and safely reverts one
    marked commit. It holds no version store of its own — git is the version
    layer and the files on disk are the source of truth, so an edit takes effect
    the moment it lands and this surface only leaves a trace, an evaluation hook
    and a safe undo.

func NewGitRefine(root string, record ImprovementRecorder, protected []string) *GitRefine
    NewGitRefine returns the refinement surface over the controlled repository
    at root. It records each successful registration through the injected
    recorder, which is optional, and admits only paths under the controlled
    patterns — the defaults when none are supplied. Every git command runs in
    root, so the surface never reaches any repository but the one it is pointed
    at.

func (g *GitRefine) Register(ctx context.Context, paths []string, note string) (RefineRecord, error)
    Register records the explicitly listed controlled files as one improvement.
    It stages exactly those paths and commits only them under the improvement
    marker, leaving the rest of the working tree and anything the user already
    staged untouched. A path outside the controlled set refuses the whole
    registration before git is touched.

    - A commit that finds nothing to commit is a normal outcome, not a failure:
    the files already hold the content the registration would record.

func (g *GitRefine) Rollback(ctx context.Context, sha string) error
    Rollback reverts one recorded improvement named by its full commit or an
    unambiguous prefix. A commit that does not carry the improvement marker is
    refused before git is touched, so a user's own commit is never reverted.
    A revert that stops on a conflict returns the git output and disposition
    guidance and leaves the revert open for the agent; reverting is a terminal
    act of the agent and never of the framework.

func (g *GitRefine) Status(ctx context.Context) ([]RefineRecord, error)
    Status lists the improvement ledger: every commit whose subject opens with
    the improvement marker, newest first, with the note and controlled paths
    each registration carried and the moment its commit landed. A commit without
    the marker — including the reverts git writes back — never enters the
    ledger. The evaluation window and post-hoc report stay zero here and are
    filled by the post-activation evaluation; a zero record is never read as
    healthy.

func (g *GitRefine) Unregistered(ctx context.Context) ([]string, error)
    Unregistered lists the controlled files whose modification time falls after
    the last registration commit: artifacts the agent has already changed
    on disk but not yet recorded. It is a soft reminder beside the ledger,
    never a gate, because a file takes effect the moment it lands regardless of
    registration.

type Improvement struct {
	// SHA is the registration commit carrying the improvement marker.
	SHA string
	// Paths lists the controlled paths the registration committed.
	Paths []string
	// Note is the author's pain-point, artifact and expected-benefit note.
	Note string
}
    Improvement is one successful registration the refine surface hands to
    its recorder: the registration commit carrying the improvement marker,
    the controlled paths it committed and the author's note.

    It is the payload of the improvement event a registration writes through
    the injected ImprovementRecorder, and its SHA is a git correlation only:
    it links the event back to the committed version and never opens or anchors
    an evaluation window.

type ImprovementRecorder func(ctx context.Context, improvement Improvement) error
    ImprovementRecorder records one successful registration as a governance
    improvement event. It is the port through which the refine surface reaches
    the governance event face without importing governance, keeping the layering
    line: evolution never depends on governance and writes governance-class
    events straight through this injected port. A nil recorder records nothing,
    and a recorder failure never retracts the commit that already landed;
    the registration reports the unrecorded event by name instead.

type PathScopeError struct {
	// Offending lists the written paths that fall outside the controlled set.
	Offending []string
	// Controlled lists the controlled patterns the paths were judged against.
	Controlled []string
}
    PathScopeError is the named refusal of a registration whose paths leave the
    controlled set: the offending paths and the controlled patterns they were
    judged against. Unwrap exposes ErrPathOutOfScope, so a caller reaches the
    named refusal through errors.Is instead of matching rendered text.

func (e *PathScopeError) Error() string
    Error renders the offending paths beside the controlled set they missed.

func (e *PathScopeError) Unwrap() error
    Unwrap exposes the named refusal underneath the rendered message.

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

    Refine is the git-native half of the self-improvement channel: an edit
    already takes effect when it lands on disk, and this surface records, lists
    and safely reverts such an edit through the host's git. The redline list (4
    rules): MUST stage exactly the listed controlled paths and commit them under
    the improvement marker. MUST refuse a path outside the controlled set before
    git is touched. MUST NOT revert a commit that does not carry the improvement
    marker. MUST NOT touch a working-tree edit outside the listed paths.

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
    RefineRecord is one entry of the improvement ledger: the controlled paths
    the improvement changed, the note its author attached, and the read face
    — registration SHA, gate report, activation window and post-hoc report —
    through which the entry traces back to the evidence it rests on.

    - The note states the pain point the improvement answers, the artifact
    it produced and the benefit it expects, so a ledger entry explains itself
    without its commit message. - The entry carries its basis GateReport,
    and through that report's EvidenceRefs and the window's activation evidence
    a reader resolves what the improvement was judged against and when it
    took effect. - The registration SHA is a git correlation only: it links
    feedback to the consumed version and never opens or anchors the window,
    whose origin is the activation moment. - A zero record carries no judgment:
    an empty window means none has opened and an empty PosteriorReport means
    none has run, and neither is read as healthy.

type RevertConflictError struct {
	// SHA is the resolved commit the revert tried to undo.
	SHA string
	// Output is the git output naming the conflict and the disposition steps.
	Output string
}
    RevertConflictError is the named failure of a rollback whose revert stopped
    on a conflict: the commit it tried to revert and the git output carrying the
    conflict. Unwrap exposes ErrRevertConflict. The repository is left in its
    open revert state, so the agent resolves the conflict and runs `git revert
    --continue`, or abandons it with `git revert --abort`; the framework never
    resolves or aborts a revert on its own.

func (e *RevertConflictError) Error() string
    Error renders the reverted commit and the git output guiding the resolution.

func (e *RevertConflictError) Unwrap() error
    Unwrap exposes the named conflict underneath the rendered message.

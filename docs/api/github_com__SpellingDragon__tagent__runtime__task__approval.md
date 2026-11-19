package approval // import "github.com/SpellingDragon/tagent/runtime/task/approval"

The wiring face of the approval channel: the manager that holds requests,
the option seam that injects its clock and rescan window, and the file face an
external approver answers. The words it records (status, subject, evidence,
state, request, channel, delivery gap) are stated once in approval.go; this
file keeps the evidence an approver reads and folds presence and ruling from the
attention facts, so the vocabulary has one spelling and the chain has the truth.

- a registration files one attention question and a ruling files one attention
answer, so the status a request reports is the answer its question carries -
presence is the question the chain still holds as a question, and the waiting
face reports exactly that set - the request file is the write entry of an
external approver and the mirror of the evidence block, never a second truth -
a ruling found in a request file lands as an answer, and the file is withdrawn
once that answer landed

The redline list (4 rules): MUST answer presence and ruling from the
attention facts alone, never from the request file or the evidence block.
MUST bind every ruling to the argument digest through the question identity, so
changed arguments never ride an old approval. MUST keep the request file at the
path external approvers already write, its three blocks flat. MUST NOT run an
unapproved call: a store that cannot record the request leaves the gate waiting,
never executing.

The durable face of the attention chain: the fact one question, answer or
clearance is filed as, written behind the store's barrier under the identity it
stands for and folded back into the chain a restarted process reads. The chain
is the truth of every presence and every ruling the approval channel answers,
so it outlives the process instead of being the one record the cut happens to
state. The write-and-read surface it fills is durable.FactLog, declared beside
the other ledgers' surfaces; the rules below are this chain's own filing and
folding obligations over that surface.

- one fact is one key, so a repeated filing rewrites nothing and no sequence
the stream skipped can sit beside it - a write the store refused is named to
the caller and at ERROR, and the fact it refused is not reported as durable - a
fold orders the stored facts by the landing stamp they carry, ties read by the
identity, so a read never depends on the map the store answers in

The redline list (2 rules): MUST refuse a stored pair whose body states another
fact than its key names. MUST fold the stored facts into the chain beside
presence and ruling it already holds, so a read answers both alike.

The attention face of the approval channel: the question one held call raises,
the answer an approver rules it with and the clearance that removes it, each
filed as a fact on a chain instead of an index of the store's own. The manager
keeps the evidence an approver reads and the file an external approver answers;
presence and ruling are folded from the facts, so what a restart reads back is
what landed.

- one approval question is identified by its kind, its target and its lineage;
the lineage names the tool, the digest of the exact argument bytes and the
approval identity the registration was issued under - presence is a question
the chain holds with no answer and no clearance beside it, judged against the
validity it was filed under - a ruling is the answer's own body, read back as
the status word it was filed with - identities are compared through the fact
key, so the lineage text is never parsed back apart

The redline list (3 rules): MUST derive every presence and every ruling from the
facts the chain holds, never from the request file. MUST compare one question,
its answer and its clearance by fact key, never by splitting the lineage back
into fields. MUST NOT keep a presence or a ruling beside the chain: the evidence
block and the request file only mirror what the facts state.

VARIABLES

var ErrApprovalExpired = errors.New("approval: the approval request expired before it was decided")
    ErrApprovalExpired reports a ruling that arrived after the request expired:
    an expired request is treated as not approved, and a late ruling never makes
    it runnable again.

var ErrApprovalIdentityMissing = errors.New("approval: the approval request carries no approval identity")
    ErrApprovalIdentityMissing reports a request registration that carries no
    approval identity. The identity is the one tagent issued when the call was
    parked, so it is required rather than replaced by a locally minted one:
    the host response names that one identity across the hold and the request.

var ErrApprovalNotFound = errors.New("approval: no approval request names that identity")
    ErrApprovalNotFound reports a ruling that names no registered approval
    request, so it is refused by name instead of being applied to a request that
    does not exist.

var ErrApprovalRulingUnknown = errors.New("approval: the approval response names no known ruling")
    ErrApprovalRulingUnknown reports a ruling whose status names no known
    decision, so it is refused closed instead of being recorded as a guess.

FUNCTIONS

func ArgsDigest(jsonArgs []byte) string
    ArgsDigest computes the argument digest of one call: the lowercase hex
    of the SHA-256 of the exact argument bytes the call carries. The digest
    binds an approval to the arguments that were held, so a ruling over other
    arguments is refused instead of approving a changed call.

TYPES

type ApprovalChannel interface {
	// Deliver offers one approval request to an external approver.
	Deliver(ctx context.Context, req ApprovalRequest) error
}
    ApprovalChannel is the outbound delivery face of the approval channel:
    one approval request is offered to an external approver over it. The channel
    is a gate, never a wall: a delivery that fails is recorded as a visible
    gap and never blocks the gate, and the request stays answerable through the
    other entries (CLI, WeChat) or through the request file itself.

    The redline list (3 rules): MUST deliver one request per call, keyed by the
    approval identity the request carries. MUST NOT read a failed delivery as a
    ruling: the gate keeps waiting. MUST NOT rewrite the request it was handed.

type ApprovalEvidence struct {
	// ArgsPreview is the bounded preview of the arguments an approver reads.
	ArgsPreview string `json:"args_preview"`
	// RiskLevel is the risk grade the pre-execution ruling was rendered at.
	RiskLevel plugin.RiskLevel `json:"risk"`
	// RuleID names the rule the ruling came from, if one matched.
	RuleID string `json:"rule_id,omitempty"`
	// Reason is the readable reason the call was held.
	Reason string `json:"reason"`
}
    ApprovalEvidence is the block an approver reads to decide one held call:
    the bounded argument preview, the grade and rule the ruling was rendered at,
    and the reason the call was held.

type ApprovalManager struct {
	// Has unexported fields.
}
    ApprovalManager is the waiting book of the approval channel: one request
    per file under <dir>/approvals/, the attention facts that carry its
    presence and its ruling, and a throttled rescan through which a ruling an
    external approver wrote to a request file lands on the chain. The manager is
    concurrent safe.

    The manager owns the digest binding: it computes the digest of the exact
    argument bytes at registration and every ruling it honours is matched
    against that digest through the question lineage, so an approval of other
    arguments never runs.

func NewApprovalManager(dir string, ttl time.Duration, opts ...ApprovalOption) *ApprovalManager
    NewApprovalManager returns the approval manager over one directory.
    A non-empty dir keeps the requests under <dir>/approvals/ as one file per
    request and restores the attention questions and answers the files state;
    an empty dir keeps the manager on its in-memory chain alone. A manager
    seated by the framework state slice (WithApprovalSeat) reads no file here:
    the restore of the stated state puts the book back instead. The ttl is the
    validity a request is issued with.

func (a *ApprovalManager) AddChannel(ch ApprovalChannel)
    AddChannel attaches one outbound delivery face the manager offers its
    requests to. A channel that fails records a visible gap and never blocks the
    gate.

func (a *ApprovalManager) AttentionPresence(ctx context.Context) []task.AttentionQuestion
    AttentionPresence reports the approval questions the attention chain still
    holds as questions: the one presence every reader of the channel shares, so
    an answered approval leaves this read exactly as it leaves the waiting face,
    whatever audit record a delivery channel keeps beside it.

func (a *ApprovalManager) BindAttention(ctx context.Context, log durable.FactLog) error
    BindAttention hands the book the durable face its attention chain is written
    to and reads it for the work the boot owes, and never for the history behind
    it.

    - A cut stated this book: the boot states the obligations still owed,
    so the read is each of their answer keys — the ruling a person filed while
    the process was down — and the resident set stays the cut's own. A request
    or a ruling of history is read by the reader that asks for it by identity
    (Lookup, Decide), never here. - No cut stated this book: the face is the
    one record of that book, so the whole stream is folded — the questions still
    standing, the rulings already stated and the request records that seat the
    identities those facts were filed under.

    The face is kept as the one every registration writes its request record to,
    so the identity an approver is handed outlives the process beside the facts
    it was filed with. The error belongs to the caller that can stop the boot
    — a chain this book could not read leaves the rulings behind the gate that
    serves them, and a book bound to no face writes no record.

func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) (*ApprovalRequest, error)
    Decide records the ruling of one approval request as the attention answer
    its question waits for, and reports the request with the ruling that took
    effect. The first ruling takes effect and a repeated response is idempotent:
    it reports the answer already on the chain instead of consuming a second
    one. A ruling over an identity the book holds no request for is answered
    from the chain that holds it, so a ruling filed before a restart still
    answers the identity the approver was handed; an identity the chain states
    no ruling for is refused beside ErrApprovalNotFound, a ruling that names no
    known decision is refused beside ErrApprovalRulingUnknown, and a ruling that
    arrived after the request expired is refused beside ErrApprovalExpired — an
    expired request is treated as not approved and is never made runnable by a
    late response. The ruling reaches the request file as its mirror under the
    store lock the evidence block is read and rewritten under, so a rescan never
    reads a pending body back over a landed ruling, and it reaches the request
    record of the answering identity, so the approver who ruled it is read back
    beside the ruling after a restart. A book that reached the chain drops the
    ruled request from its resident set once that record landed — the ruling is
    read back through the chain — so the book keeps the obligations still open;
    a book beside no face keeps it, the file being its only record. An identity
    the book holds no resident request for is ruled from the record the chain
    keeps under it, so an approver answering a request the cut left behind still
    rules that request instead of one a book would have to mint.

func (a *ApprovalManager) DeliveryGaps() []DeliveryGap
    DeliveryGaps reports the visible channel failures recorded so far.

func (a *ApprovalManager) Lookup(tool, argsDigest string) *ApprovalRequest
    Lookup reports the request the gate reads one (tool, arguments) pair under:
    an unexpired approved request — the approval the held call may proceed
    on — or, when none is approved, the newest request of that pair with its
    effective status: pending, denied, or expired once its validity ran out.
    The status is folded from the attention facts and the effective status from
    the validity the question was filed under, so a request whose validity ran
    out reads as expired and never as a standing approval.

    - A pair with no resident request is answered from the chain, by the
    identity it was filed under: the newest record of that pair, ruled or still
    open. - The read a ruling costs belongs to the gate that asks, never to the
    boot that restores the book.

    An unapproved read triggers a throttled rescan of the request directory,
    so a ruling an external approver wrote to a request file lands on the chain
    at runtime instead of waiting for a restart.

func (a *ApprovalManager) Pending() []*ApprovalRequest
    Pending reports the requests still awaiting a ruling and within their
    validity, oldest first, for an approval entry to display and answer.
    The set is the presence the attention chain holds, so the waiting face and
    a presence read name the same questions. - The evidence map is keyed by the
    identity each caller asked under, and a second registration of one standing
    pair aliases the one request it read back, so the map can name one approval
    identity twice. Presence is a set of questions, so the read reports each
    approval identity once: a repeated registration widens the alias, never the
    waiting set, and two requests stay two entries because their identities
    differ.

func (a *ApprovalManager) Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*ApprovalRequest, error)
    Request registers one pending approval request under the approval identity
    tagent issued when the call was parked: it files the attention question the
    request stands on, mirrors the request into its own file, and offers it to
    every attached channel. The manager computes the argument digest itself,
    so the registration keeps the digest binding the hold was issued over.

    A repeated registration of one approval identity, and a registration of
    arguments a standing question already covers, read that request back instead
    of filing a second question, so one held call is one question on the chain.
    The question is filed before the answer it waits for, so no ruling names a
    question the chain does not hold. A channel that fails records a visible
    gap and never fails the registration (the gate is a gate, never a wall).
    A request that cannot be written to its file, or whose question the chain
    refuses, is reported to the caller beside the request and the wait stands
    either way: a hold is never turned into a denial by a store-side failure.
    A caller that asks under an id of its own while a standing question already
    holds the pair names that same question: its id resolves the same ruling,
    so deciding by the id one minted never misses the question the gate filed
    first.

func (a *ApprovalManager) RestoreBoot(ctx context.Context) error
    RestoreBoot restores the book from the row of request files, for the boot
    whose state states no approval book: the files are the per-request durable
    write and the only record a process that took no cut left behind.

    A row this boot cannot read leaves the book empty and the failure visible
    beside it, never a boot that dies on it: an unreadable row states no ruling
    either way, so the restore it refuses approves nothing, and a call that
    still needs a ruling parks again and reports the same failure when it files
    its own request. A boot that stopped here would be a wall around a gate.

func (a *ApprovalManager) RestoreEvidence(ctx context.Context, stated []ApprovalRequest, keys []string) error
    RestoreEvidence puts one stated approval state back: every request still
    within its validity is seated beside the question it stands on and, when a
    ruling landed, beside the answer that ruling was filed as, so a restarted
    book reports the same presence and the same rulings the process before it
    reported.

    The read is the stated state alone — no request file is opened. A hold past
    its validity is dropped rather than resurrected, because an expired hold
    never proceeds and a restart that put one back would hand a dead wait a
    second life. A ruled request is kept past its validity instead: the answer
    it stands on is the record that these exact arguments were refused or
    approved by a person, and a book that forgot it would mint a fresh request
    the same person never saw — the very request a restart must not be able to
    turn into an approval. It also states that a cut is this book's record,
    so the bind that follows reads that face for the work the cut left and never
    for the history behind it.

func (a *ApprovalManager) SnapshotEvidence() ([]ApprovalRequest, []string)
    SnapshotEvidence reports the obligations the book still owes, for the
    framework state slice to state: the identity each caller asked under,
    the evidence an approver reads and the state each request stands in.

    - An expired hold is not stated: a restore drops it, so the cut would carry
    a request no process keeps. - A ruled request is stated only beside no
    durable face: a bound book reads its ruling back by identity. - A face-less
    book states its rulings: there the cut is the one record those arguments
    were ruled over.

    The identity each entry is stated under is separate from the request's
    own identity because a second registration aliases a caller identity onto
    one standing request: the two answers a cut must carry are the entry and
    the identity it is read under. The read takes the book's own read lock and
    nothing else, so it never waits on a write of its own nor flushes one.

type ApprovalOption func(*ApprovalManager)
    ApprovalOption configures an approval manager at construction.

func WithApprovalClock(now func() time.Time) ApprovalOption
    WithApprovalClock injects the clock the manager reads its time from,
    so a test drives validity windows deterministically instead of sleeping.

func WithApprovalRescanInterval(window time.Duration) ApprovalOption
    WithApprovalRescanInterval injects the throttled window between two rescans
    of the request directory. The window exists so an external approver's write
    to a request file becomes visible at runtime without rescanning on every
    read; zero rescans on every unapproved read, which a test uses to stay
    deterministic.

func WithApprovalSeat() ApprovalOption
    WithApprovalSeat declares that the framework state slice seats this book:
    the book reads no request file at construction, and the restore of the
    stated state puts it back (see RestoreEvidence and RestoreBoot). A book
    wired through its directory alone keeps restoring itself from the files at
    construction.

type ApprovalRequest struct {
	ApprovalSubject
	ApprovalEvidence
	ApprovalState
}
    ApprovalRequest is one request for a human ruling over one held call:
    one request per file, readable and writable by the external approver.
    Every ruling is bound to ArgsDigest — the digest of the exact argument bytes
    that were held — so an approval of other arguments is refused instead of
    running a changed call.

    The request is three blocks — who was held, what the approver reads,
    and where the request stands — and its file keeps them as one flat document,
    so an approver answers a request without knowing the blocks.

type ApprovalState struct {
	// CreatedMs is the registration time of the request, in Unix milliseconds.
	CreatedMs int64 `json:"created_ms"`
	// ExpiresMs is the moment the request loses its validity, in Unix
	// milliseconds. An undecided request past this moment reads as expired.
	ExpiresMs int64 `json:"expires_ms"`
	// Status is the state the request is in.
	Status ApprovalStatus `json:"status"`
	// DecidedBy names the approver whose ruling took effect.
	DecidedBy string `json:"decided_by,omitempty"`
}
    ApprovalState is the block that reports where one request stands: when it
    was registered, when its validity runs out, and the ruling that took effect.

type ApprovalStatus string
    ApprovalStatus is the state one approval request is in. The four states
    carry one meaning each: pending awaits an external ruling and the governed
    call does not run meanwhile, approved lets that very call proceed, denied
    is a terminal refusal of it, and expired means the request outlived its
    validity without a ruling and is treated as not approved — a later response
    never makes an expired request runnable again.

const (
	// ApprovalPending is waiting for an external approver's ruling; the
	// governed operation does not execute meanwhile.
	ApprovalPending ApprovalStatus = "pending"

	// ApprovalApproved lets the held call proceed.
	ApprovalApproved ApprovalStatus = "approved"

	// ApprovalDenied is a terminal refusal: the call ends and returns as a
	// refused result.
	ApprovalDenied ApprovalStatus = "denied"

	// ApprovalExpired is a request that outlived its validity without a
	// ruling: it is treated as not approved, and nothing proceeds on the old
	// request afterwards.
	ApprovalExpired ApprovalStatus = "expired"
)
type ApprovalSubject struct {
	// ID is the approval identity tagent issued when the call was parked. The
	// host response names this identity, so the hold and the request resolve
	// under one name.
	ID string `json:"id"`
	// ToolName is the tool the held call wants to run.
	ToolName string `json:"tool"`
	// ArgsDigest is the digest of the exact argument bytes the call carries,
	// computed by ArgsDigest. Every ruling is bound to it.
	ArgsDigest string `json:"args_digest"`
}
    ApprovalSubject is the identity block of one approval request: the tool the
    held call wants to run, and the two names the request is resolved under.

type DeliveryGap struct {
	// Request is the approval identity the failed delivery was for; it is empty
	// beside a failure that belongs to the book rather than to one request.
	Request string
	// Err is the failure the channel or the boot read reported.
	Err error
}
    DeliveryGap is one visible failure of the book that is not a ruling:
    an approval request the channels could not be handed, or a boot that could
    not read the row of request files. The gap is recorded and exposed instead
    of being swallowed, and the gate keeps waiting rather than treating the
    failure as an approval or as a crash.

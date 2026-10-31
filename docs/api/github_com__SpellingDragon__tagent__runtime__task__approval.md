package approval // import "github.com/SpellingDragon/tagent/runtime/task/approval"

The wiring face of the approval channel: the manager that holds requests,
the option seam that injects its clock and rescan window, and the file-backed
request directory an external approver writes into. The words it records
(status, subject, evidence, state, request, channel, delivery gap) are stated
once in approval.go; this file only keeps and resolves them, so the vocabulary
has one spelling and the store has one job.

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
    ApprovalManager is the waiting book of the approval channel: one request per
    file under <dir>/approvals/, an in-memory index beside it, and a throttled
    rescan through which a ruling an external approver wrote to a request file
    becomes visible at runtime. The manager is concurrent safe.

    The manager owns the digest binding: it computes the digest of the exact
    argument bytes at registration and every ruling it honours is matched
    against that digest, so an approval of other arguments never runs.

func NewApprovalManager(dir string, ttl time.Duration, opts ...ApprovalOption) *ApprovalManager
    NewApprovalManager returns the approval manager over one directory.
    A non-empty dir keeps the requests under <dir>/approvals/ as one file per
    request and rebuilds the index from what it finds there; an empty dir keeps
    the manager in memory only. The ttl is the validity a request is issued
    with.

func (a *ApprovalManager) AddChannel(ch ApprovalChannel)
    AddChannel attaches one outbound delivery face the manager offers its
    requests to. A channel that fails records a visible gap and never blocks the
    gate.

func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) (*ApprovalRequest, error)
    Decide records the ruling of one approval request and reports the request
    with the ruling that took effect. The first ruling takes effect and a
    repeated response is idempotent: it reports the first ruling again instead
    of consuming a second one. A ruling over an unknown identity is refused
    beside ErrApprovalNotFound, a ruling that names no known decision is refused
    beside ErrApprovalRulingUnknown, and a ruling that arrived after the
    request expired is refused beside ErrApprovalExpired — an expired request
    is treated as not approved and is never made runnable by a late response.
    The ruling reaches the request file under the store lock the index entry is
    read and rewritten under, so a rescan never reads a pending body back over a
    landed ruling.

func (a *ApprovalManager) DeliveryGaps() []DeliveryGap
    DeliveryGaps reports the visible channel failures recorded so far.

func (a *ApprovalManager) Lookup(tool, argsDigest string) *ApprovalRequest
    Lookup reports the request the gate reads one (tool, arguments) pair under:
    an unexpired approved request — the approval the held call may proceed
    on — or, when none is approved, the newest request of that pair with its
    effective status: pending, denied, or expired once its validity ran out.
    The effective status is what the returned copy reports, so a request whose
    validity ran out reads as expired and never as a standing approval.

    An unapproved read triggers a throttled rescan of the request directory,
    so a ruling an external approver wrote to a request file becomes visible at
    runtime instead of waiting for a restart.

func (a *ApprovalManager) Pending() []*ApprovalRequest
    Pending reports the requests still awaiting a ruling and within their
    validity, oldest first, for an approval entry to display and answer.

func (a *ApprovalManager) Request(ctx context.Context, id, tool string, jsonArgs []byte, risk plugin.RiskLevel, rule, reason string) (*ApprovalRequest, error)
    Request registers one pending approval request under the approval identity
    tagent issued when the call was parked and writes it to its own file beside
    offering it to every attached channel. The manager computes the argument
    digest itself, so the registration keeps the digest binding the hold was
    issued over.

    The request file lands before the in-memory index names the request:
    a registration is the file, so a ruling of that identity is written only
    after the pending body it rules on reached the disk, and the two writes of
    one identity never share a temporary file. The write and the index entry it
    registers are one step of the store lock, so no rescan slips between them.
    A channel that fails records a visible gap and never fails the registration
    (the gate is a gate, never a wall), and a repeated registration under the
    same identity keeps the request already stored. A request that cannot be
    written to its file is reported to the caller beside the request, which
    stays registered in memory: a hold waits either way and is never turned into
    a denial by a channel-side failure.

type ApprovalOption func(*ApprovalManager)
    ApprovalOption configures an approval manager at construction.

func WithApprovalClock(now func() time.Time) ApprovalOption
    WithApprovalClock injects the clock the manager reads its time from,
    so a test drives validity windows deterministically instead of sleeping.

func WithApprovalRescanInterval(window time.Duration) ApprovalOption
    WithApprovalRescanInterval injects the throttled window between two
    directory rescans. The window exists so an external approver's write to a
    request file becomes visible at runtime without rescanning on every read;
    zero rescans on every unapproved read, which a test uses to stay
    deterministic.

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
	// Request is the approval identity the failed delivery was for.
	Request string
	// Err is the delivery failure the channel reported.
	Err error
}
    DeliveryGap is one visible channel failure: an approval request the channels
    could not be handed. The gap is recorded and exposed instead of being
    swallowed, and the gate keeps waiting rather than treating the failure as an
    approval or as a crash.

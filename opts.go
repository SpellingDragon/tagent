package tagent

import (
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
	"github.com/SpellingDragon/tagent/telemetry"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// publishContract is the precondition contract the assembly enforces on every
// publication: the three structural checks must each carry a disposition.
var publishContract = state.PublishContract{Required: []state.CheckName{"eval", "benchmark", "apply-gate"}}

// ErrGovernanceMissing reports a formal assembly that carries no governance:
// the sole publication entry is driven under a real governance and never under
// a stand-in that passes every required check by construction.
var ErrGovernanceMissing = errors.New("tagent: the formal assembly carries no governance")

// ErrRetrievalUnwired reports a recall against a run assembled beside no
// storage tier: the retrieval face reads through the observation table over
// the committed store, and a run whose facts live only in memory carries no
// such store, so the recall is refused by name instead of answered as an empty
// reference set that would read as "nothing was found".
var ErrRetrievalUnwired = errors.New("tagent: the run carries no retrieval face (assembled beside no storage)")

// assemblyConfig is the dependency-injection surface of the formal assembly:
// the governance every publication is decided under, the approval ruling every
// admitted call is decided under beside its enforcement grade, and the storage
// tier the task facts persist through. The model and tool implementations are
// contributed per agent through the NativeAgent map; these entries are the
// shared dependencies of the whole run.
type assemblyConfig struct {
	governance  state.Governance
	approver    task.Approver
	policy      plugin.Governor
	enforcement plugin.Enforcement
	storage     *ledger.StorageConfig
	embedder    memory.Embedder
	recorder    rl.Recorder
	observer    telemetry.Observer
	execManager *tagentexec.Manager
	// reflectionAnchors is the anchor record path whose watermark the
	// reflection face reports.
	reflectionAnchors string
}

// Option configures the formal assembly at construction.
type Option func(*assemblyConfig)

// WithGovernance attaches the governance every publication of this assembly is
// decided under. The formal assembly carries no stand-in governance: an
// assembly beside no governance is refused by name before anything is built.
func WithGovernance(governance state.Governance) Option {
	return func(c *assemblyConfig) { c.governance = governance }
}

// WithApprover attaches the approval ruling every call admitted through the
// registration wrapper of this assembly is decided under. A policy attached
// through WithPolicy occupies this seat instead: the pre-execution ruling then
// comes from that policy alone and this approver is not consulted.
func WithApprover(approve task.Approver) Option {
	return func(c *assemblyConfig) { c.approver = approve }
}

// WithPolicy attaches the governance policy every call admitted through the
// registration wrapper of this assembly is ruled by: the ruling consumes the
// one Before pass the call carries — the final call beside the risk grade its
// risk faces rendered — exactly once per call, before any execution. The
// policy occupies the approval seat, so it is the sole pre-execution ruling of
// the assembly: a refused call is turned away before any execution with its
// named paired result, and a held call parks in the approval wait.
func WithPolicy(policy plugin.Governor) Option {
	return func(c *assemblyConfig) { c.policy = policy }
}

// WithEnforcement names the enforcement grade this assembly admits calls
// under. The zero grade behaves as the warn grade beside no approver: a call is
// admitted for execution. The strict grade beside no approver refuses the call
// before any execution instead of admitting it by default.
func WithEnforcement(grade plugin.Enforcement) Option {
	return func(c *assemblyConfig) { c.enforcement = grade }
}

// WithStorage opens the named storage tier for this assembly and stores the
// task facts through it: every fact the run commits lands on the tier's own
// fact log, and the chains that log already holds are folded back at assembly.
// An assembly beside no storage keeps its facts in memory and restores
// nothing.
func WithStorage(cfg ledger.StorageConfig) Option {
	return func(c *assemblyConfig) { c.storage = &cfg }
}

// WithEmbedder attaches the embedding binding the retrieval face of this
// assembly generates its event vectors under. An assembly beside no embedder
// builds no vector generation at all: recall answers through the keyword leg
// alone and behaves exactly as it did before the embedding existed. The seam
// borrows the binding per vector generation and never owns the endpoint.
func WithEmbedder(embedder memory.Embedder) Option {
	return func(c *assemblyConfig) { c.embedder = embedder }
}

// WithRecorder attaches the training record production face this assembly
// writes its episode, decision and attempt records through: the production
// points of the formal root record exactly what they observed, and a write the
// recorder refuses is counted in the explicit recording-loss account of the
// run instead of being dropped silently.
func WithRecorder(recorder rl.Recorder) Option {
	return func(c *assemblyConfig) { c.recorder = recorder }
}

// WithTelemetry attaches the post-commit observation face this assembly
// observes every committed fact through: the fact is read back from the
// committed ledger unchanged and handed to the observer, so the observation is
// a read-only attachment to facts that already committed and never a second
// source of truth beside that ledger.
func WithTelemetry(observer telemetry.Observer) Option {
	return func(c *assemblyConfig) { c.observer = observer }
}

// WithReflectionAnchors points the reflection face of this assembly at the
// anchor record its watermark is read from: the store owns that record and
// answers the watermark the diagnostic reading reports. An assembly beside no
// path carries no reflection anchor store, and its reflection face reports that
// absence instead of a watermark of its own.
func WithReflectionAnchors(path string) Option {
	return func(c *assemblyConfig) { c.reflectionAnchors = path }
}

// WithExecManager attaches the execution resource ledger this assembly explains
// its registered leases through: the blocker explanation read of the run
// reports each registration beside the retention facts the ledger records
// under its key, and beside the accounting the ledger keeps over its leases.
// An assembly beside no ledger still explains its task and notification
// blockers, and refuses to explain a registered lease by name.
func WithExecManager(manager *tagentexec.Manager) Option {
	return func(c *assemblyConfig) { c.execManager = manager }
}

// inspectorSubject, projectionWindow and agentShutdownPollInterval are the
// named constants of the assembly: the subject the diagnostic read face of the
// run reads the state owner's sites under, how many recent readings the
// projection keeps servable, and how often a close looks at real settlement.
const (
	inspectorSubject          = state.Subject("tagent-assembly-inspector")
	projectionWindow          = 8
	agentShutdownPollInterval = 2 * time.Millisecond
)

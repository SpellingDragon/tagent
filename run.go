// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// The minimal public assembly entry of the runtime: one Run assembles the
// shared faces once, publishes the candidate bindings of one declaration
// through the sole publication entry, and drives the agent's own persistent
// event loop per invocation. This entry is the only public door the execution
// domain opens; a later host assembly builds on it instead of assembling a
// second runtime beside it.
//
// The assembly is real, not a test stand-in: the model, tools and binding
// generation come from prepare and the state owner exactly as a production
// assembly would, the tool calls are admitted through the task registration
// wrapper, and the terminal of every decision is reduced by the model package's
// single reduction entry.
package tagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
)

// NativeAgent is what the embedding process contributes for one declared
// agent: the native model the agent runs against and the native tools visible
// to it. The assembly registers those tools through the task registration
// wrapper before the binding is published, so every call is admitted and
// settled through the task face.
//
// The redline list (4 rules):
// MUST register every declared tool through the task wrapper before the binding is published.
// MUST refuse the assembly by name when the declared model is nil.
// MUST NOT replace, cache or reorder the tools and plugin elements the host declares.
// MUST NOT let a native tool run beside the admission the task face keeps.
type NativeAgent struct {
	// Model is the native model of the agent; a nil model refuses the assembly.
	Model model.Model
	// Tools lists the native callable tools of the agent.
	Tools []trpctool.CallableTool
	// Plugins lists the plugin pipeline elements the agent's binding generation
	// carries: the one Before pass grades and the After pass presents every call
	// of that generation over these elements, in the declared order.
	Plugins []plugin.Plugin
}

// InvokeRequest is one invocation admitted at the assembly entry: the
// responsibility range identity, the explicit agent session address it belongs
// to, the application's own episode boundary it belongs to and the native
// input messages ready for its first decision.
//
// The redline list (4 rules):
// MUST read the episode boundary from this input alone.
// MUST keep the input in the order the caller admits it.
// MUST NOT default the address to a most-recent session.
// MUST NOT derive an address or an episode from a session or a trace.
type InvokeRequest struct {
	// InvocationID identifies the responsibility range.
	InvocationID event.InvocationID
	// Address is the explicit agent session address of the range; it is never
	// defaulted to a most-recent session.
	Address event.Address
	// Episode names the application's own episode boundary the range belongs
	// to. It is read from this input alone and never guessed from a session or
	// a trace: a range that names no episode records no episode.
	Episode event.EpisodeID
	// TriggerSource is the trigger-source stamp every input fact of the range
	// carries. An empty stamp records none, so an assembly that names no
	// lineage behaves exactly as it did before the stamp existed.
	TriggerSource string
	// Input is the native input of the range, in the order the caller admits it.
	Input []model.Message
}

// Run is the assembled runtime of the minimal entry: the shared faces, the
// sole publication entry, the execution loop over them, the fact ledger the
// tool results are recorded in, and the close and read faces the public SDK
// drives the runtime through. Every face it reads is read from the owner that
// already holds that fact, so the run keeps no second copy of the live scene;
// the one part of the scene it holds in its own right is the session transcript,
// which is the execution scene's read-out face rather than a copy of what an
// owner holds. The serializable scene is its facts beside its configuration,
// never a snapshot of the conversation body: the durable fact chain carries what
// survives a restart, and the body a later process reads back is recovered by
// replaying that chain instead of restoring a body snapshot kept beside the
// facts.
//
// The redline list (4 rules):
// MUST borrow through the sole publication entry and carry that generation's retirement drain.
// MUST read every face from the owner that already holds the fact.
// MUST NOT keep a second live copy of the scene inside the run.
// MUST NOT admit new input past the gate the close already closed.
type Run struct {
	mu          sync.Mutex
	instance    event.InstanceID
	current     state.StatePlan
	recovery    task.RestoreReport
	bindings    *state.Bindings
	publisher   *state.Publisher
	occupancy   *state.Occupancy
	authority   *state.Authority
	store       *state.MemoryStore
	inspector   state.Subject
	admission   *task.MemoryAdmission
	commit      *runCommit
	registry    *task.MemoryRegistry
	manager     *task.MemoryManager
	shutdown    *agent.Shutdown
	delegations *agent.Delegator
	watermark   meditation.WatermarkGate
	projection  *query.Projection
	explainer   *query.Explainer
	retrieval   *retrievalSupply
	tier        *ledger.Tier
	observed    *memory.MemoryEventQuery
	embedder    memory.Embedder
	loop        *agent.Loop
	sites       map[event.Address]bool
	facts       map[event.EventID]event.Event
	history     map[event.Address][]model.Message
	factSeq     uint64
	records     memoryPublications
	approver    task.Approver
	policy      plugin.Governor
	enforcement governance.Enforcement
	recorder    rl.Recorder
	observer    plugin.Observer
	presented   []view.Presentation
	snapshots   []RequestSnapshotRecord
	fixers      map[event.AgentID]*fileFixer
	replays     map[event.SessionID]replayEntry
	closing     bool
}

// Assemble builds the minimal public assembly of the runtime and publishes the
// first binding generation of the declaration before it returns: the shared
// faces are established in order, the tools of every agent are registered
// through the task wrapper, and the candidate passes the sole publication
// entry under the governance the assembly is configured with. A formal
// assembly carries no stand-in governance and is refused by name beside none.
// With a storage tier configured, the task facts persist through that tier and
// the chains it already holds are folded back into the registry before the
// assembly returns. A construction or publication failure frees only what that
// candidate acquired and returns the failure as the cause.
//
// The loop borrows through the sole publication entry, so every borrow carries
// the retirement drain of the generation it took.
// The redline list (4 rules):
// MUST establish the shared faces in order and publish the first binding generation before it returns.
// MUST free only what the failed candidate acquired.
// MUST NOT carry a stand-in governance in a formal assembly.
// MUST NOT return a half-assembled run beside a publication failure.
func Assemble(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, opts ...Option) (*Run, error) {
	if decl == nil {
		return nil, errors.New("tagent: the assembly carries no declaration")
	}
	var cfg assemblyConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.governance == nil {
		return nil, ErrGovernanceMissing
	}
	r := &Run{
		instance:    newInstanceID(),
		bindings:    state.NewBindings(),
		occupancy:   state.NewOccupancy(),
		inspector:   inspectorSubject,
		admission:   task.NewMemoryAdmission(),
		sites:       map[event.Address]bool{},
		facts:       map[event.EventID]event.Event{},
		history:     map[event.Address][]model.Message{},
		fixers:      map[event.AgentID]*fileFixer{},
		replays:     map[event.SessionID]replayEntry{},
		approver:    cfg.approver,
		policy:      cfg.policy,
		enforcement: cfg.enforcement,
		recorder:    cfg.recorder,
		observer:    cfg.observer,
	}
	if cfg.reflectionAnchors != "" {
		anchors, err := meditation.NewAnchorStore(cfg.reflectionAnchors)
		if err != nil {
			return nil, err
		}
		r.watermark = anchors
	}
	r.authority = state.NewAuthority()
	r.store = state.NewMemoryStore(r.authority)
	var durable *state.DurableCommit
	var tier *ledger.Tier
	if cfg.storage != nil {
		opened, err := ledger.OpenTier(ctx, *cfg.storage)
		if err != nil {
			return nil, err
		}
		tier = opened
		assemblyInfo("tagent: storage tier opened tier=%s durable=%t root=%s",
			tier.Capabilities.Tier, tier.Capabilities.Durable, cfg.storage.Root)
		if !tier.Capabilities.Durable {
			assemblyWarn("tagent: storage tier keeps no record across process exit tier=%s restored=none",
				tier.Capabilities.Tier)
		}
		durable, err = state.NewDurableCommit(ledger.TierFactLog(tier), nil)
		if err != nil {
			return nil, err
		}
		r.commit = &runCommit{inner: durable, held: map[event.TaskID]bool{}}
	} else {
		r.commit = &runCommit{inner: task.NewMemoryCommit(nil), held: map[event.TaskID]bool{}}
	}
	r.registry = task.NewMemoryRegistry(r.commit, r.admission)
	r.manager = task.NewMemoryManager(r.registry, nil, task.WithEnvelopeDeliverer(r.deliver))
	r.shutdown = agent.NewShutdown(closeFace{manager: r.manager, registry: r.registry})
	r.delegations = agent.NewDelegator(r.shutdown, r.bindings, r.instance)
	r.shutdown.SetReconciler(r.delegations)
	r.publisher = state.NewPublisher(r.bindings, publishContract, cfg.governance, &r.records)
	r.loop = agent.NewLoop(r.publisher, r.occupancy, r.instance, r.resolveFact)
	r.loop.SetHistory(r)
	r.loop.SetWatch(r.shutdown.Watch)
	r.loop.SetPresentations(r.takePresentation)
	r.loop.SetFactCommit(r.commitFact)
	r.loop.SetFactPipelines(r.factPipeline)
	r.loop.SetRequestFace(r.requestBoundary)
	r.loop.SetRequestSnapshotFace(r.snapshotRequest)
	if cfg.recorder != nil {
		r.loop.SetRecorder(rl.NewRecorderAdapter(cfg.recorder))
	}
	inventory := query.NewInventory(runReading{run: r})
	r.projection = query.NewProjection(inventory, projectionWindow)
	r.explainer = query.NewExplainer(inventory, ExecLeaseExplainer(cfg.execManager))
	if _, err := r.Publish(ctx, decl, agents); err != nil {
		return nil, err
	}
	if durable != nil {
		report, err := r.restoreChains(ctx, durable)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.recovery = report
		r.mu.Unlock()
	}
	if tier != nil {
		supply, err := supplyRetrieval(ctx, tier, cfg.embedder)
		if err != nil {
			return nil, err
		}
		if err := supply.searcher.SetRetrievalTuning(cfg.vectorTopK, cfg.keywordTopK, cfg.rrfK); err != nil {
			return nil, err
		}
		observed, err := seedObservedEvents(ctx, tier)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.retrieval = supply
		r.tier = tier
		r.observed = observed
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.embedder = cfg.embedder
	r.mu.Unlock()
	return r, nil
}

// Publish runs one declaration's candidate binding through the sole
// publication entry and reports how the attempt took effect. The candidate
// records the baseline of the entry at construction and carries the canonical
// fingerprint of the declaration, so a byte-identical reload publishes nothing
// while a changed declaration takes effect as the generation a request that
// begins executing afterwards borrows.
//
// The configuration and lease dimensions of the diagnostic read take the
// account the publication just admitted, so the read source follows the owner
// of the live scene instead of a static copy or an empty ledger.
func (r *Run) Publish(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent) (state.PublishOutcome, error) {
	if decl == nil {
		return state.PublishOutcome{}, errors.New("tagent: the candidate carries no declaration")
	}
	fingerprint, err := config.Fingerprint(*decl)
	if err != nil {
		return state.PublishOutcome{}, err
	}
	plan := state.OrgPlan{
		Declaration: decl,
		Baseline:    r.publisher.Baseline(),
		Fingerprint: fingerprint,
	}
	cand, err := prepare(ctx, plan, r.suppliers(decl, agents))
	if err != nil {
		return state.PublishOutcome{}, err
	}
	outcome, err := r.publisher.Publish(ctx, cand)
	if err == nil {
		r.loop.SetFinalRound(finalRoundPolicy(decl))
		r.mu.Lock()
		r.current = cand.Plan
		r.mu.Unlock()
	}
	return outcome, err
}

// finalRoundPolicy maps the watchdog budget a declaration names for its entry
// agent onto the policy every drive of this run runs under. The closing round
// stays opt-in: a declaration that names no budget yields the zero policy,
// which keeps every drive unbounded.
func finalRoundPolicy(decl *config.Declaration) agent.FinalRoundPolicy {
	spec := finalRoundSpecOf(decl)
	return agent.FinalRoundPolicy{
		MaxRounds: spec.MaxRounds,
		Budget:    time.Duration(spec.BudgetSeconds * float64(time.Second)),
		Timeout:   time.Duration(spec.TimeoutSeconds * float64(time.Second)),
		Prompt:    spec.Prompt,
	}
}

// specOfLocked reports the spec one declaration names for an agent under the
// run lock: a declaration that names no such agent reports the zero spec, so a
// switch an agent does not declare keeps the assembly as it stood.
func specOfLocked(decl *config.Declaration, id event.AgentID) config.AgentSpec {
	if decl == nil {
		return config.AgentSpec{}
	}
	for _, spec := range decl.Agents {
		if event.AgentID(spec.ID) == id {
			return spec
		}
	}
	return config.AgentSpec{}
}

// agentSpecOf reports the spec the declaration in effect names for one agent.
func (r *Run) agentSpecOf(id event.AgentID) config.AgentSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return specOfLocked(r.current.Plan.Declaration, id)
}

// finalRoundSpecOf reports the watchdog budget a declaration names for its entry
// agent; a declaration naming no entry agent or no budget reports the zero
// budget.
func finalRoundSpecOf(decl *config.Declaration) config.FinalRoundSpec {
	for _, spec := range decl.Agents {
		if spec.ID == decl.Entry {
			return spec.FinalRound
		}
	}
	return config.FinalRoundSpec{}
}

// restoreChains folds every durable fact chain the storage already holds back
// into the registry of this assembly under the current orchestration binding,
// one outcome per task the chains cover. A chain carrying no admission
// material is refused by name in the report and rebuilds nothing.
//
// The whole fold is observed as one line at INFO — the chains read, the tasks
// rebuilt and the chains dropped (a chain the restore refused, or a link the
// registry already held) beside the elapsed time — so a restart states what it
// recovered instead of leaving the reader to count report entries. A failed fold
// is observed at ERROR beside the chain it stopped on.
func (r *Run) restoreChains(ctx context.Context, durable *state.DurableCommit) (task.RestoreReport, error) {
	started := time.Now()
	owners := runOwners{bindings: r.bindings}
	merged := task.RestoreReport{}
	chains := durable.TaskIDs()
	for _, taskID := range chains {
		chain, err := durable.Records(ctx, taskID)
		if err != nil {
			assemblyError("tagent: restore stopped task=%s err=%v", taskID, err)
			return merged, err
		}
		report, err := r.registry.Restore(ctx, chain, owners)
		merged.Tasks = append(merged.Tasks, report.Tasks...)
		if err != nil {
			assemblyError("tagent: restore stopped task=%s err=%v", taskID, err)
			return merged, err
		}
		for _, outcome := range report.Tasks {
			if outcome.Recovered {
				r.commit.remember(outcome.TaskID)
			}
		}
	}
	recovered, dropped := restoredCounts(merged)
	assemblyInfo("tagent: restore folded chains=%d recovered=%d dropped=%d elapsed=%s",
		len(chains), recovered, dropped, time.Since(started).Round(time.Microsecond))
	return merged, nil
}

// restoredCounts reports how many tasks the fold rebuilt and how many outcomes
// ended without a rebuild: a chain the restore refused and a link the registry
// already held are both counted as dropped, so the two halves always add up to
// the number of outcomes.
func restoredCounts(report task.RestoreReport) (recovered, dropped int) {
	for _, outcome := range report.Tasks {
		if outcome.Recovered {
			recovered++
			continue
		}
		dropped++
	}
	return recovered, dropped
}

// Recovery reports how the assembly folded the durable chains its storage
// already held at startup: one outcome per task the chains covered. The report
// is empty for an assembly beside no storage.
func (r *Run) Recovery() task.RestoreReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return task.RestoreReport{Tasks: append([]task.RestoreOutcome(nil), r.recovery.Tasks...)}
}

// newInstanceID mints the unguessable identity of one assembled runtime.
func newInstanceID() event.InstanceID {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("tagent: mint the runtime instance identity: " + err.Error())
	}
	return event.InstanceID(hex.EncodeToString(buf[:]))
}

// assemblyInfo renders one line of the assembly face at INFO: the storage tier
// the assembly opened and what the fold of the durable chains recovered from it.
// One assembly writes one line per event, so the INFO stream is bounded by the
// assemblies a process builds.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed assembly.
// MUST write one line per opened tier and one per completed fold.
// MUST NOT render the stored records, the prompts or the model output.
func assemblyInfo(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Infof(format, args...)
}

// assemblyWarn renders one line of the assembly face at WARN: the tier the
// assembly opened holds nothing across process exit, so the run starts beside no
// chain and restores nothing. The claim a durable tier would let the process
// make is weaker, and a quieter level would leave that change unsaid.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed assembly.
// MUST write one line per tier that guarantees no durability.
// MUST NOT render the stored records, the prompts or the model output.
func assemblyWarn(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Warnf(format, args...)
}

// assemblyError renders one line of the assembly face at ERROR: a fold of the
// durable chains that stopped before it covered every chain. The assembly is
// refused beside it, so nothing runs on a partially folded scene.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a changed refusal.
// MUST write one line per fold that stopped.
// MUST NOT render the stored records, the prompts or the model output.
func assemblyError(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Errorf(format, args...)
}

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
	enforcement governance.Enforcement
	storage     *ledger.StorageConfig
	embedder    memory.Embedder
	// vectorTopK and keywordTopK bound the two retrieval legs; rrfK is the
	// rank constant their fusion scores under. The zero value keeps the legs
	// unbounded and the canonical constant, so an untuned assembly behaves
	// exactly as it did before the tuning existed.
	vectorTopK  int
	keywordTopK int
	rrfK        int
	recorder    rl.Recorder
	observer    plugin.Observer
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
func WithEnforcement(grade governance.Enforcement) Option {
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
func WithTelemetry(observer plugin.Observer) Option {
	return func(c *assemblyConfig) { c.observer = observer }
}

// WithRetrievalTuning bounds the two retrieval legs and names the rank
// constant their fusion scores under. A zero bound keeps that leg unbounded
// and a zero constant keeps search.RRFRankConstant, so the zero value is the
// untuned retrieval exactly as it stood.
func WithRetrievalTuning(vectorTopK, keywordTopK, rrfK int) Option {
	return func(c *assemblyConfig) {
		c.vectorTopK = vectorTopK
		c.keywordTopK = keywordTopK
		c.rrfK = rrfK
	}
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

// ErrObservationUnwired reports an observation read attempted on a run
// assembled beside no storage tier: the face is refused by name rather than
// answered as an empty chain.
var ErrObservationUnwired = errors.New("tagent: the run is assembled beside no observable storage tier")

// ErrCommittedFactMissing reports a fact identity this run holds neither in
// its live ledger nor in the storage tier it was assembled over.
var ErrCommittedFactMissing = errors.New("tagent: the committed fact is not held by this run")

// establish makes the explicit address a site of this instance and grants the
// diagnostic read face of the run a read of it, so an address is established
// exactly once whatever door admitted it.
func (r *Run) establish(ctx context.Context, addr event.Address) error {
	if !addr.IsComplete() {
		return fmt.Errorf("tagent: the invocation address is incomplete: %v", addr)
	}
	r.mu.Lock()
	closing, established := r.closing, r.sites[addr]
	r.mu.Unlock()
	if closing {
		return agent.ErrShutdownRefused
	}
	if established {
		return nil
	}
	if _, err := r.store.Establish(ctx, addr, r.instance); err != nil {
		return err
	}
	if err := r.authority.GrantRead(ctx, r.inspector, addr); err != nil {
		return err
	}
	r.mu.Lock()
	r.sites[addr] = true
	r.mu.Unlock()
	return nil
}

// InjectEvent commits one explicit input event of the named responsibility
// range and drives the loop session of its address with it, exactly as the
// Invoke entry drives a range the host admitted. The caller owns the event's
// address and lineage; this entry stamps only the identities that scope it to
// this run and range, and reports the identity the fact committed under.
func (r *Run) InjectEvent(ctx context.Context, evt event.Event, invocationID event.InvocationID) (agent.Turn, event.EventID, error) {
	if !evt.Owner.IsComplete() {
		return agent.Turn{}, "", fmt.Errorf("tagent: the injected event address is incomplete: %v", evt.Owner)
	}
	if invocationID == "" {
		return agent.Turn{}, "", errors.New("tagent: the injected event carries no invocation identity")
	}
	if err := r.establish(ctx, evt.Owner); err != nil {
		return agent.Turn{}, "", err
	}
	if evt.Source.IsZero() {
		evt.Source = evt.Owner
	}
	if evt.Target.IsZero() {
		evt.Target = evt.Owner
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	evt.InvocationID = invocationID
	evt.InstanceID = r.instance
	key := r.commitFact(evt)
	evt.Key = key
	turn, err := r.loop.Drive(ctx, agent.Invocation{
		InvocationID: invocationID,
		Address:      evt.Owner,
		Instance:     r.instance,
		Ready:        []event.Event{evt},
	})
	return turn, key, err
}

// ReflectionGate reports the watermark gate the reflection face of this run
// reads, or nil when the assembly was configured beside none. The gate is the
// one anchor store the assembly built, so a reflection never opens a second
// store over the same record.
func (r *Run) ReflectionGate() meditation.WatermarkGate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.watermark
}

// ObservedEvents answers one query over the fact chain this run can observe:
// the events its storage tier already committed, merged with every fact this
// process committed, landed idempotently under the identity each fact carries.
// A run assembled beside no storage tier refuses the read by name.
func (r *Run) ObservedEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	r.mu.Lock()
	observed := r.observed
	facts := make([]event.Event, 0, len(r.facts))
	for _, evt := range r.facts {
		facts = append(facts, evt)
	}
	r.mu.Unlock()
	if observed == nil {
		return nil, ErrObservationUnwired
	}
	for _, evt := range facts {
		record, held := eventRecordOf(evt)
		if !held {
			continue
		}
		if err := observed.Store(ctx, record); err != nil {
			return nil, err
		}
	}
	return observed.QueryEvents(ctx, opts)
}

// CommittedEvent reports the fact an identity names: the live fact of this
// process when it holds one, and otherwise the event the storage tier already
// committed. A committed body carries the light fields and no trigger source,
// so a fact restored from the tier carries the lineage its bytes carry and
// never an invented one.
func (r *Run) CommittedEvent(ctx context.Context, id event.EventID) (event.Event, error) {
	if id == "" {
		return event.Event{}, fmt.Errorf("%w: the identity is empty", ErrCommittedFactMissing)
	}
	r.mu.Lock()
	evt, held := r.facts[id]
	tier := r.tier
	r.mu.Unlock()
	if held {
		return evt, nil
	}
	if tier == nil {
		return event.Event{}, fmt.Errorf("%w: %s", ErrCommittedFactMissing, id)
	}
	stored, found, err := tier.Segments.GetEvent(ctx, id)
	if err != nil {
		return event.Event{}, err
	}
	if !found {
		return event.Event{}, fmt.Errorf("%w: %s", ErrCommittedFactMissing, id)
	}
	body, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return event.Event{}, err
	}
	meta := map[string]string{}
	if body.Summary != "" {
		meta[event.MetaKeyEventSummary] = body.Summary
	}
	return event.Event{
		Key:       id,
		Type:      body.Type,
		Timestamp: time.UnixMilli(body.Timestamp),
		Meta:      meta,
		Message:   model.Message{Content: body.Content},
		Owner:     body.Owner,
	}, nil
}

// seedObservedEvents lands every event the storage tier already committed into
// the observation table this run merges its live facts into: the body decodes
// under the schema, including the earlier generation an older writer left, and
// the record keeps the partition and window the segment index committed it
// under.
func seedObservedEvents(ctx context.Context, tier *ledger.Tier) (*memory.MemoryEventQuery, error) {
	observed := memory.NewMemoryEventQuery()
	events, err := tier.Segments.Events(ctx)
	if err != nil {
		return nil, err
	}
	for _, stored := range events {
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return nil, fmt.Errorf("tagent: observe the committed event %s: %w", stored.Key, err)
		}
		if err := observed.Store(ctx, body.Record(stored.PartitionID, stored.WindowTS)); err != nil {
			return nil, err
		}
	}
	return observed, nil
}

// eventRecordOf projects one committed fact onto the query record the
// observation table reads it under: the partition its owning agent resolves to,
// its frozen type, its summary and content, and its event time on the
// millisecond axis. A fact naming no owning agent belongs to no partition and
// is not observed rather than filed under an invented one.
func eventRecordOf(evt event.Event) (memory.EventRecord, bool) {
	if evt.Key == "" {
		return memory.EventRecord{}, false
	}
	agent := agentOf(evt)
	if agent == "" {
		return memory.EventRecord{}, false
	}
	timestamp := evt.Timestamp.UnixMilli()
	return memory.EventRecord{
		Key:         evt.Key,
		PartitionID: memory.PartitionIDFromName(string(agent)),
		Type:        evt.Type,
		Summary:     evt.Meta[event.MetaKeyEventSummary],
		Content:     evt.Message.Content,
		Timestamp:   timestamp,
		WindowTS:    timestamp,
	}, true
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

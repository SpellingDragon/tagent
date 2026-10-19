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
	"sync"

	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/state"
	"github.com/SpellingDragon/tagent/task"
	"github.com/SpellingDragon/tagent/telemetry"
	"github.com/SpellingDragon/tagent/view"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
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
	loop        *agent.Loop
	sites       map[event.Address]bool
	facts       map[event.EventID]event.Event
	history     map[event.Address][]model.Message
	factSeq     uint64
	records     memoryPublications
	approver    task.Approver
	policy      plugin.Governor
	enforcement plugin.Enforcement
	recorder    rl.Recorder
	observer    telemetry.Observer
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
		r.retrieval = supply
	}
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
	cand, err := prepare(ctx, plan, r.suppliers(agents))
	if err != nil {
		return state.PublishOutcome{}, err
	}
	outcome, err := r.publisher.Publish(ctx, cand)
	if err == nil {
		r.mu.Lock()
		r.current = cand.Plan
		r.mu.Unlock()
	}
	return outcome, err
}

// restoreChains folds every durable fact chain the storage already holds back
// into the registry of this assembly under the current orchestration binding,
// one outcome per task the chains cover. A chain carrying no admission
// material is refused by name in the report and rebuilds nothing.
func (r *Run) restoreChains(ctx context.Context, durable *state.DurableCommit) (task.RestoreReport, error) {
	owners := runOwners{bindings: r.bindings}
	merged := task.RestoreReport{}
	for _, taskID := range durable.TaskIDs() {
		chain, err := durable.Records(ctx, taskID)
		if err != nil {
			return merged, err
		}
		report, err := r.registry.Restore(ctx, chain, owners)
		merged.Tasks = append(merged.Tasks, report.Tasks...)
		if err != nil {
			return merged, err
		}
		for _, outcome := range report.Tasks {
			if outcome.Recovered {
				r.commit.remember(outcome.TaskID)
			}
		}
	}
	return merged, nil
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

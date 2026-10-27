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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory/ledger"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
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
	// Plugins lists the plugin instances the agent's binding generation
	// carries: the one Before pass grades and the After pass presents every call
	// of that generation over these instances, in the declared order.
	Plugins []plugin.Instance
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
	retrieval   *retrievalSupply
	tier        *ledger.Tier
	observed    *memory.MemoryEventQuery
	embedder    memory.Embedder
	loop        *agent.Loop
	sites       map[event.Address]bool
	facts       map[event.EventID]event.Event
	// factOrder keeps the commit order of the live facts, so a read of one
	// session's chain answers in the order that chain was written.
	factOrder []event.EventID
	// plugins holds the run-level instances every agent generation carries.
	plugins []plugin.Instance
	factSeq uint64
	// landing serializes the sequence slot of one partition beside the segment
	// commit that takes it, so two facts committed at once never claim one slot.
	landing sync.Mutex
	// landedSeq holds the last sequence slot each partition committed: it is
	// seeded from the events the tier already published, so a fact committed
	// after a restart takes the slot after the last committed one.
	landedSeq   map[int]int64
	records     memoryPublications
	approver    task.Approver
	policy      plugin.Governor
	enforcement governance.Enforcement
	// execManager is the execution resource ledger the exec face acquires
	// through and the lease explanation reads; a declaration that grants the
	// exec face beside no ledger is refused by name at call time.
	execManager *tagentexec.Manager
	// mcpServers is the live registry the MCP gateways read, mcpGateways the
	// fixed gateway tools over it and mcpBinder the binding of the declared
	// server generation onto it.
	mcpServers  *mcpgw.ServerRegistry
	mcpGateways *mcpgw.Gateways
	mcpBinder   *mcpgw.Binder
	recorder    rl.Recorder
	observer    plugin.Observer
	// leases holds the model leases the host attached per agent, in the shape
	// the assembly mounted them with: the lease that serves the agent, the
	// rebuild a replacement fact runs and the publication entry it installs
	// the rebuilt model through.
	leases map[event.AgentID]*leaseState
	// pendingModels holds the models a publication contributed for a declared
	// model change, so the replacement fact that publication judged has a
	// rebuild source even when the host named no explicit one.
	pendingModels map[event.AgentID]model.Model
	// leaseObservers holds the replacement consumer mounted on each leased
	// agent's pipeline: one fact->observation face per leased agent.
	leaseObservers map[event.AgentID]*rl.LeaseObserver
	// leasePublications records every version a lease installed through this
	// run's default publication entry, in installation order.
	leasePublications []rl.ModelIdentity
	// leaseLosses records every declared model change whose replacement fact
	// could not commit, so a failed judgment is readable beside the run.
	leaseLosses []error
	// capture is the trajectory capture this run fixes, admits and seals
	// through, or nil beside no capture. It is the one home of the safe request
	// views too: a decision's request is fixed once, through the capture's scope,
	// and the run's read face answers from that same account rather than beside a
	// second copy of its own.
	capture      rl.Capture
	captureScope rl.CaptureScope
	presented    []view.Presentation
	fixers       map[event.AgentID]*fileFixer
	// replays holds the mapped sequence every registered chain replays into a
	// request, keyed by the full address — the agent beside its session — so two
	// agents sharing a session id keep their projections apart; a registration
	// the AttachReplay seam made names no agent and is kept under the bare
	// session for every user of it.
	replays map[event.Address]replayEntry
	// ageFold folds a session's projection once its replay fills the window the
	// declaration gave it; it is mounted on every agent that declares a window.
	ageFold *AgeFoldPlugin
	closing bool
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
	if err := cfg.resolveMemory(); err != nil {
		return nil, err
	}
	for id, held := range cfg.leases {
		if held.lease == nil {
			return nil, fmt.Errorf("%w: agent %q", ErrModelLeaseMissing, id)
		}
		if !declaresAgent(decl, id) {
			return nil, fmt.Errorf("%w: agent %q", ErrModelLeaseUndeclared, id)
		}
	}
	r := &Run{
		instance:       newInstanceID(),
		bindings:       state.NewBindings(),
		occupancy:      state.NewOccupancy(),
		inspector:      inspectorSubject,
		admission:      task.NewMemoryAdmission(),
		sites:          map[event.Address]bool{},
		facts:          map[event.EventID]event.Event{},
		fixers:         map[event.AgentID]*fileFixer{},
		replays:        map[event.Address]replayEntry{},
		landedSeq:      map[int]int64{},
		approver:       cfg.approver,
		policy:         cfg.policy,
		enforcement:    cfg.enforcement,
		recorder:       cfg.recorder,
		observer:       cfg.observer,
		execManager:    cfg.execManager,
		capture:        cfg.capture,
		leases:         map[event.AgentID]*leaseState{},
		pendingModels:  map[event.AgentID]model.Model{},
		leaseObservers: map[event.AgentID]*rl.LeaseObserver{},
	}
	for id, held := range cfg.leases {
		lease := held
		r.leases[id] = &lease
	}
	r.ageFold = NewAgeFoldPlugin(ageFoldSessions{run: r})
	if cfg.reflectionAnchors != "" {
		anchors, err := meditation.NewAnchorStore(cfg.reflectionAnchors)
		if err != nil {
			return nil, err
		}
		r.watermark = anchors
	}
	if cfg.plugins != nil {
		instances, err := cfg.plugins(r)
		if err != nil {
			return nil, err
		}
		r.plugins = instances
	}
	r.authority = state.NewAuthority()
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
	if durable != nil {
		r.store = state.NewMemoryStore(r.authority, durable)
	} else {
		r.store = state.NewMemoryStore(r.authority)
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
	r.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		return r.commitFactChecked(context.Background(), evt)
	})
	r.loop.SetFactPipelines(r.factPipeline)
	r.loop.SetRequestFace(r.requestBoundary)
	if cfg.recorder != nil {
		r.loop.SetRecorder(rl.NewRecorderAdapter(cfg.recorder))
	}
	inventory := query.NewInventory(runReading{run: r})
	r.projection = query.NewProjection(inventory, projectionWindow)
	r.mcpServers = mcpgw.NewServerRegistry()
	r.mcpGateways = mcpgw.NewGateways(r.mcpServers, mcpgw.NewRegistryDiscovery())
	r.mcpBinder = mcpgw.NewBinder(r.mcpServers)
	if err := r.mountLeaseObservers(); err != nil {
		return nil, err
	}
	if err := r.mountCapture(); err != nil {
		return nil, err
	}
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
		landed, err := seedLandedSeq(ctx, tier)
		if err != nil {
			return nil, err
		}
		if err := r.seedReplay(ctx, tier); err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.retrieval = supply
		r.tier = tier
		r.observed = observed
		r.landedSeq = landed
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
	servers := declaredServersOf(decl)
	if err := mcpgw.ValidateDeclared(servers); err != nil {
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
	r.mu.Lock()
	previous := r.current
	r.mu.Unlock()
	outcome, err := r.publisher.Publish(ctx, cand)
	if err == nil {
		r.loop.SetFinalRound(finalRoundPolicy(decl))
		r.loop.SetAgentFinalRounds(agentFinalRoundPolicies(decl))
		r.mu.Lock()
		r.current = cand.Plan
		r.mu.Unlock()
		if err := r.bindMCPServers(ctx, servers); err != nil {
			return outcome, err
		}
		r.noteDeclaredModelChanges(ctx, previous, decl, agents)
	}
	return outcome, err
}

// declaredServersOf maps the server section of one declaration onto the named
// servers the live registry binds, in name order so one generation binds in one
// order across restarts.
func declaredServersOf(decl *config.Declaration) []mcpgw.DeclaredServer {
	names := make([]string, 0, len(decl.Servers))
	for name := range decl.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	servers := make([]mcpgw.DeclaredServer, 0, len(names))
	for _, name := range names {
		spec := decl.Servers[name]
		servers = append(servers, mcpgw.DeclaredServer{
			Name:      name,
			Transport: spec.Transport,
			URL:       spec.URL,
			APIKeyEnv: spec.APIKeyEnv,
		})
	}
	return servers
}

// bindMCPServers moves the live MCP registry onto the generation the published
// declaration names: a server the generation dropped stops answering, a server
// it added answers immediately, and a server whose declaration did not change
// keeps the connection it holds.
func (r *Run) bindMCPServers(ctx context.Context, servers []mcpgw.DeclaredServer) error {
	if r.mcpBinder == nil {
		return mcpgw.ErrRegistryMissing
	}
	if err := r.mcpBinder.Bind(ctx, servers); err != nil {
		return fmt.Errorf("tagent: bind the declared MCP servers: %w", err)
	}
	return nil
}

// CloseMCPServers releases every MCP server the live registry holds: the close
// closes each server's connection exactly once, waits for the calls still in
// flight and answers the release errors it accumulated. An embedding process
// calls it beside the close of the run; a repeated call releases nothing anew.
func (r *Run) CloseMCPServers() error {
	if r.mcpServers == nil {
		return nil
	}
	return r.mcpServers.Close()
}

// finalRoundPolicy maps the watchdog budget a declaration names for its entry
// agent onto the default policy the drives of this run run under. The closing
// round stays opt-in: a declaration that names no budget yields the zero policy,
// which keeps every drive unbounded.
func finalRoundPolicy(decl *config.Declaration) agent.FinalRoundPolicy {
	return finalRoundPolicyOf(finalRoundSpecOf(decl))
}

// agentFinalRoundPolicies maps the watchdog budget every declared agent names
// for itself onto the per-agent table of the loop: an agent that names no
// budget carries no entry, so its drives read the entry agent's default.
func agentFinalRoundPolicies(decl *config.Declaration) map[event.AgentID]agent.FinalRoundPolicy {
	policies := make(map[event.AgentID]agent.FinalRoundPolicy, len(decl.Agents))
	for _, spec := range decl.Agents {
		if spec.FinalRound == (config.FinalRoundSpec{}) {
			continue
		}
		policies[event.AgentID(spec.ID)] = finalRoundPolicyOf(spec.FinalRound)
	}
	return policies
}

// finalRoundPolicyOf maps one declared watchdog budget onto the policy the
// drives of its own agent run under.
func finalRoundPolicyOf(spec config.FinalRoundSpec) agent.FinalRoundPolicy {
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

// assemblyDebug renders one line of the assembly face at DEBUG: the delivery
// diagnoses a turn that ended without a deliverable body. The line names the
// session, so an operator reading the log can tell a silent turn from a broken
// delivery path without sampling the process.
func assemblyDebug(format string, args ...any) {
	if trpclog.Default == nil {
		return
	}
	defer func() { _ = recover() }()
	trpclog.Default.Debugf(format, args...)
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
	hostSender MessageSender
	// deliveryRuler is the review plugin's delivery face the host consults
	// before it sends one turn body. A nil ruler keeps every delivery allowed.
	deliveryRuler plugin.DeliveryRuler
	governance    state.Governance
	approver      task.Approver
	policy        plugin.Governor
	// approvals is the approval book the host opens the declared risk table
	// over: the policy that grades the document's risk_rules registers the
	// held call there, so an external approver rules the very request.
	approvals   *approval.ApprovalManager
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
	// plugins builds the run-level plugin instances every agent's binding
	// generation carries: the event-driven faces that observe every commit of
	// the run, whoever made it.
	plugins func(*Run) ([]plugin.Instance, error)
	// memory is the merged memory declaration the assembly opens: the storage
	// tier, the embedding binding and the retrieval tuning the declaration
	// names. An option that states one of those faces directly keeps it, so a
	// host that wires its own embedder is never overridden.
	memory *MemoryPlan
	// leases holds the model leases the host attached per agent: the lease
	// that serves the agent beside the rebuild and the publication entry a
	// replacement fact runs. A leased agent's binding carries the lease's
	// stable model handle, so one rebuilt version serves the requests that
	// begin after it was installed.
	leases map[event.AgentID]leaseState
	// capture is the trajectory capture the assembly attaches to the loop.
	capture rl.Capture
}

// Option configures the formal assembly at construction. It is the assembly
// name of the host option: one config struct, so the manual and the host entry
// accept the same constructors.
type Option = HostOption

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

// WithApprovals attaches the approval book a host document's risk table grades
// through: a document that declares risk_rules is opened as a policy over this
// book, so a call at the critical grade registers one approval request and
// parks in the approval wait, and the book stays the one external approvers
// rule through. An assembly beside no book keeps the named refusal of the
// critical grade instead of parking anything.
func WithApprovals(approvals *approval.ApprovalManager) Option {
	return func(c *assemblyConfig) { c.approvals = approvals }
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

// WithMemoryPlan opens the merged memory declaration of one organization: the
// storage tier every declaring agent shares, the embedding binding its recall
// vocabulary generates its vectors under and the retrieval tuning its engine
// names. The plan is resolved once, so the storage face and the retrieval face
// of this assembly run over the one binding it built. An assembly beside no
// plan keeps the faces its other options state, exactly as it did before the
// plan existed.
func WithMemoryPlan(plan *MemoryPlan) Option {
	return func(c *assemblyConfig) { c.memory = plan }
}

// WithReflectionAnchors points the reflection face of this assembly at the
// anchor record its watermark is read from: the store owns that record and
// answers the watermark the diagnostic reading reports. An assembly beside no
// path carries no reflection anchor store, and its reflection face reports that
// absence instead of a watermark of its own.
func WithReflectionAnchors(path string) Option {
	return func(c *assemblyConfig) { c.reflectionAnchors = path }
}

// WithPlugins builds the run-level plugin instances every agent generation
// carries: an event-driven face attached here observes every commit of the run,
// whichever agent session committed it, exactly as a contributed instance
// observes the commits of its own agent. The builder runs once, after the
// watermark gate is established and before the first publication, and the
// instances it answers are never replaced.
func WithPlugins(build func(*Run) ([]plugin.Instance, error)) Option {
	return func(c *assemblyConfig) { c.plugins = build }
}

// WithExecManager attaches the execution resource ledger this assembly acquires
// through and explains its registered leases by: a declaration that grants the
// exec face acquires through this ledger under the monitor rhythm its own
// declaration fixes, and the blocker explanation read of the run reports each
// registration beside the retention facts the ledger records under its key. An
// assembly beside no ledger still explains its task and notification blockers,
// refuses to explain a registered lease by name, and refuses a call of the
// granted exec face by name instead of starting anything.
func WithExecManager(manager *tagentexec.Manager) Option {
	return func(c *assemblyConfig) { c.execManager = manager }
}

// ExecState reports the retained state of one execution resource the ledger of
// this run holds: the lease deadline, the renewals observed after the acquiring
// call returned and the stop the resource was last seen under. The read is the
// diagnostic face of the same ledger the exec face acquires through, so it
// moves nothing; a run assembled beside no ledger refuses the read by name
// instead of answering an empty state.
func (r *Run) ExecState(key tagentexec.ResourceKey) (tagentexec.Explanation, error) {
	if r == nil || r.execManager == nil {
		return tagentexec.Explanation{}, tagentexec.ErrUnwired
	}
	return r.execManager.Explain(key)
}

// execOwner reports the responsibility range one execution call belongs to: the
// explicit address the call frame carries and the task identity the task face
// registered for that call's identity. A call beside no frame, an incomplete
// address or an identity the admission never registered answers false, so the
// resource is still tracked but its lifecycle events drive no follow-up turn.
func (r *Run) execOwner(ctx context.Context) (tagentexec.Owner, bool) {
	if r == nil || r.admission == nil {
		return tagentexec.Owner{}, false
	}
	frame, ok := task.CallFrameFrom(ctx)
	if !ok || !frame.Address.IsComplete() {
		return tagentexec.Owner{}, false
	}
	taskID, ok := r.admission.Lookup(frame.Identity.DedupKey())
	if !ok || taskID == "" {
		return tagentexec.Owner{}, false
	}
	return tagentexec.Owner{Address: frame.Address, TaskID: taskID}, true
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
// address and lineage; this entry backfills only the stamps the caller left
// unstated — an unstated source and target read as the owner of the event, an
// unstated invocation identity as the one this call names — while the runtime
// instance stamp is always this run's own and never the caller's to write. The
// fact commits under the identity the event carries after the backfill, and
// the drive runs the range under that same identity.
func (r *Run) InjectEvent(ctx context.Context, evt event.Event, invocationID event.InvocationID) (agent.Turn, event.EventID, error) {
	if !evt.Owner.IsComplete() {
		return agent.Turn{}, "", fmt.Errorf("tagent: the injected event address is incomplete: %v", evt.Owner)
	}
	if invocationID == "" && evt.InvocationID == "" {
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
	if evt.InvocationID == "" {
		evt.InvocationID = invocationID
	}
	evt.InstanceID = r.instance
	key, err := r.commitFactChecked(ctx, evt)
	if err != nil {
		return agent.Turn{}, "", fmt.Errorf("tagent: commit the injected event: %w", err)
	}
	evt.Key = key
	turn, err := r.loop.Drive(ctx, agent.Invocation{
		InvocationID: evt.InvocationID,
		Address:      evt.Owner,
		Instance:     r.instance,
		Ready:        []event.Event{evt},
	})
	return turn, key, err
}

// CommitToSession commits one event into the fact chain of an explicit address,
// under a fresh invocation identity when the caller states none. It is the
// cross-session write face a reflection turn delivers a summary through: the
// event keeps the address it names, the commit is observed through the target
// agent's own published pipeline exactly as any other commit of this run, and
// an address the run cannot establish is refused by name.
//
// The commit is a write of this run, never a second store beside it: the fact
// lands in the same live ledger, on the same durable log and in the same mapped
// sequence as the target session's own input, so that session's next assembly
// reads it like any other external input. The address block of the event is
// backfilled, never overwritten: a source, target or owner the caller already
// stamped is kept as the caller wrote it, and only an unstated stamp reads as
// the target address of the commit.
func (r *Run) CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error) {
	if err := r.establish(ctx, addr); err != nil {
		return "", err
	}
	if evt.Type == "" {
		return "", errors.New("tagent: the cross-session event carries no frozen type")
	}
	if evt.Owner.IsZero() {
		evt.Owner = addr
	}
	if evt.Source.IsZero() {
		evt.Source = addr
	}
	if evt.Target.IsZero() {
		evt.Target = addr
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}
	evt.InstanceID = r.instance
	if evt.InvocationID == "" {
		evt.InvocationID = event.InvocationID(fmt.Sprintf("%s#delivery-%s", addr.SessionID, r.mintEventID()))
	}
	key, err := r.commitFactChecked(ctx, evt)
	if err != nil {
		return "", fmt.Errorf("tagent: commit the cross-session event: %w", err)
	}
	pipeline, err := r.factPipeline(addr.AgentID)
	if err != nil {
		return key, err
	}
	r.takePresentation(pipeline.After(ctx, key))
	return key, nil
}

// ActiveSessions reports the complete addresses this run has established,
// filtered by agent: the live bindings a cross-session delivery resolves
// against, ordered by agent and session so the same scene always answers the
// same list.
func (r *Run) ActiveSessions(agentID event.AgentID) []event.Address {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event.Address, 0, len(r.sites))
	for addr := range r.sites {
		if !addr.IsComplete() {
			continue
		}
		if agentID != "" && addr.AgentID != agentID {
			continue
		}
		out = append(out, addr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AgentID != out[j].AgentID {
			return out[i].AgentID < out[j].AgentID
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}

// SessionFacts reports the facts of one session in the order they committed:
// the chain one address wrote, as the run holds it — the facts this process
// committed beside every fact the storage tier already held at assembly, both
// read through the one ledger. A fact outside the session the address names is
// answered never.
func (r *Run) SessionFacts(addr event.Address) []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event.Event, 0, len(r.factOrder))
	for _, key := range r.factOrder {
		evt, held := r.facts[key]
		if !held || sessionOf(evt) != addr.SessionID {
			continue
		}
		if addr.AgentID != "" && agentOf(evt) != addr.AgentID {
			continue
		}
		copied := evt
		if evt.Meta != nil {
			copied.Meta = make(map[string]string, len(evt.Meta))
			for k, v := range evt.Meta {
				copied.Meta[k] = v
			}
		}
		out = append(out, copied)
	}
	return out
}

// CommittedFacts reports how many facts this run holds: the counter the metrics
// endpoint reports beside the turns and deliveries of the host.
func (r *Run) CommittedFacts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.facts)
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
// committed. A committed body carries every field the event committed with, so
// a fact restored from the tier carries the whole metadata projection, the
// native payload and the lineage its bytes carry and never an invented one.
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
	restored := body.Event()
	restored.Key = id
	return restored, nil
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

// seedReplay registers the mapped sequence every session chain already
// committed, so a restarted run presents the material the tier holds to its
// next request instead of reading a transcript no process carries any more.
// Every committed fact is held in the live ledger of this run beside the
// registration (see seedFact), so a projected entry resolves the fact it was
// minted from and the call-result pairing survives the restart. The whole
// registered sequence reads as ended material: a cold start has no range in
// flight, and the next committed input opens the first one, which
// moves the boundary without dropping a restored entry. A session whose
// declaration names no model window registers under the no-budget window
// exactly as the live attach path does, and a body the schema cannot decode
// fails the boot instead of silently dropping the material the tier holds.
func (r *Run) seedReplay(ctx context.Context, tier *ledger.Tier) error {
	events, err := tier.Segments.Events(ctx)
	if err != nil {
		return err
	}
	type sessionSeed struct {
		projection *replay.ReplayProjection
		window     FoldWindow
	}
	seeds := map[event.Address]*sessionSeed{}
	var order []event.Address
	for _, stored := range events {
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return fmt.Errorf("tagent: replay the committed event %s: %w", stored.Key, err)
		}
		evt := body.Event()
		evt.Key = stored.Key
		r.seedFact(evt)
		addr := replayAddressOf(evt)
		if addr.SessionID == "" {
			continue
		}
		entry, ok := mappedEntryOf(evt)
		if !ok {
			continue
		}
		seed, held := seeds[addr]
		if !held {
			r.mu.Lock()
			window := foldWindowOf(specOfLocked(r.current.Plan.Declaration, agentOf(evt)))
			r.mu.Unlock()
			seed = &sessionSeed{projection: replay.NewReplayProjection(), window: window}
			seeds[addr] = seed
			order = append(order, addr)
		}
		seed.projection.Append(ctx, entry)
		if err := restoreFoldForm(ctx, seed.projection, seed.window); err != nil {
			return err
		}
	}
	for _, addr := range order {
		seed := seeds[addr]
		if err := r.attachScopedReplay(addr, seed.window, seed.projection); err != nil {
			return err
		}
	}
	if len(order) > 0 {
		r.attachNoticeFeed()
	}
	return nil
}

// seedFact holds one fact the storage tier already committed in the live
// ledger of this run: the same map every commit of this process lands in, in
// the commit order the tier recorded. A restarted run then reads its facts
// back beside the fresh ones — the identity one projected entry was minted from
// resolves again, so the call-result pairing a request replays is recovered
// from the facts alone — and a re-commit of an identity the tier already holds
// is judged against the very fact it names instead of writing a second one.
func (r *Run) seedFact(evt event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.facts[evt.Key] = evt
	r.factOrder = append(r.factOrder, evt.Key)
}

// restoreFoldForm folds one replayed projection at the commit boundaries the
// chain recorded, exactly as the age fold plugin judges every live commit: the
// registered sequence then carries the fold form the process left instead of the
// raw sequence the fold had already consumed, so the first request after the
// restart reads the same summary the live process read. The fold is
// projection-derived: every judgment it re-derives is already a compress request
// on the chain, so the restore files nothing back and writes no fact of its own.
// The fold threshold is one identity of the window and never the registration
// gate: a window naming no tokens measures its fold against the default model
// window instead of folding nothing, so the restored chain keeps the fold form
// the age rules give it. A fold refused by the context fails the boot instead
// of silently leaving the material at one shape the chain never had.
func restoreFoldForm(ctx context.Context, projection *replay.ReplayProjection, window FoldWindow) error {
	if projection == nil {
		return nil
	}
	measured := window
	if measured.MaxTokens <= 0 {
		measured.MaxTokens = defaultFoldWindowTokens
	}
	_, err := projection.FoldByAge(ctx, replay.AgeFoldPlan{
		KeepRecent:      measured.KeepRecentTasks,
		VolumeThreshold: measured.VolumeThresholdBytes(),
	})
	return err
}

// defaultFoldWindowTokens is the model window a fold measures against when the
// declaration names no tokens: the fold threshold is the window's own identity,
// so a zero window reads the default measure instead of dissolving the fold.
const defaultFoldWindowTokens = 32000

// seedLandedSeq reports the last sequence slot every partition already
// committed, so the landing path of a restarted run continues the positions the
// tier holds instead of claiming one of them again.
func seedLandedSeq(ctx context.Context, tier *ledger.Tier) (map[int]int64, error) {
	events, err := tier.Segments.Events(ctx)
	if err != nil {
		return nil, err
	}
	landed := map[int]int64{}
	for _, stored := range events {
		if stored.Seq > landed[stored.PartitionID] {
			landed[stored.PartitionID] = stored.Seq
		}
	}
	return landed, nil
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

// ErrMemoryDeclaration reports a memory declaration that cannot be merged into
// one storage tier and one semantic engine: a declaration that names half a
// storage tier, two agents that name different tiers or engines, or an engine
// beside no tier at all.
var ErrMemoryDeclaration = errors.New("tagent: the memory declaration is inconsistent")

// ErrMemoryEngine reports a semantic engine declaration the core cannot
// assemble: an unknown backend, a negative retrieval bound, or an embedding
// connection that does not name what its provider needs.
var ErrMemoryEngine = errors.New("tagent: the memory engine declaration is invalid")

// memoryEngineBackend is the only engine backend the core wires: the semantic
// retrieval of the memory side. A declaration naming another backend is refused
// by name instead of being read as this one.
const memoryEngineBackend = "memory"

// MemoryPlan is the merged memory declaration of one organization: the one
// storage tier every declaring agent shares and the one semantic engine its
// recall vocabulary generates embeddings under. The plan is resolved once at
// assembly and caches the binding it built, so the storage face and the
// retrieval face of one run run over the one embedder.
type MemoryPlan struct {
	storage  ledger.StorageConfig
	hasStore bool
	engine   *config.EngineSpec
	embedder memory.Embedder
	built    bool
}

// MemoryPlanOf merges every agent's memory declaration into the one plan the
// assembly opens, beside the storage section the document states at its top
// level. Every declaring agent must name the same tier and the same engine: a
// shared store read under two different engines would answer one query two
// ways. An engine beside no tier is refused by name, because an embedding with
// nowhere to persist its vectors is no engine at all. A document that declares
// neither reports a nil plan, which wires no storage face and no embedder.
func MemoryPlanOf(decl *config.Declaration, storageType, storageRoot string) (*MemoryPlan, error) {
	if decl == nil {
		return nil, fmt.Errorf("%w: the assembly carries no declaration", ErrMemoryDeclaration)
	}
	plan := &MemoryPlan{}
	declaredBy := ""
	engineBy := ""
	for _, spec := range decl.Agents {
		id := spec.ID
		entry := spec.Memory
		storageType := strings.TrimSpace(entry.Type)
		storagePath := strings.TrimSpace(entry.Path)
		storageDeclared := storageType != "" || storagePath != ""
		if storageDeclared && (storageType == "" || storagePath == "") {
			return nil, fmt.Errorf("%w: agent %q names half a storage tier (type and path are both required)", ErrMemoryDeclaration, id)
		}
		if storageDeclared {
			candidate := ledger.StorageConfig{Type: ledger.StorageType(storageType), Root: storagePath}
			if !plan.hasStore {
				plan.storage = candidate
				plan.hasStore = true
				declaredBy = id
			} else if plan.storage != candidate {
				return nil, fmt.Errorf("%w: agents %q and %q name different storage tiers", ErrMemoryDeclaration, declaredBy, id)
			}
		}
		if declaresEngine(entry.Engine) {
			engine := entry.Engine
			if plan.engine == nil {
				plan.engine = &engine
				engineBy = id
			} else if *plan.engine != engine {
				return nil, fmt.Errorf("%w: agents %q and %q name different semantic engines", ErrMemoryDeclaration, engineBy, id)
			}
		}
	}
	topType := strings.TrimSpace(storageType)
	topRoot := strings.TrimSpace(storageRoot)
	if topType != "" {
		top := ledger.StorageConfig{Type: ledger.StorageType(topType), Root: topRoot}
		if plan.hasStore && plan.storage != top {
			return nil, fmt.Errorf("%w: the top-level storage disagrees with the memory tier agent %q declares", ErrMemoryDeclaration, declaredBy)
		}
		if !plan.hasStore {
			plan.storage = top
			plan.hasStore = true
		}
	}
	if !plan.hasStore && plan.engine == nil {
		return nil, nil
	}
	if !plan.hasStore {
		return nil, fmt.Errorf("%w: the semantic engine requires a storage tier", ErrMemoryDeclaration)
	}
	if plan.engine != nil {
		if err := validateEngine(*plan.engine); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

// Storage reports the storage tier the plan opens, or nil beside no tier.
func (p *MemoryPlan) Storage() *ledger.StorageConfig {
	if p == nil || !p.hasStore {
		return nil
	}
	cfg := p.storage
	return &cfg
}

// Engine reports the merged semantic engine, or nil beside no engine.
func (p *MemoryPlan) Engine() *config.EngineSpec {
	if p == nil {
		return nil
	}
	return p.engine
}

// Embedder reports the embedding binding the plan generates its vectors under:
// the core default implementation over the declared engine, built on the first
// call and cached so one run runs over one binding. A plan beside no engine
// reports no embedder, which is the keyword-only retrieval.
func (p *MemoryPlan) Embedder() (memory.Embedder, error) {
	if p == nil || p.engine == nil {
		return nil, nil
	}
	if p.built {
		return p.embedder, nil
	}
	built, err := embedder.New(p.engine.Embedding)
	if err != nil {
		return nil, err
	}
	p.embedder, p.built = built, true
	return built, nil
}

// Tuning reports the two leg bounds and the rank constant the merged engine
// declares. A plan beside no engine reports the zero tuning, which keeps the
// retrieval exactly as it stood.
func (p *MemoryPlan) Tuning() (int, int, int) {
	if p == nil || p.engine == nil {
		return 0, 0, 0
	}
	return p.engine.VectorTopK, p.engine.KeywordTopK, p.engine.RRFK
}

// resolveMemory folds the declared memory plan into the faces this assembly
// opens. A face an option states directly keeps it, so a host that wires its
// own embedder over a declared tier is never overridden. The declaration is the
// one source of the retrieval tuning: a zero bound keeps that leg unbounded and
// a zero constant keeps search.RRFRankConstant.
func (c *assemblyConfig) resolveMemory() error {
	if c.memory == nil {
		return nil
	}
	if c.storage == nil {
		c.storage = c.memory.Storage()
	}
	if c.embedder == nil {
		embedder, err := c.memory.Embedder()
		if err != nil {
			return err
		}
		c.embedder = embedder
	}
	c.vectorTopK, c.keywordTopK, c.rrfK = c.memory.Tuning()
	return nil
}

// declaresEngine reports whether a memory engine section names anything.
func declaresEngine(engine config.EngineSpec) bool {
	return strings.TrimSpace(engine.Backend) != "" ||
		strings.TrimSpace(engine.Embedding.Provider) != "" ||
		strings.TrimSpace(engine.Embedding.Model) != "" ||
		strings.TrimSpace(engine.Embedding.APIKeyEnv) != "" ||
		engine.Embedding.Dimensions != 0 ||
		strings.TrimSpace(engine.Embedding.APIEndpoint) != "" ||
		engine.VectorTopK != 0 || engine.KeywordTopK != 0 || engine.RRFK != 0
}

// validateEngine validates one merged semantic engine: the backend must be the
// one the core wires, the retrieval bounds must not be negative, and the
// embedding connection must name everything its provider needs.
func validateEngine(engine config.EngineSpec) error {
	backend := strings.TrimSpace(engine.Backend)
	if backend == "" {
		return fmt.Errorf("%w: the engine names no backend", ErrMemoryEngine)
	}
	if backend != memoryEngineBackend {
		return fmt.Errorf("%w: backend %q is not a known engine backend", ErrMemoryEngine, backend)
	}
	if engine.VectorTopK < 0 || engine.KeywordTopK < 0 || engine.RRFK < 0 {
		return fmt.Errorf("%w: the engine carries a negative retrieval bound (vector_top_k=%d keyword_top_k=%d rrf_k=%d)",
			ErrMemoryEngine, engine.VectorTopK, engine.KeywordTopK, engine.RRFK)
	}
	if err := embedder.Validate(engine.Embedding); err != nil {
		return fmt.Errorf("%w: %w", ErrMemoryEngine, err)
	}
	return nil
}

// ErrModelLeaseMissing and the lease wiring errors beside it name the way the
// model replacement wiring was refused, so a host routes each by name instead
// of matching on message text.
var (
	// ErrModelLeaseMissing reports a model lease option that names no lease: a
	// version in effect is never invented by the assembly.
	ErrModelLeaseMissing = errors.New("tagent: the model lease option names no lease")
	// ErrModelLeaseUndeclared reports a model lease attached to an agent the
	// declaration does not declare: the declaration is the authorization, so a
	// lease beside no declared agent is refused rather than mounted unwired.
	ErrModelLeaseUndeclared = errors.New("tagent: the model lease names an agent the declaration does not declare")
	// ErrModelChangeOwnerMissing reports a model change naming an agent this
	// run holds no lease for: no lease means no version to move, and the change
	// is refused by name rather than committed against nobody.
	ErrModelChangeOwnerMissing = errors.New("tagent: the model change names an agent this run holds no model lease for")
	// ErrModelChangeTargetMissing reports a model change that names no target
	// version: an unidentified version is never installed.
	ErrModelChangeTargetMissing = errors.New("tagent: the model change names no target version")
	// ErrModelChangeUncommitted reports a model change whose fact did not commit:
	// the judgment reached no ledger, so no observation can read it back.
	ErrModelChangeUncommitted = errors.New("tagent: the model change fact did not commit")
	// ErrCaptureUnscoped reports a capture this assembly cannot scope to one
	// request view: the loop hands a decision's view material per request, and a
	// capture beside no scope would have to invent the view it fixes.
	ErrCaptureUnscoped = errors.New("tagent: the capture cannot be scoped to one request view")
)

// leaseState is one agent's attached model lease at assembly: the lease that
// serves the agent, the rebuild a replacement fact runs and the publication
// entry the rebuilt model is installed through. A nil rebuild takes the model
// the publication that judged the change contributed; a nil publish records the
// installed identity beside the run and installs the version in the lease only.
type leaseState struct {
	lease   *rl.ModelLease
	rebuild func(version string) (model.Model, error)
	publish func(rl.ModelIdentity, model.Model) error
}

// ModelLeaseOption configures the model lease one agent is assembled with.
type ModelLeaseOption func(*leaseState)

// WithLeaseRebuild names the rebuild step a model replacement fact runs: the
// step receives the version the fact asks for and answers the model that
// version is installed as. A rebuild that fails installs no version, and the
// lease keeps the version in effect.
func WithLeaseRebuild(rebuild func(version string) (model.Model, error)) ModelLeaseOption {
	return func(state *leaseState) { state.rebuild = rebuild }
}

// WithModelLease attaches the model lease of one agent to this assembly: the
// agent's binding carries the lease's stable model handle instead of the model
// the host contributed, every committed model replacement fact owned by the
// agent is consumed by the lease observer mounted on its pipeline, and the
// lease's rebuild-publication-installation order decides whether the requested
// version takes effect. A rebuild and a publication entry are configured
// through the model lease options; a replacement beside no rebuild takes the
// model the publication that judged the change contributed.
//
// The lease agent must be declared by the assembly's declaration: the
// declaration is the authorization, and a lease beside no declared agent is
// refused by name.
func WithModelLease(agentID event.AgentID, lease *rl.ModelLease, opts ...ModelLeaseOption) Option {
	return func(c *assemblyConfig) {
		if c.leases == nil {
			c.leases = map[event.AgentID]leaseState{}
		}
		held := leaseState{lease: lease}
		for _, opt := range opts {
			opt(&held)
		}
		c.leases[agentID] = held
	}
}

// WithCapture attaches the trajectory capture this assembly fixes each request
// view through, admits each observed attempt through and seals each turn
// through. The capture must be scoped, because the loop hands a decision's view
// material per request and the capture fixes the snapshot against that view; a
// capture beside no scope is refused by name at assembly.
func WithCapture(capture rl.Capture) Option {
	return func(c *assemblyConfig) { c.capture = capture }
}

// declaresAgent reports whether one declaration declares the named agent.
func declaresAgent(decl *config.Declaration, agentID event.AgentID) bool {
	for _, spec := range decl.Agents {
		if event.AgentID(spec.ID) == agentID {
			return true
		}
	}
	return false
}

// leaseOf reports the lease state attached to one agent, or nil beside none.
// The map is written once at assembly and never after, and the lock keeps a
// concurrent publication from reading it mid-write.
func (r *Run) leaseOf(agentID event.AgentID) *leaseState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.leases[agentID]
}

// mountLeaseObservers mounts the replacement consumer of every attached lease
// on the agent's pipeline: one fact->observation face that reads a committed
// model replacement fact back and applies it through the lease's own rebuild,
// publication and installation order. A lease observer the run cannot build is
// refused before the first publication, so a half-wired assembly is never
// returned.
func (r *Run) mountLeaseObservers() error {
	for id, held := range r.leases {
		observer, err := rl.NewLeaseObserver(id, held.lease, runFacts{run: r}, r.rebuildLeaseModel(id), r.publishLease(id))
		if err != nil {
			return fmt.Errorf("tagent: mount the model lease observer of agent %q: %w", id, err)
		}
		r.leaseObservers[id] = observer
	}
	return nil
}

// rebuildLeaseModel reports the rebuild step one agent's lease replacement
// runs: the explicit rebuild the host named when it attached the lease, or the
// model the publication that judged the change contributed. The pending model
// is consumed once, so one contributed model installs one version and a second
// replacement beside none is refused by name.
func (r *Run) rebuildLeaseModel(agentID event.AgentID) func(version string) (model.Model, error) {
	return func(version string) (model.Model, error) {
		if held := r.leaseOf(agentID); held != nil && held.rebuild != nil {
			return held.rebuild(version)
		}
		r.mu.Lock()
		next, pending := r.pendingModels[agentID]
		if pending {
			delete(r.pendingModels, agentID)
		}
		r.mu.Unlock()
		if !pending || next == nil {
			return nil, fmt.Errorf("%w: no model is pending for agent %q version %q", rl.ErrLeaseRebuildFailed, agentID, version)
		}
		return next, nil
	}
}

// publishLease reports the publication entry one agent's lease replacement
// installs its rebuilt model through: the explicit entry the host named when it
// attached the lease, or the default entry that records the installed identity
// beside the run. The default entry installs nothing outside the lease, because
// the binding already carries the lease's stable model handle and the version
// in effect is what that handle borrows.
func (r *Run) publishLease(agentID event.AgentID) func(rl.ModelIdentity, model.Model) error {
	return func(identity rl.ModelIdentity, rebuilt model.Model) error {
		if held := r.leaseOf(agentID); held != nil && held.publish != nil {
			return held.publish(identity, rebuilt)
		}
		r.mu.Lock()
		r.leasePublications = append(r.leasePublications, identity)
		r.mu.Unlock()
		return nil
	}
}

// mountCapture attaches the trajectory capture to the loop: the run's capture
// is scoped to one request view per decision and adapted onto the loop's narrow
// capture face. A capture beside no scope is refused by name before the first
// publication.
func (r *Run) mountCapture() error {
	if r.capture == nil {
		return nil
	}
	scope, ok := r.capture.(rl.CaptureScope)
	if !ok {
		return fmt.Errorf("%w: %T", ErrCaptureUnscoped, r.capture)
	}
	adapter, err := rl.NewCaptureAdapter(scope)
	if err != nil {
		return err
	}
	r.loop.SetCapture(adapter)
	r.captureScope = scope
	return nil
}

// NoteModelChange files the judgment that one agent's model reference moves
// from one version to another and commits it as a model replacement fact on the
// agent's own pipeline, the explicit entry a host drives when its own
// configuration source observed the change; a publication that moves an
// agent's declared model reference files the same fact itself, with the
// configuration source stamp. The lease mounted on the agent runs the rebuild,
// publication and installation order and decides whether the version takes
// effect.
//
// The identity of the committed fact is returned; a change this run cannot
// file — no lease for the agent, no target version, no commit face — is refused
// by name, and an empty from version is the version the lease holds.
func (r *Run) NoteModelChange(agentID event.AgentID, from, to, source string) (event.EventID, error) {
	return r.noteModelReplacement(context.Background(), agentID, from, to, source)
}

// noteModelReplacement is the one filing path of a model replacement judgment:
// it resolves the lease of the owning agent, defaults the from version to the
// version in effect and the source to the configuration source, and commits the
// fact through the loop's commit face so the After pass of the owning agent's
// pipeline observes it exactly as the loop's own facts are observed.
func (r *Run) noteModelReplacement(ctx context.Context, agentID event.AgentID, from, to, source string) (event.EventID, error) {
	held := r.leaseOf(agentID)
	if held == nil || held.lease == nil {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeOwnerMissing, agentID)
	}
	if to == "" {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeTargetMissing, agentID)
	}
	if source == "" {
		source = event.ModelReplacementSourceConfig
	}
	if from == "" {
		from = held.lease.CurrentVersion()
	}
	fact := event.ModelReplacementFor(agentID, from, to, source, event.ModelReplacementResultRequested)
	key, commitErr := r.loop.CommitFactFor(ctx, agentID, fact.Event(time.Now().UTC()))
	if commitErr != nil {
		return "", fmt.Errorf("tagent: commit the model replacement fact of agent %q: %w", agentID, commitErr)
	}
	if key == "" {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeUncommitted, agentID)
	}
	return key, nil
}

// noteDeclaredModelChanges files the replacement judgments one successful
// publication implies: every leased agent whose declared model reference moved
// since the generation in effect is judged once, from the version its lease
// holds to the version the new reference names, with the configuration source
// stamp. The model the publication contributed for the agent is kept as the
// rebuild source when the host named no explicit one.
//
// The declared model reference is the configuration fingerprint of the model
// dimension: a byte-identical reload publishes nothing and judges nothing, and
// a changed reference is the change itself. A change whose fact cannot commit
// is recorded beside the run rather than read as a replacement that happened.
func (r *Run) noteDeclaredModelChanges(ctx context.Context, previous state.StatePlan, decl *config.Declaration, agents map[event.AgentID]NativeAgent) {
	if previous.Plan.Declaration == nil || decl == nil {
		return
	}
	before := make(map[event.AgentID]string, len(previous.Plan.Declaration.Agents))
	for _, spec := range previous.Plan.Declaration.Agents {
		before[event.AgentID(spec.ID)] = spec.Model
	}
	for _, spec := range decl.Agents {
		id := event.AgentID(spec.ID)
		if r.leaseOf(id) == nil || before[id] == spec.Model {
			continue
		}
		target := rl.ParseModelIdentity(spec.Model).Version
		if target == "" {
			continue
		}
		if native, contributed := agents[id]; contributed && native.Model != nil {
			r.mu.Lock()
			r.pendingModels[id] = native.Model
			r.mu.Unlock()
		}
		if _, err := r.noteModelReplacement(ctx, id, "", target, event.ModelReplacementSourceConfig); err != nil {
			r.mu.Lock()
			r.leaseLosses = append(r.leaseLosses, err)
			r.mu.Unlock()
		}
	}
}

// LeasePublications reports the versions the model leases installed through
// this run's default publication entry, in installation order, as a copy the
// caller owns.
func (r *Run) LeasePublications() []rl.ModelIdentity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]rl.ModelIdentity(nil), r.leasePublications...)
}

// LeaseLosses reports the declared model changes whose replacement fact could
// not commit, as a copy the caller owns: a judgment that reached no ledger is
// named here rather than read as a replacement that happened.
func (r *Run) LeaseLosses() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.leaseLosses...)
}

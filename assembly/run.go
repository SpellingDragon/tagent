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
package assembly

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
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
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/embedder"
	"github.com/SpellingDragon/tagent/memory/query"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"github.com/SpellingDragon/tagent/tool/subagent"

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
	// reflectionSession names the dedicated session whose request alone may see
	// the reflection-only tools; empty for an agent without host self-reflection.
	// These two fields carry the session-scoped tool face from the composition
	// root into the published binding, so it survives a reload unchanged.
	reflectionSession event.SessionID
	// reflectionOnly names the cross-session tools only reflectionSession may
	// see. Every other session of the agent has them filtered out of its request
	// face, so the entry tool face stays free of the channel-bypassing send.
	reflectionOnly []string
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
	// InputMeta carries the per-message metadata of Input, one entry per
	// message in the same order; a nil entry stamps nothing, so a caller that
	// names none behaves exactly as it did before the face existed.
	InputMeta []map[string]string
}

// writerLedger is the writer account of one run as its close reads it: the
// registration one production opens and the read of the ones that confirmed no
// stop. The loop builds and owns the ledger of its productions; the run only
// reads it through these two operations, so the mechanism never becomes a second
// public surface beside the productions it names.
type writerLedger interface {
	// Begin registers one production that is starting now and answers the
	// confirmation that closes its registration.
	Begin(name string) func()
	// Stopped reports whether every registered production confirmed its stop,
	// and names the ones that did not in registration order.
	Stopped() (bool, []string)
}

// continuationTable is the continuation account one run shares among the tools
// of its agents: the round a delegated task is served again by. The table itself
// is built inside the subagent package; the run only holds the one a re-entry
// claims and serves through, so the mechanism never becomes a public type here.
type continuationTable = interface {
	// Served reports whether the table still serves the named task a round.
	Served(taskID event.TaskID) bool
	// Release ends the continuation of one task.
	Release(taskID event.TaskID)
	// Reenter hands fresh input to the named task and serves its next round.
	Reenter(ctx context.Context, taskID event.TaskID, input string) error
	// Claim takes the one round slot of the named task.
	Claim(taskID event.TaskID) error
	// Abandon gives back a claimed slot without serving a round.
	Abandon(taskID event.TaskID)
	// Serve serves the next round of a task a caller already claimed.
	Serve(ctx context.Context, taskID event.TaskID, input string, after func(error)) error
	// Seed rebuilds the continuation of one task from the declaration its
	// delegated input fact carries, so a re-entry after a restart serves the next
	// round of a task this process never admitted in memory.
	Seed(taskID event.TaskID, declaration []byte, siteSession event.SessionID, builder subagent.Builder) error
}

// runMovement is the fence of one run: it reads the private sequences the
// accounts of the run advance — the task chain and its obligations, and the
// mailbox — through the accounts that own them. Either account moving changes
// the sum, so a judgment that unions their sets detects a write between its
// reads; a run beside no account reads the zero every instant equals. The
// sequence belongs to the account itself, so the fence never becomes a second
// authority beside it.
type runMovement struct {
	registry *task.MemoryRegistry
	store    *state.MemoryStore
}

// Advance records one write of the session chain this run keeps: the fact chain
// is an input round a consumer may still owe, so a committed fact moves the task
// account's sequence at the write point.
func (m runMovement) Advance() {
	if m.registry != nil {
		m.registry.Add(1)
	}
}

// Read reports the accounts of this run at the instant a rest judgment stands:
// the task chain's sequence beside the mailbox's, each read from the account
// that advances it.
func (m runMovement) Read() uint64 {
	var seq uint64
	if m.registry != nil {
		seq += m.registry.Load()
	}
	if m.store != nil {
		seq += m.store.Load()
	}
	return seq
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
	mu       sync.Mutex
	instance event.InstanceID
	current  state.StatePlan
	// natives is the construction contribution each declared agent's live pieces
	// were assembled from — the models and tools the embedding process supplies —
	// read beside the publication so a delegated round rebuilt after a restart
	// resolves the same model and tool references the generation in effect names.
	// It is configuration material, never a copy of the scene.
	natives   map[event.AgentID]NativeAgent
	recovery  task.RestoreReport
	bindings  *state.Bindings
	publisher *state.Publisher
	// observationEnabled records that the composition root wired the commit
	// observation face; guarded by mu like the faces it gates.
	observationEnabled bool
	occupancy          *state.Occupancy
	authority          *state.Authority
	store              *state.MemoryStore
	inspector          state.Subject
	admission          *task.MemoryAdmission
	commit             *runCommit
	registry           *task.MemoryRegistry
	manager            *task.MemoryManager
	// writers is the ledger of the productions this run started — the drives, the
	// model streams of their decisions and the tool productions of their calls —
	// and the close reads it as the writer-stopped criterion of the run. The loop
	// builds and owns it; the run reads it through the loop itself.
	writers     writerLedger
	delegations *agent.Delegator
	// progress pulses whenever the account behind this run moved: a fact
	// committed, a notification consumed, a segment closed, a production stopped.
	// The close waits on it instead of polling the criterion, so the wait is an
	// event and the close's own budget its only clock.
	progress chan struct{}

	// continuations is the account of the delegated rounds this run serves again
	// after a task of theirs settled and was reactivated: the first tool of the
	// run mints it and every later tool is built beside the same account, because
	// a re-entered round is a round of the same task the delegation ledger admits
	// and settles under. The run only holds the shared account by its two faces —
	// the tool parking a round and the host re-entry claiming it.
	continuations continuationTable
	watermark     meditation.WatermarkGate
	projection    *query.Projection
	retrieval     *retrievalSupply
	tier          *ledger.Tier
	embedder      memory.Embedder
	loop          *agent.Loop
	sites         map[event.Address]bool
	facts         map[event.EventID]event.Event
	// factOrder keeps the commit order of the live facts, so a read of one
	// session's chain answers in the order that chain was written.
	factOrder []event.EventID
	// controlFacts counts the live facts that are control-plane records of the
	// configuration chain instead of facts of a session chain, and controlScanned
	// is the point of factOrder that count has been brought up to. A trim shrinks
	// factOrder and recomputes both over the kept prefix (compactFactOrderLocked),
	// so the cursor stays a valid index and the count tracks the surviving records.
	controlFacts   int
	controlScanned int
	// sessionFactCount counts the live facts each session chain holds, so the
	// per-commit window trim stays O(1) until a chain can actually hold a cut.
	sessionFactCount map[event.SessionID]int
	// pinnedFacts names the facts the delivery account still owes a target: a
	// settlement fact of an unsettled obligation (owed or held) is exempt from
	// the hot window trim, so the read face of the obligation never falls out
	// of the window while the obligation stands. The set is pinned by the
	// delivery layer (host_delivery.go) and never by a commit, so a fact no
	// obligation names ages out on the ordinary trim.
	pinnedFacts map[event.EventID]struct{}
	// plugins holds the run-level instances every agent generation carries.
	plugins []plugin.Instance
	factSeq uint64
	// movement is the fence of this run: it reads the private sequences the
	// accounts of the run advance whenever they are written — the task chain and
	// its obligations, and the mailbox — through the accounts themselves, so a
	// judgment that spans them detects a write between its reads instead of
	// trusting one account's sequence as if the others moved with it. The run
	// keeps no sequence of its own.
	movement runMovement
	// holds is the waiting table of every approval channel this run parks a call
	// through: the one account of the waits a ruling of this process can still
	// resume, since a hold lives in memory alone and a wait restored from the
	// chain carries none.
	holds []*task.HoldBook
	// landing serializes the sequence slot of one partition beside the segment
	// commit that takes it, so two facts committed at once never claim one slot.
	landing sync.Mutex
	// checkpointMu serializes the framework checkpoint cuts of this run with each
	// other, so one account is stated by one cut: two cuts over one slot would
	// file two payloads under one identity. No commit takes it.
	checkpointMu sync.Mutex
	// flight is the quiet point of the fact pipeline: a commit holds it while it
	// lands and until the state it leaves its holder in, and the coordinator
	// drains it before it reads any state. The drain is what makes a cut state a
	// state without a lock over the commit stream — a lock that a holder's own
	// commit waits on from inside the state reading that asked for it.
	flight commitFence
	// states is the state seam of this run: the registry every consumer's
	// recoverable state slice is mounted on, aggregated into one checkpoint.
	states *state.StateRegistry
	// obligations is the account every landed fact is applied to (see
	// mountObligationAccount): the delivery account of the host mounted on this
	// run. A run beside no mounted account lands its facts without one.
	obligations func(event.Event)
	// checkpointEvery and checkpointPeriod are the two cadence dials of the
	// framework checkpoint: a busy chain files one after this many commits and a
	// quiet chain still files one after this long.
	checkpointEvery  int
	checkpointPeriod time.Duration
	// checkpointCommits counts the commits since the last checkpoint, and
	// checkpointAt is the moment it was filed: it opens at the assembly, so the
	// period measures a chain that has been committing rather than the wall clock
	// since the epoch.
	checkpointCommits int
	checkpointAt      time.Time
	// checkpointDue wakes the goroutine that files the carrier the cadence owes:
	// a commit that reaches the due signals it and takes no cut itself.
	checkpointDue chan struct{}
	// checkpointDuePending records the due a wake stands for, so a wake a cut
	// already answered files nothing.
	checkpointDuePending bool
	// checkpointStop ends the filer once the close of this run began, and the once
	// makes a repeated close stop it once.
	checkpointStop     chan struct{}
	checkpointStopOnce sync.Once
	// checkpointFiled is the fact key of the newest checkpoint this run filed or
	// restored, so a cadence that reaches the same cut files nothing twice.
	checkpointFiled event.EventID
	// checkpointOffset is the cut the newest checkpoint of this run states: the
	// partition-sequence watermark the restored state corresponds to.
	checkpointOffset event.CheckpointOffset
	// restoredProjection is the session survivors the newest checkpoint stated:
	// the boot rebuilds each chain from them instead of folding the covered
	// history, and it is emptied as the boot takes them.
	restoredProjection map[event.Address]*restoredChain
	// checkpointSlices names the state slices the newest checkpoint carried, so a
	// reader reads only past the cut of a checkpoint that actually states its
	// state and reads the whole chain beside one that does not.
	checkpointSlices map[string]bool
	// restoredInputs is the input rounds the newest checkpoint stated with no
	// answer: they stand before every fact that landed past the cut, so the rest
	// criterion judges the owed drives of a restart beside the tail it replays.
	restoredInputs []restoredInput
	// landedSeq holds the last sequence slot each partition committed: it is
	// seeded from the events the tier already published, so a fact committed
	// after a restart takes the slot after the last committed one. The landing
	// lock guards the map, so the commit that claims a slot and the cut that reads
	// them never read a partition beside a write of it.
	landedSeq   map[int]int64
	records     publicationRecords
	approver    task.Approver
	policy      plugin.Governor
	enforcement governance.Enforcement
	// execManager is the execution resource ledger the exec face acquires
	// through and the lease explanation reads; a declaration that grants the
	// exec face beside no ledger is refused by name at call time.
	execManager *tagentexec.Manager
	// genRegistry is the generation-scoped consumer registry of this run: the
	// consumers mount themselves through the options that wire them, and the
	// publication path drives the registry without naming any consumer.
	genRegistry *state.GenRegistry
	// genFaces reports the per-generation native face of every declared agent as
	// the consumer that owns it built it for one generation: the publication
	// path reads the face of the candidate generation here instead of carrying
	// the tools of one generation in the natives the caller handed over. It is
	// wired by the composition root that owns the document those faces are read
	// from; a run assembled beside none keeps the natives it was handed.
	genFaces GenerationFaces
	// genConsumers is the ordered account of the consumers mounted on
	// genRegistry: the name each registration is keyed by, in the order the
	// wiring mounted them. The publication path builds a candidate generation in
	// exactly this order and unwinds it in the reverse, so a consumer that reads
	// a face another consumer installed is mounted after it. The order is read
	// from the wiring alone and never from map iteration, so the same wiring
	// always builds the same order.
	genConsumers []string
	// publishMu serializes one publication attempt end to end: the consumer
	// faces a candidate installs are read by its own binding construction, so
	// two attempts never interleave the faces they install and restore.
	publishMu sync.Mutex
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
	// rebuild a model configuration change runs and the publication entry it
	// installs the rebuilt model through. Boot fills the table for the first
	// generation; a publication that adds a declared agent extends it under the
	// run lock, and the lease of an agent a later generation removes stays in the
	// table, so a re-added address serves the version in effect instead of a
	// second lease beside it.
	leases map[event.AgentID]*leaseState
	// leaseObservers holds the replacement consumer mounted on each leased
	// agent's pipeline: one fact->observation face per leased agent.
	leaseObservers map[event.AgentID]*rl.LeaseObserver
	// leasePublications records every version a lease installed through this
	// run's default publication entry, in installation order.
	leasePublications []rl.ModelIdentity
	// leaseLosses records every model configuration change whose fact could not
	// commit, so a failed judgment is readable beside the run.
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
	// ageFold judges a session's projection once its replay fills the window the
	// declaration gave it; it is mounted on every agent that declares a window.
	ageFold *AgeFoldPlugin
	// compress executes the judgments the age fold files: the consumer folds the
	// material a compress request names and files the product beside it.
	compress *CompressionConsumer
	// finalRound judges the watchdog budget of a range off its chain and files
	// the closing round it answered: the judgment is a fact, and the consumer
	// that runs the closing round reads it at its own decision boundary.
	finalRound *agent.FinalRoundPlanner
	// closing reports that a close of this run has begun, so the intake refuses
	// new work before the first read of the rest criterion.
	closing bool
	// closeReturned reports that a close of this run has returned to its caller.
	// The consumer set that backed the run stopped with that close, so a result
	// that lands afterwards is kept owed on the mailbox instead of being handed
	// to a range nobody drives any more.
	closeReturned bool
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
//
// - The producer of the closing round reads the budget its own agent declares over the same declaration the loop's table was set from, so the judgment the chain carries names the policy the drive runs under.
// - The consumer of compression judgments is an instance of the material flow like the fold that judges them, so every agent's After pass executes the judgments already filed instead of waiting for a producer to hand them over.
// - The producer of the closing round is an instance of the same flow: every agent's After pass judges the budget of the segment its commit grew, so the closing round of a spent budget stands on the chain as the fact it is.
// - The loop builds the writer ledger for itself and the run reads it through the loop, so the close judges the very registrations the productions made.
func Assemble(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, opts ...Option) (*Run, error) {
	return assemble(ctx, decl, agents, true, opts...)
}

// assemble builds a run and optionally trims before the host's delivery recovery.
func assemble(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, trimAfterSeed bool, opts ...Option) (*Run, error) {
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
		instance:         newInstanceID(),
		bindings:         state.NewBindings(),
		occupancy:        state.NewOccupancy(),
		inspector:        inspectorSubject,
		admission:        task.NewMemoryAdmission(),
		progress:         make(chan struct{}, 1),
		sites:            map[event.Address]bool{},
		facts:            map[event.EventID]event.Event{},
		sessionFactCount: map[event.SessionID]int{},
		fixers:           map[event.AgentID]*fileFixer{},
		replays:          map[event.Address]replayEntry{},
		landedSeq:        map[int]int64{},
		states:           state.NewStateRegistry(),
		checkpointEvery:  CheckpointCommitInterval,
		checkpointPeriod: CheckpointPeriod,
		checkpointAt:     time.Now(),
		checkpointDue:    make(chan struct{}, 1),
		checkpointStop:   make(chan struct{}),
		approver:         cfg.approver,
		policy:           cfg.policy,
		enforcement:      cfg.enforcement,
		recorder:         cfg.recorder,
		observer:         cfg.observer,
		execManager:      cfg.execManager,
		genRegistry:      state.NewGenRegistry(),
		genFaces:         cfg.genFaces,
		capture:          cfg.capture,
		leases:           map[event.AgentID]*leaseState{},
		leaseObservers:   map[event.AgentID]*rl.LeaseObserver{},
	}
	if cfg.observer != nil {
		r.enableGenerationObservation()
	}
	for _, slice := range []state.Stateful{projectionStateSlice{run: r}, inputsStateSlice{run: r}, compressStateSlice{run: r}} {
		if _, err := r.states.Register(slice); err != nil {
			return nil, err
		}
	}
	for id, held := range cfg.leases {
		lease := held
		r.leases[id] = &lease
	}
	for _, reg := range cfg.genRegistrations {
		if err := r.genRegistry.Register(reg); err != nil {
			return nil, err
		}
		r.genConsumers = append(r.genConsumers, reg.Name)
	}
	for _, port := range cfg.genPorts {
		port.bind(r)
	}
	traceStart := time.Now()
	booTTrace := func(name string) {
		if os.Getenv("TAGENT_BOOT_TRACE") == "1" {
			assemblyInfo("tagent: boot trace phase=%s elapsed=%s", name, time.Since(traceStart).Round(time.Microsecond))
		}
	}
	booTTrace("built")
	r.compress = NewCompressionConsumer(ageFoldSessions{run: r}).withSummaryGist(bandSummaries{run: r})
	r.ageFold = NewAgeFoldPlugin(ageFoldSessions{run: r})
	r.ageFold.setExecutor(r.compress)
	r.finalRound = agent.NewFinalRoundPlanner(finalRoundChain{run: r}, r.finalRoundPolicyOf)
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
	if r.compress != nil {
		r.plugins = append(r.plugins, r.compress)
	}
	if r.finalRound != nil {
		r.plugins = append(r.plugins, r.finalRound)
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
		for _, s := range durable.LoadStats().Scans {
			assemblyInfo("tagent: durable load scan prefix=%s keys=%d bytes=%d elapsed=%s",
				s.Prefix, s.Keys, s.Bytes, s.Elapsed.Round(time.Microsecond))
		}
		r.commit = &runCommit{inner: durable, held: map[event.TaskID]bool{}}
		booTTrace("storage")
	}
	r.records.attach(tier)
	if durable == nil {
		r.commit = &runCommit{inner: task.NewMemoryCommit(nil), held: map[event.TaskID]bool{}}
	}
	if durable != nil {
		r.store = state.NewMemoryStore(r.authority, durable)
	} else {
		r.store = state.NewMemoryStore(r.authority)
	}
	r.registry = task.NewMemoryRegistry(r.commit, r.admission, task.WithActivity(r.noteProgress), task.WithTerminalTTL(cfg.terminalTTL()))
	if _, err := r.states.Register(state.NewMailboxState(r.store)); err != nil {
		return nil, err
	}
	if _, err := r.states.Register(tasksStateSlice{registry: r.registry}); err != nil {
		return nil, err
	}
	r.movement = runMovement{registry: r.registry, store: r.store}
	r.manager = task.NewMemoryManager(r.registry, nil, task.WithEnvelopeDeliverer(r.deliver))
	r.delegations = agent.NewDelegator(closeGate{run: r, face: closeFace{manager: r.manager, registry: r.registry}}, r.bindings, r.instance)
	r.publisher = state.NewPublisher(r.bindings, publishContract, cfg.governance, &r.records)
	r.publisher.ObserveRetirements(r.recordGenerationRetirement)
	r.loop = agent.NewLoop(r.publisher, r.occupancy, r.instance, r.resolveFact)
	r.writers = r.loop
	r.delegations.SetResident(r.loop)
	r.loop.SetHistory(r)
	r.loop.SetPresentations(r.takePresentation)
	r.loop.SetFactCommit(func(evt event.Event) (event.EventID, error) {
		return r.commitFactChecked(context.Background(), evt)
	})
	r.loop.SetFactPipelines(r.factPipeline)
	r.loop.SetRequestFace(r.requestBoundary)
	r.loop.SetDeliveryPending(r.deliveryPendingFor)
	inventory := query.NewInventory(hostReading{runReading: runReading{run: r}, approvals: cfg.approvals})
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
	if cfg.bootReconcile != nil {
		if err := cfg.bootReconcile(ctx, r, decl, agents); err != nil {
			return nil, err
		}
	} else if _, err := r.Publish(ctx, decl, agents); err != nil {
		return nil, err
	}
	if durable != nil {
		if err := r.restoreCheckpointState(ctx, tier); err != nil {
			return nil, err
		}
		if err := r.restoreInMemoryState(ctx, durable); err != nil {
			return nil, err
		}
		booTTrace("restoreChains")
	}
	if tier != nil {
		supply, err := supplyRetrieval(ctx, tier, cfg.embedder)
		if err != nil {
			return nil, err
		}
		booTTrace("supplyRetrieval")
		landed, err := r.seedReplay(ctx, tier)
		if err != nil {
			return nil, err
		}
		booTTrace("seedReplay")
		r.mu.Lock()
		r.retrieval = supply
		r.tier = tier
		r.mu.Unlock()
		r.landing.Lock()
		r.landedSeq = landed
		r.landing.Unlock()
		if trimAfterSeed {
			r.trimSeededFacts()
		}
	}
	r.mu.Lock()
	r.embedder = cfg.embedder
	r.mu.Unlock()
	if err := r.convergeGenerationRetirements(ctx); err != nil {
		return nil, err
	}
	booTTrace("converge")
	r.startCheckpointFiler(ctx)
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
	return r.publishDeclared(ctx, decl, agents, publicationWindow{})
}

// generationLeaseKind is the resource kind a generation-scoped consumer's
// exclusive registration takes in the candidate ledger, so the diagnostic read
// of the run names the same kind the publication path registered.
const generationLeaseKind = "gen"

// publishDeclared is the one preparation of a publication candidate: the
// declaration fingerprint, the declared MCP servers, the prepared live binding
// of every declared agent and the one publication entry, with the decision
// window the record of this attempt carries. It is the same path Publish runs,
// so every caller that judges a window still enters through the sole entry.
//
// The generation-scoped consumers build their instances for the candidate
// generation before the binding construction reads their faces, and their
// exclusive resources enter the candidate ledger beside the binding's own, so
// the retirement of a superseded generation releases them through the existing
// drain. A candidate whose own plan does not take effect — a failed
// preparation, a refused publication, unchanged content or a publication that
// rolled back to other content — puts the faces it installed back and frees only
// what it acquired; a candidate that published keeps them installed.
//
// - The host dialect a publication of a host document carries is part of the fingerprint: the shared declaration does not hold the risk table, the observation section or the skill and read face of an agent, so a document that moved only those sections would otherwise publish nothing.
func (r *Run) publishDeclared(ctx context.Context, decl *config.Declaration, agents map[event.AgentID]NativeAgent, window publicationWindow) (state.PublishOutcome, error) {
	if decl == nil {
		return state.PublishOutcome{}, errors.New("tagent: the candidate carries no declaration")
	}
	r.publishMu.Lock()
	defer r.publishMu.Unlock()
	fingerprint, err := config.Fingerprint(*decl)
	if err != nil {
		return state.PublishOutcome{}, err
	}
	fingerprint = publicationFingerprint(fingerprint, window.dialectKey)
	servers := declaredServersOf(decl)
	if err := mcpgw.ValidateDeclared(servers); err != nil {
		return state.PublishOutcome{}, err
	}
	plan := state.OrgPlan{
		Declaration: decl,
		Baseline:    r.publisher.Baseline(),
		Fingerprint: fingerprint,
	}
	gen := state.Generation(r.publisher.Current().Generation + 1)
	built, err := r.genRegistry.Build(ctx, decl, gen)
	if err != nil {
		return state.PublishOutcome{}, err
	}
	if r.genFaces != nil {
		faces, ok := r.genFaces.Face(gen)
		if !ok {
			built.Restore()
			return state.PublishOutcome{}, errors.Join(
				fmt.Errorf("tagent: the consumer side built no native face for generation %d", gen),
				built.Release(ctx),
			)
		}
		agents = faces
	}
	cand, err := prepare(ctx, plan, r.suppliers(decl, agents))
	if err != nil {
		built.Restore()
		return state.PublishOutcome{}, errors.Join(err, built.Release(ctx))
	}
	for _, consumer := range built {
		if consumer.Resources.Resources == nil {
			continue
		}
		ref := cand.Resources.Register(generationLeaseKind, state.GenLeaseKey(gen, consumer.Name), consumer.Resources.Resources.ReleaseAll)
		cand.Plan.Leases = append(cand.Plan.Leases, ref)
	}
	cand = cand.WithWindow(window.previous, window.previousFingerprint, window.models, window.dimensions, window.at)
	outcome, err := r.publisher.Publish(ctx, cand)
	if outcome.Result != state.PublishApplied {
		built.Restore()
	}
	if err == nil {
		r.loop.SetFinalRound(finalRoundPolicy(decl))
		r.loop.SetAgentFinalRounds(agentFinalRoundPolicies(decl))
		r.mu.Lock()
		r.current = cand.Plan
		r.natives = agents
		r.mu.Unlock()
		if err := r.bindMCPServers(ctx, servers); err != nil {
			return outcome, err
		}
	}
	return outcome, err
}

// publicationFingerprint reports the publication fingerprint of one declaration
// under one host dialect: the declaration fingerprint beside the digest of the
// sections the shared declaration does not carry. A document that declares no
// dialect section answers the digest of nothing, so its fingerprint stays the
// declaration fingerprint and a publication of the same configuration again
// reads as unchanged.
func publicationFingerprint(fingerprint, dialectKey string) string {
	if dialectKey == "" {
		return fingerprint
	}
	return fingerprint + "|host:" + dialectKey
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

// NarrowFace narrows the face one agent may see from the next request boundary
// on, or opens it again when the selection carries no list: the binding is not
// republished, so the change reaches the very next request. The host and the
// plugins that guard one agent call this; the request's tools, the environment
// block and the capture view all read the narrowed face.
func (r *Run) NarrowFace(ctx context.Context, agent event.AgentID, selection state.VisibleSelection) error {
	handle, err := r.bindings.Borrow(ctx, agent)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Release(ctx) }()
	resources, ok := handle.Binding.Set.(*state.ToolResources)
	if !ok {
		return fmt.Errorf("tagent: agent %q carries no tool resources to narrow", agent)
	}
	resources.SetVisible(selection)
	return nil
}

// SessionToolNames reports the tool names one session's request face exposes,
// in the order the request emits them. It reads the live, session-scoped tool
// face of the agent's current binding — reflection-only tools appear only for
// the agent's dedicated reflection session — so a caller observes exactly the
// face a request at that session would carry, without driving a turn.
func (r *Run) SessionToolNames(agent event.AgentID, session event.SessionID) ([]string, error) {
	ctx := context.Background()
	handle, err := r.bindings.Borrow(ctx, agent)
	if err != nil {
		return nil, err
	}
	defer func() { _ = handle.Release(ctx) }()
	_, order := state.ToolFace(handle.Binding, session)
	return append([]string(nil), order...), nil
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

// publishedEntry reports the entry agent the declaration in effect names, and the
// empty identity beside a run that published no declaration.
func (r *Run) publishedEntry() event.AgentID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return entryOfDeclaration(r.current.Plan.Declaration)
}

// publishedOrganization reports the entry agent the declaration in effect names
// beside whether it declares the named agent. Both halves are read under the run
// lock, so one generation answers them together: a concurrent publication never
// lends the entry of one generation to the declared set of another.
func (r *Run) publishedOrganization(agentID event.AgentID) (event.AgentID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	decl := r.current.Plan.Declaration
	return entryOfDeclaration(decl), decl == nil || declaresAgent(decl, agentID)
}

// entryOfDeclaration reports the entry agent one declaration names, and the empty
// identity beside no declaration.
func entryOfDeclaration(decl *config.Declaration) event.AgentID {
	if decl == nil {
		return ""
	}
	return event.AgentID(strings.TrimSpace(decl.Entry))
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

// restoreInMemoryState is the second step of the restart-recovery consumption
// logic: the in-memory state is restored from the checkpoint the last process
// filed, or from the full replay of the fact chains beside a store that holds
// none.
func (r *Run) restoreInMemoryState(ctx context.Context, durable *state.DurableCommit) error {
	report, err := r.restoreChains(ctx, durable)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.recovery = report
	r.mu.Unlock()
	return nil
}

// restoreCheckpointState reads the newest framework checkpoint the storage holds
// and hands every state slice back to its holder, so the fold that follows
// completes an account instead of rebuilding one: the task slice states every
// task of the cut beside the chain revision it stands at, and the delivery,
// mailbox and input slices state the work the process still owed. A tier holding
// no checkpoint states the empty cut, and the holders are restored from that the
// same way: each starts from the empty state and the replay past the cut rebuilds
// it, which past the zero cut is the whole chain. The mailbox states the
// obligations its durable account still owes on either cut, so the work a stopped
// process left open is seated by this restore and no consumer keeps a restore
// path of its own.
//
// The carrier is read once per boot: a holder mounted after this call states its
// slice from the pending set, and the chain rebuild that follows takes the state
// this call restored instead of restoring it again.
func (r *Run) restoreCheckpointState(ctx context.Context, tier *ledger.Tier) error {
	if tier == nil {
		return nil
	}
	if _, restored, err := r.restoreNewestCheckpoint(ctx, tier); err != nil {
		return err
	} else if restored {
		return nil
	}
	return r.states.RestoreEmptyState(ctx)
}

// restoreChains folds the durable fact chains the storage already holds back
// into the registry of this assembly under the current orchestration binding,
// one outcome per task the chains cover. A chain carrying no admission material
// is refused by name in the report and rebuilds nothing.
//
//   - a chain the newest framework checkpoint states is folded from the link that landed past the stated revision, and one the checkpoint states with no such link is not read at all: a restart costs the account it keeps plus the increment past the cut, never the chains it already settled.
//   - a chain the checkpoint did not state — one younger than the cut — is folded whole, which is the fallback that keeps the two paths agreeing.
//   - a chain the checkpoint states with no record past its cut costs no read: it joins the covered set the checkpoint-state call restores.
//   - a checkpoint-stated task the carrier rebuilt at the cut, whose tail links the fold completes, is held by the registry already and the fold reports no rebuild of its own: its recovery is the checkpoint's account, credited here as Recovered=true beside the covered tasks so the read projection serves it.
//   - the report names the tasks in chain order whichever path restored them, so a restart answers the same account whether it read a chain or the checkpoint.
//   - the whole fold is observed as one line at INFO — the chains read, the links they carry, the chains the checkpoint covered, the tasks rebuilt and the chains dropped beside the elapsed time — and a failed fold at ERROR beside the chain it stopped on.
func (r *Run) restoreChains(ctx context.Context, durable *state.DurableCommit) (task.RestoreReport, error) {
	started := time.Now()
	owners := runOwners{bindings: r.bindings}
	merged := task.RestoreReport{}
	coverage := r.registry.CheckpointCoverage()
	chains := durable.TaskIDs()
	folded := task.RestoreReport{}
	var covered []event.TaskID
	links := 0
	for _, taskID := range chains {
		revision, stated := coverage[taskID]
		if stated && durable.Revision(taskID) <= revision {
			covered = append(covered, taskID)
			continue
		}
		chain, err := durable.Records(ctx, taskID)
		if err != nil {
			assemblyError("tagent: restore stopped task=%s err=%v", taskID, err)
			return merged, err
		}
		if stated {
			chain = chainPast(chain, revision)
		}
		links += len(chain)
		report, err := r.registry.Restore(ctx, chain, owners)
		if err != nil {
			assemblyError("tagent: restore stopped task=%s err=%v", taskID, err)
			return merged, err
		}
		if stated {
			for i := range report.Tasks {
				report.Tasks[i].Recovered = true
			}
		}
		folded.Tasks = append(folded.Tasks, report.Tasks...)
		for _, outcome := range report.Tasks {
			if outcome.Recovered {
				r.commit.remember(outcome.TaskID)
			}
		}
	}
	restored, err := r.registry.CheckpointRestored(ctx, covered, owners)
	if err != nil {
		assemblyError("tagent: restore stopped at the tasks the checkpoint states err=%v", err)
		return merged, err
	}
	byTask := make(map[event.TaskID]task.RestoreReport, len(chains))
	for _, outcome := range folded.Tasks {
		held := byTask[outcome.TaskID]
		held.Tasks = append(held.Tasks, outcome)
		byTask[outcome.TaskID] = held
	}
	for _, outcome := range restored.Tasks {
		held := byTask[outcome.TaskID]
		held.Tasks = append(held.Tasks, outcome)
		byTask[outcome.TaskID] = held
		r.commit.remember(outcome.TaskID)
	}
	merged.Tasks = make([]task.RestoreOutcome, 0, len(chains))
	for _, taskID := range chains {
		merged.Tasks = append(merged.Tasks, byTask[taskID].Tasks...)
	}
	recovered, dropped := restoredCounts(folded)
	assemblyInfo("tagent: restore folded chains=%d links=%d covered=%d recovered=%d dropped=%d elapsed=%s",
		len(chains)-len(covered), links, len(covered), recovered, dropped, time.Since(started).Round(time.Microsecond))
	return merged, nil
}

// chainPast reports the links of one chain that landed past the state a
// checkpoint already states: the links a fold completes the restored account
// with, and no link the account already covers.
func chainPast(chain []task.TaskRecord, revision int64) []task.TaskRecord {
	past := make([]task.TaskRecord, 0, len(chain))
	for _, link := range chain {
		if link.Fact.ExpectedRevision < revision {
			continue
		}
		past = append(past, link)
	}
	return past
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
// the assembly opened, the per-prefix read cost of the durable load (five scan
// lines beside a durable tier) and what the fold of the durable chains
// recovered from it. One assembly writes a bounded line set, so the INFO stream
// stays proportional to the assemblies a process builds.
//
// The redline list (3 rules):
// MUST stay a bypass: a failing logger is a missing line, never a failed assembly.
// MUST write one line per opened tier, five scan lines per durable load, and one per completed fold.
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
// storage tier: the retrieval face reads the durable chain on demand, and a
// run whose facts live only in memory carries no
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
	recorder    rl.Recorder
	observer    plugin.Observer
	execManager *tagentexec.Manager
	// genRegistrations carries the generation-scoped consumer mounts the
	// wiring options contribute: each consumer registers its own builder, and
	// the assembly drives the registry without naming any consumer.
	genRegistrations []state.GenRegistration
	// genFaces reports the per-generation native face the host consumer built
	// for one generation, so the publication path composes the candidate binding
	// over the face of its own generation.
	genFaces GenerationFaces
	// genPorts are the consumers of the host's own dialect this assembly hands
	// the run it builds, so the builder of every candidate generation installs
	// its face on the run before that generation's binding is prepared.
	genPorts []hostGenerationPort
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
	// taskTerminalTTL is how long a task record outlives the terminal outcome it holds,
	// read as task_terminal_ttl: an assembly that names none keeps DefaultTerminalTTL, and
	// a negative one keeps every record the run ever admitted.
	taskTerminalTTL time.Duration
	// bootReconcile is the startup reconciliation the host entry names: it runs
	// after the storage tier is open and the configuration chain is attached,
	// and before the first publication, so a restart decides whether the source
	// file or the chain is the truth of this process instead of publishing the
	// file unconditionally. A nil hook keeps the plain first publication.
	bootReconcile func(context.Context, *Run, *config.Declaration, map[event.AgentID]NativeAgent) error
}

// withBootConfigReconcile attaches the startup reconciliation of the host entry:
// the hook reads the configuration chain before the first publication and
// decides which declaration this process serves. It is the host entry's own
// wiring, never a second publication path: the hook enters the sole publication
// entry (or adopts the ledger's publication) the same way a reload does.
func withBootConfigReconcile(reconcile func(context.Context, *Run, *config.Declaration, map[event.AgentID]NativeAgent) error) Option {
	return func(c *assemblyConfig) { c.bootReconcile = reconcile }
}

// Option configures the formal assembly at construction. It is the assembly
// name of the host option: one config struct, so the manual and the host entry
// accept the same constructors.
type Option = HostOption

// withTaskTerminalTTL sets how long a task record outlives the terminal outcome it holds: the
// record of a settled, failed or cancelled task older than this is dropped beside the next
// admission, so this run's account of tasks is bounded by it. A call naming a pruned task is
// refused by name, and every fact of that task stays on the chain — the bound is the live
// account, never the history. The zero value keeps task.DefaultTerminalTTL, and a negative
// one keeps every record for the life of the run.
func withTaskTerminalTTL(ttl time.Duration) Option {
	return func(c *assemblyConfig) { c.taskTerminalTTL = ttl }
}

// terminalTTL reports the bound this assembly builds its task table under.
func (c *assemblyConfig) terminalTTL() time.Duration {
	switch {
	case c == nil:
		return task.DefaultTerminalTTL
	case c.taskTerminalTTL < 0:
		return 0
	case c.taskTerminalTTL == 0:
		return task.DefaultTerminalTTL
	}
	return c.taskTerminalTTL
}

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

// WithEmbedder attaches the binding the retrieval face calls only while a
// query scores its candidates. An assembly beside no embedder calls no
// embedding at all: recall answers through the keyword leg alone and behaves
// exactly as it did before the embedding existed. The binding is used on
// demand and never owns a runtime vector table.
func WithEmbedder(embedder memory.Embedder) Option {
	return func(c *assemblyConfig) { c.embedder = embedder }
}

// WithRecorder attaches the training record pipeline this assembly derives its
// attempt records through: the run admits a decision's request view and an
// attempt's reduced outcome once, and the pipeline writes one attempt record per
// admission exactly as the attempt observed it, so a recorded outcome is never
// re-derived beside the model's own. The episode boundary and the stored input of
// a decision are not observed at this admission: a pipeline that needs those
// derives them from the capture's evidence beside the chain the run keeps. A
// write the recorder refuses is counted in the explicit recording-loss account of
// the run instead of being dropped silently.
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

// WithGenerationConsumer mounts one consumer on the generation-scoped registry
// of this assembly: the consumer answers the builder of its per-generation
// instance, the publication path rebuilds it for every candidate generation
// without naming the consumer, and the order the options are applied in is the
// order the registry builds.
//
// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
//   - The builder owns the whole life of its instance: it constructs the instance, installs it as the face the candidate's binding construction reads, and answers the exclusive resources it acquired beside the restore that puts the previous face back; a builder that acquires nothing exclusive answers a nil resource ledger and is registered nowhere.
//   - Only the process-private resources of one generation enter that ledger: an external durable resource survives the switch and is re-probed by the next generation, and a shared native is reused as it stands, so a retirement releases neither.
func WithGenerationConsumer(reg state.GenRegistration) Option {
	return func(c *assemblyConfig) { c.genRegistrations = append(c.genRegistrations, reg) }
}

// GenerationFaces reports the per-generation native face of every declared agent
// of one generation. A generation-scoped consumer builds the face of a candidate
// generation from the declaration that generation publishes, and the publication
// path reads it here when it composes the candidate binding: the model of an
// agent is the shared native that survives every switch, while the tools, skill
// roots, read root, file surface and reflection scope are the face of exactly
// one generation.
type GenerationFaces interface {
	// Face reports the composed natives of one generation, and false beside a
	// generation the source never built.
	Face(gen state.Generation) (map[event.AgentID]NativeAgent, bool)
}

// WithGenerationFaces wires the source of the per-generation native face. A run
// assembled beside none composes every candidate over the natives its caller
// handed over, which is the shape a caller that owns its own agent materials
// reads; a host wires the consumer that rebuilds the face from its document.
func WithGenerationFaces(source GenerationFaces) Option {
	return func(c *assemblyConfig) { c.genFaces = source }
}

// hostGenerationPort is one consumer of the host's own dialect that keeps its
// face on the run: the assembly hands it the run it built before the first
// generation is published, so the builder of every candidate generation installs
// its face where the binding construction of that generation reads it.
type hostGenerationPort interface {
	// bind attaches the assembled run the consumer installs its face on.
	bind(run *Run)
}

// withHostGenerationPorts hands the host dialect consumers of this assembly the
// run it builds, before the first generation is published.
func withHostGenerationPorts(ports ...hostGenerationPort) Option {
	return func(c *assemblyConfig) { c.genPorts = append(c.genPorts, ports...) }
}

// WithExecManager attaches the execution resource ledger this assembly acquires
// through and explains its registered leases by: a declaration that grants the
// exec face acquires through this ledger under the monitor rhythm its own
// declaration fixes, and the blocker explanation read of the run reports each
// registration beside the retention facts the ledger records under its key. The
// ledger also mounts the execution consumer on the generation-scoped registry,
// so every published generation rebuilds its exec instance over this ledger's
// executor and options. An assembly beside no ledger still explains its task
// and notification blockers, refuses to explain a registered lease by name, and
// refuses a call of the granted exec face by name instead of starting anything.
func WithExecManager(manager *tagentexec.Manager) Option {
	mount := WithGenerationConsumer(tagentexec.GenRegistration(manager))
	return func(c *assemblyConfig) {
		c.execManager = manager
		if manager != nil {
			mount(c)
		}
	}
}

// GenerationConsumer is one consumer of the generation-scoped registry as the
// run reports it: the name its registration is keyed by, the frozen key its
// exclusive registration takes in one generation, and whether the generation
// the run serves carries that registration.
type GenerationConsumer struct {
	// Name is the consumer's registered name.
	Name string
	// Exclusive is the frozen key of its exclusive registration in one
	// generation, in the form "gen:<id>:<consumer>".
	Exclusive string
	// Registered reports whether the generation the run serves carries that
	// exclusive registration: a consumer that acquired nothing exclusive
	// registers none, and a consumer a generation never built carries none
	// either.
	Registered bool
}

// GenerationConsumers reports the consumers the generation-scoped registry
// rebuilds, in build order: the name each registration is keyed by beside the
// exclusive registration the served generation carries for it. The read is the
// diagnostic face of the registry the publication path drives, so it moves
// nothing; it names a generation other than the served one as the registrations
// that generation would take rather than the ones in service.
func (r *Run) GenerationConsumers(gen state.Generation) []GenerationConsumer {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	names := append([]string(nil), r.genConsumers...)
	registered := make(map[string]bool, len(r.current.Leases))
	for _, ref := range r.current.Leases {
		if ref.Kind == generationLeaseKind {
			registered[ref.Key] = true
		}
	}
	r.mu.Unlock()
	out := make([]GenerationConsumer, 0, len(names))
	for _, name := range names {
		key := state.GenLeaseKey(gen, name)
		out = append(out, GenerationConsumer{Name: name, Exclusive: key, Registered: registered[key]})
	}
	return out
}

// generationRetireDimension is the configuration dimension the retirement of one
// generation is filed under, beside the frozen generation stamp: the
// configuration chain's control-plane records reuse the one configuration change
// fact type under their own dimension, so the retirement is a fact of that chain
// instead of a second fact type no reader of this build maps.
const generationRetireDimension = "generation-retire"

// generationRetirePrefix is the frozen key prefix one retirement is quoted by:
// the key derives from the generation alone, so a boot replay that derives the
// same retirement again is answered as the fact the chain already holds.
const generationRetirePrefix = "gen-retire:"

// generationRetireKey names the record of one retired generation: the frozen key
// "gen-retire:<id>" a retirement is quoted by. The key is derived from the
// generation alone, so a boot replay that derives the same retirement again is
// answered as the fact the chain already holds instead of adding a second one.
func generationRetireKey(gen state.Generation) event.EventID {
	return event.EventID(generationRetirePrefix + strconv.FormatUint(uint64(gen), 10))
}

// generationRetirementRecord reports whether one committed fact is the
// retirement of a generation: a control-plane record of the configuration chain
// rather than a fact of a session chain.
func generationRetirementRecord(evt event.Event) bool {
	return strings.HasPrefix(string(evt.Key), generationRetirePrefix)
}

// recordGenerationRetirement files the retirement of one generation on the
// chain: the drain freed the exclusive resources of a generation no publication
// serves any more, and the record states that retirement beside the generation
// number. The record is written after the release and never triggers one, and the
// boot convergence reads it as the anchor of a retirement the restart does not
// have to derive again.
//
// 契约: docs/wiki/platform/org-hot-reload.md#apply-record
func (r *Run) recordGenerationRetirement(ctx context.Context, gen state.Generation) error {
	if r == nil || gen == 0 {
		return nil
	}
	number := strconv.FormatUint(uint64(gen), 10)
	summary := "generation " + number + " retired"
	evt := event.Event{
		Key:       generationRetireKey(gen),
		Type:      event.TypeConfigChange,
		Timestamp: time.Now().UTC(),
		Message:   model.Message{Role: model.RoleSystem, Content: summary},
		Meta: map[string]string{
			event.MetaKeyEventSummary:           summary,
			event.MetaKeyTriggerSource:          generationRetireDimension,
			event.MetaKeyConfigChangeDimension:  generationRetireDimension,
			event.MetaKeyConfigChangeGeneration: number,
		},
		Owner: r.generationOwner(),
	}
	if _, err := r.commitFactChecked(ctx, evt); err != nil {
		return fmt.Errorf("tagent: record the retirement of generation %d: %w", gen, err)
	}
	return nil
}

// generationOwner reports the address the generation records of this run are
// owned by: the entry agent of the declaration in effect, the agent the chain
// files the organization-level judgments of the configuration under. A run
// beside no declaration in effect answers no owner, and its records stay the
// in-process facts they are.
func (r *Run) generationOwner() event.Address {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current.Plan.Declaration == nil {
		return event.Address{}
	}
	entry := strings.TrimSpace(r.current.Plan.Declaration.Entry)
	if entry == "" {
		return event.Address{}
	}
	return event.Address{AgentID: event.AgentID(entry)}
}

// convergeGenerationRetirements records the retirement of every generation the
// chain this process booted over already left behind: a generation older than the
// one in effect was superseded before this process started and holds no request,
// so a boot that finds no record of its retirement files one. It closes the
// window a crash opens between the generation switch and the drain that retires
// the generation it replaced.
//
// The convergence records only: the exclusive resources of those generations were
// process-private and went with the process that ended, so the record says what
// happened instead of releasing anything again.
func (r *Run) convergeGenerationRetirements(ctx context.Context) error {
	current := state.Generation(r.publisher.Current().Generation)
	for gen := state.Generation(1); gen < current; gen++ {
		_, err := r.CommittedEvent(ctx, generationRetireKey(gen))
		if err == nil {
			continue
		}
		if !errors.Is(err, ErrCommittedFactMissing) {
			return fmt.Errorf("tagent: read the retirement record of generation %d: %w", gen, err)
		}
		if err := r.recordGenerationRetirement(ctx, gen); err != nil {
			return err
		}
	}
	return nil
}

// trackGenerationConsumers records the exclusive resources the consumers of one
// adopted generation acquired on the generation the publication adopted: each
// enters the retirement ledger under the frozen generation key it was registered
// by, so a later publication that supersedes this generation drains them through
// the same release a published candidate's resources go through, and the plan in
// effect names those registrations beside the resources of the adoption itself.
func (r *Run) trackGenerationConsumers(ctx context.Context, gen state.Generation, built state.GenInstances) error {
	resources := state.NewLeases()
	refs := make([]state.LeaseRef, 0, len(built))
	for _, consumer := range built {
		if consumer.Resources.Resources == nil {
			continue
		}
		refs = append(refs, resources.Register(generationLeaseKind, state.GenLeaseKey(gen, consumer.Name), consumer.Resources.Resources.ReleaseAll))
	}
	if len(refs) == 0 {
		return nil
	}
	r.mu.Lock()
	r.current.Leases = append(r.current.Leases, refs...)
	r.mu.Unlock()
	return r.publisher.TrackGeneration(ctx, gen, resources)
}

// installObserver installs the observation face one candidate generation
// declares: the commit observer its bindings are built with reads it here, so a
// generation that moved its telemetry section observes every fact through the
// observer it declared.
func (r *Run) installObserver(observer plugin.Observer) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.observer = observer
	r.mu.Unlock()
}

// observerInEffect reports the observation face of the generation in effect.
func (r *Run) observerInEffect() plugin.Observer {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.observer
}

// installGovernor installs the governance face one candidate generation
// declares: the admission wrap a generation's tools are registered under rules
// each call over it.
func (r *Run) installGovernor(policy plugin.Governor) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.policy = policy
	r.mu.Unlock()
}

// governorInEffect reports the governance face of the generation in effect.
func (r *Run) governorInEffect() plugin.Governor {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.policy
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

// InjectEvent commits one explicit input event and wakes its address Consumer,
// exactly as Invoke does. The input keeps its source invocation, while the Consumer
// mints the segment identity that answers it. The caller owns the event's
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
	turn, err := r.waitResident(ctx, evt.Owner, evt.InvocationID, []event.EventID{key})
	return turn, key, err
}

// CommitToSession commits one event into the fact chain of an explicit address,
// under a fresh invocation identity when the caller states none, and runs the
// target agent's own published pipeline over the fact exactly as any other
// commit of this run: an address the run cannot establish is refused by name.
//
// The commit is a write of this run, never a second store beside it: the fact
// lands in the same live ledger, on the same durable log and in the same mapped
// sequence as the target session's own input, so that session's next assembly
// reads it like any other external input. The address block of the event is
// backfilled, never overwritten: a source, target or owner the caller already
// stamped is kept as the caller wrote it, and only an unstated stamp reads as
// the target address of the commit. The purposes it serves are these:
//
// - a task notification's material: the notice lands as the user message its address pulls, so the attention projection reads it as input rather than as a line beside it
// - the audit facts of the host: an execution state, a refused delivery and a delivered failure notice each land as one feedback fact that carries no model payload
// - the shared write seam of the full-duplex face: a send from one session to another and a reflection delivery both land their body through the commit-only segment below, which backfills the address block by this face's rule and leaves the observation to the target's own next boundary
func (r *Run) CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error) {
	key, err := r.commitOnly(ctx, addr, evt)
	if err != nil {
		return key, err
	}
	pipeline, err := r.factPipeline(addr.AgentID)
	if err != nil {
		return key, err
	}
	r.takePresentation(pipeline.After(ctx, key))
	return key, nil
}

// commitOnly lands one event in the fact chain of an explicit address and stops
// at the commit: the address block is backfilled exactly as the cross-session
// write face backfills it, and no observation runs here. A caller that lands a
// message beside a range already in flight uses this segment so the fact is not
// observed twice: the After pass of that fact belongs to the boundary read the
// running drive performs at its own next decision, which observes each fact of
// its range once.
func (r *Run) commitOnly(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error) {
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
//
// The control-plane records of the configuration chain are not part of it: a
// generation retirement is filed under the configuration change type with the
// frozen generation key and names no session chain, so it is held and readable
// by identity while it never stands for a fact of the material this run carries.
func (r *Run) CommittedFacts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countControlFactsLocked()
	return len(r.facts) - r.controlFacts
}

// countControlFactsLocked brings the control-plane count up to the commit order
// the run holds: it judges the suffix past controlScanned once. A trim resets the
// cursor and count over the kept prefix (compactFactOrderLocked), so the suffix
// this reads is always a valid slice of the current order. The caller holds the
// run lock.
func (r *Run) countControlFactsLocked() {
	for _, key := range r.factOrder[r.controlScanned:] {
		if evt, held := r.facts[key]; held && generationRetirementRecord(evt) {
			r.controlFacts++
		}
	}
	r.controlScanned = len(r.factOrder)
}

// ReflectionGate reports the watermark gate of the generation in effect: the
// anchor store the reflection loop it mounted reads its consumed watermark from,
// and the store the assembly holds beside a generation that enables no
// meditation. It answers nil only beside an assembly wired with no anchor record
// at all.
func (r *Run) ReflectionGate() meditation.WatermarkGate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.watermark
}

// installWatermark installs the anchor store of one generation's reflection loop
// as the gate the run reports and the diagnostics read.
func (r *Run) installWatermark(gate meditation.WatermarkGate) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.watermark = gate
	r.mu.Unlock()
}

// generationObserver is the observation face one binding is built with: it
// forwards every committed fact to the observer of the generation in effect, so
// a dialect install that replaces the observation face reaches the binding
// already in service instead of the one it was built under.
type generationObserver struct {
	run *Run
}

var _ plugin.Observer = generationObserver{}

// Observe reports one committed fact to the observation face in effect. A run
// beside no observer observes nothing and reports no failure.
func (o generationObserver) Observe(ctx context.Context, evt event.Event) error {
	face := o.run.observerInEffect()
	if face == nil {
		return nil
	}
	return face.Observe(ctx, evt)
}

// enableGenerationObservation wires the commit observer of every binding this
// run builds: its observation face is read when a fact commits, so a host whose
// observation consumer is mounted observes through the face of the generation in
// effect, including a face a later install replaced. A run assembled beside no
// face wires none: its bindings carry no commit observer.
func (r *Run) enableGenerationObservation() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.observationEnabled = true
	r.mu.Unlock()
}

// observationWired reports whether the bindings of this run observe committed
// facts through the face in effect.
func (r *Run) observationWired() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.observationEnabled
}

// admissionRuling resolves the ruling of one call when the call is admitted: the
// governance policy of the generation in effect rules it, and beside no policy
// the approver this assembly was configured with rules it, or the strict grade
// refuses it. The ruling is resolved per call, so a governance table a reload
// installed rules the bindings already in service.
func (r *Run) admissionRuling() task.Approver {
	return func(ctx context.Context, name string, jsonArgs []byte) (task.Ruling, error) {
		if policy := r.governorInEffect(); policy != nil {
			return policyRuling(policy)(ctx, name, jsonArgs)
		}
		if r.approver != nil {
			return r.approver(ctx, name, jsonArgs)
		}
		if r.enforcement == governance.EnforcementStrict {
			return denyWithoutApprover(ctx, name, jsonArgs)
		}
		return task.Ruling{Kind: task.RulingRun}, nil
	}
}

// ObservedEvents answers one query over the durable fact chain, independent
// of the runtime window and of any landing pass.
// A run assembled beside no storage tier refuses the read by name.
func (r *Run) ObservedEvents(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	r.mu.Lock()
	tier := r.tier
	r.mu.Unlock()
	if tier == nil {
		return nil, ErrObservationUnwired
	}
	return NewTierQuerySource(tier.Segments).QueryEvents(ctx, opts)
}

// factWindowQuery answers one query over the facts this run holds in its hot
// window — nothing outside it and nothing from the storage tier it did not seed.
// The delivery recovery reads the settlement facts of the registered chains
// through it (F3/F11): the read is O(事实窗口内), the partitions come from the
// chains the replays registered, and the returned set is narrowed by the
// unexported key prefix the caller states, which is an index-free filter on the
// window and never a second scan. Every QueryOptions term applies exactly as it
// does on the observed read; the narrowing only removes records from the result.
func (r *Run) factWindowQuery(ctx context.Context, opts memory.QueryOptions) ([]memory.EventReference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query := memory.NewMemoryEventQuery()
	r.mu.Lock()
	facts := make([]event.Event, 0, len(r.factOrder))
	for _, key := range r.factOrder {
		if evt, held := r.facts[key]; held {
			facts = append(facts, evt)
		}
	}
	r.mu.Unlock()
	for _, evt := range facts {
		record, held := eventRecordOf(evt)
		if !held {
			continue
		}
		if err := query.Store(ctx, record); err != nil {
			return nil, err
		}
	}
	return query.QueryEvents(ctx, opts)
}

// factWindowEvents reports the facts this run holds in its hot window, in commit
// order: the material the delivery recovery reads the owed declarations off. The
// window — never the whole chain — is the scope, for the same reason the
// settlement query has no wider one.
func (r *Run) factWindowEvents() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]event.Event, 0, len(r.factOrder))
	for _, key := range r.factOrder {
		if evt, held := r.facts[key]; held {
			out = append(out, evt)
		}
	}
	return out
}

// registeredPartitions reports the partitions of the chains this run registered,
// derived from the replay registration keys: the recovery reads the chains the
// run actually serves instead of scanning every partition of the store. A
// registration naming no agent (the AttachReplay seam) names no partition and is
// skipped, because its session is read through the chain that does name one.
func (r *Run) registeredPartitions() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := make(map[int]bool, len(r.replays))
	partitions := make([]int, 0, len(r.replays))
	for addr := range r.replays {
		if addr.AgentID == "" {
			continue
		}
		partition := memory.PartitionIDFromName(string(addr.AgentID))
		if seen[partition] {
			continue
		}
		seen[partition] = true
		partitions = append(partitions, partition)
	}
	sort.Ints(partitions)
	return partitions
}

// pinFact marks one committed fact as one the hot window keeps however far the
// chain grows: the delivery account pins the settlement fact of every obligation
// that still owes something, so the fact is inside the window for as long as the
// obligation is. The read is taken under the run lock by the trim, and the pin
// set is written under the same lock here.
func (r *Run) pinFact(key event.EventID) {
	if key == "" {
		return
	}
	r.mu.Lock()
	if r.pinnedFacts == nil {
		r.pinnedFacts = map[event.EventID]struct{}{}
	}
	r.pinnedFacts[key] = struct{}{}
	r.mu.Unlock()
}

// unpinFact releases one pin: the obligation the fact named is settled, so the
// fact ages out on the ordinary window trim from here on.
func (r *Run) unpinFact(key event.EventID) {
	if key == "" {
		return
	}
	r.mu.Lock()
	delete(r.pinnedFacts, key)
	r.mu.Unlock()
}

// deliveryPendingFor reports whether the delivery account of this run still
// owes the named address a body: the settlement fact of an owed or held
// obligation is pinned on the hot window (host_delivery.go), and the held-with-
// retrying state is unsettled too, so the retry gap never reads as settled. The
// fact's target is the address the delivery reached, which is the address the
// reaper asks about. The scan is bounded by the fact window, never by the chain.
func (r *Run) deliveryPendingFor(addr event.Address) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.pinnedFacts {
		evt, held := r.facts[key]
		if held && evt.Target == addr {
			return true
		}
	}
	return false
}

// pinnedFact reports whether one identity is exempt from the window trim.
func (r *Run) pinnedFact(key event.EventID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, held := r.pinnedFacts[key]
	return held
}

// keepPinnedLocked drops the pinned entries from one window cut, so the trim
// keeps the facts an unsettled obligation still reads. The caller holds the run
// lock.
func (r *Run) keepPinnedLocked(cut []event.EventID) []event.EventID {
	if len(cut) == 0 || len(r.pinnedFacts) == 0 {
		return cut
	}
	kept := cut[:0]
	for _, key := range cut {
		if _, pinned := r.pinnedFacts[key]; pinned {
			continue
		}
		kept = append(kept, key)
	}
	return kept
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

// eachCommittedEvent reads every stored event in the order its commits landed
// in, ranked across partitions rather than by partition and sequence: the index
// walk answers commit order newest first, so reversing its references gives the
// commit order itself. The index walk carries no bodies; one commit-order read
// seam serves the full replay and the session reader.
func eachCommittedEvent(ctx context.Context, tier *ledger.Tier, visit func(ledger.StoredEvent) error) error {
	var refs []anchorRef
	if err := tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		refs = append(refs, anchorRef{key: key, partition: partition, seq: seq, typ: typ, session: session})
		return nil
	}); err != nil {
		return err
	}
	for i := len(refs) - 1; i >= 0; i-- {
		stored, held, err := tier.Segments.GetEvent(ctx, refs[i].key)
		if err != nil {
			return err
		}
		if !held {
			return fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, refs[i].key)
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	return nil
}

// EachSessionFact streams the durable chain of one session in commit order: the
// cold-start read of a chain, which names every fact the session committed —
// beside the window the live ledger still presents — decoded from the tier the
// way CommittedEvent decodes one identity, ordered by the partition sequence
// the landing path numbers commits with — the order the run committed in, which
// a replay refolds at the boundaries the commits recorded, while the segment
// index orders by event time and a later-judged product names a later moment of
// than the material it folded. A run beside no storage tier holds
// no durable chain, so the visit answers the resident window alone in commit
// order. A visit error ends the stream and is returned.
func (r *Run) EachSessionFact(ctx context.Context, addr event.Address, visit func(event.Event) error) error {
	r.mu.Lock()
	tier := r.tier
	r.mu.Unlock()
	if tier == nil {
		for _, key := range r.factOrder {
			evt, held := r.facts[key]
			if !held || sessionOf(evt) != addr.SessionID {
				continue
			}
			if addr.AgentID != "" && agentOf(evt) != addr.AgentID {
				continue
			}
			if err := visit(evt); err != nil {
				return err
			}
		}
		return nil
	}
	return eachCommittedEvent(ctx, tier, func(stored ledger.StoredEvent) error {
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return fmt.Errorf("tagent: replay the committed event %s: %w", stored.Key, err)
		}
		evt := body.Event()
		evt.Key = stored.Key
		if sessionOf(evt) != addr.SessionID {
			return nil
		}
		if addr.AgentID != "" && agentOf(evt) != addr.AgentID {
			return nil
		}
		return visit(evt)
	})
}

// sessionSeed is one session chain a restart rebuild collects: the projection
// it replays into, beside the window its agent declared.
type sessionSeed struct {
	projection *replay.ReplayProjection
	window     FoldWindow
}

// seedReport counts what one rebuild pass decoded. The pass reads the whole
// store: no earlier stop is safe in commit order, because a surviving original
// can sit deeper than an old product event.
type seedReport struct {
	Decoded   int
	Anchors   int
	Fallbacks int
	// Checkpoints counts the chains a framework checkpoint restored: their
	// material below the cut is read from the checkpoint, never re-decoded.
	Checkpoints int
	Landed      map[int]int64
}

// seedReplay registers the mapped sequence every session chain already
// committed, so a restarted run presents the material the tier holds to its
// next request instead of reading a transcript no process carries any more.
// The rebuild starts from the newest fold anchor of each chain and replays only
// the material committed past the checkpoint, so the decoded bodies of a boot do
// not grow with the history the folds already summarised: a chain that never
// folded, an anchor whose payload cannot be read and a store whose index carries
// no frozen type each answer the full walk of that chain instead, and the
// rebuilt state is the state the full walk answers. The delivery recovery reads
// the durable chain rather than this ledger, so the material an anchor folded
// away is still read by the obligation it settled.
// Every chain the walk registered is established as a site of this instance, so
// the active set and the unanswered rounds survive the restart instead of
// resetting with the process.
func (r *Run) seedReplay(ctx context.Context, tier *ledger.Tier) (map[int]int64, error) {
	seeds, report, err := r.seedAnchoredReplay(ctx, tier)
	if err != nil {
		return nil, err
	}
	var order []event.Address
	for addr := range seeds {
		order = append(order, addr)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].AgentID != order[j].AgentID {
			return order[i].AgentID < order[j].AgentID
		}
		return order[i].SessionID < order[j].SessionID
	})
	for _, addr := range order {
		seed := seeds[addr]
		if err := r.attachScopedReplay(addr, seed.window, seed.projection); err != nil {
			return nil, err
		}
	}
	if err := r.seedSites(ctx, order); err != nil {
		return nil, err
	}
	if len(order) > 0 {
		r.attachNoticeFeed()
	}
	return report.Landed, nil
}

// seedSites makes every session chain a restart rebuilt a site of this
// instance, so the active set a cross-session delivery resolves against is the
// set the chain holds rather than the set this process happened to open. The
// establishment is the one door every address passes, whatever admitted it, so
// a restarted run answers the same active addresses and the same unanswered
// rounds the process before it answered.
func (r *Run) seedSites(ctx context.Context, order []event.Address) error {
	for _, addr := range order {
		if !addr.IsComplete() {
			continue
		}
		if err := r.establish(ctx, addr); err != nil {
			return fmt.Errorf("tagent: establish the session site %v the chain holds: %w", addr, err)
		}
	}
	return nil
}

// seedFullReplay replays every session chain from the beginning, the path a
// chain that minted no summary answers: each committed fact is seeded, each
// mapped entry appended in commit order and each product restored on top.
func (r *Run) seedFullReplay(ctx context.Context, tier *ledger.Tier) (map[event.Address]*sessionSeed, map[int]int64, seedReport, error) {
	seeds := map[event.Address]*sessionSeed{}
	products := map[event.Address][]event.CompressProduct{}
	var order []event.Address
	report := seedReport{}
	landed := map[int]int64{}
	if err := eachCommittedEvent(ctx, tier, func(stored ledger.StoredEvent) error {
		report.Decoded++
		if stored.Seq > landed[stored.PartitionID] {
			landed[stored.PartitionID] = stored.Seq
		}
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return fmt.Errorf("tagent: replay the committed event %s: %w", stored.Key, err)
		}
		evt := body.Event()
		evt.Key = stored.Key
		r.seedFact(evt)
		addr := replayAddressOf(evt)
		if addr.SessionID == "" {
			return nil
		}
		if evt.Type == event.CompressProductType {
			product, stated := compressProductOf(evt)
			if !stated {
				assemblyWarn("tagent: the compression product %s states no readable product and is not restored", evt.Key)
				return nil
			}
			products[addr] = append(products[addr], product)
			r.compress.NoteProduct(product)
			return nil
		}
		entry, ok := mappedEntryOf(evt)
		if !ok {
			return nil
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
		seed.projection = trimReplayProjection(ctx, seed.projection, seed.window)
		return nil
	}); err != nil {
		return nil, nil, report, err
	}
	for _, addr := range order {
		seed := seeds[addr]
		seed.projection.RestoreProducts(products[addr])
	}
	return seeds, landed, report, nil
}

// anchorRef carries one resident index entry a restart rebuild reads: identity,
// position, frozen event type and session, with no event body attached.
type anchorRef struct {
	key       event.EventID
	partition int
	seq       int64
	typ       string
	session   event.SessionID
}

// anchorSlot is the isolation key one chain is rebuilt under: the partition
// the facts were filed into beside the session they belong to. Two agents that
// happen to share a session id remain two chains, as ChainKey requires.
type anchorSlot struct {
	partition int
	session   event.SessionID
}

// seedAnchoredReplay rebuilds each session chain from the newest compress
// product the index names by frozen event type, then replays the events of that
// chain committed past the newest survivor the product states. The index walk
// reads no event body; a product whose anchor payload is unreadable, and a
// session that never folded, answer the full replay of that session instead.
// Anchor discovery never inspects a fact key's spelling.
//
// - The survivors a checkpoint states are held to the same resident bound the full walk applies as it appends: a chain that declared no window keeps the recent material rather than every survivor the payload names.
// - The checkpoint slices this boot restored are the state every slice of the runtime had reached at the cut. A chain beside a fold product reads only the bodies of the facts committed past that product's newest survivor, so the entries the product covers never reach a body read and the chain's boot costs the increment. A chain the restored slices carry beside no fold product reads the bodies of its survivors and of the facts committed past the newest of them, so that boot costs the survivor count plus the increment instead of the history the survivors already state. Configuration records belong to no session chain and stay, being the declaration ledger the restarted run states that no slice carries.
// - A chain the checkpoint stated is a chain of this run even beside no fact past its cut, so its slot joins the rebuild and the tail of its chain is replayed onto the state the checkpoint states.
// - The boundary both carriers are read past is the newest survivor the payload names, which is the material the projection states rather than the cut the carrier states: a cut read before the holders were asked may state a position the survivors behind it already present, and a fact between the two is one the projection already holds.
// - A compression fact the cut did not cover but the boundary did is handed to the compression account alone, because the projection states the material it folded and the account does not state the judgment or the answer it carries.
func (r *Run) seedAnchoredReplay(ctx context.Context, tier *ledger.Tier) (map[event.Address]*sessionSeed, seedReport, error) {
	report := seedReport{}
	var refs []anchorRef
	if err := tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		refs = append(refs, anchorRef{key: key, partition: partition, seq: seq, typ: typ, session: session})
		return nil
	}); err != nil {
		return nil, report, err
	}
	report.Landed = map[int]int64{}
	for _, entry := range refs {
		if entry.seq > report.Landed[entry.partition] {
			report.Landed[entry.partition] = entry.seq
		}
	}
	seedsRestored := r.takeRestoredProjection()
	accountCut := r.checkpointCut(checkpointSliceCompress)
	anchors := map[anchorSlot]int{}
	seen := map[anchorSlot]struct{}{}
	var order []anchorSlot
	for at, ref := range refs {
		if ref.session == "" {
			continue
		}
		slot := anchorSlot{partition: ref.partition, session: ref.session}
		if _, held := seen[slot]; !held {
			seen[slot] = struct{}{}
			order = append(order, slot)
		}
		if ref.typ == event.CompressProductType {
			if _, held := anchors[slot]; !held {
				anchors[slot] = at
			}
		}
	}
	restoredSlots := map[anchorSlot]struct{}{}
	for addr := range seedsRestored {
		slot := anchorSlot{partition: memory.PartitionIDFromName(string(addr.AgentID)), session: addr.SessionID}
		restoredSlots[slot] = struct{}{}
		if _, held := seen[slot]; !held {
			seen[slot] = struct{}{}
			order = append(order, slot)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].session != order[j].session {
			return order[i].session < order[j].session
		}
		return order[i].partition < order[j].partition
	})
	if err := r.seedConfigurationFacts(ctx, tier, refs, &report); err != nil {
		return nil, report, err
	}
	seeds := map[event.Address]*sessionSeed{}
	for _, slot := range order {
		session := slot.session
		seed := &sessionSeed{projection: replay.NewReplayProjection()}
		at, hasAnchor := anchors[slot]
		usable := false
		var tailProducts []event.CompressProduct
		if base, addr, held := restoredSeedOf(seedsRestored, slot); held && !hasAnchor {
			seed.window = r.foldWindowOfAgent(addr.AgentID)
			boundary, err := r.restoreSurvivors(ctx, tier, seed, base.survivors, &report)
			if err != nil {
				return nil, report, err
			}
			seed.projection = trimReplayProjection(ctx, seed.projection, seed.window)
			tailProducts, err = r.replayChainPastCut(ctx, tier, refs, slot, seed, boundary, accountCut, "", &report)
			if err != nil {
				return nil, report, err
			}
			seed.projection.RestoreProducts(tailProducts)
			seeds[addr] = seed
			report.Checkpoints++
			usable = true
			hasAnchor = false
		}
		if hasAnchor {
			stored, held, err := tier.Segments.GetEvent(ctx, refs[at].key)
			if err != nil {
				return nil, report, err
			}
			if held {
				report.Decoded++
				body, err := memory.DecodeEventBody(stored.Body)
				if err != nil {
					return nil, report, fmt.Errorf("tagent: replay the anchor %s: %w", stored.Key, err)
				}
				evt := body.Event()
				evt.Key = stored.Key
				product, stated := compressProductOf(evt)
				var survivors []anchorEntry
				if stated {
					if err := json.Unmarshal(product.Checkpoint(), &survivors); err != nil {
						stated = false
					}
				}
				if stated && len(survivors) > 0 {
					addr := replayAddressOf(evt)
					if agent := agentOf(evt); agent != "" {
						seed.window = r.foldWindowOfAgent(agent)
					}
					boundary, err := r.restoreSurvivors(ctx, tier, seed, survivors, &report)
					if err != nil {
						return nil, report, err
					}
					seed.projection = trimReplayProjection(ctx, seed.projection, seed.window)
					r.compress.NoteProduct(product)
					report.Anchors++
					seeds[addr] = seed
					tailProducts, err = r.replayChainPastCut(ctx, tier, refs, slot, seed, boundary, accountCut, refs[at].key, &report)
					if err != nil {
						return nil, report, err
					}
					seed.projection.RestoreProducts(tailProducts)
					usable = true
				} else {
					assemblyWarn("tagent: the anchor %s of session %s states no readable survivor payload, falling back to the full replay", refs[at].key, session)
				}
			} else {
				assemblyWarn("tagent: the anchor %s of session %s is missing from the chain, falling back to the full replay", refs[at].key, session)
			}
		}
		if !usable {
			report.Fallbacks++
			var products []event.CompressProduct
			agent := event.AgentID("")
			for i := len(refs) - 1; i >= 0; i-- {
				if refs[i].session != slot.session || refs[i].partition != slot.partition {
					continue
				}
				product, stated, owner, err := r.replayRef(ctx, tier, refs[i].key, seed, &report)
				if err != nil {
					return nil, report, err
				}
				if agent == "" {
					agent = owner
				}
				if stated {
					products = append(products, product)
				}
			}
			seed.projection.RestoreProducts(products)
			seeds[event.Address{AgentID: agent, SessionID: session}] = seed
		}
	}
	return seeds, report, nil
}

// seedConfigurationFacts seeds the indexed facts that belong to no session
// chain: the configuration chain records of the tier. The full walk seeds them
// beside the session material, so a boot that rebuilds from anchors holds them
// too — a configuration change a restart reads back by identity is one of them,
// and its retention is the ledger a restarted run states about the configuration
// it serves. The facts are seeded in commit order, and none of them may be
// answered by the anchor of a session it never belonged to.
func (r *Run) seedConfigurationFacts(ctx context.Context, tier *ledger.Tier, refs []anchorRef, report *seedReport) error {
	for i := len(refs) - 1; i >= 0; i-- {
		if refs[i].session != "" || refs[i].typ == event.TypeCheckpoint {
			continue
		}
		stored, held, err := tier.Segments.GetEvent(ctx, refs[i].key)
		if err != nil {
			return err
		}
		if !held {
			continue
		}
		report.Decoded++
		body, err := memory.DecodeEventBody(stored.Body)
		if err != nil {
			return fmt.Errorf("tagent: replay the committed event %s: %w", stored.Key, err)
		}
		evt := body.Event()
		evt.Key = stored.Key
		r.seedFact(evt)
	}
	return nil
}

// foldWindowOfAgent answers the replay window one chain's agent declared.
func (r *Run) foldWindowOfAgent(agent event.AgentID) FoldWindow {
	r.mu.Lock()
	defer r.mu.Unlock()
	return foldWindowOf(specOfLocked(r.current.Plan.Declaration, agent))
}

// restoreSurvivors hydrates one anchor payload in display order: a key the
// chain still holds is read back through the fact->projection mapping so its
// private fields are recovered and its text is the chain's, and a fold product —
// whose text the product fact files under an identity of its own — is stated from
// the text the payload carries. Nothing here reads a kind off a key's prefix, and
// a survivor with neither a mapped fact nor a carried text refuses the boot by
// name instead of presenting an entry with no text.
func (r *Run) restoreSurvivors(ctx context.Context, tier *ledger.Tier, seed *sessionSeed, survivors []anchorEntry, report *seedReport) (int64, error) {
	boundary := int64(0)
	for _, survivor := range survivors {
		stored, held, err := tier.Segments.GetEvent(ctx, survivor.Key)
		if err != nil {
			return 0, err
		}
		if held {
			if stored.Seq > boundary {
				boundary = stored.Seq
			}
			report.Decoded++
			body, err := memory.DecodeEventBody(stored.Body)
			if err != nil {
				return 0, fmt.Errorf("tagent: hydrate the survivor %s: %w", stored.Key, err)
			}
			evt := body.Event()
			evt.Key = stored.Key
			r.seedFact(evt)
			if entry, ok := mappedEntryOf(evt); ok {
				seed.projection.Append(ctx, entry)
				continue
			}
		}
		if survivor.Content == "" {
			return 0, fmt.Errorf("%w: %s", ErrCheckpointSurvivorUnstated, survivor.Key)
		}
		seed.projection.Append(ctx, replay.ProjectionEntry{
			Key:     survivor.Key,
			Type:    survivor.Type,
			Summary: survivor.Summary,
			Content: survivor.Content,
			Time:    time.UnixMilli(survivor.Time),
		})
	}
	return boundary, nil
}

// replayChainPastCut replays one chain onto the state its carrier states and
// answers the products a restart applies on top of that state: a fact committed
// past the boundary is replayed whole, a compression fact the carrier's cut did
// not cover but the boundary did is handed to the compression account alone, and
// the material the boundary covers is read by neither path because the carrier
// already states it.
//
//   - The boundary is the newest survivor the carrier names, never the cut it states: the state read at the quiet point carries every commit that landed before the holders were asked, so the survivors may stand past the watermark. A fact between the two landed after the cut and before the boundary, so the carrier holds the material it folded and the replay must not seat it twice.
//
// The covered range costs one index walk and no body read. The exception is a
// compression fact of that range, which the carrier's account states only up to
// the cut: a carrier stating no account at all leaves the range unread.
func (r *Run) replayChainPastCut(ctx context.Context, tier *ledger.Tier, refs []anchorRef, slot anchorSlot, seed *sessionSeed, boundary int64, accountCut event.CheckpointOffset, skip event.EventID, report *seedReport) ([]event.CompressProduct, error) {
	var tail, behind []anchorRef
	for _, ref := range refs {
		if ref.session != slot.session || ref.partition != slot.partition || ref.key == skip {
			continue
		}
		if ref.seq > boundary {
			tail = append(tail, ref)
			continue
		}
		if accountCut != nil && event.CompressionMechanism(ref.typ) && pastCut(accountCut, ref) {
			behind = append(behind, ref)
		}
	}
	sort.Slice(behind, func(i, j int) bool { return behind[i].seq < behind[j].seq })
	for _, ref := range behind {
		if err := r.seedAccountFact(ctx, tier, ref.key, report); err != nil {
			return nil, err
		}
	}
	sort.Slice(tail, func(i, j int) bool { return tail[i].seq < tail[j].seq })
	products := make([]event.CompressProduct, 0, len(tail))
	for _, ref := range tail {
		product, stated, _, err := r.replayRef(ctx, tier, ref.key, seed, report)
		if err != nil {
			return nil, err
		}
		if stated {
			products = append(products, product)
		}
	}
	return products, nil
}

// seedAccountFact seeds one compression fact into the compression account of a
// rebuilding run without appending it to the projection: the fact stands behind
// the boundary the anchor states, so the projection already holds the material it
// folded, while the account the carrier states was taken at a cut of its own and
// owes the judgment or the answer this fact carries.
func (r *Run) seedAccountFact(ctx context.Context, tier *ledger.Tier, key event.EventID, report *seedReport) error {
	stored, held, err := tier.Segments.GetEvent(ctx, key)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, key)
	}
	report.Decoded++
	body, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return fmt.Errorf("tagent: replay the committed event %s: %w", stored.Key, err)
	}
	evt := body.Event()
	evt.Key = stored.Key
	r.seedFact(evt)
	if evt.Type == event.CompressProductType {
		if product, stated := compressProductOf(evt); stated {
			r.compress.NoteProduct(product)
		}
	}
	return nil
}

// replayRef decodes one indexed event and applies it to a rebuilding seed:
// the live ledger takes every fact, the projection takes every mapped entry,
// and a product fact is left to the anchor that already stated its result.
// replayRef replays one committed event of a chain into the seed the walk
// carries and answers the compression product the event states beside the agent
// that committed it.
//
// - A product reaches the compression ledger whichever side of the anchor it stands on, so a restarted run never executes a judgment a second time.
func (r *Run) replayRef(ctx context.Context, tier *ledger.Tier, key event.EventID, seed *sessionSeed, report *seedReport) (event.CompressProduct, bool, event.AgentID, error) {
	stored, held, err := tier.Segments.GetEvent(ctx, key)
	if err != nil {
		return event.CompressProduct{}, false, "", err
	}
	if !held {
		return event.CompressProduct{}, false, "", nil
	}
	report.Decoded++
	body, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return event.CompressProduct{}, false, "", fmt.Errorf("tagent: replay the committed event %s: %w", stored.Key, err)
	}
	evt := body.Event()
	evt.Key = stored.Key
	r.seedFact(evt)
	if evt.Type == event.CompressProductType {
		product, stated := compressProductOf(evt)
		if stated {
			r.compress.NoteProduct(product)
		}
		return product, stated, agentOf(evt), nil
	}
	if seed.window.MaxTokens == 0 {
		if agent := agentOf(evt); agent != "" {
			seed.window = r.foldWindowOfAgent(agent)
		}
	}
	entry, ok := mappedEntryOf(evt)
	if !ok {
		return event.CompressProduct{}, false, agentOf(evt), nil
	}
	seed.projection.Append(ctx, entry)
	seed.projection = trimReplayProjection(ctx, seed.projection, seed.window)
	return event.CompressProduct{}, false, agentOf(evt), nil
}

// seedFact holds one fact the storage tier already committed in the live
// ledger of this run: the same map every commit of this process lands in, in
// the commit order the tier recorded. A restarted run then reads its facts
// back beside the fresh ones — the identity one projected entry was minted from
// resolves again, so the call-result pairing a request replays is recovered
// from the facts alone — and a re-commit of an identity the tier already holds
// is judged against the very fact it names instead of writing a second one.
// A compression judgment the chain carries is handed to the consumer that
// executes judgments, so the material a judgment names folds in this process
// too; the products of the chain answer their judgments beside it, so a
// judgment already executed is never executed twice.
//
// The rebuild reaches one fact by more than one read path — the survivors a
// checkpoint states are hydrated by key and the tail of their chain is replayed
// whole over them — so the seed answers the fact the ledger already holds
// instead of filing its key twice: one identity names one immutable fact, and a
// commit order naming it twice would read one fact as two.
func (r *Run) seedFact(evt event.Event) {
	if evt.Type == event.CompressRequestType {
		r.compress.noteSeededJudgment(evt)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, held := r.facts[evt.Key]; held {
		return
	}
	r.facts[evt.Key] = evt
	r.factOrder = append(r.factOrder, evt.Key)
}

// eventRecordOf projects one committed fact onto the query record the
// durable query reads it under: the partition its owning agent resolves to,
// its frozen type, its summary and content, and its event time on the
// millisecond axis. A fact naming no owning agent belongs to no partition and
// is not observed rather than filed under an invented one. A compression
// mechanism fact carries an internal summary and an anchor payload: both are
// recovery metadata read by the rebuild, so the read face refuses the fact by
// its frozen type and never surfaces that wording into the model's context.
func eventRecordOf(evt event.Event) (memory.EventRecord, bool) {
	if evt.Key == "" {
		return memory.EventRecord{}, false
	}
	if event.CompressionMechanism(evt.Type) {
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

// inspectorSubject and projectionWindow are the named constants of the
// assembly: the subject the diagnostic read face of the run reads the state
// owner's sites under, and how many recent readings the projection keeps
// servable.
const (
	inspectorSubject = state.Subject("tagent-assembly-inspector")
	projectionWindow = 8
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
// ways. An engine beside no tier is refused by name: without a durable chain
// query-time embedding has no candidates. A document that declares
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

// Embedder reports the binding candidate queries call on demand:
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

// resolveMemory folds the declared memory plan into the faces this assembly
// opens. A face an option states directly keeps it, so a host that wires its
// own embedder over a declared tier is never overridden. The declaration is the
// one source of the storage and embedding faces. The retrieval leg bounds and
// the fusion rank constant are mechanism constants: both legs run under the one
// page bound and the fusion keeps its canonical rank constant.
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
	return nil
}

// declaresEngine reports whether a memory engine section names anything.
func declaresEngine(engine config.EngineSpec) bool {
	return strings.TrimSpace(engine.Backend) != "" ||
		strings.TrimSpace(engine.Embedding.Provider) != "" ||
		strings.TrimSpace(engine.Embedding.Model) != "" ||
		strings.TrimSpace(engine.Embedding.APIKeyEnv) != "" ||
		engine.Embedding.Dimensions != 0 ||
		strings.TrimSpace(engine.Embedding.APIEndpoint) != ""
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
	if err := embedder.Validate(engine.Embedding); err != nil {
		return fmt.Errorf("%w: %w", ErrMemoryEngine, err)
	}
	return nil
}

// ErrModelLeaseMissing and the lease wiring errors beside it name the way the
// model lease wiring was refused, so a host routes each by name instead of
// matching on message text.
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
// serves the agent, the rebuild a model configuration change runs and the
// publication entry the rebuilt model is installed through. A lease attached
// beside no rebuild refuses every change at that step by name; a nil publish
// records the installed identity beside the run and installs the version in the
// lease only.
type leaseState struct {
	lease   *rl.ModelLease
	rebuild func(version string) (model.Model, error)
	publish func(rl.ModelIdentity, model.Model) error
}

// ModelLeaseOption configures the model lease one agent is assembled with.
type ModelLeaseOption func(*leaseState)

// WithLeaseRebuild names the rebuild step a model configuration change runs:
// the step receives the version the change asks for and answers the model that
// version is installed as. A rebuild that fails installs no version, and the
// lease keeps the version in effect.
func WithLeaseRebuild(rebuild func(version string) (model.Model, error)) ModelLeaseOption {
	return func(state *leaseState) { state.rebuild = rebuild }
}

// WithModelLease attaches the model lease of one agent to this assembly: the
// agent's binding carries the lease's stable model handle instead of the model
// the host contributed, every committed model configuration change owned by the
// agent is consumed by the lease observer mounted on its pipeline, and the
// lease's rebuild-publication-installation order decides whether the requested
// version takes effect. A rebuild and a publication entry are configured
// through the model lease options; a lease attached beside no rebuild refuses
// every change at that step by name.
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
// Boot writes the table for the first generation, and a publication that adds a
// declared agent extends it; the lock keeps a concurrent publication from reading
// either write mid-way.
func (r *Run) leaseOf(agentID event.AgentID) *leaseState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.leases[agentID]
}

// modelLeaseBinding is one model lease a publication attaches to this run: the
// lease state the agent's binding borrows the version from and the replacement
// consumer mounted on its pipeline.
type modelLeaseBinding struct {
	agent    event.AgentID
	held     leaseState
	observer *rl.LeaseObserver
}

// mountLeaseObservers mounts the replacement consumer of every attached lease
// on the agent's pipeline: one fact->observation face that reads a committed
// model configuration change back and applies it through the lease's own rebuild,
// publication and installation order. A lease observer the run cannot build is
// refused before the first publication, so a half-wired assembly is never
// returned.
func (r *Run) mountLeaseObservers() error {
	for id, held := range r.leases {
		binding, err := r.buildModelLeaseBinding(id, *held)
		if err != nil {
			return err
		}
		r.leaseObservers[id] = binding.observer
	}
	return nil
}

// buildModelLeaseBinding builds the replacement consumer of one attached lease
// through the same construction mountLeaseObservers runs at assembly. It builds
// beside the run and mounts nothing: a lease the assembly did not attach is
// mounted only after the generation that declared its agent published.
func (r *Run) buildModelLeaseBinding(agentID event.AgentID, held leaseState) (modelLeaseBinding, error) {
	observer, err := rl.NewLeaseObserver(agentID, held.lease, runFacts{run: r}, r.rebuildLeaseModel(agentID), r.publishLease(agentID))
	if err != nil {
		return modelLeaseBinding{}, fmt.Errorf("tagent: mount the model lease observer of agent %q: %w", agentID, err)
	}
	return modelLeaseBinding{agent: agentID, held: held, observer: observer}, nil
}

// installModelLeases mounts the lease and observer of every agent a published
// generation added, under the run lock: the next publication reads them through
// the lease table and observer map the assembly filled, so a model change of an
// agent added at runtime is served by the lease path. A lease the run already
// holds is left as it stands, so re-adding an agent serves the version in effect
// instead of a second lease beside it.
func (r *Run) installModelLeases(bindings []modelLeaseBinding) {
	if len(bindings) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, binding := range bindings {
		if _, held := r.leases[binding.agent]; held {
			continue
		}
		held := binding.held
		r.leases[binding.agent] = &held
		r.leaseObservers[binding.agent] = binding.observer
	}
}

// rebuildLeaseModel reports the rebuild step one agent's lease replacement
// runs: the explicit rebuild the host named when it attached the lease. A lease
// attached beside no rebuild answers a named refusal, so no model is invented
// for a version nothing can build.
func (r *Run) rebuildLeaseModel(agentID event.AgentID) func(version string) (model.Model, error) {
	return func(version string) (model.Model, error) {
		held := r.leaseOf(agentID)
		if held == nil || held.rebuild == nil {
			return nil, fmt.Errorf("%w: no rebuild is attached for agent %q version %q", rl.ErrLeaseRebuildFailed, agentID, version)
		}
		return held.rebuild(version)
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

// mountCapture attaches the trajectory capture and the training recorder to the
// loop's one evidence face: the run admits a decision's request view and an
// attempt's reduced outcome once, and hands that admission to every attached
// face, so the trajectory and the record pipeline never stand as two production
// points of one attempt. A capture beside no scope is refused by name before the
// first publication.
func (r *Run) mountCapture() error {
	faces := make([]agent.Capture, 0, 2)
	if r.capture != nil {
		scope, ok := r.capture.(rl.CaptureScope)
		if !ok {
			return fmt.Errorf("%w: %T", ErrCaptureUnscoped, r.capture)
		}
		adapter, err := rl.NewCaptureAdapter(scope)
		if err != nil {
			return err
		}
		faces = append(faces, adapter)
		r.captureScope = scope
	}
	if r.recorder != nil {
		faces = append(faces, rl.NewRecorderAdapter(r.recorder))
	}
	if len(faces) == 0 {
		return nil
	}
	r.loop.SetCapture(captureFaces(faces))
	return nil
}

// captureFaces hands one loop admission to every attached face in order: each
// face reads the same view material of a decision and the same reduced outcome
// of an attempt, and the first face that refuses it reports the refusal, so an
// admission is never counted as taken by a face it never reached.
type captureFaces []agent.Capture

// Snapshot fixes the view of one decision's request on every attached face.
func (f captureFaces) Snapshot(ctx context.Context, decisionID event.DecisionID, modelName, promptVersion string, visibleTools, visibleSkills []string, req model.Request) error {
	for _, face := range f {
		if err := face.Snapshot(ctx, decisionID, modelName, promptVersion, visibleTools, visibleSkills, req); err != nil {
			return err
		}
	}
	return nil
}

// Record admits one observed attempt on every attached face.
func (f captureFaces) Record(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) error {
	for _, face := range f {
		if err := face.Record(ctx, attemptID, decisionID, episodeID, outcome); err != nil {
			return err
		}
	}
	return nil
}

// Seal closes every attached face: a seal refused by one face is reported rather
// than read as a completed capture of the turn.
func (f captureFaces) Seal(ctx context.Context) error {
	for _, face := range f {
		if err := face.Seal(ctx); err != nil {
			return err
		}
	}
	return nil
}

// noteModelConfigurationChange files the judgment that one agent's model
// reference moves onto a published generation as one configuration change of
// the model dimension. The fact commits through the loop's commit face so the
// After pass of the owning agent's pipeline observes it exactly as the loop's
// own facts are observed. A repeated judgment commits no second fact but still
// runs that pass, so an install the lease refused is retried by the next
// judgment that names the same move.
//
// The identity of the committed fact is returned; a change this run cannot
// file — no lease for the agent, no target version, no commit face — is refused
// by name, and an empty from version is the version the lease holds.
func (r *Run) noteModelConfigurationChange(ctx context.Context, agentID event.AgentID, generation uint64, from, to string) (event.EventID, error) {
	held := r.leaseOf(agentID)
	if held == nil || held.lease == nil {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeOwnerMissing, agentID)
	}
	if to == "" {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeTargetMissing, agentID)
	}
	if from == "" {
		from = held.lease.CurrentVersion()
	}
	fact := event.ConfigChangeModelFor(agentID, generation, from, to, event.ModelReplacementResultRequested)
	if r.loop == nil {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeUncommitted, agentID)
	}
	key, commitErr := r.loop.CommitFactFor(ctx, agentID, fact.Event(time.Now().UTC()))
	if commitErr != nil {
		return "", fmt.Errorf("tagent: commit the model configuration change of agent %q: %w", agentID, commitErr)
	}
	if key == "" {
		return "", fmt.Errorf("%w: agent %q", ErrModelChangeUncommitted, agentID)
	}
	return key, nil
}

// recordLeaseLoss records one model configuration change whose fact did not
// commit beside the run, so a judgment that reached no ledger is readable as the
// loss it is.
func (r *Run) recordLeaseLoss(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	r.leaseLosses = append(r.leaseLosses, err)
	r.mu.Unlock()
}

// LeasePublications reports the versions the model leases installed through
// this run's default publication entry, in installation order, as a copy the
// caller owns.
func (r *Run) LeasePublications() []rl.ModelIdentity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]rl.ModelIdentity(nil), r.leasePublications...)
}

// LeaseLosses reports the model configuration changes whose fact could not
// commit, as a copy the caller owns: a judgment that reached no ledger is named
// here rather than read as a move that happened.
func (r *Run) LeaseLosses() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.leaseLosses...)
}

// metaKeyFinalRoundRange, metaKeyFinalRoundReason, metaKeyFinalRoundRounds and
// metaKeyFinalRoundElapsed are the metadata keys a final round request fact states
// the fields of its judgment under: a request is a judgment about a range and never
// material of a transcript, so the event metadata is the one durable carrier of
// what it judged.
const (
	metaKeyFinalRoundRange   = "final_round_range"
	metaKeyFinalRoundReason  = "final_round_reason"
	metaKeyFinalRoundRounds  = "final_round_rounds"
	metaKeyFinalRoundElapsed = "final_round_elapsed"
)

// finalRoundChain is the chain face the final round planner reads one commit
// through: the live ledger of this run beside the declaration it published last.
type finalRoundChain struct{ run *Run }

// The face satisfies the chain port the planner judges and files through.
var _ agent.FinalRoundChain = finalRoundChain{}

// AddressOf reports the (agent, session) address one committed fact belongs to;
// a fact this run holds no record of, and a fact naming no agent or no session,
// belongs to no chain.
func (c finalRoundChain) AddressOf(_ context.Context, id event.EventID) (event.Address, bool) {
	c.run.mu.Lock()
	evt, held := c.run.facts[id]
	c.run.mu.Unlock()
	if !held {
		return event.Address{}, false
	}
	addr := event.Address{AgentID: agentOf(evt), SessionID: sessionOf(evt)}
	return addr, addr.IsComplete()
}

// SegmentOf reports the segment one address flies, as one budget judgment reads
// it.
func (c finalRoundChain) SegmentOf(_ context.Context, addr event.Address) (agent.FinalRoundSegment, bool) {
	return c.run.finalRoundSegmentOf(addr)
}

// FileFinalRoundRequest files one judgment on the chain of the address whose
// budget it judged.
func (c finalRoundChain) FileFinalRoundRequest(ctx context.Context, addr event.Address, request event.FinalRoundRequest) error {
	return c.run.fileFinalRoundRequest(ctx, addr, request)
}

// finalRoundSegmentOf reports the segment one address flies, read as one budget
// judgment reads it: the facts committed past the last agent_output of that
// chain, the range the newest of them names, the tool rounds that range ran and
// the moment the earliest of them landed. The agent_output fact closes the
// segment before it, so the material a closed range produced is never judged as
// the material of the range in flight.
//
// The redline list (3 rules):
// MUST read the segment from the committed facts of the address alone.
// MUST answer no segment for an address beside no fact past its last agent_output.
// MUST NOT count a round the range did not run: only the call facts of the range in flight are rounds.
func (r *Run) finalRoundSegmentOf(addr event.Address) (agent.FinalRoundSegment, bool) {
	if !addr.IsComplete() {
		return agent.FinalRoundSegment{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	segment := make([]event.Event, 0, len(r.factOrder))
	for at := len(r.factOrder) - 1; at >= 0; at-- {
		evt, held := r.facts[r.factOrder[at]]
		if !held || sessionOf(evt) != addr.SessionID || agentOf(evt) != addr.AgentID {
			continue
		}
		if evt.Type == event.TypeAgentOutput {
			break
		}
		segment = append(segment, evt)
	}
	if len(segment) == 0 {
		return agent.FinalRoundSegment{}, false
	}
	rangeID := event.InvocationID("")
	for _, evt := range segment {
		if evt.InvocationID != "" {
			rangeID = evt.InvocationID
			break
		}
	}
	started := segment[len(segment)-1].Timestamp
	if rangeID == "" || started.IsZero() {
		return agent.FinalRoundSegment{}, false
	}
	rounds := 0
	for _, evt := range segment {
		if evt.Type == event.TypeThinkingPlan && evt.InvocationID == rangeID {
			rounds++
		}
	}
	return agent.FinalRoundSegment{Range: rangeID, Rounds: rounds, Started: started}, true
}

// fileFinalRoundRequest lands one final round judgment in the live ledger of
// this run under the identity the judgment is quoted by. The fact is the
// judgment itself: it carries no message, so it never enters the projection it
// judged, and it commits outside the After pass, so the judgment just filed
// never re-enters the observation that filed it.
// A range already carrying a request answers as filed, whatever reason that
// earlier judgment named: one range carries one request, so a later evaluation
// of the same range never rewrites the fact the first one filed. A judgment the
// ledger refused is recorded at this call site rather than answered as filed.
func (r *Run) fileFinalRoundRequest(ctx context.Context, addr event.Address, request event.FinalRoundRequest) error {
	if !addr.IsComplete() {
		return errors.New("tagent: the final round judgment names no agent address")
	}
	if request.ID == "" || request.Range == "" {
		return errors.New("tagent: the final round judgment names no identity")
	}
	r.mu.Lock()
	filed := r.finalRoundFiledLocked(request.Range)
	r.mu.Unlock()
	if filed {
		return nil
	}
	if _, err := r.commitFactChecked(ctx, finalRoundRequestEvent(addr, request, time.Now())); err != nil {
		if errors.Is(err, memory.ErrFactKeyConflict) {
			return nil
		}
		assemblyWarn("tagent: the final round judgment of range %s was not committed err=%v", request.Range, err)
	}
	return nil
}

// finalRoundFiledLocked reports whether the chain already carries a request for
// one range: the judgment identity is content-addressed over the range and the
// reason, so both reasons a judgment can name are read back by the identity they
// would file. The read is taken under the run lock the caller holds.
func (r *Run) finalRoundFiledLocked(rangeID event.InvocationID) bool {
	for _, reason := range []string{string(agent.FinalRoundRounds), string(agent.FinalRoundBudget)} {
		if _, held := r.facts[event.FinalRoundRequestFor(rangeID, reason, 0, 0).ID]; held {
			return true
		}
	}
	return false
}

// finalRoundRequestEvent renders one judgment as the fact it commits as: the
// judgment identity as the event key, the address whose range was judged as the
// owner and as the range the fact belongs to, and the five fields of the
// judgment in the event metadata. The message stays empty, because a judgment
// about a range is not material of its transcript and must never enter the
// projection it judged.
func finalRoundRequestEvent(addr event.Address, request event.FinalRoundRequest, at time.Time) event.Event {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return event.Event{
		Key:       request.ID,
		Type:      request.FactType(),
		Timestamp: at,
		Meta: map[string]string{
			event.MetaKeyEventSummary: "final round request " + request.Reason,
			metaKeyFinalRoundRange:    string(request.Range),
			metaKeyFinalRoundReason:   request.Reason,
			metaKeyFinalRoundRounds:   strconv.Itoa(request.Rounds),
			metaKeyFinalRoundElapsed:  request.Elapsed.String(),
		},
		Owner:        addr,
		InvocationID: request.Range,
	}
}

// finalRoundPolicyOf reports the watchdog budget the drives of one agent run
// under: the budget the declaration in effect names for that agent, and the
// entry agent's budget while it names none of its own. The read runs over the
// same declaration the loop's budget table was set from, so the judgment the
// planner files names the policy the drive runs under. An assembly beside no
// published declaration answers the zero policy, which keeps every drive
// unbounded.
func (r *Run) finalRoundPolicyOf(agentID event.AgentID) agent.FinalRoundPolicy {
	r.mu.Lock()
	decl := r.current.Plan.Declaration
	r.mu.Unlock()
	if decl == nil {
		return agent.FinalRoundPolicy{}
	}
	if policy, held := agentFinalRoundPolicies(decl)[agentID]; held {
		return policy
	}
	return finalRoundPolicy(decl)
}

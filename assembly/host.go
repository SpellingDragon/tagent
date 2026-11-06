// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Boot is the turnkey entry of the composition root: it reads one host
// document, resolves the declaration it carries, and returns the Host the
// application drives. The Host owns the assembled run, the scheduled
// reflection and the two loopback endpoints; the application fills the
// outbound message face, the approval face and the reflection delivery face
// through the options, and reads nothing else out of the document.
package assembly

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/features/telemetry"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	fmetric "trpc.group/trpc-go/trpc-agent-go/telemetry/metric"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// ErrHostConfigMissing reports a Boot call that names no configuration path:
// the host has no document to read, so no runtime is assembled.
var ErrHostConfigMissing = errors.New("tagent: the host names no configuration path")

// ErrHostSenderMissing reports a host that admits an invocation or a drive at
// the user address beside no outbound message sender: the turn would produce
// output nothing can deliver, so the run is refused by name. A document that
// grants the send tool demands no sender, because a send reaches no channel.
var ErrHostSenderMissing = errors.New("tagent: the host carries no outbound message sender")

// ErrHostMeditationToolMisplaced reports a document whose reflection would
// deliver through a face it does not carry: an independent curation agent that
// enables meditation with a declared deliver_to white list but declares no
// send tool could never write its delivery, so the boot refuses it by name.
// Host self-reflection is not judged here: its dedicated reflection session is
// granted send by the meditation plugin (session-scoped), while the entry tool
// face deliberately carries none.
var ErrHostMeditationToolMisplaced = errors.New("tagent: a meditation tool is granted to the wrong agent")

// ErrHostAddressMissing reports an invocation that names no complete session
// address: the runtime is driven at an explicit address and never at a
// most-recent one.
var ErrHostAddressMissing = errors.New("tagent: the invocation names no complete session address")

// ErrHostClosed reports an invocation or endpoint drive that reached a host
// whose close already began.
var ErrHostClosed = errors.New("tagent: the host is closed")

// ErrHostAgentUndeclared reports an invocation whose address names an agent the
// declaration in effect does not declare: a generation that removed an agent
// answers new input at that address by name, while every fact the agent already
// committed stays on the chain and no cancellation is invented for it. The
// address of an agent the generation still declares is never refused here, even
// when it stopped being the entry.
var ErrHostAgentUndeclared = errors.New("tagent: the invocation names an agent the published organization does not declare")

// ErrHostNoActiveSession reports a task request arriving before any user turn:
// the endpoint carries no session identity of its own, so it drives the one
// session the host admitted last.
var ErrHostNoActiveSession = errors.New("tagent: no user session has been admitted yet")

// ErrHostUnauthenticated reports a task request whose subject did not
// authenticate: a configured credential must arrive as a bearer token, and a
// face beside no credential admits the loopback peer only.
var ErrHostUnauthenticated = errors.New("tagent: the task request is not authenticated")

// ErrHostEmptyTask reports a task request whose messages carry no user
// content: a turn reads user input only, so the request is refused by name.
var ErrHostEmptyTask = errors.New("tagent: the task request carries no user message")

// ErrDeliveryNotFound reports an approval or refusal that names no held
// delivery: no hold was issued under that identity, or a decision already
// consumed it. A repeated decision is refused by name and delivers nothing.
var ErrDeliveryNotFound = errors.New("tagent: no held delivery names that identity")

// MessageSender is the outbound message face the host delivers turn output
// through: one body, one explicit address, and the delivery outcome reported
// to the caller.
//
// The redline list (3 rules):
// MUST deliver the body to the explicit address the host names.
// MUST NOT resolve a target from a most-recent session or from the body.
// MUST NOT report a refused delivery as a delivered one.
type MessageSender interface {
	// Send delivers one body to the target address.
	Send(ctx context.Context, target event.Address, content string) error
}

// hostDeliveryMeter wraps the outbound face of the host and counts the bodies it
// accepted. Every delivery of the host reaches the face through this meter — the
// answer of a user turn and the body a declared send tool delivers alike — so
// the counter face publishes one number for the messages the deployment really
// sent. A refused body never moves the counter.
type hostDeliveryMeter struct {
	sender   MessageSender
	accepted atomic.Uint64
}

// Send delivers one body through the wrapped face and counts the accepted
// delivery.
func (m *hostDeliveryMeter) Send(ctx context.Context, target event.Address, content string) error {
	if err := m.sender.Send(ctx, target, content); err != nil {
		return err
	}
	m.accepted.Add(1)
	return nil
}

// acceptedCount reports the bodies the face accepted so far; a host beside no
// sender reads no delivery.
func (m *hostDeliveryMeter) acceptedCount() uint64 {
	if m == nil {
		return 0
	}
	return m.accepted.Load()
}

// HostOption configures one Boot call. It is the host-facing name of the
// assembly option, so the host protocol's option type is declared here rather
// than borrowed by alias.
type HostOption func(*assemblyConfig)

// WithSender attaches the outbound message face every turn output of the host
// is delivered through.
func WithSender(sender MessageSender) HostOption {
	return func(c *assemblyConfig) { c.hostSender = sender }
}

// WithDeliveryRuler attaches the review plugin's delivery face: the host rules
// every outbound body through it before the sender is called. A host beside no
// ruler delivers every body, so the default review allows every delivery.
func WithDeliveryRuler(ruler plugin.DeliveryRuler) HostOption {
	return func(c *assemblyConfig) { c.deliveryRuler = ruler }
}

// hostNativeBuilder constructs the native pieces of one declared agent from one
// document, through the construction Boot ran: the model the provider registry of
// the document names beside the tools the declaration contributes.
type hostNativeBuilder func(context.Context, *hostDocument, event.AgentID) (NativeAgent, error)

// Host is the assembled runtime of one host document: the run the entry
// drives, the scheduled reflection beside it and the two loopback endpoints a
// supervisor and an inbound channel reach. The host holds no application
// concept of its own: every channel identity, approval ruling and delivery
// target arrives through the options and the invocation address.
type Host struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    *Run
	// entry is the entry agent Boot published as the first generation. It is the
	// initial value and the fallback of a host whose run published no generation;
	// every reader of the entry in service reads the generation in effect through
	// entryAgent, so a reload that moves the entry speaks for the next turn instead
	// of a field the boot froze.
	entry  event.AgentID
	sender MessageSender
	// deliveries counts every body the outbound face accepted, whether the host
	// delivered a turn answer or a declared send tool delivered its own body.
	deliveries *hostDeliveryMeter
	// deliveryRuler rules every turn body before the sender is called; a nil
	// ruler keeps every delivery allowed.
	deliveryRuler plugin.DeliveryRuler
	// pendingDeliveries holds the bodies the host owes a target before the
	// sender ran or was refused, keyed by the identity an approval or refusal
	// names, and heldSeq orders the identities. Every obligation is recorded
	// under the storage tier beside the table entry, so the table is rebuilt
	// from the records beside a restart.
	pendingDeliveries map[string]*heldDelivery
	heldSeq           uint64
	// deliveryStates is the host's in-process view of the settlement facts it
	// wrote or restored: the delivery identity beside the latest settlement
	// state token. The facts remain the truth — this table only spares a window
	// read on the per-body path and is rebuilt from the chain on every boot.
	deliveryStates map[string]string
	// deliveryPins names the settlement fact kept hot for every obligation that
	// still owes something, so settling it releases the pin of the fact it
	// replaces. The facts themselves carry the pins on the run's window.
	deliveryPins map[string]event.EventID
	// deliveryClaims names the obligations one live path of this process already
	// claimed to deliver, so the closed segment, the hand-off and the retry
	// consumer never send one obligation twice at the same time.
	deliveryClaims map[string]bool
	// retries is the DeliveryConsumer: the self-timed redrive of the obligations
	// the account still owes. It is built on the first obligation that needs it
	// and stopped by the host close; it never listens to commits, because its
	// timing is the state machine's own clock.
	retries *deliveryConsumer
	// deliveryBackoff is the wait before the k-th retry of one obligation; zero
	// reads the family constant. It is the one dial a check shortens to run the
	// exhaustion path without waiting the production sequence out.
	deliveryBackoff time.Duration
	// deliveryLog is the fact surface the delivery account reads and writes; nil
	// reads the storage tier the run opened. It is the seam the settlement
	// checks install a counting log on.
	deliveryLog state.FactLog

	// configPath is the source the host was booted from and the file a reload
	// reads again; doc is the document last published, natives are the tool
	// instances every declared agent was assembled with, nativeFor builds one
	// agent's pieces from a document through the construction Boot ran, models
	// builds a model from the document in effect and capture is the run-wide
	// trajectory.
	configPath string
	doc        *hostDocument
	natives    map[event.AgentID]NativeAgent
	nativeFor  hostNativeBuilder
	models     *hostModelBuilder
	capture    *rl.TrajectoryCapture

	face       *hostFace
	meditation *meditation.Driver
	settle     *SettleDriver
	// consumers owns the resident instances of the run: one per agent address
	// its material names, each the only driver of the model decisions of its
	// address. It stays nil beside no assembled run loop.
	consumers *agent.Consumers
	// execManager is the execution resource ledger the declared exec face
	// acquires through; the host is the production consumer of its lifecycle:
	// the manager observes state changes after a call returned, and the host
	// releases every held resource when it closes.
	execManager *tagentexec.Manager
	// execWG counts the follow-up turns one execution state event drives, so a
	// close waits for them before it drains the run.
	execWG sync.WaitGroup
	// telemetryStop shuts the observation pipeline the document declared down;
	// it is nil beside the log face and beside a document that starts none.
	telemetryStop func(context.Context) error

	reloadMu sync.Mutex
	mu       sync.Mutex
	lastAddr event.Address
	// userAddresses remembers the entry address each user session was admitted
	// at, so a terminal notice that lands after the turn ended still finds the
	// explicit address its follow-up turn runs at.
	userAddresses map[event.SessionID]event.Address
	// userTurns counts the user turns in flight per address. A turn is judged
	// against the entry the generation in effect declared when the turn was
	// admitted, so a publication that moves the entry never turns away the answer
	// of a turn already running: its address keeps the user port until the turn
	// returns.
	userTurns map[event.Address]int
	// noticeAccounts holds the settle account of the follow-up turn one notice
	// material drives, under the key that material lands as, from before the
	// commit until the delivery of the segment that consumed it closes the entry.
	// A consumer segment answers the entry that waits on it only after its own
	// delivery, so the account of the entry is settled on the delivery face
	// instead of behind the wait: a stop inside the send then leaves a landed
	// attempt the restart closes with its receipt instead of a notice it drives a
	// second time.
	noticeAccounts map[event.EventID]settleBarrier
	invocations    uint64
	// closed reports that injection into this host has stopped: no external
	// input is admitted past it.
	closed bool
	// halted reports that the host is past its quiesce: the consumers and the
	// settle driver stop and no derived turn starts beside them.
	halted bool
	// closeReport is the state the last close ended in, kept so the caller that
	// only receives an error still reads the per-address report behind it.
	closeReport agent.DrainReport
	closeErr    error
}

var (
	_ func(string, ...HostOption) (*Host, error)                                  = Boot
	_ func(MessageSender) HostOption                                              = WithSender
	_ func(plugin.DeliveryRuler) HostOption                                       = WithDeliveryRuler
	_ func(task.Approver) HostOption                                              = WithApprover
	_ func(state.Governance) HostOption                                           = WithGovernance
	_ func(*Host, context.Context, InvokeRequest) (agent.Turn, error)             = (*Host).Invoke
	_ func(*Host, context.Context) error                                          = (*Host).Close
	_ func(*Host, context.Context, agent.CloseRequest) (agent.DrainReport, error) = (*Host).Quiesce
	_ func(*Host) agent.DrainReport                                               = (*Host).CloseReport
	_ func(*Host) *Run                                                            = (*Host).Run
	_ func(*Host) string                                                          = (*Host).FaceAddr
	_ func(*Host, context.Context) (hostReloadReport, error)                      = (*Host).Reload
)

// hostApprovalsDir is the directory under the storage root the boot-local
// approval book lives in when the host attaches none of its own.
const hostApprovalsDir = "approvals"

// hostGCTraceEnv names the environment variable that asks for the runtime's GC
// trace, and hostGCTraceSetting is the GODEBUG entry it asks for.
const (
	hostGCTraceEnv     = "TAGENT_GCTRACE"
	hostGCTraceSetting = "gctrace=1"
)

// hostSwitchOn reports whether the environment value names a switch left on.
func hostSwitchOn(value string) bool {
	on, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && on
}

// hostGODEBUGSetting reports the value the GODEBUG declaration names for key,
// and an empty string when the declaration carries no such key.
func hostGODEBUGSetting(debug, key string) string {
	for _, entry := range strings.Split(debug, ",") {
		name, value, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

// applyHostGCTrace reads the GC trace switch of this process and reports what a
// boot can do about it. The runtime reads gctrace when the process starts, so a
// GODEBUG written here stays inert for this process: the measurement is one
// program that sets the variable after start and collects without one trace
// line, beside the same program launched with GODEBUG=gctrace=1 and tracing
// every collection. The boot therefore names the boundary in its report and
// leaves the merged value standing for the subprocesses this process starts.
func applyHostGCTrace() {
	if !hostSwitchOn(os.Getenv(hostGCTraceEnv)) {
		return
	}
	debug := strings.TrimSpace(os.Getenv("GODEBUG"))
	if declared := hostGODEBUGSetting(debug, "gctrace"); declared != "" {
		assemblyInfo("tagent: %s names the GC trace and GODEBUG=%q was declared before the process started, so the collections of this process trace with gctrace=%s",
			hostGCTraceEnv, debug, declared)
		return
	}
	merged := hostGCTraceSetting
	if debug != "" {
		merged = debug + "," + hostGCTraceSetting
	}
	if err := os.Setenv("GODEBUG", merged); err != nil {
		assemblyWarn("tagent: %s names the GC trace but GODEBUG could not be set: %v", hostGCTraceEnv, err)
		return
	}
	assemblyWarn("tagent: %s names the GC trace but GODEBUG carried no gctrace, and the runtime reads gctrace only when the process starts: this process traces no collection. Launch it with GODEBUG=%s to trace it; GODEBUG=%q stands for the subprocesses this host starts",
		hostGCTraceEnv, hostGCTraceSetting, merged)
}

// Boot reads one host document and returns the assembled host: the
// declaration is resolved from the document, the models, tool instances,
// storage tier, embedder and reflection plan are built for it, the run is
// assembled through Assemble, and the reflection driver and the loopback
// endpoints start beside it. Every failure before the assembly returns the
// named refusal and leaves nothing running.
//
// - The meditation pass yields to the model requests of the observed session, and the read of them is the consumption loop's own: the driver is assembled before the loop is, so the face is a method value that reads it when a pass runs.
//
// - Every decision of a segment runs under the lineage of the material that opened it, so a body a call of that decision sends leaves under the line of its own turn instead of the send tool's own default; the direct drive stamps the same line on its context.
//
// - The one approval book of a boot is the host-attached book when the caller mounted it, and a boot-local book under <root>/approvals only fills a process that declared none, so a caller that mounted the default book keeps ruling the very book it handed over; the risk-rule gate, the loop's batch fuse and the assembly's attention reading read that same book, so the presence a reader sees and the seat a held call is parked through are one book and never two.
//
// - A fuse declaration on the entry agent wires the loop: a response above the call cap or past the long-context threshold parks its whole batch on that one book before anything executes, and a declaration that names neither knob leaves the limits the process already declared in place.
//
// - TAGENT_GCTRACE=1 asks for the runtime's GC trace, which the runtime reads when the process starts: a boot merges GODEBUG=gctrace=1 so the subprocesses this host starts trace, and names in one line that the host process itself traces only when it was launched beside it.
// - nativeFor is the one construction of an agent's native pieces: Boot builds the first generation through it, and a reload builds an agent its document adds through it over the ports bound in this call.
func Boot(configPath string, opts ...HostOption) (*Host, error) {
	applyHostGCTrace()
	doc, err := loadHostDocument(configPath)
	if err != nil {
		return nil, err
	}
	var cfg assemblyConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	decl, err := doc.Declaration()
	if err != nil {
		return nil, err
	}
	mapped, err := doc.document()
	if err != nil {
		return nil, err
	}
	plan, err := MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil {
		return nil, err
	}
	meditationPlan, err := resolveHostMeditation(mapped, doc.PromptDir, storageRootOf(plan))
	if err != nil {
		return nil, err
	}
	if err := doc.validateMeditationTools(meditationPlan); err != nil {
		return nil, err
	}
	// The one outbound face of the document is wrapped before the tools are
	// assembled, so the counter face and every delivery path read the same book.
	var deliveryMeter *hostDeliveryMeter
	if cfg.hostSender != nil {
		deliveryMeter = &hostDeliveryMeter{sender: cfg.hostSender}
		cfg.hostSender = deliveryMeter
	}
	relations, err := openHostRelations(plan)
	if err != nil {
		return nil, err
	}
	embedder, err := plan.Embedder()
	if err != nil {
		return nil, err
	}
	recallPort := NewRecallPort(embedder, relations)
	ctx, cancel := context.WithCancel(context.Background())
	meditationPort := &hostMeditationPort{plan: meditationPlan}
	reentryPort := &hostSendReentryPort{}
	client := &http.Client{Timeout: doc.requestTimeout()}
	models := newHostModelBuilder(doc, client)
	nativeFor := func(ctx context.Context, doc *hostDocument, id event.AgentID) (NativeAgent, error) {
		return doc.nativeAgent(ctx, string(id), client, recallPort, meditationPlan, meditationPort, reentryPort)
	}
	// host is assigned once the assembly returns; every face assembled before it
	// reads the variable at call time instead of capturing a nil instance, because
	// a call can only ever run beside a published host.
	var host *Host
	natives, err := doc.nativeAgents(ctx, client, recallPort, meditationPlan, meditationPort, reentryPort)
	if err != nil {
		cancel()
		return nil, err
	}
	hostApprovals := cfg.approvals
	if hostApprovals == nil {
		if root := storageRootOf(plan); root != "" {
			dir := filepath.Join(root, hostApprovalsDir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				cancel()
				return nil, fmt.Errorf("tagent: create the approvals directory %s: %w", dir, err)
			}
			book := approval.NewApprovalManager(dir, 0)
			hostApprovals = book
		}
	}
	var capture *rl.TrajectoryCapture
	if root := storageRootOf(plan); root != "" {
		capture, err = openHostCapture(root, specOfLocked(decl, event.AgentID(decl.Entry)).CaptureBudgetMB)
		if err != nil {
			cancel()
			return nil, err
		}
	}
	sealed := false
	var stopTelemetry func(context.Context) error
	defer func() {
		if sealed {
			return
		}
		if capture != nil {
			sealHostCapture(capture)
		}
		if stopTelemetry != nil {
			_ = stopTelemetry(context.Background())
		}
	}()
	var driver *meditation.Driver
	var settleDriver *SettleDriver
	// The settle face captures the host variable, because the three faces it needs
	// are the host's own memory and drive and are only called after Boot published
	// it. The execution ledger is created before the assembly and observes state
	// through the same variable, because a resource can only be acquired once the
	// host drives a call.
	var execManager *tagentexec.Manager
	observer, telemetryStop, err := hostTelemetryObserver(ctx, doc.Telemetry)
	if err != nil {
		cancel()
		return nil, err
	}
	stopTelemetry = telemetryStop
	options := []Option{
		WithGovernance(governanceOf(cfg.governance)),
		WithTelemetry(observer),
	}
	if declaresExecFace(decl) {
		executor, err := tagentexec.NewTmuxExecutor()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("tagent: an agent declares the %q face: %w", tagentexec.PresetName, err)
		}
		execManager = tagentexec.NewManager(executor, tagentexec.WithStateObserver(func(evt tagentexec.StateEvent) {
			if host != nil {
				host.observeExecState(evt)
			}
		}))
		options = append(options, WithExecManager(execManager))
	}
	riskRules, err := doc.riskRules()
	if err != nil {
		cancel()
		return nil, err
	}
	if len(riskRules) > 0 {
		if cfg.policy != nil {
			cancel()
			return nil, fmt.Errorf("tagent: the document declares governance risk rules beside a policy attached through WithPolicy")
		}
		options = append(options, WithPolicy(governance.Open(
			governance.WithRiskRules(riskRules...),
			governance.WithApprovals(hostApprovals),
		)))
	}
	if capture != nil {
		options = append(options, WithCapture(&hostRunCapture{trajectory: capture}))
	}
	for _, spec := range decl.Agents {
		id := event.AgentID(spec.ID)
		lease, err := rl.NewModelLease(natives[id].Model, doc.modelVersion(spec.ID))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("tagent: attach the model lease of agent %q: %w", spec.ID, err)
		}
		options = append(options, WithModelLease(id, lease, WithLeaseRebuild(models.rebuild(id))))
	}
	if plan != nil {
		options = append(options, WithMemoryPlan(plan))
	}
	if meditationPlan != nil {
		options = append(options, WithReflectionAnchors(meditationPlan.anchors))
	}
	options = append(options, WithPlugins(func(run *Run) ([]plugin.Instance, error) {
		instances := make([]plugin.Instance, 0, 2)
		if meditationPlan != nil {
			gate := run.ReflectionGate()
			if gate == nil {
				return nil, fmt.Errorf("tagent: agent %q enables meditation beside no reflection anchor store", meditationPlan.agent)
			}
			reflection, err := meditation.NewReflection(meditation.ReflectionConfig{
				Loop:     meditation.ReflectionLoop{Agent: meditationPlan.agent, Session: meditationPlan.session},
				Face:     meditation.ReflectionFace{ObservedNamespaces: meditationPlan.observed, ReadNamespaces: meditationPlan.reads},
				Cadence:  meditation.ReflectionCadence{Interval: meditationPlan.interval, MinGap: meditationPlan.minGap},
				Prompt:   meditationPlan.prompt,
				Delivery: meditation.ReflectionDelivery{Targets: meditationPlan.targets, Sessions: meditationPort},
			}, gate, run.ObservationFacts(), run.ReflectionInput())
			if err != nil {
				return nil, err
			}
			driver = meditation.NewDriver(reflection, meditationPlan.interval, meditationPlan.minGap, run.ModelInFlight, run.instance)
			driver.SetObserver(func(outcome meditation.TickOutcome) {
				assemblyInfo("tagent: meditation driver of %s: %s (%s)", meditationPlan.agent, outcome.Decision.Disposition, outcome.Decision.Reason)
			})
			instances = append(instances, driver)
		}
		settleDriver = NewSettleDriver(
			run.CommittedEvent,
			func() SettleWake {
				if run.store == nil {
					return nil
				}
				return run.store
			},
			func(ctx context.Context, addr event.Address, fact event.Event) error {
				return host.fireNoticeTurn(ctx, addr, fact)
			},
		)
		settleDriver.setHandoff(func(ctx context.Context, addr event.Address, notice task.Notice) error {
			return host.handoffSettledBodies(ctx, addr, notice)
		})
		if scope, declared := settleScopeOfDocument(decl); declared {
			settleDriver.DeclareScope(scope.declaring, scope.targets...)
		}
		instances = append(instances, settleDriver)
		return instances, nil
	}))
	if cfg.approver != nil {
		options = append(options, WithApprover(cfg.approver))
	}
	if cfg.policy != nil {
		options = append(options, WithPolicy(cfg.policy))
	}
	if cfg.enforcement != "" {
		options = append(options, WithEnforcement(cfg.enforcement))
	}
	if hostApprovals != nil {
		options = append(options, WithApprovals(hostApprovals))
	}
	options = append(options, withBootConfigReconcile(func(ctx context.Context, run *Run, decl *config.Declaration, natives map[event.AgentID]NativeAgent) error {
		return reconcileBootConfig(ctx, run, configPath, doc, decl, natives)
	}))
	run, err := Assemble(ctx, decl, natives, options...)
	if err != nil {
		cancel()
		return nil, err
	}
	if entrySpec := specOfLocked(decl, event.AgentID(decl.Entry)); entrySpec.MaxCallsPerResponse > 0 || entrySpec.LongContextTokens > 0 {
		run.loop.SetFuseLimits(agent.FuseLimits{
			CallCap:           entrySpec.MaxCallsPerResponse,
			LongContextTokens: entrySpec.LongContextTokens,
		})
	}
	if hostApprovals != nil {
		if cfg.approvals != nil {
			run.loop.SetApprovalBook(hostApprovals)
		} else if agent.DefaultApprovalBook() == nil {
			run.loop.SetApprovalBook(hostApprovals)
		}
	}
	recallPort.Bind(run)
	meditationPort.Bind(run)
	reentryPort.Bind(run)
	host = &Host{
		ctx:               ctx,
		cancel:            cancel,
		run:               run,
		entry:             event.AgentID(decl.Entry),
		sender:            cfg.hostSender,
		deliveries:        deliveryMeter,
		deliveryRuler:     cfg.deliveryRuler,
		pendingDeliveries: map[string]*heldDelivery{},
		meditation:        driver,
		settle:            settleDriver,
		execManager:       execManager,
		configPath:        configPath,
		doc:               doc,
		natives:           natives,
		nativeFor:         nativeFor,
		models:            models,
		capture:           capture,
		telemetryStop:     telemetryStop,
		userAddresses:     map[event.SessionID]event.Address{},
		userTurns:         map[event.Address]int{},
	}
	if err := host.restoreUnconsumedEvents(ctx, settleDriver); err != nil {
		cancel()
		return nil, host.abortBoot(err)
	}
	host.consumers = host.consumersOf()
	if settleDriver != nil && !host.run.Unsettled(ctx).Resting() {
		settleDriver.Wake()
	}
	face, err := newHostFace(ctx, host, os.Getenv(hostFaceTokenEnv))
	if err != nil {
		cancel()
		return nil, host.abortBoot(err)
	}
	host.run.CompleteRestartRecovery(ctx)
	face.start()
	host.face = face
	sealed = true
	return host, nil
}

// governanceOf reports the governance one Boot call publishes under: the
// caller's own, or the structural policy the composition root carries by
// default.
func governanceOf(governance state.Governance) state.Governance {
	if governance != nil {
		return governance
	}
	return evolution.NewPolicy(evolution.NewStructuralGate())
}

// hostTelemetryMeter is the instrumentation scope the SDK observer records its
// committed-fact counter under: the scope telemetry.SDKObserver declares, so a
// provider this host starts exports exactly the counter the observer writes.
const hostTelemetryMeter = "tagent/telemetry"

// hostTelemetryObserver opens the observation face one host document declares:
// the log observer beside no OTel switch, and the layered SDK observer beside
// it. A declared endpoint starts the framework trace and metric pipelines the
// layers export through, and the returned stop func shuts them down in the
// reverse order. An endpoint beside the switch off is refused instead of being
// read past, and so is an endpoint carrying a URL where the exporters expect a
// host:port pair.
func hostTelemetryObserver(ctx context.Context, spec config.TelemetrySpec) (plugin.Observer, func(context.Context) error, error) {
	endpoint := strings.TrimSpace(spec.Endpoint)
	if !spec.OTelEnabled {
		if endpoint != "" {
			return nil, nil, fmt.Errorf("tagent: telemetry.endpoint %q is declared beside otel_enabled: false", endpoint)
		}
		return telemetry.NewLogObserver(trpclog.Default), nil, nil
	}
	options := make([]telemetry.Option, 0, 1)
	stops := make([]func(context.Context) error, 0, 2)
	if endpoint != "" {
		if strings.Contains(endpoint, "://") {
			return nil, nil, fmt.Errorf("tagent: telemetry.endpoint %q names a URL, want host:port", endpoint)
		}
		stopTrace, err := ftrace.Start(ctx, ftrace.WithEndpoint(endpoint))
		if err != nil {
			return nil, nil, fmt.Errorf("tagent: start the trace pipeline at %s: %w", endpoint, err)
		}
		stops = append(stops, func(context.Context) error { return stopTrace() })
		provider, err := fmetric.NewMeterProvider(ctx, fmetric.WithEndpoint(endpoint))
		if err != nil {
			for _, stop := range stops {
				_ = stop(context.Background())
			}
			return nil, nil, fmt.Errorf("tagent: start the metric pipeline at %s: %w", endpoint, err)
		}
		stops = append(stops, provider.Shutdown)
		options = append(options, telemetry.WithMeter(provider.Meter(hostTelemetryMeter)))
	}
	observer, err := telemetry.NewSDKObserver(nil, options...)
	if err != nil {
		for _, stop := range stops {
			_ = stop(context.Background())
		}
		return nil, nil, err
	}
	stop := func(ctx context.Context) error {
		failures := make([]error, 0, len(stops))
		for i := len(stops) - 1; i >= 0; i-- {
			failures = append(failures, stops[i](ctx))
		}
		return errors.Join(failures...)
	}
	return observer, stop, nil
}

// declaresExecFace reports whether any agent section of the declaration grants
// the execution face.
func declaresExecFace(decl *config.Declaration) bool {
	for _, spec := range decl.Agents {
		for _, ref := range spec.Tools {
			if ref.Name == tagentexec.PresetName {
				return true
			}
		}
	}
	return false
}

// storageRootOf reports the root of the storage tier the plan opens, and an
// empty root beside no tier.
func storageRootOf(plan *MemoryPlan) string {
	if plan == nil || plan.Storage() == nil {
		return ""
	}
	return plan.Storage().Root
}

// openHostRelations reports the relation ledger the recall trace walks: the
// journal beside the storage tier the plan opens, and an empty ledger beside no
// tier.
func openHostRelations(plan *MemoryPlan) (*replay.MemoryRelations, error) {
	cfg := plan.Storage()
	if cfg == nil {
		return replay.NewMemoryRelations(), nil
	}
	root := filepath.Join(cfg.Root, "relations")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("tagent: create the relation journal %s: %w", root, err)
	}
	relations, err := replay.OpenMemoryRelations(root)
	if err != nil {
		return nil, fmt.Errorf("tagent: open the relation journal %s: %w", root, err)
	}
	return relations, nil
}

// abortBoot drains the run of a Boot call that failed after the assembly and
// reports the failure it carries. The reflection driver is stopped first: it
// starts before the endpoints, so a failure in that window must not leave a
// reflection pass landing on a run that is already draining.
func (h *Host) abortBoot(cause error) error {
	if h.settle != nil {
		h.settle.Stop()
	}
	if h.meditation != nil {
		h.meditation.Stop()
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), hostShutdownGrace)
	defer cancel()
	if h.run != nil {
		_, _ = h.run.Close(closeCtx, agent.CloseRequest{Timeout: time.Now().Add(hostShutdownGrace)})
		_ = h.run.CloseMCPServers()
	}
	_ = h.stopTelemetry(closeCtx)
	h.cancel()
	return cause
}

// stopTelemetry shuts the observation pipeline the document started down, once:
// the trace and metric exporters are flushed under the close grace, and a
// second close finds nothing to stop.
func (h *Host) stopTelemetry(ctx context.Context) error {
	if h == nil || h.telemetryStop == nil {
		return nil
	}
	stop := h.telemetryStop
	h.telemetryStop = nil
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostShutdownGrace)
	defer cancel()
	return stop(stopCtx)
}

// consumersOf reports the resident instances this host drives its addresses
// through, mounting the set over the assembled run on first use. Boot mounts it
// beside the run it just assembled, and a host assembled straight over a run
// mounts it at its first drive, so one host drives one path either way.
//
// - The set lives as long as the context the host was booted under, and a host booted without one mounts a set that lives as long as its run: no instance outlives the run it drives.
// - A host beside no assembled run answers none, and the call that needed one reports the closed host it is.
func (h *Host) consumersOf() *agent.Consumers {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.consumers != nil {
		return h.consumers
	}
	if h.run == nil || h.run.loop == nil {
		return nil
	}
	base := h.ctx
	if base == nil {
		base = context.Background()
	}
	candidate := agent.NewConsumers(h.run.loop, base, h.deliverClosedSegment)
	consumers := h.run.loop.SetConsumers(candidate)
	if consumers == nil {
		return nil
	}
	consumers.SetProgress(h.run.noteProgress)
	consumers.SetTurnContext(func(decisionCtx context.Context, trigger string) context.Context {
		return withDeliveryLineage(decisionCtx, deliveryLineageOf(trigger))
	})
	h.run.delegations.SetResident(h.run.loop)
	h.consumers = consumers
	return consumers
}

// Invoke drives one invocation through the resident instance of its address:
// the input commits as the facts of its session, the instance of that address
// pulls the message with whatever else arrived, batches them into one model
// request, and the call answers with the aggregated turn of the segment that
// consumed it — a second entry whose message the same segment consumed answers
// the same turn. The segment identity the answer carries is the one the
// instance minted, never the arrival identity the caller named: the address is
// driven by its instance and by nothing beside it.
//
// An invocation at the user address of the entry agent delivers the fresh
// assistant output through the message sender from the delivery face of that
// instance, once per closed segment, so an entry that only waits on the segment
// never delivers a body a second time. Every other declared agent's address is
// driven headless: its answer stays on its own chain.
// The entry agent and the declared agent set are read from the generation in
// effect: a reload that moves the entry speaks for the next turn, while a turn
// already running keeps the entry it was admitted under, and an address the
// generation removed answers new input by name.
func (h *Host) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error) {
	if h == nil || h.run == nil {
		return agent.Turn{}, ErrHostClosed
	}
	if !req.Address.IsComplete() {
		return agent.Turn{}, fmt.Errorf("%w: agent=%q session=%q",
			ErrHostAddressMissing, req.Address.AgentID, req.Address.SessionID)
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return agent.Turn{}, ErrHostClosed
	}
	sender := h.sender
	h.mu.Unlock()
	entry, declared := h.run.publishedOrganization(req.Address.AgentID)
	if !declared {
		return agent.Turn{}, fmt.Errorf("%w: agent %q", ErrHostAgentUndeclared, req.Address.AgentID)
	}
	user := req.Address.AgentID == entry
	if user && sender == nil {
		return agent.Turn{}, fmt.Errorf("%w: the turn runs at the user address of %q", ErrHostSenderMissing, entry)
	}
	if req.InvocationID == "" {
		req.InvocationID = h.nextInvocation(req.Address)
	}
	if h.consumersOf() == nil {
		return agent.Turn{}, ErrHostClosed
	}
	if user {
		h.beginUserTurn(req.Address)
		defer h.endUserTurn(req.Address)
	}
	return h.invokeConsumed(ctx, req, user)
}

// invokeConsumed lands the input of one request as the facts of its session,
// wakes the resident instance of that address, and answers with the aggregated
// turn of the segment that consumed them: the model request the instance served
// read the message beside whatever else arrived. The fresh bodies of the closing
// answer leave through the sender from the instance's own delivery face, once
// per closed segment, so an entry that only waits on the segment never delivers
// a body a second time.
//
// - An entry whose context ended while its segment still flies answers the abandoned turn it asked for: the caller reads a cancelled turn beside no error, exactly as the direct drive answers one, and the segment stays with the loop that still flies it.
// - An entry whose loop ended beside its input answers the same cancelled turn by name: the loop that would have consumed the input stopped, so the input stays owed on the chain and the entry reads the cancellation instead of a segment that will never close.
// - Only a user turn is remembered here: the follow-up turn a terminal notice fires lands at the address a user speaks through, so a turn of another declared agent never renames the user session of the host.
func (h *Host) invokeConsumed(ctx context.Context, req InvokeRequest, user bool) (agent.Turn, error) {
	now := time.Now()
	keys := make([]event.EventID, 0, len(req.Input))
	for index, msg := range req.Input {
		evt := h.run.inputEventOf(req, msg, index, req.InvocationID, now)
		id, err := h.run.commitOnly(ctx, req.Address, evt)
		if err != nil {
			return agent.Turn{}, fmt.Errorf("tagent: the input of %s did not commit: %w", req.InvocationID, err)
		}
		if id != "" {
			keys = append(keys, id)
		}
	}
	if user {
		h.rememberUserAddress(req.Address)
	}
	consumer := h.consumers.Ensure(req.Address)
	if consumer == nil {
		return agent.Turn{}, ErrHostClosed
	}
	consumer.Wake()
	answer, err := consumer.WaitAnswer(ctx, keys)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, agent.ErrSegmentCancelled) {
			return agent.Turn{InvocationID: req.InvocationID, Terminal: model.KindCancelled}, nil
		}
		return answer.Turn, err
	}
	return answer.Turn, nil
}

// deliverClosedSegment hands the fresh answer of one closed consumption segment
// to the outbound face of the address the host speaks for: the assistant bodies
// the invocation of that segment generated leave under the lineage the material
// that opened it named, and the answer of a segment no entry waits on — the one
// a settle notice drives into being — leaves by the very same path. A host that
// stopped beside the closing segment delivers nothing, and the obligation stays
// owed to the restart.
//
// The settle account of an entry whose notice material this segment consumed is
// closed here, before the answer leaves: the entry that waits on the segment
// reads its answer only after this face ran, so closing the account behind the
// wait would leave the terminal of a landed turn unrecorded for as long as the
// send runs. A stop inside the send is then answered by a restart that closes
// the landed attempt instead of driving the notice a second time.
//
// - Only a segment of the user port delivers: the outbound face belongs to the user session of the entry the generation in effect declares, so the answer another declared agent's segment lands stays on that agent's own chain.
// - A user turn already running keeps the entry it was admitted under: a publication that moves the entry while the turn flies never turns away the answer the user port still owes it.
func (h *Host) deliverClosedSegment(ctx context.Context, addr event.Address, answer agent.SegmentAnswer) {
	h.mu.Lock()
	halted := h.halted
	sender := h.sender
	h.mu.Unlock()
	h.settleConsumedNotices(ctx, answer)
	if halted {
		return
	}
	if !h.deliversAt(addr) {
		return
	}
	h.deliverFresh(ctx, addr, sender, deliveryLineageOf(answer.Trigger), answer.Turn)
}

// deliversAt reports whether one closed segment delivers through the host's
// outbound face: its address speaks for the entry the generation in effect
// declares, or it is serving a user turn admitted under an earlier entry that is
// still in flight.
func (h *Host) deliversAt(addr event.Address) bool {
	if addr.AgentID == h.entryAgent() {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.userTurns[addr] > 0
}

// beginUserTurn registers one user turn in flight at its address, so the
// delivery of the turn is judged against the entry it was admitted under.
func (h *Host) beginUserTurn(addr event.Address) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.userTurns == nil {
		h.userTurns = map[event.Address]int{}
	}
	h.userTurns[addr]++
}

// endUserTurn closes one user turn in flight at its address.
func (h *Host) endUserTurn(addr event.Address) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.userTurns[addr] <= 1 {
		delete(h.userTurns, addr)
		return
	}
	h.userTurns[addr]--
}

// settleConsumedNotices closes the settle account of every entry whose notice
// material the closing segment consumed: the terminal the segment landed is
// recorded as the attempt of its notification, the delivery obligation of every
// body the turning segment generated is registered, the failure of a failed
// answer is landed as an observation the target reads, and the consumption
// receipt of the entry is written. The registration is taken, not read, so one
// delivery closes one entry exactly once.
func (h *Host) settleConsumedNotices(ctx context.Context, answer agent.SegmentAnswer) {
	for _, barrier := range h.takeNoticeAccounts(answer.Consumed) {
		if _, err := h.settleTerminal(withSettleBarrier(ctx, barrier), answer.Turn); err != nil {
			assemblyWarn("tagent: the settle account of %s was not closed beside the segment that consumed it: %v",
				barrier.notice.FactKey(), err)
		}
	}
}

// holdNoticeAccount registers the settle account of one notice material under
// the key that material lands as, so the delivery of the segment that consumes
// it closes the entry before the answer leaves. The returned func drops a
// registration nothing consumed, so a segment that closed without reading the
// material never settles an entry it did not answer.
func (h *Host) holdNoticeAccount(key event.EventID, barrier settleBarrier) func() {
	if key == "" {
		return func() {}
	}
	h.mu.Lock()
	if h.noticeAccounts == nil {
		h.noticeAccounts = map[event.EventID]settleBarrier{}
	}
	h.noticeAccounts[key] = barrier
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.noticeAccounts, key)
		h.mu.Unlock()
	}
}

// takeNoticeAccounts reports the settle accounts registered under any of the
// keys one closed segment consumed and drops them: the segment that consumed an
// entry's material is the one turn that entry is closed beside.
func (h *Host) takeNoticeAccounts(consumed []event.EventID) []settleBarrier {
	if len(consumed) == 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var held []settleBarrier
	for _, key := range consumed {
		barrier, ok := h.noticeAccounts[key]
		if !ok {
			continue
		}
		delete(h.noticeAccounts, key)
		held = append(held, barrier)
	}
	return held
}

// settleTerminal closes the settle account of one follow-up drive beside the
// terminal fact it just landed: the terminal the turn ended on is recorded as
// the attempt of the notification that fired it, and the consumption receipt of
// its entry is written beside that same result. Every terminal the reduction can
// persist settles the notification — completed, failed and cancelled each end
// the turn — so a failed follow-up closes its entry and drives no second turn,
// which is the loop the receipt forbids. A completed turn registers the delivery
// obligation of every body it generated before its receipt: the obligation
// stands before the receipt because a stop between the two is answered by the
// owed body a restart delivers, never by a missing replay. A failed turn also
// lands its failure as the model-visible observation the chain keeps, so the
// cause reaches the request that reads the chain instead of dying with this
// process. A turn the close itself cancelled records nothing: the close already
// refused new input and stopped the consumers, so that cancelled terminal is the
// cancellation of the stop, not an outcome the target reached. Leaving the
// in-flight marker as it stands keeps the entry owed, and the next boot resumes
// the turn instead of closing it on a result nobody delivered. A body the
// account could not hold refuses the receipt, so no receipt ever closes an entry
// whose body no account carries: the landed attempt is read again by the next
// wake, which registers the body and closes the entry. The answer reports
// whether the account was settled: a drive that carries no barrier is a user
// turn, and a turn the close itself cancelled, both settle nothing here.
//
// The two faces that can close one account — the delivery of the segment that
// consumed the notice and the fire turn the notice drove — share one settlement
// marker on the barrier. The face that settles second reads the account as
// already closed and skips it, so the obligation of a body the first face
// registered is never read again beside a stopping drive, one entry takes one
// receipt, and the answer of a settled entry never fails the drive that waited
// on it. A settlement that stops before its receipt hands the marker back, so
// the other face or the next wake still closes the entry.
//
// - A terminal outside the reduction's closed set names no result the account can close on: the attempt is recorded and the entry stays owed for the wake that reads a real terminal.
func (h *Host) settleTerminal(ctx context.Context, turn agent.Turn) (bool, error) {
	barrier, held := settleBarrierFrom(ctx)
	if !held {
		return false, nil
	}
	h.mu.Lock()
	closing := h.closed
	h.mu.Unlock()
	if closing && turn.Terminal == model.KindCancelled {
		assemblyDebug("tagent: the close cancelled the settle turn of %s; the notice stays owed", barrier.notice.FactKey())
		return false, nil
	}
	if !barrier.claimSettlement() {
		assemblyDebug("tagent: the settle account of %s was closed by the face that settled it first", barrier.notice.FactKey())
		return true, nil
	}
	closed := false
	defer func() {
		if !closed {
			barrier.releaseSettlement()
		}
	}()
	if err := barrier.settle(ctx, turn.Terminal); err != nil {
		assemblyWarn("tagent: the settle attempt of %s was not recorded: %v", barrier.notice.FactKey(), err)
	}
	switch turn.Terminal {
	case model.KindCompleted:
	case model.KindFailed:
		h.noteFailureObservation(ctx, barrier, turn)
	case model.KindCancelled:
	default:
		return false, nil
	}
	if err := barrier.consume(ctx); err != nil && !errors.Is(err, state.ErrClaimSettled) {
		assemblyWarn("tagent: the settle consumption of %s beside its terminal fact reported: %v", barrier.notice.FactKey(), err)
	}
	closed = true
	return true, nil
}

// failureObservationSummary is the audit prefix of the one chain fact a failed
// settle answer lands: the fact the close report names the answer by and the
// fact the next request of the address reads the cause from.
const failureObservationSummary = "failure-observation"

// noteFailureObservation lands the failure of one settle answer as a fact of the
// address the notification drove, and names it to that address's consumption
// loop: the fact stands past the failed answer, so no position of the chain
// opens a segment on it by itself — the name is what the S1a wake law reads as
// "this fact still drives", and the request the segment makes carries the cause
// as its material. The queue is named, never invented a second time: the key is
// derived from the failed invocation, so a repeated landing of one answer finds
// the fact already standing and drives it nowhere new. A host that has stopped
// its loops still lands the fact and leaves the drive to none, so the failure
// stays readable on the chain even when no request can read it.
func (h *Host) noteFailureObservation(ctx context.Context, barrier settleBarrier, turn agent.Turn) {
	if h == nil || h.run == nil {
		return
	}
	cause := strings.TrimSpace(turn.Failure)
	if turn.InvocationID == "" {
		return
	}
	key := failureObservationKey(turn.InvocationID)
	if !h.run.settleFactLanded(key) {
		material := fmt.Sprintf("tagent: the follow-up turn of task %s ended failed", barrier.notice.TaskID)
		if recorded := failureNoticeText(cause); recorded != "" {
			material += "\nfailure:\n" + recorded
		}
		if _, err := h.run.CommitToSession(ctx, barrier.addr, event.Event{
			Key:       key,
			Type:      event.TypeFeedback,
			Timestamp: time.Now(),
			Meta: map[string]string{
				event.MetaKeyTriggerSource:          "failure",
				event.MetaKeyEventSummary:           failureObservationSummary,
				event.MetaPrefix + "terminal":       model.KindFailed.String(),
				event.MetaPrefix + "failure_reason": cause,
				event.MetaPrefix + "invocation":     string(turn.InvocationID),
				event.MetaPrefix + "task_id":        string(barrier.notice.TaskID),
			},
			Message:      model.Message{Role: model.RoleUser, Content: material},
			InvocationID: turn.InvocationID,
		}); err != nil {
			assemblyWarn("tagent: the failure observation of %s left no fact: %v", turn.InvocationID, err)
			return
		}
	}
	h.mu.Lock()
	halted := h.halted
	h.mu.Unlock()
	if halted {
		return
	}
	consumers := h.consumersOf()
	if consumers == nil {
		return
	}
	consumer := consumers.Ensure(barrier.addr)
	if consumer == nil {
		return
	}
	consumer.Drive(key)
	consumer.Wake()
}

// failureObservationKey reports the stable identity the failure observation of
// one failed answer lands under: the failed invocation, so the same answer
// observed twice keeps one fact and a second drive finds the same name.
func failureObservationKey(invocation event.InvocationID) event.EventID {
	return event.EventID("failure-observation:" + string(invocation))
}

// fireNoticeTurn drives one unattended follow-up turn at the address a terminal
// settle belongs to. The turn carries no input: the notice fact this method
// commits under the follow-up's own identity is the material the turn reads at
// its head, because a range reads the notifications of its own identity prefix.
// The turn is stamped "task", so the driver that fired this turn never fires on
// its output.
//
// The material commits as the fact it is and the wake hands it to the resident
// instance of the address, which is the only driver of the decisions of that
// address: no follow-up turn runs beside a segment that instance may already
// fly, whether an entry waits on that segment or a notification alone drove it
// into being. The answer leaves through the delivery face of the instance, the
// path a user turn's answer leaves by, and only a turn that really delivered its
// answer closes the mailbox entry it drove from: a stopped or failed turn
// reports the incomplete delivery by name, so the entry stays owed for the next
// wake instead of a receipt recording a turn nobody read.
//
// - The material lands under the key the notification's own four-part identity derives, so a second attempt commits the same fact and is answered as the fact the chain already holds: the transcript carries the instruction once, however many processes try the notification.
// - The key is named to the instance before the wake, because the attempt that died left the material behind the answer it closed on: the pull of a restarted process reads that position as material a closed segment already carried, and the account of the entry is what says this fact still drives.
// - An entry whose task the flying segment already paired is answered here without a second turn: that segment settles the entry through its own delivery, and the consumed state this reads is the one the segment writes inside that delivery, so a segment that paired the result and has not delivered yet leaves the task awaited and this call waits for the delivery instead of closing the entry on a pairing alone.
// - A wait the settle window ends answers the state as it stands, so a segment still flying past the window leaves the notice turn driving beside it and the warning names the task.
// - The segment that consumes the material answers this call only after its own delivery, so the settle account of the entry is registered here, under the key the material lands as and before the commit: the delivery face of the instance closes it beside the segment that consumed that key, and a segment that closed without reading the material leaves the account owed for the wake.
// - The account still stands owed when the close itself cancelled the turn: that cancellation is the stop, not an outcome the target reached, and the next boot resumes it.
func (h *Host) fireNoticeTurn(ctx context.Context, addr event.Address, fact event.Event) error {
	if h == nil || h.run == nil {
		return ErrHostClosed
	}
	if !addr.IsComplete() {
		return fmt.Errorf("%w: agent=%q session=%q", ErrHostAddressMissing, addr.AgentID, addr.SessionID)
	}
	h.mu.Lock()
	halted := h.halted
	h.mu.Unlock()
	if halted {
		return ErrHostClosed
	}
	consumers := h.consumersOf()
	if consumers == nil {
		return ErrHostClosed
	}
	if barrier, held := settleBarrierFrom(ctx); held {
		_, _, key := noticeMaterialOf(ctx, fact)
		defer h.holdNoticeAccount(key, barrier)()
	}
	if consumer := consumers.Ensure(addr); consumer != nil {
		settleWait, settleCancel := context.WithTimeout(ctx, hostSettleWindow)
		taken := consumer.AwaitTaskState(settleWait, noticeTaskIDOf(fact))
		settleCancel()
		if taken == agent.TaskConsumed {
			return nil
		}
		if taken == agent.TaskAwaited {
			assemblyWarn("tagent: the segment of %s still awaits the settled task %s; the notice turn drives beside it",
				addr.SessionID, noticeTaskIDOf(fact))
		}
	}
	key, err := h.commitNoticeMaterial(ctx, addr, fact)
	if err != nil {
		return err
	}
	consumer := consumers.Ensure(addr)
	if consumer == nil {
		return ErrHostClosed
	}
	consumer.Drive(key)
	consumer.Wake()
	answer, err := consumer.WaitAnswer(ctx, []event.EventID{key})
	if err != nil {
		return err
	}
	settled, err := h.settleTerminal(ctx, answer.Turn)
	if err != nil {
		assemblyWarn("tagent: the notice invocation %s of %s left its settle account open: %v",
			answer.Turn.InvocationID, addr.SessionID, err)
		return err
	}
	if !settled {
		return fmt.Errorf("%w: %s ended as %s", ErrNoticeTurnIncomplete, answer.Turn.InvocationID, answer.Turn.Terminal)
	}
	return nil
}

// noticeMaterialOf reports the three identities one notification material lands
// under: the notification the fact names, the four-part invocation the follow-up
// turn drives under — the entry's own identity when the drive carries the
// barrier of its settle account — and the key the material is stored as. The key
// is derived from those identities alone, so the account of the entry can be
// registered before the commit writes the material.
func noticeMaterialOf(ctx context.Context, fact event.Event) (task.Notice, event.InvocationID, event.EventID) {
	notice := task.Notice{TaskID: noticeTaskIDOf(fact), Kind: noticeKindOf(fact), Event: fact.Key}
	identified := notice
	if barrier, held := settleBarrierFrom(ctx); held {
		identified = barrier.notice
	}
	invocationID := noticeInvocation(identified)
	return notice, invocationID, agent.NoticeFactKey(invocationID, notice)
}

// commitNoticeMaterial lands one notification fact as the input material of its
// address: the notice reads as one user message under its own invocation
// identity, and the identity is the notification's own four-part one, so the
// material is the fact the next consumer of that address pulls — never a
// message a second producer mints beside it. The answer is the identity of the
// committed fact, which is the key the pull of the address reads it under and
// the key an entry waits on.
func (h *Host) commitNoticeMaterial(ctx context.Context, addr event.Address, fact event.Event) (event.EventID, error) {
	notice, invocationID, key := noticeMaterialOf(ctx, fact)
	material := strings.TrimSpace(fact.Message.Content)
	if material == "" {
		material = settleNoticeText(notice)
	}
	if _, err := h.run.CommitToSession(ctx, addr, event.Event{
		Key:       key,
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource: "task",
			event.MetaKeyEventSummary:  string(notice.Kind),
			event.MetaKeyNoticeKind:    string(notice.Kind),
			event.MetaKeyTaskID:        string(notice.TaskID),
		},
		Message:      model.Message{Role: model.RoleUser, Content: material},
		InvocationID: invocationID,
		TaskID:       notice.TaskID,
	}); err != nil {
		return "", err
	}
	return key, nil
}

// noticeInvocation reports the invocation identity one follow-up turn of a
// notification drives under: the notification's own four-part identity — task,
// round, kind and sequence — with no counter and no instance in it. The identity
// is what makes the notice fact idempotent across processes: two attempts at one
// notification commit the same key, the second is answered as the fact already
// stored, and the transcript appends the instruction once.
func noticeInvocation(notice task.Notice) event.InvocationID {
	return event.InvocationID("notice:" + notice.FactKey())
}

// settleNoticeText renders the input material one unattended settle reads as:
// the task and the terminal it settled on beside the result body the
// notification carries, as a user message of the follow-up range.
func settleNoticeText(notice task.Notice) string {
	text := fmt.Sprintf("tagent: task %s %s", notice.TaskID, notice.Kind)
	if body := strings.TrimSpace(notice.Text); body != "" {
		text += "\nresult:\n" + body
	}
	return text
}

// observeExecState records one lifecycle event of an execution resource as an
// audit fact and nothing else: stable, suspect and exited observations are
// evidence a reader can query, never material a model reads and never a reason
// to drive a turn. The acquiring call answered already, so an observation beside
// it must not wake the model, and a release or retention stays on the explicit
// answer of the call that released or retained the resource.
//
// The fact follows noteFailureNotice: a feedback fact owned by the address, no
// message payload and no notice keys, so consumers that drive a turn from a kind
// and a task id never see it; the observation stays readable in the resource,
// phase and exit-code metadata, a refused commit is a warning beside it, and the
// observer bypasses the manager (outside its lock, silent after close, never
// changing whether the resource stays held).
func (h *Host) observeExecState(evt tagentexec.StateEvent) {
	if h == nil || h.run == nil {
		return
	}
	if !evt.Owner.Address.IsComplete() || evt.Owner.TaskID == "" {
		return
	}
	switch evt.Phase {
	case tagentexec.StateStable, tagentexec.StateSuspect, tagentexec.StateExited:
	default:
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.execWG.Add(1)
	h.mu.Unlock()
	go func() {
		defer h.execWG.Done()
		ctx, cancel := context.WithTimeout(h.ctx, hostShutdownGrace)
		defer cancel()
		meta := map[string]string{
			event.MetaKeyTriggerSource:    "host",
			event.MetaKeyEventSummary:     "exec-state: " + string(evt.Phase),
			event.MetaPrefix + "resource": evt.Resource.String(),
			event.MetaPrefix + "phase":    string(evt.Phase),
		}
		if evt.Confirmed {
			meta[event.MetaPrefix+"exit_code"] = strconv.Itoa(evt.ExitCode)
		}
		if _, err := h.run.CommitToSession(ctx, evt.Owner.Address, event.Event{
			Type:      event.TypeFeedback,
			Timestamp: time.Now(),
			Meta:      meta,
		}); err != nil && !errors.Is(err, ErrHostClosed) {
			assemblyWarn("tagent: the execution state of %s left no audit fact: %v", evt.Resource, err)
		}
		assemblyDebug("tagent: execution resource %s observed %s (audit only, no turn)", evt.Resource, evt.Phase)
	}()
}

// Quiesce stops injection into this host and runs it to its fixed point: no
// external input is admitted, the work the run already accepted converges, the
// derived turns those accepted obligations owe keep running, and no consumption
// stays unconfirmed. The consumers and the settle driver stay mounted for the
// whole quiesce, because the derived work is driven through them.
//
// A host that reached rest answers Quiesced; one that spent its timeout with
// work still on the chain answers Cut, which is a normal return because the
// remaining work is on the chain and a restart resumes it; and one whose
// criterion could not be read answers Broken.
// The redline list (4 rules):
// MUST refuse external input before the first read of the rest criterion.
// MUST admit the derived turns an accepted obligation owes for the whole quiesce.
// MUST NOT report a run at rest while a consumer still flies or a writer has not stopped.
// MUST answer a cancelled caller as Cut where the run stands: the criterion is read beside a context that does not carry the caller's cancellation.
//
// - The close's own drain path is the one offer a delivery a live path refused receives: its task account still owes the notification, and no retained offer stands beside it. The delivered notice stays unconsumed on the mailbox account until its follow-up turn lands, so the criterion re-read above cannot report the run at rest beside work this released.
func (h *Host) Quiesce(ctx context.Context, req agent.CloseRequest) (agent.DrainReport, error) {
	if h == nil || h.run == nil {
		return agent.DrainReport{}, ErrHostClosed
	}
	timeout, err := req.Resolve()
	if err != nil {
		return agent.DrainReport{Outcome: agent.CloseBroken}, err
	}
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	timer := time.NewTimer(time.Until(timeout))
	defer timer.Stop()
	woken := ""
	judge := context.WithoutCancel(ctx)
	for {
		read := h.run.Unsettled(judge)
		stopped, writers := h.run.writers.Stopped()
		landing := h.run.landingOwed()
		if read.Resting() && h.consumersOf().Resting() && stopped && !landing {
			return agent.DrainReport{Outcome: agent.CloseQuiesced, Drained: true, Failed: h.run.failedAnswers()}, nil
		}
		if ctx.Err() != nil || !time.Now().Before(timeout) {
			report := agent.DrainReport{
				Outcome:   agent.CloseCut,
				Unsettled: mergeUnsettled(h.run.unsettledObligations(read), agent.WriterObligations(writers), landingObligations(landing)),
				Failed:    h.run.failedAnswers(),
			}
			if read.Unjudged != "" {
				report.Outcome = agent.CloseBroken
				assemblyWarn("tagent: the quiesce could not judge the run: %s", read.Unjudged)
				return report, brokenCloseReason(read)
			}
			return report, nil
		}
		h.run.offerOwed(judge)
		if digest := settleDigest(read); digest != woken {
			woken = digest
			if h.settle != nil {
				h.settle.Wake()
			}
			h.run.wakeOwedDrivers()
		}
		select {
		case <-ctx.Done():
		case <-timer.C:
		case <-h.run.Progress():
		}
	}
}

// Rejections reports the feedback fact of every notification a target refused,
// as the state account of this host holds it: the notification each refusal
// answers beside the routing it was offered to and the reason the target gave. A
// refusal is a consumption result, so it is the diagnostic face of what the
// account already answered rather than a second criterion of the close.
func (h *Host) Rejections() []state.Rejection {
	if h == nil || h.run == nil {
		return nil
	}
	return h.run.Rejections()
}

// CloseReport reports the state the last close of this host ended in, beside the
// work it could not settle.
func (h *Host) CloseReport() agent.DrainReport {
	if h == nil {
		return agent.DrainReport{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closeReport
}

// Close stops injection into this host, quiesces the run it drives and only then
// stops the consumers, the settle driver and the facilities behind them. One
// deadline bounds the quiesce and the run close behind it, so a cancelled caller
// never abandons the run close half-way. Every stop here is a join: a consumer
// set, a driver and a face answer only once the writers they cancelled returned,
// so the writer barrier this close reads is a barrier over writers that are truly
// stopped — a segment a cancelled decision still unwinds is landed by that loop
// before the close reads its stop, never after the close returned. A repeated
// close reports the first result and runs no second quiesce; a close that
// follows a bare quiesce still stops the facilities that quiesce left running.
// - Every facility behind the run is torn down however the close ended: a close that could not judge the run still stops its servers, seals the trajectory, stops telemetry and releases the execution face, and a broken judgment is what a repeated close reports.
// - The producers stop before the host declares itself halted: only past their join does a closed host refuse a delivery, so what a stopping producer hands in is settled instead of dropped.
// - The report of the run close is folded in whichever way that close ended, so the two faces of one close never disagree.
// - The redrive stops before the drain starts: a send that starts after the quiesce would land on a chain the close is already judging, and an attempt already in flight finishes the send it began and records its own state.
func (h *Host) Close(ctx context.Context) (first error) {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	if h.halted {
		err := h.closeErr
		h.mu.Unlock()
		return err
	}
	h.mu.Unlock()
	var fatal error
	defer func() {
		if err := h.run.CloseMCPServers(); err != nil && first == nil {
			first = err
		}
		if err := h.sealCapture(ctx); err != nil && first == nil {
			first = err
		}
		if err := h.stopTelemetry(ctx); err != nil && first == nil {
			first = err
		}
		h.finishClose(first, fatal)
	}()
	deadline := time.Now().Add(hostShutdownGrace)
	report, quiesceErr := h.Quiesce(ctx, agent.CloseRequest{Timeout: deadline})
	if quiesceErr != nil {
		first = quiesceErr
	}
	if h.consumers != nil {
		h.consumers.Stop()
	}
	h.mu.Lock()
	retries := h.retries
	h.mu.Unlock()
	if retries != nil {
		retries.stopAll()
	}
	if h.settle != nil {
		h.settle.Stop()
	}
	h.mu.Lock()
	h.halted = true
	h.closeReport = report
	h.mu.Unlock()
	if h.meditation != nil {
		h.meditation.Stop()
	}
	if h.face != nil {
		if err := h.face.stop(ctx); err != nil && first == nil {
			first = err
		}
	}
	if h.execManager != nil {
		for _, execReport := range h.execManager.Close(ctx) {
			if execReport.Evidence.Released {
				continue
			}
			assemblyWarn("tagent: the execution resource %s stays held after close: %s", execReport.Key, execReport.Evidence.Reason)
		}
	}
	h.execWG.Wait()
	closeCtx, closeCancel := context.WithTimeout(context.WithoutCancel(ctx), hostShutdownGrace)
	defer closeCancel()
	runReport, err := h.run.Close(closeCtx, agent.CloseRequest{Timeout: deadline})
	h.setCloseReport(report, runReport)
	if err != nil {
		assemblyWarn("tagent: the close of the run could not judge it: %v", err)
		fatal = err
		if first == nil {
			first = err
		}
		return first
	}
	if report.Outcome == agent.CloseCut {
		assemblyWarn("tagent: the host close was cut with %d unsettled item(s) on the chain", len(report.Unsettled))
	}
	return first
}

// setCloseReport folds the report of the run's own close into the one the host
// quiesce reported: the two name the work of one close, so the state is the
// worse of the two, the per-address list is their union and the failed answers
// are the one set both read off the chain.
func (h *Host) setCloseReport(quiesced, run agent.DrainReport) {
	merged := quiesced
	if outcomeRank(run.Outcome) > outcomeRank(merged.Outcome) {
		merged.Outcome = run.Outcome
	}
	merged.Drained = merged.Drained && run.Drained
	merged.Unsettled = mergeUnsettled(merged.Unsettled, run.Unsettled)
	merged.Failed = mergeFailedAnswers(merged.Failed, run.Failed)
	h.mu.Lock()
	h.closeReport = merged
	h.mu.Unlock()
}

// mergeFailedAnswers unions two readings of the failed answers one close names,
// keeping the first reading's order, so folding the host and run reports never
// names one answer twice.
func mergeFailedAnswers(first, second []event.InvocationID) []event.InvocationID {
	if len(second) == 0 {
		return first
	}
	seen := make(map[event.InvocationID]bool, len(first)+len(second))
	merged := make([]event.InvocationID, 0, len(first)+len(second))
	for _, invocation := range append(append([]event.InvocationID(nil), first...), second...) {
		if invocation == "" || seen[invocation] {
			continue
		}
		seen[invocation] = true
		merged = append(merged, invocation)
	}
	return merged
}

// outcomeRank orders the close states by how far they are from a run at rest, so
// a fold of two reports answers the worse one.
func outcomeRank(outcome agent.CloseOutcome) int {
	switch outcome {
	case agent.CloseQuiesced:
		return 0
	case agent.CloseCut:
		return 1
	case agent.CloseBroken:
		return 2
	default:
		return 3
	}
}

// sealCapture seals the run-wide trajectory once the run drained: the sink's
// seal barries every record admitted before it, so the attempt file and its
// manifest account every turn the run served. The seal runs under the host
// close grace even beside a cancelled caller, because it is the one write that
// must not be abandoned half-way.
func (h *Host) sealCapture(ctx context.Context) error {
	if h == nil || h.capture == nil {
		return nil
	}
	sealCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostShutdownGrace)
	defer cancel()
	_, err := h.capture.Seal(sealCtx)
	return err
}

// finishClose records the result a repeated close reports.
func (h *Host) finishClose(first, fatal error) {
	h.mu.Lock()
	if fatal != nil {
		h.closeErr = fatal
	} else {
		h.closeErr = first
	}
	h.mu.Unlock()
	if h.cancel != nil {
		h.cancel()
	}
}

// Run reports the assembled run the host drives.
func (h *Host) Run() *Run {
	if h == nil {
		return nil
	}
	return h.run
}

// FaceAddr reports the address the loopback endpoints listen on, and an empty
// address beside a host that serves none.
func (h *Host) FaceAddr() string {
	if h == nil || h.face == nil {
		return ""
	}
	return h.face.addr()
}

// nextInvocation mints the invocation identity of one drive the host admits on
// behalf of its endpoints.
func (h *Host) nextInvocation(addr event.Address) event.InvocationID {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.invocations++
	return event.InvocationID(fmt.Sprintf("%s#%d", addr.SessionID, h.invocations))
}

// rememberUserAddress records the user session the task face drives when the
// next injection arrives, and keeps that session's own address for the
// follow-up turn a terminal notice fires.
func (h *Host) rememberUserAddress(addr event.Address) {
	h.mu.Lock()
	h.lastAddr = addr
	if addr.SessionID != "" {
		h.userAddresses[addr.SessionID] = addr
	}
	h.mu.Unlock()
}

// userAddressOf reports the user address the host remembered for one session:
// the explicit address an unattended follow-up turn runs at. A session the host
// never admitted answers no address, so a settle of a session no user drives
// fires nothing.
func (h *Host) userAddressOf(session event.SessionID) (event.Address, bool) {
	if h == nil {
		return event.Address{}, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	addr, ok := h.userAddresses[session]
	return addr, ok && addr.IsComplete()
}

// userAddress reports the user session the host admitted last.
func (h *Host) userAddress() (event.Address, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastAddr, h.lastAddr.IsComplete()
}

// entryAgent reports the entry agent the generation in effect declares: the
// declaration the run published last, so a reload that moves the entry speaks for
// the next turn. The entry Boot published is the fallback of a host whose run
// published no generation yet.
func (h *Host) entryAgent() event.AgentID {
	if h == nil {
		return ""
	}
	if h.run != nil {
		if entry := h.run.publishedEntry(); entry != "" {
			return entry
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.entry
}

// injectAddress spells the address of one inject-only session: the entry
// agent beside a session identity under the "injected" namespace, kept apart
// from every chat session a channel spells. The label is reduced to the safe
// spelling so two labels never collide through escaping.
func (h *Host) injectAddress(label string) event.Address {
	var b strings.Builder
	b.WriteString("injected:")
	for _, r := range strings.TrimSpace(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 72 {
			break
		}
	}
	return event.Address{AgentID: h.entryAgent(), SessionID: event.SessionID(b.String())}
}

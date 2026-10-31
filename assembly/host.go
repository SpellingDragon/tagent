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

// ErrHostSenderMissing reports a host that grants the send tool or admits an
// invocation at the user address beside no outbound message sender: the turn
// would produce output nothing can deliver, so the run is refused by name.
var ErrHostSenderMissing = errors.New("tagent: the host carries no outbound message sender")

// ErrHostMeditationToolMisplaced reports a document that grants the
// cross-session write tool outside the meditation session, or grants the send
// tool to the meditation session itself: the summary tool belongs to the
// reflection turn alone, and send is a host channel face a reflection session
// never carries.
var ErrHostMeditationToolMisplaced = errors.New("tagent: a meditation tool is granted to the wrong agent")

// ErrHostAddressMissing reports an invocation that names no complete session
// address: the runtime is driven at an explicit address and never at a
// most-recent one.
var ErrHostAddressMissing = errors.New("tagent: the invocation names no complete session address")

// ErrHostClosed reports an invocation or endpoint drive that reached a host
// whose close already began.
var ErrHostClosed = errors.New("tagent: the host is closed")

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

// Host is the assembled runtime of one host document: the run the entry
// drives, the scheduled reflection beside it and the two loopback endpoints a
// supervisor and an inbound channel reach. The host holds no application
// concept of its own: every channel identity, approval ruling and delivery
// target arrives through the options and the invocation address.
type Host struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    *Run
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

	// configPath is the source the host was booted from and the file a reload
	// reads again; doc is the document last published, natives are the tool
	// instances every declared agent was assembled with, models builds a model
	// from the document in effect and capture is the run-wide trajectory.
	configPath string
	doc        *hostDocument
	natives    map[event.AgentID]NativeAgent
	models     *hostModelBuilder
	capture    *rl.TrajectoryCapture

	face       *hostFace
	meditation *meditation.Driver
	settle     *SettleDriver
	// consumers owns the resident consumption loops of the run: one per user
	// address an entry wakes, each the only driver of the model decisions of
	// its address. It stays nil beside no assembled run loop.
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
	closed         bool
	closeErr       error

	// gates holds the one-slot gate of each session address: a channel, never a
	// mutex, so a cancelled follow-up leaves the wait instead of outliving the
	// stop that cancelled it.
	gates sync.Map
}

var (
	_ func(string, ...HostOption) (*Host, error)                      = Boot
	_ func(MessageSender) HostOption                                  = WithSender
	_ func(plugin.DeliveryRuler) HostOption                           = WithDeliveryRuler
	_ func(task.Approver) HostOption                                  = WithApprover
	_ func(state.Governance) HostOption                               = WithGovernance
	_ func(*Host, context.Context, InvokeRequest) (agent.Turn, error) = (*Host).Invoke
	_ func(*Host, context.Context) error                              = (*Host).Close
	_ func(*Host) *Run                                                = (*Host).Run
	_ func(*Host) string                                              = (*Host).FaceAddr
	_ func(*Host, context.Context) (hostReloadReport, error)          = (*Host).Reload
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
// - The one approval book of a boot is the host-attached book when the caller mounted it, and a boot-local book under <root>/approvals only fills a process that declared none, so a caller that mounted the default book keeps ruling the very book it handed over; the risk-rule gate and the loop's batch fuse read that same book.
//
// - A fuse declaration on the entry agent wires the loop: a response above the call cap or past the long-context threshold parks its whole batch on that one book before anything executes, and a declaration that names neither knob leaves the limits the process already declared in place.
//
// - TAGENT_GCTRACE=1 asks for the runtime's GC trace, which the runtime reads when the process starts: a boot merges GODEBUG=gctrace=1 so the subprocesses this host starts trace, and names in one line that the host process itself traces only when it was launched beside it.
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
	if cfg.hostSender == nil && doc.declaresSendOutside(meditationAgentOf(meditationPlan)) {
		return nil, fmt.Errorf("%w: the document grants the %q tool", ErrHostSenderMissing, hostSendToolID)
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
	client := &http.Client{Timeout: doc.requestTimeout()}
	models := newHostModelBuilder(doc, client)
	// host is assigned once the assembly returns; every face assembled before it
	// reads the variable at call time instead of capturing a nil instance, because
	// a call can only ever run beside a published host.
	var host *Host
	deliverSend := func(ctx context.Context, delivery plugin.Delivery) (deliveryOutcome, string) {
		if host == nil {
			return deliveryRefused, "the host is not assembled"
		}
		return host.deliverOne(ctx, host.sender, delivery)
	}
	natives, err := doc.nativeAgents(ctx, client, cfg.hostSender, deliverSend, recallPort, meditationPlan, meditationPort)
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
				Loop:    meditation.ReflectionLoop{Agent: meditationPlan.agent, Session: meditationPlan.session},
				Face:    meditation.ReflectionFace{ObservedNamespaces: meditationPlan.observed, ReadNamespaces: meditationPlan.reads},
				Cadence: meditation.ReflectionCadence{Interval: meditationPlan.interval, MinGap: meditationPlan.minGap},
				Prompt:  meditationPlan.prompt,
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
		models:            models,
		capture:           capture,
		telemetryStop:     telemetryStop,
		userAddresses:     map[event.SessionID]event.Address{},
	}
	if err := host.restorePendingDeliveries(ctx); err != nil {
		cancel()
		return nil, host.abortBoot(err)
	}
	host.consumers = agent.NewConsumers(run.loop, ctx, host.deliverClosedSegment)
	host.consumers.SetTurnContext(func(decisionCtx context.Context, trigger string) context.Context {
		return withDeliveryLineage(decisionCtx, deliveryLineageOf(trigger))
	})
	run.loop.SetConsumers(host.consumers)
	if settleDriver != nil {
		settleDriver.Wake()
	}
	face, err := newHostFace(ctx, host, os.Getenv(hostFaceTokenEnv))
	if err != nil {
		cancel()
		return nil, host.abortBoot(err)
	}
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
		_, _ = h.run.Close(closeCtx, agent.CloseRequest{})
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

// Invoke drives one invocation through the assembled run. An invocation at the
// user address of the entry agent delivers the fresh assistant output through
// the message sender; every other address is driven headless.
//
// A user message commits as a fact of its session and wakes the resident
// consumption loop of its address: the loop pulls the message with whatever
// else arrived, batches them into one model request, and the call answers with
// the aggregated turn of the segment that consumed it — a second entry whose
// message the same segment consumed answers the same turn. Every other address
// drives a turn of its own through the run, exactly as before.
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
	user := req.Address.AgentID == h.entry
	sender := h.sender
	consumers := h.consumers
	h.mu.Unlock()
	if user && sender == nil {
		return agent.Turn{}, fmt.Errorf("%w: the turn runs at the user address of %q", ErrHostSenderMissing, h.entry)
	}
	if req.InvocationID == "" {
		req.InvocationID = h.nextInvocation(req.Address)
	}
	if user && consumers != nil {
		return h.invokeConsumed(ctx, req)
	}
	unlock, err := h.lockAddressContext(ctx, req.Address)
	if err != nil {
		return agent.Turn{}, err
	}
	defer unlock()
	return h.driveLocked(ctx, req)
}

// invokeConsumed lands the input of one user request as the facts of its
// session, wakes the resident consumption loop of its address, and answers
// with the aggregated turn of the segment that consumed them: the model
// request the loop served read the message beside whatever else arrived. The
// fresh bodies of the closing answer leave through the sender from the
// consumer's own delivery face, once per closed segment, so an entry that only
// waits on the segment never delivers a body a second time.
//
// - An entry whose context ended while its segment still flies answers the abandoned turn it asked for: the caller reads a cancelled turn beside no error, exactly as the direct drive answers one, and the segment stays with the loop that still flies it.
func (h *Host) invokeConsumed(ctx context.Context, req InvokeRequest) (agent.Turn, error) {
	now := time.Now()
	keys := make([]event.EventID, 0, len(req.Input))
	for _, msg := range req.Input {
		evt := h.run.inputEventOf(req, msg, req.InvocationID, now)
		id, err := h.run.commitOnly(ctx, req.Address, evt)
		if err != nil {
			return agent.Turn{}, fmt.Errorf("tagent: the input of %s did not commit: %w", req.InvocationID, err)
		}
		if id != "" {
			keys = append(keys, id)
		}
	}
	h.rememberUserAddress(req.Address)
	consumer := h.consumers.Ensure(req.Address)
	if consumer == nil {
		return agent.Turn{}, ErrHostClosed
	}
	consumer.Wake()
	answer, err := consumer.WaitAnswer(ctx, keys)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return agent.Turn{InvocationID: req.InvocationID, Terminal: model.KindCancelled}, nil
		}
		return answer.Turn, err
	}
	return answer.Turn, nil
}

// deliverClosedSegment hands the fresh answer of one closed consumption segment
// to the outbound face of its address: the assistant bodies the invocation of
// that segment generated leave under the lineage the material that opened it
// named, and the answer of a segment no entry waits on — the one
// a settle notice drives into being — leaves by the very same path. A host
// closed beside the closing segment delivers nothing, and the obligation stays
// owed to the restart.
//
// The settle account of an entry whose notice material this segment consumed is
// closed here, before the answer leaves: the entry that waits on the segment
// reads its answer only after this face ran, so closing the account behind the
// wait would leave the terminal of a landed turn unrecorded for as long as the
// send runs. A stop inside the send is then answered by a restart that closes
// the landed attempt instead of driving the notice a second time.
func (h *Host) deliverClosedSegment(ctx context.Context, addr event.Address, answer agent.SegmentAnswer) {
	h.mu.Lock()
	closed := h.closed
	sender := h.sender
	h.mu.Unlock()
	if closed {
		return
	}
	h.settleConsumedNotices(ctx, answer)
	h.deliverFresh(ctx, addr, sender, deliveryLineageOf(answer.Trigger), answer.Turn)
}

// settleConsumedNotices closes the settle account of every entry whose notice
// material the closing segment consumed: the terminal the segment landed is
// recorded as the attempt of its notification, the delivery obligation of every
// body the turning segment generated is registered, and the consumption receipt
// of the entry is written. The registration is taken, not read, so one delivery
// closes one entry exactly once.
func (h *Host) settleConsumedNotices(ctx context.Context, answer agent.SegmentAnswer) {
	for _, barrier := range h.takeNoticeAccounts(answer.Consumed) {
		if err := h.settleTerminal(withSettleBarrier(ctx, barrier), answer.Turn); err != nil {
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

// driveLocked drives one invocation whose session gate the caller already
// holds, so a user turn and an unattended follow-up turn of the same address
// never run beside each other. The delivery of a user turn reads the assistant
// bodies its own range committed, anchored on the identity of the invocation
// rather than on a position of the chain read at the gate: the chain is held to
// its hot window, so a position names other material once the window moves. The
// token stays held until this method returns, so the settle account and the
// delivery read the chain without a neighbour committing beside them.
//
// A follow-up drive carries the barrier of the settle account it answers on, and
// the barrier is closed right after the terminal fact of the drive landed: the
// terminal the turn ended on is recorded, a completed turn writes its
// consumption receipt there, and only then is the answer delivered. So the
// receipt never lags the delivery it confirms by a whole turn, and a stop
// between the two leaves a landed attempt the next process closes with its
// receipt instead of driving a second turn.
func (h *Host) driveLocked(ctx context.Context, req InvokeRequest) (agent.Turn, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return agent.Turn{}, ErrHostClosed
	}
	user := req.Address.AgentID == h.entry
	sender := h.sender
	h.mu.Unlock()
	if user {
		h.rememberUserAddress(req.Address)
	}
	lineage := deliveryLineageOf(req.TriggerSource)
	ctx = withDeliveryLineage(ctx, lineage)
	turn, err := h.run.Invoke(ctx, req)
	if err != nil {
		assemblyWarn("tagent: the invocation %s of %s ended by error: %v", req.InvocationID, req.Address.SessionID, err)
		return turn, err
	}
	if err := h.settleTerminal(ctx, turn); err != nil {
		assemblyWarn("tagent: the invocation %s of %s left its settle account open: %v", req.InvocationID, req.Address.SessionID, err)
		return turn, err
	}
	if user {
		h.deliverFresh(ctx, req.Address, sender, lineage, turn)
	}
	return turn, nil
}

// settleTerminal closes the settle account of one follow-up drive beside the
// terminal fact it just landed: the terminal the turn ended on is recorded as
// the attempt of the notification that fired it, a turn that completed registers
// the delivery obligation of every body it generated, and only behind those
// obligations is the consumption receipt of its entry written — before the
// answer leaves through the sender and before the drive returns to its caller.
// The obligation stands before the receipt because the receipt forbids every
// replay of the notification: a stop between the two is answered by the owed
// body a restart delivers, never by a second turn. A turn that ended on any
// other terminal records that terminal and leaves the entry owed, so the next
// wake retries the delivery instead of reading a stopped turn as a delivered
// one. A body the account could not hold refuses the receipt, so no receipt ever
// closes an entry whose body no account carries: the landed attempt is read
// again by the next wake, which registers the body and closes the entry. A drive
// that carries no barrier is a user turn and settles nothing here.
func (h *Host) settleTerminal(ctx context.Context, turn agent.Turn) error {
	barrier, held := settleBarrierFrom(ctx)
	if !held {
		return nil
	}
	if err := barrier.settle(ctx, turn.Terminal); err != nil {
		assemblyWarn("tagent: the settle attempt of %s was not recorded: %v", barrier.notice.FactKey(), err)
	}
	if turn.Terminal != model.KindCompleted {
		return nil
	}
	lineage, stamped := deliveryLineageFrom(ctx)
	if !stamped {
		lineage = "task"
	}
	bodies := generatedBodiesOf(h.run.SessionFacts(barrier.addr), turn.InvocationID)
	if _, err := h.recordDeliveryOwed(ctx, barrier.addr, lineage, bodies); err != nil {
		return fmt.Errorf("tagent: the delivery obligations of the settled turn %s were not registered: %w", barrier.notice.FactKey(), err)
	}
	if err := barrier.consume(ctx); err != nil && !errors.Is(err, state.ErrClaimSettled) {
		assemblyWarn("tagent: the settle consumption of %s beside its terminal fact reported: %v", barrier.notice.FactKey(), err)
	}
	return nil
}

// fireNoticeTurn drives one unattended follow-up turn at the user address a
// terminal settle belongs to. The turn carries no input: the notice fact this
// method commits under the follow-up's own invocation is the material the drive
// reads at its head, because a range reads the notifications of its own
// identity prefix. The turn is stamped "task" and holds the same address gate a
// user turn holds, so the two never run beside each other.
//
// The invocation is the notification's own four-part identity rather than a
// minted counter: the same notification always drives the same range, so a
// second attempt commits the same notice fact and is answered as the fact the
// chain already holds — the transcript carries the instruction once, however
// many processes try the notification. The fact it commits is a feedback fact,
// never a terminal settle with a settle lineage, so the driver that fired this
// turn never fires on its output. The assistant body the turn produces is
// delivered through the message sender by the ordinary drive path, exactly as a
// user turn's output is, and the entry the turn drove is closed beside its
// terminal fact by the barrier the driver stamped on the context. Only a turn
// that really delivered its answer closes the mailbox entry it drove from: a
// stopped or failed turn reports the incomplete delivery by name, so the entry
// stays owed for the next wake instead of a receipt recording a turn nobody
// read.
//
// - An address a resident consumption loop owns is driven by that loop alone: the notice material commits as the fact it is and the wake hands it to the next pull of the loop, so no follow-up turn runs beside the segment the loop may already fly. The entry the notification was accepted on is settled beside the turn the loop lands, exactly as a direct drive settles it: the entry waits for the segment that consumed the material, the landed terminal is recorded as its attempt, and only then is the consumption receipt written — an entry is never closed beside a turn that never ran. The answer leaves through the delivery face of the consumer, which is the same path a user turn's answer leaves by.
//
// - The segment that consumes the material answers the entry only after its own delivery, so the account of the entry is registered here, under the key the material lands as and before the commit: the delivery face of the consumer closes it beside the segment that consumed that key, and a segment that closed without reading the material leaves the account owed for the wake.
// - A settled result the flying segment still awaits is harvested by that segment's own next decision (the account settles through settleConsumedNotices), so the notice turn drives only a task already paired or never admitted.
func (h *Host) fireNoticeTurn(ctx context.Context, addr event.Address, fact event.Event) error {
	if h == nil || h.run == nil {
		return ErrHostClosed
	}
	if !addr.IsComplete() {
		return fmt.Errorf("%w: agent=%q session=%q", ErrHostAddressMissing, addr.AgentID, addr.SessionID)
	}
	h.mu.Lock()
	closed := h.closed
	consumers := h.consumers
	h.mu.Unlock()
	if closed {
		return ErrHostClosed
	}
	if consumers.Has(addr) {
		if barrier, held := settleBarrierFrom(ctx); held {
			_, _, key := noticeMaterialOf(ctx, fact)
			defer h.holdNoticeAccount(key, barrier)()
		}
		if consumer := consumers.Ensure(addr); consumer != nil {
			settleWait, settleCancel := context.WithTimeout(ctx, hostSettleWindow)
			state := consumer.AwaitTaskState(settleWait, noticeTaskIDOf(fact))
			settleCancel()
			if state == agent.TaskConsumed {
				return nil
			}
			if state == agent.TaskAwaited {
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
		consumer.Wake()
		answer, err := consumer.WaitAnswer(ctx, []event.EventID{key})
		if err != nil {
			return err
		}
		if err := h.settleTerminal(ctx, answer.Turn); err != nil {
			assemblyWarn("tagent: the notice invocation %s of %s left its settle account open: %v",
				answer.Turn.InvocationID, addr.SessionID, err)
			return err
		}
		if answer.Turn.Terminal != model.KindCompleted {
			return fmt.Errorf("%w: %s ended as %s", ErrNoticeTurnIncomplete, answer.Turn.InvocationID, answer.Turn.Terminal)
		}
		return nil
	}
	unlock, err := h.lockAddressContext(ctx, addr)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := h.commitNoticeMaterial(ctx, addr, fact); err != nil {
		return err
	}
	identified := task.Notice{TaskID: noticeTaskIDOf(fact), Kind: noticeKindOf(fact), Event: fact.Key}
	if barrier, held := settleBarrierFrom(ctx); held {
		identified = barrier.notice
	}
	invocationID := noticeInvocation(identified)
	turn, err := h.driveLocked(ctx, InvokeRequest{
		InvocationID:  invocationID,
		Address:       addr,
		TriggerSource: "task",
	})
	if err != nil {
		return err
	}
	if turn.Terminal != model.KindCompleted {
		return fmt.Errorf("%w: %s ended as %s", ErrNoticeTurnIncomplete, invocationID, turn.Terminal)
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

// Close stops the endpoints and the reflection driver, drains the run and
// closes the MCP servers. A repeated close reports the first result and runs no
// second drain.
func (h *Host) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	if h.closed {
		err := h.closeErr
		h.mu.Unlock()
		return err
	}
	h.closed = true
	h.mu.Unlock()
	var first error
	if h.consumers != nil {
		h.consumers.Stop()
	}
	if h.settle != nil {
		h.settle.Stop()
	}
	if h.meditation != nil {
		h.meditation.Stop()
	}
	if h.face != nil {
		if err := h.face.stop(ctx); err != nil {
			first = err
		}
	}
	if h.execManager != nil {
		for _, report := range h.execManager.Close(ctx) {
			if report.Evidence.Released {
				continue
			}
			assemblyWarn("tagent: the execution resource %s stays held after close: %s", report.Key, report.Evidence.Reason)
		}
	}
	h.execWG.Wait()
	report, err := h.run.Close(ctx, agent.CloseRequest{})
	if err != nil {
		if sealErr := h.sealCapture(ctx); sealErr != nil {
			assemblyWarn("tagent: the trajectory seal after a failed drain reported: %v", sealErr)
		}
		h.finishClose(first, err)
		return err
	}
	if !report.Drained {
		assemblyWarn("tagent: the host close reported %d unsettled obligation(s)", len(report.Unsettled))
	}
	if err := h.run.CloseMCPServers(); err != nil && first == nil {
		first = err
	}
	if err := h.sealCapture(ctx); err != nil && first == nil {
		first = err
	}
	if err := h.stopTelemetry(ctx); err != nil && first == nil {
		first = err
	}
	h.finishClose(first, nil)
	return first
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
	h.mu.Lock()
	entry := h.entry
	h.mu.Unlock()
	return event.Address{AgentID: entry, SessionID: event.SessionID(b.String())}
}

// addressGate is the one-slot gate that serializes the drives of one session:
// the token is held by the drive in flight, and a waiter parks on the channel
// instead of a mutex, so a cancelled follow-up leaves the wait.
type addressGate struct {
	token chan struct{}
}

// gateOf reports the one-slot gate of one session address.
func (h *Host) gateOf(addr event.Address) *addressGate {
	value, _ := h.gates.LoadOrStore(addr, &addressGate{token: make(chan struct{}, 1)})
	return value.(*addressGate)
}

// lockAddress serializes the drives of one session.
func (h *Host) lockAddress(addr event.Address) func() {
	unlock, err := h.lockAddressContext(context.Background(), addr)
	if err != nil {
		return func() {}
	}
	return unlock
}

// lockAddressContext serializes the drives of one session and leaves the wait
// when the caller's context ends: the stop of the settle driver cancels the
// follow-up waiting here, so a close never hangs on a gate a user turn holds.
func (h *Host) lockAddressContext(ctx context.Context, addr event.Address) (func(), error) {
	gate := h.gateOf(addr)
	select {
	case gate.token <- struct{}{}:
		return func() { <-gate.token }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

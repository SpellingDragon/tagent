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
	invocations   uint64
	closed        bool
	closeErr      error

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

// Boot reads one host document and returns the assembled host: the
// declaration is resolved from the document, the models, tool instances,
// storage tier, embedder and reflection plan are built for it, the run is
// assembled through Assemble, and the reflection driver and the loopback
// endpoints start beside it. Every failure before the assembly returns the
// named refusal and leaves nothing running.
func Boot(configPath string, opts ...HostOption) (*Host, error) {
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
	var capture *rl.TrajectoryCapture
	if root := storageRootOf(plan); root != "" {
		capture, err = openHostCapture(root)
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
			governance.WithApprovals(cfg.approvals),
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
			driver = meditation.NewDriver(reflection, meditationPlan.interval, meditationPlan.minGap)
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
// A user message that arrives while a drive of that address is already running
// is admitted into the chain of that running range instead of parking outside
// the gate: the message reaches the next decision boundary of the turn in
// flight, and the call answers the admission itself — no decision reached and no
// error. Every other arrival, and a user arrival beside a gate no drive holds,
// takes the gate and drives a turn of its own, exactly as before.
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
	h.mu.Unlock()
	if user && sender == nil {
		return agent.Turn{}, fmt.Errorf("%w: the turn runs at the user address of %q", ErrHostSenderMissing, h.entry)
	}
	if req.InvocationID == "" {
		req.InvocationID = h.nextInvocation(req.Address)
	}
	if user {
		if turn, accepted, err := h.acceptInflight(ctx, req); accepted {
			return turn, err
		}
	}
	unlock, err := h.lockAddressContext(ctx, req.Address)
	if err != nil {
		return agent.Turn{}, err
	}
	defer unlock()
	return h.driveLocked(ctx, req)
}

// acceptInflight lands the messages of one user request in the chain of the
// range a drive is running at that address, and answers the admission itself.
// The facts commit under the identity of the running range, so that drive reads
// them at its own next decision boundary instead of a second turn opening after
// it; the After pass of each fact stays with that boundary read, which observes
// one fact of its range once. The answer carries no decision and no error: what
// the call reports is that the message is on the chain the turn in flight reads.
//
// A gate no drive holds names no range, and the call is not admitted here: the
// caller takes the gate and drives its turn as it always did. The gate is read
// once more behind the commit for the reason the drive clears its range before
// its own bookkeeping: a range may reach its last decision boundary while the
// message lands, and that range reads the chain never again. A gate that names
// no range from here on is answered the same way — the caller waits for the
// address and drives a turn of its own, which reads the message as its input
// instead of leaving it on a chain no decision of that range will read.
func (h *Host) acceptInflight(ctx context.Context, req InvokeRequest) (agent.Turn, bool, error) {
	inflight, running := h.gateOf(req.Address).running()
	if !running {
		return agent.Turn{}, false, nil
	}
	now := time.Now()
	for _, msg := range req.Input {
		evt := h.run.inputEventOf(req, msg, inflight, now)
		if _, err := h.run.commitOnly(ctx, req.Address, evt); err != nil {
			return agent.Turn{}, true, fmt.Errorf("tagent: the input of %s was not committed into the chain of the range in flight %s: %w",
				req.InvocationID, inflight, err)
		}
	}
	if _, stillRunning := h.gateOf(req.Address).running(); !stillRunning {
		return agent.Turn{}, false, nil
	}
	return agent.Turn{InvocationID: inflight, Terminal: model.KindUnknown}, true, nil
}

// driveLocked drives one invocation whose session gate the caller already
// holds, so a user turn and an unattended follow-up turn of the same address
// never run beside each other. The baseline of a user turn is read after the
// gate is held and before the drive: the delivery of that turn reads exactly
// the assistant bodies its own range committed past it. The address stops naming
// the range as soon as the drive returns, because no boundary of that range
// reads input after Invoke answered: an input that arrives from here on waits
// for the token and drives a turn of its own instead of landing in a range that
// would never read it, while the token itself stays held until this method
// returns, so the settle account and the delivery read the chain without a
// neighbour committing beside them.
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
	gate := h.gateOf(req.Address)
	gate.hold(req.InvocationID)
	defer gate.release()
	var baseline int
	if user {
		h.rememberUserAddress(req.Address)
		baseline = len(h.run.SessionFacts(req.Address))
	}
	lineage := deliveryLineageOf(req.TriggerSource)
	ctx = withDeliveryLineage(ctx, lineage)
	turn, err := h.run.Invoke(ctx, req)
	gate.release()
	if err != nil {
		assemblyWarn("tagent: the invocation %s of %s ended by error: %v", req.InvocationID, req.Address.SessionID, err)
		return turn, err
	}
	if err := h.settleTerminal(ctx, turn); err != nil {
		assemblyWarn("tagent: the invocation %s of %s left its settle account open: %v", req.InvocationID, req.Address.SessionID, err)
		return turn, err
	}
	if user {
		h.deliverFresh(ctx, req.Address, baseline, sender, lineage, turn)
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
func (h *Host) fireNoticeTurn(ctx context.Context, addr event.Address, fact event.Event) error {
	if h == nil || h.run == nil {
		return ErrHostClosed
	}
	if !addr.IsComplete() {
		return fmt.Errorf("%w: agent=%q session=%q", ErrHostAddressMissing, addr.AgentID, addr.SessionID)
	}
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		return ErrHostClosed
	}
	unlock, err := h.lockAddressContext(ctx, addr)
	if err != nil {
		return err
	}
	defer unlock()
	kind := noticeKindOf(fact)
	notice := task.Notice{TaskID: noticeTaskIDOf(fact), Kind: kind, Event: fact.Key}
	identified := notice
	if barrier, held := settleBarrierFrom(ctx); held {
		identified = barrier.notice
	}
	invocationID := noticeInvocation(identified)
	material := strings.TrimSpace(fact.Message.Content)
	if material == "" {
		material = settleNoticeText(notice)
	}
	if _, err := h.run.CommitToSession(ctx, addr, event.Event{
		Key:       agent.NoticeFactKey(invocationID, notice),
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource: "task",
			event.MetaKeyEventSummary:  string(kind),
			event.MetaKeyNoticeKind:    string(kind),
			event.MetaKeyTaskID:        string(notice.TaskID),
		},
		Message:      model.Message{Role: model.RoleUser, Content: material},
		InvocationID: invocationID,
		TaskID:       notice.TaskID,
	}); err != nil {
		return err
	}
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
// instead of a mutex, so a cancelled follow-up leaves the wait. Beside the token
// the gate names the range the holder drives, which is the identity an input
// admitted while the drive runs commits under.
type addressGate struct {
	token chan struct{}
	// mu guards inflight, the range the holder of the token drives; it reads
	// empty beside a gate no drive holds.
	mu       sync.Mutex
	inflight event.InvocationID
}

// gateOf reports the one-slot gate of one session address.
func (h *Host) gateOf(addr event.Address) *addressGate {
	value, _ := h.gates.LoadOrStore(addr, &addressGate{token: make(chan struct{}, 1)})
	return value.(*addressGate)
}

// hold names the range the drive of the token is about to run, so an input that
// arrives beside it commits into that range's chain.
func (g *addressGate) hold(invocation event.InvocationID) {
	g.mu.Lock()
	g.inflight = invocation
	g.mu.Unlock()
}

// release forgets the range the drive of the token ran. It runs before the token
// is handed back, so a gate that reads free names no range.
func (g *addressGate) release() {
	g.mu.Lock()
	g.inflight = ""
	g.mu.Unlock()
}

// running reports the range the holder of the token drives, and false beside a
// gate whose holder named none.
func (g *addressGate) running() (event.InvocationID, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.inflight, g.inflight != ""
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

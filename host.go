// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// Boot is the turnkey entry of the composition root: it reads one host
// document, resolves the declaration it carries, and returns the Host the
// application drives. The Host owns the assembled run, the scheduled
// reflection and the two loopback endpoints; the application fills the
// outbound message face, the approval face and the reflection delivery face
// through the options, and reads nothing else out of the document.
package tagent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/features/evolution/meditation"
	"github.com/SpellingDragon/tagent/features/rl"
	"github.com/SpellingDragon/tagent/features/telemetry"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	tagentexec "github.com/SpellingDragon/tagent/tool/exec"
	tagentfile "github.com/SpellingDragon/tagent/tool/file"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
	"github.com/SpellingDragon/tagent/tool/recall"
	"github.com/SpellingDragon/tagent/tool/spec"
	"github.com/SpellingDragon/tagent/tool/subagent"

	"github.com/openai/openai-go/option"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	fmetric "trpc.group/trpc-go/trpc-agent-go/telemetry/metric"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
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
	unlock, err := h.lockAddressContext(ctx, req.Address)
	if err != nil {
		return agent.Turn{}, err
	}
	defer unlock()
	return h.driveLocked(ctx, req)
}

// driveLocked drives one invocation whose session gate the caller already
// holds, so a user turn and an unattended follow-up turn of the same address
// never run beside each other. The baseline of a user turn is read after the
// gate is held and before the drive: the delivery of that turn reads exactly
// the assistant bodies its own range committed past it.
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
	var baseline int
	if user {
		h.rememberUserAddress(req.Address)
		baseline = len(h.run.SessionFacts(req.Address))
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
// instead of a mutex, so a cancelled follow-up leaves the wait.
type addressGate struct{ token chan struct{} }

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
	value, _ := h.gates.LoadOrStore(addr, &addressGate{token: make(chan struct{}, 1)})
	gate := value.(*addressGate)
	select {
	case gate.token <- struct{}{}:
		return func() { <-gate.token }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// failureNoticeMaxBytes bounds the raw cause one failure notice carries: a
// provider error may run long, so the notice keeps its first 2 KiB and marks
// the cut instead of letting the channel refuse the whole notice.
const failureNoticeMaxBytes = 2 << 10

// failureNoticeTruncated marks a failure notice whose cause was cut at
// failureNoticeMaxBytes.
const failureNoticeTruncated = "（已截断）"

// failureNoticeSummary is the summary prefix of the fact one failure notice
// leaves in the session chain: the audit line a later reader finds the notice
// by.
const failureNoticeSummary = "failure-notice"

// deliverFresh delivers the assistant bodies the session's committed facts carry past
// the invocation baseline through the sender; a session the chain maps onto a projection
// commits no transcript history, so the facts are the one read face both session kinds share.
//   - A body wins over a failure: a turn that produced a deliverable answer delivers it.
//   - A failed turn without a body delivers the raw recorded cause; no cause stays silent.
//   - Each body stands as a durable delivery obligation before its send: a stop between the
//     record and the confirmed send is answered by a restart that sends the body, and the
//     obligation is settled under the identity the generation registered.
func (h *Host) deliverFresh(ctx context.Context, addr event.Address, baseline int, sender MessageSender, lineage string, turn agent.Turn) {
	if sender == nil {
		return
	}
	bodies := freshAssistantOf(h.run.SessionFacts(addr), baseline)
	if len(bodies) == 0 {
		if turn.Terminal == model.KindFailed {
			if notice := failureNoticeText(turn.Failure); notice != "" {
				h.noteFailureNotice(ctx, addr, turn)
				h.deliverOne(ctx, sender, plugin.Delivery{Content: notice, Lineage: lineage, Target: addr})
				return
			}
			assemblyDebug("tagent: the invocation of %s failed without a recorded cause", addr.SessionID)
			return
		}
		assemblyDebug("tagent: the invocation of %s produced no deliverable assistant body", addr.SessionID)
		return
	}
	if _, err := h.recordDeliveryOwed(ctx, addr, lineage, bodies); err != nil {
		assemblyWarn("tagent: the delivery obligations of %s were not registered: %v", addr.SessionID, err)
	}
	for _, body := range bodies {
		obligation, _ := freshObligationOf(body)
		delivery := plugin.Delivery{Content: body.content, Lineage: lineage, Target: addr}
		if _, cause := h.deliverObligation(ctx, obligation, sender, delivery); cause != "" {
			assemblyDebug("tagent: the delivery to %s stands owed: %s", addr.SessionID, cause)
		}
	}
}

// HeldDelivery is one outbound body the review plugin held before the sending
// plugin delivered it: the identity an approval or refusal names, the delivery
// semantic the ruler read, and the moment the hold was recorded.
type HeldDelivery struct {
	// ID names this hold and is the identity ApproveDelivery and
	// RefuseDelivery resolve.
	ID string

	// Delivery is the semantic the ruler held.
	Delivery plugin.Delivery

	// HeldAt is the moment the host recorded the hold.
	HeldAt time.Time
}

// heldDelivery is the host-side record of one held delivery: the public
// identity beside the order the hold was taken in.
type heldDelivery struct {
	id       string
	delivery plugin.Delivery
	heldAt   time.Time
}

// hostDeliveryPrefix locates the durable record of every delivery obligation
// the review table holds, on the same fact surface the durable consume
// receipts commit through.
const hostDeliveryPrefix = "tagent/task/v1/delivery:"

// hostDeliveryOwed names the record state of a body the host generated and has
// not delivered: the obligation a restart reads back and sends through the same
// review ruling the live path applies.
const hostDeliveryOwed = "owed"

// hostDeliveryFreshPrefix names the obligation of one generated body: the record
// is keyed by the identity of the fact that carries the body, so the obligation a
// restart registers and the one the live path registered name one record.
const hostDeliveryFreshPrefix = "delivery-fresh-"

// hostDeliveryHeld names the record state of a body the review parked before
// the sender ran.
const hostDeliveryHeld = "held"

// hostDeliveryRetry names the record state of a body the channel refused: the
// record of the redelivery the body owes.
const hostDeliveryRetry = "retry"

// hostDeliverySent names the record state of a body the sender accepted: the
// receipt of an obligation discharged.
const hostDeliverySent = "sent"

// hostDeliveryRefused names the record state of a body a refusal discarded:
// the receipt of an obligation given up beside its audit fact.
const hostDeliveryRefused = "refused"

// hostDeliveryHeadless names the record state of a body retired without a send
// because it was generated at an address that is not the entry: the host drives
// every other address headless, so its answer belongs to the caller that drove it
// and never to a message channel.
const hostDeliveryHeadless = "headless"

// hostDeliveryRecord is the durable form of one delivery obligation: the
// identity, content, lineage and target of the body beside the state the
// obligation stands in and the moment it was taken.
type hostDeliveryRecord struct {
	ID      string        `json:"id"`
	Content string        `json:"content"`
	Lineage string        `json:"lineage"`
	Target  event.Address `json:"target"`
	Status  string        `json:"status"`
	HeldAt  time.Time     `json:"held_at"`
}

// HeldDeliveries reports the bodies the host owes a target before delivery:
// the holds the review plugin parked and the redeliveries a refused send owes,
// oldest first. The result is a copy the caller owns: reading here changes no
// hold, and only ApproveDelivery or RefuseDelivery resolves one.
func (h *Host) HeldDeliveries() []HeldDelivery {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	held := make([]HeldDelivery, 0, len(h.pendingDeliveries))
	for _, entry := range h.pendingDeliveries {
		held = append(held, HeldDelivery{ID: entry.id, Delivery: entry.delivery, HeldAt: entry.heldAt})
	}
	h.mu.Unlock()
	sort.Slice(held, func(i, j int) bool { return held[i].ID < held[j].ID })
	return held
}

// ApproveDelivery re-delivers one held body exactly once: the body is sent to
// the target it named, and the hold is consumed, so a repeated approval names
// no hold and delivers no second copy. A send the channel refuses restores the
// hold for a later approval and is reported to the caller. Every hold is
// recorded beside the review table, so the approval resolves the body across a
// restart as well.
//
// The receipt of the approval lands only after the send it confirms returned: a
// receipt the tier refuses leaves the obligation owed and answers the landed
// delivery by name, so the caller learns the body reached the channel and the
// account did not — a restart delivers the body once more, which is the one
// extra copy an unconfirmed receipt tolerates.
func (h *Host) ApproveDelivery(ctx context.Context, id string) error {
	if h == nil {
		return ErrHostClosed
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrHostClosed
	}
	held, ok := h.pendingDeliveries[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDeliveryNotFound, id)
	}
	sender := h.sender
	delete(h.pendingDeliveries, id)
	h.mu.Unlock()
	if sender == nil {
		missing := fmt.Errorf("%w: the held delivery of %s", ErrHostSenderMissing, id)
		return errors.Join(missing, h.restoreHeldDelivery(ctx, held, hostDeliveryRetry))
	}
	if err := sender.Send(ctx, held.delivery.Target, held.delivery.Content); err != nil {
		refused := fmt.Errorf("tagent: the approved delivery of %s was refused: %w", id, err)
		return errors.Join(refused, h.restoreHeldDelivery(ctx, held, hostDeliveryRetry))
	}
	if err := h.recordDeliveryState(ctx, held, hostDeliverySent); err != nil {
		landed := fmt.Errorf("tagent: the delivery of %s landed but its receipt did not: %w", id, err)
		return errors.Join(landed, h.restoreHeldDelivery(ctx, held, hostDeliveryHeld))
	}
	assemblyInfo("tagent: the held delivery of %s was approved and sent", id)
	return nil
}

// RefuseDelivery discards one held body: nothing is delivered, the hold is
// consumed, and an audit feedback fact without a model payload records the
// refusal at the target address. The refusal consumes the durable record as
// well, so no later boot owes the body again.
//
// Both commits are checked before the refusal publishes: a record or an audit
// fact the tier refused answers the caller by name and leaves the body in the
// review table, so a refusal no account accepted is never read as a decision.
func (h *Host) RefuseDelivery(ctx context.Context, id string) error {
	if h == nil {
		return ErrHostClosed
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return ErrHostClosed
	}
	held, ok := h.pendingDeliveries[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrDeliveryNotFound, id)
	}
	delete(h.pendingDeliveries, id)
	h.mu.Unlock()
	if err := h.recordDeliveryState(ctx, held, hostDeliveryRefused); err != nil {
		unrecorded := fmt.Errorf("tagent: the refusal of the held delivery %s was not recorded: %w", id, err)
		return errors.Join(unrecorded, h.restoreHeldDelivery(ctx, held, hostDeliveryHeld))
	}
	if err := h.noteDeliveryRefusal(ctx, held); err != nil {
		return errors.Join(err, h.restoreHeldDelivery(ctx, held, hostDeliveryHeld))
	}
	assemblyInfo("tagent: the held delivery of %s was refused", id)
	return nil
}

// deliveryDurable reports the fact surface the delivery records commit
// through: the storage tier the run opened, and nil beside a run that keeps no
// tier, where the review table stays process-local.
func (h *Host) deliveryDurable() state.FactLog {
	if h == nil || h.run == nil || h.run.tier == nil {
		return nil
	}
	return ledger.TierFactLog(h.run.tier)
}

// recordDeliveryState commits the durable record of one delivery obligation
// under the state it stands in and completes the write barrier before it
// returns, so a caller publishes a state change only behind the record that
// carries it. A run beside no storage tier keeps the review table process-local
// and commits nothing.
func (h *Host) recordDeliveryState(ctx context.Context, held *heldDelivery, status string) error {
	log := h.deliveryDurable()
	if log == nil {
		return nil
	}
	body, err := json.Marshal(hostDeliveryRecord{
		ID:      held.id,
		Content: held.delivery.Content,
		Lineage: held.delivery.Lineage,
		Target:  held.delivery.Target,
		Status:  status,
		HeldAt:  held.heldAt,
	})
	if err != nil {
		return fmt.Errorf("tagent: the delivery record of %s does not marshal: %w", held.id, err)
	}
	if err := log.Put(ctx, hostDeliveryPrefix+held.id, body); err != nil {
		return fmt.Errorf("tagent: the delivery record of %s was refused: %w", held.id, err)
	}
	if err := log.Sync(ctx); err != nil {
		return fmt.Errorf("tagent: the delivery record of %s left no write barrier: %w", held.id, err)
	}
	return nil
}

// recordDeliveryOwed registers the delivery obligation of every generated body
// the account does not hold yet, in one batch behind one write barrier, and
// reports every body whose obligation still stands owed when it returns: the
// bodies it just registered beside the ones an earlier registration left owed
// because no send ever discharged them. The hand-off sends that whole set, so an
// obligation stranded between its record and its send is redriven by the process
// that meets it again instead of waiting for a restart. A body whose record
// already carries a later state — held, retry, sent or refused — is left as it
// stands, so one generation registers one obligation however often the hand-off
// runs and a discharged body is never delivered a second time here. The write
// barrier is taken only by a batch that staged a record: a set made entirely of
// records an earlier call already committed carries nothing new to sync.
//
// Only a body generated at the entry address takes an obligation at all: every
// other address is driven headless, which means its answer is read by the caller
// that drove it and never pushed to a message channel. That is the same reader
// the live delivery applies, so the record and the send agree on which addresses
// own a delivery. A run beside no storage tier registers nothing: its deliveries
// are process-local.
func (h *Host) recordDeliveryOwed(ctx context.Context, addr event.Address, lineage string, bodies []freshBody) ([]freshBody, error) {
	log := h.deliveryDurable()
	if log == nil || len(bodies) == 0 || addr.AgentID != h.entry {
		return nil, nil
	}
	owed := make([]freshBody, 0, len(bodies))
	staged := false
	for _, body := range bodies {
		id, ok := freshObligationOf(body)
		if !ok {
			continue
		}
		key := hostDeliveryPrefix + id
		payload, held, err := log.Get(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("tagent: read the delivery obligation %s: %w", id, err)
		}
		if held {
			if owedDeliveryRecord(payload) {
				owed = append(owed, body)
			}
			continue
		}
		staged = true
		payload, err = json.Marshal(hostDeliveryRecord{
			ID:      id,
			Content: body.content,
			Lineage: lineage,
			Target:  addr,
			Status:  hostDeliveryOwed,
			HeldAt:  time.Now(),
		})
		if err != nil {
			return nil, fmt.Errorf("tagent: the delivery obligation %s does not marshal: %w", id, err)
		}
		if err := log.Put(ctx, key, payload); err != nil {
			return nil, fmt.Errorf("tagent: the delivery obligation %s was refused: %w", id, err)
		}
		owed = append(owed, body)
	}
	if staged {
		if err := log.Sync(ctx); err != nil {
			return nil, fmt.Errorf("tagent: the delivery obligations of %s left no write barrier: %w", addr.SessionID, err)
		}
	}
	if len(owed) == 0 {
		return nil, nil
	}
	return owed, nil
}

// owedDeliveryRecord reports whether the durable record of one delivery
// obligation still stands owed: a body the account registered and no send has
// discharged. Bytes the record cannot be read out of stand for no obligation
// here, because this call only decides whether to send again; the boot that
// meets the same bytes refuses to start beside them by name instead of guessing
// a disposition the record does not state.
func owedDeliveryRecord(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	var record hostDeliveryRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return false
	}
	return record.Status == hostDeliveryOwed
}

// handoffSettledBodies registers the delivery obligation of the bodies a landed
// follow-up turn generated and sends every obligation that still stands owed
// through the review ruling the live path applies. The settle driver calls it
// before it consumes the mailbox entry that turn answered: the receipt forbids
// every replay of the notification, so the bodies of a turn whose process stopped
// between its terminal fact and its send are carried by the account instead of by
// a turn that never runs again.
//
// The send face is the whole owed set, not only the bodies this call registered:
// an obligation an earlier registration left owed — a batch whose later record
// the account refused, a sibling process that stopped after its record — is
// redriven here instead of being stranded in this process until the next boot,
// and an obligation a restart already delivered, parked or discarded carries a
// later state and is left alone. A redrive that lands beside the boot's own send
// of the same body is the one extra copy an unconfirmed receipt tolerates; both
// write the same receipt, and the record it leaves is what stops a third. An
// obligation the account refuses is reported to the driver, which keeps the entry
// owed instead of closing it.
func (h *Host) handoffSettledBodies(ctx context.Context, addr event.Address, notice task.Notice) error {
	if h == nil || h.run == nil {
		return nil
	}
	bodies := generatedBodiesOf(h.run.SessionFacts(addr), noticeInvocation(notice))
	owed, err := h.recordDeliveryOwed(ctx, addr, deliveryLineageOf("task"), bodies)
	if err != nil {
		return err
	}
	for _, body := range owed {
		obligation, _ := freshObligationOf(body)
		delivery := plugin.Delivery{Content: body.content, Lineage: deliveryLineageOf("task"), Target: addr}
		if _, cause := h.deliverObligation(ctx, obligation, h.sender, delivery); cause != "" {
			assemblyDebug("tagent: the hand-off delivery to %s stands owed: %s", addr.SessionID, cause)
		}
	}
	return nil
}

// restoreHeldDelivery returns one held body to the waiting table after a
// decision could not be carried out, so a later approval can resolve it, and
// records the obligation beside the state it stands in. The table entry is
// published before the record is attempted: an obligation the account does not
// hold still waits in front of the caller.
func (h *Host) restoreHeldDelivery(ctx context.Context, held *heldDelivery, status string) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return fmt.Errorf("%w: the held delivery of %s", ErrHostClosed, held.id)
	}
	if h.pendingDeliveries == nil {
		h.pendingDeliveries = map[string]*heldDelivery{}
	}
	h.pendingDeliveries[held.id] = held
	h.mu.Unlock()
	return h.recordDeliveryState(ctx, held, status)
}

// holdDelivery parks one body the host owes a target: the obligation keeps the
// identity its generation registered, or takes a host-local one beside a body no
// generation named, and the durable record beside the table entry carries the
// state the hold stands in. The caller delivers nothing.
//
// A record the tier refused is reported to the caller beside the published
// entry: the body stays visible to a decision, and the account stays behind it.
func (h *Host) holdDelivery(ctx context.Context, obligation string, delivery plugin.Delivery, reason, status string) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		assemblyWarn("tagent: the held delivery to %s was dropped beside a closed host", delivery.Target.SessionID)
		return fmt.Errorf("%w: the held delivery to %s", ErrHostClosed, delivery.Target.SessionID)
	}
	if h.pendingDeliveries == nil {
		h.pendingDeliveries = map[string]*heldDelivery{}
	}
	id := obligation
	if id == "" {
		h.heldSeq++
		id = fmt.Sprintf("delivery-%06d", h.heldSeq)
	}
	held := &heldDelivery{id: id, delivery: delivery, heldAt: time.Now()}
	h.pendingDeliveries[id] = held
	h.mu.Unlock()
	assemblyInfo("tagent: the delivery to %s was held: %s", delivery.Target.SessionID, reason)
	if err := h.recordDeliveryState(ctx, held, status); err != nil {
		return fmt.Errorf("tagent: the hold of %s left no record: %w", id, err)
	}
	return nil
}

// restorePendingDeliveries rebuilds the review table from the durable delivery
// records of the storage tier: every obligation still owed stands restored
// under the identity and hold moment it was recorded with, and the sequence
// behind the identities continues past the restored ones. A body the account
// holds as owed — generated and never sent — is sent here through the review
// ruling the live path applies, so a restart delivers what a stop left behind
// instead of dropping it; an owed body whose target is not the entry address is
// retired without a send instead, because a headless address's answer belongs to
// the caller that drove it and never to a message channel. A record that names no
// identity, or a body that does not unmarshal, refuses the boot by name: the host
// never starts beside an obligation it cannot honor.
func (h *Host) restorePendingDeliveries(ctx context.Context) error {
	log := h.deliveryDurable()
	if log == nil {
		return nil
	}
	records, err := log.Scan(ctx, hostDeliveryPrefix)
	if err != nil {
		return fmt.Errorf("tagent: read the delivery records: %w", err)
	}
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	unattended := make([]hostDeliveryRecord, 0, len(keys))
	for _, key := range keys {
		var record hostDeliveryRecord
		if err := json.Unmarshal(records[key], &record); err != nil {
			return fmt.Errorf("tagent: the delivery record %s does not unmarshal: %w", key, err)
		}
		if record.ID == "" {
			return fmt.Errorf("tagent: the delivery record %s names no identity", key)
		}
		if seq := deliveryHeldSeq(record.ID); seq > h.heldSeq {
			h.heldSeq = seq
		}
		switch record.Status {
		case hostDeliveryOwed:
			if record.Target.AgentID != h.entry {
				h.retireHeadlessDelivery(ctx, record)
				continue
			}
			unattended = append(unattended, record)
			continue
		case hostDeliveryHeld, hostDeliveryRetry:
		default:
			continue
		}
		held := &heldDelivery{
			id:       record.ID,
			delivery: plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target},
			heldAt:   record.HeldAt,
		}
		h.mu.Lock()
		if h.pendingDeliveries == nil {
			h.pendingDeliveries = map[string]*heldDelivery{}
		}
		h.pendingDeliveries[held.id] = held
		h.mu.Unlock()
		assemblyInfo("tagent: the delivery %s stands owed beside the restart: %s", record.ID, record.Status)
	}
	for _, record := range unattended {
		delivery := plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target}
		if _, cause := h.deliverObligation(ctx, record.ID, h.sender, delivery); cause != "" {
			assemblyWarn("tagent: the owed delivery of %s still stands owed: %s", record.ID, cause)
			continue
		}
		assemblyInfo("tagent: the owed delivery of %s was delivered beside the restart", record.ID)
	}
	return nil
}

// retireHeadlessDelivery gives up the owed record of a body generated at an
// address that is not the entry: the host answers a headless address for the
// caller that drove it and never pushes its body to a message channel, so an
// obligation another generation registered there is closed under its own state
// instead of being delivered beside the restart. The record keeps the body, so a
// reader sees what was given up and why. A record the tier refuses is warned and
// left owed: nothing is sent here, and the next boot retires it again.
func (h *Host) retireHeadlessDelivery(ctx context.Context, record hostDeliveryRecord) {
	held := &heldDelivery{
		id:       record.ID,
		delivery: plugin.Delivery{Content: record.Content, Lineage: record.Lineage, Target: record.Target},
		heldAt:   record.HeldAt,
	}
	if err := h.recordDeliveryState(ctx, held, hostDeliveryHeadless); err != nil {
		assemblyWarn("tagent: the owed delivery of %s at the headless address %s was not retired: %v", record.ID, record.Target.AgentID, err)
		return
	}
	assemblyInfo("tagent: the owed delivery of %s at the headless address %s was retired without a send", record.ID, record.Target.AgentID)
}

// deliveryHeldSeq reports the order number inside one delivery identity, and
// zero beside an identity that carries none.
func deliveryHeldSeq(id string) uint64 {
	seq, err := strconv.ParseUint(strings.TrimPrefix(id, "delivery-"), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

// deliveryOutcome is the terminal state of one reviewed delivery: the sender
// accepted the body, the review parked it, or the channel refused it.
type deliveryOutcome int

const (
	// deliverySent reports a body the sender accepted.
	deliverySent deliveryOutcome = iota
	// deliveryHeld reports a body the review parked before the sender ran.
	deliveryHeld
	// deliveryRefused reports a body the sender refused.
	deliveryRefused
)

// hostDeliverFunc is the one delivery outlet of the host: every outbound body —
// the answer of a turn, the notice of a failed turn and the body a declared send
// tool delivers — reaches the sender through it.
type hostDeliverFunc func(ctx context.Context, delivery plugin.Delivery) (deliveryOutcome, string)

// deliverOne sends one outbound body that no generation registered an obligation
// for, under the identity the delivery takes when it is parked.
func (h *Host) deliverOne(ctx context.Context, sender MessageSender, delivery plugin.Delivery) (deliveryOutcome, string) {
	return h.deliverObligation(ctx, "", sender, delivery)
}

// deliverObligation sends one outbound body through the review plugin's ruling
// under the identity of the obligation it discharges: an allowed or recorded
// delivery is sent, a hold parks the body under that same obligation, and a
// ruling that could not be rendered parks it too instead of delivering
// unreviewed content. A send the channel refuses parks the body as the
// redelivery it owes. The second result names the cause of a hold or a refusal
// for the caller's receipt; a delivered body carries none beside the
// unconfirmed-receipt note below.
//
// An obligation the sender accepted is recorded as sent behind its write
// barrier: a receipt the tier refused leaves the obligation owed, so a restart
// delivers the body once more rather than reading an unconfirmed send as
// discharged. That unconfirmed receipt is named in the second result beside the
// landed outcome, so a caller learns the body reached the channel and the account
// did not — the same note a hold carries when its own record did not land.
func (h *Host) deliverObligation(ctx context.Context, obligation string, sender MessageSender, delivery plugin.Delivery) (deliveryOutcome, string) {
	if sender == nil {
		return deliveryRefused, "the host carries no outbound message sender"
	}
	if ruler := h.deliveryRuler; ruler != nil {
		disposition, err := ruler.Rule(ctx, delivery)
		switch {
		case err != nil:
			assemblyWarn("tagent: the review of the delivery to %s failed: %v", delivery.Target.SessionID, err)
			cause := "the delivery review failed: " + err.Error()
			return deliveryHeld, cause + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, cause, hostDeliveryHeld))
		case disposition == plugin.DispositionHold:
			cause := "the review held the delivery"
			return deliveryHeld, cause + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, cause, hostDeliveryHeld))
		case disposition == plugin.DispositionAllow, disposition == plugin.DispositionRecord:
		default:
			assemblyWarn("tagent: the review of the delivery to %s returned %q", delivery.Target.SessionID, disposition)
			cause := fmt.Sprintf("the delivery review returned %q", disposition)
			return deliveryHeld, cause + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, cause, hostDeliveryHeld))
		}
	}
	if err := sender.Send(ctx, delivery.Target, delivery.Content); err != nil {
		assemblyWarn("tagent: the host delivery to %s was refused: %v", delivery.Target.SessionID, err)
		reason := "the channel refused the delivery: " + err.Error()
		return deliveryRefused, err.Error() + holdRecordNote(h.holdDelivery(ctx, obligation, delivery, reason, hostDeliveryRetry))
	}
	if obligation != "" {
		held := &heldDelivery{id: obligation, delivery: delivery, heldAt: time.Now()}
		if err := h.recordDeliveryState(ctx, held, hostDeliverySent); err != nil {
			assemblyWarn("tagent: the delivery of %s landed but its receipt did not: %v", obligation, err)
			return deliverySent, sentRecordNote(err)
		}
	}
	return deliverySent, ""
}

// sentRecordNote renders the receipt note one landed send adds when its record
// did not reach the account: the body was accepted by the channel and the
// obligation still stands owed, so a restart delivers it once more. The note is
// the counterpart of holdRecordNote, so a caller reads an unconfirmed receipt the
// same way it reads an unrecorded hold.
func sentRecordNote(err error) string {
	if err == nil {
		return ""
	}
	return "the delivery landed but its receipt did not: " + err.Error()
}

// holdRecordNote renders the receipt note one hold adds when its record did not
// land, and nothing beside a hold the account accepted.
func holdRecordNote(err error) string {
	if err == nil {
		return ""
	}
	return "; " + err.Error()
}

// deliveryLineageKey is the context key one running turn's trigger-source
// stamp is carried under, from the drive down to the send tool.
type deliveryLineageKey struct{}

// withDeliveryLineage stamps the trigger-source line of the running turn on the
// context the drive hands to its calls, so a cross-agent body names the line its
// turn names instead of inventing one.
func withDeliveryLineage(ctx context.Context, lineage string) context.Context {
	return context.WithValue(ctx, deliveryLineageKey{}, lineage)
}

// deliveryLineageFrom reports the line the running turn stamped and whether one
// is set.
func deliveryLineageFrom(ctx context.Context) (string, bool) {
	lineage, ok := ctx.Value(deliveryLineageKey{}).(string)
	if !ok || lineage == "" {
		return "", false
	}
	return lineage, true
}

// deliveryLineageOf reports the lineage one turn body carries: the trigger
// source the invocation stated, or the user stamp a user-address turn carries
// when the invocation names none.
func deliveryLineageOf(triggerSource string) string {
	if stamp := strings.TrimSpace(triggerSource); stamp != "" {
		return stamp
	}
	return "user"
}

// noteDeliveryRefusal commits the audit fact of one refused delivery: a
// feedback fact owned by the target address, carrying the hold identity and the
// delivery dimensions in its metadata and no model payload in its message, so
// the fact enters no model request. A refused commit answers the caller by name:
// a refusal is a decision the account states, not one the log states for it.
func (h *Host) noteDeliveryRefusal(ctx context.Context, held *heldDelivery) error {
	if h == nil || h.run == nil {
		return nil
	}
	if _, err := h.run.CommitToSession(ctx, held.delivery.Target, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource:          "host",
			event.MetaKeyEventSummary:           "delivery refused",
			event.MetaPrefix + "delivery_id":    held.id,
			event.MetaPrefix + "lineage":        held.delivery.Lineage,
			event.MetaPrefix + "target_agent":   string(held.delivery.Target.AgentID),
			event.MetaPrefix + "target_session": string(held.delivery.Target.SessionID),
			event.MetaPrefix + "content_bytes":  strconv.Itoa(len(held.delivery.Content)),
		},
	}); err != nil {
		return fmt.Errorf("tagent: the refusal of the held delivery %s left no fact: %w", held.id, err)
	}
	return nil
}

// failureNoticeText renders the body one failed turn delivers: the raw cause as
// the reduction recorded it, bounded to failureNoticeMaxBytes at a rune boundary
// and marked when it was cut. It never invents a reason: an empty cause answers
// an empty body the caller does not send.
func failureNoticeText(cause string) string {
	text := strings.TrimSpace(cause)
	if text == "" || len(text) <= failureNoticeMaxBytes {
		return text
	}
	cut := failureNoticeMaxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + failureNoticeTruncated
}

// noteFailureNotice commits the audit fact of one delivered failure notice: a feedback
// fact owned by the address, carrying the raw cause and the terminal in its metadata and
// no model payload in its message.
// - No message content: the fact maps onto no projection entry and enters no model request.
// - No notice kind and no task id: the notice consumers drive follow-up turns from those keys.
// - A refused commit is a warning beside the delivery, never a reason to withhold the notice.
func (h *Host) noteFailureNotice(ctx context.Context, addr event.Address, turn agent.Turn) {
	if h == nil || h.run == nil {
		return
	}
	if _, err := h.run.CommitToSession(ctx, addr, event.Event{
		Type:      event.TypeFeedback,
		Timestamp: time.Now(),
		Meta: map[string]string{
			event.MetaKeyTriggerSource:          "host",
			event.MetaKeyEventSummary:           failureNoticeSummaryOf(turn.Failure),
			event.MetaPrefix + "terminal":       turn.Terminal.String(),
			event.MetaPrefix + "failure_reason": turn.Failure,
		},
		InvocationID: turn.InvocationID,
	}); err != nil {
		assemblyWarn("tagent: the failure notice of %s left no fact: %v", addr.SessionID, err)
	}
}

// failureNoticeSummaryOf renders the audit line of one failure notice: the
// summary prefix beside the first line of the cause, bounded by the same rune
// bound the projection summary uses. The durable body keeps this line alone, so
// the bound decides how much of the cause a later reader finds there.
func failureNoticeSummaryOf(cause string) string {
	line := cause
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	line = strings.TrimSpace(line)
	summary := failureNoticeSummary
	if line != "" {
		summary += ": " + line
	}
	runes := []rune(summary)
	if len(runes) <= mappedSummaryRunes {
		return summary
	}
	return string(runes[:mappedSummaryRunes]) + "…"
}

// freshBody is one assistant body a drive generated beside the identity of the
// fact that carries it: the identity names the delivery obligation of that body,
// so the registration beside the generation and the hand-off a restart reads
// back name one record instead of two.
type freshBody struct {
	// fact is the key of the committed fact the body was generated in.
	fact event.EventID
	// content is the trimmed body the fact carries.
	content string
}

// assistantBodyOf reports the plain assistant body one committed fact carries:
// the turn-end answer of a drive, never a tool call and never an empty body.
func assistantBodyOf(evt event.Event) (freshBody, bool) {
	msg := evt.Message
	if msg.Role != model.RoleAssistant || len(msg.ToolCalls) > 0 {
		return freshBody{}, false
	}
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return freshBody{}, false
	}
	return freshBody{fact: evt.Key, content: content}, true
}

// freshAssistantOf reports the plain assistant bodies one session's committed
// facts carry past the baseline: the bodies the invocation that just ended
// produced, and nothing an earlier delivery already carried.
func freshAssistantOf(facts []event.Event, baseline int) []freshBody {
	if baseline > len(facts) {
		baseline = len(facts)
	}
	bodies := make([]freshBody, 0, 4)
	for _, evt := range facts[baseline:] {
		if body, ok := assistantBodyOf(evt); ok {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

// generatedBodiesOf reports the plain assistant bodies one invocation generated:
// the bodies the follow-up turn of a notification committed under its own
// four-part identity, read back from the chain a restart rebuilt. A drive names
// its facts by invocation identity, so the bodies of the turn are recoverable
// after the process that generated them is gone.
func generatedBodiesOf(facts []event.Event, invocation event.InvocationID) []freshBody {
	if invocation == "" {
		return nil
	}
	bodies := make([]freshBody, 0, 2)
	for _, evt := range facts {
		if evt.InvocationID != invocation {
			continue
		}
		if body, ok := assistantBodyOf(evt); ok {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

// freshObligationOf reports the delivery obligation identity of one generated
// body and whether the body carries one at all: a fact committed without a key
// names no obligation, and its body is delivered without a durable record.
func freshObligationOf(body freshBody) (string, bool) {
	if body.fact == "" {
		return "", false
	}
	return hostDeliveryFreshPrefix + string(body.fact), true
}

// hostDocument is the host document dialect one Boot call decodes: the
// declaration-shaped sections the core shares beside the provider registry, the
// governance section and the application section a deployment owns. The
// governance and application sections are carried opaquely, so the core reads no
// application concept out of them.
type hostDocument struct {
	Entry          string                          `yaml:"entry"`
	Provider       string                          `yaml:"provider"`
	Model          string                          `yaml:"model"`
	PromptDir      string                          `yaml:"prompt_dir"`
	Providers      map[string]hostProvider         `yaml:"providers"`
	Agents         map[string]hostAgent            `yaml:"agents"`
	Storage        hostStorage                     `yaml:"storage"`
	TimeoutSeconds int                             `yaml:"request_timeout_seconds"`
	MCPServers     map[string]config.MCPServerSpec `yaml:"mcp_servers"`
	Governance     map[string]any                  `yaml:"governance"`
	App            map[string]any                  `yaml:"app"`
	Telemetry      config.TelemetrySpec            `yaml:"telemetry"`

	once      sync.Once
	mapped    document.Document
	mappedErr error
}

// hostStorage is the top-level storage section of a host document.
type hostStorage struct {
	Type string `yaml:"type"`
	Root string `yaml:"root"`
}

// hostProvider is one entry of the provider registry: the protocol the
// connection speaks, the endpoint it reaches and the environment variable
// holding its key.
type hostProvider struct {
	Provider    string `yaml:"provider"`
	APIEndpoint string `yaml:"api_endpoint"`
	APIKeyEnv   string `yaml:"api_key_env"`
}

// hostAgent is one agent section of a host document.
type hostAgent struct {
	Provider     string                `yaml:"provider"`
	Model        string                `yaml:"model"`
	SystemPrompt hostPrompt            `yaml:"system_prompt"`
	Tools        []hostToolRef         `yaml:"tools"`
	SkillDirs    []string              `yaml:"skill_dirs"`
	ReadRoot     string                `yaml:"read_root"`
	FinalRound   *hostFinalRound       `yaml:"final_round"`
	Memory       config.MemorySpec     `yaml:"memory"`
	Meditation   config.MeditationSpec `yaml:"meditation"`
	File         config.FileSpec       `yaml:"file"`
	Exec         config.ExecSpec       `yaml:"exec"`

	Temperature       *float64 `yaml:"temperature"`
	ThinkingEnabled   *bool    `yaml:"thinking_enabled"`
	ReasoningEffort   string   `yaml:"reasoning_effort"`
	MaxToolIterations int      `yaml:"max_tool_iterations"`
	MaxTokens         int      `yaml:"max_tokens"`
	CompressThreshold float64  `yaml:"compress_threshold"`
	KeepRecentTasks   int      `yaml:"keep_recent_tasks"`
}

// hostPrompt is the prompt section of one agent: the declared files resolved
// beside the document's prompt directory.
type hostPrompt struct {
	Files []string `yaml:"files"`
}

// hostFinalRound is the watchdog budget one agent declares.
type hostFinalRound struct {
	MaxRounds      int     `yaml:"max_rounds"`
	BudgetSeconds  float64 `yaml:"budget_seconds"`
	TimeoutSeconds float64 `yaml:"timeout_seconds"`
	Prompt         string  `yaml:"prompt"`
}

// hostToolRef is one tool reference of an agent declaration: a tool instance
// the host contributes, or a delegation to another declared agent.
type hostToolRef struct {
	Kind            string   `yaml:"kind"`
	ID              string   `yaml:"id"`
	Agent           string   `yaml:"agent"`
	DescriptionFile string   `yaml:"description_file"`
	EventParams     []string `yaml:"event_params"`
}

// loadHostDocument reads and strictly decodes the configuration source one Boot
// call is started with; an unknown key stops the startup instead of being read
// past.
func loadHostDocument(path string) (*hostDocument, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrHostConfigMissing
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tagent: read %s: %w", path, err)
	}
	doc := &hostDocument{}
	if err := config.DecodeStrict(path, raw, doc); err != nil {
		return nil, fmt.Errorf("tagent: %w", err)
	}
	if strings.TrimSpace(doc.PromptDir) == "" {
		doc.PromptDir = "."
	}
	return doc, nil
}

// document maps the host sections onto the declaration-shaped section the core
// shares, once per document.
func (d *hostDocument) document() (document.Document, error) {
	d.once.Do(func() { d.mapped, d.mappedErr = d.mapDocument() })
	return d.mapped, d.mappedErr
}

// Declaration returns the organization declaration the host assembles. The
// telemetry section stays the host's own dialect: the core reads no observation
// concept out of the declaration, so the section never enters it.
func (d *hostDocument) Declaration() (*config.Declaration, error) {
	mapped, err := d.document()
	if err != nil {
		return nil, err
	}
	decl, err := mapped.Declaration()
	if err != nil {
		return nil, err
	}
	return decl, nil
}

// mapDocument builds the declaration-shaped document of the host.
func (d *hostDocument) mapDocument() (document.Document, error) {
	agents := make(map[string]config.AgentSpec, len(d.Agents))
	for id, entry := range d.Agents {
		finalRound := entry.finalRound()
		if finalRound.MaxRounds == 0 && entry.MaxToolIterations > 0 {
			finalRound.MaxRounds = entry.MaxToolIterations
		}
		spec := config.AgentSpec{
			ID:                id,
			Model:             d.modelName(id),
			FinalRound:        finalRound,
			Temperature:       entry.Temperature,
			ThinkingEnabled:   entry.ThinkingEnabled,
			ReasoningEffort:   entry.ReasoningEffort,
			MaxToolIterations: entry.MaxToolIterations,
			MaxTokens:         entry.MaxTokens,
			CompressThreshold: entry.CompressThreshold,
			KeepRecentTasks:   entry.KeepRecentTasks,
			Memory:            entry.Memory,
			Meditation:        entry.Meditation,
			File:              entry.File,
			Exec:              entry.Exec,
		}
		files, err := d.promptFiles(id)
		if err != nil {
			return document.Document{}, err
		}
		spec.Files = files
		refs, err := d.toolRefs(id)
		if err != nil {
			return document.Document{}, err
		}
		spec.Tools = refs
		agents[id] = spec
	}
	return document.Document{Entry: d.Entry, Agents: agents, Servers: d.MCPServers}, nil
}

// ErrUnknownAgentReference reports an agent reference that names no declared
// agent.
var ErrUnknownAgentReference = errors.New("tagent: the agent reference names no declared agent")

// ErrUnknownHostTool reports a declared tool this host does not contribute.
var ErrUnknownHostTool = errors.New("tagent: the declared tool is not contributed by this host")

// toolRefs resolves the tool references one declared agent carries.
func (d *hostDocument) toolRefs(id string) ([]config.ToolRef, error) {
	refs := make([]config.ToolRef, 0, len(d.Agents[id].Tools))
	for _, ref := range d.Agents[id].Tools {
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" && strings.TrimSpace(ref.Agent) != "" {
			kind = "agent"
		}
		switch kind {
		case "tool":
			if !hostContributes(ref.ID) {
				return nil, fmt.Errorf("%w: %q declared by %q", ErrUnknownHostTool, ref.ID, id)
			}
			refs = append(refs, config.ToolRef{Name: ref.ID})
		case "agent":
			target := strings.TrimSpace(ref.Agent)
			if _, ok := d.Agents[target]; target == "" || !ok {
				return nil, fmt.Errorf("%w: %q referenced by %q", ErrUnknownAgentReference, target, id)
			}
			params := map[string]string{subagent.ParamAgent: target}
			if file := strings.TrimSpace(ref.DescriptionFile); file != "" {
				params[subagent.ParamDescriptionFile] = filepath.Join(d.PromptDir, file)
			}
			if len(ref.EventParams) > 0 {
				params[subagent.ParamEventParams] = strings.Join(ref.EventParams, ",")
			}
			name := target
			if declared := strings.TrimSpace(ref.ID); declared != "" {
				name = declared
			}
			refs = append(refs, config.ToolRef{Name: name, Params: params})
		default:
			return nil, fmt.Errorf("tagent: agent %q declares tool kind %q, which this host does not wire", id, ref.Kind)
		}
	}
	return refs, nil
}

// hostToolIDs lists the tool ids the host assembly accepts in a declaration:
// the instances the host contributes beside the presets the core assembles.
var hostToolIDs = append(append([]string{
	hostSendToolID,
	meditation.DeliverSummaryToolName,
	recall.QueryToolName,
	recall.GetToolName,
	recall.RecentToolName,
	recall.TraceToolName,
	knowledge.SkillSearchToolName,
	knowledge.SkillLoadToolName,
	spec.ToolName,
}, tagentfile.Names()...),
	tagentexec.PresetName,
	subagent.CallSubagentRef,
	mcpgw.ToolNameMCPCall,
	mcpgw.ToolNameMCPDiscover,
)

// hostContributes reports whether the host assembly accepts one tool id.
func hostContributes(id string) bool {
	for _, name := range hostToolIDs {
		if name == id {
			return true
		}
	}
	return false
}

// finalRound reports the watchdog budget of one agent section.
func (a hostAgent) finalRound() config.FinalRoundSpec {
	if a.FinalRound == nil {
		return config.FinalRoundSpec{}
	}
	return config.FinalRoundSpec{
		MaxRounds:      a.FinalRound.MaxRounds,
		BudgetSeconds:  a.FinalRound.BudgetSeconds,
		TimeoutSeconds: a.FinalRound.TimeoutSeconds,
		Prompt:         a.FinalRound.Prompt,
	}
}

// promptFiles reads the prompt files one declared agent is assembled from.
func (d *hostDocument) promptFiles(id string) ([]config.FileRef, error) {
	files, absent, err := document.PromptFiles(d.PromptDir, d.Agents[id].SystemPrompt.Files)
	if err != nil {
		return nil, fmt.Errorf("tagent: %w", err)
	}
	for _, path := range absent {
		assemblyInfo("tagent: the declared prompt file is absent and skipped: %s", path)
	}
	return files, nil
}

// modelName reports the model one agent runs against, and the document default
// beside no agent-level declaration.
func (d *hostDocument) modelName(id string) string {
	if name := strings.TrimSpace(d.Agents[id].Model); name != "" {
		return name
	}
	return strings.TrimSpace(d.Model)
}

// declaresSendOutside reports whether an agent section other than except
// declares the send tool. The meditation session never carries the host's
// channel face, so its declaration neither needs nor demands a sender.
func (d *hostDocument) declaresSendOutside(except string) bool {
	for id := range d.Agents {
		if except != "" && id == except {
			continue
		}
		if d.declaresTool(id, hostSendToolID) {
			return true
		}
	}
	return false
}

// validateMeditationTools refuses a document that grants a meditation tool to
// the wrong agent: deliver_summary belongs to the meditation session alone, and
// the send tool is a host channel face the meditation session never carries.
func (d *hostDocument) validateMeditationTools(plan *hostMeditationPlan) error {
	session := meditationAgentOf(plan)
	for id := range d.Agents {
		if d.declaresTool(id, meditation.DeliverSummaryToolName) && id != session {
			return fmt.Errorf("%w: agent %q declares %q, which only the meditation session carries",
				ErrHostMeditationToolMisplaced, id, meditation.DeliverSummaryToolName)
		}
		if id == session && d.declaresTool(id, hostSendToolID) {
			return fmt.Errorf("%w: the meditation session %q declares %q",
				ErrHostMeditationToolMisplaced, id, hostSendToolID)
		}
	}
	return nil
}

// meditationAgentOf reports the observing agent of one resolved plan, and an
// empty name beside no plan.
func meditationAgentOf(plan *hostMeditationPlan) string {
	if plan == nil {
		return ""
	}
	return plan.agent
}

// riskRules reads the declared risk table of the governance section: the
// ordered entries the boot opens its policy over. A section beside no table
// declares no rule, so a document that never named one keeps the behavior it
// had. Every entry names a tool (or a prefix) and one declared grade; a key, a
// value or a grade the table does not name is refused at boot instead of being
// read past.
func (d *hostDocument) riskRules() ([]governance.ToolRiskRule, error) {
	raw, held := d.Governance["risk_rules"]
	if !held || raw == nil {
		return nil, nil
	}
	entries, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("tagent: governance risk_rules is not a list")
	}
	rules := make([]governance.ToolRiskRule, 0, len(entries))
	for i, entry := range entries {
		fields, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tagent: governance risk_rules[%d] is not a mapping", i)
		}
		var rule governance.ToolRiskRule
		for key, value := range fields {
			switch key {
			case "tool":
				text, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("tagent: governance risk_rules[%d].tool is not a string", i)
				}
				rule.Tool = strings.TrimSpace(text)
			case "prefix":
				flag, ok := value.(bool)
				if !ok {
					return nil, fmt.Errorf("tagent: governance risk_rules[%d].prefix is not a bool", i)
				}
				rule.Prefix = flag
			case "level":
				text, ok := value.(string)
				if !ok {
					return nil, fmt.Errorf("tagent: governance risk_rules[%d].level is not a string", i)
				}
				rule.Level = plugin.RiskLevel(strings.TrimSpace(text))
			default:
				return nil, fmt.Errorf("tagent: governance risk_rules[%d] names the unknown key %q", i, key)
			}
		}
		if rule.Tool == "" {
			return nil, fmt.Errorf("tagent: governance risk_rules[%d] names no tool", i)
		}
		if rule.Level.Rank() == 0 {
			return nil, fmt.Errorf("tagent: governance risk_rules[%d] (%s) carries the unknown risk grade %q", i, rule.Tool, rule.Level)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// declaresTool reports whether one agent section declares one host tool.
func (d *hostDocument) declaresTool(id, name string) bool {
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind == "tool" && ref.ID == name {
			return true
		}
	}
	return false
}

// nativeAgents constructs the live pieces every declared agent is assembled
// with.
func (d *hostDocument) nativeAgents(ctx context.Context, client *http.Client, sender MessageSender, deliver hostDeliverFunc, recallPort *RecallPort, meditationPlan *hostMeditationPlan, meditationPort *hostMeditationPort) (map[event.AgentID]NativeAgent, error) {
	decl, err := d.Declaration()
	if err != nil {
		return nil, err
	}
	out := make(map[event.AgentID]NativeAgent, len(decl.Agents))
	for _, spec := range decl.Agents {
		native, err := d.modelFor(spec.ID, client)
		if err != nil {
			return nil, err
		}
		tools, err := d.toolsFor(ctx, spec.ID, sender, deliver, recallPort, meditationPlan, meditationPort)
		if err != nil {
			return nil, err
		}
		out[event.AgentID(spec.ID)] = NativeAgent{Model: native, Tools: tools}
	}
	return out, nil
}

// toolsFor constructs the tool instances the host contributes for one agent;
// the core owns the execution, MCP and delegation families of the declaration.
// The recall set is built before the run is assembled, so its observation table
// and original store are wired to the port itself (it forwards to the faces Bind
// attaches) and the relation ledger comes from the port's read face, so
// recall_trace walks the host's parent chain.
func (d *hostDocument) toolsFor(ctx context.Context, id string, sender MessageSender, deliver hostDeliverFunc, recallPort *RecallPort, meditationPlan *hostMeditationPlan, meditationPort *hostMeditationPort) ([]trpctool.CallableTool, error) {
	available := make(map[string]trpctool.CallableTool, len(hostToolIDs))
	if id != meditationAgentOf(meditationPlan) && d.declaresTool(id, hostSendToolID) {
		available[hostSendToolID] = &hostSendTool{sender: sender, deliver: deliver}
	}
	if id == meditationAgentOf(meditationPlan) && d.declaresTool(id, meditation.DeliverSummaryToolName) {
		available[meditation.DeliverSummaryToolName] = meditation.NewDeliverSummaryTool(meditationPlan.targets, meditationPort, meditationPort)
	}
	if d.declaresRecall(id) {
		reads := recallPort.Reads()
		reads.Table = recallPort
		reads.Originals = recallPort
		for _, candidate := range recall.New(reads, memory.PartitionsOf(id, d.Agents[id].Memory.ReadNamespaces)) {
			available[candidate.Declaration().Name] = candidate
		}
	}
	if d.declaresKnowledge(id) {
		surface, err := d.knowledgeTools(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, candidate := range surface {
			available[candidate.Declaration().Name] = candidate
		}
	}
	planRoot := ""
	if d.declaresTool(id, spec.ToolName) {
		backend := d.planBackend()
		available[spec.ToolName] = spec.NewTool(backend)
		planRoot = backend.WorkspaceRoot()
		if err := os.MkdirAll(planRoot, 0o755); err != nil {
			return nil, fmt.Errorf("tagent: create the plan workspace %s: %w", planRoot, err)
		}
	}
	if d.declaresFile(id) {
		surface, err := d.planFileTools(ctx, id, planRoot)
		if err != nil {
			return nil, err
		}
		for _, candidate := range surface {
			available[candidate.Declaration().Name] = candidate
		}
	}
	tools := make([]trpctool.CallableTool, 0, len(available))
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind != "tool" {
			continue
		}
		if tool, ok := available[ref.ID]; ok {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

// planBackend reports the plan backend the plan agent's work is written
// through.
func (d *hostDocument) planBackend() spec.Backend {
	return spec.NewOpenSpecBackend()
}

// planFileTools builds the file surface one agent's declaration grants. The
// write root is the plan workspace when the agent declares the plan tool, and
// the declared base directory otherwise.
func (d *hostDocument) planFileTools(ctx context.Context, id, writeRoot string) ([]trpctool.CallableTool, error) {
	granted := make([]string, 0, len(tagentfile.Names()))
	for _, ref := range d.Agents[id].Tools {
		if ref.Kind != "tool" {
			continue
		}
		for _, name := range tagentfile.Names() {
			if ref.ID == name {
				granted = append(granted, name)
			}
		}
	}
	if len(granted) == 0 {
		return nil, nil
	}
	base := writeRoot
	if base == "" {
		base = d.Agents[id].File.BaseDir
	}
	readRoots, err := hostReadRoots(d.Agents[id].File.ReadRoots)
	if err != nil {
		return nil, fmt.Errorf("tagent: resolve the read roots of agent %q: %w", id, err)
	}
	surface, err := tagentfile.New(tagentfile.Options{BaseDir: base, ReadRoots: readRoots, Tools: granted})
	if err != nil {
		return nil, fmt.Errorf("tagent: construct the file surface of agent %q: %w", id, err)
	}
	tools := make([]trpctool.CallableTool, 0, len(granted))
	for _, candidate := range surface.Tools(ctx) {
		callable, ok := candidate.(trpctool.CallableTool)
		if !ok {
			return nil, fmt.Errorf("tagent: a file tool of agent %q carries no native call surface", id)
		}
		tools = append(tools, callable)
	}
	return tools, nil
}

// hostReadRoots resolves the extra read roots one file declaration names.
func hostReadRoots(declared []string) ([]string, error) {
	roots := make([]string, 0, len(declared))
	for _, root := range declared {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		absolute, err := filepath.Abs(trimmed)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", trimmed, err)
		}
		roots = append(roots, absolute)
	}
	return roots, nil
}

// declaresFile reports whether one agent section declares any file tool.
func (d *hostDocument) declaresFile(id string) bool {
	for _, name := range tagentfile.Names() {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// declaresRecall reports whether one agent section declares any recall tool.
func (d *hostDocument) declaresRecall(id string) bool {
	for _, name := range []string{recall.QueryToolName, recall.GetToolName, recall.RecentToolName, recall.TraceToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// declaresKnowledge reports whether one agent section declares any knowledge
// tool.
func (d *hostDocument) declaresKnowledge(id string) bool {
	for _, name := range []string{knowledge.SkillSearchToolName, knowledge.SkillLoadToolName} {
		if d.declaresTool(id, name) {
			return true
		}
	}
	return false
}

// knowledgeTools constructs the knowledge surface of one declared agent: its
// skill roots, the read root its file reads are confined to and the ledger the
// loaded bodies publish to, so the request boundary carries a loaded body into
// that agent's next request instead of answering a name alone.
func (d *hostDocument) knowledgeTools(ctx context.Context, id string) ([]trpctool.CallableTool, error) {
	opts := knowledge.Options{
		ReadRoot: d.readRoot(id),
		Sink:     NewSkillLoadSink(event.AgentID(id)),
	}
	if roots := d.skillRoots(id); len(roots) > 0 {
		repository, err := skill.NewFSRepository(roots...)
		if err != nil {
			return nil, fmt.Errorf("tagent: open the skill repository of agent %q: %w", id, err)
		}
		opts.Skills = repository
	}
	return knowledge.New(ctx, opts)
}

// skillRoots reports the declared skill roots of one agent section.
func (d *hostDocument) skillRoots(id string) []string {
	roots := make([]string, 0, len(d.Agents[id].SkillDirs))
	for _, dir := range d.Agents[id].SkillDirs {
		if trimmed := strings.TrimSpace(dir); trimmed != "" {
			roots = append(roots, trimmed)
		}
	}
	return roots
}

// readRoot reports the directory the knowledge reads of one agent are confined
// to.
func (d *hostDocument) readRoot(id string) string {
	if root := strings.TrimSpace(d.Agents[id].ReadRoot); root != "" {
		return root
	}
	return "."
}

// requestTimeout reports the HTTP timeout the models of this document run
// under.
func (d *hostDocument) requestTimeout() time.Duration {
	if d.TimeoutSeconds > 0 {
		return time.Duration(d.TimeoutSeconds) * time.Second
	}
	return 10 * time.Minute
}

// modelRef reports the provider registry key and the model name one declared
// agent resolves in this document, before either half is built into a model.
func (d *hostDocument) modelRef(id string) (string, string) {
	providerName := strings.TrimSpace(d.Agents[id].Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(d.Provider)
	}
	return providerName, d.modelName(id)
}

// modelFor constructs the native model of one declared agent from its provider
// entry. Chat telemetry stays on the entry agent's model only.
func (d *hostDocument) modelFor(id string, client *http.Client) (trpcmodel.Model, error) {
	providerName, modelName := d.modelRef(id)
	return d.buildModel(id, providerName, modelName, client)
}

// buildModel constructs one native model from an explicit provider and model
// name: the reentrant step both Boot and a lease rebuild run, so a model
// replacement reads the very construction the first generation read.
func (d *hostDocument) buildModel(id, providerName, modelName string, client *http.Client) (trpcmodel.Model, error) {
	providerName = strings.TrimSpace(providerName)
	provider, ok := d.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("tagent: the host document declares no provider %q", providerName)
	}
	if protocol := strings.TrimSpace(provider.Provider); protocol != "" && protocol != "openai" {
		return nil, fmt.Errorf("tagent: provider %q runs protocol %q, which this host does not wire", providerName, protocol)
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, fmt.Errorf("tagent: agent %q declares no model", id)
	}
	keyEnv := strings.TrimSpace(provider.APIKeyEnv)
	if keyEnv == "" {
		return nil, fmt.Errorf("tagent: provider %q names no api_key_env", providerName)
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return nil, fmt.Errorf("tagent: provider %q requires %s in the environment", providerName, keyEnv)
	}
	endpoint := strings.TrimSpace(provider.APIEndpoint)
	if id == strings.TrimSpace(d.Entry) {
		if override := os.Getenv(hostAPIEndpointEnv); override != "" {
			endpoint = override
		}
	}
	opts := []openai.Option{openai.WithAPIKey(key)}
	if id == strings.TrimSpace(d.Entry) {
		opts = append(opts, openai.WithChatTelemetry(true))
	}
	if endpoint != "" {
		opts = append(opts, openai.WithBaseURL(endpoint))
	}
	if client != nil {
		opts = append(opts, openai.WithOpenAIOptions(option.WithHTTPClient(client)))
	}
	return openai.New(modelName, opts...), nil
}

// hostAPIEndpointEnv names the environment variable that overrides the endpoint
// of the entry agent's provider.
const hostAPIEndpointEnv = "TAGENT_API_ENDPOINT"

// hostSendToolID is the declaration-side name of the outbound message tool.
const hostSendToolID = "send"

// hostSendContentMax is the byte ceiling of one body the send tool delivers.
const hostSendContentMax = 8 * 1024

// hostSendLineage is the delivery lineage of a call that carries no governance
// pass: the body originated in the send tool itself, so the line names the tool
// rather than borrowing the user's line.
const hostSendLineage = "send"

// hostSendTool is the model-visible outbound face: one content argument handed
// to the host's one delivery outlet, which rules it before any body leaves.
type hostSendTool struct {
	sender  MessageSender
	deliver func(context.Context, plugin.Delivery) (deliveryOutcome, string)
}

var _ trpctool.CallableTool = (*hostSendTool)(nil)

// hostSendArgs is the argument shape the model sees: the body, and nothing
// else.
type hostSendArgs struct {
	Content string `json:"content"`
}

// Declaration implements tool.CallableTool.
func (t *hostSendTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        hostSendToolID,
		Description: "Deliver one body to the user address of this call. The target comes from the call's session address; it is not an argument. A refusal returns a [send_denied] result text, and a body held for review returns a [send_held] result text; the turn continues either way.",
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"content": {Type: "string", Description: "The body to deliver, UTF-8, at most 8KiB"},
			},
			Required: []string{"content"},
		},
	}
}

// Call implements tool.CallableTool: every refusal and delivery failure is
// answered as result text, and an error is reserved for a malformed argument
// shape. The body is handed to the host's one delivery outlet, so the same
// ruler rules a cross-agent send and a turn body, and a hold is answered as a
// hold: the receipt must never report a held body as delivered. A delivered
// body is answered with the bare ok: the receipt is the credential that the
// delivery happened, not a report to repeat, so it names no target session and
// carries no wording the model could speak to the user.
func (t *hostSendTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(jsonArgs, &probe); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if unknown := hostUnknownFields(probe); len(unknown) > 0 {
		return t.denied("the arguments name fields outside the declaration " + strings.Join(unknown, ", ") +
			"; send accepts content only, and the target comes from the host"), nil
	}
	var args hostSendArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("send: malformed arguments: %w", err)
	}
	if args.Content == "" {
		return t.denied("content is empty"), nil
	}
	if len(args.Content) > hostSendContentMax {
		return t.denied(fmt.Sprintf("content exceeds the 8KiB ceiling (%d bytes)", len(args.Content))), nil
	}
	if t.sender == nil {
		return t.denied("the host carries no outbound message sender"), nil
	}
	addr, ok := hostCallAddress(ctx)
	if !ok {
		return t.denied("this call carries no explicit session address"), nil
	}
	if t.deliver == nil {
		return t.denied("the host carries no delivery outlet"), nil
	}
	outcome, cause := t.deliver(ctx, plugin.Delivery{
		Content: args.Content,
		Lineage: hostSendLineageOf(ctx),
		Target:  addr,
	})
	switch outcome {
	case deliverySent:
		return "ok", nil
	case deliveryHeld:
		return t.held(cause), nil
	default:
		return t.denied(cause), nil
	}
}

// denied renders one refusal as result text.
func (t *hostSendTool) denied(reason string) string {
	return "[send_denied] " + reason
}

// held renders one hold as result text: the body is waiting for review, not
// delivered, and the text must not read as a delivery receipt.
func (t *hostSendTool) held(reason string) string {
	return "[send_held] " + reason
}

// hostSendLineageOf reports the lineage a send call hands to the delivery
// ruler: the governance pass of the running call when the dispatch stated one,
// the trigger-source stamp of the running turn otherwise, and the send tool's
// own line when the call runs beside neither.
func hostSendLineageOf(ctx context.Context) string {
	if pass, ok := governance.CallPassFrom(ctx); ok {
		if source := strings.TrimSpace(pass.TriggerSource); source != "" {
			return source
		}
	}
	if lineage, ok := deliveryLineageFrom(ctx); ok {
		return lineage
	}
	return hostSendLineage
}

// hostCallAddress reports the explicit invocation address the call frame
// carries.
func hostCallAddress(ctx context.Context) (event.Address, bool) {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok || !frame.Address.IsComplete() {
		return event.Address{}, false
	}
	return frame.Address, true
}

// hostUnknownFields names the argument fields outside the declaration, sorted
// so the text is reproducible.
func hostUnknownFields(probe map[string]json.RawMessage) []string {
	var out []string
	for key := range probe {
		if key != "content" {
			out = append(out, "'"+key+"'")
		}
	}
	sort.Strings(out)
	return out
}

// hostMeditationPlan is the one reflection section a document enables: the
// observing agent, the loop session, the cadence and floor, the static prompt,
// the observed and read namespaces, the declared delivery targets and the
// anchor record the watermark persists through.
type hostMeditationPlan struct {
	agent    string
	session  string
	interval time.Duration
	minGap   time.Duration
	prompt   string
	observed []string
	reads    []string
	targets  []string
	anchors  string
}

// hostMeditationDefaultInterval is the tick cadence of a declaration that names
// none.
const hostMeditationDefaultInterval = 30 * time.Minute

// resolveHostMeditation resolves the one meditation section a document enables:
// the entry agent first and the remaining agents in declaration order, so one
// document resolves to one plan across restarts. A document enabling none
// reports a nil plan, and every inconsistent section is refused by name.
func resolveHostMeditation(doc document.Document, promptDir, storageRoot string) (*hostMeditationPlan, error) {
	var plan *hostMeditationPlan
	declaredBy := ""
	for _, id := range doc.Order() {
		spec, ok := doc.Agents[id]
		if !ok || !spec.Meditation.Enabled {
			continue
		}
		if plan != nil {
			return nil, fmt.Errorf("tagent: agents %q and %q both enable meditation, and one host drives one reflection loop",
				declaredBy, id)
		}
		entry := spec.Meditation
		interval := entry.Interval
		if interval < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation interval %s", id, interval)
		}
		if interval == 0 {
			interval = hostMeditationDefaultInterval
		}
		if entry.MinGap < 0 {
			return nil, fmt.Errorf("tagent: agent %q declares a negative meditation min_gap %s", id, entry.MinGap)
		}
		prompt, err := hostMeditationPrompt(promptDir, id, entry.PromptFile)
		if err != nil {
			return nil, err
		}
		reads := memory.PartitionsOf(id, spec.Memory.ReadNamespaces)
		scope, err := meditation.ResolveObservationScope(id, entry.ObservedNamespaces, reads)
		if err != nil {
			return nil, fmt.Errorf("tagent: %w", err)
		}
		targets, err := hostMeditationTargets(doc, id, entry.DeliverTo)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(storageRoot) == "" {
			return nil, fmt.Errorf("tagent: agent %q enables meditation beside no storage tier, so its anchor record has nowhere to persist", id)
		}
		plan = &hostMeditationPlan{
			agent:    id,
			session:  hostSessionPrefix(doc.Entry) + "meditation",
			interval: interval,
			minGap:   entry.MinGap,
			prompt:   prompt,
			observed: scope.Names,
			reads:    reads,
			targets:  targets,
			anchors:  filepath.Join(storageRoot, "meditation", id, "anchors.json"),
		}
		declaredBy = id
	}
	return plan, nil
}

// hostMeditationPrompt reads the static prompt one enabled meditation carries.
// A relative path resolves beside the declared prompt directory, and a file the
// checkout does not carry refuses the assembly.
func hostMeditationPrompt(promptDir, id, name string) (string, error) {
	declared := strings.TrimSpace(name)
	if declared == "" {
		return "", fmt.Errorf("tagent: agent %q enables meditation but names no prompt_file", id)
	}
	path := declared
	if !filepath.IsAbs(path) {
		path = filepath.Join(promptDir, path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("tagent: agent %q cannot read the meditation prompt %s: %w", id, path, err)
	}
	if strings.TrimSpace(string(body)) == "" {
		return "", fmt.Errorf("tagent: agent %q declares an empty meditation prompt %s", id, path)
	}
	return string(body), nil
}

// hostMeditationTargets resolves the delivery white list of one enabled
// meditation: every name must be a declared agent, deduplicated in declaration
// order, and a section that names no target is refused by name.
func hostMeditationTargets(doc document.Document, id string, declared []string) ([]string, error) {
	targets := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, name := range declared {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		if _, ok := doc.Agents[trimmed]; !ok {
			return nil, fmt.Errorf("tagent: agent %q delivers its meditation to the undeclared agent %q", id, trimmed)
		}
		seen[trimmed] = true
		targets = append(targets, trimmed)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("tagent: agent %q enables meditation but names no deliver_to target", id)
	}
	return targets, nil
}

// hostSessionPrefix reports the session identity prefix of one entry agent: the
// label the deployment names, and the host default beside none.
func hostSessionPrefix(entry string) string {
	label := strings.TrimSpace(os.Getenv("TAGENT_SESSION_ID"))
	if label == "" {
		label = "tagent-session"
	}
	return label + ":" + strings.TrimSpace(entry) + ":"
}

// hostMeditationPort is the host's binding of the two faces a reflection turn
// writes through: the cross-session sink a delivered summary commits into and
// the live-session query it resolves its declared targets against.
//
// The port is built before the run and bound after Assemble, so the tool the
// meditation session carries reads the live scene the run holds instead of a
// copy made at construction. The meditation loop session is never a delivery
// target: it is the observing session itself, not a binding of the agent.
type hostMeditationPort struct {
	plan *hostMeditationPlan
	run  *Run
}

var (
	_ meditation.EventSink    = (*hostMeditationPort)(nil)
	_ meditation.SessionQuery = (*hostMeditationPort)(nil)
)

// Bind attaches the assembled run the port writes through.
func (p *hostMeditationPort) Bind(run *Run) { p.run = run }

// CommitToSession commits one delivered event into the chain of its address.
func (p *hostMeditationPort) CommitToSession(ctx context.Context, addr event.Address, evt event.Event) (event.EventID, error) {
	if p.run == nil {
		return "", errors.New("tagent: the meditation port is bound to no run")
	}
	return p.run.CommitToSession(ctx, addr, evt)
}

// ActiveSessions reports the run's live bindings of one agent, without the
// observing session of the reflection itself.
func (p *hostMeditationPort) ActiveSessions(agentID event.AgentID) []event.Address {
	if p.run == nil {
		return nil
	}
	sessions := p.run.ActiveSessions(agentID)
	out := make([]event.Address, 0, len(sessions))
	for _, addr := range sessions {
		if p.plan != nil && addr.AgentID == event.AgentID(p.plan.agent) && addr.SessionID == event.SessionID(p.plan.session) {
			continue
		}
		out = append(out, addr)
	}
	return out
}

const (
	// hostFacePortEnv names the environment variable holding the endpoint port.
	hostFacePortEnv = "TAGENT_HTTP_PORT"
	// hostFaceDefaultPort is the endpoint port beside no configured one.
	hostFaceDefaultPort = "8089"
	// hostFaceTokenEnv names the environment variable holding the request
	// credential.
	hostFaceTokenEnv = "TAGENT_RL_AUTH_TOKEN"
	// hostFaceBodyLimit is the byte ceiling of one task request body.
	hostFaceBodyLimit = 1 << 20
	// hostShutdownGrace is the span a close gives the in-flight requests.
	hostShutdownGrace = 5 * time.Second
)

// hostInjection is the body an inbound channel posts to the task endpoint: the
// messages the turn reads and the lineage the channel declares for it. A
// non-empty Session spells an inject-only session of its own instead of
// reusing the chat session the host admitted last.
type hostInjection struct {
	Messages      []hostInjectionMessage `json:"messages"`
	TriggerSource string                 `json:"trigger_source"`
	Session       string                 `json:"session"`
}

// hostInjectionMessage is one message of an injection: the role it speaks for
// and its content.
type hostInjectionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// userText reports the user content the injection carries: the last non-empty
// user message is the one a turn reads.
func (in hostInjection) userText() string {
	for i := len(in.Messages) - 1; i >= 0; i-- {
		msg := in.Messages[i]
		if msg.Role != string(model.RoleUser) {
			continue
		}
		if text := strings.TrimSpace(msg.Content); text != "" {
			return msg.Content
		}
	}
	return ""
}

// hostFace serves the host's three loopback endpoints: the liveness probe a
// supervisor polls, the task endpoint an inbound channel posts to, and the
// reload endpoint a supervisor posts to after it rewrites the configuration
// source.
type hostFace struct {
	ctx      context.Context
	host     *Host
	token    string
	listener net.Listener
	server   *http.Server
	done     chan struct{}
}

// newHostFace opens the endpoint address of one host beside the request
// credential it accepts; an empty credential leaves the loopback peer as the
// only admitted subject.
func newHostFace(ctx context.Context, host *Host, token string) (*hostFace, error) {
	address := hostFaceListenAddr(os.Getenv(hostFacePortEnv))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("tagent: listen on %s: %w", address, err)
	}
	face := &hostFace{
		ctx:      ctx,
		host:     host,
		token:    strings.TrimSpace(token),
		listener: listener,
		done:     make(chan struct{}),
	}
	face.server = &http.Server{Handler: face.Handler()}
	return face, nil
}

// metrics answers the counter face of the host in the Prometheus text format:
// the admitted turns, the committed facts and the accepted deliveries, written
// by hand so the endpoint carries no client library of its own. The endpoint
// sits in the health family: it is reachable on the loopback listener beside
// the liveness probe and asks for no credential, because it exposes counts and
// never message content.
func (f *hostFace) metrics(w http.ResponseWriter, _ *http.Request) {
	turns, facts, deliveries := f.host.counters()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprint(w, "# HELP tagent_turns_total Invocations admitted by this host.\n")
	fmt.Fprint(w, "# TYPE tagent_turns_total counter\n")
	fmt.Fprintf(w, "tagent_turns_total %d\n", turns)
	fmt.Fprint(w, "# HELP tagent_facts_total Committed facts held by this run.\n")
	fmt.Fprint(w, "# TYPE tagent_facts_total counter\n")
	fmt.Fprintf(w, "tagent_facts_total %d\n", facts)
	fmt.Fprint(w, "# HELP tagent_deliveries_total Messages accepted by the delivery face.\n")
	fmt.Fprint(w, "# TYPE tagent_deliveries_total counter\n")
	fmt.Fprintf(w, "tagent_deliveries_total %d\n", deliveries)
}

// counters reads the counter faces the metrics endpoint publishes: the
// invocations admitted by this host, the facts the run holds and the
// notifications its delivery face accepted.
func (h *Host) counters() (uint64, int, uint64) {
	h.mu.Lock()
	turns := h.invocations
	h.mu.Unlock()
	facts := 0
	if h.run != nil {
		facts = h.run.CommittedFacts()
	}
	return turns, facts, h.deliveries.acceptedCount()
}

// Handler returns the endpoints of the face: GET /healthz answers the liveness
// probe, GET /metrics answers the counters, POST /task admits one injection,
// and POST /reload rereads the configuration source and answers the model
// changes it judged.
func (f *hostFace) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("GET /metrics", f.metrics)
	mux.HandleFunc("POST /task", f.inject)
	mux.HandleFunc("POST /reload", f.reload)
	return mux
}

// reload rereads the configuration source and answers the model changes the
// reload judged. The endpoint authenticates exactly as the task endpoint does:
// a configured credential must arrive as a bearer token, and a face beside no
// credential admits the loopback peer only.
func (f *hostFace) reload(w http.ResponseWriter, r *http.Request) {
	if err := f.authorize(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	report, err := f.host.Reload(f.ctx)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrHostClosed):
			status = http.StatusServiceUnavailable
		case errors.Is(err, ErrHostReloadStructural):
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(report); err != nil {
		assemblyWarn("tagent: write the reload answer: %v", err)
	}
}

// inject admits one injection and answers before its turn runs: the body's user
// content is driven at the user session the host admitted last, in the host
// context so a shutdown still reaches the turn.
func (f *hostFace) inject(w http.ResponseWriter, r *http.Request) {
	if err := f.authorize(r); err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="tagent"`)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, hostFaceBodyLimit))
	if err != nil {
		http.Error(w, fmt.Sprintf("read the task body: %v", err), http.StatusBadRequest)
		return
	}
	var injection hostInjection
	if err := json.Unmarshal(body, &injection); err != nil {
		http.Error(w, fmt.Sprintf("decode the task body: %v", err), http.StatusBadRequest)
		return
	}
	text := injection.userText()
	if text == "" {
		http.Error(w, ErrHostEmptyTask.Error(), http.StatusBadRequest)
		return
	}
	var addr event.Address
	if label := strings.TrimSpace(injection.Session); label != "" {
		addr = f.host.injectAddress(label)
	} else {
		var ok bool
		addr, ok = f.host.userAddress()
		if !ok {
			http.Error(w, ErrHostNoActiveSession.Error(), http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
	go func() {
		if _, err := f.host.Invoke(f.ctx, InvokeRequest{
			Address:       addr,
			TriggerSource: injection.TriggerSource,
			Input:         []model.Message{{Role: model.RoleUser, Content: text}},
		}); err != nil {
			assemblyWarn("tagent: the injected task failed (session=%s): %v", addr.SessionID, err)
		}
	}()
}

// authorize judges one task request: a configured credential must arrive as a
// bearer token and match in constant time, and a face beside no credential
// admits the loopback peer only.
func (f *hostFace) authorize(r *http.Request) error {
	if f.token != "" {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return ErrHostUnauthenticated
		}
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(token)), []byte(f.token)) != 1 {
			return ErrHostUnauthenticated
		}
		return nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ErrHostUnauthenticated
	}
	if !net.ParseIP(host).IsLoopback() {
		return ErrHostUnauthenticated
	}
	return nil
}

// start serves the endpoints on the opened listener.
func (f *hostFace) start() {
	go func() {
		defer close(f.done)
		if err := f.server.Serve(f.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			assemblyWarn("tagent: the task face stopped: %v", err)
		}
	}()
}

// stop shuts the endpoints down and waits for the in-flight handlers.
func (f *hostFace) stop(ctx context.Context) error {
	if f == nil || f.server == nil {
		return nil
	}
	err := f.server.Shutdown(ctx)
	if err != nil {
		_ = f.server.Close()
	}
	<-f.done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// addr reports the address the face listens on.
func (f *hostFace) addr() string {
	if f == nil || f.listener == nil {
		return ""
	}
	return f.listener.Addr().String()
}

// hostFaceListenAddr reports the listen address of the face: the loopback
// interface on the named port, and on the default port when the value names no
// port number.
func hostFaceListenAddr(port string) string {
	if _, err := strconv.Atoi(port); err != nil {
		port = hostFaceDefaultPort
	}
	return net.JoinHostPort("127.0.0.1", port)
}

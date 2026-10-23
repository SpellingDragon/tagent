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

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/evolution/governance"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/rl"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/telemetry"
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
	natives, err := doc.nativeAgents(ctx, client, cfg.hostSender, recallPort, meditationPlan, meditationPort)
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
	// host is assigned once the assembly returns; the settle face captures the
	// variable, because the three faces it needs are the host's own memory and
	// drive and are only called after Boot published it.
	var host *Host
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
		options = append(options, WithExecManager(tagentexec.NewManager(executor)))
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
			func(session event.SessionID) (event.Address, bool) { return host.userAddressOf(session) },
			func(ctx context.Context, addr event.Address, fact event.Event) error {
				return host.fireNoticeTurn(ctx, addr, fact)
			},
		)
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
		ctx:           ctx,
		cancel:        cancel,
		run:           run,
		entry:         event.AgentID(decl.Entry),
		sender:        cfg.hostSender,
		deliveries:    deliveryMeter,
		meditation:    driver,
		settle:        settleDriver,
		configPath:    configPath,
		doc:           doc,
		natives:       natives,
		models:        models,
		capture:       capture,
		telemetryStop: telemetryStop,
		userAddresses: map[event.SessionID]event.Address{},
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
	turn, err := h.run.Invoke(ctx, req)
	if err != nil {
		assemblyWarn("tagent: the invocation %s of %s ended by error: %v", req.InvocationID, req.Address.SessionID, err)
		return turn, err
	}
	if user {
		h.deliverFresh(ctx, req.Address, baseline, sender)
	}
	return turn, nil
}

// fireNoticeTurn drives one unattended follow-up turn at the user address a
// terminal settle belongs to. The turn carries no input: the notice fact this
// method commits under the follow-up's own invocation is the material the drive
// reads at its head, because a range reads the notifications of its own
// identity prefix. The turn is stamped "task" and holds the same address gate a
// user turn holds, so the two never run beside each other.
//
// The fact it commits is a feedback fact, never a terminal settle with a settle
// lineage, so the driver that fired this turn never fires on its output. The
// assistant body the turn produces is delivered through the message sender by
// the ordinary drive path, exactly as a user turn's output is.
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
	invocationID := h.nextInvocation(addr)
	kind := noticeKindOf(fact)
	notice := task.Notice{TaskID: noticeTaskIDOf(fact), Kind: kind, Event: fact.Key}
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
	_, err = h.driveLocked(ctx, InvokeRequest{
		InvocationID:  invocationID,
		Address:       addr,
		TriggerSource: "task",
	})
	return err
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

// deliverFresh delivers the assistant bodies the session's committed facts
// carry past the invocation baseline through the sender. A session the chain
// maps onto a projection commits no transcript history, so the facts are the
// one read face both session kinds share.
func (h *Host) deliverFresh(ctx context.Context, addr event.Address, baseline int, sender MessageSender) {
	if sender == nil {
		return
	}
	contents := freshAssistantOf(h.run.SessionFacts(addr), baseline)
	if len(contents) == 0 {
		assemblyDebug("tagent: the invocation of %s produced no deliverable assistant body", addr.SessionID)
		return
	}
	for _, content := range contents {
		if err := sender.Send(ctx, addr, content); err != nil {
			assemblyWarn("tagent: the host delivery to %s was refused: %v", addr.SessionID, err)
		}
	}
}

// freshAssistantOf reports the plain assistant bodies one session's committed
// facts carry past the baseline: the bodies the invocation that just ended
// produced, and nothing an earlier delivery already carried.
func freshAssistantOf(facts []event.Event, baseline int) []string {
	if baseline > len(facts) {
		baseline = len(facts)
	}
	contents := make([]string, 0, 4)
	for _, evt := range facts[baseline:] {
		msg := evt.Message
		if msg.Role != model.RoleAssistant || len(msg.ToolCalls) > 0 {
			continue
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			contents = append(contents, content)
		}
	}
	return contents
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
func (d *hostDocument) nativeAgents(ctx context.Context, client *http.Client, sender MessageSender, recallPort *RecallPort, meditationPlan *hostMeditationPlan, meditationPort *hostMeditationPort) (map[event.AgentID]NativeAgent, error) {
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
		tools, err := d.toolsFor(ctx, spec.ID, sender, recallPort, meditationPlan, meditationPort)
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
func (d *hostDocument) toolsFor(ctx context.Context, id string, sender MessageSender, recallPort *RecallPort, meditationPlan *hostMeditationPlan, meditationPort *hostMeditationPort) ([]trpctool.CallableTool, error) {
	available := make(map[string]trpctool.CallableTool, len(hostToolIDs))
	if id != meditationAgentOf(meditationPlan) && d.declaresTool(id, hostSendToolID) {
		available[hostSendToolID] = &hostSendTool{sender: sender}
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

// hostSendTool is the model-visible outbound face: one content argument
// delivered to the address the call frame carries through the host's message
// sender.
type hostSendTool struct {
	sender MessageSender
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
		Description: "Deliver one body to the user address of this call. The target comes from the call's session address; it is not an argument. A refusal returns a [send_denied] result text and the turn continues.",
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
// shape.
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
	if err := t.sender.Send(ctx, addr, args.Content); err != nil {
		return t.denied(err.Error()), nil
	}
	return "[send_ok] delivered to " + string(addr.SessionID), nil
}

// denied renders one refusal as result text.
func (t *hostSendTool) denied(reason string) string {
	return "[send_denied] " + reason
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
// messages the turn reads and the lineage the channel declares for it.
type hostInjection struct {
	Messages      []hostInjectionMessage `json:"messages"`
	TriggerSource string                 `json:"trigger_source"`
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
	addr, ok := f.host.userAddress()
	if !ok {
		http.Error(w, ErrHostNoActiveSession.Error(), http.StatusServiceUnavailable)
		return
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

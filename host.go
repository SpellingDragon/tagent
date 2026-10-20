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
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/document"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/evolution"
	"github.com/SpellingDragon/tagent/evolution/meditation"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/replay"
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
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ErrHostConfigMissing reports a Boot call that names no configuration path:
// the host has no document to read, so no runtime is assembled.
var ErrHostConfigMissing = errors.New("tagent: the host names no configuration path")

// ErrHostSenderMissing reports a host that grants the send tool or admits an
// invocation at the user address beside no outbound message sender: the turn
// would produce output nothing can deliver, so the run is refused by name.
var ErrHostSenderMissing = errors.New("tagent: the host carries no outbound message sender")

// ErrHostDelivererMissing reports a document that enables meditation beside no
// reflection deliverer: a fired reflection would have nowhere to go, so the
// host is refused by name instead of running a silent loop.
var ErrHostDelivererMissing = errors.New("tagent: the host enables meditation beside no reflection deliverer")

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

// DeliverFunc is the reflection delivery face the host hands a fired
// reflection to: the source agent the reflection belongs to, the declared
// target agents it flows back to and one body per call. A target is named by
// its agent identity alone; the application resolves the sessions it has bound
// to that agent.
type DeliverFunc func(ctx context.Context, source string, targets []event.Address, content string)

// HostOption configures one Boot call; it aliases the assembly Option.
type HostOption = Option

// WithSender attaches the outbound message face every turn output of the host
// is delivered through.
func WithSender(sender MessageSender) HostOption {
	return func(c *assemblyConfig) { c.hostSender = sender }
}

// WithDeliverer attaches the reflection delivery face of a document that
// enables meditation.
func WithDeliverer(deliver DeliverFunc) HostOption {
	return func(c *assemblyConfig) { c.hostDeliverer = deliver }
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

	face       *hostFace
	meditation *hostReflection

	mu          sync.Mutex
	lastAddr    event.Address
	sent        map[event.Address]int
	invocations uint64
	closed      bool
	closeErr    error

	gates sync.Map
}

var (
	_ func(string, ...HostOption) (*Host, error)                      = Boot
	_ func(MessageSender) HostOption                                  = WithSender
	_ func(task.Approver) HostOption                                  = WithApprover
	_ func(DeliverFunc) HostOption                                    = WithDeliverer
	_ func(state.Governance) HostOption                               = WithGovernance
	_ func(*Host, context.Context, InvokeRequest) (agent.Turn, error) = (*Host).Invoke
	_ func(*Host, context.Context) error                              = (*Host).Close
	_ func(*Host) *Run                                                = (*Host).Run
	_ func(*Host) string                                              = (*Host).FaceAddr
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
	if cfg.hostSender == nil && doc.declaresSend() {
		return nil, fmt.Errorf("%w: the document grants the %q tool", ErrHostSenderMissing, hostSendToolID)
	}
	plan, err := MemoryPlanOf(decl, doc.Storage.Type, doc.Storage.Root)
	if err != nil {
		return nil, err
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
	natives, err := doc.nativeAgents(ctx, cfg.hostSender, recallPort)
	if err != nil {
		cancel()
		return nil, err
	}
	meditationPlan, err := resolveHostMeditation(mapped, doc.PromptDir, storageRootOf(plan))
	if err != nil {
		cancel()
		return nil, err
	}
	if meditationPlan != nil && cfg.hostDeliverer == nil {
		cancel()
		return nil, fmt.Errorf("%w: agent %q enables meditation", ErrHostDelivererMissing, meditationPlan.agent)
	}
	options := []Option{
		WithGovernance(governanceOf(cfg.governance)),
		WithTelemetry(telemetry.NewLogObserver(trpclog.Default)),
	}
	if plan != nil {
		options = append(options, WithMemoryPlan(plan))
	}
	if meditationPlan != nil {
		options = append(options, WithReflectionAnchors(meditationPlan.anchors))
	}
	if cfg.approver != nil {
		options = append(options, WithApprover(cfg.approver))
	}
	run, err := Assemble(ctx, decl, natives, options...)
	if err != nil {
		cancel()
		return nil, err
	}
	recallPort.Bind(run)
	host := &Host{
		ctx:    ctx,
		cancel: cancel,
		run:    run,
		entry:  event.AgentID(decl.Entry),
		sender: cfg.hostSender,
		sent:   map[event.Address]int{},
	}
	if meditationPlan != nil {
		driver, err := host.newReflection(meditationPlan, cfg.hostDeliverer)
		if err != nil {
			cancel()
			return nil, host.abortBoot(err)
		}
		driver.start(ctx)
		host.meditation = driver
	}
	face, err := newHostFace(ctx, host, os.Getenv(hostFaceTokenEnv))
	if err != nil {
		cancel()
		return nil, host.abortBoot(err)
	}
	face.start()
	host.face = face
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
// reports the failure it carries.
func (h *Host) abortBoot(cause error) error {
	closeCtx, cancel := context.WithTimeout(context.Background(), hostShutdownGrace)
	defer cancel()
	if h.run != nil {
		_, _ = h.run.Close(closeCtx, agent.CloseRequest{})
		_ = h.run.CloseMCPServers()
	}
	h.cancel()
	return cause
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
	unlock := h.lockAddress(req.Address)
	defer unlock()
	if user {
		h.rememberUserAddress(req.Address)
	}
	turn, err := h.run.Invoke(ctx, req)
	if err != nil {
		return turn, err
	}
	if user {
		h.deliverFresh(ctx, req.Address, sender)
	}
	return turn, nil
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
		h.finishClose(first, err)
		return err
	}
	if !report.Drained {
		assemblyWarn("tagent: the host close reported %d unsettled obligation(s)", len(report.Unsettled))
	}
	if err := h.run.CloseMCPServers(); err != nil && first == nil {
		first = err
	}
	h.finishClose(first, nil)
	return first
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
// next injection arrives.
func (h *Host) rememberUserAddress(addr event.Address) {
	h.mu.Lock()
	h.lastAddr = addr
	h.mu.Unlock()
}

// userAddress reports the user session the host admitted last.
func (h *Host) userAddress() (event.Address, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastAddr, h.lastAddr.IsComplete()
}

// lockAddress serializes the drives of one session.
func (h *Host) lockAddress(addr event.Address) func() {
	value, _ := h.gates.LoadOrStore(addr, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// deliverFresh delivers the assistant messages the session accumulated since
// the previous delivery through the sender.
func (h *Host) deliverFresh(ctx context.Context, addr event.Address, sender MessageSender) {
	if sender == nil {
		return
	}
	for _, content := range h.freshAssistant(addr) {
		if err := sender.Send(ctx, addr, content); err != nil {
			assemblyWarn("tagent: the host delivery to %s was refused: %v", addr.SessionID, err)
		}
	}
}

// freshAssistant reports the assistant contents of one session since the
// previous read and records the read position, so a reply never repeats a body
// an earlier delivery already carried.
func (h *Host) freshAssistant(addr event.Address) []string {
	history := h.run.SessionHistory(addr)
	h.mu.Lock()
	from := h.sent[addr]
	if from > len(history) {
		from = len(history)
	}
	h.sent[addr] = len(history)
	h.mu.Unlock()
	contents := make([]string, 0, len(history)-from)
	for _, msg := range history[from:] {
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

// Declaration returns the organization declaration the host assembles.
func (d *hostDocument) Declaration() (*config.Declaration, error) {
	mapped, err := d.document()
	if err != nil {
		return nil, err
	}
	return mapped.Declaration()
}

// mapDocument builds the declaration-shaped document of the host.
func (d *hostDocument) mapDocument() (document.Document, error) {
	agents := make(map[string]config.AgentSpec, len(d.Agents))
	for id, entry := range d.Agents {
		spec := config.AgentSpec{
			ID:         id,
			Model:      d.modelName(id),
			FinalRound: entry.finalRound(),
			Memory:     entry.Memory,
			Meditation: entry.Meditation,
			File:       entry.File,
			Exec:       entry.Exec,
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
	recall.QueryToolName,
	recall.GetToolName,
	recall.RecentToolName,
	recall.TraceToolName,
	knowledge.SkillSearchToolName,
	knowledge.SkillLoadToolName,
	spec.ToolName,
}, tagentfile.Names()...),
	tagentexec.PresetName,
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

// declaresSend reports whether any agent section declares the send tool.
func (d *hostDocument) declaresSend() bool {
	for id := range d.Agents {
		if d.declaresTool(id, hostSendToolID) {
			return true
		}
	}
	return false
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
func (d *hostDocument) nativeAgents(ctx context.Context, sender MessageSender, recallPort *RecallPort) (map[event.AgentID]NativeAgent, error) {
	decl, err := d.Declaration()
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: d.requestTimeout()}
	out := make(map[event.AgentID]NativeAgent, len(decl.Agents))
	for _, spec := range decl.Agents {
		native, err := d.modelFor(spec.ID, client)
		if err != nil {
			return nil, err
		}
		tools, err := d.toolsFor(ctx, spec.ID, sender, recallPort)
		if err != nil {
			return nil, err
		}
		out[event.AgentID(spec.ID)] = NativeAgent{Model: native, Tools: tools}
	}
	return out, nil
}

// toolsFor constructs the tool instances the host contributes for one agent;
// the core owns the execution, MCP and delegation families of the declaration.
func (d *hostDocument) toolsFor(ctx context.Context, id string, sender MessageSender, recallPort *RecallPort) ([]trpctool.CallableTool, error) {
	available := make(map[string]trpctool.CallableTool, len(hostToolIDs))
	if d.declaresTool(id, hostSendToolID) {
		available[hostSendToolID] = &hostSendTool{sender: sender}
	}
	if d.declaresRecall(id) {
		for _, candidate := range recall.New(recallPort.Reads(), memory.PartitionsOf(id, d.Agents[id].Memory.ReadNamespaces)) {
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
// skill roots and the read root its file reads are confined to.
func (d *hostDocument) knowledgeTools(ctx context.Context, id string) ([]trpctool.CallableTool, error) {
	opts := knowledge.Options{ReadRoot: d.readRoot(id)}
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

// modelFor constructs the native model of one declared agent from its provider
// entry. Chat telemetry stays on the entry agent's model only.
func (d *hostDocument) modelFor(id string, client *http.Client) (trpcmodel.Model, error) {
	entry := d.Agents[id]
	providerName := strings.TrimSpace(entry.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(d.Provider)
	}
	provider, ok := d.Providers[providerName]
	if !ok {
		return nil, fmt.Errorf("tagent: the host document declares no provider %q", providerName)
	}
	if protocol := strings.TrimSpace(provider.Provider); protocol != "" && protocol != "openai" {
		return nil, fmt.Errorf("tagent: provider %q runs protocol %q, which this host does not wire", providerName, protocol)
	}
	modelName := d.modelName(id)
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

// hostReflection drives one declared reflection loop: each tick reads the
// observed face through the reflection chain, injects the reflection input into
// the observing agent's own loop session, and hands a fired reflection's output
// to the delivery face.
type hostReflection struct {
	plan       *hostMeditationPlan
	source     event.Address
	targets    []event.Address
	reflection *meditation.Reflection
	host       *Host
	deliver    DeliverFunc

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// newReflection builds the scheduled reflection of one resolved plan over the
// run's own observation, input and watermark faces.
func (h *Host) newReflection(plan *hostMeditationPlan, deliver DeliverFunc) (*hostReflection, error) {
	gate := h.run.ReflectionGate()
	if gate == nil {
		return nil, errors.New("tagent: the assembly carries no reflection anchor store")
	}
	reflection, err := meditation.NewReflection(meditation.ReflectionConfig{
		Loop:    meditation.ReflectionLoop{Agent: plan.agent, Session: plan.session},
		Face:    meditation.ReflectionFace{ObservedNamespaces: plan.observed, ReadNamespaces: plan.reads},
		Cadence: meditation.ReflectionCadence{Interval: plan.interval, MinGap: plan.minGap},
		Prompt:  plan.prompt,
	}, gate, h.run.ObservationFacts(), h.run.ReflectionInput())
	if err != nil {
		return nil, err
	}
	source := event.Address{AgentID: event.AgentID(plan.agent), SessionID: event.SessionID(plan.session)}
	targets := make([]event.Address, 0, len(plan.targets))
	for _, name := range plan.targets {
		targets = append(targets, event.Address{AgentID: event.AgentID(name)})
	}
	return &hostReflection{
		plan:       plan,
		source:     source,
		targets:    targets,
		reflection: reflection,
		host:       h,
		deliver:    deliver,
	}, nil
}

// start begins the tick loop: the first tick runs one interval after the start,
// so a restart never fires a reflection the previous process just consumed.
func (r *hostReflection) start(ctx context.Context) {
	r.mu.Lock()
	if r.cancel != nil {
		r.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.cancel, r.done = cancel, done
	r.mu.Unlock()
	go r.run(loopCtx, done)
}

// run ticks the reflection loop until its context ends.
func (r *hostReflection) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(r.plan.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

// tick runs one scheduled reflection and, when it fired, hands what the
// reflection turn produced to the delivery face.
func (r *hostReflection) tick(ctx context.Context) {
	outcome := r.reflection.Tick(ctx, time.Now())
	if outcome.Decision.Disposition != meditation.TickFired {
		assemblyInfo("tagent: meditation tick of %s: %s (%s)", r.plan.agent, outcome.Decision.Disposition, outcome.Decision.Reason)
		return
	}
	delivery := meditation.DeliveryPolicy{}
	if outcome.Injection == nil || !delivery.Deliverable(outcome.Injection.Event) {
		assemblyInfo("tagent: meditation tick of %s withheld the reflection: no host-visible lineage", r.plan.agent)
		return
	}
	assemblyInfo("tagent: meditation tick of %s fired reflection %s", r.plan.agent, outcome.Injection.Key)
	for _, content := range r.host.freshAssistant(r.source) {
		r.deliver(ctx, string(r.source.AgentID), r.targets, content)
	}
}

// Stop ends the tick loop and waits for the tick in flight to return.
func (r *hostReflection) Stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.cancel, r.done = nil, nil
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
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

// hostFace serves the host's two loopback endpoints: the liveness probe a
// supervisor polls and the task endpoint an inbound channel posts to.
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

// Handler returns the endpoints of the face: GET /healthz answers the liveness
// probe, and POST /task admits one injection.
func (f *hostFace) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	})
	mux.HandleFunc("POST /task", f.inject)
	return mux
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

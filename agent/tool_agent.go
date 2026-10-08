// Package agent provides tool agent registration and the AgentToolWrapper that
// turns a TagentAgent into a CallableTool for extensible agent composition.
//
// - 注册三阶段：内置工厂在 tagent/builtin.go 的 init 注册，自定义工厂经 RegisterToolAgent 注册，tagent.New 解析 ToolRef 构建被引用的 agent。
// - AgentToolWrapper 在 InputSchema 声明 event_key 参数（当 EventParams 含它时），把 event_key 解析为从父 MemStore 取回的完整事件，并将其作为外部上下文交给子 agent。
// 契约: docs/wiki/agent/agent-architecture.md#core-components
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/agent/reliability"
	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/prompt"
	tagenttool "github.com/SpellingDragon/tagent/tool"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ExternalContextEntry is the serializable representation of an external event
// for cross-process context passing via RuntimeState.
type ExternalContextEntry struct {
	EventKey     int64  `json:"event_key"`
	EventType    string `json:"event_type"`
	EventSummary string `json:"event_summary"`
}

// ExternalContextKey is the RuntimeState key used to pass external context
// through the Invocation → A2A metadata → Invocation chain.
// Exported so that tagent.go can use it with a2aagent.WithTransferStateKey.
const ExternalContextKey = "external_context"

// PerCallOverridesKey is the RuntimeState key under which one delegation carries
// its per-call execution-view overrides to the delegate's assembly point. It
// belongs to the same invocation-scoped input family as ExternalContextKey: the
// payload rides the Invocation, dies with the call, and is held on neither the
// agent instance nor any shared generation face.
const PerCallOverridesKey = "percall_overrides"

// PerCallOverrideError is the structured refusal of a per-call override
// argument: it names the rejected field and, where relevant, the offending
// entries, so the caller learns what was refused and against which bound,
// instead of meeting a silently narrowed or silently accepted call.
type PerCallOverrideError struct {
	// Field is the rejected argument name (e.g. "tools_subset").
	Field string
	// Offenders names the rejected entries within Field.
	Offenders []string
	// Reason states the rule the request violated.
	Reason string
}

// Error implements error.
func (e PerCallOverrideError) Error() string {
	if len(e.Offenders) > 0 {
		return fmt.Sprintf("per-call override refused: %s: %s [%s]", e.Field, e.Reason, strings.Join(e.Offenders, ", "))
	}
	return fmt.Sprintf("per-call override refused: %s: %s", e.Field, e.Reason)
}

// OverrideField names the argument this refusal rejected.
func (e PerCallOverrideError) OverrideField() string { return e.Field }

// parsePerCallOverrides reads the per-call override arguments of one delegation
// and validates them against the delegate's declared maximum tool domain and the
// model references of the execution view this call will assemble on. Every
// refusal is returned at the argument-checking point, before the delegate runs.
// A nil result means the call carries no override, so assembly uses the
// generation's own view.
func (w *AgentToolWrapper) parsePerCallOverrides(ctx context.Context, args map[string]any) (*task.Overrides, error) {
	ov := &task.Overrides{}
	declared := false

	if raw, ok := args["system_prompt_override"]; ok && raw != nil {
		s, isStr := raw.(string)
		if !isStr {
			return nil, PerCallOverrideError{Field: "system_prompt_override", Reason: "must be a string"}
		}
		ov.SystemPrompt = s
		declared = true
	}

	if raw, ok := args["model_override"]; ok && raw != nil {
		ref, isStr := raw.(string)
		if !isStr {
			return nil, PerCallOverrideError{Field: "model_override", Reason: "must be a model reference name"}
		}
		view, viewErr := w.callModelRefs(ctx)
		if viewErr != nil {
			return nil, viewErr
		}
		if _, err := resolveModelReference(ref, view); err != nil {
			return nil, err
		}
		ov.ModelRef = ref
		declared = true
	}

	if raw, ok := args["tools_subset"]; ok && raw != nil {
		items, isArr := raw.([]interface{})
		if !isArr {
			return nil, PerCallOverrideError{Field: "tools_subset", Reason: "must be an array of tool names"}
		}
		if len(items) == 0 {
			return nil, PerCallOverrideError{Field: "tools_subset",
				Reason: "must name at least one tool; omit the argument to keep the declared surface"}
		}
		domain := w.maxToolDomain(ctx)
		allowed := make(map[string]bool, len(domain))
		for _, name := range domain {
			allowed[name] = true
		}
		subset := make([]string, 0, len(items))
		var offenders []string
		for _, item := range items {
			name, isStr := item.(string)
			if !isStr {
				return nil, PerCallOverrideError{Field: "tools_subset", Reason: "every entry must be a tool name"}
			}
			if !allowed[name] {
				offenders = append(offenders, name)
				continue
			}
			subset = append(subset, name)
		}
		if len(offenders) > 0 {
			return nil, PerCallOverrideError{Field: "tools_subset", Offenders: offenders,
				Reason: "outside the delegate's maximum tool domain " + formatToolDomain(domain)}
		}
		ov.ToolsSubset = subset
		declared = true
	}

	if !declared || ov.IsEmpty() {
		return nil, nil
	}
	return ov, nil
}

// maxToolDomain lists the tool names of the view this call would assemble with:
// the generation this wrapper's face declared when it carries one, the delegate's
// own declared surface otherwise. That list IS the delegate's maximum tool
// domain — the hard upper bound a per-call tools_subset must fit inside, so the
// check and the assembled view always read one source.
func (w *AgentToolWrapper) maxToolDomain(ctx context.Context) []string {
	if b := w.selectedCallGeneration(ctx); b != nil && b.runCfg != nil {
		return toolNamesOf(b.runCfg.Tools)
	}
	return toolNamesOf(w.agent.Tools())
}

// selectedCallGeneration returns the execution generation THIS call assembles on:
// the lease the call carries, but only when that lease pins a generation OF THE
// DELEGATE'S OWN context manager — armDeclaredCall's declared-generation lease, or
// a re-entry riding its initiator. That is exactly the condition under which
// agent/session.go copies the generation's assembled config into the per-call view
// instead of using the resident definition, so validation cannot answer "which
// generation am I running on" differently from assembly. nil means there is no
// generation to read: the call assembles on the resident definition, or the target
// is not a local agent at all.
//
// Both per-call override bounds resolve through here — the tool domain above and
// the model references below — because one selected view is one truth; two
// predicates would let a call be validated against a generation it does not run on.
func (w *AgentToolWrapper) selectedCallGeneration(ctx context.Context) *execBinding {
	child, isLocal := w.agent.(*TagentAgent)
	if !isLocal || child == nil || child.contextManager == nil {
		return nil
	}
	cl, ok := execLeaseFromContext(ctx)
	if !ok || cl == nil || !cl.belongsToOwnerOf(child.contextManager) {
		return nil
	}
	return cl.pinnedBinding()
}

// callModelRefs returns the FROZEN model-reference set of the execution view this
// call resolves against — the one read of that view a per-call model override gets.
//
//   - A generation published with a reference snapshot hands out exactly that snapshot.
//   - A generation without one, and a call with no generation at all, read the registry once here.
//
// The process registry is only ever READ on this path, so a refused candidate
// (reserved-name conflict) cannot re-point or drop a reference another view serves.
func (w *AgentToolWrapper) callModelRefs(ctx context.Context) (*ModelRefSnapshot, error) {
	if b := w.selectedCallGeneration(ctx); b != nil {
		if b.modelRefs != nil {
			return b.modelRefs, nil
		}
		name, m := generationViewModel(b)
		return NewModelRefSnapshot(name, m)
	}
	name, m := w.residentViewModel()
	return NewModelRefSnapshot(name, m)
}

// generationViewModel is one generation's model identity: the assembled
// per-generation config first (the very object agent/session.go copies into the
// per-call view), else the published face that generation was built from. A lazily
// materialized generation has no assembled config, which is why the face is the
// second source and never the first.
func generationViewModel(b *execBinding) (string, model.Model) {
	if b == nil {
		return "", nil
	}
	if b.runCfg != nil {
		name := b.runCfg.Name
		if name == "" {
			name = b.face.Name
		}
		return name, b.runCfg.Model
	}
	return b.face.Name, b.face.Model
}

// residentViewModel is the delegate's own construction-time definition — the view
// an un-leased call assembles on. A remote target's model lives in another process,
// so it claims no reserved name here and only registry references resolve.
func (w *AgentToolWrapper) residentViewModel() (string, model.Model) {
	child, isLocal := w.agent.(*TagentAgent)
	if !isLocal || child == nil {
		return "", nil
	}
	if child.config != nil {
		name := child.config.Name
		if name == "" {
			name = child.name
		}
		return name, child.config.Model
	}
	return child.name, nil
}

// toolNamesOf lists the declaration names of a tool surface.
func toolNamesOf(tools []trpctool.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if d := t.Declaration(); d != nil && d.Name != "" {
			names = append(names, d.Name)
		}
	}
	return names
}

// formatToolDomain renders a maximum tool domain for a refusal message.
func formatToolDomain(names []string) string {
	if len(names) == 0 {
		return "(the delegate declares no tool at all)"
	}
	return "[" + strings.Join(names, ", ") + "]"
}

// perCallOverridesFromInvocation reads the per-call override payload this
// invocation carries. It returns nil when the call carries none, and an error
// when a payload is present but unreadable — a call must never assemble a
// partial view off a corrupt override.
func perCallOverridesFromInvocation(inv *agent.Invocation) (*task.Overrides, error) {
	if inv == nil || inv.RunOptions.RuntimeState == nil {
		return nil, nil
	}
	raw, ok := inv.RunOptions.RuntimeState[PerCallOverridesKey]
	if !ok {
		return nil, nil
	}
	var data []byte
	switch v := raw.(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	case string:
		data = []byte(v)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var ov task.Overrides
	if err := json.Unmarshal(data, &ov); err != nil {
		return nil, fmt.Errorf("per-call overrides unreadable: %w", err)
	}
	if ov.IsEmpty() {
		return nil, nil
	}
	return &ov, nil
}

// applyPerCallOverrides presses one invocation's overrides into the config its
// execution view is assembled from. Each present field replaces that view face
// for THIS call; absent fields keep the generation's value. cfg is the
// per-invocation copy, so neither the resident definition nor a shared
// generation face is written.
//
// A call that owns its prompt also owns the prompt channel: a file-backed source
// rewrites the system message on every round of the view it belongs to, so the
// assembled view carries no source, while the resident definition keeps its own.
// pinned is the reference snapshot of the execution generation this call was
// selected on. It is variadic on purpose: an assembly site that has the pinned
// generation at hand passes it and resolves against THAT frozen set, while a site
// that does not yet hand one over keeps compiling and resolves against the view
// assembled here (its own model under the reserved name, plus the names currently
// registered). The org wiring passes the generation's snapshot so validation and
// assembly read one object; nothing else about the assembled view changes.
func applyPerCallOverrides(cfg *TagentConfig, ov *task.Overrides, pinned ...*ModelRefSnapshot) error {
	if ov.SystemPrompt != "" {
		cfg.SystemPrompt = ov.SystemPrompt
		cfg.SystemPromptSource = nil
	}
	if ov.ModelRef != "" {
		view, err := modelRefsOfView(cfg, pinned)
		if err != nil {
			return err
		}
		m, err := resolveModelReference(ov.ModelRef, view)
		if err != nil {
			return err
		}
		cfg.Model = m
	}
	if ov.ToolsSubset != nil {
		declared := make(map[string]bool, len(cfg.Tools))
		for _, t := range cfg.Tools {
			if t == nil {
				continue
			}
			if d := t.Declaration(); d != nil {
				declared[d.Name] = true
			}
		}
		kept := make([]trpctool.Tool, 0, len(ov.ToolsSubset))
		var missing []string
		for _, name := range ov.ToolsSubset {
			if !declared[name] {
				missing = append(missing, name)
				continue
			}
			for _, t := range cfg.Tools {
				if t == nil {
					continue
				}
				if d := t.Declaration(); d != nil && d.Name == name {
					kept = append(kept, t)
					break
				}
			}
		}
		if len(missing) > 0 {
			return PerCallOverrideError{Field: "tools_subset", Offenders: missing,
				Reason: "not declared by the effective generation: " + formatToolDomain(toolNamesOf(cfg.Tools))}
		}
		cfg.Tools = kept
	}
	return nil
}

// serializeExternalContext converts FullEvents into compact JSON entries
// suitable for RuntimeState transport. Only EventKey/EventType/EventSummary
// are included — Content is intentionally excluded to keep the payload small.
func serializeExternalContext(events []memory.FullEvent) ([]byte, error) {
	entries := make([]ExternalContextEntry, len(events))
	for i, evt := range events {
		entries[i] = ExternalContextEntry{
			EventKey:     evt.EventKey,
			EventType:    evt.EventType,
			EventSummary: evt.EventSummary,
		}
	}
	return json.Marshal(entries)
}

// deserializeExternalContext converts JSON bytes back into FullEvents.
// Content is left empty — the remote sub-agent only needs EventSummary
// for context injection (injectExternalContext uses EventSummary only).
func deserializeExternalContext(data []byte) ([]memory.FullEvent, error) {
	var entries []ExternalContextEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("deserialize external context: %w", err)
	}
	events := make([]memory.FullEvent, len(entries))
	for i, e := range entries {
		events[i] = memory.FullEvent{
			EventKey:     e.EventKey,
			EventType:    e.EventType,
			EventSummary: e.EventSummary,
		}
	}
	return events, nil
}

// ExtraParam declares one additional routing-level parameter for an
// agent-kind tool. Declared params are added
// to the tool's InputSchema and, when present in a call, packed together
// with request into a JSON message body — a whitelist pass-through for small
// routing fields (e.g. plan's action/name), NOT a general RPC channel.
type ExtraParam struct {
	Name        string   `json:"name" yaml:"name"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty"`
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
}

// AgentToolWrapper 把一个 agent 包装成工具：携带其描述（可由 prompt.Source 热更）、事件参数、
// 父级存储与投影，以及属主的常驻 cm 句柄。常驻 cm 是**有意**存 cm 而非存某一代执行器——
// cm 活得比它发布过的任何执行器都久，长寿命的任务闭包持有它不会钉住已退役的执行器。
type AgentToolWrapper struct {
	agent            agent.Agent
	desc             string
	descSource       *prompt.Source
	eventParams      []string
	parentStore      memory.MemoryStore
	parentProjection *compress.SessionProjection

	// parentCM is the RESIDENT owner's context manager — the handle a re-entry
	// (Resume/Relaunch of a stored task) reads the CURRENT effective orchestration
	// face from when no initiating call holds a binding. It is wired at
	// cold start alongside parentProjection and is deliberately the owner's cm, not
	// a generation: the cm outlives every executor it publishes, so storing it in a
	// long-lived task closure pins no retired executor.
	parentCM *ContextManager

	// asyncDisabled forces synchronous execution even when a task spawner is
	// available in the invocation context. Default (false) = async-by-default:
	// long sub-agent runs that exceed the sync-wait window return an ack and
	// settle later via task_settled. The fallback switch is retained per spec.
	asyncDisabled bool

	// asyncDenseDuration overrides the dense phase for the sub-agent task's
	// detector (0 → detector default ≈ 10s). The dense→sparse boundary is the
	// sync→async ack point; mainly used by tests to control inline vs ack.
	asyncDenseDuration time.Duration
	// resumeContextRounds caps the prior rounds restored on resume
	// (0 → DefaultResumeContextRounds).
	resumeContextRounds int

	// extraParams are additional routing-level parameters declared via ToolRef
	//. Empty → plain-text request messages,
	// behavior unchanged.
	extraParams []ExtraParam

	// declared is the child generation this wrapper's face declared at publish
	// time (3.2 trunk, armed by WireOrgGeneration). Non-nil → Call resolves the
	// target through THAT generation's own execution view — pinning it for the
	// call subtree — instead of whatever is currently effective on the child.
	// Atomic because the retroactive wiring of the OUTGOING generation may stamp
	// a wrapper while an in-flight call on that very face is reading it; a stamp
	// only ever goes nil→binding (idempotent per publish), so a reader sees
	// either the wired target or the pre-wiring fallback, never a torn one.
	declared atomic.Pointer[execBinding]
}

// setDeclared arms the wrapper's declared-generation target (publish wiring
// only; never a hot path).
func (w *AgentToolWrapper) setDeclared(b *execBinding) {
	if w == nil || b == nil {
		return
	}
	w.declared.Store(b)
}

// declaredSet reports whether this wrapper was wired to a declared generation.
func (w *AgentToolWrapper) declaredSet() bool {
	return w != nil && w.declared.Load() != nil
}

// armDeclaredCall pins a sub-agent run to the execution view THIS wrapper's face
// declared: the child binding it routes to, not the caller's own generation. Every
// path that runs a child — Call and both re-entry closures — arms through here, so
// the view a run executes on is always the one the delegation resolved. Without it
// the child sees a lease that belongs to its owner, falls back to its birth config,
// and silently serves retired prompts, models and tools while the operator believes
// the current orchestration is in force.
//
// It returns the context carrying the declared-generation lease plus the release for
// that one reference. A wrapper never wired to a declared generation returns the
// context unchanged with a no-op release.
func (w *AgentToolWrapper) armDeclaredCall(ctx context.Context, agentName string) (context.Context, func(), error) {
	d := w.declared.Load()
	if d == nil {
		return ctx, func() {}, nil
	}
	dl, err := d.acquireDeclared(LeaseSubCall)
	if err != nil {
		return ctx, func() {}, fmt.Errorf("agent tool %q: declared generation unavailable: %w", agentName, err)
	}
	return dl.WithContext(ctx), dl.Release, nil
}

// autoInjectMaxEvents is the maximum number of recent events to auto-inject
// when LLM does not pass event_keys.
const autoInjectMaxEvents = 5

// subagentTTLParam is the Declarative.Params key under which a sub-agent's
// self-set lifetime (seconds, as decimal text) is persisted so the task-registry
// rebuild can replay the reaper anchor across a restart. It matches the command
// tool's ttl param key (tool/action pTTL = "ttl").
const subagentTTLParam = "ttl"

// NewAgentToolWrapper creates a new AgentToolWrapper.
// - ag: the sub-agent to wrap (must implement agent.Agent — local TagentAgent or remote A2AAgent)
// - desc: tool description shown to parent agent's LLM
// - eventParams: which event-derived parameters to declare and resolve
// - parentStore: parent agent's MemStore for resolving event_key to full event data
func NewAgentToolWrapper(
	ag agent.Agent,
	desc string,
	eventParams []string,
	parentStore memory.MemoryStore,
) *AgentToolWrapper {
	return &AgentToolWrapper{
		agent:       ag,
		desc:        desc,
		eventParams: eventParams,
		parentStore: parentStore,
	}
}

// SetParentProjection sets the PUBLISHED parentProjection used as the
// auto-inject fallback when a call runs outside any flow. It is a
// construction/publish-time wire (cold start, new generation) — /D2: an
// in-flight invocation never re-binds it, because one wrapper instance is shared
// by every concurrent call of its owner. While a flow is running, the flow's own
// projection wins (see projectionForCall).
func (w *AgentToolWrapper) SetParentProjection(p *compress.SessionProjection) {
	w.parentProjection = p
}

// SetParentCM wires the RESIDENT owner's context manager into a freshly built
// delegation wrapper (cold start, same point as SetParentProjection — /D2's
// allowed exception: a not-yet-published object is configured, a published one is
// never rewritten). It is what lets a stored task's re-entry resolve its target
// against the effective face instead of the instance that spawned it.
func (w *AgentToolWrapper) SetParentCM(cm *ContextManager) {
	w.parentCM = cm
}

// SetAsyncDisabled forces this sub-agent to always run synchronously (the
// fallback switch). By default sub-agent calls are async when a task spawner is
// present in the invocation context.
func (w *AgentToolWrapper) SetAsyncDisabled(disabled bool) {
	w.asyncDisabled = disabled
}

// SetAsyncDenseDuration overrides the dense phase for sub-agent async spawning
// (0 → detector default). Runs shorter than this settle inline; longer ones ack.
func (w *AgentToolWrapper) SetAsyncDenseDuration(d time.Duration) {
	w.asyncDenseDuration = d
}

// SetResumeContextRounds caps how many prior rounds the task-chain restorer
// injects on resume (0 → DefaultResumeContextRounds).
func (w *AgentToolWrapper) SetResumeContextRounds(n int) {
	w.resumeContextRounds = n
}

// SetExtraParams declares additional routing-level parameters for this tool
// (added to InputSchema; packed with request into a JSON message when present).
func (w *AgentToolWrapper) SetExtraParams(params []ExtraParam) {
	w.extraParams = params
}

func (w *AgentToolWrapper) effectiveResumeRounds() int {
	if w.resumeContextRounds > 0 {
		return w.resumeContextRounds
	}
	return DefaultResumeContextRounds
}

// SetDescriptionSource sets a hot-reloadable prompt source for the tool description.
// When set, Declaration() re-reads the description from disk on each call,
// detecting file changes via mtime. Falls back to static desc if source is nil or read fails.
func (w *AgentToolWrapper) SetDescriptionSource(src *prompt.Source) {
	w.descSource = src
}

// Declaration implements trpctool.Tool.
//
// The per-call execution-view arguments are declared on every delegation target:
// the override layer is a property of the call, not of one agent definition, and
// each argument takes effect for that call alone.
func (w *AgentToolWrapper) Declaration() *trpctool.Declaration {
	desc := w.desc
	if w.descSource != nil {
		if loaded, err := w.descSource.Get(); err == nil && loaded != "" {
			desc = loaded
		}
	}

	decl := &trpctool.Declaration{
		Name:        w.agent.Info().Name,
		Description: desc,
		InputSchema: &trpctool.Schema{
			Type:       "object",
			Properties: map[string]*trpctool.Schema{},
			Required:   []string{"request"},
		},
	}

	decl.InputSchema.Properties["request"] = &trpctool.Schema{
		Type:        "string",
		Description: "The request or instruction to process",
	}

	for _, p := range w.extraParams {
		if p.Name == "" || p.Name == "request" || p.Name == "event_keys" {
			continue
		}
		typ := p.Type
		if typ == "" {
			typ = "string"
		}
		schema := &trpctool.Schema{Type: typ, Description: p.Description}
		if len(p.Enum) > 0 {
			for _, e := range p.Enum {
				schema.Enum = append(schema.Enum, e)
			}
		}
		decl.InputSchema.Properties[p.Name] = schema
	}

	for _, param := range w.eventParams {
		switch param {
		case "event_key", "event_keys":
			decl.InputSchema.Properties["event_keys"] = &trpctool.Schema{
				Type:        "array",
				Description: "[LLM-selected] Array of event keys (canonical hex strings, exactly as shown in [evt_...] prefixes and archive cards) for related events from the conversation context. Pass them so the tool can retrieve full event details.",
				Items: &trpctool.Schema{
					Type: "string",
				},
			}
		}
	}

	decl.InputSchema.Properties["system_prompt_override"] = &trpctool.Schema{
		Type:        "string",
		Description: "Optional. System prompt replacing the delegate's own for THIS call only. It is not written onto the delegate: a later call without it is served the configured prompt.",
	}
	decl.InputSchema.Properties["model_override"] = &trpctool.Schema{
		Type:        "string",
		Description: "Optional. Registered model reference replacing the delegate's model for THIS call only. An unregistered reference is refused.",
	}
	decl.InputSchema.Properties["tools_subset"] = &trpctool.Schema{
		Type:        "array",
		Items:       &trpctool.Schema{Type: "string"},
		Description: "Optional. Tool names the delegate may use for THIS call only. Every entry must be one the delegate declares; an out-of-domain entry is refused, never dropped silently.",
	}
	decl.InputSchema.Properties["context_refs"] = &trpctool.Schema{
		Type:        "array",
		Items:       &trpctool.Schema{Type: "string"},
		Description: "Optional. Event keys whose archived content is handed to the delegate as external context for THIS call, beside event_keys.",
	}

	decl.InputSchema.Properties["ttl"] = &trpctool.Schema{
		Type:        "integer",
		Description: "ABSOLUTE lifetime of this sub-agent run in seconds. The unified reaper retires the task this long after spawn (fresh sub-agent runs are not reentrant, so this is not refreshed by later turns). 0 or omitted = configured default (10 minutes if unset); there is no way to disable the reaper. Raise it alongside the model's own pacing for long delegations.",
	}

	return decl
}

// defaultSubAgentTimeout is the default timeout for sub-agent calls.
//
// This is intentionally generous: a sub-agent's real work bound is its own
// max_tool_iterations (e.g. plan create runs self-check → init → new change →
// write proposal.md + tasks.md → validate over many rounds; a slow LLM like
// glm-5.2 takes ~15-25s/round). The timeout must NOT sever normal multi-round
// work — it is only a backstop against a truly runaway/stuck invocation.
const defaultSubAgentTimeout = 600 * time.Second

// isRemoteAgent checks if the wrapped agent is a remote A2AAgent.
func isRemoteAgent(ag agent.Agent) bool {
	return fmt.Sprintf("%T", ag) == "*a2aagent.A2AAgent"
}

// Call implements trpctool.CallableTool.
// It:
//  1. Parses JSON args to extract event_keys
//  2. If event_keys are present and parentStore is available, fetches full event data
//  3. Serializes the events into RuntimeState["external_context"] (compact JSON)
//  4. Constructs an Invocation and calls agent.Run with timeout — unified for local and remote
//  5. For remote A2A agents, retries once on failure with 500ms backoff
//  6. Collects the sub-agent's final output from the event stream
//
// context_refs resolves through the same retrieval channel and merges into the
// keys above, so override-supplied context travels the existing path.
func (w *AgentToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	agentName := w.agent.Info().Name

	armed, release, armErr := w.armDeclaredCall(ctx, agentName)
	if armErr != nil {
		return nil, armErr
	}
	defer release()
	ctx = armed

	// Parse args using json.Number to preserve int64 precision for Snowflake
	// event keys. Default json.Unmarshal parses numbers as float64, which
	// loses precision for keys larger than 2^53 (e.g., 1297371431025250304).
	var args map[string]interface{}
	if len(jsonArgs) > 0 {
		dec := json.NewDecoder(bytes.NewReader(jsonArgs))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			return nil, fmt.Errorf("agent tool %q: parse args: %w", agentName, err)
		}
	}

	request, _ := args["request"].(string)

	// Parse the optional self-set lifetime (seconds). json.Number preserves
	// int64 precision. Semantics mirror the command tool's ttl: >0 effective,
	// 0/omitted → configured default (reaper floor), negative → REJECTED here
	// before any spawn.
	var ttlSeconds int64
	if raw, ok := args["ttl"]; ok && raw != nil {
		n, isNum := raw.(json.Number)
		if !isNum {
			if f, isFloat := raw.(float64); isFloat {
				n = json.Number(strconv.FormatInt(int64(f), 10))
			} else {
				return nil, fmt.Errorf("agent tool %q: ttl must be an integer number of seconds", agentName)
			}
		}
		v, perr := n.Int64()
		if perr != nil {
			return nil, fmt.Errorf("agent tool %q: ttl must be an integer number of seconds: %w", agentName, perr)
		}
		if v < 0 {
			return nil, fmt.Errorf("agent tool %q: ttl must be >= 0 (0/omitted = configured default); got %d", agentName, v)
		}
		ttlSeconds = v
	}

	overrides, ovErr := w.parsePerCallOverrides(ctx, args)
	if ovErr != nil {
		return nil, ovErr
	}

	messageBody := request
	extraName := ""
	if len(w.extraParams) > 0 {
		packed := map[string]any{}
		for _, p := range w.extraParams {
			v, ok := args[p.Name]
			if !ok || v == nil {
				continue
			}
			packed[p.Name] = v
			if p.Name == "name" {
				if s, _ := v.(string); s != "" {
					extraName = s
				}
			}
		}
		if len(packed) > 0 {
			packed["request"] = request
			if data, err := json.Marshal(packed); err == nil {
				messageBody = string(data)
			}
		}
	}

	// Resolve event_keys → full event context
	var keys []int64
	var externalEvents []memory.FullEvent
	if w.parentStore != nil {
		if eventKeysRaw, ok := args["event_keys"]; ok {
			switch v := eventKeysRaw.(type) {
			case []interface{}:
				for _, item := range v {
					if key := toInt64Key(item); key > 0 {
						keys = append(keys, key)
					}
				}
			case float64:
				if key := toInt64Key(v); key > 0 {
					keys = append(keys, key)
				}
			}
		}

		if refsRaw, ok := args["context_refs"]; ok && refsRaw != nil {
			items, isArr := refsRaw.([]interface{})
			if !isArr {
				return nil, PerCallOverrideError{Field: "context_refs", Reason: "must be an array of event keys"}
			}
			seen := make(map[int64]bool, len(keys))
			for _, k := range keys {
				seen[k] = true
			}
			for _, item := range items {
				key := toInt64Key(item)
				if key == 0 {
					log.Warnf("[AgentToolWrapper] agent %q: context_refs entry %v is not a resolvable event key", agentName, item)
					continue
				}
				if !seen[key] {
					seen[key] = true
					keys = append(keys, key)
				}
			}
		}

		if len(keys) == 0 && w.hasEventKeysParam() {
			if proj := w.projectionForCall(ctx); proj != nil {
				keys = autoInjectEventKeys(proj)
				if len(keys) > 0 {
					log.Infof("[AgentToolWrapper] auto-injected %d event_keys for agent %q", len(keys), agentName)
				}
			}
		}

		for _, key := range keys {
			evt, err := w.parentStore.GetEvent(key)
			if err == nil && evt != nil {
				externalEvents = append(externalEvents, *evt)
			}
		}
	}

	log.Infof("[TRACE] tool_enter agent=%s request_len=%d event_keys=%d external_events=%d",
		agentName, len(request), len(keys), len(externalEvents))

	runOpts := agent.RunOptions{}
	if len(externalEvents) > 0 {
		serialized, err := serializeExternalContext(externalEvents)
		if err != nil {
			return nil, fmt.Errorf("agent tool %q: serialize external context: %w", agentName, err)
		}
		if runOpts.RuntimeState == nil {
			runOpts.RuntimeState = map[string]any{}
		}
		runOpts.RuntimeState[ExternalContextKey] = json.RawMessage(serialized)
	}
	if overrides != nil {
		data, err := json.Marshal(overrides)
		if err != nil {
			return nil, fmt.Errorf("agent tool %q: serialize per-call overrides: %w", agentName, err)
		}
		if len(data) > 0 {
			if runOpts.RuntimeState == nil {
				runOpts.RuntimeState = map[string]any{}
			}
			runOpts.RuntimeState[PerCallOverridesKey] = json.RawMessage(data)
		}
	}

	inv := agent.NewInvocation(
		agent.WithInvocationMessage(model.NewUserMessage(messageBody)),
		agent.WithInvocationRunOptions(runOpts),
	)

	if spawner, ok := task.TaskSpawnerFromContext(ctx); ok && !w.asyncDisabled {
		rounds := &subagentRounds{cap: w.effectiveResumeRounds()}
		callerLease, _ := execLeaseFromContext(ctx)
		bgLease := callerLease.Derive(LeaseBackground)
		detector := task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer bgLease.Release()
			out, err := w.runAndCollect(bgLease.WithContext(runCtx), inv, agentName)
			if err == nil {
				rounds.add(messageBody, out)
			}
			return out, err
		}, w.asyncDenseDuration)
		spawnKey := agentName + ":" + request
		if extraName != "" {
			spawnKey = agentName + ":" + extraName
		}
		// Self-set lifetime → TaskSpec.TTL (explicit only; 0 defers to the manager's
		// configured default / 10m floor, so the three-level chain is reused). The
		// ttl is persisted in Declarative.Params so RebuildTaskRegistry replays the
		// reaper anchor across a restart (SubagentSpecFromDeclarative reads it back).
		var specTTL time.Duration
		var declParams map[string]string
		if ttlSeconds > 0 {
			specTTL = time.Duration(ttlSeconds) * time.Second
			declParams = map[string]string{subagentTTLParam: strconv.FormatInt(ttlSeconds, 10)}
		}
		res := spawner.Spawn(task.TaskSpec{
			Kind: "subagent",
			Desc: agentName + ": " + truncate(request, 60),
			Key:  spawnKey,
			TTL:  specTTL,
			Declarative: &task.Declarative{
				Kind:        "subagent",
				Desc:        agentName + ": " + truncate(request, 60),
				Key:         spawnKey,
				AgentName:   agentName,
				MessageBody: request,
				Params:      declParams,
				Overrides:   overrides,
			},
			Relaunch: subagentRelaunchClosure(w.parentCM, spawner, inv, agentName, request, spawnKey, ttlSeconds, overrides),
			ResumeFn: subagentResumeClosureWithOverrides(w.parentCM, agentName, rounds, overrides),
		}, detector)
		if res.Blocked != "" {
			waitForUnadoptedStop(ctx, detector)
			return "子任务未被任务层纳管（已启动的后台运行已被取消跟踪，结果不会回写）：" + res.Blocked + "。可稍后重发。", nil
		}
		if res.Deduped {
			waitForUnadoptedStop(ctx, detector)
			return fmt.Sprintf("同名计划任务已在运行 (task %s)；请等待其 task_settled 结果，不要重复发起同名调用。",
				res.Task.ID), nil
		}
		if res.Settled {
			if res.Signal.Err != nil {
				return nil, fmt.Errorf("agent tool %q: run failed: %w", agentName, res.Signal.Err)
			}
			return res.Signal.Output, nil
		}
		return fmt.Sprintf("子 agent %q 已在后台运行 (task %s)；完成后其结果会作为 task_settled 回写。",
			agentName, res.Task.ID), nil
	}

	out, err := w.runAndCollect(ctx, inv, agentName)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// waitForUnadoptedStop honours  for a detector whose producer had ALREADY
// started when the task layer refused to adopt it (spawn gate blocked, or a
// same-key dedup hit). Cancel is only a notification, so the derived execution
// reference this call created must not be dropped until the producer really
// returns. The wait is bounded by the caller's own context — and if that deadline
// arrives first, the reference stays held (the producer closure still owns its
// release), which is the safe-retention outcome, never a premature reclaim.
func waitForUnadoptedStop(ctx context.Context, d task.SettleDetector) {
	if d == nil {
		return
	}
	select {
	case <-d.Stopped():
	case <-ctx.Done():
		log.Warnf("[AgentToolWrapper] un-adopted sub-run still active at the caller's deadline; its generation reference stays held until the producer stops")
	}
}

// RedispatchAsync runAndCollect runs the sub-agent for the given invocation and collects its
// final output from the event stream. Shared by the synchronous path and the
// async task detector. Isolation is preserved by Run (fresh bus/CM/projection
// per invocation), so this is safe to run concurrently / in a background task.
// RedispatchAsync：跨重启 subagent relaunch
// 的重投递入口——以原 request 重新走 Call 的完整 spawn 路径（声明了 extra
// params 时按无参调用降级：plan-name 等 extra 参数不跨重启保留，已知边界）。
// RedispatchAsync re-enters the delegate with the stored body AND the overrides
// frozen on the task: the same argument keys a live caller would send, so
// parsePerCallOverrides validates and applies them through the single parse
// point — a rebuilt-then-relaunched task runs on the same view the original
// call assembled, never a silently de-overrideed one.
func (w *AgentToolWrapper) RedispatchAsync(ctx context.Context, request string, overrides *task.Overrides) (any, error) {
	args := map[string]any{"request": request}
	if overrides != nil {
		if overrides.SystemPrompt != "" {
			args["system_prompt_override"] = overrides.SystemPrompt
		}
		if overrides.ModelRef != "" {
			args["model_override"] = overrides.ModelRef
		}
		if overrides.ToolsSubset != nil {
			args["tools_subset"] = overrides.ToolsSubset
		}
	}
	b, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return w.Call(ctx, b)
}

// DeclaredAgentName reports the wrapped sub-agent's name. It is the SAME string
// the spawn path persists into a task's Declarative.AgentName (Call derives that
// field from `w.agent.Info().Name`), so it is the one key that both the effective-
// generation routing table and a cross-restart relaunch can resolve against —
// deliberately not Declaration().Name, which may re-read a description source.
// Cheap, and safe to call while holding executor locks.
func (w *AgentToolWrapper) DeclaredAgentName() string {
	return w.agent.Info().Name
}

// DenseDuration exposes the async dense-window length (R2 redispatch detector
// shape parity with subagentRelaunch).
func (w *AgentToolWrapper) DenseDuration() time.Duration {
	return w.asyncDenseDuration
}

// remoteRetryBackoff is the wait before the single retry of a failed REMOTE
// delegation. Local targets are never retried: their failure is a defect in this
// process, and re-running it only hides it.
const remoteRetryBackoff = 500 * time.Millisecond

// runAndCollect runs one delegation and collects its result, retrying a failed REMOTE attempt
// exactly once: the retry wraps the whole attempt, send plus drain, because that is
// where a transport failure actually surfaces.
//
// - Both attempts use the same invocation and the same wrapper instance, i.e. the target the initiating call was bound to: a retry rides the initiating call lease binding, never a re-resolution against whatever generation is published by then.
func (w *AgentToolWrapper) runAndCollect(ctx context.Context, inv *agent.Invocation, agentName string) (string, error) {
	out, err := w.collectAttempt(ctx, inv, agentName)
	if err == nil || !isRemoteAgent(w.agent) {
		return out, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return out, err
	}
	log.Warnf("[AgentToolWrapper] remote agent %q attempt failed (%v), retrying once in %v",
		agentName, err, remoteRetryBackoff)
	select {
	case <-time.After(remoteRetryBackoff):
	case <-ctx.Done():
		return out, err
	}
	return w.collectAttempt(ctx, inv, agentName)
}

// collectAttempt is ONE send-and-drain attempt of a sub-agent delegation.
func (w *AgentToolWrapper) collectAttempt(ctx context.Context, inv *agent.Invocation, agentName string) (string, error) {
	startTime := time.Now()

	eventCh, err := w.runWithTimeout(ctx, inv, agentName)
	if err != nil {
		return "", fmt.Errorf("agent tool %q: run failed: %w", agentName, err)
	}

	// Collect the final output from the sub-agent
	var finalOutput string
	var toolCallCount int
	var lastErr *model.ResponseError
	for evt := range eventCh {
		var resp *model.Response
		if evt.Response != nil {
			resp = evt.Response.Clone()
		}
		if resp != nil && resp.Error != nil {
			lastErr = resp.Error
			log.Warnf("[ToolAgent:%s] upstream model error: type=%s message=%s", agentName, resp.Error.Type, resp.Error.Message)
			continue
		}
		if resp != nil && len(resp.Choices) > 0 {
			choice := resp.Choices[len(resp.Choices)-1]
			if len(choice.Message.ToolCalls) > 0 {
				toolCallCount++
				var names []string
				for _, tc := range choice.Message.ToolCalls {
					names = append(names, fmt.Sprintf("%s(%s)", tc.Function.Name, truncate(string(tc.Function.Arguments), 80)))
				}
				log.Infof("[ToolAgent:%s] round %d tool call: %s", agentName, toolCallCount, strings.Join(names, ", "))
			}
			if choice.Message.Role == model.RoleTool && choice.Message.Content != "" {
				log.Debugf("[ToolAgent:%s] round %d tool response: %s", agentName, toolCallCount, truncate(choice.Message.Content, 120))
			}
			if choice.Message.Content != "" && len(choice.Message.ToolCalls) == 0 {
				finalOutput = choice.Message.Content
			}
		}
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Infof("[TRACE] tool_exit agent=%s output_len=%d tool_calls=%d elapsed=%v",
		agentName, len(finalOutput), toolCallCount, elapsed)

	if finalOutput == "" && lastErr != nil {
		return "", fmt.Errorf("agent tool %q: upstream model error (%s): %s", agentName, lastErr.Type, lastErr.Message)
	}
	if finalOutput == "" {
		finalOutput = "tool agent completed without output"
	}

	return finalOutput, nil
}

// subagentRelaunchClosure builds the relaunch closure of a subagent task. It is a
// free function on purpose: its
// captured environment holds a RESIDENT owner handle plus plain data, never an
// executable wrapper. A stored task can be re-entered long after the generation
// that spawned it was superseded, and running the spawn-time instance would revive
// a retired routing decision (with its stale declaration parameters) while the
// operator believes the current orchestration is in force. The target is therefore
// resolved at re-entry time — see ResolveReentryDelegation.
//
// The re-spawned task is itself relaunchable and keeps the SAME idempotency key as
// the original spawn (name-based when a plan name was declared — D4 single-flight
// covers relaunch rounds too, not just the first spawn).
//
//   - The owner-face lease accounts for the re-entry while armDeclaredCall selects the execution view it runs on, so a stored task cannot silently serve a retired generation's prompts, model or tools.
func subagentRelaunchClosure(owner *ContextManager, spawner task.TaskSpawner, inv *agent.Invocation, agentName, request, spawnKey string, ttlSeconds int64, overrides *task.Overrides) func(context.Context) (task.SpawnResult, error) {
	return func(ctx context.Context) (task.SpawnResult, error) {
		target, lease, err := ResolveReentryDelegation(ctx, owner, agentName)
		if err != nil {
			return task.SpawnResult{}, err
		}
		detector := task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer lease.Release()
			callCtx, release, err := target.armDeclaredCall(lease.WithContext(runCtx), agentName)
			if err != nil {
				return "", err
			}
			defer release()
			return target.runAndCollect(callCtx, inv, agentName)
		}, target.DenseDuration())
		var specTTL time.Duration
		var declParams map[string]string
		if ttlSeconds > 0 {
			specTTL = time.Duration(ttlSeconds) * time.Second
			declParams = map[string]string{subagentTTLParam: strconv.FormatInt(ttlSeconds, 10)}
		}
		res := spawner.Spawn(task.TaskSpec{
			Kind: "subagent",
			Desc: agentName + ": " + truncate(request, 60),
			Key:  spawnKey,
			TTL:  specTTL,
			Declarative: &task.Declarative{
				Kind:        "subagent",
				Desc:        agentName + ": " + truncate(request, 60),
				Key:         spawnKey,
				AgentName:   agentName,
				MessageBody: request,
				Params:      declParams,
				Overrides:   overrides,
			},
			Relaunch: subagentRelaunchClosure(owner, spawner, inv, agentName, request, spawnKey, ttlSeconds, overrides),
		}, detector)
		if (res.Blocked != "" || res.Deduped) && hasInitiator(ctx) {
			waitForUnadoptedStop(ctx, detector)
		}
		return res, nil
	}
}

// hasInitiator ResolveReentryDelegation picks the delegation target a Resume/Relaunch re-entry
// runs on: a re-entry riding an initiating call inherits THAT
// call's binding (its own generation's face and its resource reference), while a
// re-entry with no initiator (console/WAL/ops path) acquires the current effective
// generation. A target the selected version does not route is REFUSED, never
// silently re-routed onto another generation and never revived from the instance
// the task happened to be spawned by.
//
// The returned lease is the reference taken on the selected generation BEFORE the
// re-entered work starts (D6「派生前获取」); the caller owns exactly one Release.
// hasInitiator reports a re-entry that rides a live call — the same predicate
// ResolveReentryDelegation uses to pick the version source, so "who vouches for
// this re-entry" is answered one way everywhere.
func hasInitiator(ctx context.Context) bool {
	_, ok := execLeaseFromContext(ctx)
	return ok
}

// ResolveReentryDelegation 为一次重入（存储任务的 Resume/Relaunch）解析委派目标，并返回随附的子调用租约。
//
// - 上下文有发起方租约时按其同一代解析；目标不在该代的编排里就直接报错，绝不悄悄改投当前生效代。
// - 无租约时退回属主常驻面，在其当前生效代上取租约；该代已收敛关闭则拒绝。
// - 解析失败必须释放刚取的租约，不留悬挂引用；调用方负责释放所得租约。
// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
func ResolveReentryDelegation(ctx context.Context, owner *ContextManager, agentName string) (*AgentToolWrapper, *ExecLease, error) {
	if l, ok := execLeaseFromContext(ctx); ok && l != nil {
		if t := l.SubagentWrapper(agentName); t != nil {
			return t, l.Derive(LeaseSubCall), nil
		}
		return nil, nil, fmt.Errorf(
			"subagent %q is not a target of generation %d (the orchestration version the initiating call holds) — relaunch refused, and the newer face is not substituted for it",
			agentName, l.Generation())
	}
	if owner == nil {
		return nil, nil, fmt.Errorf(
			"subagent %q cannot be resolved for re-entry: this delegation carries no resident owner to read the effective orchestration from — relaunch refused", agentName)
	}
	l := owner.AcquireLease(LeaseSubCall)
	if err := l.Err(); err != nil {
		return nil, nil, fmt.Errorf(
			"subagent %q re-entry refused: its owner generation already converged shut: %w", agentName, err)
	}
	if t := l.SubagentWrapper(agentName); t != nil {
		return t, l, nil
	}
	l.Release()
	return nil, nil, fmt.Errorf(
		"subagent %q is not a target of the EFFECTIVE orchestration generation — relaunch refused (a retired binding is not revived, and the task chain context is kept untouched)", agentName)
}

// subagentRounds is the task-local round chain: the {input, output} pairs of
// this task's settled rounds, shared across resume rounds via closure. cap
// bounds how many rounds are restored (and, with headroom, retained).
type subagentRounds struct {
	mu     sync.Mutex
	cap    int
	rounds []subagentRound
}

type subagentRound struct {
	input  string
	output string
	at     time.Time
}

func (r *subagentRounds) add(input, output string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rounds = append(r.rounds, subagentRound{input: input, output: output, at: time.Now()})
	limit := r.cap
	if limit <= 0 {
		limit = DefaultResumeContextRounds
	}
	if len(r.rounds) > limit*4 {
		r.rounds = append([]subagentRound(nil), r.rounds[len(r.rounds)-limit*2:]...)
	}
}

func (r *subagentRounds) recent(n int) []subagentRound {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rounds) <= n {
		return append([]subagentRound(nil), r.rounds...)
	}
	return append([]subagentRound(nil), r.rounds[len(r.rounds)-n:]...)
}

// subagentResumeClosure builds the resume closure of a subagent task.
//
// - 目标与目标自己的声明参数（restoration cap、dense window）都来自重入所选的那一代，因此存量任务不会重放已退役代的参数；所选代无目标则在读写任务链之前拒绝，被拒的重入不留任务链改动。
// - 返回的是新的单轮 Run，其 external_context 只携带本任务的历史轮次（最近的结算结果在最前），不含无关任务。
// - 子 agent 仍是单轮原语，不做进程复活：状态恢复是框架侧的喂入，不是子 agent 的持续性。
func subagentResumeClosure(owner *ContextManager, agentName string, rounds *subagentRounds) func(context.Context, string) (task.SettleDetector, error) {
	return subagentResumeClosureWithOverrides(owner, agentName, rounds, nil)
}

// subagentResumeClosureWithOverrides builds the resume closure of a subagent
// task, replaying the per-call overrides the original delegation carried. A
// resumed round runs on the same view as the round it continues — the overrides
// are part of that task's identity, not of the generation it happens to land on.
func subagentResumeClosureWithOverrides(owner *ContextManager, agentName string, rounds *subagentRounds, overrides *task.Overrides) func(context.Context, string) (task.SettleDetector, error) {
	return func(ctx context.Context, input string) (task.SettleDetector, error) {
		target, lease, err := ResolveReentryDelegation(ctx, owner, agentName)
		if err != nil {
			return nil, err
		}
		prior := rounds.recent(target.effectiveResumeRounds())
		if len(prior) == 0 {
			lease.Release()
			return nil, fmt.Errorf("subagent task has no settled round to resume from — use relaunch_task or a fresh call")
		}

		restored := make([]memory.FullEvent, 0, len(prior))
		for _, rd := range prior {
			restored = append(restored, memory.FullEvent{
				EventType: "task_round",
				EventSummary: fmt.Sprintf("〔本任务上一轮〕指令: %s\n结果: %s",
					truncate(rd.input, 200), rd.output),
				Timestamp: rd.at.UnixMilli(),
			})
		}
		serialized, err := serializeExternalContext(restored)
		if err != nil {
			lease.Release()
			return nil, fmt.Errorf("serialize task-chain context: %w", err)
		}
		runOpts := agent.RunOptions{RuntimeState: map[string]any{
			ExternalContextKey: json.RawMessage(serialized),
		}}
		if overrides != nil {
			if data, err := json.Marshal(overrides); err == nil && len(data) > 0 {
				runOpts.RuntimeState[PerCallOverridesKey] = json.RawMessage(data)
			}
		}
		inv := agent.NewInvocation(
			agent.WithInvocationMessage(model.NewUserMessage(input)),
			agent.WithInvocationRunOptions(runOpts),
		)
		return task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer lease.Release()
			callCtx, release, armErr := target.armDeclaredCall(lease.WithContext(runCtx), agentName)
			if armErr != nil {
				return "", armErr
			}
			defer release()
			out, err := target.runAndCollect(callCtx, inv, agentName)
			if err == nil {
				rounds.add(input, out)
			}
			return out, err
		}, target.DenseDuration()), nil
	}
}

// runWithTimeout starts ONE sub-agent run under a context timeout and keeps the
// context alive for the whole consumption of the returned stream.
//
// - agent.Run 是异步的，立即返回 channel；cancel 不得 defer 在本函数，否则会赶在调用方读完 channel 之前取消上下文，因此把 channel 包一层 goroutine，待其关闭后再调用 cancel。
// - 重试策略在上一层 runAndCollect：远程传输失败通常以 error 事件（Run 返回 nil + channel）到达，本函数内的 error 分支覆盖不到那种形状。
func (w *AgentToolWrapper) runWithTimeout(ctx context.Context, inv *agent.Invocation, agentName string) (<-chan *event.Event, error) {
	runCtx, cancel := context.WithTimeout(ctx, defaultSubAgentTimeout)

	eventCh, err := w.agent.Run(runCtx, inv)
	if err != nil {
		cancel()
		return nil, err
	}

	wrapped := make(chan *event.Event, cap(eventCh))
	go func() {
		defer cancel()
		defer close(wrapped)
		for evt := range eventCh {
			wrapped <- evt
		}
	}()
	return wrapped, nil
}

// hasEventKeysParam checks if eventParams includes "event_keys" or "event_key".
func (w *AgentToolWrapper) hasEventKeysParam() bool {
	for _, p := range w.eventParams {
		if p == "event_key" || p == "event_keys" {
			return true
		}
	}
	return false
}

// projectionForCall 解析本次委托自动注入所用的投影。
//
// 外层调用自己的投影（由 RunFlow 绑到调用链上下文上）才是权威：wrapper 本身是**共享的已发布
// 对象**，同一实例服务其属主的每一次并发调用，因此它自己的发布绑定单凭自身不可能对调用正确。
// 在流之外（直接工具调用或兼容入口）才退回 SetToolParentProjection 设置的构造期绑定。
func (w *AgentToolWrapper) projectionForCall(ctx context.Context) *compress.SessionProjection {
	if p, ok := callProjectionFromContext(ctx); ok {
		return p
	}
	return w.parentProjection
}

// autoInjectEventKeys returns the most recent N EventKeys from proj.
// Skips EventKey == 0. Returns nil if proj is nil or empty.
func autoInjectEventKeys(proj *compress.SessionProjection) []int64 {
	if proj == nil {
		return nil
	}
	refs := proj.GetAll()
	if len(refs) == 0 {
		return nil
	}
	start := 0
	if len(refs) > autoInjectMaxEvents {
		start = len(refs) - autoInjectMaxEvents
	}
	var keys []int64
	for _, ref := range refs[start:] {
		if ref.EventKey > 0 {
			keys = append(keys, ref.EventKey)
		}
	}
	return keys
}

// ToolAgentFactory assembles a tool agent execution configuration from the given
// inputs. It must NOT construct the agent itself.
//
// - org 拥有唯一的出生路径与唯一的发布路径；返回成品 agent 会造出面路径之外的第二种 owner 出生，每次发布都得重建整个 agent 并留下无人关闭的孤儿。
// - 返回的 TagentConfig 逐字采纳有意义字段：Name 为空回退注册 id，MemoryStore 填充该 name 借用的 org store，MemStoreRelease 恒由后续装配填 org 的那一个。
// - 装配为每一代重新调用工厂并交给那一代的值，由这些值推导的声明随配置移动。
// 契约: docs/wiki/tool/tool-architecture.md#tool-agent-factory
type ToolAgentFactory func(cfg ToolAgentFactoryConfig) (*TagentConfig, error)

// ToolAgentFactoryConfig provides everything a factory needs to produce the agent's
// configuration declaration (it does NOT construct a TagentAgent — see ToolAgentFactory).
//
// In the new architecture, each tool agent has its own isolated MemStore.
// The parent agent's MemStore is NOT passed here — context is delivered via
// the AgentToolWrapper's event_key resolution at call time.
type ToolAgentFactoryConfig struct {
	// ID is the tool agent identifier (e.g., "knowledge", "recall")
	ID string

	// Model is the LLM model for the tool agent (resolved from config)
	Model model.Model

	// SystemPrompt is the loaded system prompt (already resolved from PromptConfig)
	SystemPrompt string

	// Description is the tool description shown to the parent agent's LLM
	Description string

	// SubTools are the pre-built sub-tools for this agent
	SubTools []trpctool.Tool

	// MemoryStore is the tool agent's own memory store (isolated from parent).
	// The factory should use this (or create its own) for the agent's internal storage.
	// Context from the parent is delivered via AgentToolWrapper at call time, not via MemStore.
	MemoryStore memory.MemoryStore

	// ReadPartitionIDs lists PartitionIDs this agent is allowed to read in addition
	// to its own namespace. Injected from MemoryConfig.ReadNamespaces at build time.
	// Used by recall agent's sub-tools to query across agent partitions.
	ReadPartitionIDs []int

	// SkillRepo is the skill repository for knowledge agent (optional).
	SkillRepo tagenttool.SkillRepository

	// MCPToolSets 是用于工具发现的 MCP 工具来源（可选）。
	MCPToolSets []trpctool.ToolSet

	// MCPRegistry is the live MCP server registry (preferred over
	// MCPToolSets): reads reflect runtime registration and config hot-sync.
	MCPRegistry tagenttool.MCPRegistry

	// MaxToolIterations Agent parameters
	MaxToolIterations int
	MaxTokens         int
	Temperature       float64

	// ThinkingEnabled Thinking/reasoning controls
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string
}

var (
	toolAgentFactories   = map[string]ToolAgentFactory{}
	toolAgentFactoriesMu sync.RWMutex
)

// RegisterToolAgent registers a factory for creating tool agents by ID. Registering the
// same ID twice panics, so a caller that re-registers on every run — a test under a
// -count>1 repetition gate, for example — must use a unique ID or reset the registry.
func RegisterToolAgent(id string, factory ToolAgentFactory) {
	toolAgentFactoriesMu.Lock()
	defer toolAgentFactoriesMu.Unlock()

	if _, exists := toolAgentFactories[id]; exists {
		panic(fmt.Sprintf("tool agent factory %q already registered", id))
	}
	toolAgentFactories[id] = factory
}

// GetToolAgentFactory returns the factory for the given ID.
func GetToolAgentFactory(id string) (ToolAgentFactory, bool) {
	toolAgentFactoriesMu.RLock()
	defer toolAgentFactoriesMu.RUnlock()

	f, ok := toolAgentFactories[id]
	return f, ok
}

// PlainToolFactory creates a plain tool (implements tool.CallableTool) from the given config.
type PlainToolFactory func(cfg PlainToolFactoryConfig) (trpctool.CallableTool, error)

// PlainToolFactoryConfig provides everything a factory needs to create a plain tool.
type PlainToolFactoryConfig struct {
	ID          string
	Description string
	Properties  map[string]any

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Tools that need a working/output directory derive it from here (e.g. the
	// action/exec tool uses <root>/exec as its tmux command working directory).
	WorkspaceRoot string

	// WorkingDir 是 agent 的统一工作根(file tools 的 base_dir 与 exec 命令 cwd 的共同基准)。
	// 空 = 各工具回退自身默认(file base_dir="."、exec 继承进程 cwd,现状不变);非空则作为二者
	// 共同根,优先级仍低于 ToolRef.properties 的显式 base_dir/workspace。由 config.WorkingDir 注入。
	WorkingDir string

	// MemStore Runtime dependencies (optional, injected by buildAgent).
	// Most plain tools (e.g., exec) ignore these fields.
	// Sub-tools that need runtime objects (e.g., skill_search needs SkillRepo,
	// memory_query needs MemStore) extract them from here.
	MemStore         memory.MemoryStore
	SkillRepo        tagenttool.SkillRepository
	MCPToolSets      []trpctool.ToolSet
	MCPRegistry      tagenttool.MCPRegistry
	ReadPartitionIDs []int

	// Degradation 是 per-agent 五依赖退化状态机（T-G，可选）。mcp_call 工具据此上报 DepMCP
	// 退化（MCP server 连续失败→degraded，成功→恢复）。nil = 未启用退化追踪（现状）。
	Degradation *reliability.DegradationManager

	// MCPProbeEvery：DepMCP degraded 时 mcp_call 的
	// 熔断半开探测间隔（每 N 次放行 1 次）。0 = 关闭。由 buildPlainToolRef 从 agent
	// DegradationBehaviors 注入。
	MCPProbeEvery int

	// ConsolidationMinSources：memory_consolidate
	// 的 min_source_events 硬门控（实际取回源不足即拒绝）。0 = 不校验。由
	// buildPlainToolRef 从该 agent 的 memory.engine.consolidation 注入。
	ConsolidationMinSources int
}

var (
	plainToolFactories   = map[string]PlainToolFactory{}
	plainToolFactoriesMu sync.RWMutex
)

// RegisterPlainTool registers a factory for creating plain tools by ID. Registering the
// same ID twice panics, so a caller that re-registers on every run — a test under a
// -count>1 repetition gate, for example — must use a unique ID or reset the registry.
func RegisterPlainTool(id string, factory PlainToolFactory) {
	plainToolFactoriesMu.Lock()
	defer plainToolFactoriesMu.Unlock()

	if _, exists := plainToolFactories[id]; exists {
		panic(fmt.Sprintf("plain tool factory %q already registered", id))
	}
	plainToolFactories[id] = factory
}

// GetPlainToolFactory returns the factory for the given ID.
func GetPlainToolFactory(id string) (PlainToolFactory, bool) {
	plainToolFactoriesMu.RLock()
	defer plainToolFactoriesMu.RUnlock()

	f, ok := plainToolFactories[id]
	return f, ok
}

var (
	modelReferences   = map[string]model.Model{}
	modelReferencesMu sync.RWMutex
)

// ReservedModelRefPrefix marks the reference name under which a config-driven
// instance's ACTUAL model is addressable inside its own execution view.
const ReservedModelRefPrefix = "agent:"

// ReservedModelRef is the reference name of the model a config-driven instance was
// assembled with: `agent:<name>`. It belongs to the execution view rather than to
// the process registry — nobody has to publish it, and it resolves to whatever
// model the selected generation actually runs. A candidate whose registry already
// claims that name for a DIFFERENT instance is refused by name instead of silently
// being served either side. An empty agent name has no reserved name.
func ReservedModelRef(agentName string) string {
	if agentName == "" {
		return ""
	}
	return ReservedModelRefPrefix + agentName
}

// ModelRefSnapshot is the read-only model-reference set of ONE execution view: the
// names currently published in the process registry plus that view's own model
// under ReservedModelRef. It is built when the view is selected (a candidate's
// construction, or once at the entry of a standalone call that was never staged),
// is never written afterwards, and dies with the generation holding it — so
// resolving one name twice inside one call cannot return two instances, and no
// model pointer outlives its generation or reaches any durable record.
type ModelRefSnapshot struct {
	refs map[string]model.Model
}

// NewModelRefSnapshot freezes the reference set of the view whose model is m under
// agent identity agentName: every name the registry publishes now, plus
// `agent:<agentName>` → m.
//
// A registry entry already occupying the reserved name with a different instance is
// a conflict, refused BY NAME. Because this constructor only ever reads the
// registry, a candidate that fails here leaves the process-wide table exactly as it
// was — it neither re-points nor drops the entry it refused to adopt. agentName ==
// "" or m == nil means the view has no model of its own to claim (a remote target,
// a face assembled without a model), so no reserved name is added and registry
// names resolve as usual.
func NewModelRefSnapshot(agentName string, m model.Model) (*ModelRefSnapshot, error) {
	modelReferencesMu.RLock()
	refs := make(map[string]model.Model, len(modelReferences)+1)
	for k, v := range modelReferences {
		refs[k] = v
	}
	modelReferencesMu.RUnlock()

	reserved := ReservedModelRef(agentName)
	if reserved == "" || m == nil {
		return &ModelRefSnapshot{refs: refs}, nil
	}
	if claimed, ok := refs[reserved]; ok && !sameModelHandle(claimed, m) {
		return nil, PerCallOverrideError{Field: "model_override", Offenders: []string{reserved},
			Reason: "the reserved name of a config-driven model is already claimed by a different registered instance"}
	}
	refs[reserved] = m
	return &ModelRefSnapshot{refs: refs}, nil
}

// Resolve reports the instance a reference names inside THIS frozen view. A name
// the view does not carry is a miss, never a fallback to some other model.
func (s *ModelRefSnapshot) Resolve(ref string) (model.Model, bool) {
	if s == nil || ref == "" {
		return nil, false
	}
	m, ok := s.refs[ref]
	return m, ok
}

// modelRefsOfView is the reference set an assembly resolves against: the pinned
// snapshot of the generation the call was selected on when one is handed over,
// otherwise the snapshot of the very view being assembled here.
func modelRefsOfView(cfg *TagentConfig, pinned []*ModelRefSnapshot) (*ModelRefSnapshot, error) {
	for _, p := range pinned {
		if p != nil {
			return p, nil
		}
	}
	if cfg == nil {
		return NewModelRefSnapshot("", nil)
	}
	return NewModelRefSnapshot(cfg.Name, cfg.Model)
}

// resolveModelReference is the ONE place a per-call model reference becomes a model
// instance. It reads only the frozen view it was handed and never goes back to the
// mutable registry, so a call that resolved its reference at the argument-checking
// point and assembles off the same view runs on the instance validation answered
// with — and a name that view does not carry is refused at both points alike.
func resolveModelReference(ref string, view *ModelRefSnapshot) (model.Model, error) {
	m, ok := view.Resolve(ref)
	if !ok {
		return nil, PerCallOverrideError{Field: "model_override", Offenders: []string{ref},
			Reason: "not a model reference of the selected execution view"}
	}
	return m, nil
}

// sameModelHandle compares two model handles by IDENTITY, not by value: two
// instances built from the same provider configuration are deliberately not the
// same model. A handle that is neither comparable nor a pointer cannot prove
// identity, so it is reported as different — the safe reading of a reserved-name
// claim, which then refuses instead of pretending the two are interchangeable.
func sameModelHandle(a, b model.Model) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta.Comparable() {
		return a == b
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Kind() == reflect.Pointer && vb.Kind() == reflect.Pointer {
		return va.Pointer() == vb.Pointer()
	}
	return false
}

// RegisterModelReference publishes a model instance under a reference name, the
// target form a per-call model_override addresses. Re-registering a name
// replaces its instance: a reload re-points a reference, and a lookup returns
// either the previous or the new instance, never a torn one.
//
// This standalone publish/lookup pair stays exactly as it is: an execution view
// DERIVES a read-only snapshot from it (NewModelRefSnapshot) instead of keeping a
// second table, and a config-driven instance's own model needs no registration at
// all — inside its own view it is addressable as ReservedModelRef(name).
func RegisterModelReference(ref string, m model.Model) {
	if ref == "" || m == nil {
		return
	}
	modelReferencesMu.Lock()
	defer modelReferencesMu.Unlock()
	modelReferences[ref] = m
}

// LookupModelReference resolves a per-call model_override reference to the
// instance registered under it. An unresolvable reference is refused by the
// caller, never served by a fallback model.
func LookupModelReference(ref string) (model.Model, bool) {
	modelReferencesMu.RLock()
	defer modelReferencesMu.RUnlock()

	m, ok := modelReferences[ref]
	return m, ok
}

// toInt64Key converts a JSON-parsed value to an int64 event key.
// Handles:
// toInt64Key converts an event-key argument to int64. Keys are canonically
// HEX strings (the [evt_...] timeline-form hex
// contract) — parsed via event.ParseEventKey. Numeric forms are kept for
// backward compatibility with models that echo keys as numbers.
func toInt64Key(v interface{}) int64 {
	switch val := v.(type) {
	case json.Number:
		i, err := val.Int64()
		if err == nil {
			return i
		}
	case string:
		s := strings.TrimPrefix(strings.TrimSpace(val), "evt_")
		if k, err := tagentevent.ParseEventKey(s); err == nil && k != 0 {
			return k
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i
		}
	case float64:
		return int64(val)
	}
	return 0
}

// truncate truncates a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

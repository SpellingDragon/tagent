// Package agent provides tool agent registration for extensible agent composition.
//
// Tool agents are TagentAgent instances wrapped as CallableTool via AgentToolWrapper.
// This file provides the registration mechanism and the wrapper implementation.
//
// Registration flow:
//
//  1. Built-in factories are registered in tagent/builtin.go init()
//  2. Custom factories can be registered via RegisterToolAgent()
//  3. tagent.New() resolves ToolRef entries by building referenced agents
//
// AgentToolWrapper replaces the previous agenttool.NewTool() approach.
// It handles:
//   - Declaring event_key parameter in InputSchema (when EventParams includes it)
//   - Resolving event_key → fetching full event from parent MemStore
//   - Passing event data as external context to the sub-agent
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// ==================== External Context Serialization ====================
//
// ExternalContextEntry is the wire format for passing external event context
// across process boundaries (local → RuntimeState → A2A metadata → RuntimeState → remote).
//
// Only EventKey, EventType, and EventSummary are serialized — NOT the full Content.
// This keeps the payload compact (suitable for A2A metadata size limits) while
// preserving the information that injectExternalContext actually uses.
// Remote sub-agents that need full event content can query their own MemoryStore
// using the EventKey.

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

// ==================== AgentToolWrapper ====================
//
// AgentToolWrapper wraps an agent.Agent (local TagentAgent or remote A2AAgent)
// as a plain CallableTool. It handles:
//
//   - InputSchema declares event_keys parameter (list of Snowflake EventKeys)
//   - On Call: extracts event_keys from args, fetches full events from parent MemStore,
//     serializes them into RuntimeState["external_context"], and calls agent.Run
//   - The LLM selects relevant event_keys from its context and passes them to the tool,
//     enabling the tool to retrieve full event details that were compressed away
//   - This prevents the LLM from breaking context isolation — the LLM only outputs
//     numeric keys, but the actual event content is resolved server-side
//   - Context delivery is unified: RuntimeState works for both local (direct Run)
//     and remote (A2A metadata auto-mapping) sub-agents

// ExtraParam declares one additional routing-level parameter for an
// agent-kind tool (plan-interaction-contract D2). Declared params are added
// to the tool's InputSchema and, when present in a call, packed together
// with request into a JSON message body — a whitelist pass-through for small
// routing fields (e.g. plan's action/name), NOT a general RPC channel.
type ExtraParam struct {
	Name        string   `json:"name" yaml:"name"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty"` // default "string"
	Enum        []string `json:"enum,omitempty" yaml:"enum,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
}

type AgentToolWrapper struct {
	agent            agent.Agent // unified: *TagentAgent (local) or *a2aagent.A2AAgent (remote)
	desc             string
	descSource       *prompt.Source              // Hot-reloadable description source (optional)
	eventParams      []string                    // Which event-derived params to declare (e.g., "event_key")
	parentStore      memory.MemoryStore          // Parent agent's MemStore for resolving event_key
	parentProjection *compress.SessionProjection // Parent agent's projection for auto-inject fallback

	// parentCM is the RESIDENT owner's context manager — the handle a re-entry
	// (Resume/Relaunch of a stored task) reads the CURRENT effective orchestration
	// face from when no initiating call holds a binding (§4.2/D5). It is wired at
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
	// (plan-interaction-contract D2). Empty → plain-text request messages,
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

// autoInjectMaxEvents is the maximum number of recent events to auto-inject
// when LLM does not pass event_keys.
const autoInjectMaxEvents = 5

// subagentTTLParam is the Declarative.Params key under which a sub-agent's
// self-set lifetime (seconds, as decimal text) is persisted so the task-registry
// rebuild can replay the reaper anchor across a restart. It matches the command
// tool's ttl param key (tool/action pTTL = "ttl").
const subagentTTLParam = "ttl"

// NewAgentToolWrapper creates a new AgentToolWrapper.
//   - ag: the sub-agent to wrap (must implement agent.Agent — local TagentAgent or remote A2AAgent)
//   - desc: tool description shown to parent agent's LLM
//   - eventParams: which event-derived parameters to declare and resolve
//   - parentStore: parent agent's MemStore for resolving event_key to full event data
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
// construction/publish-time wire (cold start, new generation) — §6.5/D2: an
// in-flight invocation never re-binds it, because one wrapper instance is shared
// by every concurrent call of its owner. While a flow is running, the flow's own
// projection wins (see projectionForCall).
func (w *AgentToolWrapper) SetParentProjection(p *compress.SessionProjection) {
	w.parentProjection = p
}

// SetParentCM wires the RESIDENT owner's context manager into a freshly built
// delegation wrapper (cold start, same point as SetParentProjection — §6.5/D2's
// allowed exception: a not-yet-published object is configured, a published one is
// never rewritten). It is what lets a stored task's re-entry resolve its target
// against the effective face instead of the instance that spawned it (§4.2).
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
func (w *AgentToolWrapper) Declaration() *trpctool.Declaration {
	desc := w.desc
	// Hot-reload: if descSource is set, re-read from disk
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

	// Standard request parameter
	decl.InputSchema.Properties["request"] = &trpctool.Schema{
		Type:        "string",
		Description: "The request or instruction to process",
	}

	// Declared routing-level extra parameters (plan-interaction-contract D2).
	for _, p := range w.extraParams {
		if p.Name == "" || p.Name == "request" || p.Name == "event_keys" {
			continue // never shadow the built-in parameters
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

	// Declare event-derived parameters
	for _, param := range w.eventParams {
		switch param {
		case "event_key", "event_keys":
			// Always expose as event_keys (array) for consistency.
			// The LLM selects relevant event keys from its context and passes them
			// as a list, enabling the tool to retrieve full event details.
			decl.InputSchema.Properties["event_keys"] = &trpctool.Schema{
				Type:        "array",
				Description: "[LLM-selected] Array of event keys (canonical hex strings, exactly as shown in [evt_...] prefixes and archive cards) for related events from the conversation context. Pass them so the tool can retrieve full event details.",
				Items: &trpctool.Schema{
					Type: "string",
				},
			}
		}
	}

	// Sub-agent lifetime self-service channel (resident-review-fixes 4.1),
	// symmetric with the command tool's `ttl`: a long multi-round delegation is
	// otherwise stuck on the 10-minute reaper floor. >0 sets this sub-agent
	// task's absolute lifetime (seconds); 0/omitted → the manager's configured
	// default (10m floor). A negative value is rejected in Call before spawning.
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
func (w *AgentToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	agentName := w.agent.Info().Name

	// 3.2 trunk: this wrapper's face was wired at publish time with the child
	// generation it DECLARES. Resolve the target through that generation's own
	// execution view: pin it for this call subtree (D5 — a mid-call publish
	// cannot split the call tree across generations), hand it to Run as the
	// inherited lease so the callee assembles from THAT generation's face, and
	// let the background producer derive from the same pin. Unwired (standalone,
	// pre-wiring fallback) keeps the legacy behavior unchanged. A reclaimed
	// declared generation is refused by name — the honest failure when no
	// declarer kept it alive (ErrExecClosed), never a silent re-route onto the
	// child's current face.
	if d := w.declared.Load(); d != nil {
		dl, derr := d.acquireDeclared(LeaseSubCall)
		if derr != nil {
			return nil, fmt.Errorf("agent tool %q: declared generation unavailable: %w", agentName, derr)
		}
		ctx = dl.WithContext(ctx)
		defer dl.Release() // Run's derived reference releases at its own tail; this one covers the window before it
	}

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

	// Extract request text
	request, _ := args["request"].(string)

	// Parse the optional self-set lifetime (seconds). json.Number preserves
	// int64 precision. Semantics mirror the command tool's ttl: >0 effective,
	// 0/omitted → configured default (reaper floor), negative → REJECTED here
	// before any spawn (resident-review-fixes 4.1).
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

	// Collect declared extra params present in this call and build the
	// message body: with extra params → JSON {params..., request} so the
	// sub-agent (LLM and custom Run alike) sees the routing fields; without →
	// plain-text request, behavior unchanged (D2).
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
		// Collect all event keys from event_keys array (LLM-provided).
		if eventKeysRaw, ok := args["event_keys"]; ok {
			switch v := eventKeysRaw.(type) {
			case []interface{}:
				for _, item := range v {
					if key := toInt64Key(item); key > 0 {
						keys = append(keys, key)
					}
				}
			case float64: // Single int passed directly
				if key := toInt64Key(v); key > 0 {
					keys = append(keys, key)
				}
			}
		}

		// Auto-inject: if LLM did not pass event_keys, automatically inject the
		// most recent N event keys as fallback context — resolved against THIS
		// call's projection (§6.5/D2), never against a field another concurrent
		// call of the same agent may have written.
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

	// === Boundary log: tool INPUT ===
	log.Infof("[TRACE] tool_enter agent=%s request_len=%d event_keys=%d external_events=%d",
		agentName, len(request), len(keys), len(externalEvents))

	// Build the Invocation with RuntimeState carrying external context.
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

	inv := agent.NewInvocation(
		agent.WithInvocationMessage(model.NewUserMessage(messageBody)),
		agent.WithInvocationRunOptions(runOpts),
	)

	// Async path: when a task spawner is present (parent's RunFlow injected it)
	// and async is not disabled, run the sub-agent as a task. Its settle = the
	// run's final output. Short runs settle within the sync-wait window and
	// return inline (equivalent to the prior synchronous behavior); long runs
	// return an ack and emit task_settled when the run returns. The run uses a
	// detached context so it can outlive the parent turn (cancel via the task).
	if spawner, ok := task.TaskSpawnerFromContext(ctx); ok && !w.asyncDisabled {
		// task.Task-local round chain: each settled round's {input, output} is
		// recorded so a later resume can restore THIS task's context (and only
		// this task's — context-scoping). Shared across relaunch/resume rounds
		// via the closure.
		rounds := &subagentRounds{cap: w.effectiveResumeRounds()}
		// §4.1 (design D6): the background run is a DERIVED execution. Its reference
		// on the generation it was spawned from is taken BEFORE the producer starts
		// and released only when that producer returns — an ACK, a settle record or
		// a cancel notice is not a stop credential. The same closure owns the
		// release on every exit (settle, error, early stop, cancellation, and the
		// rejected/deduped paths where the task layer never adopted the work), so a
		// reference can neither leak nor drop early.
		callerLease, _ := execLeaseFromContext(ctx)
		bgLease := callerLease.Derive(LeaseBackground)
		detector := task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer bgLease.Release()
			// The lease goes into the run context as well: holding the reference on the
			// initiating generation while resolving nested targets against whatever is
			// published NOW would split the execution across two generations (D5「ACK 后
			// 后台调用 → 继承发起调用租约」). Same shape as the relaunch/resume producers.
			out, err := w.runAndCollect(bgLease.WithContext(runCtx), inv, agentName)
			if err == nil {
				rounds.add(messageBody, out)
			}
			return out, err
		}, w.asyncDenseDuration)
		// Idempotency key: a non-empty declared `name` (e.g. plan's change name)
		// keys the task by identity, so concurrent calls on the SAME plan
		// single-flight via task-layer dedup (plan-interaction-contract D4).
		// Without a name, fall back to keying by request text.
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
			// R2（resident-continuity-r2-r4）：声明式投影——重启后 Relaunch 经 agents map
			// 重投递（承诺表：subagent Resume 不可重建，rounds 无事件源）。
			Declarative: &task.Declarative{
				Kind:        "subagent",
				Desc:        agentName + ": " + truncate(request, 60),
				Key:         spawnKey,
				AgentName:   agentName,
				MessageBody: request,
				Params:      declParams,
			},
			Relaunch: subagentRelaunchClosure(w.parentCM, spawner, inv, agentName, request, spawnKey, ttlSeconds),
			ResumeFn: subagentResumeClosure(w.parentCM, agentName, rounds),
		}, detector)
		if res.Blocked != "" {
			// 5.4（design-report-closeout）+ §8.1：子 agent 在 Spawn 前已开跑，gate 拒绝
			// = 不纳入任务层（Spawn 已 Cancel detector 防失控）——文案必须如实。
			// §4.1: Cancel is a notification, so wait for the producer's real stop
			// before returning (the derived reference stays held meanwhile either way).
			waitForUnadoptedStop(ctx, detector)
			return "子任务未被任务层纳管（已启动的后台运行已被取消跟踪，结果不会回写）：" + res.Blocked + "。可稍后重发。", nil
		}
		if res.Deduped {
			// Same-name single-flight: factual ticket only (stable-context-
			// compaction D5) — the existing task is necessarily in-flight
			// (dedup only matches active tasks); no tool-name teaching here,
			// lifecycle guidance lives in the plan tool description.
			waitForUnadoptedStop(ctx, detector) // §4.1: this call's own detector ran
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

	// Synchronous fallback (no spawner / async disabled): current behavior.
	out, err := w.runAndCollect(ctx, inv, agentName)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// waitForUnadoptedStop honours §4.1 for a detector whose producer had ALREADY
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

// runAndCollect runs the sub-agent for the given invocation and collects its
// final output from the event stream. Shared by the synchronous path and the
// async task detector. Isolation is preserved by Run (fresh bus/CM/projection
// per invocation), so this is safe to run concurrently / in a background task.
// RedispatchAsync（R2，resident-continuity-r2-r4 D1.2）：跨重启 subagent relaunch
// 的重投递入口——以原 request 重新走 Call 的完整 spawn 路径（声明了 extra
// params 时按无参调用降级：plan-name 等 extra 参数不跨重启保留，已知边界）。
func (w *AgentToolWrapper) RedispatchAsync(ctx context.Context, request string) (any, error) {
	args, err := json.Marshal(map[string]any{"request": request})
	if err != nil {
		return nil, err
	}
	return w.Call(ctx, args)
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

// runAndCollect runs one delegation and collects its result, retrying a failed
// REMOTE attempt exactly once (§3.3 传输重试 for the A2A shape).
//
// The retry wraps the WHOLE attempt — send plus drain — because that is where a
// transport failure actually surfaces: the A2A client reports a failed request as
// an event carrying Response.Error while Run itself returns a channel and a nil
// error. The earlier branch inside runWithTimeout, which retried only when Run
// returned an error, therefore never fired on the shape it was written for (a 503
// failed the parent call outright) — see a2a_delegation_test.go.
//
// Both attempts use the SAME invocation and the SAME wrapper instance, i.e. the
// target the initiating call was bound to (D5「传输重试：继承发起调用租约」) — no
// re-resolution against whatever generation is published by then.
func (w *AgentToolWrapper) runAndCollect(ctx context.Context, inv *agent.Invocation, agentName string) (string, error) {
	out, err := w.collectAttempt(ctx, inv, agentName)
	if err == nil || !isRemoteAgent(w.agent) {
		return out, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The caller is being cancelled (shutdown): retrying would race the teardown
		// and the transport error above is the informative one.
		return out, err
	}
	log.Warnf("[AgentToolWrapper] remote agent %q attempt failed (%v), retrying once in %v",
		agentName, err, remoteRetryBackoff)
	select {
	case <-time.After(remoteRetryBackoff):
	case <-ctx.Done():
		return out, err // keep the original transport cause; the cancel is in the log
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
		// Surface upstream model-API errors instead of letting their message
		// text masquerade as a normal final output — otherwise the parent
		// agent (and the operator) only ever sees an opaque provider string
		// with no error classification.
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

	// A run that ended on an upstream error with no usable final output is a
	// FAILURE: return err so the settle path classifies it (status=failed,
	// 错误: ... in the task_settled notification) instead of storing the
	// provider's opaque error text as if it were a result.
	if finalOutput == "" && lastErr != nil {
		return "", fmt.Errorf("agent tool %q: upstream model error (%s): %s", agentName, lastErr.Type, lastErr.Message)
	}
	if finalOutput == "" {
		finalOutput = "tool agent completed without output"
	}

	return finalOutput, nil
}

// subagentRelaunchClosure builds the relaunch closure of a subagent task. It is a
// free function on purpose (introduce-durable-workflow-engine §4.2, R03): its
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
func subagentRelaunchClosure(owner *ContextManager, spawner task.TaskSpawner, inv *agent.Invocation, agentName, request, spawnKey string, ttlSeconds int64) func(context.Context) (task.SpawnResult, error) {
	return func(ctx context.Context) (task.SpawnResult, error) {
		target, lease, err := ResolveReentryDelegation(ctx, owner, agentName)
		if err != nil {
			return task.SpawnResult{}, err
		}
		detector := task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer lease.Release() // §4.1: the producer closure owns the release on every exit
			return target.runAndCollect(lease.WithContext(runCtx), inv, agentName)
			// The dense window is read from the RESOLVED target, not from anything this
			// closure captured at spawn time: like the resume rounds, it is a declaration
			// parameter of the generation actually serving the re-entry (§4.2「不沿用退役代
			// 的声明参数」), and the settle point is host-visible.
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
			// R2（review 🟠8）：relaunch 产物同样携带声明式投影——否则该产物重启后
			// 成幽灵（无 task_spawned 记录可回放）。ttl 一并持久化，保持锚点一致。
			Declarative: &task.Declarative{
				Kind:        "subagent",
				Desc:        agentName + ": " + truncate(request, 60),
				Key:         spawnKey,
				AgentName:   agentName,
				MessageBody: request,
				Params:      declParams,
			},
			Relaunch: subagentRelaunchClosure(owner, spawner, inv, agentName, request, spawnKey, ttlSeconds),
		}, detector)
		if (res.Blocked != "" || res.Deduped) && hasInitiator(ctx) {
			// §4.1: the producer for THIS re-entry already started, so its derived
			// reference stays held until it really stops even though the task layer
			// adopted nothing. The wait rides the initiator's own deadline; with no
			// initiator there is nothing to bound it (a sub-agent run may last the
			// whole 600s timeout), so the reference simply stays held.
			waitForUnadoptedStop(ctx, detector)
		}
		return res, nil
	}
}

// ResolveReentryDelegation picks the delegation target a Resume/Relaunch re-entry
// runs on, per design D5 row 4: a re-entry riding an initiating call inherits THAT
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
	l.Release() // the reference was taken to READ the face; a refusal must not pin anything
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
	// Bound the chain: only the newest cap rounds are ever restored, so older
	// entries are dead weight on a long-lived resumable task.
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

// subagentResumeClosure builds the resume (送输入) closure of a subagent task.
// Like the relaunch closure it captures no executable wrapper (§4.2, R03): the
// target and the target's OWN declaration parameters (restoration cap, dense
// window) come from the generation the re-entry selected, so a stored task cannot
// keep replaying a retired generation's parameters. The refusal happens BEFORE the
// round chain is read or written, so a rejected re-entry leaves the chain
// untouched (「所选代无目标则拒绝且不改任务链」).
//
// The closure returns a NEW single-turn Run whose external_context carries this
// task's prior rounds — the last settle result foremost — and nothing from
// unrelated tasks (context-scoping). No process resurrection: the sub agent stays
// a single-turn primitive; restoration is the framework's engineering feed, not
// sub-agent statefulness.
//
// NOTE(curation): once settle results carry their archived event key (a
// resultRef bridge), the restorer can additionally walk RelationStore
// for curated artifacts on this task's causal chain; the injection slot is
// already here.
func subagentResumeClosure(owner *ContextManager, agentName string, rounds *subagentRounds) func(context.Context, string) (task.SettleDetector, error) {
	return func(ctx context.Context, input string) (task.SettleDetector, error) {
		target, lease, err := ResolveReentryDelegation(ctx, owner, agentName)
		if err != nil {
			return nil, err
		}
		prior := rounds.recent(target.effectiveResumeRounds())
		if len(prior) == 0 {
			lease.Release() // nothing to resume: the reference taken to read the face goes back
			return nil, fmt.Errorf("subagent task has no settled round to resume from — use relaunch_task or a fresh call")
		}

		// Restore the task chain as external context events (newest last; the
		// last settle result is what the resumed run references first).
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
		inv := agent.NewInvocation(
			agent.WithInvocationMessage(model.NewUserMessage(input)),
			agent.WithInvocationRunOptions(runOpts),
		)
		return task.NewFuncSettleDetector(context.Background(), func(runCtx context.Context) (string, error) {
			defer lease.Release()
			out, err := target.runAndCollect(lease.WithContext(runCtx), inv, agentName)
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
// IMPORTANT: agent.Run is async — it returns an event channel immediately and
// the sub-agent produces events in a background goroutine. The cancel function
// must NOT be deferred here, because that would cancel the context as soon as
// this function returns (before the caller finishes consuming the channel).
// Instead, we wrap the returned channel in a goroutine that calls cancel after
// the channel is closed.
//
// Retry policy lives one level up, in runAndCollect: a remote transport failure
// normally arrives as an error EVENT (Run returns nil + channel), so an error
// branch here could never cover the shape it was written for.
func (w *AgentToolWrapper) runWithTimeout(ctx context.Context, inv *agent.Invocation, agentName string) (<-chan *event.Event, error) {
	runCtx, cancel := context.WithTimeout(ctx, defaultSubAgentTimeout)

	eventCh, err := w.agent.Run(runCtx, inv)
	if err != nil {
		cancel()
		return nil, err
	}

	// Wrap channel: cancel context after consumption to enforce timeout
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

// projectionForCall resolves the projection this delegation auto-injects from.
// §6.5/D2: the enclosing call's own projection (bound by RunFlow onto the
// call-chain context) is authoritative, because the wrapper itself is a SHARED
// published object — the same instance serves every concurrent invocation of
// its owner, so its published binding can never be call-correct on its own.
// Outside a flow (direct tool call, legacy callers) the construction-time
// binding set by SetToolParentProjection remains the fallback.
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
	// Take the most recent N events
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

// ==================== Tool Agent Factory Registry ====================
//
// The factory registry provides ID-based lookup for tool agent factories
// (knowledge, recall). These factories create TagentAgent instances that
// are then wrapped by AgentToolWrapper in tagent.New().
//
// In the agent-centric config model, the primary path for creating tool
// agents is via the Agents map in Config. The factory registry supports
// programmatic registration of custom tool agents by ID.

// ToolAgentFactory assembles a tool agent's EXECUTION CONFIGURATION from the
// given inputs. It must NOT construct the agent itself: the org owns the single
// birth path (wireAgent assembles every real owner — store-lease slot, drain
// wiring, task-domain recovery included), and the single publish path
// (stageOrgGenerations advances one face per owner per generation). A factory
// that returned a finished *TagentAgent would be a second owner-birth mechanism
// outside both: it could never advance through the face path, so every publish
// had to rebuild the whole agent (an orphan nobody closed) and every pinned
// delegation kept reading the stale construction config (design D1「避免用返回
// 完整临时 agent 的方式隐式制造第二 owner」; contract migrated round 91 with
// user approval — evidence §5.47).
//
// The returned *TagentConfig is adopted verbatim where it is meaningful:
//   - Name: the factory's choice is respected (the old contract's「产物整只
//     使用」promise); empty falls back to the registered id.
//   - MemoryStore: the org's store borrowed for this name fills a nil — a
//     factory that opens its OWN store must not also be handed the org lease.
//   - MemStoreRelease: always the org's, filled by the assembly after this call
//     returns — a factory neither keeps nor invents a release for it.
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

	// MCPToolSets are MCP tool sources for tool discovery (optional, legacy).
	MCPToolSets []trpctool.ToolSet

	// MCPRegistry is the live MCP server registry (preferred over
	// MCPToolSets): reads reflect runtime registration and config hot-sync.
	MCPRegistry tagenttool.MCPRegistry

	// Agent parameters
	MaxToolIterations int
	MaxTokens         int
	Temperature       float64

	// Thinking/reasoning controls
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string
}

var (
	toolAgentFactories   = map[string]ToolAgentFactory{}
	toolAgentFactoriesMu sync.RWMutex
)

// RegisterToolAgent registers a factory for creating tool agents by ID.
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

// ==================== Plain Tool Factory Registry ====================

// PlainToolFactory creates a plain tool (implements tool.CallableTool) from the given config.
type PlainToolFactory func(cfg PlainToolFactoryConfig) (trpctool.CallableTool, error)

// PlainToolFactoryConfig provides everything a factory needs to create a plain tool.
type PlainToolFactoryConfig struct {
	ID          string
	Description string
	Properties  map[string]any // Tool-specific config, deserialized by each factory

	// WorkspaceRoot is the unified on-disk scratch root (default: .tagent-workspace).
	// Tools that need a working/output directory derive it from here (e.g. the
	// action/exec tool uses <root>/exec as its tmux command working directory).
	WorkspaceRoot string

	// WorkingDir 是 agent 的统一工作根(file tools 的 base_dir 与 exec 命令 cwd 的共同基准)。
	// 空 = 各工具回退自身默认(file base_dir="."、exec 继承进程 cwd,现状不变);非空则作为二者
	// 共同根,优先级仍低于 ToolRef.properties 的显式 base_dir/workspace。由 config.WorkingDir 注入。
	WorkingDir string

	// Runtime dependencies (optional, injected by buildAgent).
	// Most plain tools (e.g., exec) ignore these fields.
	// Sub-tools that need runtime objects (e.g., skill_search needs SkillRepo,
	// memory_query needs MemStore) extract them from here.
	MemStore         memory.MemoryStore         // For memory-dependent tools
	SkillRepo        tagenttool.SkillRepository // For skill-dependent tools
	MCPToolSets      []trpctool.ToolSet         // For MCP-dependent tools (legacy static slice)
	MCPRegistry      tagenttool.MCPRegistry     // Live MCP server registry (preferred over MCPToolSets)
	ReadPartitionIDs []int                      // For recall tools that query cross-namespace

	// Degradation 是 per-agent 五依赖退化状态机（T-G，可选）。mcp_call 工具据此上报 DepMCP
	// 退化（MCP server 连续失败→degraded，成功→恢复）。nil = 未启用退化追踪（现状）。
	Degradation *reliability.DegradationManager

	// MCPProbeEvery（5.4 design-report-closeout）：DepMCP degraded 时 mcp_call 的
	// 熔断半开探测间隔（每 N 次放行 1 次）。0 = 关闭。由 buildPlainToolRef 从 agent
	// DegradationBehaviors 注入。
	MCPProbeEvery int

	// ConsolidationMinSources（4.4 design-report-closeout）：memory_consolidate
	// 的 min_source_events 硬门控（实际取回源不足即拒绝）。0 = 不校验。由
	// buildPlainToolRef 从该 agent 的 memory.engine.consolidation 注入。
	ConsolidationMinSources int
}

var (
	plainToolFactories   = map[string]PlainToolFactory{}
	plainToolFactoriesMu sync.RWMutex
)

// RegisterPlainTool registers a factory for creating plain tools by ID.
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

// toInt64Key converts a JSON-parsed value to an int64 event key.
// Handles:
// toInt64Key converts an event-key argument to int64. Keys are canonically
// HEX strings (the [evt_...] timeline form, unified-event-projection hex
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
		// Canonical: hex (possibly with evt_ prefix echoed by the model).
		s := strings.TrimPrefix(strings.TrimSpace(val), "evt_")
		if k, err := tagentevent.ParseEventKey(s); err == nil && k != 0 {
			return k
		}
		// Legacy fallback: decimal strings from older transcripts.
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

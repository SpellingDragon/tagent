package agent

import (
	"context"

	"github.com/SpellingDragon/tagent/agent/compress"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// transparentUnwrapper / transparentInner are the structural contracts of the
// transparent tool decorators that peer through to the tool they wrap:
// OutputLimitTool exposes Unwrap, GovernanceTool exposes Inner. Matching by
// method set rather than concrete type keeps this package decoupled from
// agent/governance while still piercing that layer.
type (
	transparentUnwrapper interface{ Unwrap() trpctool.Tool }
	transparentInner     interface{ Inner() trpctool.Tool }
)

// collectAgentToolWrappers returns every *AgentToolWrapper reachable through the
// transparent decorator chain in tools. §6.5/W-1: agent.New wraps EVERY tool —
// sub-agent delegation wrappers included — in an OutputLimitTool, so the
// published execution face holds OutputLimitTool(*AgentToolWrapper), never the
// bare wrapper. A top-level `t.(*AgentToolWrapper)` assertion therefore silently
// misses them and the event_keys auto-inject fallback dies with no error (the
// exact "transparent wrapper hides AgentToolWrapper" failure this closes).
// Peeling is read-only and never mutates a published wrapper; it only locates
// the wrappers this build owns so the caller can wire each to its real parent
// projection (design §W-1: 投影接线穿透现有透明装饰器).
func collectAgentToolWrappers(tools []trpctool.Tool) []*AgentToolWrapper {
	var out []*AgentToolWrapper
	var walk func(trpctool.Tool)
	walk = func(t trpctool.Tool) {
		switch w := t.(type) {
		case *AgentToolWrapper:
			out = append(out, w)
		case transparentUnwrapper:
			walk(w.Unwrap())
		case transparentInner:
			walk(w.Inner())
		}
	}
	for _, t := range tools {
		walk(t)
	}
	return out
}

// ContextManager exposes the resident context manager (hotswap-fix 5.7): org
// hot-reload constructs the candidate executor on THIS cm (see
// ContextManager.NewExecutorCandidate / PublishExecutor) so the fresh execution
// face is assembled against the resident state face — projection/bus/callbacks
// are never the shell's own (the 2026-09-13 n=1-system-only incident root cause).
func (ta *TagentAgent) ContextManager() *ContextManager {
	return ta.contextManager
}

// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).
func (ta *TagentAgent) MemStore() memory.MemoryStore {
	return ta.memStore
}

// Runner returns the underlying Runner from ContextManager.
func (ta *TagentAgent) Runner() runner.Runner {
	if ta.contextManager != nil {
		// R4（review 🟠5）：走读锁路径（currentRunner）——裸字段读与发布写
		// （PublishExecutor / ActivateExecutor 在 executorMu 写段内换 runner）构成 data race。
		return ta.contextManager.currentRunner()
	}
	return nil
}

// SessionSvc exposes the resident session service (R4 review 🔴1: the
// executorOnly rebuild shell reuses it so session records and the
// AppendEventHook→outputCh wiring stay on the resident instance).
func (ta *TagentAgent) SessionSvc() session.Service {
	if ta == nil {
		return nil
	}
	return ta.sessionSvc
}

// SetToolParentProjection wires the agent's compress.SessionProjection to all
// AgentToolWrapper instances in the tool list. This enables auto-inject
// of event_keys when LLM does not pass them.
// Must be called after NewTagentAgent (which creates the projection).
//
// §6.5/W-1: the list holds OutputLimitTool(*AgentToolWrapper) after agent.New
// has wrapped every tool, so the wiring pierces the transparent decorator chain
// (collectAgentToolWrappers) instead of asserting the bare type — otherwise the
// projection never reaches the sub-agent wrappers and auto-inject is dead.
//
// This is the ONLY production publish of that binding, and it runs while the
// agent is still being constructed (not yet serving). §6.5/D2 removed the
// per-ContextManager rebinding: a live invocation publishes nothing into shared
// tools and carries its own projection through the context instead.
func (ta *TagentAgent) SetToolParentProjection() {
	if ta.projection == nil || ta.config == nil {
		return
	}
	for _, w := range collectAgentToolWrappers(ta.config.Tools) {
		w.SetParentProjection(ta.projection)
	}
}

// SetDelegationOwnerCM wires the resident owner's ContextManager into this
// agent's delegation wrappers, pierce-through the same transparent chain
// SetToolParentProjection uses (§6.5/W-1: asserting the bare type would miss every
// OutputLimitTool-wrapped wrapper). Cold start only — like the projection wire, it
// configures a not-yet-serving object and is the sole publish of that binding.
//
// It is what lets a stored task's Resume/Relaunch resolve its target on the
// generation in force AT RE-ENTRY (§4.2) instead of the instance that spawned it.
func (ta *TagentAgent) SetDelegationOwnerCM(cm *ContextManager) {
	if ta == nil || ta.config == nil {
		return
	}
	for _, w := range collectAgentToolWrappers(ta.config.Tools) {
		w.SetParentCM(cm)
	}
}

// callProjectionCtxKey carries the CURRENT call's projection down to tool.Call.
//
// §6.5/D2 (reopen): the projection a delegation auto-injects event keys from is
// CALL-PRIVATE state — the persistent loop and every sub-agent invocation build
// their own ContextManager with their own projection. Those private CMs share
// one thing though: the delegation wrappers live in the owner's config.Tools,
// so a per-cm `SetParentProjection` writes through a single published object.
// Two calls of the same agent then (a) race on that field and (b) leak one
// call's recent events into the other call's injected context. Passing the
// projection with the call-chain context — exactly how the task spawner already
// travels (task.WithTaskSpawner, injected at the same RunFlow site) — keeps the
// published tool immutable and makes the value impossible to cross.
type callProjectionCtxKey struct{}

// withCallProjection binds p as the projection for tools invoked by this call.
func withCallProjection(ctx context.Context, p *compress.SessionProjection) context.Context {
	return context.WithValue(ctx, callProjectionCtxKey{}, p)
}

// callProjectionFromContext returns the projection bound by the enclosing
// RunFlow, or (nil, false) outside any flow — the caller then falls back to the
// wrapper's published (construction-time) binding.
func callProjectionFromContext(ctx context.Context) (*compress.SessionProjection, bool) {
	p, ok := ctx.Value(callProjectionCtxKey{}).(*compress.SessionProjection)
	return p, ok && p != nil
}

// invocationIDCtxKey carries the delegation's originating invocation id (the S1
// correlation handle) through the call chain — the SAME ctx-threading discipline
// as withCallProjection, not a shared mutable ContextManager field, so two
// concurrent turns can never cross it. introduce-durable-workflow-engine S2m
// threads it so a task spawned during the turn can stamp it into Origin: the越窗
// task_settled then carries the id (Origin→Metadata verbatim) that S3m's
// per-invocation loop uses to route the late settle back to THIS invocation.
type invocationIDCtxKey struct{}

// withInvocationID binds id as the turn's delegation correlation handle. An
// empty id is a no-op so non-delegation (user/entry) turns stay untouched.
func withInvocationID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, invocationIDCtxKey{}, id)
}

// invocationIDFromContext returns the delegation id bound by the enclosing turn,
// or ("", false) when the turn was not a delegation (no handle threaded).
func invocationIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(invocationIDCtxKey{}).(string)
	return id, ok && id != ""
}

package plugin

import (
	"context"
	"sync"
)

// ==================== 归因章 ctx 载体（TC0 · 热配置/自进化地基）====================
//
// Attribution 是回合级归因章，写入 FullEvent.Metadata，使任意产出事件可回溯到产生
// 它时生效的版本上下文（bundle_id / rollout_id / agent）。这修复了「FullEvent.Metadata
// 在生产代码中从未被填充」的事实缺口（报告 §4.3 F1）——归因盖章是 T-EVO 自我改进
// （可归因/可回滚）与 T-B 可观测（事件维度切分）的共同地基。
//
// 载体模式仿 ProjectionSink（projection_sink.go）：RunFlow 每回合绑定，MemoryPlugin
// 在存储同步点读取写入 Metadata。两条持久化路径（插件管线 onEvent + persistBusEvent）
// 均须盖章，避免归因盲区（报告 R5）。
//
// 未注入归因时（AttributionFrom 返回 false），Metadata 仅含 MemoryPlugin 盖的基线
// provenance（agent_name），行为向后兼容。

// Attribution 是归因章键值对（写入 FullEvent.Metadata）。
type Attribution map[string]string

// attributionKey 是 ctx 载体键（每回合绑定，主循环与子 agent 天然隔离）。
type attributionKey struct{}

// WithAttribution 返回携带归因章的 ctx（RunFlow 每回合绑定）。空归因不注入（省分配）。
func WithAttribution(ctx context.Context, a Attribution) context.Context {
	if len(a) == 0 {
		return ctx
	}
	return context.WithValue(ctx, attributionKey{}, a)
}

// AttributionFrom 从 ctx 提取归因章（无则返回 nil,false）。
func AttributionFrom(ctx context.Context) (Attribution, bool) {
	a, ok := ctx.Value(attributionKey{}).(Attribution)
	return a, ok && len(a) > 0
}

// echoCredentialCtxKey is the context key for THIS attempt's echo credential.
// §4.4 (design 决策4): replaces the old whole-turn DurableInbound "first envelope /
// any user" flag. A fresh credential is minted per runner attempt (RunFlow) and
// released when the call's ctx ends — no turn-global or "first envelope" state.
type echoCredentialCtxKey struct{}

// EchoCredential identifies THIS attempt's expected input echo so MemoryPlugin can
// skip re-storing it precisely, instead of skipping every user event in a durable turn.
// The event loop commits the batch's per-message facts before running the model; the
// framework then echoes the merged input back through the plugin pipeline. Only the
// exact echo matching this credential may skip storage — sub-calls, assistants, tools,
// and any other user message take the normal path.
type EchoCredential struct {
	AttemptToken  string  // unique per runner attempt (a retry mints a new one, reusing the same facts)
	Agent         string  // expected agent name
	Session       string  // expected session id
	MergedMessage string  // the canonical merged input the loop built (normalized-match target)
	CommittedKeys []int64 // fact keys already persisted for this batch (consumed by §4.5)

	mu       sync.Mutex
	boundID  string // exact event invocation id bound on the first match
	boundSet bool
	rejected bool   // §4.5: a field-mismatch / plugin error downgraded this attempt
	reason   string // why rejected (diagnostics)
}

// Verified reports whether this attempt's expected echo was positively identified
// (bound) AND not later rejected. The model-entry gate (§4.5) blocks the model when a
// credential was installed for the turn but Verified() is false — the committed inputs
// were never confirmed to be the ones the framework ran on.
func (c *EchoCredential) Verified() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.boundSet && !c.rejected
}

// MarkRejected downgrades an already-installed credential to non-verifiable (§4.5:
// "字段不匹配或插件记录错误 → 调用级凭据置为拒绝"). Sticky (first reason wins). The
// model-entry gate and the loop's ack decision consult Verified(), so a swallowed
// plugin error can no longer let the turn cross the commit gate.
func (c *EchoCredential) MarkRejected(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.rejected {
		c.rejected = true
		c.reason = reason
	}
}

// Bind records the ROOT INVOCATION id of the first echo match (idempotent: later
// matches within the same attempt don't overwrite). event.Event carries no per-event
// ID (it embeds *model.Response + InvocationID), so the root invocation id is the
// exact, stable identity of THIS attempt's input echo; §4.5's verify MUST assert at
// this per-attempt granularity, not per-event. Audit / §4.5 hook.
func (c *EchoCredential) Bind(invocationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.boundSet {
		c.boundID = invocationID
		c.boundSet = true
	}
}

// BoundID returns the invocation id bound on the first echo match (empty until bound).
func (c *EchoCredential) BoundID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.boundID
}

// WithEchoCredential returns a context carrying this attempt's echo credential.
func WithEchoCredential(ctx context.Context, c *EchoCredential) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, echoCredentialCtxKey{}, c)
}

// EchoCredentialFrom extracts the attempt's echo credential, if any.
func EchoCredentialFrom(ctx context.Context) (*EchoCredential, bool) {
	if ctx == nil {
		return nil, false
	}
	c, ok := ctx.Value(echoCredentialCtxKey{}).(*EchoCredential)
	return c, ok && c != nil
}

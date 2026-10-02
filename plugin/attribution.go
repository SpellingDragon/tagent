package plugin

import (
	"context"
	"sync"
)

// Attribution 是回合级归因章（写入 FullEvent.Metadata），由 RunFlow 每回合经 ctx 绑定。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#attribution-carrier
type Attribution map[string]string

type attributionKey struct{}

// WithAttribution 返回携带归因章的 ctx；空归因不写入，返回原 ctx。
func WithAttribution(ctx context.Context, a Attribution) context.Context {
	if len(a) == 0 {
		return ctx
	}
	return context.WithValue(ctx, attributionKey{}, a)
}

// AttributionFrom 从 ctx 取回归因章；无有效归因时返回 (nil, false)。
func AttributionFrom(ctx context.Context) (Attribution, bool) {
	a, ok := ctx.Value(attributionKey{}).(Attribution)
	return a, ok && len(a) > 0
}

type echoCredentialCtxKey struct{}

// EchoCredential 标识本次尝试期望的输入回显，使 MemoryPlugin 精确跳过那一条的重复入库。
// 它按每次 runner 尝试新建、随该调用的 ctx 结束而释放，不存在回合级或「首个信封」状态。
// 绑定与拒绝状态由内部字段承载，判定粒度是根调用 id（框架事件不携带逐事件 id）。
type EchoCredential struct {
	AttemptToken  string
	Agent         string
	Session       string
	MergedMessage string
	CommittedKeys []int64

	mu       sync.Mutex
	boundID  string
	boundSet bool
	rejected bool
	reason   string
}

// Verified 报告期望回显是否已被正面确认（已绑定且未被降级）。装入了凭据而未 Verified 的回合，
// 模型入口与 ack 决策都必须失败关闭。
func (c *EchoCredential) Verified() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.boundSet && !c.rejected
}

// MarkRejected 把已装入的凭据降级为不可验证并记录首个原因（粘滞）。
func (c *EchoCredential) MarkRejected(reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.rejected {
		c.rejected = true
		c.reason = reason
	}
}

// Bind 记录首次匹配所在的根调用 id；同一尝试内的后续匹配不覆盖（幂等）。
func (c *EchoCredential) Bind(invocationID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.boundSet {
		c.boundID = invocationID
		c.boundSet = true
	}
}

// BoundID 返回首次匹配绑定的调用 id；未绑定时为空串。
func (c *EchoCredential) BoundID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.boundID
}

// WithEchoCredential 让本次尝试的凭据经 ctx 传递；nil 不注入。
func WithEchoCredential(ctx context.Context, c *EchoCredential) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, echoCredentialCtxKey{}, c)
}

// EchoCredentialFrom 取回本次尝试的凭据；不存在或为 nil 时返回 (nil, false)。
func EchoCredentialFrom(ctx context.Context) (*EchoCredential, bool) {
	if ctx == nil {
		return nil, false
	}
	c, ok := ctx.Value(echoCredentialCtxKey{}).(*EchoCredential)
	return c, ok && c != nil
}

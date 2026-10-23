# host-protocol：Host 入口协议＋双端并发

## 协议（签名级冻结——两端的唯一契约）

```go
// tagent/host.go

// Host is the turnkey entry for application hosts: it reads a configuration
// file, assembles the runtime, and exposes the running faces.
type Host struct { /* unexported fields */ }

// Boot reads the configuration and assembles the runtime automatically.
// It resolves providers→models, builds declared tool surfaces (send/recall/
// knowledge/spec/file/exec/mcp/subagent), wires storage+embedder+meditation,
// and starts the standard injection face (/healthz + /task on loopback).
// The caller registers only what the core cannot know: how to send messages
// to real users and how to surface approvals.
func Boot(configPath string, opts ...HostOption) (*Host, error)

// HostOption customizes a Host.
type HostOption func(*hostConfig)

// WithSender registers the application's outbound message face.
// The Host routes agent output addressed to users through this port.
func WithSender(sender MessageSender) HostOption

// WithApprover registers the approval face.
func WithApprover(approver task.Approver) HostOption

// WithDeliverer registers where meditation reflection output goes.
func WithDeliverer(deliver DeliverFunc) HostOption

// WithGovernance overrides the default governance policy.
func WithGovernance(governance state.Governance) HostOption

// MessageSender is the port the application fills: send one message
// to one address. The core calls it when agent output targets a user.
type MessageSender interface {
    Send(ctx context.Context, target event.Address, content string) error
}

// DeliverFunc delivers one meditation reflection to its declared targets.
type DeliverFunc func(ctx context.Context, source string, targets []event.Address, content string)

// Host faces:

// Invoke runs one invocation on the entry agent.
func (h *Host) Invoke(ctx context.Context, req InvokeRequest) (agent.Turn, error)

// Close drains and shuts down.
func (h *Host) Close(ctx context.Context) error

// Run exposes the underlying runtime for advanced consumers.
func (h *Host) Run() *Run

// FaceAddr returns the loopback address of the injection face.
func (h *Host) FaceAddr() string
```

## 验收协议

- wechat-bot main.go ≤50 行（读配置→Boot→bindWechat→Run）
- wechat-bot wechat.go ≤1000 行（纯微信适配＋自定义逻辑）
- wechat-bot tagent import ≤3 个（tagent、tagent/api/event、wechat-robot-go）
- `tagent.Boot` 从配置到可用 Host 一条路零手动装配

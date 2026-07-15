## Context

当前 `agent/` 包有 13 个源文件（5517 行）+ 24 个测试文件（5650 行）。核心问题是 `tagent_agent.go` 承担了过多职责（1232 行、38 个函数），且 `tool_agent.go` 和 `trajectory_recorder.go` / `http_api.go` 与核心引擎无私有字段依赖，可以独立。

### 关键约束

1. **外部引用广泛**：`tagent.go`、`builtin.go`、`tool/knowledge/`、`tool/recall/`、`tool/draw/`、`tool/speak/`、`tool/plan/`、`tool/file/`、`examples/wechat-bot/` 都引用 `agent` 包的导出类型
2. **PlainToolFactoryConfig 被 7 个工具子包使用**：移动此类型需要更新所有工具子包的 import
3. **TagentAgent 和 TagentConfig 被 6 个工具子包直接引用**：这些必须保留在 `agent/` 包中
4. **tool_agent.go 中的注册表（ToolAgentFactory/PlainToolFactory）被根包使用**：这些全局 registry 也被 6+ 个工具子包注册

## Goals / Non-Goals

**Goals:**

- tagent_agent.go 拆为 7 个文件（≤250 行/文件），每个文件单一职责
- TrajectoryRecorder + HTTPAPI 移到 `rl/` 包
- SwappableModel 移到 `rl/` 包（它是 RL 场景的 AReaL proxy model 切换器）
- 所有测试继续通过
- 外部引用更新（约 15 个文件的 import 路径变更）

**Non-Goals:**

- 不移动 AgentToolWrapper / PlainToolFactoryConfig / ToolAgentFactoryConfig（它们被太多工具子包引用，移动成本超过收益）
- 不改变任何行为、接口签名、或公有 API
- 不重构 context_manager.go / smart_compress.go 的内部结构
- 不引入新的接口抽象层

## Decisions

### D1: tagent_agent.go 拆分策略

按函数归属拆分为 7 个文件，每个文件的职责通过文件名即可理解：

```
agent.go:
  - type TagentAgent struct { ... }
  - type TagentConfig struct { ... }
  - type CompressConfig struct { ... }
  - type MeditationConfig struct { ... }
  - type Closer interface { ... }
  - const DefaultMaxToolIterations, DefaultMaxTokens, etc.
  - func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) { ... }

lifecycle.go:
  - func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error)
  - func (ta *TagentAgent) StopLoop()
  - func (ta *TagentAgent) Close() error
  - func (ta *TagentAgent) RegisterCloser(c Closer)
  - func (ta *TagentAgent) SetTrajectoryRecorder(tr *TrajectoryRecorder)
  - func (ta *TagentAgent) TrajectoryRecorder() *TrajectoryRecorder

event_loop.go:
  - func (ta *TagentAgent) runEventLoop(ctx, bus, cm)
  - func extractTriggerSource(events []*AgentEvent) string
  - func extractRootMetadata(events []*AgentEvent) map[string]string
  - func (ta *TagentAgent) publishErrorEvent(bus, err)
  - func summarizeEvents(events []*AgentEvent) string

inject.go:
  - func (ta *TagentAgent) InjectMessage(msg model.Message)
  - func (ta *TagentAgent) InjectMessageWithSource(source string, msg model.Message)
  - func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string)
  - func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent)
  - func (ta *TagentAgent) injectExternalContext(msg model.Message) model.Message

session.go:
  - func (ta *TagentAgent) Run(ctx, inv) (<-chan *event.Event, error)
  - func (ta *TagentAgent) Tools() []tool.Tool
  - func (ta *TagentAgent) Info() agent.Info
  - func (ta *TagentAgent) SubAgents() []agent.Agent
  - func (ta *TagentAgent) FindSubAgent(name string) agent.Agent
  - func (ta *TagentAgent) setActiveBus(bus *EventBus)
  - func (ta *TagentAgent) restorePersistentBus()
  - func (ta *TagentAgent) setSessionContext(userID, sessionID string)
  - func (ta *TagentAgent) getOrCreateSession(sessionID ...string) *session.Session
  - func (ta *TagentAgent) makeOnEventCallback(sessionID string, projection *SessionProjection) func(evt *event.Event)

a2a.go:
  - func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error)

helpers.go:
  - func buildCompressorOpts(cfg *TagentConfig) []SmartCompressorOption
  - func newCompressorFromConfig(cfg *TagentConfig) *SmartCompressor
  - func newContextManagerFromConfig(...)
  - func (ta *TagentAgent) MemStore() memory.MemoryStore
  - func (ta *TagentAgent) Runner() runner.Runner
  - func (ta *TagentAgent) SetToolParentProjection()
  - func truncateString(s string, n int) string
```

### D2: rl/ 包拆分

将以下三个类型移到 `rl/` 包：

- `TrajectoryRecorder` → `rl.TrajectoryRecorder`
- `HTTPAPI` / `NewHTTPAPI` → `rl.HTTPAPI` / `rl.NewHTTPAPI`
- `SwappableModel` / `NewSwappableModel` → `rl.SwappableModel` / `rl.NewSwappableModel`

**接口注入**：HTTPAPI 需要 TagentAgent 的能力（InjectMessage + StartLoop + LoopActive）。定义接口：

```go
// rl/interfaces.go
type AgentLoop interface {
    InjectMessage(msg model.Message)
    InjectMessageWithSource(source string, msg model.Message)
    StartLoop(userID, sessionID string) (<-chan *event.Event, error)
    StopLoop()
    IsLoopActive() bool
}
```

### D3: 不移动 AgentToolWrapper

AgentToolWrapper 引用了 `SessionProjection`（通过 `SetParentProjection`）和 `TagentAgent`（通过 `agent.Agent` 接口）。它被 `tagent.go` 直接实例化。移动它会导致循环依赖（toolwrap → agent → toolwrap）。保留在 `agent/` 包中。

### D4: 不移动 PlainToolFactoryConfig / ToolAgentFactoryConfig

这些类型被 7+ 个工具子包使用（knowledge、recall、draw、speak、file、plan、action）。移动它们的成本（修改 7 个包的 import + 类型引用）大于收益。保留在 `agent/` 包中。

## Risks / Trade-offs

- **[R1] 文件数量增加**：agent/ 从 13 个文件变为 19 个（+6）。缓解：每个文件职责明确，按 IDE 搜索更容易定位。
- **[R2] rl/ 包引入 interface 适配层**：HTTPAPI 需要 AgentLoop 接口。缓解：接口很小（5 个方法），且语义清晰。
- **[R3] 外部引用变更范围**：`tagent.go` 和 `examples/wechat-bot/main.go` 需要更新 import。缓解：项目未公开发布，影响面有限。

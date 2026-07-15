## 任务清单

本变更分为 3 个阶段，严格按顺序执行。每阶段结束后 `go test ./... -short` 必须通过。

**关键原则**：每个阶段只做一类操作（拆分 OR 移动），不混合。这确保中间状态始终可编译。

---

## 阶段 1: tagent_agent.go 拆分为 7 个文件（agent/ 包内）

> 目标: 将 1232 行的巨石文件按职责拆分，不改变任何行为
> 方法: 逐个文件提取函数，每提取一个文件后立即验证编译
> 状态: ✅ 已完成 (commits da041ff..bf0f9e0)

### Task 1.1: 创建 agent/agent.go — 结构体定义与构造

- [x] 从 `tagent_agent.go` 中提取以下内容到新文件 `agent/agent.go`：
  - 包级文档注释（package agent 开头的注释块）
  - `type Closer interface { Close() error }`
  - `type TagentAgent struct { ... }`（完整结构体定义，约 60 行）
  - `type TagentConfig struct { ... }`（完整结构体定义，约 40 行）
  - `type CompressConfig struct { ... }`
  - `type MeditationConfig struct { ... }`
  - 所有 `const` 和默认值（`DefaultMaxToolIterations`、`DefaultMaxTokens` 等）
  - `func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) { ... }`（构造函数）
- [x] 从 `tagent_agent.go` 中删除已提取的代码（保留 import 和其余函数）
- [x] `go build ./agent/` 通过

### Task 1.2: 创建 agent/lifecycle.go — 生命周期管理

- [x] 从 `tagent_agent.go` 中提取以下函数到 `agent/lifecycle.go`：
  - `func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error)`
  - `func (ta *TagentAgent) StopLoop()`
  - `func (ta *TagentAgent) Close() error`
  - `func (ta *TagentAgent) RegisterCloser(c Closer)`
  - `func (ta *TagentAgent) SetTrajectoryRecorder(tr *TrajectoryRecorder)`
  - `func (ta *TagentAgent) TrajectoryRecorder() *TrajectoryRecorder`
- [x] 添加必要的 import（`context`, `sync`, `time`, `event`, `log` 等）
- [x] `go build ./agent/` 通过

### Task 1.3: 创建 agent/event_loop.go — 事件循环

- [x] 从 `tagent_agent.go` 中提取以下函数到 `agent/event_loop.go`：
  - `func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager)`
  - `func extractTriggerSource(events []*AgentEvent) string`
  - `func extractRootMetadata(events []*AgentEvent) map[string]string`
  - `func (ta *TagentAgent) publishErrorEvent(bus *EventBus, runErr error)`
  - `func summarizeEvents(events []*AgentEvent) string`
- [x] 添加必要的 import
- [x] `go build ./agent/` 通过

### Task 1.4: 创建 agent/inject.go — 消息注入

- [x] 从 `tagent_agent.go` 中提取以下函数到 `agent/inject.go`：
  - `func (ta *TagentAgent) InjectMessage(msg model.Message)`
  - `func (ta *TagentAgent) InjectMessageWithSource(source string, msg model.Message)`
  - `func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string)`
  - `func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent)`
  - `func (ta *TagentAgent) injectExternalContext(msg model.Message) model.Message`
- [x] 添加必要的 import
- [x] `go build ./agent/` 通过

### Task 1.5: 创建 agent/session.go — 子 Agent 会话

- [x] 从 `tagent_agent.go` 中提取以下函数到 `agent/session.go`：
  - `func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error)`
  - `func (ta *TagentAgent) Tools() []tool.Tool`
  - `func (ta *TagentAgent) Info() agent.Info`
  - `func (ta *TagentAgent) SubAgents() []agent.Agent`
  - `func (ta *TagentAgent) FindSubAgent(name string) agent.Agent`
  - `func (ta *TagentAgent) setActiveBus(bus *EventBus)`
  - `func (ta *TagentAgent) restorePersistentBus()`
  - `func (ta *TagentAgent) setSessionContext(userID, sessionID string)`
  - `func (ta *TagentAgent) getOrCreateSession(sessionID ...string) *session.Session`
  - `func (ta *TagentAgent) makeOnEventCallback(sessionID string, projection *SessionProjection) func(evt *event.Event)`
- [x] 添加必要的 import
- [x] `go build ./agent/` 通过

### Task 1.6: 创建 agent/a2a.go — A2A 服务器

- [x] 从 `tagent_agent.go` 中提取以下函数到 `agent/a2a.go`：
  - `func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error)`
- [x] 添加必要的 import
- [x] `go build ./agent/` 通过

### Task 1.7: 创建 agent/helpers.go — 辅助函数

- [x] 从 `tagent_agent.go` 中提取以下函数到 `agent/helpers.go`：
  - `func buildCompressorOpts(cfg *TagentConfig) []SmartCompressorOption`
  - `func newCompressorFromConfig(cfg *TagentConfig) *SmartCompressor`
  - `func newContextManagerFromConfig(...) *ContextManager`
  - `func (ta *TagentAgent) MemStore() memory.MemoryStore`
  - `func (ta *TagentAgent) Runner() runner.Runner`
  - `func (ta *TagentAgent) SetToolParentProjection()`
  - `func truncateString(s string, n int) string`
  - `func NewSwappableModel(m model.Model) *SwappableModel`（暂留，阶段 2 移走）
  - `type SwappableModel struct { ... }` 及其方法（暂留，阶段 2 移走）
- [x] 添加必要的 import
- [x] `go build ./agent/` 通过

### Task 1.8: 删除 tagent_agent.go 中的空壳

- [x] 此时 `tagent_agent.go` 应该只剩下 `package agent` 和 import（可能为空）
- [x] 如果为空，删除文件
- [x] `go build ./agent/` 通过
- [x] `go test ./agent/ -count=1 -timeout 60s` 通过
- [x] `go test ./... -short -count=1 -timeout 180s` 全部通过

### Task 1.9: 验证阶段 1

- [x] `go build ./...` 通过
- [x] `go vet ./...` 无警告
- [x] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [x] `wc -l agent/agent.go agent/lifecycle.go agent/event_loop.go agent/inject.go agent/session.go agent/a2a.go agent/helpers.go` 每个文件 ≤ 350 行
- [x] `ls agent/tagent_agent.go 2>/dev/null` 确认已删除

---

## 阶段 2: 创建 rl/ 包

> 目标: 将 TrajectoryRecorder + HTTPAPI + SwappableModel 移到独立包
> 方法: 先创建新包 + 接口，再逐个移动文件，每步编译验证

### Task 2.1: 创建 rl/ 包骨架

- [ ] 创建 `rl/` 目录
- [ ] 创建 `rl/agent_loop.go`：定义 AgentLoop 接口
  ```go
  package rl
  
  import (
      "trpc.group/trpc-go/trpc-agent-go/event"
      "trpc.group/trpc-go/trpc-agent-go/model"
  )
  
  // AgentLoop is the interface that decouples rl/ from agent/.
  type AgentLoop interface {
      InjectMessage(msg model.Message)
      InjectMessageWithSource(source string, msg model.Message)
      StartLoop(userID, sessionID string) (<-chan *event.Event, error)
      StopLoop()
  }
  ```
- [ ] `go build ./rl/` 通过

### Task 2.2: 移动 TrajectoryRecorder

- [ ] 将 `agent/trajectory_recorder.go` 复制到 `rl/trajectory_recorder.go`
- [ ] 修改包名为 `package rl`
- [ ] 修改 import 路径（如有引用 agent 包内部类型，改为从 rl 包外导入或通过参数传入）
- [ ] 在 `agent/` 中保留一个 type alias 或重导出（暂时，避免外部 break）：
  ```go
  // agent/trajectory_compat.go
  // Deprecated: Use rl.TrajectoryRecorder directly.
  type TrajectoryRecorder = rl.TrajectoryRecorder
  ```
- [ ] 删除 `agent/trajectory_recorder.go`
- [ ] `go build ./...` 通过
- [ ] 将 `agent/trajectory_recorder_test.go` 移到 `rl/trajectory_recorder_test.go`，修改包名
- [ ] `go test ./rl/ -v` 通过

### Task 2.3: 移动 SwappableModel

- [ ] 将 SwappableModel 相关代码从 `agent/helpers.go` 提取到 `rl/swappable_model.go`
- [ ] 修改包名为 `package rl`
- [ ] 在 `agent/` 中保留 type alias（暂时）：
  ```go
  // agent/swappable_compat.go
  type SwappableModel = rl.SwappableModel
  var NewSwappableModel = rl.NewSwappableModel
  ```
- [ ] `go build ./...` 通过

### Task 2.4: 移动 HTTPAPI

- [ ] 将 `agent/http_api.go` 复制到 `rl/http_api.go`
- [ ] 修改包名为 `package rl`
- [ ] 将 `HTTPAPI` 结构体中对 `*TagentAgent` 的引用改为 `AgentLoop` 接口
- [ ] 修改 `NewHTTPAPI` 签名：`func NewHTTPAPI(agent AgentLoop) *HTTPAPI`
- [ ] 在 `agent/` 中保留 type alias（暂时）：
  ```go
  // agent/http_api_compat.go
  type HTTPAPI = rl.HTTPAPI
  var NewHTTPAPI = rl.NewHTTPAPI
  ```
- [ ] 删除 `agent/http_api.go`
- [ ] `go build ./...` 通过
- [ ] 将 `agent/http_api_test.go` 移到 `rl/http_api_test.go`，修改包名
- [ ] `go test ./rl/ -v` 通过

### Task 2.5: 更新外部引用（移除 compat alias）

- [ ] 更新 `tagent.go`：
  - import `"github.com/SpellingDragon/tagent/rl"` 替代 `agent.TrajectoryRecorder` 等
  - 替换 `agent.NewTrajectoryRecorder` → `rl.NewTrajectoryRecorder`
  - 替换 `agent.TrajectoryRecorder` → `rl.TrajectoryRecorder`
- [ ] 更新 `examples/wechat-bot/main.go`：
  - import `"github.com/SpellingDragon/tagent/rl"`
  - 替换 `agent.NewSwappableModel` → `rl.NewSwappableModel`
  - 替换 `agent.NewHTTPAPI` → `rl.NewHTTPAPI`
- [ ] 删除 `agent/trajectory_compat.go`、`agent/swappable_compat.go`、`agent/http_api_compat.go`
- [ ] `go build ./...` 通过
- [ ] `go test ./... -short -count=1` 全部通过

### Task 2.6: 验证阶段 2

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [ ] `ls rl/` 确认有 4 个 .go 文件：`agent_loop.go`, `trajectory_recorder.go`, `swappable_model.go`, `http_api.go`
- [ ] `grep -r "agent.TrajectoryRecorder\|agent.HTTPAPI\|agent.SwappableModel\|agent.NewHTTPAPI\|agent.NewSwappableModel" --include="*.go" | grep -v "_test.go"` 无结果（所有引用已更新）

---

## 阶段 3: 清理 + 最终验证

> 目标: 清理残留、验证完整性

### Task 3.1: 检查 agent 包 unused import

- [ ] 运行 `goimports -w agent/` 或手动检查每个新文件的 import 是否有未使用项
- [ ] `go build ./...` 通过

### Task 3.2: 检查文件行数

- [ ] `wc -l agent/agent.go agent/lifecycle.go agent/event_loop.go agent/inject.go agent/session.go agent/a2a.go agent/helpers.go`
- [ ] 每个文件 ≤ 350 行（如超过，考虑是否有可进一步拆分的函数）

### Task 3.3: 更新 README 项目结构

- [ ] 在 `README.md` 的"项目结构"章节中新增 `rl/` 条目
- [ ] 更新 `agent/` 的描述（注明文件拆分）

### Task 3.4: 最终验证

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [ ] `cd examples/wechat-bot && go build .` 通过
- [ ] `go test -run TestInvariant ./tests/ -v` 三个不变量测试通过
- [ ] `find agent/ -name "*.go" ! -name "*_test.go" | wc -l` 确认 agent 包有 ~15 个源文件（原13 + 新增7 - 删除tagent_agent.go - 移出3 = ~16）
- [ ] `find rl/ -name "*.go" ! -name "*_test.go" | wc -l` 确认 rl 包有 4 个源文件

---

## 迁移说明

### 对下游应用的影响

1. **import 路径变更**：
   - `agent.TrajectoryRecorder` → `rl.TrajectoryRecorder`（import `"github.com/SpellingDragon/tagent/rl"`）
   - `agent.NewHTTPAPI` → `rl.NewHTTPAPI`
   - `agent.NewSwappableModel` → `rl.NewSwappableModel`
   - `agent.SwappableModel` → `rl.SwappableModel`

2. **无行为变更**：所有 API 签名、功能、默认值保持不变

3. **TagentAgent / TagentConfig / CompressConfig / MeditationConfig 保持在 `agent/` 包中不变**
## 任务清单

本变更分为 3 个阶段，严格按顺序执行。每阶段结束后 `go test ./... -short` 必须通过。

**关键原则**：每个阶段只做一类操作（拆分 OR 移动），不混合。这确保中间状态始终可编译。

---

## 阶段 1: tagent_agent.go 拆分为 7 个文件（agent/ 包内）

> 目标: 将 1232 行的巨石文件按职责拆分，不改变任何行为
> 方法: 逐个文件提取函数，每提取一个文件后立即验证编译

### Task 1.1: 创建 agent/agent.go — 结构体定义与构造

- [ ] 从 `tagent_agent.go` 中提取以下内容到新文件 `agent/agent.go`：
  - 包级文档注释（package agent 开头的注释块）
  - `type Closer interface { Close() error }`
  - `type TagentAgent struct { ... }`（完整结构体定义，约 60 行）
  - `type TagentConfig struct { ... }`（完整结构体定义，约 40 行）
  - `type CompressConfig struct { ... }`
  - `type MeditationConfig struct { ... }`
  - 所有 `const` 和默认值（`DefaultMaxToolIterations`、`DefaultMaxTokens` 等）
  - `func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) { ... }`（构造函数）
- [ ] 从 `tagent_agent.go` 中删除已提取的代码（保留 import 和其余函数）
- [ ] `go build ./agent/` 通过

### Task 1.2: 创建 agent/lifecycle.go — 生命周期管理

- [ ] 从 `tagent_agent.go` 中提取以下函数到 `agent/lifecycle.go`：
  - `func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error)`
  - `func (ta *TagentAgent) StopLoop()`
  - `func (ta *TagentAgent) Close() error`
  - `func (ta *TagentAgent) RegisterCloser(c Closer)`
  - `func (ta *TagentAgent) SetTrajectoryRecorder(tr *TrajectoryRecorder)`
  - `func (ta *TagentAgent) TrajectoryRecorder() *TrajectoryRecorder`
- [ ] 添加必要的 import（`context`, `sync`, `time`, `event`, `log` 等）
- [ ] `go build ./agent/` 通过

### Task 1.3: 创建 agent/event_loop.go — 事件循环

- [ ] 从 `tagent_agent.go` 中提取以下函数到 `agent/event_loop.go`：
  - `func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager)`
  - `func extractTriggerSource(events []*AgentEvent) string`
  - `func extractRootMetadata(events []*AgentEvent) map[string]string`
  - `func (ta *TagentAgent) publishErrorEvent(bus *EventBus, runErr error)`
  - `func summarizeEvents(events []*AgentEvent) string`
- [ ] 添加必要的 import
- [ ] `go build ./agent/` 通过

### Task 1.4: 创建 agent/inject.go — 消息注入

- [ ] 从 `tagent_agent.go` 中提取以下函数到 `agent/inject.go`：
  - `func (ta *TagentAgent) InjectMessage(msg model.Message)`
  - `func (ta *TagentAgent) InjectMessageWithSource(source string, msg model.Message)`
  - `func (ta *TagentAgent) InjectMessageWithMetadata(source string, msg model.Message, metadata map[string]string)`
  - `func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent)`
  - `func (ta *TagentAgent) injectExternalContext(msg model.Message) model.Message`
- [ ] 添加必要的 import
- [ ] `go build ./agent/` 通过

### Task 1.5: 创建 agent/session.go — 子 Agent 会话

- [ ] 从 `tagent_agent.go` 中提取以下函数到 `agent/session.go`：
  - `func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error)`
  - `func (ta *TagentAgent) Tools() []tool.Tool`
  - `func (ta *TagentAgent) Info() agent.Info`
  - `func (ta *TagentAgent) SubAgents() []agent.Agent`
  - `func (ta *TagentAgent) FindSubAgent(name string) agent.Agent`
  - `func (ta *TagentAgent) setActiveBus(bus *EventBus)`
  - `func (ta *TagentAgent) restorePersistentBus()`
  - `func (ta *TagentAgent) setSessionContext(userID, sessionID string)`
  - `func (ta *TagentAgent) getOrCreateSession(sessionID ...string) *session.Session`
  - `func (ta *TagentAgent) makeOnEventCallback(sessionID string, projection *SessionProjection) func(evt *event.Event)`
- [ ] 添加必要的 import
- [ ] `go build ./agent/` 通过

### Task 1.6: 创建 agent/a2a.go — A2A 服务器

- [ ] 从 `tagent_agent.go` 中提取以下函数到 `agent/a2a.go`：
  - `func NewA2AServer(ta *TagentAgent, host string) (*a2ago.A2AServer, error)`
- [ ] 添加必要的 import
- [ ] `go build ./agent/` 通过

### Task 1.7: 创建 agent/helpers.go — 辅助函数

- [ ] 从 `tagent_agent.go` 中提取以下函数到 `agent/helpers.go`：
  - `func buildCompressorOpts(cfg *TagentConfig) []SmartCompressorOption`
  - `func newCompressorFromConfig(cfg *TagentConfig) *SmartCompressor`
  - `func newContextManagerFromConfig(...) *ContextManager`
  - `func (ta *TagentAgent) MemStore() memory.MemoryStore`
  - `func (ta *TagentAgent) Runner() runner.Runner`
  - `func (ta *TagentAgent) SetToolParentProjection()`
  - `func truncateString(s string, n int) string`
  - `func NewSwappableModel(m model.Model) *SwappableModel`（暂留，阶段 2 移走）
  - `type SwappableModel struct { ... }` 及其方法（暂留，阶段 2 移走）
- [ ] 添加必要的 import
- [ ] `go build ./agent/` 通过

### Task 1.8: 删除 tagent_agent.go 中的空壳

- [ ] 此时 `tagent_agent.go` 应该只剩下 `package agent` 和 import（可能为空）
- [ ] 如果为空，删除文件
- [ ] `go build ./agent/` 通过
- [ ] `go test ./agent/ -count=1 -timeout 60s` 通过
- [ ] `go test ./... -short -count=1 -timeout 180s` 全部通过

### Task 1.9: 验证阶段 1

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [ ] `wc -l agent/agent.go agent/lifecycle.go agent/event_loop.go agent/inject.go agent/session.go agent/a2a.go agent/helpers.go` 每个文件 ≤ 350 行
- [ ] `ls agent/tagent_agent.go 2>/dev/null` 确认已删除

---

## 阶段 2: 创建 rl/ 包

> 目标: 将 TrajectoryRecorder + HTTPAPI + SwappableModel 移到独立包
> 方法: 先创建新包 + 接口，再逐个移动文件，每步编译验证

### Task 2.1: 创建 rl/ 包骨架

- [ ] 创建 `rl/` 目录
- [ ] 创建 `rl/agent_loop.go`：定义 AgentLoop 接口
  ```go
  package rl
  
  import (
      "trpc.group/trpc-go/trpc-agent-go/event"
      "trpc.group/trpc-go/trpc-agent-go/model"
  )
  
  // AgentLoop is the interface that decouples rl/ from agent/.
  type AgentLoop interface {
      InjectMessage(msg model.Message)
      InjectMessageWithSource(source string, msg model.Message)
      StartLoop(userID, sessionID string) (<-chan *event.Event, error)
      StopLoop()
  }
  ```
- [ ] `go build ./rl/` 通过

### Task 2.2: 移动 TrajectoryRecorder

- [ ] 将 `agent/trajectory_recorder.go` 复制到 `rl/trajectory_recorder.go`
- [ ] 修改包名为 `package rl`
- [ ] 修改 import 路径（如有引用 agent 包内部类型，改为从 rl 包外导入或通过参数传入）
- [ ] 在 `agent/` 中保留一个 type alias 或重导出（暂时，避免外部 break）：
  ```go
  // agent/trajectory_compat.go
  // Deprecated: Use rl.TrajectoryRecorder directly.
  type TrajectoryRecorder = rl.TrajectoryRecorder
  ```
- [ ] 删除 `agent/trajectory_recorder.go`
- [ ] `go build ./...` 通过
- [ ] 将 `agent/trajectory_recorder_test.go` 移到 `rl/trajectory_recorder_test.go`，修改包名
- [ ] `go test ./rl/ -v` 通过

### Task 2.3: 移动 SwappableModel

- [ ] 将 SwappableModel 相关代码从 `agent/helpers.go` 提取到 `rl/swappable_model.go`
- [ ] 修改包名为 `package rl`
- [ ] 在 `agent/` 中保留 type alias（暂时）：
  ```go
  // agent/swappable_compat.go
  type SwappableModel = rl.SwappableModel
  var NewSwappableModel = rl.NewSwappableModel
  ```
- [ ] `go build ./...` 通过

### Task 2.4: 移动 HTTPAPI

- [ ] 将 `agent/http_api.go` 复制到 `rl/http_api.go`
- [ ] 修改包名为 `package rl`
- [ ] 将 `HTTPAPI` 结构体中对 `*TagentAgent` 的引用改为 `AgentLoop` 接口
- [ ] 修改 `NewHTTPAPI` 签名：`func NewHTTPAPI(agent AgentLoop) *HTTPAPI`
- [ ] 在 `agent/` 中保留 type alias（暂时）：
  ```go
  // agent/http_api_compat.go
  type HTTPAPI = rl.HTTPAPI
  var NewHTTPAPI = rl.NewHTTPAPI
  ```
- [ ] 删除 `agent/http_api.go`
- [ ] `go build ./...` 通过
- [ ] 将 `agent/http_api_test.go` 移到 `rl/http_api_test.go`，修改包名
- [ ] `go test ./rl/ -v` 通过

### Task 2.5: 更新外部引用（移除 compat alias）

- [ ] 更新 `tagent.go`：
  - import `"github.com/SpellingDragon/tagent/rl"` 替代 `agent.TrajectoryRecorder` 等
  - 替换 `agent.NewTrajectoryRecorder` → `rl.NewTrajectoryRecorder`
  - 替换 `agent.TrajectoryRecorder` → `rl.TrajectoryRecorder`
- [ ] 更新 `examples/wechat-bot/main.go`：
  - import `"github.com/SpellingDragon/tagent/rl"`
  - 替换 `agent.NewSwappableModel` → `rl.NewSwappableModel`
  - 替换 `agent.NewHTTPAPI` → `rl.NewHTTPAPI`
- [ ] 删除 `agent/trajectory_compat.go`、`agent/swappable_compat.go`、`agent/http_api_compat.go`
- [ ] `go build ./...` 通过
- [ ] `go test ./... -short -count=1` 全部通过

### Task 2.6: 验证阶段 2

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [ ] `ls rl/` 确认有 4 个 .go 文件：`agent_loop.go`, `trajectory_recorder.go`, `swappable_model.go`, `http_api.go`
- [ ] `grep -r "agent.TrajectoryRecorder\|agent.HTTPAPI\|agent.SwappableModel\|agent.NewHTTPAPI\|agent.NewSwappableModel" --include="*.go" | grep -v "_test.go"` 无结果（所有引用已更新）

---

## 阶段 3: 清理 + 最终验证

> 目标: 清理残留、验证完整性

### Task 3.1: 检查 agent 包 unused import

- [ ] 运行 `goimports -w agent/` 或手动检查每个新文件的 import 是否有未使用项
- [ ] `go build ./...` 通过

### Task 3.2: 检查文件行数

- [ ] `wc -l agent/agent.go agent/lifecycle.go agent/event_loop.go agent/inject.go agent/session.go agent/a2a.go agent/helpers.go`
- [ ] 每个文件 ≤ 350 行（如超过，考虑是否有可进一步拆分的函数）

### Task 3.3: 更新 README 项目结构

- [ ] 在 `README.md` 的"项目结构"章节中新增 `rl/` 条目
- [ ] 更新 `agent/` 的描述（注明文件拆分）

### Task 3.4: 最终验证

- [ ] `go build ./...` 通过
- [ ] `go vet ./...` 无警告
- [ ] `go test ./... -short -count=1 -timeout 180s` 全部通过
- [ ] `cd examples/wechat-bot && go build .` 通过
- [ ] `go test -run TestInvariant ./tests/ -v` 三个不变量测试通过
- [ ] `find agent/ -name "*.go" ! -name "*_test.go" | wc -l` 确认 agent 包有 ~15 个源文件（原13 + 新增7 - 删除tagent_agent.go - 移出3 = ~16）
- [ ] `find rl/ -name "*.go" ! -name "*_test.go" | wc -l` 确认 rl 包有 4 个源文件

---

## 迁移说明

### 对下游应用的影响

1. **import 路径变更**：
   - `agent.TrajectoryRecorder` → `rl.TrajectoryRecorder`（import `"github.com/SpellingDragon/tagent/rl"`）
   - `agent.NewHTTPAPI` → `rl.NewHTTPAPI`
   - `agent.NewSwappableModel` → `rl.NewSwappableModel`
   - `agent.SwappableModel` → `rl.SwappableModel`

2. **无行为变更**：所有 API 签名、功能、默认值保持不变

3. **TagentAgent / TagentConfig / CompressConfig / MeditationConfig 保持在 `agent/` 包中不变**

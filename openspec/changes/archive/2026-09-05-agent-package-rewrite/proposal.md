## Why

`agent/tagent_agent.go` 当前 1232 行，承担了过多职责：Agent 生命周期管理、事件循环、Session 上下文、A2A 服务器、InjectMessage/InjectMessageWithMetadata、Closer 注册、子 Agent 分发等。这使得代码难以定位、难以测试、难以独立修改。

同时，`agent/` 包内的 13 个源文件通过私有字段直接访问彼此的结构体（`ta.contextManager`、`ta.projection`、`cm.bus`），形成了"假内聚"——它们在物理上共享一个包，但逻辑上是独立的子系统。

本变更进行大胆重写：
1. **拆分 tagent_agent.go**：将 1232 行拆为 5 个职责清晰的文件（均保持在 `agent/` 包内）
2. **拆出 toolwrap 包**：将 `AgentToolWrapper` 和 `OutputLimitTool` 移到独立包
3. **拆出 rl 包**：将 `TrajectoryRecorder` 和 `HTTPAPI` 移到独立包

上下文管理（context_manager + smart_compress + context_compressor + projection_organizer）保留在 `agent/` 包内，因为它们与 EventBus/Projection/onEvent 回调有密切的私有字段交互，强制拆分只会增加导出表面和接口适配代码。

## What Changes

### tagent_agent.go 拆分（agent/ 包内重组）

将 `tagent_agent.go` (1232 行) 拆为：

| 新文件 | 行数估计 | 职责 |
|--------|---------|------|
| `agent.go` | ~250 | TagentAgent 结构体定义、NewTagentAgent 构造、Config 结构体、默认值常量 |
| `lifecycle.go` | ~200 | StartLoop/StopLoop/Close/RegisterCloser/SetTrajectoryRecorder |
| `event_loop.go` | ~250 | runEventLoop/extractTriggerSource/extractRootMetadata/publishErrorEvent/summarizeEvents |
| `inject.go` | ~150 | InjectMessage/InjectMessageWithSource/InjectMessageWithMetadata/IngestExternalEvents |
| `session.go` | ~150 | Run()/setActiveBus/restorePersistentBus/setSessionContext/makeOnEventCallback |
| `a2a.go` | ~100 | A2AServer/NewA2AServer/Start |
| `helpers.go` | ~80 | buildCompressorOpts/newCompressorFromConfig/newContextManagerFromConfig |

总计约 1180 行（接近原始 1232 行，但职责分离清晰）。

### 拆出 toolwrap/ 包

- `toolwrap/agent_tool_wrapper.go` — AgentToolWrapper 结构体、NewAgentToolWrapper、Declaration、Call、外部上下文序列化/反序列化
- `toolwrap/output_limit_tool.go` — OutputLimitTool
- `toolwrap/types.go` — PlainToolFactoryConfig、ToolAgentFactoryConfig 等类型定义

**接口适配**：AgentToolWrapper 需要调用 TagentAgent.Run() 和 MemoryStore.GetEvent()。这两个都是已导出的接口，无需新增导出。

### 拆出 rl/ 包

- `rl/trajectory_recorder.go` — TrajectoryRecorder
- `rl/http_api.go` — HTTPAPI、SetModelUpdateFn、/task /healthz handler

**接口适配**：TrajectoryRecorder 实现 `model.Model` 接口（wrap 底层 model），HTTPAPI 需要 TagentAgent.InjectMessage 和 StartLoop/StopLoop。通过接口注入解耦。

### 保留在 agent/ 包内不动的文件

- `event_bus.go` — EventBus + AgentEvent
- `projection.go` — SessionProjection
- `meditation.go` — MeditationManager
- `context_manager.go` — ContextManager + BeforeModel 回调
- `context_compressor.go` — ContextCompressor
- `smart_compress.go` — SmartCompressor
- `task_segmenter.go` — TaskSegmenter
- `projection_organizer.go` — ProjectionOrganizer

这些文件与核心事件引擎有密切的私有字段交互（`cm.bus.TryPull()`、`cm.projection.GetAll()`），保持在同一包内避免不必要的导出。

## Capabilities

### New Capabilities

- `agent-file-split`: tagent_agent.go 按职责拆分为 7 个文件，每个文件单一职责
- `toolwrap-package`: AgentToolWrapper 独立为 toolwrap/ 包，通过接口注入依赖
- `rl-package`: TrajectoryRecorder + HTTPAPI 独立为 rl/ 包，通过接口注入依赖

### Modified Capabilities

（无——不修改任何行为，纯结构重组）

## Impact

### 代码变更

- **删除文件**：`agent/tagent_agent.go`（拆分为 7 个文件）、`agent/tool_agent.go`（移到 toolwrap/）、`agent/output_limit_tool.go`（移到 toolwrap/）、`agent/trajectory_recorder.go`（移到 rl/）、`agent/http_api.go`（移到 rl/）
- **新增文件**：`agent/agent.go`、`agent/lifecycle.go`、`agent/event_loop.go`、`agent/inject.go`、`agent/session.go`、`agent/a2a.go`、`agent/helpers.go`、`toolwrap/*.go`、`rl/*.go`
- **修改文件**：`tagent.go`（import 路径 + 类型引用更新）、`builtin.go`（PlainToolFactoryConfig 类型引用）、`registry.go`（类型引用）、`examples/wechat-bot/main.go`（import）

### 测试

- 所有测试文件随源文件移动或保留在原位
- `agent/*_test.go` 保留（包内测试，不受拆分影响）
- `tests/*_test.go`（外部测试）需更新 import 路径
- 新增 `toolwrap/*_test.go` 和 `rl/*_test.go`

### 向后兼容

- **BREAKING**：`agent.AgentToolWrapper` → `toolwrap.AgentToolWrapper`
- **BREAKING**：`agent.TrajectoryRecorder` → `rl.TrajectoryRecorder`
- **BREAKING**：`agent.HTTPAPI` → `rl.HTTPAPI`
- **BREAKING**：`agent.PlainToolFactoryConfig` → `toolwrap.PlainToolFactoryConfig`
- **BREAKING**：`agent.ToolAgentFactoryConfig` → `toolwrap.ToolAgentFactoryConfig`
- 项目未公开发布，内部 BREAKING 可接受

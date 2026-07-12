## Why

`agent` 包当前 13 个源文件、~4900 行代码，承担了 6 个独立职责：核心事件循环、上下文管理（压缩/分段/回调链）、工具包装、RL 集成、冥想心跳、计划进度追踪。文件间通过私有字段直接访问，耦合度高。项目孵化期无需考虑兼容性，适合按原型边界彻底拆分。

## What Changes

**BREAKING** — 按 B 方案彻底分包，重命名所有跨包引用：

- `agent` 包精简为核心：`tagent_agent.go`（生命周期 + 事件循环）、`event_bus.go`、`projection.go`、`meditation.go`。导出必要字段供跨包访问
- 新建 `contextmgr` 包：`context_manager.go`、`smart_compress.go`、`task_segmenter.go`、`chunk_splitter.go`、`plan_progress_tracker.go`、`compactor.go`（从 task_segmenter 拆出）
- 新建 `toolwrap` 包：`tool_agent.go`（AgentToolWrapper）、`output_limit_tool.go`
- 新建 `rl` 包：`trajectory_recorder.go`、`http_api.go`
- `tagent.go`（根包）更新所有 import 路径
- `examples/wechat-bot/main.go` 更新 import 路径
- 所有测试文件随源文件移动

## Capabilities

### New Capabilities
- `package-restructure`: agent 包按原型边界拆分为 agent / contextmgr / toolwrap / rl 四个包

### Modified Capabilities
（无——不修改已有 spec 的需求）

## Impact

- 新建 `contextmgr/`、`toolwrap/`、`rl/` 三个包目录
- 移动 9 个源文件 + 对应测试文件到新包
- `agent/tagent_agent.go` 导出 `PersistentBus`、`ActiveBus`、`ContextManager`、`Projection`、`Config` 等字段或通过构造注入
- `agent/event_bus.go` 导出 `AgentEvent` 已满足（已导出）
- `tagent.go`、`builtin.go`、`registry.go`、`testing.go`、`examples/wechat-bot/main.go` 更新 import
- 预计 ~15 个文件的 import 路径变更

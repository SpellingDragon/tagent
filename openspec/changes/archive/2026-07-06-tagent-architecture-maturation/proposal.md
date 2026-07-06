## Why

tagent 的原型（`prototype/agent.go`）用 126 行证明了事件驱动 Agent 的最小骨架，当前生产实现已基于 trpc-agent-go 完成了 EventBus + runEventLoop + ContextManager + MemoryPlugin 的核心架构。但回归初心审视，仍有几个关键差距：

1. **事件驱动闭环不完整**：原型中工具结果显式回写 eventBus，当前实现的工具结果仅通过框架 Runner 回流到 SessionProjection，未重新进入 EventBus，限制了"工具结果触发额外逻辑"的能力。
2. **错误韧性不足**：`runEventLoop` 中 `RunFlow` 失败只打日志，无重试/退避/降级策略。
3. **压缩缺乏可观测性**：SmartCompressor/Compactor 触发条件、压缩前后 token 数、丢弃任务列表仅靠日志，无法结构化监控和调优。
4. **MemoryStore 生产化待验证**：FileSegmentStore 的崩溃恢复、并发写入稳定性、RelationStore 持久化缺乏端到端验证。
5. **A2A 远程调用缺乏韧性**：无超时、重试、熔断机制。
6. **RL 训练闭环未端到端验证**：TrajectoryRecorder 能记录但 AReaL 格式转换、奖励反馈链路仅脚本级别。
7. **三个不变量缺乏自动化测试**：投影、Compact 范围、工具结果回流的不变量没有回归保护。

## What Changes

- **事件循环韧性增强**：`runEventLoop` 中 `RunFlow` 失败后引入指数退避重试（最多 3 次），超过重试上限后降级为日志 + continue 而非静默吞掉
- **工具结果 Bus 桥接**：框架 Runner 产生的 `action_command` 事件在 `onEvent` 回调中同步 `Publish` 到 EventBus，使工具结果可被外部监听器（如冥想响应、TmuxMonitor 回调链）消费
- **压缩可观测性**：`SmartCompressor`/`Compactor` 触发时发出结构化指标（压缩前 token、压缩后 token、丢弃段数、保留段数、耗时），通过 `log.Infof` + 可选 OTLP span attribute 输出
- **MemoryStore 崩溃恢复验证**：FileSegmentStore 在写入时使用临时文件 + rename 原子操作；启动时检测并修复未完成的 compaction 残留；RelationStore WAL + snapshot 在崩溃后自动恢复
- **A2A 超时与重试**：`AgentToolWrapper.Call` 增加 context timeout（默认 120s），远程调用失败后重试 1 次；`A2AAgent.Run` 的 HTTP 请求设置超时
- **RL 训练闭环验证**：TrajectoryRecorder 增加 `flush` 机制确保数据落盘；AReaL 格式转换脚本与 `train/rl/convert_trajectories.py` 集成测试验证
- **架构不变量测试**：新增 `tests/invariants_test.go`，自动化验证三个不变量：(1) SessionProjection 只含 EventReference (2) Compactor 不修改 MemoryStore (3) 工具结果经 onEvent 回流到 SessionProjection
- **BREAKING**：`runEventLoop` 在 `RunFlow` 连续失败超过重试上限后，将失败事件 `Publish` 到 EventBus（source=`error`），而非静默 continue。消费方需处理 `Source == "error"` 的 AgentEvent

## Capabilities

### New Capabilities

- `event-loop-resilience`: runEventLoop 错误处理 — RunFlow 失败重试、退避策略、错误事件发布到 EventBus
- `compression-observability`: SmartCompressor/Compactor 结构化指标 — 压缩前后 token、丢弃段数、保留段数、耗时、触发阈值
- `memstore-production-hardening`: FileSegmentStore 原子写入、崩溃恢复、RelationStore WAL 持久化验证
- `a2a-resilience`: AgentToolWrapper 超时与重试、A2AAgent HTTP 超时
- `rl-training-pipeline`: TrajectoryRecorder flush 机制、AReaL 格式转换端到端验证
- `architecture-invariant-tests`: 三个原型不变量的自动化回归测试

### Modified Capabilities

- `persistent-event-loop`: 更新 spec 以反映 EventBus 架构（旧 spec 引用 mailbox/mergeBatch），增加工具结果 Publish 到 Bus 的要求
- `remote-agent-communication`: 增加超时、重试要求到 AgentToolWrapper.Call
- `trajectory-recording`: 增加 flush 机制和 AReaL 端到端验证要求

## Impact

**代码变更范围**：
- `agent/tagent_agent.go` — `runEventLoop` 增加重试逻辑和错误事件发布
- `agent/context_manager.go` — `RunFlow` 增加重试支持；`onEvent` 回调增加工具结果 Bus 桥接
- `agent/smart_compress.go` — 增加结构化指标输出
- `agent/task_segmenter.go` — Compactor 增加指标输出
- `agent/tool_agent.go` — `AgentToolWrapper.Call` 增加超时和重试
- `memory/segment_store.go` — 原子写入（tmpfile + rename）
- `memory/relation_store.go` — WAL 恢复验证
- `agent/trajectory_recorder.go` — 增加 Flush 方法
- `tests/invariants_test.go` — 新增不变量测试

**不涉及**：
- prototype/agent.go 不修改（原型是契约，不是实现）
- trpc-agent-go 框架代码不修改
- 现有工具实现（ActionTool/RecallAgent/KnowledgeAgent）核心逻辑不变

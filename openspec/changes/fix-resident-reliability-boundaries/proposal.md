# 常驻可靠性边界收敛（已废弃）

> 状态：SUPERSEDED。用户已要求废弃本计划；唯一替代入口为 [complete-resident-reliability-protocol](../complete-resident-reliability-protocol/proposal.md)。以下仅为历史证据，不得继续 apply、不得按已完成 archive、不得同步本目录的 delta 到主规格。原有源码保留为新计划基线；历史完成标记不构成验收。

## Why

对 `aeb273d..a16fdce` 的两日变更审查发现，可靠输入在来源保真、事实提交、完成确认和资源关闭之间仍有断点，离线存储基准也遗漏生产耐久屏障。需要修复全部十项发现，将成功语义落实到事实、模型请求和实际投递决定，而不扩大为架构重写。

## What Changes

- 可靠输入无损保留消息、来源、时间和业务 Metadata；每个消息槽位在事实写入前持久化固定身份与规范化事实载荷，重放不再依赖事后紧凑追加的 EventKeys。
- **BREAKING（消费时机）**：按用户确认的批次语义，每次 turn 开始时冻结已拉取事件，合并为一次输入并计作一个 turn；执行中新到事件进入下一批。移除 `BeforeModel` 中途消费 bus，不增加动态 turn claim 登记系统。
- 按冻结批次、逐 envelope 管理输入提交与完成确认：失败不冒充已存储；被过滤或空输入有明确处置结果；只有事实与完成 receipt 满足提交条件后才能 Ack。
- 保持公共 `StoreEvent` 对重复 EventKey 的拒绝语义，内部重放使用显式窄接口补齐缺失原文、索引和必要元数据；修复重复增计数及由此触发的过度淘汰。
- 将共享 backend、engine、后台 worker 和 writer lock 纳入同代资源 owner；租约释放与 agent Close 幂等，关闭后 reopen 不引用旧实例。
- 将 recovery notice 放到最终模型请求装配尾部，一次性消费，不进入 FullEvent 或 projection；durable、volatile、one-shot 路径行为一致。
- 离线基准显式保留 `Sync` 等被测能力及错误，重新测量 storage 矩阵，区分屏障语义、实际数据规模、Go 内存统计与真实 RSS；保留旧原始报告但撤销不成立的耐久性能解释。
- **BREAKING（可靠 inbox 文件格式）**：新写入使用 `inbox-v2`；未排空的 v1/旧 spill 阻止升级，不猜测已丢失的 Metadata 或错位 key；回退旧二进制前必须排空 v2 或继续由支持 v2 的版本消费。

## Capabilities

### New Capabilities

无。复用现有 inbox、事实存储、资源 registry、请求装配和验证体系。

### Modified Capabilities

- `persistent-event-loop`：固定批次的 turn 边界、无损可靠输入、预备身份、逐 envelope 的提交/处置/receipt/Ack 条件与格式升级门。
- `event-segment-store`：公共重复拒绝与内部重放分离，缺槽补写、并发提交、幂等计数和计数未知时的淘汰约束。
- `runtime-resource-ownership`：一次性同代租约、backend/engine 完整所有权、并发关闭及 reopen、构建失败回收。
- `event-sourced-projection`：恢复提示只存在于实际请求尾部，失败或未调用模型时不提前消费。
- `resident-release-evidence`：测试装饰器能力保真、同语义基准对比与十项问题的端到端验收。
- `async-tool-event-fix`：将旧 `InjectBusInputs` 中途注入条款改为批次消费时归一化 role，保留原始消息不变。

## Impact

- 输入链：`agent/event_bus.go`、`agent/reliability/inbox.go`、`agent/event_loop.go`、`agent/context_manager.go`、`agent/lifecycle.go`、`plugin/` 及其测试。
- 存储链：`memory/segment_store.go`、`memory/in_memory_store.go`、错误追踪/engine 装饰器及 mem_spill 重放；`MemoryStore` 主接口和既有 FullEvent/段键格式不变。
- 生命周期：`resources.go`、`wiring.go`、`build_agent.go`、`memory/engine/`；保持 agent 身份隔离、配置指纹及执行壳借用语义。
- 消费与证据：`examples/wechat-bot` 投递测试、`tests/resident_e2e_test.go`、升级回滚演练、`tests/offline_bench/` 和相关操作文档。
- 不增加依赖、数据库或消息中间件；不默认开启可选能力，不承诺工具副作用或消息发送 exactly-once，不修改压缩算法、token 估算器或查询索引。
- 本次只生成提案工件；实现、源码回归、真实部署、付费调用、提交/推送和归档均未执行，也不由本提案隐含授权。

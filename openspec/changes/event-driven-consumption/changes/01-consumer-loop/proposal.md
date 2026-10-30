# 01-consumer-loop — 消费循环

父变更：`event-driven-consumption`（依赖 `design.md#D6` 冻结签名）。

## 边界与依赖

- **依赖**：`design.md#D6` 冻结结构（`ConsumptionBatch` / `ConsumptionFact` / 会话消费者签名）；`api/event` 事实族只读。
- **被依赖方**：02 会话入口、03 分段闭合、04 派生读面。
- **接口面**：批次取用判据（哪些事实算「已到达且未被取走」）、`ConsumptionFact` 的记录点与字段、工具结果进入后续批次的路径。
- **禁止事项**：不改 `assembly/*`；不改 `api/event` 事实族；不引入会话级锁（claim / Occupancy 扩展均被否）；不改变一次模型请求只看固定批次的性质。

## Why

`dispatch` 先派发全部工具再逐个 `await`（`runtime/agent/loop.go`），慢工具把快结果与新输入一起挡住；`boundaryInput` 只按本 invocation 身份与 `startsRange` 取数，工具结果与新输入进不了下一次消费。这是三条症状的共同根因。

## What Changes

1. 工具调用派发后不再同步等待结果；receipts 仍进当轮 transcript，结果作为后续批次的事件。
2. 批次取用按「本会话、已到达、未被此前消费取走」判定，跨来源合批，来源关联保留。
3. 一次模型请求只看冻结批次；批次内事件不重复消费。
4. 每次模型产出的 `agent_output` 记录本次请求实际携带的输入键（消费关系），供 03 段闭合。

## Risk

- 批次判据放宽会改变大量既有锚；缓解：红锚先行，旧锚逐条按新语义迁移（禁改断言绕过）。
- 工具结果从同步 observation 改为后续批次，会改变下一次请求的消息形状；需与 03 的段语义一起验收。

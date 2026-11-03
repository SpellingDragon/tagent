# storm-fuse Specification

## Purpose

本规范定义长响应风暴的熔断：单响应工具调用整批挂人审而不截断、降级消息限长、长上下文同形态挂审，且关闭态零行为差异。

## Requirements

### Requirement: 单响应工具调用整批挂人审

单次模型响应的工具调用数 SHALL 受 `max_calls_per_response`（0=关）约束：超限时**整批 N 个调用作为一次审批**挂起（审批插件检查 input、人确认后执行），MUST NOT 截断丢弃。无审批簿接线时 SHALL fail-closed 拒绝整批（不得回退为截断或直接执行）。

#### Scenario: 超限挂审
- **WHEN** 模型单响应发出 N>K 个调用且审批簿已接线
- **THEN** 审批簿恰登记 1 项（含 N 调用清单；call ID 不进 digest），受理数为 0；批准后整批按原顺序执行恰一次（N 条受理回执+1 条批次通知），拒绝后零执行（N 条拒绝回执），过期后零执行

#### Scenario: 无簿全拒
- **WHEN** 超限响应到达而无审批簿
- **THEN** 整批拒绝执行，每调用一条具名回执，审批面零登记

### Requirement: 降级消息限长

sanitize 降级 user message（调用与结果两类路径）的 payload 段 SHALL 受 4KB 上限约束（超出截断并标记），防降级消息二次膨胀链。

#### Scenario: 超长 payload 截断
- **WHEN** 无效调用或无效结果携带 >4KB payload 被降级
- **THEN** 降级消息 ≤4KB 且携带截断标记，代码块闭合

### Requirement: 长上下文防线同挂人审

prompt_tokens 超过 `long_context_tokens`（0=关）时，该响应的全部调用 SHALL 同形态整批挂审批（与调用数上限同一代码点、同一审批通道）。

#### Scenario: 超阈值挂审
- **WHEN** 会话 prompt 超阈值且模型发出多个并行调用
- **THEN** 整批挂审批，三态语义与调用数上限一致

### Requirement: 关闭态零行为差异

两键均为 0 时系统行为 SHALL 与熔断引入前逐字节等价。

#### Scenario: 兼容缺省
- **WHEN** 两配置均为 0
- **THEN** 全量集成测试与基线一致

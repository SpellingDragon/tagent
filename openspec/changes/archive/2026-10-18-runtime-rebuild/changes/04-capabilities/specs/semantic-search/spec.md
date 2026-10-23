# semantic-search Specification

## Purpose

本 delta 使向量链路与重建运行时对齐：embedding 活绑定随代持有、异步生成不阻塞、未配置时零行为变化，且与预算 epoch 无耦合冲突。

## ADDED Requirements

### Requirement: 向量链路与 state 绑定/预算对齐

异步向量生成流水线 SHALL 使用**当次请求借用代**的 embedder 绑定；事件入库 MUST NOT 等待向量；embedding 未配置时 MUST 保持现状（零行为变化）。向量生成与检索 MUST NOT 修改事件原文或工具声明，且 MUST NOT 与预算 epoch 互相侵蚀。

#### Scenario: 入库不等待向量（并发）

- **WHEN** embedding 已配置且 embedder 响应缓慢（>1s）
- **THEN** `StoreEvent` 同步路径耗时不受影响
- **AND** 向量在事件可查之后异步就绪

#### Scenario: 未配置零行为变化（失败）

- **WHEN** 未配置 embedding 段
- **THEN** 不产生 embedding 调用，行为与埋点前一致

#### Scenario: 队列满丢弃可观测（失败）

- **WHEN** 投递队列满
- **THEN** 该事件向量被丢弃并计数递增，事件本身正常入库

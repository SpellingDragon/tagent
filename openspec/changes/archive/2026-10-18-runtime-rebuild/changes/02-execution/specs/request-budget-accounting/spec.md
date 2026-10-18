# request-budget-accounting delta（02-execution）

> 复用登记（非规范，不改本条语义）：usage 尾包归约与单终局消费共享同一终局消费路径，不产生第二套归约入口或第二个终局来源。
> 本 delta 只新增「usage 尾包归约与单终局消费」；本能力既有 Requirement（单次请求声明快照、完整预算输入与诚实精度、单一压缩及安全消费、同步摘要可界定等待）由主 spec `openspec/specs/request-budget-accounting/spec.md` 继承（**证据映射，不改**）。

## ADDED Requirements

### Requirement: usage 尾包归约与单终局消费

模型流末尾携带的 usage（token 用量）尾包 SHALL 归约进**同一**终局消费路径，MUST NOT 触发独立第二轮请求或第二循环。一次请求的 token 结算 SHALL 恰好消费一次 usage；重复到达的 usage 与尾包 MUST 去重，MUST NOT 二次计入预算账目。

usage 尾包 SHALL 与被拒发（未实际进入模型）区分：请求未实际进入模型时 MUST NOT 消费 usage、MUST NOT 吞掉一次性恢复提示。归约入口 SHALL 单一；异常尾包 SHALL 明确呈现，MUST NOT 静默丢弃。

#### Scenario: 尾包单次消费

- **WHEN** 一次流在末尾给出 usage 尾包
- **THEN** 账目 SHALL 恰好计入一次，尾包不再另起一轮请求

#### Scenario: 重复 usage 去重

- **WHEN** 同一请求的 usage 被重复到达
- **THEN** 账目 MUST NOT 二次计入

#### Scenario: 未实际进入模型不消费 usage

- **WHEN** 请求因门禁被拒发、未实际进入模型
- **THEN** MUST NOT 消费 usage、MUST NOT 吞掉一次性恢复提示

#### Scenario: 异常尾包可观测

- **WHEN** usage 尾包格式异常
- **THEN** 异常 SHALL 明确呈现（含日志），MUST NOT 静默丢弃后默认成功

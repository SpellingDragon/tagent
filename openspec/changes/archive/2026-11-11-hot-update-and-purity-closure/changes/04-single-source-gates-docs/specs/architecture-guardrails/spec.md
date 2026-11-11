# architecture-guardrails Specification（MODIFIED delta）

## Purpose

常量与门禁单一真相源：换算常量机械连接登记处，gate 解析覆盖映射式与列表式 agent 且不误判合法新键，文档不述已撤机制。

## MODIFIED Requirements

### Requirement: 常量单源机械连接

换算/overhead 常量 SHALL 单一登记并机械连接消费点，SHALL NOT 靠注释声称两表一致。

#### Scenario: 反扫命中

- WHEN 消费点另立同值常量
- THEN 常量登记门报未登记/双源 exit1

### Requirement: gate 覆盖两式 agent

特性边界门 SHALL 覆盖映射式与列表式 agent，且 SHALL NOT 误判声明面合法新键。

#### Scenario: 列表式漏检修复

- WHEN 列表式 `- id:` agent 挂 feature-specific 段
- THEN gate 检出 exit1

#### Scenario: 合法新键不误判

- WHEN 映射式 agent 用声明面合法键 output_reserve
- THEN gate 放行 exit0

### Requirement: 文档不述已撤机制

本域 owning 文档 SHALL NOT 残留已撤配置键/机制描述。

#### Scenario: 撤键零残留

- WHEN grep 已撤检索键/capture_budget_mb 于 owning 页
- THEN 零命中
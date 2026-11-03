# architecture-guardrails Delta

## ADDED Requirements

### Requirement: 领域公共面与接口形状分门验收

接口形状预算与领域公共面 SHALL 是两个独立合并结果。新增或改名的公共命名类型、接口、FactType/状态词 SHALL 绑定既有 canonical concept、类别（fact/read/declaration/port）和唯一非 superseded 主规格；基线、绑定目标或锚读取失败 MUST fail-closed。普通方法、函数和测试支架不逐个登记，合法读面不得按符号数量误杀。

#### Scenario: 零字段机制新增导出
- **WHEN** 一个 writer 台账或序号围栏作为新导出类型出现且没有独立领域事实依据
- **THEN** domain-surface 必须具名拒绝，即使既有形状预算没有发现

#### Scenario: 合法读投影
- **WHEN** 一个公共读面只从既有投递事实计算且不持自有权威
- **THEN** 它可绑定为 read，不计作新领域事实，也不因导出符号增加而失败

#### Scenario: 同名搬迁或纯改名
- **WHEN** 公共概念只搬迁或改名且语义权威不变
- **THEN** 概念净额为 0，不得报告为新增或收敛

### Requirement: 失效规范不得保留现行命令

标记 superseded 的主规格 MUST NOT 在 Requirements 中继续包含现行 SHALL/MUST。仍有效的条款 SHALL 先迁移到唯一现行 capability 并映射公共行为证据；历史文本 SHALL 留在归档而非主规格。

#### Scenario: superseded 仍含旧循环命令
- **WHEN** persistent-event-loop 标为 superseded 而 Requirements 仍要求 RunFlow/StopLoop/mailbox 双模式
- **THEN** 规范权威检查必须失败，单加注释不得使其通过

#### Scenario: 有效残条迁移
- **WHEN** 旧规格仍含现行可靠输入、通知或安全要求
- **THEN** 每条必须先映射到唯一现行规格及公共消费者锚，之后才能从旧规格删除

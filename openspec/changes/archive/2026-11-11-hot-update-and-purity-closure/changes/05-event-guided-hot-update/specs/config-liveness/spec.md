# config-liveness Specification（MODIFIED delta）

## Purpose

声明事实为装配权威：boot 对账/Adopt 覆盖每代消费者，崩溃窗口退役收敛，单次 reload 尝试单一 outcome 事实。

## MODIFIED Requirements

### Requirement: boot 采纳新消费者

boot 重放 SHALL 从声明事实重建每代消费者（含新热更档），并对被取代且无后续借用的代补退役。

#### Scenario: 崩溃窗口收敛

- WHEN 崩溃发生在换指针与退役 emit 之间后重启
- THEN boot 补退役、无泄漏句柄、无双重释放

#### Scenario: Adopt 新消费者

- WHEN boot 重放含新热更档消费者的声明事实
- THEN 重建该消费者当前代实例

### Requirement: 单次尝试单一 outcome

一次 reload 尝试 SHALL 仅一条 outcome 事实（applied 或 held），SHALL NOT 先乐观 applied 后 held 矛盾并存。

#### Scenario: 安装失败单一 held

- WHEN 候选安装失败
- THEN 该尝试仅 held 事实，无先前 applied 事实
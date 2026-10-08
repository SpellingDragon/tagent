# minimal-form-exploration Specification

## ADDED Requirements

### Requirement: 三档能力分类表交付

报告 SHALL 交付能力三档分类表：默认装配（常驻基础）/ opt-in（部署选项）/ 死代码候选（历史遗留），每项含代码量级、消费者证据与归属理由。E12 的"最小运行形态"问题 SHALL 以此表作答。

#### Scenario: 分类可稽

- **WHEN** 编排者四查本报告
- **THEN** 三档合计 ≥15 项且每项有量级与消费者证据

### Requirement: 跨域冲突登记

X1-X5 报告间的同题异判结论 SHALL 被登记为冲突对（双方报告出处+判定差异+建议裁决方向），登记 SHALL NOT 替代 W3c 编排者裁决。

#### Scenario: 冲突不丢失

- **WHEN** W3c 汇总处理跨域结论
- **THEN** 冲突对清单可直接引用（含出处行）

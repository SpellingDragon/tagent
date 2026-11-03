## ADDED Requirements

### Requirement: 核心语义经正式消费者闭环

常驻消费、send 回执、通知结清与关闭语义 SHALL 由根包公共装配及至少一个真实消费者端到端验证。测试额外装配的内部接线、helper 直调或零匹配测试 MUST NOT 作为生产完成证据。

#### Scenario: 组件绿但正式入口断链

- **WHEN** 内部组件测试通过而公共入口未调用该实现或负向路径仍可达
- **THEN** 对应任务 SHALL 保持未完成并具名报告断链位置

#### Scenario: CI 与产品失败分线

- **WHEN** 门禁出现超时、资源预算或 runner 抖动
- **THEN** 处置 SHALL 限于测试/CI 面；MUST NOT 新增或修改生产状态机来迎合时序，产品断言失败仍按产品缺陷处理

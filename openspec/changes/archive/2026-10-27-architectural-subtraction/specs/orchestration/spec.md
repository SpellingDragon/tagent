# Orchestration 契约

## ADDED Requirements

### Requirement: 启动门

批 1 SHALL 以 fidelity-and-recovery 全部完成为前置（G4/3.2/1.2）。

#### Scenario: 前置未齐不派发

- **WHEN** fidelity-and-recovery 尚有未勾叶
- **THEN** 批 1 SHALL NOT 派发

### Requirement: 每批三问

每批收尾 SHALL 回答：删了什么状态（diff）/责任归谁（报告）/原有验收仍成立（全量+定点破坏）。

#### Scenario: 三问缺一不勾批

- **WHEN** 任一问无证据
- **THEN** 该批 SHALL NOT 勾选

### Requirement: 不变量红即违例

D5 锚测试任一红 SHALL 视为对应不变量违例，阻断收口。

#### Scenario: 锚红不得收口

- **WHEN** 任一锚测试红
- **THEN** F3 SHALL NOT 勾选

### Requirement: 验收锚随任务行冻结

每叶验收面（正锚/负锚/消费者锚）SHALL 随任务行冻结于派发前（design D6）；执行期只对账，追认缩小验收面=该叶重开。

#### Scenario: 缩水即重开

- **WHEN** 完成时以替代测试或缩小的验收面勾选
- **THEN** 该叶 SHALL 重开

### Requirement: 波后独立偏差评估

每批执行完、任务勾选前 SHALL 派独立验证子代理（与实施者分离）对照原任务行验收面五面核查（产物/正式链路/负锚/勾选/目标覆盖），偏差分级处置，返工止于波内。

#### Scenario: 无偏差报告不得勾批

- **WHEN** 波后无独立偏差报告
- **THEN** 该批 SHALL NOT 勾选收口

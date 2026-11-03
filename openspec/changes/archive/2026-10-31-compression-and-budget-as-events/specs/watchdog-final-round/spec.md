# watchdog-final-round

## ADDED Requirements

### Requirement: 预算耗尽是一条事实

预算耗尽的判定 SHALL 以 `final_round.request` 事实落链（range、原因 rounds|budget、判定输入），由生产方在提交后产出；loop SHALL 在决策边界读本 range 的 request 决定本轮为收尾轮。

#### Scenario: 读到 request 即收尾
- **WHEN** 本 range 的 `final_round.request` 已在链上且未收尾
- **THEN** 下一次决策 SHALL 是收尾轮，四条红线不变（零策略不设限、只在回合边界读、至多一轮、收尾轮不受理工具调用）

#### Scenario: 未接生产方时本地快判
- **WHEN** 决策边界读不到 request（生产方未挂载或判定尚未提交）
- **THEN** loop SHALL 按既有策略本地快判降级续行；事实的产出归生产方（PostCommit 提交），loop 不在消费路径上补写事实，以保持判定与执行的分离

#### Scenario: 一个 range 至多一条
- **WHEN** 同一 range 的判定被重复评估
- **THEN** 该 request 的身份不变，链上不出现第二条

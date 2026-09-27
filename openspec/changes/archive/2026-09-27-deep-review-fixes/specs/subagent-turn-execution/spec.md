# subagent-turn-execution Delta

## ADDED Requirements

### Requirement: 投递对账屏障的 booking 与投递期望一一配对

子调用环的投递对账屏障（delivery-accounting barrier）SHALL 保证每个 invocation 的 pending 计数与真实投递期望一一配对：包装 spawner 在内层 spawn 返回 inline settle、dedup 命中或被 gate 拒绝（Blocked）三种「本次调用不再拥有 settle 期望」的形态时，MUST 撤销（void）本次预登记的 booking；booking 的登记 MUST 保持先于内层 spawn（期望先于任务可能极快的 settle 而存在）。

#### Scenario: 同 key 任务在飞时重复发起（dedup single-flight）

- **WHEN** 子调用的某 turn 内对同 key 任务第二次发起调用，任务层 dedup 命中返回既有 active 任务
- **THEN** 本次调用的 booking 被撤销，屏障 pending 不因 dedup 而净增；既有任务 settle 时按其原发起调用的 booking 配对递减
- **THEN** 子调用环在该调用最后一个真实 settle 投递并排空总线后静默退出，不被无人消耗的 booking 钉住

#### Scenario: spawn gate 拒绝纳管（disk 退化）

- **WHEN** 子调用的某 turn 内 spawn 被 disk 退化闸拒绝（Blocked，工作已被取消跟踪）
- **THEN** 本次调用的 booking 被撤销，屏障不泄漏；调用环按正常静默条件退出

#### Scenario: inline settle 保持既有配对（回归对照）

- **WHEN** 内层 spawn 在同步等待窗口内结算（Settled=true）
- **THEN** booking 照旧被撤销（既有行为不变），屏障与环退出行为与本变更前逐字节一致

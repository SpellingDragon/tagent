# meditation-idle-gating Specification（delta）

## REMOVED Requirements

### Requirement: 空闲锚点血统无关

空闲锚点 `lastTurnEnd` SHALL 在**每个** turn 结束时无条件更新（含冥想触发的 turn、task_settled 回收 turn、失败/重试耗尽的 turn），不依据 trigger_source 过滤。冥想衍生活动对空闲锚点的影响 SHALL 仅表现为推迟下一次冥想，SHALL NOT 使其重新武装新颖性闸门。

#### Scenario: 冥想衍生任务 settle 不再武装冥想（永动机防护）

- **WHEN** 一次冥想 turn 派生的后台任务 settle 并完成其回收 turn，期间无任何新用户输入
- **THEN** `lastTurnEnd` 前移但 `lastUserInput` 不变
- **AND** 此后无论经过多少个 `MinGap`，冥想 SHALL NOT 再次触发，直至新用户输入到达

#### Scenario: 失败 turn 同样刷新空闲锚点

- **WHEN** 一个 turn 以 RunFlow 错误（含重试耗尽）结束
- **THEN** `lastTurnEnd` SHALL 更新为该 turn 结束时刻

## MODIFIED Requirements

### Requirement: 冥想触发采用双闸门判定

冥想触发 SHALL 采用双门判定：novelty 门（观察面内存在 `Timestamp > lastMeditation` 且非自管谱系的事件）与节奏门（`now - lastMeditation >= min_gap`，两次**执行**之间的下限；`lastMeditation` 为零时直通）。让路 SHALL 由注入时刻的批次合并规则承担（见「混合批次中丢弃冥想事件」），MUST NOT 以任意回合的时间门槛预先判定忙闲。

#### Scenario: 节奏门按执行间隔判定

- **WHEN** 上一次冥想执行距今不足 min_gap 且观察面有新事实
- **THEN** 本 tick 不触发；间隔满足后的下个 tick 可触发

#### Scenario: 家务回合不阻塞

- **WHEN** 观察面所属属主的 loop 持续运行自管谱系回合（结算摘要/回执注入等）且观察面有非自管新事实
- **THEN** 节奏门不受这些回合影响，到点即评

### Requirement: 混合批次中丢弃冥想事件

冥想事件与任何非冥想事件同批时 SHALL 被丢弃（让位语义不变），丢弃 SHALL 记为**推迟**而非放弃：水位不动、pending 清零，下个 interval tick 对同一事实面重新评估。被让位的反思所覆盖的事实 MUST NOT 因让位而老于水位（不烧窗口）。

#### Scenario: 让位后补位

- **WHEN** 注入的冥想事件与用户输入同批被让位，用户随后离开
- **THEN** 下个 tick 重新注入并执行，水位覆盖包含让位窗口期的全部事实

## ADDED Requirements

### Requirement: 水位执行语义与让位补位

冥想水位 SHALL 在冥想 turn 被消费时推进（注入时刻为水位值），MUST NOT 在注入时推进；混合批让位 SHALL 记为推迟（水位不动、pending 清零、下个 interval 重试）；pending SHALL 防重入并在超 3×interval 未决时 WARN 复位；lastMeditation 为零（首次/锚缺失）时节奏门 SHALL 直通。

#### Scenario: 让位不烧窗口

- **WHEN** 注入的冥想事件与真实事件同批被让位丢弃，随后观察面安静
- **THEN** 水位未推进，下个 tick 对同一事实面再次评估并可触发，覆盖完整

#### Scenario: 冷启动直通

- **WHEN** 属主从未执行过冥想且观察面有非自管新事实
- **THEN** 首个 tick 触发一次通读，水位自锁

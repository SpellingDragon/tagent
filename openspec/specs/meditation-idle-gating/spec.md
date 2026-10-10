# meditation-idle-gating Specification

## Purpose

定义冥想心跳的双闸门触发判据：以“真相便宜的位置”拆分空闲闸门（血统无关，任意 turn 结束算忙）与新颖性闸门（锚定输入侧 `source==user`），使冥想不依赖输出侧血统追踪即可免疫“冥想→派生任务→task_settled→再冥想”的自触发永动机。
## Requirements
### Requirement: 冥想触发采用双闸门判定

冥想触发 SHALL 采用双门判定：novelty 门（观察面内存在 `Timestamp > lastMeditation` 且非自管谱系的事件）与节奏门（`now - lastMeditation >= min_gap`，两次**执行**之间的下限；`lastMeditation` 为零时直通）。让路 SHALL 由注入时刻的批次合并规则承担（见「混合批次中丢弃冥想事件」），MUST NOT 以任意回合的时间门槛预先判定忙闲。

#### Scenario: 节奏门按执行间隔判定

- **WHEN** 上一次冥想执行距今不足 min_gap 且观察面有新事实
- **THEN** 本 tick 不触发；间隔满足后的下个 tick 可触发

#### Scenario: 家务回合不阻塞

- **WHEN** 观察面所属属主的 loop 持续运行自管谱系回合（结算摘要/回执注入等）且观察面有非自管新事实
- **THEN** 节奏门不受这些回合影响，到点即评

### Requirement: 门控不依赖输出侧血统追踪

冥想门控 SHALL NOT 依赖输出事件、任务层 `Origin` 行李或 task_settled 的血统标记；事件回调（`makeOnEventCallback`）SHALL NOT 更新冥想锚点。novelty 判据读取的 `trigger_source` 是**提交时盖章在事实链上的持久归因**（入库路径的既有部分），属"输入侧事实"而非输出侧追踪；除此之外门控不引入任何输出侧读取。

#### Scenario: 事件回调与锚点解耦

- **WHEN** 任意 trigger_source 的 final response 经过事件回调
- **THEN** 冥想锚点不因该回调而变化

#### Scenario: 唯一 novelty 读径是事实链

- **WHEN** 审查 novelty 判定路径
- **THEN** 其数据来源仅 QueryEvents/GetEvent 的持久归因，无输出事件或回调读取

### Requirement: 混合批次中丢弃冥想事件

冥想事件与任何非冥想事件同批时 SHALL 被丢弃；此外，**纯冥想批在被消费为 turn 之前，若同刻存在待处理的非冥想事件，SHALL 同样让位**（丢弃+推迟记账）——让位点覆盖注入与消费两个时刻，判据均为结构性事件在场，MUST NOT 引入时间阈值。两种让位均 SHALL 记为推迟而非放弃：水位不动、pending 清零，下个 interval tick 对同一事实面重新评估。外部策展形态（总线与业务线隔离）的复查 SHALL 恒无在场事件、行为零变化。被让位的反思所覆盖的事实 MUST NOT 因让位而老于水位（不烧窗口）。

#### Scenario: 消费时刻让位

- **WHEN** 纯冥想批已合并、RunFlow 启动前，总线上可见待处理的用户事件
- **THEN** 该批让位（deferred 记账、水位不动），与用户事件合并处理或下 tick 重投

#### Scenario: 让位后补位

- **WHEN** 注入的冥想事件与用户输入同批被让位，用户随后离开
- **THEN** 下个 tick 重新注入并执行，水位覆盖包含让位窗口期的全部事实

### Requirement: 水位执行语义与让位补位

冥想水位 SHALL 在冥想 turn 被消费时推进（注入时刻为水位值），MUST NOT 在注入时推进；混合批让位 SHALL 记为推迟（水位不动、pending 清零、下个 interval 重试）；pending SHALL 防重入，长期未决（超 3×interval）只 SHALL 出 WARN 观察线而 MUST NOT 自动复位重投（回合时长无上界，时间复位=重投踩踏；兜底取向 fail-safe 停摆）；lastMeditation 为零（首次/锚缺失）时节奏门 SHALL 直通。

#### Scenario: 让位不烧窗口

- **WHEN** 注入的冥想事件与真实事件同批被让位丢弃，随后观察面安静
- **THEN** 水位未推进，下个 tick 对同一事实面再次评估并可触发，覆盖完整

#### Scenario: 冷启动直通

- **WHEN** 属主从未执行过冥想且观察面有非自管新事实
- **THEN** 首个 tick 触发一次通读，水位自锁


# meditation-idle-gating Specification（delta）

## MODIFIED Requirements

### Requirement: 混合批次中丢弃冥想事件

冥想事件与任何非冥想事件同批时 SHALL 被丢弃；此外，**纯冥想批在被消费为 turn 之前，若同刻存在待处理的非冥想事件，SHALL 同样让位**（丢弃+推迟记账）——让位点覆盖注入与消费两个时刻，判据均为结构性事件在场，MUST NOT 引入时间阈值。两种让位均 SHALL 记为推迟而非放弃：水位不动、pending 清零，下个 interval tick 对同一事实面重新评估。外部策展形态（总线与业务线隔离）的复查 SHALL 恒无在场事件、行为零变化。被让位的反思所覆盖的事实 MUST NOT 因让位而老于水位（不烧窗口）。

#### Scenario: 消费时刻让位

- **WHEN** 纯冥想批已合并、RunFlow 启动前，总线上可见待处理的用户事件
- **THEN** 该批让位（deferred 记账、水位不动），与用户事件合并处理或下 tick 重投

#### Scenario: 让位后补位

- **WHEN** 注入的冥想事件与用户输入同批被让位，用户随后离开
- **THEN** 下个 tick 重新注入并执行，水位覆盖包含让位窗口期的全部事实

# meditation-agent-partition delta（07-evolution）

> 本 delta 只承载**新增条款**；其余既有 Requirement（观察面授权解析、novelty 判据、门控与锚点两锚结构、产出落自身分区、反思注入本循环 session）语义不变，随新内核重建实现，**继承**（见 `../proposal.md` §继承）。

## ADDED Requirements

### Requirement: 反思作为显式输入生产者与水位新鲜度

反思产出的触发动作 SHALL 以**显式输入事件**（`source=meditation`）注入本 agent 的循环 session，作为该 agent 的显式输入生产者，MUST NOT 以输出侧派生或隐式后台调用代替；消费者（digest/评估/宿主诊断）SHALL 能就同一事实面观察到该输入事实。

水位新鲜度 SHALL 满足：注入时刻 MUST NOT 推进水位，水位 SHALL 在反思 turn 被**消费**时推进（值取注入时刻）；让位（deferred）SHALL 记水位不动、pending 清零，被让位反思覆盖的事实 MUST NOT 因让位而老于水位（不烧窗口）；水位缺失/不可解析 SHALL fall-closed——门保持关闭并具名 debug，MUST NOT 回落任何备用判据或以"新鲜"默认放行。

#### Scenario: 反思输入可被消费者观察

- **WHEN** 一次反思有效触发
- **THEN** 观察面消费者（digest/评估/宿主诊断）可就该 agent 观察到一条 `source=meditation` 的显式输入事实；该事实不是由输出事件或任务层行李反推而来

#### Scenario: 让位不烧窗口（水位新鲜度）

- **WHEN** 注入的反思事件与真实事件同批被让位丢弃，随后观察面安静
- **THEN** 水位未推进、pending 清零；下个 interval tick 对**同一事实面**（含让位窗口期全部事实）重新评估并可触发，被让位反思覆盖的事实不低于水位

#### Scenario: 水位不可解析 fail-closed

- **WHEN** 锚点水位不可建立（来源未接/记录不可读/不可解析）
- **THEN** 本轮门保持关闭并输出具名 debug；不按"新鲜"默认触发、不回落备用判据
- **场景：首次无锚点记录的引导态**
  - WHEN 启动时锚点记录文件不存在（无历史执行） THEN 零水位引导放行一次，消费推进后按水位锁定，重复结算不得再次触发


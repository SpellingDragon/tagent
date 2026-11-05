# delivery-closure Specification（delta）

## ADDED Requirements

### Requirement: 交付义务随答案同笔成立

交付义务 MUST 以 owed 声明嵌入 agent_output 的提交载荷、随其同一事件原子落链：不存在"答案已落链而义务未立"的链形态。结算四态（held/sent/refused/headless-retired）＋迁移专用 owed 态（仅历史输出迁移，非常规结算终态；retry-exhausted 只是 held 的 reason 之一，非独立终态）MUST 为独立事实族**追加式**落链：**事实键=交付身份+终态**（键含状态——事实键不可改，状态转移即新事实）；幂等指**同（身份,状态）键**重放不添第二条、同键异内容按既有同键冲突拒收；按交付身份前缀可回读全部状态（恒可回读为两段式：当前由 Lifecycle 未挂载保证；挂载后由负 TTL 豁免保证；折叠豁免同前）。

#### Scenario: 提交失败两不立

- **WHEN** 义务声明随答案提交被拒（如存储故障）
- **THEN** 答案与义务都不落链（同一笔），出站通道收到失败提示而非正文

### Requirement: 交付状态变化的可见性口径

交付**结算事实**（四态：held/sent/refused/headless-retired；迁移专用 owed 态仅作历史迁移事实、非常规呈现态；held 的 reason ∈ {approval, retrying, retry-exhausted, unknown}，retrying/retry-exhausted 均非独立终态）MUST 进入模型消费与投影呈现（`delivery.settlement` 类型 MUST 注册类型→角色映射，未注册类型会被投影拒收；角色映射仅限结算类型）；**owed 声明随 agent_output 本体呈现，无独立可见面**；**管道记账噪音**（重试计时、中间探测、迁移记账）MUST NOT 进入模型投影可见面。

#### Scenario: 异常必被模型感知

- **WHEN** 重试耗尽或结果未知落 held 结算
- **THEN** 该结算事件出现在模型随后的请求可见面，模型可自主决定表达

#### Scenario: 记账噪音隔离

- **WHEN** 重试计时与内部探测发生
- **THEN** 投影可见面不出现这些噪音

### Requirement: held 的事实携带原因

held 结算事实 MUST 携带原因，且 reason MUST 属于 {approval=等审批 / retrying=重试在途（首次重试前写入，成功后由 sent 覆盖对账）/ retry-exhausted=重试耗尽 / unknown=结果未知}；模型可据此区分“等审批”“重试中”与“已放弃”。retrying 与 retry-exhausted 均只是 held 的 reason（由 DeliveryConsumer 写入），MUST NOT 被当作独立结算终态。

#### Scenario: 区分等待与放弃

- **WHEN** 模型读到三条 held 结算
- **THEN** 一条 reason=approval（等人工放行）、一条 reason=retrying（重试在途）、一条 reason=retry-exhausted（已放弃自动重试），三者可分辨

### Requirement: 错误分类驱动的重试

发送失败的分类 MUST 互斥：渠道确定性拒绝（连接被拒、鉴证失败、渠道明确否定完成，且非超时形状）为**明确失败**，MUST 有界退避自动重试（退避序列与次数上限为实现常量，见 design）；**首次重试前 MUST 写 held(reason=retrying) 结算事实**（成功后由 sent 覆盖对账），重试耗尽落 held(reason=retry-exhausted)；超时、中断、错误不可辨为**未知**，MUST NOT 自动重发（对齐 delivery-dispatch-receipts）。每次重试 MUST 关联原发送动作身份；redrive MUST NOT 对已确认 sent 的动作发生。

#### Scenario: 瞬时明确失败

- **WHEN** Send 返回拒连形状的明确失败
- **THEN** 退避后自动重发成功，全程无人工介入

#### Scenario: 纯超时归未知

- **WHEN** Send 返回超时形状错误
- **THEN** 不自动重发，落 held 等待正式入口或人工处置

### Requirement: 兜底告知两支：失败告知与延迟告知（r8 F1 定版；r9 F3 收口）

兜底告知 MUST 分为互斥两支，并按下列对照表区分；**支②是延迟告知，不是失败告知**——`failure-notice` 只管辖支①的失败告知话术；支②仅以 failure-notice「成功但无正文保持静默」场景的豁免句衔接（该场景 MODIFIED 增豁免：未清 held 义务存在时按延迟告知恰发一条，非失败话术；不新增通道、不改 failed 触发条件）：

| 支 | 名称 | 触发前提 | 话术与条款归属 |
|---|---|---|---|
| ① | **失败告知** | failed 终态 + 无正文 + 有原因（义务是否未清不影响） | 既有 failure-notice 照发；01 不新增第二条告知通道，只保证该通知必然发生 |
| ② | **延迟告知** | **存在未清 held 交付义务 × 该义务的驱动轮（=当前观察轮，产生该通知判定的那一轮）结束无面向用户产出** | 01 新增恰一条最小延迟告知，话术按 held.reason 分层：`retrying`→稍后必补送；`approval`/`unknown`/`retry-exhausted`→已转人工处置。MUST NOT 与模型产出重复；不属 failure-notice 的失败话术，仅由其静默场景豁免句放行 |

支② MUST NOT 以“成功终态”为前置：body 存在⇒owed 随 agent_output 在链上，send 失败落 held，该前提不可达；延迟告知在模型感知 held 之后、该驱动轮（=当前观察轮，产生该通知判定的那一轮）无面向用户产出时发出，**且不限于 answer 轮终态**（任何驱动该义务的轮结束且无面向用户产出均触发）。支②话术 MUST 按 held.reason 分层：`retrying`→稍后必补送；`approval`/`unknown`/`retry-exhausted`→已转人工处置——MUST NOT 对结果未知或等审批的义务承诺自动补送。成功但无正文且无未清义务、cancelled、宿主错误等场景的静默边界以 failure-notice 条款为准（无未清 held 义务才静默）；**悬空无终态 MUST NOT 发框架通知**（1.4b 实测：input 事实存在、无 agent_output；若暴露无人知晓的悬空轮=法定停点上报，不得 skip 结项）；模型已有面向用户产出时框架 MUST NOT 代答。

#### Scenario: 失败终态照发既有通知

- **WHEN** 一轮以 failed 终态结束、无正文且带原因（义务是否未清不影响）
- **THEN** 既有 failure-notice 恰发一条最小告知（由支①失败告知承担）

#### Scenario: held 义务未清且本轮无面向用户产出（延迟告知）

- **WHEN** 存在未清 held 交付义务，且该义务的驱动轮（=当前观察轮，产生该通知判定的那一轮）结束无面向用户产出
- **THEN** 框架恰发一条最小延迟告知（话术按 held.reason 分层：retrying→稍后必补送；approval/unknown/retry-exhausted→已转人工处置；支②），不按失败告知措辞，且不与模型产出重复

#### Scenario: 成功但无正文静默

- **WHEN** 回合成功完成但没有正文产出且无未清义务
- **THEN** 即使存在记账噪音，兜底告知不发声；存在未清 held 义务时改由上一场景的延迟告知恰发一条（非失败话术，failure-notice MODIFIED 豁免句）

### Requirement: 恢复只读链与旧 KV 六态迁移

重启恢复的补送范围 MUST 为**无结算事实 ∪ held(reason=retrying)** 的义务：中断在退避途中的义务 MUST 在重启后续跑重试（不因重启搁浅），已确认 sent 的义务 MUST NOT 重发（**at-least-once**：发过未销账窗口内可能重发一次，与 durable-delivery 同义）。恢复枚举 MUST 在事实窗口内读取已提交事实（结算事实按交付身份前缀过滤返回集；会话枚举可经既有注册序列），MUST NOT 以全链 O(N) 枚举代替；未清交付事实（held/owed 活动集）MUST 驻留事实窗口内，MUST NOT 落到事实窗口外。既有旁路 KV 交付记录（旧 KV 六态：owed/held/retry/sent/refused/**headless**）MUST 在启动时幂等迁移为对应事实（headless→retired，不重发、正文留档）后转只读留存；不可解析的记录 MUST 具名拒启动；迁移后恢复路径 MUST NOT 再扫描旧 KV 前缀。

#### Scenario: 多会话恢复有界

- **WHEN** 恢复时存在多个已注册会话链并注入与交付无关的 agent_output 事实
- **THEN** 恢复**返回的匹配事实条目数**有界（计数断言）：注入的无关事实不抬高匹配数，全部未清义务都在结果中；匹配只在事实窗口内读取，MUST NOT 以全链扫描替代

#### Scenario: 中断重试恢复后续跑

- **WHEN** 进程在 held(reason=retrying) 已落链、退避尚未完成时重启
- **THEN** 该义务 MUST 被纳入补送范围并继续有界退避（覆盖无结算事实与 retrying 两种状态），不因重启搁浅

#### Scenario: 迁移幂等

- **WHEN** 含旧 KV 记录的存储连续重启两次
- **THEN** 义务事实恰建一次，任何 body 不双发

#### Scenario: headless 不重发

- **WHEN** 迁移遇到 headless 记录
- **THEN** 记为 retired 结算，正文留档，不发生发送

#### Scenario: 旧记录损坏

- **WHEN** KV 记录无法解析
- **THEN** 启动被具名拒绝，不猜测处置

# Design: polish-meditation-runtime

基线 main `8744725`（dev `59fb57b` 同树+go.mod TODO 注释）。

## D1 执行时刻让位的判据形态（含被否方案）
**采纳**：event_loop 在批合并产出"纯冥想批"后、RunFlow 启动前，做一次**在场复查**——同刻总线上还有未拉取的非冥想事件（`bus.Pending()` 或等价的非破坏性窥视，实施期按 event_bus 现有 API 选型并申报）→ 在场则：本批事件放回/丢弃（按注入事件幂等性决定，见 D2）、通知 `NoteMeditationBatchOutcome(false)`。
- 判据是"看得见待处理真实事件"——结构性、无时间参数；
- 被否 A：时间窗（"用户 turn 结束后 N 秒内不让"）——回到时间门槛预猜，违背五不变量哲学；
- 被否 B：消费后抢占——回合原子性不可行；
- 被否 C：彻底禁止自察形态执行期让位（只靠注入时刻）——即现状，残余缺口不闭合。

## D2 让位后的事件处置
复查让位时，已注入的冥想事件**丢弃**（与注入时刻让位同构：deferred 记账、水位不动、下 tick 重投）——不尝试"放回队列"（放回引入重排序复杂度且与 mixed-batch drop 语义分叉）。幂等性：重投生成新事件，旧事件已丢弃无重复消费。

## D3 外部形态零影响断言
外部策展线的总线与业务线隔离（不同 agent 不同 loop），复查对其恒"无在场真实事件"→ 恒不让位 → 行为零变化。必须有专门断言（复用 naturalize 的外部 e2e 基架加"用户事件密集期策展线照常执行"用例），防复查误伤。

## D4 digest 欠账计数
`deferredCount atomic.Int64`（deferred++、consumed 清零）+ `lastDeferredAt`；digest 在"观察面概览"之后渲染一行。样式对齐现有 digest 行（中文冒号句式）。不引入新持久化（进程内计数，重启归零——欠账是会话语义非账本语义）。

## D5 测试面
- 单测：在场复查让位（bus 预置待拉事件→纯冥想批 deferred）/ 无在场事件→照常 consumed / 外部形态恒 executed；
- digest：deferred 后 buildMeditationMessage 含计数行、consumed 后归零；
- e2e：自察"用户消息间隙的纯冥想批让位→用户离开后下 tick 执行且水位覆盖"；
- 回归：naturalize 全部用例零改动通过（D4 七场景不回归）。

## D6 风险
- 复查的窥视 API 若 event_bus 现无（只有破坏性 Pull），需加非破坏性 PendingLen/Peek——**新增总线 API 属共享面**，实施期如需，契约冻结后单写（R22）；
- 复查让位可能造成"真实事件永远领先一步"的活锁（极端高频输入下冥想持续让位）——恰是期望行为（用户在场优先），且 min_gap 节奏与欠账计数使其可见；无饥饿死锁（事件流终有间隙）。

# unified-intake：单门事件流消费——决策边界就绪增量 + 冥想活跃闸

## 症状（验收矩阵第一行——用户原话直译）

| 用户原话 | 直译测试（规划期即红着） |
|---|---|
| 「不插入进行中的 turn 不符合事件流消费预期」 | `TestMessageReachesTheNextDecisionOfAInFlightTurn`：脚本化慢工具轮 turn 进行中，同一 session 发第二条用户消息 → 该消息出现在**同一 turn 后续 decision 的输入**（现状红：排队到 turn 结束、以新 turn 消费） |
| 「冥想要看的是 session 活跃」 | `TestMeditationYieldsBesideAnActiveSession`：同一 session 注入用户事件后（min_gap 窗口内）→ 冥想不 fire；`turn 在途`（决策席被占）→ 冥想不 fire（现状红：节奏门只看执行间隔） |

## 考古结论（两次修正后的定性）

1. **域 A 是实现偏离规格**，不是新语义：`openspec/specs/persistent-event-loop` 已有「decision 进行中 `task_settled` SHALL 排队至**下一轮**被消费，SHALL NOT 打断进行中的 decision」「排队 SHALL 保留通知及其未结义务」（:94/:105/:123）。而实现是 **turn 级串行**（`addressGate` 让整个 turn 结束才驱动；`observeReady` 只在 Drive 开头跑一次，尽管 `Invocation.Ready` 注释自称「aggregated for the next decision」）。用户 2026-10-25 案 00.3 冻结的「忙时保留」与规格一致——**用户推翻的是实现现状，规格条款保留；规格缺口=外部输入（非 task_settled）的在途消费语义未单列，本次补 ADDED**。
2. **域 B 是规格精准小修**：`meditation-idle-gating` 现明文「MUST NOT 以任意回合的**时间门槛**预先判定忙闲」（反「时间门槛」，因回合时长无上界），并区分「自管家务回合不阻塞」。用户「看 session 活跃」= 以**结构性判据**（`source==user` 事件新鲜度，与 novelty 门同源读径）加闸——不引入时间门槛、不碰反永动机条款（自管回合不重置冷却，仅用户事件重置 + 决策席占用时不 fire）。

## What Changes

- **(A) 单门事件流**：用户/外部输入到达即落链成事实；turn 在途时消息**不排队等 turn**，由在途 turn 的**下一 decision 边界**重新聚合链上就绪事件消费（ready 增量读面 + host Invoke 受理语义 + gate 降级为 turn 发起权门）。
- **(B) 冥想活跃闸**：节奏门在 `min_gap` 基础上加两侧判据——①同 session 最近用户事件新鲜度（`source==user` 且晚于 last fire）→ 冷却自最后用户事件重置；②该 session 决策席在途 → 不 fire。自管谱系回合（结算摘要/回执注入）不重置冷却（沿用「家务回合不阻塞」）。
- **(C) 规格 delta**：`persistent-event-loop` ADDED「在途 turn 的外部输入在 decision 边界消费」；`meditation-idle-gating` MODIFIED 节奏门条款加活跃闸（保留时间门槛禁令的其余部分）。

## 被否方案（D6 详）

- 立即打断（中止在途工具/模型流）：决策因果重放被破坏，改动面数倍，用户已裁定取「决策边界消费」。
- 单门改造与冥想闸分案：同主题（事件消费时序）一案承载，合并规则归编排。

## 必答一问

「本变更全部完成、全绿归档，'进行中的 turn 吃不到新消息''活跃时冥想照跳'还会发生吗？」——不会：两条症状直译测试规划期红着，修复后绿，回滚即红（F3 双证据入 run notes）。

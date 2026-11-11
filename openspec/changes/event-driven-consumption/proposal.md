# Proposal: event-driven-consumption（superseded 记录，不归档）

## Why

本变更是 dev 收敛（squash `170ef267`，2026-11-04"one fact chain and one consumption loop"）随库落盘的 spec-delta 记录，无独立实施历史。03/04 域已转移至 `hot-update-and-purity-closure`（`changes/10-turn-segment`、`changes/11-derived-faces`）；余下 01/02 的语义已由该收敛直接兑现在 dev 并吸收进现行主规格。

## superseded-by 注记（2026-11-08 归档清理裁定）

**不执行 `openspec archive`**，原因有二：

1. **01 delta 的目标能力已是墓碑**：`openspec/specs/persistent-event-loop/spec.md` 经 runtime-architecture-convergence 4.2 迁移后明示"只作 superseded 记录，不再承载任何现行规范命令"。archive 会把本 delta 的现行 SHALL/MUST 条款写回墓碑，与 `consumer-loop-convergence` 等现行能力形成第二权威；概念门 D1（`scripts/gates/check_concept_budget.py --rule D1`）对此机械判红（superseded spec 含现行 SHALL/MUST 即 exit 1；本裁定时点实测 exit 0）。
2. **02 delta 与现行条款语义重复**：`config-state-twinship` 已含"同一 session 最多一个有效模型决策""慢模型不阻塞接纳"，`consumer-loop-convergence` 已含"输入入口只提交事件""每地址恰一实例"，归档即重复立法。

## 吸收映射（delta 条款 → 现行主规格/代码）

| delta 条款 | 现行承接处 |
|---|---|
| 01 一次消费只看已到达的事件（跨来源合批、只消费一次） | `consumer-loop-convergence`「每会话一个常驻消费循环」（拉取已到达事件、同会话不同来源合批、快结果先到先消费）＋「重入输入恰一次」；代码 `runtime/agent/consumer.go` `pullDriving`（读集按事实身份去重） |
| 01 工具结果不阻塞下一次消费 | `consumer-loop-convergence`「工具派发后不等结果」「慢工具不阻塞新输入」 |
| 01 一次请求只看冻结批次 | `consumer-loop-convergence`「A/B 闭合」（后到输入属下一段，MUST NOT 误标） |
| 01 消费关系随产出记录 | `task-skeleton-compression`「段边界按 `input_events` 消费关系归段」 |
| 02 提交即受理 | `consumer-loop-convergence`「输入入口只提交事件」＋「gate 与 Occupancy 收敛」（并发两请求同 session 各自提交） |
| 02 同会话单消费者 | `config-state-twinship`「session mailbox 记账与执行占用」＋`consumer-loop-convergence`「每 agent 地址一个常驻消费实例」 |
| 02 不移写来源身份 | `event-metadata-contract`「来源保留」（`trigger_source` MUST NOT 因混合输入被改写；各自来源可从持久事实查询） |
| 02 同步返回语义不变（KindUnknown=已受理非失败） | 代码 `runtime/agent/consumer.go:226`（受理即返 `Turn{Terminal: KindUnknown}`）、`assembly/host_face.go:249`（读作在途非失败）；语义分层由 `unified-task-semantics`「受理≠结果」承接 |

**未逐字承接项（如实登记）**：01「显式隔离的事件单独成批」scenario 在现行主规格无对应条款（现行最近形态为 `driveInvocation` 显式调用独立段，`runtime/agent/consumer.go:215`）；不据此归档，如需事件级隔离声明语义应另立变更。

## 后续

03/04 的未实现面由 `hot-update-and-purity-closure/changes/10-turn-segment`、`11-derived-faces` 继续承载。本文件即本变更的收口记录；delta 原文保留作考古，不再同步主规格。
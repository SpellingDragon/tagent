# Proposal: attention-lifecycle

## Why

tagent 今天有三条平行路径做同一件事——"向人要一个答案"——却各有一套存储与
路由，且没有写死的清除规则：

- **审批**（`runtime/task/approval`）：文件目录 + 内存索引为真源，digest 绑定，
  TTL 30 分钟；裁决落定后请求**不清除**，文件与索引残留到过期。
- **通知**（`task.Notice` + `ObjectNotice` 投影）：事实与 state 邮箱，四相
  owed→accepted→claimed→consumed；**无 expiry**——无人消费的通知永远挂着。
- **反思投递**（`meditation` 的 `deliver_summary`）：external_input 事实投出
  即忘，无相位、无答案、无清除。

反事实已经发生：每新增一类"需要人"（调用审批、熔断整批审批、通知、反思投递），
就多一条存储与一条路由（`HoldBook`、`ApprovalBook` seam、approvalGateway 各自
为政）。不统一，下一类"需要人"还会再长一条。

## What Changes

引入 **Attention** 原语：一次"向目标线程/人要一个答案"的完整生命周期作为事实
与投影——

- **question** 事实：问题正文 + 目标（线程/人）+ 上下文血统（调用/任务/反思
  来源）+ 有效期；
- **answer / expiry / clearance** 事实：裁决、过期、清除各是一条事实；
- **清除规则写死**：回答后清除（answer 落链即清），或工作结束时清除
  （宿主 Close 排空时未决项全部落 clearance）——不依赖内存态，重启后投影一致；
- **审批变为 attention 的一种**：governance 门、熔断 ApprovalBook seam、
  wechat/CLI 人侧裁决全部读写 attention；
- **blocked task 与反思决策复用同一原语**：notice 的"人的那一半"挂上 expiry 与
  工作结束清除；`deliver_summary` 作为第三种 kind 接入投影与清除，投递路径不动；
- 投递走现有 ledger（事实链），人侧统一 Pending 面读同一投影。

## Capabilities

- `attention-primitive`（01 域）：事实族、meta 键、落链与读回、清除规则引擎。
- `approval-attention`（02 域）：审批迁移为 attention 消费者（gate/fuse/seam）。
- `attention-clearance`（03 域）：投影 `ObjectAttention`、notice expiry 与工作
  结束清除、deliver_summary kind、人侧统一入口。

规格已下沉到二级子变更（本目录 `specs/` 只留编排契约），二级嵌套不被 CLI 跟踪
是有意设计：进度靠本变更 `tasks.md` 的 checkbox 人工更新。

## 症状直译锚（立案已确认，第一天红着）

1. `TestAttentionClearsApprovalOnAnswer`：一次审批被批准并执行后，在其清除点
   读取 attention 面，该问题**不再在场**（现状：approved 文件与索引残留 → 红）。
2. `TestAttentionClearsNoticeOnWorkEnd`：一个结算后无人消费的通知，在宿主工作
   结束（Close 排空）后读取投影，**不再挂着**（现状：无 expiry，永远 owed → 红）。

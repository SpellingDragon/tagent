# attention-clearance Specification

## Purpose

本规格定义 Attention 的投影与清除契约：在场读面统一（ObjectAttention）、两类
清除写死（回答后、工作结束后）、notice 与反思投递按此生命周期接入，人侧入口
收敛到同一 Pending/answer 面。

## Requirements

### Requirement: 在场读面统一为 ObjectAttention

`memory/query` SHALL 提供 `ObjectAttention` 维度：每条在场问题带 kind（approval|
notice|summary）、目标、lineage 与相位（question→answered|expired|cleared），
并 SHALL 进入既有 snapshot 与 Follow 增量面；人侧 Pending 显示（wechat/CLI）
SHALL 读该维度，MUST NOT 各自维护第二份待答清单。

#### Scenario: 三类同面

- **WHEN** 一个审批问题、一条未消费通知与一条已投递摘要同时在场
- **THEN** `ObjectAttention` SHALL 以三种 kind 各报一条，人侧 Pending 面读到的
  与投影一致

#### Scenario: 增量随行

- **WHEN** 一条 question 收到 answer
- **THEN** Follow 增量 SHALL 报该对象的相位变化，快照 SHALL 不再列其在场

### Requirement: 回答后清除写死

approved/denied 的 answer 落链 SHALL 即刻移除该问题的在场读数；审批文件面残留
SHALL 同步撤下；清除 SHALL 只作用于在场读数，MUST NOT 删除任何已落事实。

#### Scenario: 症状锚——审批回答后不再在场

- **WHEN** 一个审批被批准并执行（症状锚 `TestAttentionClearsApprovalOnAnswer`）
- **THEN** 审批目录与 `ObjectAttention` SHALL 均不再报告该请求

### Requirement: 工作结束清除写死

宿主 `Close` 排空成功后，attention 读面 SHALL 整体退出在场（全有或全无：
排空未完成则一条不清）；排空失败或未排空 MUST NOT 清除。三类在场均为读时
推导（approval 类自 question/answer 事实，notice 与 summary 自通知状态与投递
事实），MUST NOT 为推导在场补造 question/clearance 假对；清除判定：过期读侧
判、工作结束由排空完成态门控。一次工作结束后的清空是**该次读数**：重启是新
工作，仍无 answer 的 question 恢复在场——答案未到，问题就还在。

#### Scenario: 症状锚——无人消费的通知在工作结束不再挂着

- **WHEN** 一条结算通知无人消费直至宿主 Close 排空完成（症状锚
  `TestAttentionClearsNoticeOnWorkEnd`）
- **THEN** 该次读数 SHALL 不再报告该通知在场（工作结束截面的症状由合并线的
  排空消费与读面门控共同清偿）

#### Scenario: 排空失败不清

- **WHEN** Close 排空报告未完成
- **THEN** 未决 attention SHALL 保持在场，MUST NOT 落 work-end 清除

### Requirement: notice 挂有效期且协议不动

未消费的通知 SHALL 在读面携带 `expires_ms`（读侧判过期，过期即不在场）；
notice 四元组、settle 驱动与段语义 MUST NOT 改动；已被段消费的通知相位沿用
既有 consumed 语义，不重复成题。

#### Scenario: 过期通知不在场

- **WHEN** 一条通知超过其有效期且无人消费
- **THEN** `ObjectAttention` SHALL 不再报告其在场，`ObjectNotice` 的既有相位
  读数不受影响

### Requirement: 反思投递按 summary kind 接入

`deliver_summary` 投递 SHALL 产生一条 kind=summary 的在场记录（投递路径
`CommitToSession` 不动，在场为读时推导）；目标会话产出下一轮输出 SHALL 即视为
该摘要已被读到并清除在场（design 回退条款的宽清除点）；未被读到的摘要 SHALL
在读面随工作结束退出在场。

#### Scenario: 读到即清

- **WHEN** 一条摘要投递后目标会话产出下一轮输出
- **THEN** 该 summary 的在场记录 SHALL 被清除，且链上投递事实俱在

#### Scenario: 未读到则随工作结束清

- **WHEN** 一条摘要投递后目标会话再无请求直至 Close 排空
- **THEN** 该 summary SHALL 经 work-end 清除退出在场

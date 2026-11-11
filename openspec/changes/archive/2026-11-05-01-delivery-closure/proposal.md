# 交付闭合（delivery-closure）——机制事件化路线

父变更：boundary-closure（W1；依赖 W0 锚①②③④红着基线，已落并亲验全红）。

## Why

评审与本地核验确认三个缺口（一级 design D1 E1-E3）：登记失败仍发送（host_delivery.go:76-81）、输出落盘与义务登记间的漏送窗口（恢复只扫旁路 KV 记录）、retry 状态无自动重试驱动。根因是**交付义务活在旁路账本（tagent/task/v1/delivery: KV 记录）而不在事实链上**——接缝因此而生。

**用户裁决（2026-11-05，r1 终裁第三轮）**：采用**机制事件化**——交付义务成为链上事实，与 agent_output 同笔提交（构造性原子，窗口归零），发送/重试/审批持有由消费挂点驱动。这最符合系统"一条链、三个挂点"的扩展思想（压缩请求/产物已是先例），代价是本域从中型升大型、需迁移旧 KV 记录并同步概念文档。

## What Changes

1. **混合形态（r3 定版）**：新输出的 owed=agent_output.Meta **声明**（同笔原子）；**结算四态（held[带 reason]/sent/refused/headless-retired）＋迁移专用 owed 态**（仅历史输出迁移，非常规结算终态；retry-exhausted 只是 held 的 reason，非独立终态）=独立事实族 `delivery.settlement`（键=交付身份+终态，追加式，按结算族构造性豁免 TTL 墓碑化，无前瞻注册、无未挂载债务，一行可读正文）。
2. **DeliveryConsumer（消费挂点）**：读链驱动发送；明确失败（拒连非超时）自动重试，纯超时归未知不自动重发；**首次重试前即写 held(reason=retrying) 结算事实**（成功后 sent 覆盖对账；retrying 只是 held 的 reason，不是独立终态）；重试耗尽落 held(reason=retry-exhausted)。
3. **可见性口径**：结算事实进模型消费与投影呈现（类型→角色映射+非空 Content；映射仅限结算类型）；**owed 随 agent_output 本体呈现、无独立可见面**；记账噪音不进投影。
4. **旁路 KV 欠条退役**：旧 KV 六态幂等迁移为事实（headless→retired 不重发）；恢复只读链——**结算事实与 owed 声明均经 `r.facts` 事实窗口内读取**（`r.facts` 本就是事实窗口内事实表；`r.replays` 的映射序列仅用于会话枚举），代价 **O(事实窗口内)**；**结算事实以 `memory/query_events.go` 的 `QueryOptions` 未导出键前缀（`keyPrefix`+`Prefix()`+`WithKeyPrefix()`）做返回集过滤**（过滤不改变扫描量）；查询 PartitionIDs 由 replays 注册键派生（不盲扫分区）；**补送仅针对无结算事实 ∪ held(reason=retrying) 的义务（中断重试恢复后续跑；at-least-once：发过未销账窗口内可能重发一次；已确认 sent 绝不重发）**；未清交付事实不落事实窗口外（held/owed 活动集驻留，03 的 1.2a 保证）。
5. **兜底两支（r8 F1 定版；r9 F3/F4 收口）**：①**失败告知**——failed 终态→既有 failure-notice 照发（failed+无正文+有原因，**无义务条件**）；②**延迟告知**——**存在未清 held 交付义务 × 该义务的驱动轮结束无面向用户产出**→01 新增恰一条最小延迟告知，话术按 held.reason 分层（retrying→稍后必补送；approval/unknown/retry-exhausted→已转人工处置；**驱动轮=当前观察轮，即产生该通知判定的那一轮**；不与模型产出重复），**不新增 failure-notice 通道**，仅在其「成功但无正文保持静默」场景增豁免句（该场景 MODIFIED：有未清 held 义务时按延迟告知恰发一条，非失败话术）；**悬空无终态不发框架通知**。
6. **无实验开关（r22 纯化）**：事件化是唯一交付路径，旧 KV 仅保留迁移读；DeliveryConsumer 不以 ObserveCommit 自监听（退避是状态机自持计时，结算事实是只读结果面）。
7. **概念文档同步**：机制导览/执行核架构/存储概念图/GLOSSARY/FACT-VOCABULARY（第十一族/表计第 10 族 delta 见 specs/fact-vocabulary/；ADR 0002 注记同步）。
8. **delta 面（本域归档含四个 delta）**：`specs/delivery-closure/`（ADDED）、`specs/fact-vocabulary/`（ADDED 第十一族）、`specs/delivery-dispatch-receipts/`（MODIFIED「发送动作身份与未确认结果」：全文保留既有条款+活进程内限定词+重启补送追加句）、`specs/failure-notice/`（MODIFIED「成功与静默边界不得被失败话术污染」：全文保留+「成功但无正文保持静默」场景的延迟告知豁免句）。

## 边界与依赖

- **依赖**：无实现依赖（W0 锚已红）。
- **被依赖方**：07 的送达收据改为读链上结算事实（比 KV 更直接）；06 送达率指标同义口径（发送方渠道确认，见 06 design）。
- **接口面**：不改 api/**；failure-notice 条款经 MODIFIED 增豁免句（不新增通道、不改 failed 触发条件）；`ApproveDelivery`/`RefuseDelivery` 公共入口语义保留（落点从 KV 记录改为事实；实现位于 assembly/host_delivery.go:194 起，restoreHeldDelivery 于 :427 起）；`tagent/task/v1/delivery:` 前缀记录退役为只读审计。
- **禁止事项**：不动 assembly/request.go（02 域）、tool/exec、memory/ledger 的存储内核（新事实族走既有登记表，不改 ledger 实现）；owed 声明无独立可见面（随 agent_output 本体呈现）；结算四态必进投影可见面；记账噪音（中间探测/重试计时）不进投影（呈现挡在投影规则，与压缩事实同等待遇）。

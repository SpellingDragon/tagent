# 骨架评估报告（docs/.dev/REPORT.md）响应与计划调整记录

> 输入：用户提供的独立评估报告（审查版本 `56c432c`，26/89 时点）。本文件=主编排者逐条甄别＋裁决＋计划调整落点；报告原文不动。
> 甄别时点：2026-10-14，当前 40/89（波 3 完成）。**先分清"已被波 2/3 解决"与"仍成立"，不重复修已修项**。

## 一、逐条甄别

| 报告发现 | 当前状态 | 依据 | 处置 |
|---|---|---|---|
| §四.1 唯一执行循环与终局职责不一致 | **终局部分已解决**：02.4 已落 ReduceChannel/ReduceSeq 请求级归约＋Outcome＋六态取证＋nil 流/cancel/ProducerDone 分别取证（model/terminal.go:335/366）。唯一循环载体（agent 驱动）＝02.5 未实施 | grep 实证 | 02.5 派发令中明确"归约唯一入口是 model 包，agent 不另写判断"；persistent-event-loop delta 的"框架 ReAct 路径"措辞矛盾→**裁决修正**（见 §三.1） |
| §四.2 派生代借用（委派继承发起代） | **仍成立**：Borrow 只借当前代；BindingRef/BorrowDerived/排队保有协议均缺 | state/binding.go 零命中 | **R1.2 修订单** |
| §四.3 审批 ApprovalID/digest 绑定 | **仍成立**：ApprovalDecision 只靠 CallID（非全局唯一） | host_bridge.go 零命中 | **R1.3 修订单** |
| §四.3b 工具参数变换时点（Enricher 是请求级，不能承担工具前变换） | **仍成立** | plugin/phases.go 无 TransformCall | **R1.5 修订单** |
| §四.4 提交自包含与 Notice 目标冻结 | **部分解决**：02.2 Handoff 有 TargetInstance 字段；但 CommitRecord.Outbox 无 per-notice 冻结目标 envelope——"失败→Handoff→重启→按当前目标错改投"反例仍成立 | task/commit.go 零命中 | **R1.4 修订单**（NoticeEnvelope） |
| §四.5 原文多引用读取/holder 协议 | **部分解决**：03.1 实现 Get(key) 按全部引用聚合（任一在窗即 Found）——过期判定已有答案；但 scoped-reader 授权与 Guard holder=Owner 弱形态仍成立 | memory/reference.go Get(key) | **R2.1/R2.2 修订单**（RefID/HolderID 最小化：不推翻按 key 聚合，补读作用域参数与 lease token） |
| §四.6 search.Query 无查询内容 | **仍成立** | port.go 无 Text | **R2.3 修订单** |
| §四.7 行数分页≠字节有界 | **仍成立** | 无 MaxBytes/Next | **R2.4 修订单** |
| §四.8 Query 分页/逐源水位/逐义务推进者 | **仍成立** | query/*.go 零命中 | **R2.5/R2.6 修订单** |
| §四.9 RL 快照可读引用/逐文件封账/reward 实例版本 | **仍成立** | rl/*.go 零命中 | **R3.1 修订单** |
| §四.10 结构门报告/后验四态分离/RefineRecord 接回 | **仍成立** | evolution/*.go 零命中 | **R3.2 修订单** |
| §六 检查器硬编码 19→20 | **已解决**（波 3 前我已修）；微信 main.go 944 行/旧 CI 覆盖欠账＝已登记迁移项（08.2/09.3） | — | 不重复处置 |

## 二、计划调整（task-graph 插入三批修订任务）

报告 §465 建议采纳：**三批骨架修订先于剩余实施**（修订性质＝阶段一延伸：改签名/字段/Godoc＋级联断言与测试，零新逻辑；已实现包的签名修订级联到实现）。编号 R1.x/R2.x/R3.x（内部设计修订，expectations §0 允许；89 是索引非上限）：

- **R1（第一批，5 单）**：R1.1 终局边界声明（GateReport for model——把 02.4 已实现语义的合同面声明清楚＋delta 措辞修正）；R1.2 BindingRef/BorrowDerived/排队保有；R1.3 ApprovalID+digest+等待状态；R1.4 NoticeEnvelope+CommitRecord 自包含扩展；R1.5 TransformCall per-tool 前置窄接口
- **R2（第二批，6 单）**：R2.1 读作用域（Get 带 scope/或 scoped-reader）；R2.2 Guard lease token；R2.3 search.Text/Cursor；R2.4 SegmentRequest/MaxBytes/Next；R2.5 mailbox 事实关联+连续前缀；R2.6 Query 分页/水位/逐义务推进者
- **R3（第三批，3 单）**：R3.1 rl 快照引用/逐文件封账/reward 实例版本/样本精确来源；R3.2 GateReport/后验四态/RefineRecord 接回；R3.3 根入口 Bridge()/待投递读面/模型工厂候选声明

**依赖重排**：R1→R2→R3 串行（共享签名闭包交叉，报告明示）；被取消的波 4 三单（01.5/02.5/03.3）及其他剩余 NN.M 实施加 R 批次前置（01.5→R1.2；02.5→R1 全部；03.3→R1.4；04.x→R1.3/R1.5/R2 相应项；05.x→R2.6；06.x→R3.1；07.x→R3.2；08.x→R3.3）。

## 三、规范矛盾裁决（本记录即裁决依据，delta 同步修正）

1. **persistent-event-loop"框架 ReAct 路径"措辞**：裁决＝唯一执行循环载体是 **tagent agent 包的事件驱动循环**（borrow→aggregate→model.Request→wrapper 受理→Notice 回流→下一 decision），非框架 Runner/LLMAgent 第二循环；delta 中"框架既有 ReAct 路径"字样改为"原生模型/工具协议之上的自有唯一循环"。
2. **causal-chain-isolation partition fallback**：裁决＝正常入口缺 session **具名拒绝**（不建 partition-only 链）；旧数据兼容导入限定在 09.2 迁移隔离域，不进普通多用户链。delta 修正。
3. **L0–L3 退役 vs storage-lifecycle 继承冲突**：裁决＝**行为不变量保留（有序压实存在、恢复清点先于回收）、层级算法从零定义**（03.4/03.2 按新计划实现，不搬旧 L1→L2→L3 机制）；storage-lifecycle delta 的"继承自动压实"措辞改为"压实行为保留、层级机制从零"。
4. **adjudications 计数修正**：R244 系处置时点值，收口（task-reentry/event-key-visibility 两行）后实际 **R242 · M268 · H60 · D31＝601**；adjudications-00.2.md §三同步修正并注明。
5. **G1 编译门 vs 原生探针口径**（报告 §五）：expectations G1 已实体化＝骨架编译门（成立）；补声明：**编译断言不等于原生时点/生命周期对齐**——原生接口探针（Model/IterModel 实调用、plugin.Manager 装配、MCP 真连接）随各域 NN.M 实施取证，登记于 trpc-reuse.md 待核表，两口径分列不互替代。

## 四、吸收报告的实施顺序建议

报告 §七的 7 条纵向链与现任务图顺序基本吻合；差异点＝"先合同闭合再纵向"已由 R1→R2→R3 前置落实。报告建议的反例清单（§七末）**逐条并入对应 NN.M 任务的 G2 场景**（派发令携带，不改任务行文本——evidence 记录）。

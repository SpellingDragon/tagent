# 04 quiescent-close：静止收束的关闭语义（事件驱动重构）

父变更：attention-lifecycle（依赖：无——独立语义单元，与既有 F3.5 修复批并行）。

## Why

现状的关闭语义（2026-10-18 runtime-rebuild 长出的"义务账+轮询排空"）与事件驱动模型不同构，且存在结构性病灶（已实证+独立评审确认）：

1. **判据永不收敛**：拒收条目永不销账（`answered` 只有门控位无销账面）+ `CloseRequest{}` 零 deadline = 进程退不出去；
2. **判据用影子账**：内存义务账（`memoryTask.obligations`，主体是项目自判的"派生索引"）被当关闭闸门；且混四个不可算回字段；
3. **顺序焊死**：`Host.Close` 先停消费者/settle 泵/关闸门再排空——"邮箱接受当结清"是该顺序的必然产物，恰与 spec"通知义务存活至逻辑消费确认"矛盾；
4. **双排空并行**：`settleOwed` 与 `settlePass` 双循环双键空间，再逻辑与；
5. **概念债**：假 TaskID 键空间（在飞模型被铸造成任务身份）、`- tasks: N open` 提示词泄漏、2ms 轮询。

独立评审（红队）确认：问题定义="排空判据永不收敛+零 deadline+闸门先焊死"；此前对话提案的"唯一判据/拒收不欠账"两处表述不成立，须修正。修正方向=**静止收束（quiesce）**：关闭是消费循环的终止相位，判据读权威账、等待挂事件、出口三态。

## What Changes

- **关闸分档**：`intake`（外部新输入）关；`derived`（关闭期由已有义务派生的回合）开——解除"等排空"与"拒收一切受理"的自锁。derived 派生工作计入收敛集合，每轮重评，预算兜底。
- **静止判据（三集合）**：工作收敛（链上无已受理未终态 task）∧ 消费收敛（权威未消费账清空：mailbox 未消费 + 持久欠通知）∧ 写入者停止（数据安全屏障，一等阻塞条件）。判据原子性=提交序号栅栏读快照并复核 seq 未变。
- **同源函数 `Unsettled()`** = 未终态工作 ∪ 未消费通知，**关闭/重启/报告三处同源**（替代重启侧"链+消费账"与关闭侧"内存账"的口径不一）。
- **三态返回**：`Quiesced`（自然收束）/ `Cut`（预算到点，余量在链上重启续上，**正常返回**）/ `Broken`（不可判，告警）。`CloseRequest` 禁零 deadline（`Deadline`→`Timeout`）。
- **报告**：超时/Cut 时逐地址报告未消费事件清单（事实键+来源）+ 未终态工作清单 + 未停写入者名单，全权威读。
- **拒收落 feedback 事实族**（拒收=消费动作的一种，跨重启可见、可重试、可归档）。
- **概念收敛**：删 Shutdown 全套、内存义务账、双排空、假 TaskID、2ms 轮询、`answered` 内存位、`- tasks: N open` 提示词泄漏；"义务/Obligation" 词汇退出；"排空/drain"→"静止/quiesce"。
- **契约与文档**：wiki §三重写、spec 相关条款 MODIFIED delta、`state-inventory` 裁决同步、契约测试的 `polls` 断言改写为收敛判据断言。

## 能力边界（不变量）

- 不假结束：取消只关 intake 闸门，Done 由收敛判据决定；
- 不假报告：截止/Cut 报告逐地址权威清单；
- 重启不丢：余量在链上，restore 续拉；
- 交付保证严语义不动（通知存活至逻辑消费确认），预算到点 Cut 合法。

## 症状直译锚（第一天红着）

| # | 场景 | 锚 |
|---|---|---|
| Q-A | 拒收后仍能关闭（Cut 合法出口），且拒收事实跨重启可见 | `TestQuiesceRejectedCutAllowed` |
| Q-B | 关闭期 derived 派生回合不被拒（不自锁），其自然收敛可达成 | `TestQuiesceDerivedNotSelfLocked` |
| Q-C | 判据同源：关闭/重启/报告读同一 `Unsettled()`，无"内存账"第二判据 | `TestQuiesceUnsettledSingleSource` |
| Q-D | 离线（模型不可用）仍能关闭（Cut 返回），报告逐地址清单 | `TestQuiesceOfflineCutReport` |

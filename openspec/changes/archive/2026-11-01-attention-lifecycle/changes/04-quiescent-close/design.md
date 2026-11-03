# 04 静态决策（D1–D10）

## D1 关闭=消费循环终止相位

关闭不是独立机制：停止注入后由消费循环自然收束。`Host.Close` 编排顺序修正为"关 intake → 静止（quiesce）→ 停消费者/设施"，替代现状"先停消费者再排空"（顺序焊死是"邮箱接受当结清"的根因）。

## D2 关闸分档

`intake`（外部新输入）关；`derived`（关闭期由已有义务派生的回合）开。derived 派生工作计入收敛集合，每轮重评（Q4=a）。**被否**：全关（自锁）、派生工作限 N 跳（人为断点）。

## D3 同源函数 `Unsettled()`

定义=**未终态工作（链上已受理未终态 task）∪ 未消费通知（权威账：mailbox 未消费 ∪ 持久欠通知）**。关闭/重启/报告三处共用，禁止各自实现第二判据。恢复侧"链+消费账"与关闭侧"内存账"的口径不一由此消失。

## D4 三态返回

`Quiesced`（自然收束）/ `Cut`（预算到点，余量在链上重启续上，正常返回）/ `Broken`（不可判，告警）。`CloseRequest` 禁零 deadline；`Deadline` 更名 `Timeout`（语义=显式超时事件的时刻）。**被否**：收敛才算成功（离线场景永远"失败"）。

## D5 写入者停止=一等屏障

写入存储/外部系统的执行体未确认停止=不收敛（数据安全屏障），与"工作完成=链上终态"并列但不同层：前者防数据损坏，后者是语义判据。设施协程（审计/seal/telemetry）的实现层完成信号可用（wg 类调度细节），不进设计词汇表。

## D6 判据原子性与增量求值

提交序号栅栏：读快照+复核 seq 未变，否则重判。判据增量维护（以权威账为索引），禁全链扫描（长链下 O(链) 成本不可接受）。

## D7 拒收落 feedback 事实族

拒收=消费动作的一种：落 feedback 事实（携带原通知引用与拒收原因），跨重启可见、可重试、可归档。spec 中"运输接收即结清"的旧实现路径删除，但"通知义务存活至逻辑消费确认"的严语义条款不动——拒收落链后它是**明确的消费结果**，不是欠账。

## D8 概念收敛清单

| 拆除 | 内容 |
|---|---|
| Shutdown 全套 | `runtime/agent/shutdown.go`（admitted/producers/settlePass/drain） |
| 内存义务账 | `memoryTask.obligations` 及 `Obligations()` 判据读面（诊断面按 D9 处置） |
| 双排空 | `settleOwed`（invoke.go）与 `settlePass`（shutdown.go） |
| 假 TaskID | `watchProducer(event.TaskID(inv.InvocationID))` 铸造路径 |
| 轮询 | `shutdownPollInterval`、`agentShutdownPollInterval` 及 `time.After` 等待点 |
| `answered` 内存位 | `obligationEntry.answered`（由 D7 的 feedback 事实替代） |
| 提示词泄漏 | `request.go` 的 `- tasks: N open` 环境块行 |

词汇："义务/Obligation" 退出（改"未终态工作/未消费通知"）；"排空/drain"→"静止/quiesce"；报告项→`UnfinishedWork`（如需保留 DrainReport 形状）。

## D9 诊断与渲染面的处置

`Obligations()` 的 9 处读点分四类：关闭判据（删）、门控 `land`（改读 `Unsettled()`）、诊断/透传（保留为观察面）、**模型提示词 `- tasks: N open`（删除）**。诊断面保留=观察用，不作关闭判据。

## D10 契约与文档同步

- spec：`async-task-execution` 的"通知义务"条款重写为"未消费通知的存活与结清"（严语义保留+拒收落链）；`persistent-event-loop` 的"循环停止"条款改"静止收束+三态出口"。
- wiki §三（execution-generations#lifecycle-convergence）：重写为静止收束语义，删轮询常量名。
- `state-inventory` 裁决：T6 派生索引裁决补"不作关闭判据"。
- 契约测试：`polls` 断言改"未收到收敛信号即不收束"（语义断言，非次数断言）。

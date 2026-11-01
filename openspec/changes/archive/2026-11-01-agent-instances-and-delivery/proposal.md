# agent 常驻实例与投递契约收敛（设计—实现对齐批 G1–G5）

## Why

设计—实现符合性探讨（2026-11-01，对照 `docs/.dev/插件框架、事件驱动与super_agent_文章骨架.md` 的设计预期）经四轮拷问达成共识：12 条机制级差距三分类后，**5 条判为真差距**（等价性论证不过或机制缺失），裁定收敛方向并立项（Q1=b、Q15=a 一个变更全包）。

共识判定基准（Q2=c 分层）：设计核心承诺（问题被解决）必须达成；机制形态允许替代，但替代须论证等价性（谁做判断、何时做、边界在哪），论证不过=真差距。

| # | 真差距 | 裁定 |
|---|---|---|
| G1 | 常驻异步事件流仅覆盖入口 agent；非入口走单槽 gate 同步直驱 | Q9=a：每 agent 有自己的实例，同步直驱退役，委派管线化 |
| G2 | 统一投递契约残缺：插件路径无范围门、两路投递元数据不同构；第三套投递门（`NewSendFace`）与第一套冲突（实现过程偏差） | Q4=b+Q10=a+Q11=b：契约对齐、删 `NewSendFace` |
| G3 | 多目标投递无逐目标账目 | Q12=b：单目标调用保持，框架补投递账目 |
| G4 | 长时反思闭环无回归保护（且历史测量引用悬空） | Q8=a+Q13=a：golden 入 CI 常规门 |
| G5 | 账目瑕疵（09 域 9.1 勾选不一致、`CommitToSession` 注释陈旧） | 并入本变更顺手修 |

已达成与取舍分支（不立变更）：插件恰六端口；Task 责任机制；上下文折叠/回捞；交付治理；恢复链；归属判断留在应用/模型层（Router 五决定的等价性论证：判断权=发送方显式寻址+main-sub 编排，框架"不猜地址"红线是反向保护）；暂不触发的 5 处局部语义保留（不建通用调度器）；业务停滞监督不立（FinalRound 未来插件化→HITL，远期注记）。

## What Changes

- **G1 agent 常驻实例（BREAKING 驱动层）**：每个 agent 地址挂常驻消费实例（现仅入口 agent）；同步直驱路径（`lockAddressContext`/`driveLocked`）退役；`call_subagent` 的 serve 形态管线化（委派=向 sub 实例投递输入、结果以事件回流，task 责任与 settle 恰好一次语义不变）。main–sub 定位为数据编排关系（main 持有与 sub 通信的事件管线），不是"调用-阻塞"关系。
- **G2 投递契约对齐**：两套投递执行**有意分立**（send=agent 主动行为、settle=框架状态变更，语义自由度不同，Q4=b），但账对齐（Q11=b）：投递元数据契约两路同构（目标/投递身份/回执键/来源分账键），去重与回执规则一致；插件派发路径补范围门（deliver_to 类白名单约束）；删除未接线的 `runtime/state/send.go` `NewSendFace`（第三套门，实现偏差产物）。
- **G3 投递账目**：保持单目标 send 调用（回执天然逐目标不失真），新增跨调用投递账目（一条事件投了哪些会话、各自投递身份与回执，可查可对账）。
- **G4 长时反思 golden**：脚本模型驱动 30–50 轮反思循环入 CI 常规门；golden 断言=水位单调不回退、零重复消费、零资源泄漏、跨会话投递逐目标恰一次、重启后水位续接。
- **G5 顺手修**：09 域 9.1 勾选对齐、`CommitToSession` 注释更新。
- **注记型预留**（Q14=b）：`material.go` 注释与设计文档登记"第七端口候选 `route`（事件→归属判断）"及准入条件（工作关联核心机制成型后开口）；恰六闭合契约不动。

## Capabilities

spec 已下沉到二级子变更；5.9 既有能力检索在各域首叶执行（预期归属：G1→`consumer-loop-convergence`/`persistent-event-loop` MODIFIED；G2/G3→`unified-task-semantics`/`cross-session-delivery` 一族 MODIFIED；G4→`symptom-anchors`/新增 golden 契约；G5 无 spec 面）。

## 症状直译锚（验收矩阵第一行，第一天红着）

| # | 用户视角直译场景 | 锚 |
|---|---|---|
| I1 | 非入口 agent 的会话在无任何调用方阻塞等待的情况下，被到达的事件驱动跑完一轮（每 agent 有自己的实例） | `TestInstancesSymptomEveryAgentResident` |
| I2 | 同一条事件经插件派发与经 send 投递到同一目标，产生的投递事实元数据同构、回执与去重规则一致 | `TestInstancesSymptomTwoEntriesOneAccount` |
| I3 | 一条事件投给多个会话后，账目能列出每个目标的投递身份与回执，逐目标恰一次 | `TestInstancesSymptomMultiTargetLedger` |
| I4 | 30+ 轮反思循环后水位单调、零重复消费、零泄漏，重启后水位续接 | `TestInstancesSymptomReflectionGolden` |

## Impact

### 代码

- `runtime/agent/consumer.go`、`assembly/host.go`（驱动分流重构：常驻实例推广、直驱退役）、`assembly/host_tools.go`（send 账目/元数据同构）、`runtime/state/send.go`（`NewSendFace` 删除）、`tool/subagent`（serve 管线化）、`features/evolution/meditation`（golden 面）。
- `runtime/agent/loop.go` 的 settle 恰好一次与会话占位约束**保持不变**（G1 的硬约束，用例防护先行）。

### 兼容

- BREAKING：驱动层形态改变（非入口地址从同步直驱变为常驻实例），委派结果回流语义保持（task 责任/Notice 身份不变）；`NewSendFace` 删除（无生产消费者）。
- 回退=回滚提交；settle 恰好一次、占位约束、白名单 fail-closed 语义零妥协。

## 非目标

- 不建通用"归属判断"调度器与业务 Thread 实体（Q6=c：暂无核心机制，`route` 端口注记预留）。
- 不立业务停滞/偏航监督（Q7=a）。
- 不改插件恰六端口契约（Q14=b）。
- 不改 send 参数契约（D6 冻结的 `target` 对象二选一保持，Q12=b 不扩 targets 数组）。

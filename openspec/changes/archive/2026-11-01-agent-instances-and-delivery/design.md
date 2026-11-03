# 静态决策（D1–D13）

## D1 范围与判定基准

G1–G5 一个变更全包（Q15=a）。判定基准=分层判定（Q2=c）：核心承诺必须达成；机制替代须论证等价性。共识总表（达成/取舍/真差距）为本变更的范围依据，取舍分支不立变更。

## D2 子域表

| # | 子域 | spec 能力（5.9 首叶检索确认） | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | agent-instances | consumer-loop-convergence、persistent-event-loop（MODIFIED） | 每 agent 常驻实例、直驱退役、委派管线化（G1） | 无（核心重构，探针先行） |
| 02 | delivery-contract | unified-task-semantics、cross-session-delivery（MODIFIED） | 投递元数据同构、插件路径补范围门、删 NewSendFace、投递账目（G2+G3） | 01（账目消费面可后置集成） |
| 03 | reflection-golden | symptom-anchors / golden 契约（5.9 定夺） | 长时反思 golden 入 CI 常规门（G4） | 无（可与 01/02 并行） |
| 04 | account-fixups | 无 | G5 账目瑕疵 + route 端口注记预留（Q14=b） | 无 |

## D3 波次表（波次仅汇报分组，解锁=依赖入 main）

| 波次 | 域 | 说明 |
|---|---|---|
| W0 防护先行 | 01 的用例防护面 | G1 动驱动核心前，settle 恰好一次/会话占位/白名单 fail-closed 的防护用例先立（Q9=a"做好规划和用例防护"） |
| W1 | 01（核心重构） | 探针先行：非入口地址常驻化最小闭环红→绿 |
| W2 | 02、03、04 并发 | 触碰集不相交（02=host_tools/state、03=测试面、04=注释/账目） |
| W3 | 集成 + F 收尾 | 全量门禁、症状锚红转绿双证据 |

## D4 合并规则（契约守护）

- 契约目录=主 `openspec/specs/` 与 `api/` 公共面；只做 D2 表声明的 delta。
- G1 独占 `runtime/agent/`、`assembly/host.go` 驱动分流；02 独占 `runtime/state/send.go`、`assembly/host_tools.go` 投递面——同 `assembly` 包两域串行或合单（R28 触碰集）。
- settle 恰好一次（`settle_wake_single_drive_test`）、会话占位（`occupancy`）、白名单 fail-closed 三条红线的既有测试**先绿后改**（W0 防护），改动中任何一条红=立即停线。

## D5 三级验收

孙任务 verify 三锚冻结（R29）；域级 spec scenarios 映射具名测试；变更级 DoD=①I1–I4 红转绿双证据 ②三条红线测试零回退 ③全量门禁 exit 0 ④CHANGELOG 四要素 ⑤openspec validate --strict。

## D6 G1：每 agent 常驻实例

每个 agent 地址挂常驻消费实例（`Consumers.Ensure` 语义推广到全部地址）；同步直驱（`lockAddressContext`/`driveLocked`）退役（清除式，不留双路径）；`call_subagent` serve 管线化=委派输入投递到 sub 实例、结果以事件回流（task 受理/Notice 身份/settle 恰好一次不变）。**main–sub=数据编排关系**（main 持有与 sub 通信的事件管线），不是调用-阻塞关系。**被否**：一步到位连占位约束一起改（风险集中，W0 防护先行分步验证）；"活跃地址才常驻"（活跃判定=框架猜测归属，违反显式寻址红线）。

## D7 双语义分立、账对齐（Q4=b / Q11=b）

send（agent 主动行为）与 settle（框架状态变更）是两类事实（`TypeExternalInput` vs `TypeFeedback`），事实类型**保留区分**；统一的是投递元数据契约：目标/投递身份/回执键/来源分账键两路同构，去重与回执规则一致。**被否**：事实类型统一（下游失去"谁在说话"判据）；新增 `TypeDelivery`（两类既有事实各自更精确）。

## D8 删 NewSendFace（Q10=a）

`runtime/state/send.go` 的 `NewSendFace`（第三套投递门，无生产调用方）删除；其"拒绝账本"概念若可取则小步并入第一套（`hostMeditationPort` 四门），不整体换基座。第一套为准（消费者原则）。删除前 `grep` 确认零生产引用，伴生引用降级回卷（§五）。

## D9 投递账目（Q12=b）

保持单目标 send 调用（D6 参数契约不扩 `targets`）；新增跨调用投递账目：一条源事件 → 多次单目标投递 → 逐目标投递身份与回执，可查可对账（对齐 9.2 表"事件投递"核对点）。**被否**：`targets` 数组（聚合回执失真、工具单文本回执表达 N 结果别扭）。

## D10 插件路径补范围门

插件派发路径（SettleDriver→fireNoticeTurn）补 deliver_to 类声明式白名单约束（现状仅地址完整性/实例非空校验）。门语义与 send 四门的白名单一档同构（fail-closed、具名拒绝）。

## D11 长时反思 golden（Q13=a）

脚本模型驱动 30–50 轮反思循环入 CI 常规门（墙钟分钟级）；golden 断言：水位单调不回退、零重复消费、零资源泄漏、跨会话投递逐目标恰一次、重启后水位续接。历史测量引用悬空（`docs/SMOKE-RESULTS.md` 从未存在）以本 golden 替代其证据职能。

## D12 route 端口注记预留（Q14=b）

`material.go` 注释与设计文档登记"第七端口候选 `route`（事件→归属判断）"及准入条件（工作关联核心机制成型后开口）；恰六闭合契约与 `validatePort` 不动。**被否**：占端口位（虚位稀释恰六防御价值）；空实现注册点（无消费者不建）。

## D13 取舍归档与远期注记

归属判断（Router 五决定）的等价性论证结论入设计文档：判断权=发送方显式寻址+main-sub 编排，框架"不猜地址"红线是反向保护；暂不触发/交回重判以 5 处局部语义保留，不建通用调度器。业务停滞监督不立；FinalRound 未来插件化→HITL 控制（远期注记，不排期）。

### D13 附录：归档明细（04 域落账，防重复探讨）

本附录只记结论与重判条件，不排期、不产生任务。

**A. 归属判断（Router 五决定）等价性论证结论**

- 结论：不立 Router 组件。归属判断的**判断权在发送方**——一次投递的目标由调用方显式寻址（`event.Address` 完整三段）给定，多目标场景由 main-sub 编排在发送侧展开；框架只做**校验与具名拒绝**，不做地址推断。
- 等价性：既有机制（地址完整性门、deliver_to 类白名单、实例归属、绑定活性、shutdown 门）逐条覆盖五决定的判定结果，代价是判定分散在五个局部点，收益是零推断、零新状态。
- 反向保护：框架"**不猜地址**"红线不是缺口而是约束——集中式 Router 一旦存在，任何未显式寻址的投递都会诱使它推断，红线随之失效。因此在"工作关联核心机制"成型前，集中化是净负收益。

**B. 暂不触发的 5 处局部语义（保留而非合并，交回重判的入口清单）**

| # | 局部语义 | 所在 | 当前判断权 | 重判条件 |
|---|---|---|---|---|
| 1 | `startsRange`（何事实开启一个 range） | `runtime/agent/loop.go` | 事实类型 + `trigger_source=task` 标记 | 出现"一条事实归属多轮"的真实用例 |
| 2 | `terminalSettleKinds`（结束任务的三种通知种类闭合表） | `assembly/settle_turn.go` | 提交方声明的 notice kind（accepted/progress 不入表） | 出现需要第四种终态的业务语义 |
| 3 | attention facts（approval/notice/summary 三类在场） | `assembly/host_attention.go` | 读时推导，不补造 question/clearance 事实 | 出现在场需要跨 agent 聚合的用例 |
| 4 | reflection watermark（反思窗口起点水位） | `features/evolution/meditation/digest.go` | 观察面在水位上的渲染器 | 出现多观察面共享单一水位的用例 |
| 5 | ops wake（ops 会话唤醒的地址完整性门） | `examples/wechat-bot/wechat.go` | 装配层门（`ErrOpsWakeAddressMissing`） | ops 语义上收进框架时随之上收 |

- 五处均**保留为局部决定**：不建通用调度器、不抽公共判定入口。集中化需要同时满足"(1) 工作关联核心机制成型 + (2) 至少两处出现跨域共享判定需求"，否则每处留在原地。
- 业务停滞监督（stall supervisor）**不立**：停滞判据依赖工作关联语义，先有前者再谈后者。

**C. FinalRound → HITL（远期注记）**

- 现状：收尾轮的**判断**已是插件实例（`agent.FinalRoundPlanner`，服务 fact->observation 端口，按看门狗预算立案并把判断作为事实写入）；判断之外的**收束动作**仍在循环自身决策边界完成，未外化。
- 远期方向：若把收束动作也外化（即"FinalRound 插件化"的剩余部分），其自然归属是 HITL 控制面（人工介入点的一种），可复用人工介入通道与审批面，而非新增调度组件。
- **不排期**：本变更不实现、不预留接口、不占端口位；仅记录方向，避免后人把"FinalRound 插件化"误读为待办。

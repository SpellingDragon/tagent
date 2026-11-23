# 契约 · 状态派生与切面覆盖（01.102 冻结）

- 候选指纹：`52d7b664521554fed8d7a5674ee1f934c410a3e3`（开工重核一致，分支 dev；`runtime-coherence/` 为未跟踪新面，无产品改动）。
- 依据：audit.md #2/#5/#6/#7/#11、evidence/anchors.md（S2＋Stateful 未写项节）、evidence/state-probes.md A/B 段、`runtime/task/commit.go`（CommitRecord/CommitResult）、`memory/ledger/segment_store.go`（FactCommitOutcome 五值）、`runtime/state/checkpoint.go:438-508`（RestoreState/pending/RestoreEmptyState）、`assembly/checkpoint.go:284-292`（RegisterStateSlice）、归档 runtime-recovery-ideal 12 域 design D8.1-H02b 与 D8.3（经 CARRYOVER 追溯，不改其文件）、spec delta `specs/recovery-derivation-contract/spec.md`。本单未跑任何 go 命令；实现由 02.204/02.205/02.206/03.301 承接。

## 一、每消费者一份有类型 apply（P2 主攻）

- 形态：每个可恢复消费者持**一份有类型 apply**；在线提交成功后的应用与恢复回放调用同一规则，MUST NOT 为恢复另立业务解释。`runtime/task` 的 `applyCommitLocked`（lifecycle.go:400）与 `foldLocked`（restore.go:239）双份结构按本条收敛为一份。定性（audit #5）：行为层双解释**已驳回**（同序列 status/revision/round/receipts ×10 一致），故本条是 P2 结构收敛要求，非缺陷修。
- 输入：已提交事实/记录（CommitRecord 族），按提交序；不含未持久化的进程内时间。
- 身份与幂等：mutation 身份与通知身份占位幂等——同身份重复不落第二事实；同身份异内容具名拒绝（`ErrMutationConflict`/`ErrRevisionConflict` 口径）。
- 提交结果分类消费：沿用既有五值 `FactCommitOutcome`，分派按归档 D8.1-H02b 已裁口径：Committed＝登记＋fold；AlreadyPresent／同内容 Conflict＝repeat 路径；异内容 Conflict＝具名拒绝；Uncertain＝具名"结果未知"，不登记、不 fold、不答成功；Refused＝具名错误。**分类由提交点产生，holder 不自判**。
- 失败语义：apply 失败具名上返，按既有全局拒 cut 口径受阻（长期错不放宽）；**恢复零副作用**——恢复期不发送、不结算、不折叠、不驱动冥想，待办由拥有者在恢复完成后推进。
- 外置面：时钟、唤醒、观测在 apply 之外；状态等价比较不含进程内时间。边界（如实记录）：仅内存 Flush 消费（日志无收据）时 obligations 在线 0 vs 恢复 2，属 at-least-once 口径——恢复按日志确认，重投由 `MemoryStore.AcceptNotice` 身份占位幂等吸收；记边界行为，不升格缺陷（A2 探针留作反证）。

## 二、切面覆盖自述与缺片五态

每个切片 SHALL 自述其覆盖的事实范围；框架按切片判定，MUST NOT 用投影或另一消费者的进度代答（D4）。五态逐态：

| 态 | 判定 | 处理 |
|---|---|---|
| 无切片 | tier 无 checkpoint | 走 `RestoreEmptyState` 全链重放重建（现状保留） |
| 缺片 | checkpoint 在册、已挂消费者无其切片 | 读该消费者所需历史重建，或具名拒绝 boot；**不得按空状态启动**（audit #6 已复现：fold 8/24、缺 16 条覆盖区事实、空态启动；机理＝`bootIndexWalk` 以 projection cut 定界＋`RestoreState` 只还声明切片） |
| 空切片 | 切片在册、载荷为声明的空态 | 按空态恢复——"声明为空"≠"无声明"，不与缺片混同 |
| 损坏 | 载荷不可读/非 JSON | 具名拒绝 boot，MUST NOT 静默按空态继续 |
| 尾部 | cut 之后的事实 | 尾段重放只完成"覆盖已成立"消费者的派生；缺片者 MUST NOT 以尾段冒充全部历史（8/24 反例即此） |

## 三、恢复前置关系由拥有者声明

消费者间恢复顺序依赖 SHALL 由拥有者的装配关系或类型化声明表达；协调者 MUST NOT 解析业务载荷、不按切片名特判（D4：否则每加一个消费者就要改协调者）。实例：任务账户先于邮箱——mailbox 席位以耐久账户欠账为源（`RestoreEmptyState` 契约注释原文），既有锚 `TestBootSeatsTheMailboxAfterTheDurableAccount`（checkpoint_test.go:2988）。

## 四、审批裁决三态（audit #2 承接）

- **未提交**（裁决确定未落账）：请求保持未决义务，切面必须陈述该 hold。
- **不确定**（写结果未知）：重启按链上事实折叠——事实在＝已裁决、事实不在＝回未决；链读失败具名上返，MUST NOT 折成"无裁决"（`rulingFromChain` 契约）。
- **已提交镜像失败**（事实已落账、进程内镜像在落账前被就地改写）：镜像以链为准重建；切面 MUST NOT 因镜像已被改写而丢项（S2 红：`Decide` 在 `fileAnswer` 前改 `req.Status`（approval_store.go:457-462），`statedByCut` 遂把该请求当已裁决跳过（:266-271））。
- 放行红线：`Lookup` 授权来源是链上事实（`facts.answer`），不是 `req.Status`；任一态 MUST NOT 把失败裁决读成批准（不放行负锚现绿，保持）。

## 五、重复事实语义（repeat ≠ filing）

归档 12 域 D8.3 裁定沿用，audit #7 复核：

- 已折叠旧身份的重试走 **repeat 路径**（`ReduceRepeat` 声明面）：不登记、不占 `factOrder`、MUST NOT 重述已折叠材料。
- 不同键同内容＝**新 filing**，登记＋fold；分类由提交点产生（五值），不由 holder 自判。
- MUST NOT 移动已完成范围：覆盖已折叠旧输出的同身份重试不重开轮次、不倒退呈现范围、不重复生成（12.R2.2）。
- f13509f9 的 "repeat folds again" 语义违背本条（7b0cde16 绿→f13509f9 红，`-count=5` 稳定红），02.205 修回。

## 六、Stateful 贡献的公开入口门（audit #11 承接）

现状静态证实：`HostOption` 即 `Option` 别名（host.go:151、run.go:1404），`assemblyConfig`（run.go:1330-1392）无状态切片贡献字段；唯一公开门 `Run.RegisterStateSlice`（assembly/checkpoint.go:287-292）仅 boot 后可用，而恢复发生在 assemble 内、Boot 返回即拒无持有者切片——经公开入口"贡献→重启→恢复"链今日不可构造。候选：

- **甲（已裁采用）**：`assemblyConfig` 增状态切片贡献字段＋新公开 option `WithStateSlice(holder)`，Boot/Assemble 同一转交点在 assemble 恢复**之前**注册，后端同 `StateRegistry.Register`——满足 P9"正式入口统一承接"；代价＝公共接口新增一项＋触碰共享面（run.go/host.go）。**编排者裁定（2026-11-22）**：采甲——S1 的消费者半面与 audit #11 缺口只有公开入口门可闭合，且该 option 与 `WithPlugins`/`WithGenerationConsumer` 同形（声明式贡献、无新挂点），属 P9 的必经步骤而非取向分叉；乙留作 03.301 的会话级配套不改此裁定。
- 乙：不改公共入口，扩 pending 机制为缺片补读（晚挂消费者补读 cut 前历史）——只解缺片覆盖，不解入口门本身；作 03.301 配套保留。
- 丙：并入 `WithPlugins` 插件生命周期——状态贡献与观察插件实例耦合、插件集冻结于 Boot（"never replaced"），语义摊派给插件契约；否决。

## 签发区

- **候选指纹**：`52d7b664521554fed8d7a5674ee1f934c410a3e3`（2026-11-22 重核）。
- **已证/已驳引用**：已复现 #2（审批切面丢项）、#6（缺片空态启动）、#7（retry 三项违 D8.3）；已驳回 #5（行为层双解释；边界＝内存 Flush at-least-once）；静态证实 #11（无公开入口门）。#1（Boot 丢插件贡献）归 02.203/01.104，不属本单。
- **所选/否决**：每消费者有类型 apply（否全仓 `Reduce`：解释权摊派、无法表达缺片与消费者身份）；切片自述覆盖（否投影进度代答：新消费者静默漏读）；缺片＝重建或具名拒绝（否"缺失即空"：已证丢未决工作）；前置由拥有者声明（否协调者特判）；五值分派沿用 H02b；入口门荐甲（乙不解门、丙耦合插件契约被否）。
- **02.204 精确写面**：`runtime/task/lifecycle.go`、`runtime/task/restore.go`、`runtime/task/checkpoint.go`、`runtime/state/durable.go`（与 02.201 串行）；若甲获裁，追加 `assembly/run.go`、`assembly/host.go`（共享面，编排者单写或冻结签名后集成）＋镜像测试。
- **02.205 精确写面**：`assembly/checkpoint.go`、`assembly/facts.go`、`assembly/task_checkpoint.go`、`assembly/task_checkpoint_test.go`、`assembly/checkpoint_test.go`。
- **02.206 精确写面**：`runtime/task/approval/approval_store.go`、`runtime/task/approval/attention_store.go`、`runtime/task/approval/` 下配套测试。
- **03.301 精确写面**：`assembly/checkpoint.go`、`assembly/compress_checkpoint.go`、`assembly/run.go`（共享面）。
- **接口/格式变化**：持久格式＝无（checkpoint 载荷、键族、版本零变化）。公共接口＝新增 `WithStateSlice` option 一项（**已裁采甲**，编排者 2026-11-22，见 §六；02.204 按此实施）。
- **真实消费者**：`tasksStateSlice`（assembly/task_checkpoint.go:31）、`mailboxState`（runtime/state/mailbox_checkpoint.go:37）、`projectionStateSlice`（assembly/checkpoint.go:1243）、`inputsStateSlice`（checkpoint_inputs.go:52）、`compressStateSlice`（compress_checkpoint.go:67）、`deliveryStateSlice`（host_delivery_checkpoint.go:120）、governance `StateSeat`（features/evolution/governance/checkpoint.go:99）。
- **三锚落点**：02.204 正＝`TestW1COnlineVsRestoreReceipted`（拟新增，W1-C 探针 A1/A3 转正；A2 边界差留反证不升格）、负＝`TestBootMountsContributedStateSlice`（拟新增，随甲落地，当前不可构造即 audit #11）、消费者＝`TestBootSeatsTheMailboxAfterTheDurableAccount`（已存在:2988）。02.205 正＝`TestRetryLeavesPresentedRangeAtTheLatestRound`（已存在:3279，现红）、负＝`TestRetryOfFoldedOutputDoesNotReopenTheRound`（已存在:3333，现红）、消费者＝`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`（已存在:3350，现红，真实重启）。02.206 正＝`TestApprovalWriteFailureDoesNotGrantPermission`（已存在:478）、负/消费者＝S2 `TestSymptomAnchorFailedRulingKeepsTheHold`（已存在，现红；Request/Decide/SnapshotEvidence/RestoreEvidence/Pending/Lookup 六真实入口）。03.301 正＝`TestNewConsumerRegistersSliceOnly`（已存在:2495，绿对照）、负＝`TestW1CLateConsumerCoversTheWholeChain`（拟新增，探针转正：缺片不得空态启动）、消费者＝`TestRecoveryEntriesPreserveStateAndObligations`（已存在:5625）。合计 11 落点＝已存在 7＋拟新增 4。
- **相邻回归**：02.204/02.205＝`./assembly`＋`./runtime/...` 全包（计时两锚独占串行，不与写单并行）＋`TestConcurrentSameFactKeepsOneApplication`（facts_test.go:2177，五值分派面）＋`TestNoPerConsumerDispatchPath`（checkpoint_test.go:2542）；02.206＝`./runtime/task/approval` 全包；03.301＝`./assembly`＋`./features/evolution/governance`＋golden/runtime 快照。
- **回滚触发**：任一三锚红且排除夹具因→停叶回门；甲被裁否或实施需改持久格式→回设计门重签本单；契约文本有误→删本文件重签，不叠补丁；回退验收＝与基线零 diff。
- **独立审**：待。

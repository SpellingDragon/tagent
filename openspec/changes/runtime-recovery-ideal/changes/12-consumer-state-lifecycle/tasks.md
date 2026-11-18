# 12 消费态生命周期 · 孙任务

> **当前状态（本轮勘误）**：12.R1/R2/R3 均未勾。乙案已有部分实现与历史红绿证据，但撤销 R264 的「三项 P1 全修／零额外成本／注释门零 findings」整体当前结论；原始记录仍见 design.md §十八，以本轮勘误为准。本轮仅静态核对并修改计划，没有运行测试或门禁，也不改写旧退出码。
>
> **阻断项**：12.R2.1 修恢复总提交序；R2.2 验已折叠旧输出重试；R2.3 在真实切点调查重复生成、补同通知跨进程至多一收据及异常保义务；R2.4 量边界读成本与全局拒 cut 影响。12.R3 保留后置，但 S5 验收先纠正为「具名拒卸载且原对象/义务仍在」，不能以放宽断言换绿。R2 的调查不等待依赖它的 R3。
>
> verify 三要素随任务行冻结（R29）：**正锚**（点名测试＋可复跑命令，测试名与 `-run` 正则逐字符一致）、**负锚**（具名错误路径测试，勾选硬门）、**消费者锚**（正式入口/真实宿主路径实测；测试手工补接线不算生产消费者）。验收面不得追认缩水，缩水＝该叶重开。
>
> 共同门：每叶结账前 `go build ./...` 与 `go vet ./runtime/state/ ./runtime/task/ ./assembly/` exit 0（build/test 与 commit 永不串联）；`go test` 命令 exit 0 且**每枚冻结锚逐名命中 > 0**（组合命令部分命中或零匹配都不是凭据）。
>
> 前置链（一级 D9）：12.R1 → G2（与 05.R1/07.R1 共用判据表）；12.R2 依 11.R2 + G2；12.R3 依 12.R2。没有 R1 裁定就不派发 R2/R3 的实现叶，不按汇报解锁。
>
> 当前行动单集中如下。文件集相对仓库根，另含本域 `tasks.md`/`design.md`；是后续实施白名单，本轮仅写这两份计划。标「拟新增、未运行」者主树尚缺；既有锚亦未在本轮复跑。每叶修复后由非实施者独立只读审查三锚及同机制路径，列范围、逐项凭据和未证项；不以实施者汇报或旧注释门结论放行。

- [ ] **12.R1 设计门：已提交未消费边界与插件生命周期证据（产出G2的一部分，不依赖G2完成）**

- **交付**（全部落在本域 design.md，不新增文件）：①J1/J2 判据与反事实结论；②证据表 E12-1..E12-8 的复跑与补证（含今日**未核**四项的实测结论）；③三候选（甲/乙/丙）择一并说明被否理由；④生命周期四态（挂载/禁用/卸载/替换）各自「应交接什么、交给谁、无承接者时如何拒绝」的定义表；⑤S4/S5 原始红证与当前验收（S4 复验；S5 纠正为拒卸载保原对象，尚待实现）；⑥全库「提交成功后返回 error 被吞」站点的语义单元枚举（按「结算/消费/投递完成」三类单元枚举，不按代码形态扫）。
- **实施期精确文件集**：`assembly/checkpoint_test.go`（现有 S4/S5 落点，纠正 S5 守卫）、`runtime/task/unsettled_test.go`（现有消费失败锚）；不重建旧拟新增的独立测试文件。只读生产实现；禁止改主 spec、门禁、配置、其他活跃变更与冻结归档。S5 的原对象消失守卫与已选拒卸载策略相反（`:3432-3435`），本叶先冻结正负验收，R3 实施；不删除原未完验收。
- **前置**：09.R1失败清单固定；本叶和05.R1/07.R1共同产出G2，不先等待G2。只读取证可并行，红锚写入及测试运行的共享构建闭包未核前不承诺并发；判据表由编排者合并，三域不得各写一版水位定义。
- **正锚**（已存在、本轮未跑）：`go test ./assembly/ -run '^TestConsumerBehindTheChainSurvivesARestart$' -count=1 -v`，保留旧红证；当前核「应用失败拒新 cut，从先前成功 cut 恢复并续做」，点名水位与未应用键，不把旧时点红写成当前实测。
- **负锚**（已存在、本轮未跑）：`go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1 -v`；具名上报已有历史绿证，义务及跨进程验收仍由 R2.3 补齐。
- **消费者锚**（既有，须保持绿）：`go test ./assembly/ -run '^(TestReducerDerivesEqualAtRuntimeAndRecovery|TestNewConsumerRegistersSliceOnly|TestNoPerConsumerDispatchPath)$' -count=1`（assembly/checkpoint_test.go:1852/:2074/:2121，钉「新消费者只注册切片」「恢复与运行期同一派生」「无按消费者特判」仍成立）。
- **跨会话/跨进程原验收保留**：`go test ./assembly/ -run '^(TestCrossSessionObligationsStayInTheirTargetSlice|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -count=1`（assembly/host_delivery_checkpoint_test.go:692、assembly/run_test.go:902）保持绿——会话切面不得因本域改动而把全局账塞进去（E12-8）。

- [ ] **12.R2 已提交但未消费的处理边界与同一状态应用（依 11.R2 + G2）**

- **交付**：按已选乙案保「切面不覆盖未应用事实」，失败具名且义务保留；运行/恢复使用同一 `Reduce` 并保持提交序。保留 `Wrapper.execute` 两处失败上报及保义务验收，不因旧绿免验。**全局拒 cut 是保守策略而非唯一设计**，影响由 R2.4 量化；不新建持久应用水位，不承诺无接收方支持的外部 exactly-once。
- **实施期精确文件集**：按 R2.1–R2.4 逐叶列出；父叶既有边界/失败补齐另含 `assembly/facts.go`、`assembly/checkpoint.go`、`runtime/state/checkpoint.go`、`runtime/state/checkpoint_test.go`、`runtime/state/mailbox.go`、`runtime/state/mailbox_checkpoint_test.go`、`runtime/task/wrapper.go`、`runtime/task/unsettled_test.go`、`assembly/checkpoint_test.go`。原 `assembly/run.go` 禁写条款由 R2.1/R2.2 的窄写面替代，与 02/05 串行；不新增旧候选测试文件。`memory/ledger/**`、治理实现、主 spec、门禁/配置均不在写面。
- **前置**：R1 通过（候选择定 + S4 红成立）；11.R2 的事实提交结果（「已提交」的权威定义）；与 05.R2/07.R2 **同触** `assembly/checkpoint.go` 与 `runtime/state/checkpoint.go` 时串行或合单，不并行派发。
- **正锚**（已存在、本轮未跑）：`go test ./assembly/ -run '^(TestConsumerBehindTheChainSurvivesARestart|TestCutNeverCoversAnUnappliedFact)$' -race -count=10 -v`；区分拒 cut、恢复漏折与错误水位。
- **负锚**：`go test ./assembly/ -run '^(TestCutRefusesToCoverAnUnappliedFact|TestRestartResumesTheObligationTheConsumerFailed)$' -race -count=10 -v`（已存在；绕过乙案拒 cut 或掐断恢复续做各应红，不虚构已不存在的应用水位）＋ `go test ./runtime/task/ -run '^TestConsumptionFailureKeepsTheNoticeOwed$' -race -count=10 -v`（拟新增、未运行，R2.3 承接；消费失败后同身份通知仍欠且错误具名）。
- **消费者锚**（既有 + 拟新增）：`go test ./runtime/state/ -run '^TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState$' -count=1`（runtime/state/mailbox_checkpoint_test.go:18，信箱欠账经切面与账户两路恢复）与 `go test ./assembly/ -run '^(TestBootSeatsTheMailboxAfterTheDurableAccount|TestSymptomAnchorBootCostFlatAcrossGenerations)$' -count=1`（assembly/task_custody_anchor_test.go:27、assembly/symptom_anchor_recovery_test.go:417）保持绿：真实 boot 入口不变慢、不丢欠账；拟新增跨进程重试锚 `TestRedeliveredNoticeAfterRestartSettlesOnce`（尚未建立）钉「同一通知身份重启后至多一条收据」。

- [ ] **12.R2.1 恢复按总提交序应用幸存与非幸存事实**。产出：覆盖区间与幸存者进入同一 `Reduce` 的顺序与运行期提交序一致，投影是否携带另行判断。事实：`assembly/run.go:2794` 先 `seedCoveredRange` 折非幸存者，`:2797` 再 `restoreSurvivors` 折幸存者；前一集合内部排序不等于两集合总序。前置：R1/G2/11.R2、02/05 释放共享写面；不等待 R3。精确文件集：`assembly/run.go`、`assembly/checkpoint_test.go`、`assembly/anchored_boot_parity_test.go`。
  正锚（拟新增、未运行）：`TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors`，构造幸存/非幸存交错且结果依赖顺序的合法 reducer，逐条比较运行/恢复序列与最终态。负锚（拟新增、未运行）：`TestAnchoredRecoveryRefusesAnOutOfOrderObligation`，恢复错误排序的变异应红；命令：`go test ./assembly/ -run '^(TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors|TestAnchoredRecoveryRefusesAnOutOfOrderObligation|TestAnchorRebuildFoldsTheRangeNoCheckpointVouches)$' -race -count=10 -v`。真实入口锚：`go test ./assembly/ -run '^(TestAnchoredBootMatchesFullBoot|TestDefaultBootRebuildsFromTheAnchor)$' -count=10 -v`，同合成耐久根重启；独立审核没有全局失序、漏折或双折，不只检查键是否出现。

- [ ] **12.R2.2 覆盖已折叠旧输出的同身份重试**。产出：重试不重新打开已完成轮次、不倒退 `SessionPresented`、不重复生成；判重依据不等同于当前投影保留键。事实：`memory/replay/replay_budget.go:110` 的 `Append` 以 `seen` 判重，`:309` 的 `forgetDroppedLocked` 清除被折叠键；既有 `TestRetryLeavesPresentedRangeAtTheLatestRound` 尚不足以证明旧输出被移出后仍安全。前置：R1/G2/11.R2，与 R2.1 共享面串行。精确文件集：`assembly/facts.go`、`assembly/run.go`、`assembly/checkpoint_test.go`、`memory/replay/replay_budget.go`、`memory/replay/replay_budget_test.go`、`memory/replay/skeleton_assert.go`。
  正锚：扩充既有 `TestRetryLeavesPresentedRangeAtTheLatestRound`，先实证旧键已被折叠移出，再经正式提交重试；负锚（拟新增、未运行）：`TestRetryOfFoldedOutputDoesNotReopenTheRound`（恢复旧判据的变异应红）；真实入口锚（拟新增、未运行）：`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`，正式 Boot 后提交同一旧身份、观察新轮次/生成/收据。命令：`go test ./assembly/ -run '^(TestRetryLeavesPresentedRangeAtTheLatestRound|TestRetryOfFoldedOutputDoesNotReopenTheRound|TestRestartRetryOfFoldedOutputKeepsThePresentedRange)$' -race -count=10 -v`。独立审同时核删除键路径与未删除键路径，不以无限保留 `seen` 换正确性。

- [ ] **12.R2.3 调查真实重复生成切点并补跨进程通知验收**。产出：提交→应用→结算→消费→投递/收据各真实失败切点的身份轨迹及最小红锚，随后修复已证缺口；同通知跨进程至多一条耐久收据，失败仍欠且具名。事实：冻结 `TestConsumptionFailureKeepsTheNoticeOwed`、`TestRedeliveredNoticeAfterRestartSettlesOnce` 尚缺；`runtime/agent/loop.go:373` 送达事实写失败后继续投递的后果未闭合。前置：R1/G2/11.R2；调查现在属于 R2，**不等 R3**；修复接收 R2.1/R2.2 的结果并串行集成。精确文件集：`assembly/facts.go`、`assembly/checkpoint_test.go`、`runtime/task/wrapper.go`、`runtime/task/unsettled_test.go`、`runtime/agent/loop.go`、`runtime/agent/loop_test.go`。
  正/真实入口锚（拟新增、未运行）：`go test ./assembly/ -run '^TestRedeliveredNoticeAfterRestartSettlesOnce$' -race -count=10 -v`；经正式 Boot、真实 localfile 合成根、两个独立进程在落事实/落收据前后受控终止后重投**同一 notice 身份**，记录生成次数、收据键/条数、欠账集；每种切点均有非空守卫，不以同进程重建冒充跨进程。负锚：`go test ./runtime/task/ -run '^(TestConsumptionFailureKeepsTheNoticeOwed|TestConsumptionFailureIsReportedByName|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10 -v`（首枚拟新增、未运行，其余既有）；另拟新增、未运行 `go test ./runtime/agent/ -run '^TestDeliveryFactFailurePreservesTheNoticeObligation$' -race -count=10 -v`。独立审核真实可达交错与异常保义务，不把日志出现或零 DATA RACE 当作不重复，不承诺外部发送 exactly-once。

- [ ] **12.R2.4 校准边界读成本与全局拒 cut 影响**。产出：真实 boot/会话 cut 的读取、解码、分配及热层增长证据；区分乙案不变量与全局拒绝的保守选择。事实：`newestSurvivorSeq`（`assembly/run.go:3033-3044`）已逐键读存储后才知道区间为空；空区间不等于零额外读取。前置：R1/G2/11.R2，采样基线绑定 R2.1/R2.2 版本；R2.3 的故障夹具可复用，不等待 R3。精确文件集：`assembly/checkpoint_test.go`、`assembly/anchored_boot_parity_test.go`、`runtime/state/checkpoint_test.go`（本叶先量化；需改实现另冻结窄写面）。
  正/真实入口锚（拟新增、未运行）：`go test ./assembly/ -run '^TestCheckpointRefusalCostAcrossSessions$' -count=10 -v`，同条件比较无错、瞬时错后同身份成功、长期错；量无关会话受阻数量/时长、拒 cut 次数、热层与恢复读量。既有成本入口：`go test ./assembly/ -run '^(TestDefaultBootRebuildsFromTheAnchor|TestSymptomAnchorSessionBootCostFlatAcrossGenerations)$' -count=10 -v`。负锚：`go test ./runtime/state/ -run '^TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames$' -race -count=10 -v`，确认量成本时没有跳过失败事实或 TTL 删义务。独立审核量了实际存储读而非仅 `Decoded`，按证据决定保留全局策略或另行比较安全的细分方案；本叶不宣称唯一设计或零成本。

- [ ] **12.R3 动态挂载/禁用/卸载/替换的义务交接（依 12.R2）**

- **交付**：沿 R1 已选「具名拒绝禁用/卸载并指出未完成项数」，拒绝后**原 holder 对象、切片名、未完义务均保留**；不是先删再报告或无承接者仍自动交接。`assembly/checkpoint_test.go:3432-3435` 当前 S5 守卫强制对象消失，与此策略相反，先纠正验收再实施，不能放宽为任意结果皆绿。空/已结切片允许正常卸载；替换保持同一切片名与权威，失败保旧代。不新增挂点或第二注册体系。
- **实施期精确文件集**：`runtime/state/checkpoint.go`、`runtime/state/genregistry.go`、`runtime/state/checkpoint_test.go`、`runtime/state/publish_test.go`、`assembly/plugin_catalog.go`、`assembly/run.go`、`assembly/checkpoint_test.go`、`assembly/plugin_catalog_test.go`、`assembly/hotupdate_generation_test.go`；沿用镜像文件，不新建旧候选测试文件。禁止改 `assembly/host.go` 治理接线、`api/config/**`、主 spec、门禁/配置与其他活跃变更。
- **前置**：12.R2 通过；使用当前已有的builtin目录与plugins声明实现，先验证其所需接口；不依赖后置复验叶02.R1完成（02.R1反而在本叶后验收），避免形成互等。原02旧勾选不替代该接口验证。
- **正锚**：既有 S5 `go test ./assembly/ -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -race -count=10 -v` 改钉拒绝具名＋原对象仍在＋后续 cut/重启同义务；拟新增、未运行 `go test ./runtime/state/ -run '^(TestSliceHandoverKeepsTheUnfinishedObligations|TestUnregisterAllowsASettledSlice)$' -race -count=10 -v`（前者保留原冻结身份，按拒卸载保原权威验收，不暗指自动交接；后者排除一律拒卸载的伪实现）。原代际/禁用验收不移除：`go test ./assembly/ -run '^(TestDisabledBuiltinHandsOverItsObligations|TestGenerationSwitchKeepsTheSameStateAuthority)$' -race -count=10 -v`（均拟新增、未运行；按拒绝保义务/同权威验，≥3 代，不预断现有代际实现丢态）。
- **负锚**（拟新增、未运行）：`go test ./runtime/state/ -run '^TestUnregisterRefusesASliceWithUnfinishedWork$' -race -count=10 -v`，移除拒绝或先删后报各应红，核原对象身份而非只核同名替身；`go test ./assembly/ -run '^TestPendingSliceIsRefusedByName$' -count=10 -v`，boot 无 holder 时具名拒绝。正负锚不接受「已删或未删均可」。
- **消费者锚**（既有、本轮未跑）：`go test ./assembly/ -run '^(TestPluginDeclarationDisablesABuiltinPlugin|TestPluginDeclarationRefusesAnUnregisteredName|TestBootAdoptNewConsumers)$' -count=1 -v` ＋ `python3 scripts/gates/check_plugin_mounting.py`。声明面仍只有 agent 级禁用；disposer 当前无生产调用方，先核实际入口，不用测试手工注册冒充线上卸载。独立审同时核可达性、原对象身份、空切片可卸载及跨重启义务，不能用放宽 S5 消掉红项。

## 收尾与界外

- 本域不产出 `specs/` 之外的实现清单；三叶完成后由编排者对账 05.R2/07.R2 是否复用同一 J1 边界（不复用＝偏差，须上报）。
- 显式不做：压缩账清减（03）、任务账清减（05.R2）、治理账清减（07.R2）、主规格同步（10）、归档/提交/推送（须用户明示）。

## 12.R1 执行记录（本轮实测；**12.R1 未勾**，理由见下）

**源树绑定**：全部 build/vet/test 与两枚红锚在 **HEAD `9547faca` 的隔离副本** `/tmp/recovery-g2consumer/tree`（`git archive 9547faca | tar -x`）执行。主树只读，18 个读取目标 `git diff --stat HEAD` 为空（逐字节等于 HEAD）。**主树零 `.go` 写入、零 `go build`/`go test`**；另一单正在编辑的五个文件本叶零读零写。不引用任何 CI 记录。

**交付落点**：全部在本域 `design.md` §七–§十五，未新增文件。①J1/J2＋反事实＋可达性勘误（§一）；②E12-1..E12-8 复跑补证＋未核四项逐项结账（§二/§九）；③候选择定＝**乙案**，甲丙被否理由（§十一，§四原「甲·推荐」已同步改写标勘误）；④生命周期四态定义表＋「具名拒绝并指出未完成项数」择一裁定（§十）；⑤S4/S5 红锚＋负锚（§十三）；⑥三类语义单元枚举（§十二）。

**本域 G2 部分＝FAIL**：C1（水位覆盖未应用事实）/C2（消费失败零报告）/C4（注销静默丢义务）/C6（运行期吞、恢复期抛，失败语义不对称）红；C3（跨重启幂等）绿；C5（四态经既有声明面）部分。**G2 整体未判，G1/G3/G4 未判**，判据表由编排者合并 05.R1/07.R1。

**锚与退出码（隔离树，命中数均 > 0）**：

| 类别 | 命令 | 命中 | exit |
|---|---|---|---|
| 正锚 S4（拟新增） | `go test ./assembly/ -run '^TestConsumerBehindTheChainSurvivesARestart$' -count=1` | 1 RUN/1 FAIL | **1**（期望红，成立） |
| S5（拟新增） | `go test ./assembly/ -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -count=1` | 1 RUN/1 FAIL | **1**（期望红，成立） |
| 负锚（拟新增） | `go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1` | 1 RUN/1 FAIL | **1**（期望红，成立） |
| 三锚稳定性 | 同上三条各 `-count=3` | 各 3 RUN/3 FAIL，同因 | **1**（无 flaky） |
| 幂等原型（拟新增） | `go test ./assembly/ -run '^TestSameStateAppliesIdempotentlyAcrossTwoRestarts$' -count=3` | 3 RUN/3 PASS | **0** |
| 消费者锚（既有） | `go test ./assembly/ -run '^(TestReducerDerivesEqualAtRuntimeAndRecovery\|TestNewConsumerRegistersSliceOnly\|TestNoPerConsumerDispatchPath)$' -count=1` | 3 RUN/3 PASS | **0** |
| 跨会话原验收（既有） | `go test ./assembly/ -run '^(TestCrossSessionObligationsStayInTheirTargetSlice\|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice\|TestBootSeatsTheMailboxAfterTheDurableAccount)$' -count=1` | 3 RUN/3 PASS | **0** |
| 信箱两路（既有） | `go test ./runtime/state/ -run '^TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState$' -count=1` | 1 RUN/1 PASS | **0** |
| 声明面/代际（既有） | `go test ./assembly/ -run '^(TestPluginDeclarationDisablesABuiltinPlugin\|TestPluginDeclarationRefusesAnUnregisteredName\|TestPluginDeclarationRefusesARunLevelPluginName\|TestBootAdoptNewConsumers\|TestHotReloadMeditationAnchorSurvivesRestart\|TestHotReloadGovernanceNoRestart)$' -count=1` | 6 RUN/6 PASS | **0** |
| 共同门 | `go build ./...`；`go vet ./assembly/`；`go vet ./runtime/task/` | — | 各 **0** |

**不勾 12.R1 的理由（正确纪律，非遗漏）**：正/负锚的文件白名单是 `assembly/consumer_lifecycle_anchor_test.go` 与 `runtime/task/consume_failure_anchor_test.go`，本轮派发令**禁止在主树写任何 `.go`**（同包另有在编辑单，构建闭包不可信）。两锚已在隔离树建立并 3/3 红，但**主树该二文件仍不存在**，任务行「拟新增、尚未建立」字面状态未变；勾选＝把隔离树证据冒充主树落盘。待编排者释放 `assembly/`／`runtime/task/` 写面，按白名单把原型落入主树并复跑同命令后方可勾。**验收面未缩水**：锚名、`-run` 正则、命令逐字未变。

**行号勘误（12.R3 消费者锚）**：`TestPluginDeclarationDisablesABuiltinPlugin`／`TestPluginDeclarationRefusesAnUnregisteredName` 实在 `assembly/plugin_mount_test.go:150`／`:188`，非任务行所写 `hotupdate_generation_test.go:622`（该行是 `TestBootAdoptNewConsumers`）。包级 `-run` 命令不变。

**本轮新发现（须具名承接，不得溶解）**：①`runtime/agent/loop.go:371-375` `Backflow` 写送达事实失败后**具名 WARN 但继续投递**，链上无收据→重启可重派；未构造最小复现，归 12.R2。②`StateRegistry.Register` 的 disposer **七个生产切片注册点全部丢弃**，唯一调用者是 `runtime/state/checkpoint_test.go:45-49`；故 12.R3「拒绝注销」须先有生产宿主，否则修好也无从触发。③`plugins:` **只能禁用 agent 级**，而三个 agent 级插件均不持切片；run/host 级 `-compress-consumer` 已被具名拒绝（既有绿锚）——proposal Why 第 4 条的「禁用后切片仍在、消费方不在」前提**在声明路径上不成立**，已在 design.md E12-7 勘误。

**未证项**：3 代以上替换累积态、外域清减后负锚级联、乙案停摆幅度、`loop.go:371` 重投后果、跨进程「至多一条收据」（`TestRedeliveredNoticeAfterRestartSettlesOnce` 尚未建立、未跑）。逐项见 design.md §十四。

**未检查**：`memory/ledger/**`、`assembly/symptom_anchor_recovery_test.go`、`scripts/gates/**`、`golden/**`、`.github/**`、`examples/wechat-bot/**`、05/07 两域计划目录、任何真实用户数据根。

## 12.R1 锚落盘（主树，2026-11-16 第二轮）：只落负锚一枚

**源树绑定**：HEAD `9547faca` 主工作树；`runtime/task/` 开工前干净；生产文件与隔离树逐字节相同。

**落盘（改名＋落点）**：

| 隔离树原型 | 主树落点 | 新名 | 本轮实测 |
|---|---|---|---|
| `runtime/task/consume_failure_anchor_test.go` / `TestConsumptionFailureIsReportedByName` | `runtime/task/unsettled_test.go`（既有文件，锚 `durable-delivery.md#envelope-states`） | 同名（冻结名未改） | **FAIL（故意红：G2-C2 上报面）** |
| 辅助 `consumeFailureLog` | 同上 | 同名保留 | — |

**落位理由**：原候选的独立文件 `consume_failure_anchor_test.go` 与 `unsettled_test.go` 争同一 `#envelope-states` 锚、而后者是 `unsettled.go` 的镜像 → 新文件会命中 `responsibility-fragmentation`。并入既有文件后该文件仍是唯一拥有此锚的测试文件。

**原始退出码（主树）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1 -v` | 1 | 1 RUN / 1 FAIL（`0 error line(s) recorded, none naming the task and the refusal`，守卫①sink 恰一次②结算确已提交③义务仍欠 1 条，全部成立） |
| 同上 `-count=3`（`red_task_x3.log`） | 1 | 3 RUN / 3 FAIL，同因 |
| `go run ./scripts/comment_policy -no-baseline runtime/task` （含本文件） | 0 | 0 finding |

**未落盘（明确排除，非缩水）**：**S4 正锚 `TestConsumerBehindTheChainSurvivesARestart`、S5 锚 `TestUnmountKeepsTheOwedItemsOfItsSlice`、幂等锚 `TestSameStateAppliesIdempotentlyAcrossTwoRestarts`** 三枚全部住在 `assembly/`，**不在本轮写面**（派发令把 `assembly/` 列为勿动目录，且并发单正在写该包；三者还引 `memory/ledger`，同属并发单的重写面）。故：

- **12.R1 仍未勾**：本叶冻结的**正锚**（C1/S4，红色症状直译锚）在主树不存在，叶的验收面缺第一枚，按「锚不存在就不是证据」不得勾。本轮只把「消费完成失败零报告」这半条（负锚）落进主树。
- **S5 的可达性限定照旧**：它走 disposer 面，而该面今天无生产调用方，属潜在能力红；未落盘不改变该限定。
- **S4 的端到端已证结论不变**：§九 E12-3 的「端到端已证」是对隔离 HEAD 树的原型而言，主树仍无该锚。

**退役相关性核对（R51）**：本域已落的一枚锚与三枚未落的原型均不引用退位机制（`grep 'Retire\|retire\|cold\|keptHot'` 零命中），无「任务五族留热」型判据，故本轮无一枚因 R51 而被排除。

## 12.R1 第三轮执行记录（2026-11-18，锚落主树；**仍不勾**，理由见 §十七·7）

**交付落点**：全部在本域 `design.md` §十七（十七·1 落盘表／十七·2 命令与红因原文／十七·3 两门红读数／十七·4 E12 八项复跑／十七·5 四类未核结账／十七·6 §十二 三类语义单元现行树复跑／十七·7 结账）。未新增任何设计文件。

**本轮写入（白名单内）**：`assembly/consumer_lifecycle_anchor_test.go`（新增，含 S4＋S5＋四处夹具/断言 helper）、本域 `design.md`、`tasks.md`。**未写**：`runtime/task/consume_failure_anchor_test.go`（冻结名已存在于 `runtime/task/unsettled_test.go:292`，同名新建将致整包编译失败；命令与测试全名逐字未变）、任何生产代码、门禁脚本、`golden/**`、`openspec/specs/**`、其他域文档。

**主树实测（11:48–11:54，命中数均 > 0；11:52 并发单落定后三条红锚与四条既有锚已全部复跑，结果逐条相同）**：

| 类别 | 命令 | 命中 | exit |
|---|---|---|---|
| 正锚 S4（新增落盘） | `go test ./assembly/ -run '^TestConsumerBehindTheChainSurvivesARestart$' -count=1` | 1 RUN / 1 FAIL | **1**（期望红） |
| S5（新增落盘） | `go test ./assembly/ -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -count=1` | 1 RUN / 1 FAIL | **1**（期望红） |
| 负锚（第二轮已落，未改） | `go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1` | 1 RUN / 1 FAIL | **1**（期望红） |
| 消费者锚（既有） | `go test ./assembly/ -run '^(TestReducerDerivesEqualAtRuntimeAndRecovery\|TestNewConsumerRegistersSliceOnly\|TestNoPerConsumerDispatchPath)$' -count=1` | 3 RUN / 3 PASS | **0** |
| 跨会话原验收（既有） | `go test ./assembly/ -run '^(TestCrossSessionObligationsStayInTheirTargetSlice\|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -count=1` | 2 RUN / 2 PASS | **0** |
| 信箱两路（既有） | `go test ./runtime/state/ -run '^TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState$' -count=1` | 1 RUN / 1 PASS | **0** |
| 声明面（既有，位置已迁移） | `go test ./assembly/ -run '^(TestPluginDeclarationDisablesABuiltinPlugin\|TestPluginDeclarationRefusesAnUnregisteredName\|TestPluginDeclarationRefusesARunLevelPluginName)$' -count=1` | 3 RUN / 3 PASS | **0** |
| 共同门 | `go build ./...`；`go vet ./runtime/state/ ./runtime/task/ ./assembly/` | — | 各 **0** |
| 布局门 | `go run ./scripts/codetools package-layout . examples/wechat-bot examples/cli-host` | 1 finding | **1**（仅本叶新文件 `test-mirror-missing`） |
| 注释门 | `go run ./scripts/comment_policy . examples/cli-host examples/wechat-bot` | 1 finding | **1**（仅本叶新文件 `responsibility-fragmentation`） |

**不勾 12.R1 的理由**：三枚锚已在主树红、共同门全绿，但**新文件单独打红两门**（`package-layout` 的 `test-mirror-missing`、`comment_policy` 的 `responsibility-fragmentation`，根因同一：该文件无生产词干可镜像、又与 `assembly/checkpoint_test.go` 争 `#framework-checkpoint` 锚）。处置为二选一——**并入 `assembly/checkpoint_test.go`**（工具自报 exit ①，与第二轮负锚并入 `unsettled_test.go` 同模式），或在 `package_layout.go` 的 `mirrorExemptFiles` 加一行＋在 `comment_policy` 基线加一个槽位——**两处均在禁写面**，须编排者裁定。裁定后无需补充证据即可勾。

**在飞瞬态（如实登记，未代改）**：实测窗口内并发单两次半成品编译错——`assembly/read.go:5 "errors" imported and not used`（11:50，随后自愈）与 `assembly/checkpoint.go:1081 cannot use "past" (untyped string constant) as int value in map index`（11:51 后，致 S4/S5 复跑 build failed，`go build ./...` 曾短暂 exit 1、11:51 复跑 exit 0）。本叶未读未写这些文件；**定向绿不授权声称全套件绿**：本报告只声明上表命中的用例，不声明 `go test ./...` 全绿。

**新增勘误**：§九/§十三 引用的 `assembly/plugin_mount_test.go:150/:188/:200` 在现行树已被删除，三个声明面测试现居 `assembly/plugin_catalog_test.go:106/:144/:156`；包级 `-run` 命令不变。其余行号漂移见 design §十七·4（`checkpoint.go` 重放面 −48 行、`run.go` 注册面 −14 行，结论均不变）。

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

- [x] 12.R1 设计门：已提交未消费边界与插件生命周期证据（产出G2的一部分，不依赖G2完成）

- **交付**（全部落在本域 design.md，不新增文件）：①J1/J2 判据与反事实结论；②证据表 E12-1..E12-8 的复跑与补证（含今日**未核**四项的实测结论）；③三候选（甲/乙/丙）择一并说明被否理由；④生命周期四态（挂载/禁用/卸载/替换）各自「应交接什么、交给谁、无承接者时如何拒绝」的定义表；⑤S4/S5 原始红证与当前验收（S4 复验；S5 纠正为拒卸载保原对象，尚待实现）；⑥全库「提交成功后返回 error 被吞」站点的语义单元枚举（按「结算/消费/投递完成」三类单元枚举，不按代码形态扫）。
- **实施期精确文件集**：`assembly/checkpoint_test.go`（现有 S4/S5 落点，纠正 S5 守卫）、`runtime/task/unsettled_test.go`（现有消费失败锚）；不重建旧拟新增的独立测试文件。只读生产实现；禁止改主 spec、门禁、配置、其他活跃变更与冻结归档。S5 的原对象消失守卫与已选拒卸载策略相反（`:3432-3435`），本叶先冻结正负验收，R3 实施；不删除原未完验收。
- **前置**：09.R1失败清单固定；本叶和05.R1/07.R1共同产出G2，不先等待G2。只读取证可并行，红锚写入及测试运行的共享构建闭包未核前不承诺并发；判据表由编排者合并，三域不得各写一版水位定义。
- **正锚**（已存在、本轮未跑）：`go test ./assembly/ -run '^TestConsumerBehindTheChainSurvivesARestart$' -count=1 -v`，保留旧红证；当前核「应用失败拒新 cut，从先前成功 cut 恢复并续做」，点名水位与未应用键，不把旧时点红写成当前实测。
- **负锚**（已存在、本轮未跑）：`go test ./runtime/task/ -run '^TestConsumptionFailureIsReportedByName$' -count=1 -v`；具名上报已有历史绿证，义务及跨进程验收仍由 R2.3 补齐。
- **消费者锚**（既有，须保持绿）：`go test ./assembly/ -run '^(TestReducerDerivesEqualAtRuntimeAndRecovery|TestNewConsumerRegistersSliceOnly|TestNoPerConsumerDispatchPath)$' -count=1`（assembly/checkpoint_test.go:1852/:2074/:2121，钉「新消费者只注册切片」「恢复与运行期同一派生」「无按消费者特判」仍成立）。
- **跨会话/跨进程原验收保留**：`go test ./assembly/ -run '^(TestCrossSessionObligationsStayInTheirTargetSlice|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -count=1`（assembly/host_delivery_checkpoint_test.go:692、assembly/run_test.go:902）保持绿——会话切面不得因本域改动而把全局账塞进去（E12-8）。

- [ ] 12.R2 已提交但未消费的处理边界与同一状态应用（依 11.R2 + G2）

- **交付**：按已选乙案保「切面不覆盖未应用事实」，失败具名且义务保留；运行/恢复使用同一 `Reduce` 并保持提交序。保留 `Wrapper.execute` 两处失败上报及保义务验收，不因旧绿免验。**全局拒 cut 是保守策略而非唯一设计**，影响由 R2.4 量化；不新建持久应用水位，不承诺无接收方支持的外部 exactly-once。
- **实施期精确文件集**：按 R2.1–R2.4 逐叶列出；父叶既有边界/失败补齐另含 `assembly/facts.go`、`assembly/checkpoint.go`、`runtime/state/checkpoint.go`、`runtime/state/checkpoint_test.go`、`runtime/state/mailbox.go`、`runtime/state/mailbox_checkpoint_test.go`、`runtime/task/wrapper.go`、`runtime/task/unsettled_test.go`、`assembly/checkpoint_test.go`。原 `assembly/run.go` 禁写条款由 R2.1/R2.2 的窄写面替代，与 02/05 串行；不新增旧候选测试文件。`memory/ledger/**`、治理实现、主 spec、门禁/配置均不在写面。
- **前置**：R1 通过（候选择定 + S4 红成立）；11.R2 的事实提交结果（「已提交」的权威定义）；与 05.R2/07.R2 **同触** `assembly/checkpoint.go` 与 `runtime/state/checkpoint.go` 时串行或合单，不并行派发。
- **正锚**（已存在、本轮未跑）：`go test ./assembly/ -run '^(TestConsumerBehindTheChainSurvivesARestart|TestCutNeverCoversAnUnappliedFact)$' -race -count=10 -v`；区分拒 cut、恢复漏折与错误水位。
- **负锚**：`go test ./assembly/ -run '^(TestCutRefusesToCoverAnUnappliedFact|TestRestartResumesTheObligationTheConsumerFailed)$' -race -count=10 -v`（已存在；绕过乙案拒 cut 或掐断恢复续做各应红，不虚构已不存在的应用水位）＋ `go test ./runtime/task/ -run '^TestConsumptionFailureKeepsTheNoticeOwed$' -race -count=10 -v`（拟新增、未运行，R2.3 承接；消费失败后同身份通知仍欠且错误具名）。
- **消费者锚**（既有 + 拟新增）：`go test ./runtime/state/ -run '^TestMailboxSliceRestoresEveryObligationTheCutAndTheAccountState$' -count=1`（runtime/state/mailbox_checkpoint_test.go:18，信箱欠账经切面与账户两路恢复）与 `go test ./assembly/ -run '^(TestBootSeatsTheMailboxAfterTheDurableAccount|TestSymptomAnchorBootCostFlatAcrossGenerations)$' -count=1`（assembly/task_custody_anchor_test.go:27、assembly/symptom_anchor_recovery_test.go:417）保持绿：真实 boot 入口不变慢、不丢欠账；拟新增跨进程重试锚 `TestRedeliveredNoticeAfterRestartSettlesOnce`（尚未建立）钉「同一通知身份重启后至多一条收据」。

- [x] 12.R2.1 恢复按总提交序应用幸存与非幸存事实。产出：覆盖区间与幸存者进入同一 `Reduce` 的顺序与运行期提交序一致，投影是否携带另行判断。事实：`assembly/run.go:2794` 先 `seedCoveredRange` 折非幸存者，`:2797` 再 `restoreSurvivors` 折幸存者；前一集合内部排序不等于两集合总序。前置：R1/G2/11.R2、02/05 释放共享写面；不等待 R3。精确文件集：`assembly/run.go`、`assembly/checkpoint_test.go`、`assembly/anchored_boot_parity_test.go`。
  正锚（拟新增、未运行）：`TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors`，构造幸存/非幸存交错且结果依赖顺序的合法 reducer，逐条比较运行/恢复序列与最终态。负锚（拟新增、未运行）：`TestAnchoredRecoveryRefusesAnOutOfOrderObligation`，恢复错误排序的变异应红；命令：`go test ./assembly/ -run '^(TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors|TestAnchoredRecoveryRefusesAnOutOfOrderObligation|TestAnchorRebuildFoldsTheRangeNoCheckpointVouches)$' -race -count=10 -v`。真实入口锚：`go test ./assembly/ -run '^(TestAnchoredBootMatchesFullBoot|TestDefaultBootRebuildsFromTheAnchor)$' -count=10 -v`，同合成耐久根重启；独立审核没有全局失序、漏折或双折，不只检查键是否出现。

- [x] 12.R2.2 覆盖已折叠旧输出的同身份重试。产出：重试不重新打开已完成轮次、不倒退 `SessionPresented`、不重复生成；判重依据不等同于当前投影保留键。事实：`memory/replay/replay_budget.go:110` 的 `Append` 以 `seen` 判重，`:309` 的 `forgetDroppedLocked` 清除被折叠键；既有 `TestRetryLeavesPresentedRangeAtTheLatestRound` 尚不足以证明旧输出被移出后仍安全。前置：R1/G2/11.R2，与 R2.1 共享面串行。精确文件集：`assembly/facts.go`、`assembly/run.go`、`assembly/checkpoint_test.go`、`memory/replay/replay_budget.go`、`memory/replay/replay_budget_test.go`、`memory/replay/skeleton_assert.go`。
  正锚：扩充既有 `TestRetryLeavesPresentedRangeAtTheLatestRound`，先实证旧键已被折叠移出，再经正式提交重试；负锚（拟新增、未运行）：`TestRetryOfFoldedOutputDoesNotReopenTheRound`（恢复旧判据的变异应红）；真实入口锚（拟新增、未运行）：`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`，正式 Boot 后提交同一旧身份、观察新轮次/生成/收据。命令：`go test ./assembly/ -run '^(TestRetryLeavesPresentedRangeAtTheLatestRound|TestRetryOfFoldedOutputDoesNotReopenTheRound|TestRestartRetryOfFoldedOutputKeepsThePresentedRange)$' -race -count=10 -v`。独立审同时核删除键路径与未删除键路径，不以无限保留 `seen` 换正确性。

- [x] 12.R2.3 调查真实重复生成切点并补跨进程通知验收。产出：提交→应用→结算→消费→投递/收据各真实失败切点的身份轨迹及最小红锚，随后修复已证缺口；同通知跨进程至多一条耐久收据，失败仍欠且具名。事实：冻结 `TestConsumptionFailureKeepsTheNoticeOwed`、`TestRedeliveredNoticeAfterRestartSettlesOnce` 尚缺；`runtime/agent/loop.go:373` 送达事实写失败后继续投递的后果未闭合。前置：R1/G2/11.R2；调查现在属于 R2，**不等 R3**；修复接收 R2.1/R2.2 的结果并串行集成。精确文件集：`assembly/facts.go`、`assembly/checkpoint_test.go`、`runtime/task/wrapper.go`、`runtime/task/unsettled_test.go`、`runtime/agent/loop.go`、`runtime/agent/loop_test.go`。
  正/真实入口锚（拟新增、未运行）：`go test ./assembly/ -run '^TestRedeliveredNoticeAfterRestartSettlesOnce$' -race -count=10 -v`；经正式 Boot、真实 localfile 合成根、两个独立进程在落事实/落收据前后受控终止后重投**同一 notice 身份**，记录生成次数、收据键/条数、欠账集；每种切点均有非空守卫，不以同进程重建冒充跨进程。负锚：`go test ./runtime/task/ -run '^(TestConsumptionFailureKeepsTheNoticeOwed|TestConsumptionFailureIsReportedByName|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10 -v`（首枚拟新增、未运行，其余既有）；另拟新增、未运行 `go test ./runtime/agent/ -run '^TestDeliveryFactFailurePreservesTheNoticeObligation$' -race -count=10 -v`。独立审核真实可达交错与异常保义务，不把日志出现或零 DATA RACE 当作不重复，不承诺外部发送 exactly-once。

- [x] 12.R2.4 校准边界读成本与全局拒 cut 影响。产出：真实 boot/会话 cut 的读取、解码、分配及热层增长证据；区分乙案不变量与全局拒绝的保守选择。事实：`survivorPositions`（`assembly/run.go:3083-3100`，锚分支`:2784/:2790`；原登记`newestSurvivorSeq:3033-3044`陈旧、run.go零命中，R284勘误）已逐键读存储后才知道区间为空；空区间不等于零额外读取。前置：R1/G2/11.R2，采样基线绑定 R2.1/R2.2 版本；R2.3 的故障夹具可复用，不等待 R3。精确文件集：`assembly/checkpoint_test.go`、`assembly/anchored_boot_parity_test.go`、`runtime/state/checkpoint_test.go`（本叶先量化；需改实现另冻结窄写面）。
  正/真实入口锚（拟新增、未运行）：`go test ./assembly/ -run '^TestCheckpointRefusalCostAcrossSessions$' -count=10 -v`，同条件比较无错、瞬时错后同身份成功、长期错；量无关会话受阻数量/时长、拒 cut 次数、热层与恢复读量。既有成本入口：`go test ./assembly/ -run '^(TestDefaultBootRebuildsFromTheAnchor|TestSymptomAnchorSessionBootCostFlatAcrossGenerations)$' -count=10 -v`。负锚：`go test ./runtime/state/ -run '^TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames$' -race -count=10 -v`，确认量成本时没有跳过失败事实或 TTL 删义务。独立审核量了实际存储读而非仅 `Decoded`，按证据决定保留全局策略或另行比较安全的细分方案；本叶不宣称唯一设计或零成本。

- [x] 12.R2.5 送达事实写失败的义务保留与投递方向（承接 R2.3 写侧留口；方向经 R287 裁定乙案并于 2026-11-18 落地）。产出：闭合 Backflow `commitSettled` 失败后「具名但不停」的静默语义改变——`consumerWarn`＋继续 `in.deliver` 而 sink 无条件 `Consume`。**机制勘误（推翻本叶原文归因，以 R287 为准，原叶行「链无收据⇒重启 settled() 按身份读不到」作废）**：`commitSettled`（`runtime/agent/loop.go:707`）提交的是 `event.TypeTaskSettled` **送达标记**，不是持久消费收据；收据唯一写入者＝`assembly/facts.go` 的 `store.Consume`（`runtime/state/durable.go:421` 的 `settled()` 只读 consume/reject 键，唯一写者 `:833/:858 recordConsumption`）。真实缺陷＝标记 commit 失败而 `Backflow` 无返回值⇒sink 无条件 `Consume`⇒**义务被清而送达标记永缺**：三个读面（`memory/replay/fold.go:132`、`assembly/request.go:1288`、`assembly/host_tools.go:1002`）少一条记录，且 `resultFactOnChain` 为假时该事实是投递正文在链上的唯一记录。**方向＝乙（R287 裁定，非本叶自裁）**：甲（阻断 `in.deliver`）被否——非权威所需（本域 design:41「不承诺外部发送 exactly-once」）、增 liveness 风险（标记持续失败则模型永不见）、`Backflow` void 下阻断须更大重构；丙（重排次序）前提被证伪——崩溃落在交接与收据之间时 `Consume` 未执行⇒重启按欠账重投同一身份。**落地三处**：①`Backflow` 签名 `void→error`，拒收以新增 `consumerError` 具名上报（标记 commit 失败是静默语义改变⇒ERROR 级；progress 分支与 `:401 commitProgressFact` 审计面不动）；②sink `refusal != nil` 走**既有** `retainNoticeOwed`（`facts.go:1011`）而非 `Consume`，其 doc 由「close 专用」泛化为「两成因（close 已返回／标记被拒）同一处置」，不新造第二义务保留机制；③`in.deliver` 照常（进程内交接非外部动作，`loop.go:2074-2084`）。精确文件集（实际落地）：`runtime/agent/loop.go`、`runtime/agent/skeleton_assert.go`（签名断言级联，白名单外但编译强制）、`assembly/facts.go`（sink 分支＋`retainNoticeOwed` doc 泛化，无其他 hunk）、`assembly/host_test.go`（新负锚＋helper 落 assembly：注入需 `run.loop.SetFactCommit`，任务行「若此锚更适合落 assembly…按真实入口归属落点」预授权，测试名逐字符不变；**R288 勘误**：原记录误名 `delivery_fact_obligation_test.go`，该文件不存在，锚实在 `host_test.go:481`，＋401 行含 helper）。
  已跑逐条命令与原始退出码见文末「12.R2.5 执行记录」；`go build ./...`/`go vet`/`gofmt -l` 三绿，两负锚变异均红且复原后 sha256 一致。
  负锚（R2.3 冻结沿用，**已建、已跑**；落点＝`assembly/host_test.go:481`，测试名逐字符不变；R288 勘误：原记名 `assembly/delivery_fact_obligation_test.go` 该文件不存在）：`go test ./assembly/ -run '^TestDeliveryFactFailurePreservesTheNoticeObligation$' -race -count=10 -v`——注入 `TypeTaskSettled` 提交失败，非空守卫断言①注入确发生（拒收计数＝1）②义务确仍欠（座位回 `ClaimPending`、根零消费收据、链零送达标记）③乙案投递不停（该轮 `DecisionCount=2` 完成，模型读到工具结果）④重启后同一通知恰写一条送达标记＋一条收据，第二次要约零第二条（按 key 幂等）。真实入口锚：`go test ./assembly/ -run '^TestRedeliveredNoticeAfterRestartSettlesOnce$' -race -count=10 -v`（R2.3 已建，本叶复跑：写侧修复后跨进程至多一条耐久收据仍绿）。负锚联动：`go test ./runtime/task/ -run '^(TestConsumptionFailureKeepsTheNoticeOwed|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10 -v`。独立审核真实可达交错与异常保义务，不把日志出现或零 DATA RACE 当作不重复，不承诺外部发送 exactly-once；`:401 commitProgressFact` 是审计面非结算收据，本叶未改。


- [x] 12.R2.6 fuse 链增量回归根因修复（R303，08.R3 全树消费者锚阻断）：产出＝定位 `tests/integration/execution` 两 fuse 红（`TestResponseCallCapFuse/approval_admits_the_whole_batch` results=184≠200、`TestStormPropagationFuse/an_approved_storm_leaves_a_bounded_chain_increment` results=403）的引入 hunk 并最小修复。`responseStormChain` 数 `ReplayEntries` 的 TypeThinkingPlan/TypeActionCommand；HEAD 树绿、现树红⇒候选引入。须用 /tmp HEAD archive 叠文件二分（禁 git 写仓库），不得削弱 fuse 断言/抬界。写面＝根因所在生产文件（诊断后精确登记，可能 assembly/facts.go/run.go/compress_consumer.go 或 runtime/agent/loop.go）＋对应测试＋本域 tasks/design；若根因属他域先报归属。正锚＝两 fuse `-count=1` 绿＋`go test ./tests/integration/execution -count=1` 绿；负锚＝根因变异复红；消费者锚＝`go test ./assembly -count=1`＋相关域锚不回退。

- [x] 12.R3 动态挂载/禁用/卸载/替换的义务交接（依 12.R2）

- **交付**：沿 R1 已选「具名拒绝禁用/卸载并指出未完成项数」，拒绝后**原 holder 对象、切片名、未完义务均保留**；不是先删再报告或无承接者仍自动交接。`assembly/checkpoint_test.go:3432-3435` 当前 S5 守卫强制对象消失，与此策略相反，先纠正验收再实施，不能放宽为任意结果皆绿。空/已结切片允许正常卸载；替换保持同一切片名与权威，失败保旧代。不新增挂点或第二注册体系。
- **实施期精确文件集**：`runtime/state/checkpoint.go`、`runtime/state/genregistry.go`、`runtime/state/checkpoint_test.go`、`runtime/state/publish_test.go`、`assembly/plugin_catalog.go`、`assembly/run.go`、`assembly/checkpoint_test.go`、`assembly/plugin_catalog_test.go`、`assembly/hotupdate_generation_test.go`；沿用镜像文件，不新建旧候选测试文件。禁止改 `assembly/host.go` 治理接线、`api/config/**`、主 spec、门禁/配置与其他活跃变更。
- **前置**：12.R2 通过；使用当前已有的builtin目录与plugins声明实现，先验证其所需接口；不依赖后置复验叶02.R1完成（02.R1反而在本叶后验收），避免形成互等。原02旧勾选不替代该接口验证。
- **正锚**：既有 S5 `go test ./assembly/ -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -race -count=10 -v` 改钉拒绝具名＋原对象仍在＋后续 cut/重启同义务；拟新增、未运行 `go test ./runtime/state/ -run '^(TestSliceHandoverKeepsTheUnfinishedObligations|TestUnregisterAllowsASettledSlice)$' -race -count=10 -v`（前者保留原冻结身份，按拒卸载保原权威验收，不暗指自动交接；后者排除一律拒卸载的伪实现）。原代际/禁用验收不移除：`go test ./assembly/ -run '^(TestDisabledBuiltinHandsOverItsObligations|TestGenerationSwitchKeepsTheSameStateAuthority)$' -race -count=10 -v`（均拟新增、未运行；按拒绝保义务/同权威验，≥3 代，不预断现有代际实现丢态）。
- **负锚**（拟新增、未运行）：`go test ./runtime/state/ -run '^TestUnregisterRefusesASliceWithUnfinishedWork$' -race -count=10 -v`，移除拒绝或先删后报各应红，核原对象身份而非只核同名替身；`go test ./assembly/ -run '^TestPendingSliceIsRefusedByName$' -count=10 -v`，boot 无 holder 时具名拒绝。正负锚不接受「已删或未删均可」。
- **消费者锚**（既有、本轮未跑）：`go test ./assembly/ -run '^(TestPluginDeclarationDisablesABuiltinPlugin|TestPluginDeclarationRefusesAnUnregisteredName|TestBootAdoptNewConsumers)$' -count=1 -v` ＋ `python3 scripts/gates/check_plugin_mounting.py`。声明面仍只有 agent 级禁用；disposer 当前无生产调用方，先核实际入口，不用测试手工注册冒充线上卸载。独立审同时核可达性、原对象身份、空切片可卸载及跨重启义务，不能用放宽 S5 消掉红项。

## 执行证据（2026-11-18 · R266 · 12.R2.1+R2.2，openspec-apply实施＋编排者核销）

- **实施**（A单openspec-apply，撞150轮于收尾记录阶段，实现已完整）：`assembly/facts.go`新增`chainHoldsFact`（按身份查链是否已持该事实），`commitFactNoCadence`对已持事实只`reduceCommitted`不占新提交位；`foldMapped`边界判据改「只认链最新事实」（读ledger提交序，非投影是否保留该键）——修R2.2折叠后重试，不动`replay.Append`/`seen`。`assembly/run.go`修R2.1：幸存/非幸存按总提交序进入同一Reduce。`persistFact`的(bool,error)中途实验已回退为单error，调用方一致。
- **正/负/入口锚**（编排者复跑）：`TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors`、`TestAnchoredRecoveryRefusesAnOutOfOrderObligation`、`TestRetryOfFoldedOutputDoesNotReopenTheRound`、`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`、`TestRetryLeavesPresentedRangeAtTheLatestRound`（含新增/folded子测）、`TestAnchorRebuildFoldsTheRangeNoCheckpointVouches` 九锚 `-race -count=10` 全绿（14.5s，0 DATA RACE）。
- **全量回归**：`go test ./assembly -count=1` 确定性红仅 `TestUnmountKeepsTheOwedItemsOfItsSlice`（S5，12.R3未实施）；`TestSymptomAnchorBootCostFlatAcrossGenerations` 在全量并发负载下偶发墙钟红，隔离 `-count=5` 全PASS，计数面（journalBytes/indexEntries/durableLoadCount/indexWalk）全平坦零＝决定性证据，归未校准2×墙钟界噪声（09.R2校准），非本叶回归。
- **级联上报**：A净改facts.go漂移变异锚`MUT-EVID-04-position-boundary`（锚0命中），归D接续重指；build/vet绿。
- **未验证面**：12.R2.3跨进程收据、12.R2.4成本量化、12.R3卸载仍待；独立只读审未做（放行待审）。

## 执行证据（2026-11-18 · R278 裁定A案 · 恢复走序两处修复，openspec-apply实施）

- **缺陷（R278 实测，本轮复验）**：两条恢复走遍都曾「先 note 产物、后 seat 判决」。anchored 面 `run.go:2798` 的 `NoteProduct` 跑在其请求重放的 `replayChainPastCut` 之前（产物 request 在 behind 段，note 落空）；fallback 面按索引遍历方向重放，方向为「新→旧」时产物恒先于其请求到达（20 条 note 全落空）。生产后果：这些「产物已在链」判决永久留未完账，`Execute` 预检只摘 plan 命名判决、材料已折完即早退，故永不摘除，每次 checkpoint 重序列化。
- **实施**：①`assembly/run.go` anchored 分支把 `NoteProduct` 后移到 `replayChainPastCut`＋`RestoreProducts` 之后（该走遍以 `skip=refs[at].key` 跳过锚产物自身，故 note 仍只发生一次）；②降级走遍改为先取该 slot 的 refs 按 `ref.seq` 升序排序再重放——提交序来自每条 ref 携带的 seq，不来自遍历方向。
- **走序方向为何不能依赖（实测）**：临时探针（已删净）读出降级走遍的遍历序为 `seq=88,87,…,19`（新→旧），而 `EachIndexNewestFirst` 文档承诺「newest first」；`blankTypedTier` 构造的索引走遍给出的是升序，且其 `anchorRef.typ` 为空。两处叠加使「反向遍历＝提交序」的隐含前提在本 fixture 不成立。
- **agent 来源（R278 要求实测）**：该 slot 全部 70 条 ref 经 `replayRef` 返回 `owner="agent-a"`，无一为空、无第二取值；`if agent == "" { agent = owner }` 的「首到达」从最新换成最旧后取值不变（同 slot 即同 partition，`memory.PartitionIDFromName(agent)` 派生）。机制未改。独立佐证：parity 的 `fallbackSeeds[addr]` 按 `{agent-a, session-anchored-boot}` 取回成功。
- **三面读数**：修复前 full 20/0、anchored 19/1、fallback 0/20（struck/pending）；修复后 **full 20/0、anchored 20/0、fallback 20/0**。红转绿因果三态实测：红先落 `anchored_boot_parity_test.go:234`（anchored 面）→ 修①后移至 `:238`（fallback 面）→ 修②后全绿。
- **凭据**：`go build ./...` exit 0；`go vet ./assembly` exit 0；`TestAnchoredBootMatchesFullBoot -race -count=10` → exit 0，10/10 PASS；03.R2 六锚 `-race -count=10` → exit 0，60 PASS；A 恢复五锚 `-race -count=5` → exit 0，25 PASS（`TestAnchoredRecoveryPreservesCommitOrderAcrossSurvivors`、`TestAnchoredRecoveryRefusesAnOutOfOrderObligation`、`TestRetryLeavesPresentedRangeAtTheLatestRound`、`TestAnchorRebuildFoldsTheRangeNoCheckpointVouches`、`TestDefaultBootRebuildsFromTheAnchor` 各 5/5）；`go test ./assembly -count=1` → exit 0（302s，0 FAIL）。
- **未动面**：`anchored_boot_parity_test.go` 本轮零改动（三处 `answeredCount` 语义原样，上一轮已适配 `struckNotes()`）；R2.1/R2.2 成果（`seedCoveredRange`/`restoreSurvivors` 总序、`chainHoldsFact`、`foldMapped` 边界）未回退；未把 `answered` 请回常驻态（design:56）。
- **未验证面**：fallback 面经生产函数 `seedAnchoredReplay`（`Boot`→`seedReplay`→`seedAnchoredReplay` 同一实现）直接调用，无「空类型索引经 Boot 触发降级」的端到端锚；其余 tier 的遍历方向分支未各自实测（②排序对方向不敏感）；独立只读审未做（放行待审）。

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

---

## 12.R2.3 执行记录（与 05.R4 同根因合单，2026-11-18）

**调查结论（身份轨迹）**：`提交 → 应用 → 结算 → 消费 → 投递/收据` 五段的真实切点上，「结算已提交、尝试已落、收据未落」与「三类记录齐落」两种停止都由同一缺口放大——重启后账户只按内存表判 `settled()`/`attemptOf()`，已终态任务不在切片陈述内，尾段只读切后键，故两个判据同时答「否」。**最小红锚即两处**：`cut_before_the_receipt`（尝试已落、收据未落 → boot 驱动第二轮＝重复生成）与状态层的陈旧载荷（已结仍读成未结）。缺口在 `runtime/state/durable.go`，修法与约束见 05 域同轮记录。

**新增锚（本域冻结名，本次建立）**：
- `TestRedeliveredNoticeAfterRestartSettlesOnce`（`assembly/checkpoint_test.go`）：一个真 localfile 根、**四个真进程**（提交事实并落切面 → 记录/尝试落盘 → 陈述欠账的进程落陈旧载荷 → 收据落盘 → 驱动进程），不以同进程重建冒充跨进程；三条子用例：
  - `cut_before_the_turn`：boot 恰驱动 **1** 轮，第三个进程重投时驱动 **0** 轮，根上恒 **1** 条 `consume:` 收据；
  - `cut_before_the_receipt`：尝试按身份读回 → boot 驱动 **0** 轮并写下**停摆进程没写的唯一那条收据**；
  - `redelivery_after_the_receipt`：陈旧载荷仍点名该通知而账户已结 → boot 欠账 **0**、驱动 **0** 轮、收据仍 **1** 条。
  非空守卫：每条用例都先断言切面/载荷点名了该通知（`seatOfTask` 必中）且结算事实可读（`Run.CommittedEvent`），故「驱动 0 轮」证明的是账户作答，而非没有工作。
- `TestConsumptionFailureKeepsTheNoticeOwed`（`runtime/task/unsettled_test.go`）：信箱写失败后任务仍欠**同一四元身份**（`TaskID/Round/Kind/Seq/Event` 逐项相等）；随后一次无应答 flush 仍欠且原因具名（`Blocked` 记录该通知与 `did not answer`），再一次被接受的 flush 才清账；全程 `deliverer` 只见到这**一个**通知（两次交出、身份相同），commit 端口只有 **1** 条 settled 事实。
  负锚实测（临时把失败当应答＝`acceptDelivery` 无条件调用，探针已撤）：该用例 FAIL `the task owes [], want the delivery obligation of … still open`；复原后绿。注：同探针曾试「identity drift」（`Seq: round`）**未转红**——wrapper 把同一个 `notice` 值同时交给信封与信箱，该断言比较的是写入腿与重投腿，不比较两次派生，如实登记此边界。

**消费者锚**：两条锚的驱动面都是正式入口——`assembly.SettleDriver`（`DeclareScope` 已声明、读 `Run.CommittedEvent`、终态经 `settleBarrierFrom` 落账）与 `task.Wrapper.execute`＋`task.MemoryManager.Flush`；无任何测试专用接线。

**`runtime/agent/loop.go:373`（送达事实写失败后继续投递）**：本单**未闭合**。该项要求改 `loop.go` 且需先定「送达事实写失败 ⇒ 是否阻断投递」的语义取向；本单触及面不含该文件行为改动，故如实留作未完成面（不勾、不宣称）。

**未验证面**：12.R2.4 成本量化（`LoadStats` 不含这三条点读：其契约是「五个族按链序出现」，点读键不在其中，且 05.R2 的体积口径另立）；独立只读审者放行（R35）未做；一级整域复跑未做。

---

## 12.R3 执行记录（2026-11-18 · openspec-apply 实施，本叶只勾 12.R3，不勾 F、不 archive、不宣告整域完成）

**沿 R1 冻结裁定落地**：卸载/禁用取「具名拒绝并指出未完成项数」，不取自动交接；拒绝后**原 holder 对象、切片名、未完义务三者均保留**；空/已结切片正常卸载；替换保持同一切片名与同一权威。未新增第四挂点、未新建第二注册体系。

**未完项数的来源（本次唯一的接缝扩展）**：`runtime/state/checkpoint.go` 新增可选声明面 `unfinishedWork`（`UnfinishedWork() int`），按既有 `reducer`/`emitter`/`sessionSnapshotter` 先例以类型断言声明——**不声明该面的 holder 不问**（注册表不去读持有者没开的账），计数由持有者自报，注册表只读「卸载会丢几项」，不读项是什么。`StateRegistry.Register` 的 disposer 由 `func()` 改为 `func() error`：有未完义务时返回 `state: refuse to unmount the %q state slice: %d unfinished obligation(s) would be lost`，且**不删**；无义务时照旧删除并保持名字顺序。持有者读取在 `r.mu` 之外（沿「框架锁不跨持有者读取」纪律），删除段仍在锁内并二次核名。

**验收面纠正（S5）**：`assembly/checkpoint_test.go` 的 `TestUnmountKeepsTheOwedItemsOfItsSlice` 原守卫断言「unmount 后切片名消失」，与 R1 相反，已改写为：首个 cut 陈述 `owedItem` → `unmount()` 必须**具名拒绝**（含切片名与 `1 unfinished obligation(s)`）→ 注册表**仍点名该切片** → 拒绝后再开的 `laterItem` 由下一次 cut 陈述（同一次 cut 里两条都在，即同一 holder 而非同名替身）→ `Close` 后经 `windowChainRun` 重启并 reconcile 挂新 `owedSlice` → `restored.owes()` 恰为 `[owedItem, laterItem]` → 第三个 cut 陈述两者 → `settle()` 后 disposer 返回 nil 且名字消失。未放宽为「任意结果皆绿」。

**新增锚**：
- `runtime/state/checkpoint_test.go`：`TestUnregisterRefusesASliceWithUnfinishedWork`（负锚：拒绝措辞含切片名与 `2 unfinished obligation(s)`；`registry.mounted()` 仍是**那一个** holder；`Names()` 保留；拒绝后 cut 仍陈述新开项；结清后才可卸载）、`TestUnregisterAllowsASettledSlice`（排除「一律拒卸载」伪实现：已结切片离开，后续 cut 不再陈述该切片）、`TestSliceHandoverKeepsTheUnfinishedObligations`（同切片名继任者被 `already mounted` 拒绝，结清后名字才可过户且继任者卸载返回 nil）。
- `assembly/plugin_catalog_test.go`：`TestDisabledBuiltinHandsOverItsObligations`（`-age-fold` 禁用非空洞：`!carriesInstance(disabled, isAgeFold)` 且 `!carriesInstance(disabled, isCompressionConsumer)`；随后真 cut 仍陈述该消费方义务，`unmount()` 拒绝并点名 `1 unfinished obligation(s)`，名字保留）。
- `assembly/hotupdate_generation_test.go`：`TestGenerationSwitchKeepsTheSameStateAuthority`（**3 次** `host.Reload`，逐代校验：serving generation 每次恰 +1、切片名保留、切换后新记录项由该次切点的 cut 陈述、disposer 拒绝的计数随代增长）。
- `assembly/checkpoint_test.go`：`TestPendingSliceIsRefusedByName`（boot 无 holder 时具名拒绝；注册表仍点名切片与实测教训：新宿主 `checkpointOwnerLocked()` 由 `r.replays` 导出，无已提交链时 `Checkpoint` 返回 `("", false, nil)`，故夹具须先提交一轮）。

**非空洞证明（临时变异，已复原）**：变异 A（删掉拒绝分支）→ `runtime/state/checkpoint_test.go:441 the unmount of a slice owing 2 obligation(s) answered nil, want it refused by name` ＋ `assembly/checkpoint_test.go:3783 the unmount of a slice stating an open obligation answered nil`；变异 B（先删再报）→ `checkpoint_test.go:448 the registry holds [] after the refused unmount, want the very holder that owes the work` ＋ `checkpoint_test.go:520 mounting a second holder of the kept name answered <nil>`。复原后 `diff -q /tmp/ckpt-pristine.go runtime/state/checkpoint.go` 输出 `RESTORED-IDENTICAL`，三枚 state 锚复绿。

**复跑凭据（独立语句，未与任何 git 写串联）**：
- `go build ./...` → exit 0；`go vet ./runtime/state ./assembly` → exit 0。
- `go test ./assembly -run '^TestUnmountKeepsTheOwedItemsOfItsSlice$' -race -count=10 -v` → 10×`--- PASS`，`ok … 2.436s`。
- `go test ./runtime/state -run '^(TestSliceHandoverKeepsTheUnfinishedObligations|TestUnregisterAllowsASettledSlice|TestUnregisterRefusesASliceWithUnfinishedWork)$' -race -count=10` → `ok … 1.245s`（`-v` 下 60 行 RUN/PASS）。
- `go test ./assembly -run '^(TestDisabledBuiltinHandsOverItsObligations|TestGenerationSwitchKeepsTheSameStateAuthority|TestPendingSliceIsRefusedByName)$' -race -count=10 -v` → `ok … 5.490s`。
- 消费者锚：`go test ./assembly -run '^(TestPluginDeclarationDisablesABuiltinPlugin|TestPluginDeclarationRefusesAnUnregisteredName|TestBootAdoptNewConsumers)$' -count=1 -v` → 3×PASS；`python3 scripts/gates/check_plugin_mounting.py` → `ok`（`mounts=0 consumers=1 registry_impl=2 present=2 errors=0`）；`go run ./scripts/codetools package-layout . examples/cli-host examples/wechat-bot` → `815 file(s); names, skeleton pins and test mirrors hold`，exit 0。
- 全量：`go test ./runtime/state ./assembly -count=1` → `ok runtime/state 0.552s`、`ok assembly 317.585s`，exit 0（上一轮 310.897s 同绿；boot 成本墙钟锚两轮均未误红）。

**写面申报（含一处白名单外强制传播）**：`runtime/state/checkpoint.go`（disposer 返回值与拒绝分支、`unfinishedWork` 面）、`runtime/state/checkpoint_test.go`、`assembly/checkpoint_test.go`、`assembly/plugin_catalog_test.go`、`assembly/hotupdate_generation_test.go`、本文件。**`assembly/checkpoint.go`（白名单未列，计划缺口）**：`Run.RegisterStateSlice` 的返回类型被 disposer 新签名机械带动为 `(func() error, error)`，仅 2 行（签名＋文档），其全部调用点本就用 `_` 丢弃 disposer，无行为变化。`runtime/state/genregistry.go`、`runtime/state/publish_test.go`、`assembly/plugin_catalog.go`、`assembly/run.go` 经核**无需改动**（切片不随代重建，故 ③ 的同权威是既有行为，本叶只补锚）。

**未验证面（如实登记，不作完成）**：12.R2.4 成本量化仍未做（非本叶）；`assembly/host.go` 治理接线处的生产卸载路径未接（本叶白名单禁改，disposer 的生产调用方仍为零，消费者锚只能经 `RegisterStateSlice` 与 boot 核可达性）；独立只读审者放行（R35）未做；整域全矩阵复跑与 F 任务未做。

---

## 12.R2.4 执行记录（2026-11-18 · openspec-apply 实施，本叶只勾 12.R2.4；不勾 12.R2 父伞、不勾 12.R1、不动 12.R3、不 archive、不宣告整域完成）

**具名更正（读面事实，非验收面缩水）**：任务行与 design 表引用的 `newestSurvivorSeq（assembly/run.go:3033-3044）` 在当前树**已不存在**——同一读由 `survivorPositions（assembly/run.go:3083-3100）` 承担，锚分支在 `:2784`（锚幸存者）与 `:2790`（恢复的 checkpoint 幸存者）各调它一次，checkpoint 恢复侧在 `restoreSurvivors（:2904-2911）` 内联同一读取。三处都**逐幸存者点读之后**才知道边界值/区间是否为空，「空区间≠零额外读取」的事实成立，故只更正符号名与行号，判据不变。

**新增锚（本域冻结名，本次建立）**：`TestCheckpointRefusalCostAcrossSessions`（`assembly/checkpoint_test.go`，三臂＋1 子测）。同条件三臂各用独立 localfile 耐久根，提交序列逐字相同（三会话各早 1 轮、被决定事实 1 条、各后 2 轮＝25 条），先各落一次 cut，再对三会话各试 `rounds=3` 次 `CheckpointSession`：`no-refusal`（消费者接受该事实）、`transient-refusal`（拒后 `allow()`＋同身份重提交）、`standing-refusal`（拒到底）。

**三臂读数（`-count=10` 十轮逐字相同，仅 `blockedFor` 与分配量浮动；日志行 `checkpoint_test.go:4496`）**：

| 臂 | 尝试 | 落地 | 拒 cut | 受阻会话 | 受阻时长 | 事件数 | 恢复折叠 | 存储值读 `ValueReads` | 常驻索引 | 走遍 visit / 值读 / 字节 | 分配 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 无错 | 3 | 3 | 0 | 0 | — | 44 | 36 | **92** | 0 | **4 / 4 / 273** | ≈1.80MB |
| 瞬时错后同身份成功 | 12 | 3 | 9 | **3** | 14–17ms | 44 | 36 | **92** | 0 | **4 / 4 / 273** | ≈1.80MB |
| 长期错 | 9 | 0 | 9 | **3** | 至臂末 | 40 | 37 | **269** | 0 | **93 / 93 / 8212** | ≈2.37MB |

- **拒 cut 的传播面**：三臂读数证明拒的不是「该事实的链」而是**整个账户**——`session-refusal-bystander` 与 `session-refusal-third` 两条与拒绝事实无关的链，`9/9` 次 cut 全被拒（`blocked=3`），与 `runtime/state/checkpoint.go:282-303`「框架不猜链」一致。这是**全局拒绝**的保守选择，不是乙案不变量：不变量只是「未应用事实不得被成功 cut 覆盖」。
- **阻塞代价落在恢复、不在 cut**：长期错臂的链**更短**（40 对 44：三次会话 cut 与关闭 cut 共 4 条从没落链），但同一根上的 boot 读得更多——存储值读 92→269（2.9×）、提交序走遍 4→93、走遍值字节 273→8212，折叠 36→37。即：一次长期拒 cut 不花 cut 的开销，而是把整段阻塞窗口留给下一次 boot 重放。
- **瞬时错等于零残留**：同身份再次应用后，三会话 cut 全部落地，boot 读数与无错臂**逐项相同**（92 / 4 / 4 / 273 / 36）⇒ 乙案「失败即具名拒绝、同身份成功即解除」的闭环在这条路径上不留额外成本。
- **cut 被拒本身零存储读**：`unappliedCutRefusal()` 在问 holder 之前就短路（`checkpoint.go:333-338`、`:370-375`），故 9 次拒绝没有一次落事实、没有一次存储读；热层（常驻索引 `IndexEntriesRead`）三臂均为 0——座位键已陈述索引，不枚举条目。

**边界读刻度（子测 `boundary_reads`，十轮逐字相同）**：稳定态（cut 落在日志末端）下，`survivors=3`、`coldReads=9`、`warmReads=0`、边界 `newest=8`（＝最新呈现事实的 seq）、无记录键 `absentReads=0`、`history=10`、boot `ValueReads=36`、折叠 `decoded=8`。
- **冷视界每幸存者 3 次存储值读**（索引条目 `partitionPositionLocked` `segment_store.go:542`＋window meta `publishedPosition` `:560`＋正文 `storedAtLocked` `:937`→`readBodyLocked` `:962`）：3 名幸存者＝9 次；这是 `survivorPositions` 出边界的价钱，而该函数**只要 seq**，正文那次读不是它需要的。
- **区间为空仍付这笔钱**：同一 boot 折叠恰为 `len(presented)=8`（＝幸存者自身），边界以上无一条事实被折（cut 在日志末端），边界读照付 ⇒ 「空区间≠零额外读取」实测成立；`design.md §十八·3`「稳态该区间为零额外成本」须收窄为「**零额外折叠**，边界仍需每幸存者点读」，不得再作「稳态零额外成本」。
- **热层把重复读变零**：同一调用在 boot 自己的视界上 `warmReads=0`（常驻集已持有这些条目），冷视界才付 9 次 ⇒ 热层增长买入的正是这笔钱。
- **对照臂**：`positions` 三名全中、`newest` 等于最新呈现事实的 seq（`seqOfFact`），故 9 次读是 store 真答出来的；对链上从未存在的键同一函数答 `2 名→0 名、newest=0` 且 `absentReads=0`，即成本随**载荷点名的幸存者**走，幻键不付费。

**复跑凭据（独立语句，未与任何 git 写串联）**：
- `go test ./assembly/ -run '^TestCheckpointRefusalCostAcrossSessions$' -count=10 -v` → exit 0，10×`--- PASS`（含 10×子测 PASS），`ok … 10.065s`；十轮 `boundary:` 行逐字相同、三臂计数面逐字相同。
- `go test ./assembly/ -run '^(TestDefaultBootRebuildsFromTheAnchor|TestSymptomAnchorSessionBootCostFlatAcrossGenerations)$' -count=10 -v` → exit 0，10×PASS ＋ 10×PASS，`ok … 39.984s`（本轮单独跑，未与全量并发，故无 R270 所述墙钟噪声）。
- `go test ./runtime/state/ -run '^TestSessionCutRefusesOnAnUnappliedFactWhateverChainItNames$' -race -count=10 -v` → exit 0，10×PASS，`0 DATA RACE`，`ok … 1.268s`（量成本未跳过失败事实、未以 TTL 删义务换读数）。
- `go build ./...` → exit 0；`go vet ./runtime/state/ ./runtime/task/ ./assembly/` → exit 0。

**新锚可红（变异→红→复原，变异只在 `/tmp` 副本树做，主树零 `.go` 写）**：
- 变异 A（把拒 cut 改成放行）：`runtime/state/checkpoint.go` 的 `unappliedCutRefusal()` 末尾改 `return nil` ⇒ 红在 `checkpoint_test.go:4384`：`the transient-refusal arm files the cut of {agent-a session-refusal-named} = ("checkpoint:16:40", true), want it refused while a fact stands unapplied`。
- 变异 B（把边界读缝跳过）：`assembly/run.go` 的 `survivorPositions` 在 `len(survivors) > 0` 时早返回空表 ⇒ 红在 `checkpoint_test.go:4574`：`the boundary named 0 survivor(s) of the 3 the payload states, so the store does not hold the chain these reads are counted over`。
- 复原：副本树两文件由备份回拷后与主树 sha256 逐字节相同（`checkpoint.go 6b319dde…`、`run.go b518c844…`、`checkpoint_test.go d3c753d0…`），副本树复跑 `-count=1` 绿；主树 `git diff --stat HEAD` 对两产品文件仍为 `run.go 223`、`checkpoint.go 42`（与本叶开工时一致 ⇒ 本叶零生产写）。副本树与备份随后删除。

**是否揭示需改生产的成本缺陷**：**未揭示到需另冻结写面的缺陷**。边界读是 `O(载荷幸存者数)` 的点读（每名 3 次），不是 `O(历史)` 扫描，故不触发停线条件；可选的窄优化（一次只答 seq 的读，省掉每名 1 次正文读，并把锚分支两次调用合一次）**留给编排者裁定**，本叶不自行改 `run.go`。全局拒 cut 的代价面已量化为「下一次 boot 重放阻塞窗口」，是否细分（按链/按 holder）同样归编排者终裁——本叶不宣称唯一设计。

**未验证面（如实登记，不作完成）**：①会话数只有 3 条（1 条被点名＋2 条无关），未扫更大规模下「受阻会话数 vs 阻塞时间」曲线；②阻塞时长是墙钟读数（14–17ms），**不作断言**（只作日志），计数面才作断言；③分配量（`TotalAlloc` 增量，≈1.80MB vs ≈2.37MB）只作读数、不作断言，因其随 GC 自带噪声；④`rounds` 固定 3 轮，未测「阻塞多轮后解除」的折旧曲线；⑤`loop.go:373`「送达事实写失败是否阻断投递」的语义分叉**不属本叶**，未触碰、未裁定；⑥独立只读审者放行（R35）未做，整域全矩阵复跑与 F 任务未做。

## 12.R2.5 执行记录（2026-11-18，实施叶；方向＝R287 裁定乙案，非本叶自裁）

**落地 hunk（三处生产＋一枚新锚）**
- `runtime/agent/loop.go`：`Backflow` 签名 `void→error`，终态分支 `return commitErr`（progress 与无结果种类 `return nil`）；拒收上报新增 `consumerError`（ERROR 级，bypass 三条红线照搬 `consumerWarn`）。`in.deliver` 位置与无条件性未动。
- `assembly/facts.go`：`sink` 在 `closeFinished()` 之后新增 `if refusal := r.loop.Backflow(frame.Identity.InvocationID, notice); refusal != nil { return r.retainNoticeOwed(ctx, frame, notice, seq) }`；`retainNoticeOwed` 实现零改动，doc＋debug 文案由「close 之后」泛化为两成因。无其他 hunk。
- `runtime/agent/skeleton_assert.go:135`：签名断言跟随（白名单外、编译强制）。
- 新负锚落 `assembly/host_test.go:481`（＋401 行含 helper；R288 勘误：原记名 `assembly/delivery_fact_obligation_test.go` 该文件不存在；落点理由见 design §二十一·3）。

**Backflow 调用方级联枚举**（`grep -rn '\.Backflow('`，全仓 25 处）：生产 2 处——`assembly/facts.go:1002`（已改）、`runtime/agent/skeleton_assert.go:135`（已改）；其余 23 处为测试与 `scripts/` 文本（`runtime/agent/{loop,consumer}_test.go`、`tests/integration/{acceptance,execution,learning}/**`、`scripts/codetools/event_ownership_test.go`、`scripts/gates/check_event_integrity_mutations.py` 文本锚）。Go 测试全部以表达式语句调用，返回值可合法丢弃，无需改动（`go vet` 全绿佐证）。`scripts/gates` 文本锚的漂移见下方未决项，本叶未改。

**逐条命令与原始退出码**（独立语句，未与任何 git 写串联）
- `go build ./...` → exit 0。
- `go vet ./runtime/agent ./runtime/task ./assembly` → exit 0（含测试编译面）。
- `gofmt -l runtime/agent/loop.go runtime/agent/skeleton_assert.go assembly/facts.go assembly/host_test.go` → 空。
- `go test ./assembly/ -run '^TestDeliveryFactFailurePreservesTheNoticeObligation$' -race -count=10 -v` → exit 0，10×`--- PASS`，`ok … 22.860s`。
- `go test ./assembly/ -run '^TestRedeliveredNoticeAfterRestartSettlesOnce$' -race -count=10` → exit 0，`ok … 156.755s`（真实入口锚，写侧修复后跨进程至多一条耐久收据仍绿）。
- `go test ./runtime/task/ -run '^(TestConsumptionFailureKeepsTheNoticeOwed|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10` → exit 0，`ok … 1.258s`。
- `go test ./runtime/agent/ -run '^(TestBackflowNotificationLanes|TestBackflowWakesSelfRegisteredResidents)$' -count=1 -v` → exit 0，`--- PASS` 8 行（2 用例＋6 子用例），签名变更后仍绿。
- `go test ./assembly -count=1` →（全包回归，见下条结果行）

**新锚可红（变异→红→复原，主树内改后即回拷，sha256 逐字节复核）**
- 变异 A（回到修复前形态：sink 无视拒收照写收据）⇒ `assembly/facts.go` 复原前 `sha256 9528d2a2cd42d82bce2ba9875d890b32a1c04c6e515e1b625462cb61f5d338e4`，跑 `-count=1` **红**于 `assembly/host_test.go:525`：`waited 10s for the refused notification to stand owed on the mailbox, want it landed`（座位被 Consume，义务未保留）。
- 变异 B（拒收分支直接 `return nil`，绕过 `retainNoticeOwed`）⇒ 同一行 **红**，同一条失败信息（claim 未还回池，座位不落 `ClaimPending`）。
- 复原：两轮变异后回拷 `/tmp/facts.go.orig`，`shasum -a 256 assembly/facts.go` 两次均回到 `9528d2a2…338e4`；复原后 `-race -count=10` 复跑绿（上条 22.860s）。

**可反转探针核验（R287 所立三条，逐条答）**
1. 破 `closeFinished` 语义？**未破**：close 分支在前、判据未改，`retainNoticeOwed` 实现未改；`TestRedeliveredNoticeAfterRestartSettlesOnce` 与全包回归绿。
2. 致 double-Release？**未致**：拒收路径的 claim 来自同一 `bookNotice`（`facts.go:992`），Release 恰一次；`ErrClaimSettled` 仍是既有容差。
3. 重启重投产出第二条标记（非幂等）？**在标记确未落的窗口里不产生**：`deliver` 的 `settleDeliveryKey` 按 key 幂等（`assembly/tools.go:146-150`），`driveSeat` 读该键；实测重启后恰一条 `TypeTaskSettled`＋一条收据，二次要约零第二条。**保留一条既有风险**：`commitSettled` 每提交铸造新身份（`facts.go:88-90`），故「先落标记、后写收据之间崩溃」窗口今天仍会追加第二条 `TypeTaskSettled`——该窗口属丙案证伪时点名的既有窗口，不是乙案验收面，本叶未修（design §二十一·1/·3）。

**未验证面（如实登记，不作完成）**
- 变异门 `MUT-CAND-01.2-handover-confirm` 锚漂移已成红门（design §二十一·4），本叶写面禁改 `scripts/gates`，需编排者裁「重指 old 串」；本叶**未跑** `check_event_integrity_mutations.py`（隔离树重跑成本高，锚命中数用等价 Python 计数实证＝0）。
- `docs/EXTENSIONS.md:68` 与 `docs/api/...runtime__agent.md:1348` 的签名文档未随改（写面外）。
- `TestDeliveryFactFailurePreservesTheNoticeObligation` 用的是既有 `snapshotTestModel`/`snapshotTestGovernance` 替身（与 `assembly` 包其它锚同源），跨进程半段以 `deliver`＋`driveSettleTurns` 组合真实面驱动，未从零自建宿主进程；R35 独立只读审未做。

## 12.R2.6 执行记录（2026-11-19，实施叶；根因＝`assembly/facts.go` 折叠守卫，归属本域）

**二分（`/tmp/fuse-head`＝`git archive HEAD`，仅叠文件，零 git 写仓库）**
- HEAD 树原样：`go test ./tests/integration/execution -run '^(TestResponseCallCapFuse|TestStormPropagationFuse)$' -count=1` → exit 0，`ok … 9.529s`。
- 全量叠当前候选 19 个非测试 `.go`：红，`chain increment = 1 actions beside 191 result facts`（cap）／`460 result facts`（storm），逐次读数不同（另一次 184／403 见叶行）。
- hunk 回卷（`facts.go` 被 `checkpoint.go` 的 disposer 签名与 `compress_consumer.go` 的 `chainHoldsFact` 依赖，故按 hunk 而非文件回卷）：删 `foldMapped` 的 `r.newestOfChainLocked(replayAddressOf(evt)) != evt.Key` 早返回（及其 `newestOfChainLocked`）⇒ exit 0，`ok … 9.541s`。引入 hunk 定位完成。
- 归因实测（守卫处临时 stderr 计数，只在 `/tmp` 副本树）：单次 storm 子用例守卫丢弃 **50** 次，逐条 `resident=true`（正是本次提交登记的事实），`newest` 为同链并发已落地的另一事实。

**落地 hunk（三处生产，无新表）**
- `assembly/facts.go`：删守卫＋`newestOfChainLocked`；`seen`／`carried` 两重试分支改走新增 `reduceCommittedRepeat`（→`states.ReduceRepeat`）；新增 `stateRegistry()` 收敛两处取 `r.states`；doc 三处改真话（`commitFactNoCadence` repeat 条目、`foldMapped` boundary 条目）。
- `assembly/checkpoint.go`：`projectionStateSlice.ReduceRepeat` 空实现（序列按提交顺序陈述身份一次，重复提交不陈述位置）。
- `runtime/state/checkpoint.go`：`Reduce`／新增 `ReduceRepeat` 共用 `fold(ctx, evt, repeat)`；声明面 `repeatReducer`；`reduceInto` 增 `repeat` 参数，未声明该面的 holder 照旧走 `Reduce`（缝「按身份幂等」契约不变）。

**逐条命令与原始退出码**（独立语句，未与任何 git 写串联）
- `go build ./...` → exit 0；`gofmt -l assembly/facts.go assembly/checkpoint.go runtime/state/checkpoint.go` → 空。
- 正锚：`go test ./tests/integration/execution -run '^(TestResponseCallCapFuse|TestStormPropagationFuse)$' -count=1` → exit 0，`ok … 9.199s`；重复 10 次 `pass=10 fail=0`。
- 正锚（全包）：`go test ./tests/integration/execution -count=1` → exit 0，`ok … 45.043s`（未出现并行抖动，无需独占复跑）。
- 消费者锚：`go test ./assembly -count=1` → exit 0，`ok … 369.954s`（前台一次＋后台一次均 exit 0）。
- 根因相关域锚：`go test ./runtime/state -count=1` → exit 0，`ok … 0.609s`；`go test ./assembly -run '^(TestDeliveryFactFailurePreservesTheNoticeObligation|TestRedeliveredNoticeAfterRestartSettlesOnce)$' -race -count=10` → exit 0，`ok … 178.193s`；`go test ./runtime/task -run '^(TestConsumptionFailureKeepsTheNoticeOwed|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10` → exit 0，`ok … 1.451s`；`go test ./tests/integration/execution -run '^(TestResponseCallCapFuse|TestStormPropagationFuse)$' -race -count=5` → exit 0，`ok … 58.857s`。
- 相邻门：`bash scripts/lint.sh` 的 gofmt／modules／`go build ./...`／`go vet ./...` 四段绿，`comment_policy` ratchet 口径 `0 finding(s); 0 beyond baseline; 0 ratchet slot(s) can be lowered`；`./comment_policy -no-baseline assembly runtime/state` → `0 finding(s) over [assembly runtime/state]`；`check_doc_budget` → `RESULT: ok`。`python3 -m unittest -q scripts/gates/test_check_event_integrity_mutations.py` → `Ran 60 tests … OK`；`test_check_runtime_evidence.py test_check_dod_coverage.py` → `Ran 126 tests … OK`。

**新锚可红（变异→红→复原，主树改后即回拷 `/tmp/fix-checkpoint.go.bak`）**
- 复原根因（把 `projectionStateSlice.ReduceRepeat` 改成 `return s.Reduce(ctx, evt)`，即重复事实按提交折叠）⇒ **红**：`checkpoint_test.go:3161 the retry of the folded output … moved the completed range from (agent-a122300, open=false) to (agent-a120300, open=false)`、`:3186` 同形（restart 面）。回拷后 `-count=1` 绿。
- 复原顺序守卫（`/tmp` 副本树）⇒ fuse 复红，读数见上（191／460）。两向变异各自打红不同面 ⇒ 修复不是凑绿：类别面钉住重试语义，顺序面去掉才使合法并发提交回到序列。

**未验证面（如实登记，不作完成）**
- `scripts/gates/test_check_runtime_symptoms.py::test_registry_matches_the_module` 为**候选树既有红**，与本草无关（`check_event_semantics_symptoms.SYMPTOM_PARENTS` 少列 `TestLocalSmokeSymptomBootDeployed`，GitHub Actions 症状门脚本不在本叶写面）：`bash scripts/lint.sh` 因它整体 exit 1，其余各段均绿；待编排者裁「补注册还是退役该锚」。
- 未跑全仓 `go test ./...`（超出叶级验收面）；`docs/EXTENSIONS.md` 等散文面未随改（写面外）。

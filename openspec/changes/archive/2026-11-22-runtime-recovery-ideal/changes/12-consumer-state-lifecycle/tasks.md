# A 提交—应用—义务 · 六个新叶

> 只按 [ORCHESTRATION](../../ORCHESTRATION.md) 派发；本轮只写计划，以下命令均未执行。旧勾选不继承，六叶全部未完成。
> 根目录：`/Users/pengweiye/Documents/codes/tagent`。下列文件均相对此根；候选清单不是授权全白名单，实施前按 [design D4](design.md#d4-方案比较与实施签发) 签出每文件理由、拥有者和完整写面。
> 本轮 Grep 已核：下列标“现有”的测试函数存在；标“拟新增，未建立未执行”的函数无命中。先读断言再复用，名称不代表已覆盖真实路径。

## 验证约定

正式验收要求每枚锚实际 RUN 且有终局、原始 exit 与同候选绑定；组合选择器少一个也不通过。`-count=10` 预期每名执行 10 次，子场景全部执行，race 不代替业务断言。命令不接管道掩盖退出码。
12.101 是反证阶段，可主题红、证伪或未证；零匹配、编译失败、夹具早退不算反证，不要求所有猜想红。后续正式验收不可用零匹配或未证关闭目标。
现有锚中的拒 cut/先投策略断言是比较输入；若设计改选，只能按已保留行为更新断言并留对照，不能删锚、改名或宽断言追认绿。测试注入优先既有存储/模型替身和镜像文件，不给产品加测试后门。

## 12.101 取证

- [x] 12.101 建立真实时序/失败切点最小反证；产出逐疑点“证实/证伪/未证”、非空前提、用户可见差异、原始时序与失败证据。验证：以下正/负/消费者锚及切点表。（2026-11-21 完成：七探针全部建立，H01/H02/H02b等价案/H03a裁决腿/H03b/H05 六项证实，H02b冲突案/H04坏字节案证伪，attemptOf 解码分支未证；控制绿；编排者复跑 H01 红核签；D7 记录 200 行，六缺陷移交 12.102。）
- **阶段与可派状态**：取证；未执行、blocked；入口核定后仅派最小探针，不派产品修复。
- **前置**：13.101。
- **精确候选文件（仅测试探针）**：`assembly/facts_test.go`、`assembly/checkpoint_test.go`、`assembly/host_test.go`、`assembly/settle_turn_test.go`、`runtime/state/durable_test.go`、`runtime/task/unsettled_test.go`、`runtime/task/approval/approval_store_test.go`、`features/evolution/governance/checkpoint_test.go`；记录落本 `design.md`/`tasks.md`。生产只读，以 .103/.104 候选及 design D2 的调用链为证据面。
- **取证顺序**：先读声明/结构清单，再跟提交→落盘→应用→快照→读取→恢复→义务；每个故障点有正常控制。确定性交错用既有可阻塞依赖，不靠随机 sleep。
- **正锚（现有）**：`TestFactCommitLandsOriginalAndRecordInOneCommit`、`TestRetryOfFoldedOutputDoesNotReopenTheRound`；前者是 persist 组件控制，后者经 CommitToSession，不冒称已证明并发。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestFactCommitLandsOriginalAndRecordInOneCommit|TestRetryOfFoldedOutputDoesNotReopenTheRound)$' -count=1 -v`；预期 2 名各 1 次、控制绿。
- **负锚（拟新增，未建立未执行）**：`TestTaskSnapshotReplaysInterleavedCommit`、`TestConcurrentCommitPreservesApplicationOrder`、`TestConcurrentSameFactKeepsOneApplication`；分别落 `checkpoint_test.go`、`facts_test.go`、`facts_test.go`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit|TestConcurrentCommitPreservesApplicationOrder|TestConcurrentSameFactKeepsOneApplication)$' -race -count=10 -v`；建立后预期每名 10 次，错误若成立落在漏任务/顺序/重复应用，不要求全红。
- **负锚（拟新增，未建立未执行）**：`TestApprovalWriteFailureDoesNotGrantPermission` 落 `runtime/task/approval/approval_store_test.go`，`TestGoalWriteFailureDoesNotChangeEffectiveState` 落 `features/evolution/governance/checkpoint_test.go`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./runtime/task/approval -run '^TestApprovalWriteFailureDoesNotGrantPermission$' -count=10 -v`；`go -C /Users/pengweiye/Documents/codes/tagent test ./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -count=10 -v`；建立后各 10 次，核写/屏障失败前后真实许可、目标有效态与重试。
- **真实消费者锚（拟新增，未建立未执行）**：`TestUnreadableReceiptDoesNotDriveAgain`、`TestBackflowFailureRecoversWithoutManualSettle`，均落 `assembly/host_test.go`，复用存储夹具，走原生工具/Boot/生产恢复泵。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestUnreadableReceiptDoesNotDriveAgain|TestBackflowFailureRecoversWithoutManualSettle)$' -race -count=10 -v`；建立后每名 10 次；不手补 settle 事实、不手调 deliver，分别核拒读不重驱、义务自动续做。
- **失败切点表**：快照已读/未落盘插新任务和旧任务推进；A 落盘后暂停让 B 应用；同键双预查及异内容；attention/审批/目标每个写与屏障；consume/reject/attempt 拒读或坏字节；Backflow 标记失败、交接后/收据前停止。各行记录真实可达性和未覆盖入口。
- **停止条件**：缺 13.101 身份/资源、探针需新生产 hook、控制不成立或跨写面即停；证伪单项可关闭怀疑，未证保留给设计门，不凭日志声称产品已修。严禁重新立“前 holder 跳过 projection”已驳回前提。
- **取证记录（12.101 执行结果）**：见 [design.md D7](design.md#d7--12101-取证记录)——逐疑点「证实/证伪/未证」、非空前提、用户可见差异、七枚探针的命令与原始终局、失败切点表逐行可达性、C8 行为身份对齐与未覆盖面。本行只是记录指针，不勾选本任务框；12.101 是否完成由编排者按 D7 凭据核。

## 12.102 设计门

- [x] 12.102 比较提交状态与义务方案并冻结实施签发；产出保留/最小修/删并三案、全局复杂度、全部消费者及完整 D4。验证：三锚证据、负变异敏感性设计、独立设计审。（2026-11-21 完成：D4 16 行全填＋D8 八节；独立设计审确认六案与 H06 证据链成立，3 阻断（消费者计数 15、delivery_fact_test.go 写面、scoped ④ 立场）已修；A/B 共享语义经编排者会签为 D8.7.1 单一记录，B 行5④ 解锁。）
- **阶段与可派状态**：设计；未执行、blocked；不是编码单，未填字段不视为签发。
- **前置**：12.101、06.101、13.102。
- **精确候选文件（计划）**：本 `proposal.md`、`design.md`、`tasks.md`、`specs/session-checkpoint-material/spec.md`、`specs/consumer-loop-convergence/spec.md`、`specs/runtime-resource-ownership/spec.md`、`specs/config-hot-reload/spec.md`；生产/测试只读。
- **比较内容**：提交分类和应用责任、快照真实截止、未应用重试、三族收据读错、审批/目标失败可见态、通知双义务与恢复泵；不预选队列/字段/Repeat 去留/全局拒 cut/Backflow 先后。
- **结构核销**：design D2 每项写独立需求及删除反事实；删并后沿全部调用方检查伴生状态和参数，不把单用接口包装成公共抽象。
- **正锚（现有，复核取证记录）**：`TestRetryOfFoldedOutputDoesNotReopenTheRound`、`TestConsumerBehindTheChainSurvivesARestart`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestRetryOfFoldedOutputDoesNotReopenTheRound|TestConsumerBehindTheChainSurvivesARestart)$' -count=1 -v`；预期各 1 次，保留旧输出不重开/被拒事实仍可恢复，不由策略断言预选方案。
- **负锚（拟新增，未建立未执行；由 12.101 建立）**：`TestConcurrentCommitPreservesApplicationOrder`、`TestTaskSnapshotReplaysInterleavedCommit`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestConcurrentCommitPreservesApplicationOrder|TestTaskSnapshotReplaysInterleavedCommit)$' -race -count=10 -v`；预期各 10 次取证，设计解释每个主题红/反证，不要求设计阶段已修绿。
- **真实消费者锚（拟新增，未建立未执行；由 12.101 建立）**：`TestBackflowFailureRecoversWithoutManualSettle`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^TestBackflowFailureRecoversWithoutManualSettle$' -race -count=10 -v`；预期 10 次，方案逐个解释真实泵的事实与义务来源；设计者复核记录，不越权改测试。
- **签发内容**：逐项填 design D4，包括签名、持久格式、完整生产/测试/文档/门写面、全部消费者、三锚、负变异、同条件基线、回滚和独立审；A/B 共享语义由编排者单写，06.102 确认再实施。
- **停止条件**：真实目标承接缺失、三案未同尺比较、消费者未枚举或独立设计审有阻断即停；没有证据不能删除防线，也不能以保守为名永久保留未解释的结构。
- **设计门记录（12.102 执行结果）**：见 [design.md D8](design.md#d8--12102-方案比较记录)——六案逐案三案同尺（R/M/D＋备选手段）、H06 三态与影响域反证、Repeat 双面裁定、D7 两个未证面的实施期取证裁定、全局复杂度净账、逐叶三锚与负变异矩阵、共享语义所需事实与候选清单、越界自查。签发区已填 16 行（[D4](design.md#d4-方案比较与实施签发)，含 [D4.1 内容指纹](design.md#d4-方案比较与实施签发) 与 [D4.2 接口/调用方](design.md#d4-方案比较与实施签发)）。本行只是记录指针，**不勾选**本任务框；12.102 是否完成由编排者按独立设计审结论核。

## 12.103 提交/应用/快照实施

- [x] 12.103 实施所选提交/应用/快照截止语义；产出同一身份/提交序对应的可观察状态与完整恢复，不在任务行预定字段或方法形状。验证：以下三锚与 D4 相邻回归。（2026-11-21 完成于三单：H02b 五值单点分派；H01 安静点标记键＋尾段改界（D8.9 重裁）；H02 landing 内有序应用点（锚编排按 D8.9 三断言更新）。编排者终验：H01/H02/H02b/成本锚 -race 绿，负锚＋消费者锚 60/60 绿；全包回归三处红精确归属 12.104 探针（H03a/H03b/H05），assembly 其余全绿；负变异证据在 D9/隔离副本。旧根缺标记＝全日志读经 `TestConsumerBehindTheChainSurvivesARestart` 等核验不漏项。）
- **阶段与可派状态**：实施；未执行、blocked；目前无签发记录。
- **前置**：12.102、06.102，且签发记录齐。
- **精确候选生产文件**：`assembly/facts.go`、`assembly/checkpoint.go`、`assembly/run.go`、`assembly/commit.go`、`assembly/task_checkpoint.go`、`runtime/state/checkpoint.go`、`runtime/state/durable.go`、`runtime/task/checkpoint.go`。
- **精确候选镜像测试**：`assembly/facts_test.go`、`assembly/checkpoint_test.go`、`assembly/run_test.go`、`assembly/commit_test.go`、`assembly/task_checkpoint_test.go`、`assembly/anchored_boot_parity_test.go`、`runtime/state/checkpoint_test.go`、`runtime/state/durable_test.go`、`runtime/task/checkpoint_test.go`；账目为本 `design.md`/`tasks.md`。签名牵连的其他文件先签发，不自动加入。
- **实施范围**：仅已选方案所需文件；提交结果未知不当失败重做或成功放行，重复只补未完应用，确定拒绝不发布部分；快照与任务尾使用已签发的同一覆盖语义。保留同一状态派生及原文原序，不恢复退役机制。
- **正锚（拟新增，未建立未执行；由 12.101 建立）**：`TestTaskSnapshotReplaysInterleavedCommit`、`TestConcurrentCommitPreservesApplicationOrder`、`TestConcurrentSameFactKeepsOneApplication`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit|TestConcurrentCommitPreservesApplicationOrder|TestConcurrentSameFactKeepsOneApplication)$' -race -count=10 -v`；预期每名 10 次全绿，核逐条序列而非仅最终键集合。
- **负锚（现有）**：`TestRefusedFactCommitPublishesNoKey`、`TestFactCommitJudgesTheDomainBeforeTheEngineBytes`、`TestRetryOfFoldedOutputDoesNotReopenTheRound`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestRefusedFactCommitPublishesNoKey|TestFactCommitJudgesTheDomainBeforeTheEngineBytes|TestRetryOfFoldedOutputDoesNotReopenTheRound)$' -race -count=10 -v`；预期每名 10 次；D4 对应负变异分别破坏原子性、领域等价和旧身份重试时应主题失败。
- **真实消费者锚（现有）**：`TestConsumerBehindTheChainSurvivesARestart`、`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`、`TestBootSeatsTheMailboxAfterTheDurableAccount`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestConsumerBehindTheChainSurvivesARestart|TestRestartRetryOfFoldedOutputKeepsThePresentedRange|TestBootSeatsTheMailboxAfterTheDurableAccount)$' -race -count=10 -v`；预期每名 10 次、正式 Assemble/恢复入口有非空义务和呈现断言。
- **关联核对**：同键/异键、同/跨分区的返回分类；全局/会话 cut；经真实 Boot/Assemble 触发无可用锚、有效锚、允许降级三类生产分支，对同事实合流（前提和非空观测见 design D3）；`seedFullReplay` 仅作算法参考，不新增生产 hook/API 凑入口；配置提交消费者；存储实现红回 B 同候选修复，不以只读目录为由跳过完成谓词。
- **停止条件**：D4 未齐、接口/格式偏离、需要新增队列/游标/公共面、出现未签文件或行为缩减即回设计门；三锚或相邻消费者红不放行。
- **执行记录（12.103 执行结果）**：见 [design.md D9](design.md#d9--12103-执行记录)——H02b 提交结局单点已实施且正/负/消费者三面转绿；H01（尾段以陈述 offset 为界）与 H02（单一有序应用点）**中止并回退**，仓库内只留 H02b 一组 hunk（`assembly/checkpoint.go`、`run.go` 已与 D4.1 基线指纹逐字符相同）。两条中止各有独立证据：H01 因任务键不在 cut 轴、按位置取界必然把已陈述任务带进尾段并击穿 `TestTaskRestoreBoundedByCheckpoint` 的成本契约；H02 因 `TestConcurrentCommitPreservesApplicationOrder` 的 `release()` 序（第二条提交返回后才放行第一条的 fold）使"fold 入有序单元"必死锁（`panic: test timed out`，栈证据见 D9.3）。本行只是记录指针，**不勾选**本任务框；12.103 是否完成、以及 H01/H02 的重新签发，由编排者按 D9 待裁决项回 12.102（H01 另需 06.102 会签）后核。
- **重开裁定（2026-11-21，设计门）**：H02／H01 两案的正式裁定已出，见 [design.md D8.9](design.md#d89--h01h02-重开裁定)——H02 取"更新锚编排、断言不变"（`TryLock` 机制断言＋落盘前读信号，禁用旧编排）；H01 取"安静点标记定界"（排水后、快照前落一条标记键，尾段＝其后的键；旧根缺键⇒读全日志），**不需 06.102 会签、不改 checkpoint 载荷**。D4 第 4/6/11/13/100 行、D4.2、D8.1 的 H01/H02 行、D8.5、D8.6 的 12.103 表与 D8.7.1 第 2/3 条已按裁定改写。**重开裁定已出，待续作**：本行不勾选任务框，12.103 的剩余实施（H01＋H02）、三锚转绿、四条对照与成本硬门归续作执行。
- **续作执行记录（2026-11-21，第二会话）**：见 [design.md D9.7](design.md#d97-续作h01-安静点标记h02-单一有序应用点2026-11-21第二执行会话)——H01 安静点标记与 H02 单一有序应用点**均已实施**：生产三处落点 `assembly/facts.go:91–180`（有序单元＋结局分派）、`assembly/facts.go:437`（`persistFactOrdered` 收为写步，判重读上提）、`assembly/checkpoint.go:346/400`（cut 在排水后、快照前落标记）与 `:903–1010`（标记族＋`tailFactKeys` 走停点），`assembly/run.go:303`（`landing` 契约注释）；测试 `assembly/facts_test.go`（H02 锚按 D8.9.2 三断言重排）、`assembly/checkpoint_test.go`（新增 `TestQuietMarkerBoundsTheTailToTheWindow` 三子场景）；`assembly/task_checkpoint_test.go` 逐字符未改。凭据：H01 正锚 10/10 绿、成本硬门 10/10 绿（读回 `chains=3 links=6 covered=0`、`tail=15`，对被证伪的 `tail=42`）、H02 正锚 10/10 绿（三断言）、H02b 回归 30/30 绿（`want=4` 前提保持）、负锚 30/30、消费者锚 30/30、成本硬门四枚 40/40；负变异两案在 `/tmp/rr-12-103-r2` 隔离副本确定性红且控制绿；`go build -buildvcs=false ./...` exit 0；相邻回归仅剩 12.104 的既有红（H05/H03a/H03b）。另有两项须回设计门：一是 `SnapshotState` 拒绝后残留的标记须由「标记所指 cut 已落盘」的配对检查退化为读全日志（D8.9 未列，D9.7.5-① 附理由与证据，可替换）；二是 `memory/ledger/CommittedIndexEntry` 因判词净删成为死生产者，`check_dead_producers.py` 转红，修在 B 写面（D9.7.5-②）。**本行不勾选**：12.103 是否完成由编排者按 D9.1＋D9.7 合并凭据核。

## 12.104 领域与义务失败闭合

- [x] 12.104 实施所选审批/目标/收据与通知义务失败闭合；产出失败后的许可、有效态、欠账和恢复处置一致。验证：写失败/拒读负锚、真实工具与恢复泵、治理宿主。（2026-11-21 完成于四单：H03a/H03b 耐久成功才可见；H04 读错具名；H05 单一身份＋单一渲染器＋泵内补齐＋拒写分支保 claim；孤儿结算经生产 `resultFactOf` 读回；超签审计（plugin_catalog 回退、mailbox 补签）；负变异五案全咬；末单编排者接手收尾。全包 0 FAIL。H05 生产装配接线移交 13.104（plugin_catalog.go:149），其取证改经 catalog 后完成。）
- **阶段与可派状态**：实施；未执行、blocked；沿 12.102 的完整 D4，只领取同根语义实际签发子集。
- **前置**：12.103。
- **精确候选生产文件（审批/目标）**：`runtime/task/approval/attention_store.go`、`runtime/task/approval/attention_log.go`、`runtime/task/approval/approval_store.go`、`features/evolution/governance/goal.go`、`features/evolution/governance/checkpoint.go`、`assembly/governance_checkpoint.go`。
- **精确候选生产文件（通知/收据）**：`runtime/state/durable.go`、`runtime/state/memory.go`、`runtime/state/mailbox.go`、`runtime/state/mailbox_checkpoint.go`、`runtime/task/wrapper.go`、`runtime/agent/loop.go`、`assembly/settle_turn.go`、`assembly/facts.go`、`assembly/tools.go`、`assembly/host_delivery_checkpoint.go`。
- **精确候选镜像测试**：`runtime/task/approval/attention_store_test.go`、`runtime/task/approval/attention_log_test.go`、`runtime/task/approval/approval_store_test.go`、`features/evolution/governance/checkpoint_test.go`、`assembly/governance_checkpoint_test.go`、`runtime/state/durable_test.go`、`runtime/state/mailbox_checkpoint_test.go`、`runtime/task/unsettled_test.go`、`runtime/agent/loop_test.go`、`assembly/settle_turn_test.go`、`assembly/facts_test.go`、`assembly/host_test.go`、`assembly/host_tools_test.go`、`assembly/host_delivery_checkpoint_test.go`；账目为本 `design.md`/`tasks.md`。
- **写面限制**：列出文件只表示可能涉及；每个实际 hunk 关联已证切点/所选设计。不能因共享包而全改；例如 Query/Count/List 若需改 `denial.go` 或工具 API，先补精确签发及 C 消费方承接。
- **正锚（现有）**：`TestConsumptionFailureKeepsTheNoticeOwed`、`TestSettlementFailureIsReportedBeforeDelivery`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./runtime/task -run '^(TestConsumptionFailureKeepsTheNoticeOwed|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10 -v`；预期每名 10 次，具名失败且欠账身份不丢。
- **负锚（拟新增，未建立未执行；由 12.101 建立）**：`TestApprovalWriteFailureDoesNotGrantPermission`、`TestGoalWriteFailureDoesNotChangeEffectiveState`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./runtime/task/approval -run '^TestApprovalWriteFailureDoesNotGrantPermission$' -race -count=10 -v`；`go -C /Users/pengweiye/Documents/codes/tagent test ./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -race -count=10 -v`；各 10 次全绿，分别破坏耐久成功前可见性时主题失败。
- **真实消费者锚（拟新增，未建立未执行；由 12.101 建立）**：`TestUnreadableReceiptDoesNotDriveAgain`、`TestBackflowFailureRecoversWithoutManualSettle`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestUnreadableReceiptDoesNotDriveAgain|TestBackflowFailureRecoversWithoutManualSettle)$' -race -count=10 -v`；每名 10 次；读失败具名阻止重驱，完整链自行补齐可恢复材料/收据，禁止手补另一 settle key。
- **真实治理消费者锚（现有）**：`TestGovernanceSliceWiredInProduction`、`TestGovernanceBootReadsSettledAnswerByIdentity`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestGovernanceSliceWiredInProduction|TestGovernanceBootReadsSettledAnswerByIdentity)$' -count=10 -v`；每名 10 次，Boot 后流序、原身份审批及真实 gate 的读取一致。
- **义务完成判据**：按 notice 身份核任务投递义务、mailbox claim、送达/配对事实、attempt、消费/拒绝收据；不以 nil、日志、投递次数代替完成，不承诺外部发送 exactly-once。
- **停止条件**：同根因不能证明、故障只在 helper 处理、真实泵需人工补事实、签名/格式/写面超签即停；Backflow 次序分叉回 12.102 比较，不由实施者沿旧裁定默认选。
- **执行记录（2026-11-21，收尾会话）**：见 [design.md D9.8](design.md#d98--12104-执行记录2026-11-21收尾会话)。实施落点：`runtime/task/approval/attention_store.go`（H03a 耐久先于入链）、`features/evolution/governance/goal.go`（H03b 先写链后改内存）、`runtime/state/{durable,memory}.go`（H04 读失败具名）、`runtime/state/{mailbox,mailbox_checkpoint}.go`（H04 恢复侧出口，超签审计判留并已在 D4 补签）、`runtime/task/delivery_fact.go`（新增 H05 唯一身份＋唯一渲染器）、`runtime/agent/loop.go`＋`assembly/tools.go`（两写入面收敛）、`assembly/settle_turn.go`（泵内补齐）；测试新增 `TestDeliveryFactOfIsOneFactForBothWriteFaces`、`TestUnreadableReceiptFailsTheRestoreByName`、`TestUnreadableReceiptDoesNotDriveAgain`、`TestBackflowFailureRecoversWithoutManualSettle`。凭据：三转绿锚 `-race -count=10` 各 10/10（H03a 子 40/40、H03b 子 40/40）、消费者锚 assembly 三枚各 10/10 与 runtime/task 两枚各 10/10、H04 两锚父 20/子 50、`go build ./...` exit 0、概念预算门由 58>57 转绿（内部化 `settleDriverOption`，new=57）；负变异五案在 `-overlay` 隔离副本确定性红（发布先于耐久、内存先发布、读错回 false、缺失事实走 release、恢复侧折回"未结算"）。**两处未闭合，本行不勾选**：一是 `assembly/plugin_catalog.go` 的 settle 驱动 commit 面接线按超签审计**已回退**并归 13.102/13.104 补签，故 H05 生产入口仍未接线（消费者锚只证组件面）；二是相邻回归 `./runtime/agent TestOrphanSettlementDrivesNewSegment` 因 `loop.go` 删除"观测随投递事实同行"而红（A/B 已证），该测试不在本叶写面，处置待编排者裁定（D9.8.5-①）。另 D8.6 的 `TestInProcessSettleFailureDrivesNoSecondTurn` 未建立。

- **收尾与终验记录（2026-11-21，第三单）**：见 [design.md D9.8.6](design.md#d986-收尾会话第三单孤儿结算回归遗漏锚与移交2026-11-21)。三件结果：①孤儿结算回归 `TestOrphanSettlementDrivesNewSegment` **已转绿**（fixture 真落结果事实、断言改从链上结果事实读回；`-count=10` 10/10、补充 `-race -count=5` 5/5）；②D8.6 的 `TestInProcessSettleFailureDrivesNoSecondTurn` **未成绿并阻塞上报**——该行预设的 D4.2 第 144 行（拒写路径保留 claim）在 `assembly/facts.go` 未实施，按定义实测为红（探针 `PROBE turns=1 seats=[]`，`assembly/settle_turn_test.go` 已还原为候选字节），实施需本叶写面外的 `assembly/facts.go` 与 `assembly/host_test.go`；③H05 生产装配缺口**移交 13.104**（`plugin_catalog.go` 构造点补 `withSettleFactCommit`，H05 取证改经 catalog 装配，现探针只证组件面）。终验：`go build ./...` exit 0；`go test ./assembly ./runtime/... ./features/... -count=1` **exit 0、11 包 ok、0 FAIL**；抽样复跑 H05 锚 `-race -count=3` 与 `TestDeliveryFactOfIsOneFactForBothWriteFaces` `-count=3` 绿；`check_concept_budget.py` exit 0（`domain_surface.new: 57`）。**本行仍不勾选**：②未建立、③未接线，12.104 是否完成由编排者按 D9.8.1/D9.8.3/D9.8.6 合并凭据核。

- **H05 保 claim 记录（2026-11-21，第四单，按 D4 原签授权）**：见 [design.md D9.8.6 ②](design.md#d986-收尾会话第三单孤儿结算回归遗漏锚与移交2026-11-21)。实施：`assembly/facts.go:1058–1061` 拒写分支改为**保 claim**（记 debug 后 `return nil`，不再调 `retainNoticeOwed`），close 路径的 `store.Release` 逐字保留——与 D4.2 末行/D8.1-H05 ④/D8.6 该行一致，只此一处；`sink` 与 `retainNoticeOwed` 文档注释改为两路径各自语义。测试：`assembly/settle_turn_test.go` 新增 `TestInProcessSettleFailureDrivesNoSecondTurn`（真 `Run`、只拒 `TypeTaskSettled`、两决策前提、两次 wake 读账户、0 claim／0 回合／席位 `ClaimClaimed`／0 投递事实／0 回执）；`assembly/host_test.go` 两处 `ClaimPending` 断言改 `ClaimClaimed` 并各留"旧语义＝回 pending"对照（`TestDeliveryFactFailurePreservesTheNoticeObligation`、`TestBackflowFailureRecoversWithoutManualSettle`），欠账仍由 `Unconsumed()` 列出承载。凭据：新锚 `-race -count=10` 10/10（FAIL 0、DATA RACE 0）；H05 两枚 `-race -count=10` 各 10/10；`TestDeliveryFactOfIsOneFactForBothWriteFaces` `-count=3` 父 3/子 9 绿；`go build ./...` exit 0；`go test ./assembly ./runtime/... ./features/... -count=1` exit 0、11 包 ok、0 FAIL（assembly 485.819s）；`check_concept_budget.py` exit 0（`new: 57`）；`comment_policy -no-baseline assembly` 34 findings＝本单前同数（本单零新增）。负变异 m1（`-overlay` 于 `/tmp/rr-12-104-f/`，拒写分支回退为 `retainNoticeOwed`）→ 同锚 3/3 红，失败行 `settle_turn_test.go:308` 报"drove 1 follow-up turn(s)…(1 claim(s))"；控制臂未变异 3/3 绿。**本行仍不勾选**：③H05 生产装配接线仍在 13.104，12.104 是否完成由编排者按 D9.8.1/D9.8.3/D9.8.6 合并凭据核。

- **残项清零记录（2026-11-21，第五/六单）：残项七红全部清零，第 8 枚红一并清零**。见 [design.md D9.10](design.md#d910--残项七红裁定与修复2026-11-21第五单) 与 [D9.10.6](design.md#d9106-残项第-8-枚红清零2026-11-21第六单)。第五单：`tests/integration` 七红（history 2／capabilities 1／host 4）判 (b) 过时断言并逐条更新，生产 `.go` 零改动、负变异五案全咬。第六单：遗留第 8 枚红 `TestEventIntegrityRestart` 判 (b)——它钉的是 H05 修好之前的"重启后欠账永留"世界（`Owed=1`＋`accept`＋drain 未完＋0 后续回合），与已签场景「回合完成而消费收据尚未写入」（重启 SHALL 继续未完工作／仅完成剩余义务／确认已消费）相反；触发者是 13.104 已签的 `assembly/plugin_catalog.go:163` 接线，单文件 `-overlay` 回退即复现旧读数并转绿，故非 12.104 生产回归。改法在 `tests/integration/history/event_integrity_test.go`：投递判据由"出站 sender 正文"改为"真实模型请求携带通知材料"（同会话通知本就不产生外发）、`restartAssertOwed` 收紧为"欠账必须做完"、`cross_boundary` 改按做过的工作区分 claim/confirmation、消费者锚改按每次驱动计数（总请求数在交付回合与调用回合间竞态，见 D9.10.6.5-①）、`restartAssertConfirmed` 增"确认侧不得再投递"夹板。凭据：`go test ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host -count=1` exit 0（9.783s／3.443s／202.255s）；该锚 `-count=10` 父 10／FAIL 0；`go test ./assembly ./runtime/... ./features/... -count=1` exit 0、11 包 ok、0 FAIL。负变异：n1–n5（含生产接线回退）与 p1–p5（补前单五案）逐枚确定性红，控制绿，仓库零 diff。**本行勾选状态不变（已勾）**。

## 12.105 全路径回归

- [x] 12.105 完成全路径生命周期/正常失败重启并发回归；产出同候选模式矩阵、逐场景原始结果及缺口归属。验证：下面三锚和补充矩阵逐名非零，产品完成仍待 12.106。
- **阶段与可派状态**：验证；未执行、blocked；只补签发过的测试，发现产品缺陷回 .103/.104 或 C，不静默扩大本叶实现。
- **前置**：12.104、13.104。
- **精确候选文件**：`assembly/checkpoint_test.go`、`assembly/facts_test.go`、`assembly/run_test.go`、`assembly/anchored_boot_parity_test.go`、`assembly/host_test.go`、`assembly/settle_turn_test.go`、`assembly/governance_checkpoint_test.go`、`assembly/host_delivery_checkpoint_test.go`、`assembly/plugin_catalog_test.go`、`assembly/hotupdate_generation_test.go`、`runtime/state/checkpoint_test.go`、`runtime/state/durable_test.go`、`runtime/state/mailbox_checkpoint_test.go`、`runtime/task/checkpoint_test.go`、`runtime/task/task_slice_receipt_anchor_test.go`、`runtime/task/unsettled_test.go`、`runtime/task/approval/approval_store_test.go`、`features/evolution/governance/checkpoint_test.go`；记录为本 `design.md`/`tasks.md`。
- **正锚（拟新增，未建立未执行；首枚同时为真实恢复消费者锚）**：`TestRecoveryEntriesPreserveStateAndObligations` 落 `assembly/checkpoint_test.go`，`TestNoTierCommitPreservesApplicationOrder` 落 `assembly/facts_test.go`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestRecoveryEntriesPreserveStateAndObligations|TestNoTierCommitPreservesApplicationOrder)$' -race -count=10 -v`；预期每名 10 次；前者按 design D3 经真实 Boot/Assemble 触发无可用锚、有效锚、允许降级三类生产分支及无压缩锚的 checkpoint 子分支，每类各 10 次且前提/观测非空，后者真无 tier 而非 memory tier，不新增耐久承诺。
- **分支证据**：无锚全链案有非空年轻链且无恢复投影；checkpoint 子案有恢复幸存者及尾段；有效锚有可解产品、非空幸存者/尾段并分别核有/无框架 checkpoint；允许降级仅用权威事实可读、可选锚元数据不可用且全链可回放的根。每案核原文/逐条状态/任务邮箱、已完凭证与未完义务、冻结路由及真实读路径。最终索引/读取/事件解码错误、缺已索引正文、幸存者还原失败另作拒绝负例，不冒充降级成功。
- **算法对照边界**：`seedFullReplay` 仅作参考；`TestAnchoredBootMatchesFullBoot` 的手调 seed 结果不计真实入口命中。不能新增生产 hook/API 凑三入口；本枚三分支消费证据交 B 06.105/106，同名测试存在或仅父测试 PASS 不算覆盖。
- **负锚（现有）**：`TestUnregisterRefusesASliceWithUnfinishedWork`、`TestUnregisterAllowsASettledSlice`、`TestSliceHandoverKeepsTheUnfinishedObligations`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./runtime/state -run '^(TestUnregisterRefusesASliceWithUnfinishedWork|TestUnregisterAllowsASettledSlice|TestSliceHandoverKeepsTheUnfinishedObligations)$' -race -count=10 -v`；预期每名 10 次；拒绝保原对象、同名替换被拒、结清可卸载，删除检查/先删后报变异各失败。
- **真实消费者锚（现有）**：`TestUnmountKeepsTheOwedItemsOfItsSlice`、`TestPendingSliceIsRefusedByName`、`TestGenerationSwitchKeepsTheSameStateAuthority`、`TestDisabledBuiltinHandsOverItsObligations`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestUnmountKeepsTheOwedItemsOfItsSlice|TestPendingSliceIsRefusedByName|TestGenerationSwitchKeepsTheSameStateAuthority|TestDisabledBuiltinHandsOverItsObligations)$' -race -count=10 -v`；预期每名 10 次；区分公开注册 API、Boot 和 Host.Reload，至少三代。禁用真实范围按 13.104，不造不存在的生产 holder。
- **快照缺失/空/损坏与收据**（现有）：`TestTaskSliceReadsBackTheReceiptItDoesNotState`、`TestTaskSliceTrustsAStatedEmptyReceiptSet`、`TestTaskSliceRefusesAReceiptOfAnotherTask`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./runtime/task -run '^(TestTaskSliceReadsBackTheReceiptItDoesNotState|TestTaskSliceTrustsAStatedEmptyReceiptSet|TestTaskSliceRefusesAReceiptOfAnotherTask)$' -race -count=10 -v`；预期每名 10 次；.105 新入口锚追加无 checkpoint/无 tasks/损坏载荷，覆盖不是只查键名。
- **跨 agent/session 与年轻链**（现有）：`TestCrossSessionObligationsStayInTheirTargetSlice`、`TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestCrossSessionObligationsStayInTheirTargetSlice|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -race -count=10 -v`；预期每名 10 次；在新的真实 Boot/Assemble 三类生产分支锚中补同 session 文本跨 agent 的隔离与同/异键并发，路由 scope 不放宽。
- **审批拒绝/过期/别名/QueryCountList**（现有）：`TestDeniedRulingMustSurviveItsValidityAcrossRestart`、`TestExpiredApprovalNeverRunsAfterRestart`、`TestApprovalAliasCollapsesAcrossTheCut`、`TestGovernanceCountAndQueryStateTheirBoundary`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./features/evolution/governance -run '^(TestDeniedRulingMustSurviveItsValidityAcrossRestart|TestExpiredApprovalNeverRunsAfterRestart|TestApprovalAliasCollapsesAcrossTheCut|TestGovernanceCountAndQueryStateTheirBoundary)$' -race -count=10 -v`；预期每名 10 次；Query/Count/List 正常/拒读都核，历史不可静默裁为窗口结果。
- **真实泵及进程切点**（前两枚拟新增，未建立未执行；末枚现有）：`TestUnreadableReceiptDoesNotDriveAgain`、`TestBackflowFailureRecoversWithoutManualSettle`、`TestRedeliveredNoticeAfterRestartSettlesOnce`。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestUnreadableReceiptDoesNotDriveAgain|TestBackflowFailureRecoversWithoutManualSettle|TestRedeliveredNoticeAfterRestartSettlesOnce)$' -race -count=10 -v`；预期每名 10 次，末枚 3 子场景各 10 次。新锚覆盖独立 OS 进程停止/重启、收据前后切点；旧手工预置只作组件对照，不替代新锚。
- **回归闭包**：复核 .103/.104 所有三锚、B 的原子结果及 C 的公开能力同候选证据；D4 签发完整相邻包/独立模块选择集，不能以本页最小命令冒充顶层 F2 全量。
- **停止条件**：零匹配、skip、只有父测试通过、缺真实入口、未归因红或生命周期需产品修复即停并回拥有叶；缺资源不是绿。全部现有/新增业务模式有结论才能交独立审。

- **执行记录（12.105 验证单，2026-11-21，网络中断重派）**：见 [design.md D12](design.md#d12--12105-回归记录)。被测＝工作树（含未提交的 .103/.104/13.104 实现），HEAD `7b0cde16`。矩阵 9 组：组 1–8 **全部 exit 0 且逐名非零**（组 1 两枚新锚 80 行全绿：`TestRecoveryEntriesPreserveStateAndObligations` 父 10＋六子场景各 10，含无可用锚／checkpoint 子分支／有效锚无 checkpoint／有效锚＋checkpoint／允许降级／幸存者不可还原具名拒绝；`TestNoTierCommitPreservesApplicationOrder` 10）；组 2 30/30、组 3 40/40、组 4 30/30、组 5 20/20、组 6 40/40、组 7 60/60（末枚三子场景各 10）；组 8 八条抽验 exit 0（21/15/15/12/9/6/3/3）。**全包终态**：`./assembly ./runtime/... ./features/...` 隔离复跑 **exit 0、11 包 ok**；`./tests/integration/...` 隔离复跑 **exit 1**——唯一红在 `tests/integration/execution` 三枚（`TestInstancesSymptomTwoEntriesOneAccount`、`TestReceiptAndBackgroundStaySeparate`、`TestSendReentersAgentTask`），其余 13 包 ok。**缺口归属**：三红经 HEAD 树基线 A/B（`git archive HEAD` 到 `/tmp/rr-12-105/headtree`，同三测试 exit 0／1.188s）判定由本候选引入，测试文件与 HEAD 逐字节相同，机制落在 H05 交付事实身份与渲染面 → 归属 **12.104（生产接线 13.104）**，R3 的委托面待拥有叶裁定（次选 05）；按派单**不修**。另记两处派发偏差（`TestOrphanSettlementDrivesNewSegment` 实在 `./runtime/agent`、`TestDeliveryFactOfIsOneFactForBothWriteFaces` 实在 `./runtime/task`，在 `./assembly` 零匹配不计通过，已按真实包补跑 3/3 绿）与并发测量污染（首轮两条组 9 命令并发致性能锚与时序断言红，隔离复跑转绿）。**本行不勾选**：12.105 是否完成由编排者按 D12 逐条凭据与三红处置裁定核。

- **三红已清（2026-11-21，第七单，拥有侧 A）**：见 [design.md D13](design.md#d13--execution-三红裁定与修复2026-11-21第七单)。三枚 **3/3 判 (b) 过时断言**，生产 `.go` 零改动（`delegate.go` 委托安全判据、`host_tools.go` 拒绝入口、`plugin_catalog.go` 13.104 接线一字未改）：①`TestInstancesSymptomTwoEntriesOneAccount` 账目 2 条 → 逐族 1 条正文＋**2 条结算**（被委托回合＋承载它的调用入职，按事实正文分开）；②`TestReceiptAndBackgroundStaySeparate` 六族 → **八族**（每次结算各一条 `task_settled` 结算行，槽判据改为"除结算行外"，扫描面扩到 flush 视图）；③`TestSendReentersAgentTask` 取"最后一条 kind 匹配的结算" → 取"**带回合结果正文**的结算"，并把三枚 send 的编排改按"端点已答过的调用"排序（拒绝材料与结算材料的投影先后属 pacing）。凭据：三锚 `-count=10 -v` **PASS 50／FAIL 0**；`./tests/integration/execution -count=1` exit 0（39.333s）；护栏 `./assembly ./runtime/... ./features/... ./tests/integration/... -count=1` **exit 0、24 包 ok**（assembly 375.432s／execution 49.870s）；H05 主锚 `-race -count=3` 6 PASS＋`TestDeliveryFactOfIsOneFactForBothWriteFaces` 12 PASS（无 DATA RACE）；负变异 n1–n5 逐枚确定性红、控制绿、仓库零 diff。**未修并上报**：`TestNativeConversationRoundTrip` 为既有 flake（候选 5/10、换回 HEAD 断言 5/10、HEAD 树 2/10、单跑 3/3 绿），不属三红亦不在本单写面。**12.105 勾选仍留编排者**。

## 12.106 独立审与结构核销

- [x] 12.106 独立审查并核销结构删除（两轮：初轮 4 阻断全清（①③记录补账、②④死写/死生产者删除门转绿）；关闭轮裁定 A 死写对已由编排者亲删并验证、裁定 B 归 13.105；A 单元 frontier 空方勾；产出旧目标→已选设计→全部调用方→证据的逐项结论、保留/删并账和未解决项。验证：只读核对三锚原始记录、负变异及实际消费者，不能凭测试通过放行。
- **阶段与可派状态**：独立审；未执行、blocked；审者非实施者，仓库只读，允许在自有隔离区定向复跑与构造反例；不改候选代码/测试/计划，结论由编排者核后回填。
- **前置**：12.105。
- **精确候选写文件**：无。读取本单元七文件、顶层合同、D4 实际签发全文件/全部消费者与 .105 原始证据；以下 argv 为定向复跑定义，先确认隔离候选与资源，不重复启动已有长跑。
- **正锚证据**：`TestTaskSnapshotReplaysInterleavedCommit`、`TestConcurrentCommitPreservesApplicationOrder`（当前拟新增，未建立未执行）。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit|TestConcurrentCommitPreservesApplicationOrder)$' -race -count=10 -v`；查同候选每名 10 次及逐条状态/顺序，不只查 PASS。
- **负锚证据**：`TestUnreadableReceiptDoesNotDriveAgain`（当前拟新增，未建立未执行）、`TestUnregisterRefusesASliceWithUnfinishedWork`（现有）。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^TestUnreadableReceiptDoesNotDriveAgain$' -race -count=10 -v`；`go -C /Users/pengweiye/Documents/codes/tagent test ./runtime/state -run '^TestUnregisterRefusesASliceWithUnfinishedWork$' -race -count=10 -v`；查各 10 次控制绿及对应负变异主题红，错误传播和原对象检查均未缩水。
- **真实消费者证据**：`TestBackflowFailureRecoversWithoutManualSettle`（当前拟新增，未建立未执行）、`TestGovernanceSliceWiredInProduction`（现有）。
  `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestBackflowFailureRecoversWithoutManualSettle|TestGovernanceSliceWiredInProduction)$' -race -count=10 -v`；查每名 10 次及 Boot/工具/恢复泵接线、原始退出码；禁止用手调 deliver 的旧锚替代。
- **真实恢复分支复核**：`go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^TestRecoveryEntriesPreserveStateAndObligations$' -race -count=10 -v`（当前拟新增，未建立未执行）；核 .105 同候选每类分支/子场景各 10 次的前提及非空状态/义务，不以 `TestAnchoredBootMatchesFullBoot` 算法对照冒充真实 Boot/Assemble。
- **审查分母**：design D1 旧行为、D2 全结构、D3 每个真/假前提、D4 所有签发文件和无改动调用方；覆盖无 tier、真实 Boot/Assemble 的无可用锚/有效锚/允许降级三类生产分支及 checkpoint 子分支、任务三族、审批/目标/历史查询、卸载/替换/热更。核损坏拒绝未充作降级、新 hook/API 未为凑入口而加；删结构后的伴生接口/字段/缓存/注释逐一核销。
- **停止条件**：未证正确性/安全/需求承接或签发漂移非零即拒绝；报告已查/未查面及具体证据，修复回拥有叶再独立审。范围内 frontier 空且全部目标证据齐才向编排者报可关闭；本叶不勾产品完成或 F。

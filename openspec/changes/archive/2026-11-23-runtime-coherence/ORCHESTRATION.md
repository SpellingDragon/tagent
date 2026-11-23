# ORCHESTRATION · runtime-coherence

## 一、结论

抵达 DoD 的唯一路径：先冻结四份契约（01），用一个任务纵向样板证明机制（02），再收敛其余消费者并删除被替代路径（03），最后在冻结候选上取完整证据与真实 CI（04）。当前最大风险：把“统一”做成新的中间层，或把未复现的静态反例当已修缺陷，导致计划再次打转。

本文件是唯一派发入口。先读本文件与 [design.md](design.md)，再读被派叶所在子变更的四件套；历史只经 [归档承接账](../archive/2026-11-22-runtime-recovery-ideal/CARRYOVER.md) 追溯。

## 二、编排四问

### 次序：就绪叶与触碰集

| 叶 | 前置 | 触碰集（改写 ∪ 验证读取） | 状态 |
|---|---|---|---|
| 01.101 | 无 | `openspec/changes/runtime-coherence/**`；新增症状锚 `assembly/symptom_anchor_coherence_test.go`、`runtime/task/approval/symptom_anchor_ruling_test.go`；其余源码只读 | 就绪 |
| 01.102 | 01.101 | 本变更契约文档；只读 `runtime/state`、`runtime/task`、`assembly` | 待 101 |
| 01.103 | 01.101 | 本变更契约文档；只读 `api/durable`、`memory/ledger` | 待 101 |
| 01.104 | 01.101 | 本变更契约文档；只读 `api/plugin`、`assembly/plugin_catalog.go` | 待 101 |
| 02.201 | 01.102、01.103 | `api/durable/fact_log.go`、`memory/ledger/tier_facts.go`、`runtime/state/durable.go`、`features/evolution/governance/denial.go` ＋镜像测试 | 待契约 |
| 02.202 | 01.103 | `memory/ledger/journal.go`、`memory/ledger/segment_store.go`、`assembly/read.go` ＋镜像测试 | 待契约 |
| 02.203 | 01.104 | `assembly/host.go`、`assembly/run.go`（共享面，编排者单写） | 待契约 |
| 02.204 | 01.102 | `runtime/task/lifecycle.go`、`runtime/task/restore.go`、`runtime/task/checkpoint.go`、`runtime/state/durable.go`（与 201 串行） | 待契约 |
| 02.205 | 02.201、02.204 | `assembly/checkpoint.go`、`assembly/facts.go`（重试已折叠输出范围回归，f13509f9 引入）、`assembly/task_checkpoint.go`、`assembly/task_checkpoint_test.go`、`assembly/checkpoint_test.go` | 待前置 |
| 02.206 | 02.201 | `runtime/task/approval/approval_store.go`、`attention_store.go` ＋测试 | 待前置 |
| 03.301 | 02.205 | `assembly/checkpoint.go`、`assembly/compress_checkpoint.go`、`assembly/run.go`（共享面） | 待 02 |
| 03.302 | 02.206 | `assembly/governance_checkpoint.go`、`features/evolution/governance/**` | 待 02 |
| 03.303 | 02.203、02.206 | `api/plugin/**`、`assembly/tools.go`、`assembly/plugin_catalog.go` | 待 02 |
| 03.304 | 03.301–03.303 | 跨文件删除；编排者单写 | 待 03 |
| 04.401–04.403 | 03.304 | `assembly/*_test.go`、`tests/integration/**`、`golden/**`、`scripts/**`、`.github/**` | 待 03 |
| F1–F4 | 04.403 | 合同、台账、CI、归档 | 待 04 |

并发准入：触碰集不相交才可并行；`assembly/run.go`、`assembly/host.go`、`assembly/checkpoint.go` 与一级账目为共享面，编排者单写或 R22 冻结签名后集成。计量/长跑独占运行，不与写单并行。

#### Wave 1 派发矩阵（01.101 内部并行取证；叶勾选仍以 01.101 整体为准）

| 单 | 类型 | 写面（全部） | 产出/验证 | 与他单相交 |
|---|---|---|---|---|
| W1-A 锚点 | openspec-apply | `assembly/symptom_anchor_coherence_test.go`、`runtime/task/approval/symptom_anchor_ruling_test.go`、`evidence/anchors.md` | S1 红（原始 exit 1）；S2 红或具名驳回；两包 build/vet 绿 | 写面与 B/C 零相交；B/C 不得在 A 写入期间跑 `./assembly`、`./runtime/...` 仓内测试（改用 /tmp 副本） |
| W1-B 存储取证 | general-purpose 只读 | `evidence/store-probes.md` | 点读 vs Scan 的键数/值数对照；order.line 零偏移下 `GetParent` vs 全序走查；全部 /tmp 副本，原始 exit 入档 | 与 A/C 零相交 |
| W1-C 状态取证 | general-purpose 只读 | `evidence/state-probes.md` | 任务在线/恢复同序列对照；旧切面缺新消费者切片的 boot 行为；Boot/reload 贡献矩阵（静态＋副本运行） | 与 A/B 零相交 |
| W1-D 归档核验 | 编排者 | 无（只读） | 归档目录对 HEAD 零 diff、文件清单齐 | — |

CI 红项分诊：非计时项（retry 三项、场景 03、会话往返）在 W1-A 写入完成后派 W1-E（只跑测试、原始 exit）；计时项（`TestBootWallClockRejectsUnattributedGrowth`、`TestSymptomAnchorPerGenerationCostFlatOverALongRun`）在 Wave 1 全部收口后独占串行重取，不与任何写单或长测并行。

#### 02 段合单裁定

`02.201` 与 `02.204` 同触 `runtime/state/durable.go`：合为单（先点读后 apply，一次交付）或严格串行；`02.202`、`02.203` 与之并行。`02.205` 待 201+204；`02.206` 待 201。

### 授权：写面与边界

- 只有 01.101 落盘时使用规划写面；实施叶必须持对应契约冻结记录与精确文件白名单。
- 禁止：改冻结归档、改外来未跟踪面（`no-prefix-criterion`、`runtime-architecture-convergence`、外来 gates）、读写真实 `.wechat-config/data`。
- 主规格只在 F4 归档时合并；子变更 delta 不等于主规格已改。
- 持久格式、公共接口签名、安全时机约束的变化：先回设计门，列出替代方案与全部消费者，不事后追认。
- 一般实施步骤不请用户表决；只有两类停线：①持久数据/兼容或保证的取舍；②目标/范围变化。

### 凭据：每单五查与复跑

- 正锚：测试名与 `-run` 逐字符一致，非空转（用例数 > 0）。
- 负锚：具名错误路径（读失败≠不存在、缺片≠空态、未注册名拒绝、写失败不放行）。
- 消费者锚：正式 `Boot`/`Assemble`/真实 Run 入口；测试手工补接线不算。
- 每单须附：argv、工作目录、原始退出码、选择集、关键计数；`grep -c` 为 0 时不得串 `&&`。
- 写后校验：`grep` 落盘实证、构建闭包可编译、镜像测试存在。

### 释放：共享面与等待表

| 共享资源 | 持有者 | 释放条件 | 等待者 |
|---|---|---|---|
| `assembly/run.go` / `host.go` 接线 | 02.203 单 | 该单验收并回填台账 | 02.205、03.301 |
| `assembly/checkpoint.go` | 02.205 → 03.301 | 纵向样板绿后移交 | 03.301 |
| `assembly/governance_checkpoint.go` | 03.302 | 单完 | 03.304 |
| `runtime/state/durable.go` | 02.201 ↔ 02.204 | 按写面串行 | 02.204 |
| 编译闭包/计时机 | 各验证叶 | 所依赖写单全部收口 | 04.402、F2 |
| Git/远端 | 编排者 | 用户明示授权 | F4 |

释放事件即触发：确认释放后当场补派等待表中的就绪单，不以“下一波”悬置。

## 三、明细

### 派发单与合单

- 默认一叶一单；触碰同一文件簇（如 02.201 与 02.204 都改 `runtime/state/durable.go`）则合单或串行。
- 01 的四叶可并行（只写各自契约文档）。
- 02.201 与 02.203 可并行；02.202 独立；02.204 与 02.201 共享 `durable.go`，串行。
- 03 三叶在 02 完成后可并行；03.304 编排者单写。

### 承接账（不复制旧文）

| 旧项 | 状态 | 本变更加工 |
|---|---|---|
| 真实 CI / 外部保护 | 2026-11-22 run `45004855439` 红 | 04.402/F4 按原文归因，不继承旧绿 |
| 三观测轴、total 未归因 | 未终裁 | 04.402 与 P4 |
| file 档发现端到端 | 待 CI 面 | 04.402 |
| 长期每代成本锚 | CI 红（`TestSymptomAnchorPerGenerationCostFlatOverALongRun`） | 阶段 0 定性 → 04.402 |
| 13 条拒并、#11 暂缓、15 条活义务 | 保持 | F1 保留状态，不自动关闭 |
| 03.R5 两窄口 | 已勘误为兑现（C11.12） | 不再承接 |

### 已知 CI 红（候选 `52d7b664`）

- test：`TestRetryLeavesPresentedRangeAtTheLatestRound/folded`、`TestRetryOfFoldedOutputDoesNotReopenTheRound`、`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`、`TestBootWallClockRejectsUnattributedGrowth`、`TestScenario03ToolReceiptAndBackground`、`TestNativeConversationRoundTrip`
- race：同上三项＋`TestSymptomAnchorPerGenerationCostFlatOverALongRun`＋`TestNativeConversationRoundTrip`
- event-integrity-short (6)、event-integrity-required 红
- archive-preflight：evidence-binding exit 1；spec-hygiene exit 2（`openspec CLI not found`）
- 处置：产品红项入阶段 0 归因；CI 接线单独修；未归因前不进入“架构已收敛”的放行结论。

## 四、台账

| 事项 | 当前事实 |
|---|---|
| 用户授权 | 归档旧账并新立计划；未授权实施、提交、push 或归档本变更 |
| 候选 | `52d7b664`（实施开始时重核） |
| 旧变更 | `archive/2026-11-22-runtime-recovery-ideal` 已归档，字节不变 |
| 本变更工件 | proposal/design/specs/tasks/ORCHESTRATION 已落盘；子变更四套已落盘 |
| 叶状态 | 18 叶全部未开始；只有 01.101 可派 |
| 证据 | 无新执行证据；审计结论为静态反例/已核源码，未跑故障注入 |
| 规划自审 | 覆盖、消费者、真实入口、三锚与承接边界已列；实施签发全部待填 |
| Wave 1 | 2026-11-22 派发：W1-A/B/C 三单并行＋W1-D 编排者核验；S1/S2 尚无执行证据；CI 红分诊排 W1-E（非计时）与收口后（计时） |
| W1-A 收单 | S1/S2 均已复现（编排者亲验 rc=1×2，复跑同红；生产树零 diff）；S2 定性＝切面丢项非误放行；Stateful 贡献断言缺公开入口门 → 归 02.204 写面 |
| W1-B 收单 | 存储两疑点已复现：Scan(完整键)＝全表枚举（N=1000→2048 slots／N=5000→16384，Get 恒 1–3）；order.line 零偏移使 GetParent 静默答空（rc=1 确定性）；word 无校验和不触发 rebuild＝设计差距 |
| W1-E 收单 | retry 三项＝产品缺陷，引入于 f13509f9（7b0cde16 绿→f13509f9 红，`-count=5` 稳定红）→ 并入 02.205 修复面；Scenario03 与 event-short shard6＝CI 环境或负载（本地绿，shard6 含 -race 绿）；NativeConversationRoundTrip＝本地间歇 3/10（settle 观测时序竞态）→ 归 04.401/04.402 |
| W1-C 收单 | 任务双解释**行为层驳回**（×10 一致；边界＝仅内存 Flush 消费 obligations 0vs2 属 at-least-once 口径）→ 02.204 降为结构收敛非缺陷修；缺片空态启动已复现（fold 8/24×5）→ 01.102/03.301；reload 不重建插件集静态证实 → 01.104 |
| Wave 1 收口 | 01.101 已勾：11 项判定入 audit.md，S1/S2 第一天红；下一波＝01.102/01.103/01.104 三契约单并行＋W1-F 计时项独占串行（与文档单不冲突，契约单禁跑 go 命令） |
| W1-F 收单 | 两计时锚本地独占串行全绿（rc=0×3：boot×3、长跑、长跑-race）→ CI-only 红，定性环境负载/阈值偏紧，归 04.402（不预先豁免）；至此 CI 红全部分诊完毕：产品缺陷仅 retry 三项（02.205） |
| 01 段收口 | 01.102/103/104 已勾：三契约落盘（69/53/51 行）抽查通过；编排者裁定两处——Stateful 入口门采甲 `WithStateSlice`（写入契约§六）、索引方案采乙（读侧身份校验，谓词＝走查命名进入键） |
| Wave 3 派发 | 02.203 在飞（host.go/run.go 独占）；02.204 拆 204a（runtime 面，与 02.201 合单 W3-1 即派）＋204b（WithStateSlice，待 02.203 释放 run.go/host.go）；02.202 待 W3-1 释放 memory/ledger 后派；assembly 消费者锚与 gen_godoc 重生成归编排者收口核验 |
| W3-1 撞限 | 前单撞 150 轮上限（实施已落、验证未做）：git 实况 +630/−128——`FactLog.Get` 已立约（fact_log.go:37）、durable/denial 迁移与任务 apply 收敛大半落盘、无证据文件；三处越界写待定性（tests/integration×2、examples/contract×1）。已派接续单 W3-1c：只补缺＋验证＋越界定性，禁重开设计、禁触 assembly/**（02.203 仍独占） |
| 02.203 收单 | 编排者亲验：4 锚各自 RUN+PASS、S1 主树/隔离树各×2 绿、负锚三变异隔离树各 exit 1、mounting/vet/gofmt 0；`cfg.plugins`＋`cfg.genRegistrations` 均转交；偏离（字段改名 pluginBuilders、+1 序锚）已档。**释放 run.go/host.go** → 02.204b 即派。债务登记：①两症状锚文件 `test-mirror-missing`（package-layout）②comment_policy 残余 3 项在 01.101 锚文件③主树 gen_godoc 差集两页（api__durable、examples__contract__api_durable，待 W3-1c 收口后统一重生成）——均编排者收口 |
| W3-1c 收单 | 02.201＋02.204a 实施核验完毕：18 个 FactLog 实现方全带 `Get`（三破口补齐）；迁移三处（readReceiptLocked/governanceTail/denial.go:360 同型）；apply 收敛＝`applyFactLocked`（lifecycle.go:405，在线 :441＋恢复 restore.go:271 两入口，唯一 status 写点）；六锚含 `-race -count=2` 绿；S2 干净基线同红→确认归 02.206；integration/examples 改动＝FactLog 实现方/契约样例涟漪（build 证）。**勾选挂起**：assembly 消费者锚 `TestGovernanceBootWorkTracksOpenItemsAndTail` 待 02.204b 释放后编排者收口。**释放 memory/ledger（除 02.202 预留两文件本即归它）** → 02.202 即派（ledger 层先行，assembly 侧 GetParent 锚归收口） |
| 02.202 收单 | 编排者亲验：负锚 `TestZeroedOrderWordNeverChangesTheLedgerWalk` PASS＋既有锚绿＋vet 0；红证隔离树先行、变异 M2/M3 咬合、全包 131s 绿；`EachIndexNewestFirstFrom` 包 `namingVisit`（谓词＝走查命名进入键）。assembly 侧 `GetParent` 锚归 04 收口面 |
| 02.204b 收单＋02 段收口 | 编排者亲验：`TestBootMountsAContributedStateSliceAndRestoresIt`（-race×5 RUN10/PASS10）＋重名负锚＋M1 变异咬合；消费者锚 `TestGovernanceBootWorkTracksOpenItemsAndTail` PASS→02.201/204a/204b 勾选；02.202 勾选（ledger 侧亲验在前）。收口债务清偿：gen_godoc 全量重生成＋`--check` rc=0；comment_policy 0 finding（S2 并入 approval_store_test.go、doc 形状修正；残余 exit 1＝外来自测失配 C7.3 在案）；package-layout 两豁免登记（同既有 symptom 锚先例）＋S2 文件删除；`memory/replay/fold_test.go` 镜像＝HEAD 继承债→03.304。**派发 02.205（retry 回归＋纵向样板）∥02.206（审批三态，S2 转绿）**，写面不相交（assembly vs runtime/task/approval） |
| 02.206 收单 | 三态落地（`Decide` 先 fileAnswer 后镜像，approval_store.go:428-486；`foldFiledRuling`:804 读回判定）；编排者亲验：S2 `TestSymptomAnchorFailedRulingKeepsTheHold` PASS（红转绿×2）＋新负锚 `TestApprovalUncertainRulingIsNotReportedAsAbsent`（基线两 leg 各红＝有牙）＋既有四锚 `-race -count=10` 绿；02.206 已勾。record 缺失致重启按 record 读回不可达＝既有边界登记不扩面。**02 段余 02.205 在飞** |
| 02.205 撞限 | 前单撞 150 轮上限：实施大半已落（retry 三锚绿 rc=0、checkpoint.go −77 特判移除、task_checkpoint.go 依赖迁拥有者侧、纵向锚 :526 在、docs/api 重生成），但**两负锚名 grep 零命中**（先前合跑 rc=0 疑零匹配空转）且无证据文件。已派接续单 02.205c：逐锚实名验证＋补缺锚＋全包/race 回归＋证据，禁重做已落部分 |
| 02.205c 收单（部分） | 三锚实名全绿（纵向 :528＋新建缺片 :689/损坏 :747，各 RUN+PASS 非空转）；依赖形态＝拥有者自述（run.go:663 restoreSeats、task_checkpoint.go:88-107），协调者仅接口＋声明序循环；retry×3、恢复四锚、race×10、build/vet/gofmt/mounting 全绿。**全包 rc=1 两新红**（`TestFactLandedBesideAnUncertainBarrierIsStatedByItsRetry`、`TestConcurrentSameFactKeepsOneApplication/the_same_fact_twice`，基线绿、02.205 首单 facts.go retry 改写引入）：`FactAlreadyPresent` 三出口被压平，丢"未登记→落地登记＋单折"人群；与 retry 三锚（登记被 trim 释放⇒重复）同形反义，需 trim 无关判据。已派终修单 02.205f（facts.go 白名单内，五锚为法）；godoc 两页 drift（assembly/approval）归收口重生成 |
| 02.205f 收单＝**02 段关闭** | 呈现判据落地：`chainAlreadyPresented`（facts.go:229-264，记录时刻 ≤ 链已呈现段 ⇒ 重复；只读 replays、trim 无关、O(窗口)），四出口恢复；编排者亲验五锚 rc=0（5 PASS）＋godoc 全量重生成 `--check` rc=0；02.205 已勾。**02 段 6/6 全关（S1/S2 双转绿、CI 唯一产品红 retry 三项已修）**。Wave 4a 派发：03.301（assembly）∥03.302-features ∥03.303-api 三单并行；03.302/303 的 assembly 接线合单 W4b 随后 |
| 03.302-features 收单 | 契约注释一处（checkpoint.go:85-90）；新正锚 `TestGovernanceMissingSliceReadsTheWholeChain`＋负锚 `TestGovernanceHistoryReadFailureIsNamedNotAbsent`（隔离副本两腿各红＝有牙，基线已具名→防回归）；编排者亲验 3 锚 PASS＋vet 0＋全包绿。勾选挂起待 W4b 装配面。遗留登记：`runtime/task/approval/attention_log.go:145/169` 完整键扫描（02.201 误记归 02.206、其写面未含）→**改归 W4b** |
| 03.303-api 收单 | 核对式落地：六趟分派已声明驱动（`observedFace.material` 携入＋`validateMaterial` 入口拒越界），零生产改动；三锚新建绿（编排者亲验 rc=0）＋M1–M4 变异咬合。**Governor 分叉编排者裁乙**：W4b 令装配裁决面消费管线声明 Governor（零新公共接口；`WithPolicy`＝无声明缺省；今日无生产声明者故零行为变化），锚 `TestGovernorDeclaredInThePipelineRulesTheCall`。勾选挂起待 W4b |
| 03.301 撞限 | 前单撞 150 轮（最后动作＝压缩挂载声明接线中）：已落 +421/−96（checkpoint/compress_checkpoint/compress_consumer/run＋测试 179 行），build rc=0、两新锚在（:381/:413）、证据已有；已派接续单 03.301c＝完成度核对（压缩挂载声明、特判删除定性、Content 判定）＋逐锚＋全包回归 |
| 03.301c 收单＝**Wave 4a 关闭** | 三核对全落（零源码补丁）：覆盖自述经 plugin_catalog:114→compress_mount:26-32→run.go:1122；`compressCut` 特判零残留；Content＝唯一来源保留＋定位锚；12 锚逐项 rc=0、全包 374s、retry 五/S1/S2 绿。编排者亲验三新锚 PASS＋godoc 全量重生成（含 governance 页）`--check` rc=0；**03.301 已勾，03.302/303 挂起待 W4b**。派发 W4b 合并单：治理装配面核对＋Governor 乙案接线（锚 `TestGovernorDeclaredInThePipelineRulesTheCall`）＋`attention_log.go:145/169` 点读迁移 |
| W4b 撞限 | A/B 已落（run.go +161/−63，Governor 乙案接线、新锚 run_test.go:1140 绿、build rc=0）；C 未做（attention_log 仍 2 处 log.Scan）。已派 W4b-c：C 迁移＋全量验证（治理锚七、S1/S2/retry、全包）＋证据 w4b.md |
| W4b-c 收单 | C 两处定性为**合法族读**（:121/:301 键不由读者命名）＋注释，原 :145/:169 点读已在 §三迁毕；B 锚（含拒绝对照腿）race×3 绿、治理七锚 7 PASS、九症状/回归锚绿、全包 371s rc=0、godoc 83 页 check 绿。**03.302/03.303 已勾**。**03.304 编排者单写收口**：删除台账八项＋不删五项＋fold_test 继承债豁免；概念预算首跑撞 `FactLog` 棘轮→按窄口先例修订契约（store-reads §一 标修订）：FactLog 保持三方法、点读为消费者侧 `pointReadLog` 断言、缺席具名拒；五生产点＋三测试包装同形改造，build rc=0、预算 RESULT ok、四包锚绿、gofmt 0。**03 段 4/4 关闭**。派发 04.401 |
| 04.401 撞限→收口 | 矩阵＋两缺口锚＋变异入 `evidence/04.401.md`；11 锚 race×10 PASS、集成两包绿一次；G3 两 flake 按 HEAD 基线定性既有（等待谓词窄于断言面）→04.402 处置；G1 措辞缺陷编排者修（`declaredPluginsOf` 收 runLevel 名字集，run 级拒绝报 `run-level`），5 锚复验绿、policy 0 finding；04.401 已勾。**04.402 拆 a/b**：a＝CI 接线（openspec CLI 安装＋evidence-binding 复核）＋计时/成本锚串行重取（由子代理）；b＝完整矩阵（十短/六长/变异/bundle）由编排者后台独占跑（R32b），跑完合并证据 |
| 04.402a 收单 | CI：archive-preflight 增 `actions/setup-node@v4`＋`npm i -g @fission-ai/openspec@1.2.0`（与 openspec 作业同版本、判据未动），YAML_OK 编排者复核；G3：等待谓词扩至断言八族＋驱动前等两通知落账，10/100 残留→1/100，残留 `closingView`≈0.5%（3/630）具名上报未改（`model.Request` 无 invocation 身份，内容判据会重定义断言面）；C 计时锚因 load 6.4→39 按负载门跳过，待矩阵收口后独占重取。**矩阵在跑**：十短＋变异（bdh22k32w）、六长改函数位置参数重发（b1xedggse；首次因 zsh `set -- $var` 不分词全 rc=2，污染目录已清） |
| 矩阵首两轮作废 | 十短 1–3 rc=2（依赖闭包跑中变化＝04.402a 写树并发）＋4–6 绿混批；六长 256 组同因 rc=2、32768 组 verdict=unattested（缺 CI 同款 `TAGENT_CALIBRATION_RUN_DIR`，logs=0）。编排教训：矩阵须排全部写单之后、env 随令。已停残留后台，空树快照（freeze.txt）后单一脚本重发：十短→变异→六长（带 env，3+3）→静默 60s 后三计时锚串行（全部原始 rc 入档） |
| 冻结树矩阵终局 | 十短 **10/10 rc=0**；六长 **6/6 rc=0**（verdict=match、durable_scan 有据）；boot 墙钟 rc=0；长跑成本锚首轮 load≈9 红、静默窗（load 3.06）**×3 全 rc=0** → 负载 flake 定谳。变异 23 项 **22 killed＋1 `compile_failure`**（`MUT-EVID-04-position-boundary` 注册表漂移，非产品红）→ 派迁移单（fault 形态＝身份边界改位置边界），要求 23/23 真跑 killed。**04.402 待迁移收口后勾选**，随后 04.403 独立审＋F1–F4 |
| 04.402 收单 | 迁移单：漂移真因＝新增读点 `chainAlreadyPresented` 未被补丁覆盖（8 条 old 逐字节命中、零改），补第 9 companion 位置化读点；单跑 killed/named_assertion、全跑 **23/23 killed、0 not_killed、0 compile_failure**、control_tree_green；另修自测陈旧断言（01.2 锚 13.103 已迁，`retainNoticeOwed`→refusal 分支断言）至 **60/60 OK**。**04.403 独立审已派**（R35：只读、对抗式、frontier 空为准）；F1–F4 待审后执行 |
| 04.403 一轮裁定 | frontier 8 项：**F1 成立 P1**（WithStateSlice 持有者旧 cut 缺片静默空态——审计#6 经新入口重现）→修＝未被覆盖的非内置持有者存在时全链回放（reducer 按身份幂等）；**F2 成立**（receipt 测试 2 条 free-standing）→编排者删冗余注释；**F3 成立**（reactivate/resume 开轮义务仅在线）→并回 applyFactLocked；**F4 成立**（store-reads 签发区陈旧）→编排者改「修订后」；**F5 成立**（declaredGovernorOf 缺端口判据）→加 Port 判据；**F6 成立**（Boot 静默丢 capture/embedder 等 option）→未支持 option 具名拒 boot；**F7** 措辞如实化（evidence-binding 端到端归真 CI/F4）；**F8** 残留定性登记（closingView 0.5%＝已声明边界；NativeRoundTrip＝继承 flake，CI 复发即回 04.401）。驳回项：证据未过期（freeze 后 .go 零改）、pointReadLog 三包重复非自证、WithStateSlice 生产使用面 0 属已裁代价、S2 带错返回正确、唯一 status 写点成立、reload/清理/豁免有真锚。派 A（F1）∥C（F3），F5+F6 待 A 释放后合单 |
| C（F3）收单 | 开轮义务并入 `applyFactLocked`（lifecycle.go:424-428，`opensRound` 单点），`Resume`/`Reactivate` 重复追加删除；round 仍单点 `replayedRound`；`ttlAnchor/reported` 留外围并注释。正锚双腿 8 PASS、`-overlay` 逆补丁两腿全红（红证）、`./runtime/...` 4 包绿、race×2、repro live=restored=[awaiting-tool]；编排者亲验两锚 PASS＋vet 0。godoc/policy 收口等 A 释放。**A（F1）在飞**，随后 B（F5+F6） |
| A（F1）收单 | 覆盖集合 `noteRestoredSlices`（框架＋会话 cut 名，`checkpointSlices` 原义不动）＋`judgeContributedSliceCoverage`（run.go:724，restore 后/seedReplay 前；无 cut 不置位）＋`seedBootChains` 置位走 `seedFullReplay`。两新锚：未覆盖→holder 自 fold 32/32；已覆盖→有界 boot 不被污染（逆补丁 overlay 下正锚 FAIL 15/32 红证）。7 回归锚＋全包 377s 绿、build/vet/gofmt/godoc-check 0；编排者亲验两锚 2 PASS。偏离申报：内置回退闭集在协调者内（安全侧默认）、docs/api 一次全量重生成（吸收在飞 durable 注释）。**B（F5+F6）在飞**，收单后统一 policy/godoc 收口与二轮 R35 |
| B＋收口批 | B：F5 端口判据（run.go:2275-2284，overlay 红基线 `asked to rule 1 call(s)`）；F6 23 字段分类 11 支持/12 具名拒（host.go:465-478 表＋:542-547 检查），6 面锚。（编排者亲验：5 名锚含 S2 复绿、预算 ok、godoc 全量重生成 rc=0、build rc=0、gofmt 0。）**policy 23 条**（A/B 新注释形状）→ 派注释收口单清零；清零后二轮 R35 |
| 注释收口＋二轮审派发 | 23/23 清零（分类：坏索引 6/测试 doc 8/external-coord 3/process-artifact 3/free-standing 2/doc-not-brief 1），直接入口 `comment_policy` rc=0、0 beyond baseline、非注释行 diff＝0；组合 `lint.sh --only policy` 残余 rc=1＝外来未跟踪自测注册表失配（C7.3 在案，不触）。**矩阵证据已过期**（修复轮改 .go）：最终重取排二轮审后冻结（F2）。**二轮 R35 已派**：F1/F3/F5 复跑、F6 拒绝表逐字段反核（含 examples/cli Boot 面不误拒）、注释零行为抽样、新面扫雷、记账一致性；frontier 空才进 F1–F4 |
| 二轮裁定与修复 | frontier 5 项全成立：R35-1（Reactivate 文档 bullet 失真＋godoc 未重生成）→ 改写 bullet 为真话＋全量重生成（check rc=0）；R35-2（保留名可按名冒充内置）→ `WithStateSlice` 注册前 `restoredFallbackSlices` 命中具名拒，新锚 `TestContributedSliceNamedLikeAFrameworkFaceIsRefused`；R35-3（store-reads §二 approval 段过期）→ 改「W4b 已迁移」；R35-4（F1 证据假锚＋wiki 缺规则）→ 证据改真、wiki §六 补「未覆盖非内置→全链回放」、删 PendingRestore 假句；R35-5（F5 反向语义未入契约）→ §四 补反向句＋回落顺序。复验：godoc/build/vet/gofmt/policy 全绿、三锚 PASS。**三轮窄审已派**（只验 5 项咬合＋无误拒）；空后冻结重取全矩阵（F2） |
| 三轮收口＝R35 关闭 | 三轮 PASS×4＋FAIL 仅字面 3 项（plugin-assembly §四 U+FFFD×2、store-reads 行号 :157/:184→实 :161/:188、§二表为迁移记录未标注）——均按字节修复并客观复核（FFFD=0、`reader.Get` :161/:188 实证、表注显式、godoc/doc-refs/policy 全 0）。判据：字面文本可字节验证，不另开四轮。**冻结树（matrix-final/freeze.txt）**：重取十短/六长/变异/计时＋三 module/root 全量 short/lint/openspec/race 串行后台；期间仅写 md，不碰 .go |
| 提交与推送 | 用户明示「commit and push」。精确暂存：`git add -u`＋新文件/变更目录，**排除外来九项**；矩阵运行产物（1568 文件）误入后已 `git reset` 出索引，证据只留 md（归档前删 matrix 目录）。提交 `bde7f394`（106 文件，+8175/−327），`52d7b664..bde7f394 dev→dev` 已推送；CI 终局观察已挂后台。本地 matrix-final/F2 全门后台仍在其上跑（工作树字节未变）。F4 余项：主 spec 同步/归档/CI 实效核销 |
| 中间产物清理 | 已清：旧波临时树与日志（/tmp 下 w1*/rr-*/0220x/f1|b-baseline/rc-repro/m401-* 等）、残留 worktree（list 仅主树）、仓内已被取代的 `evidence/matrix/`。保留：当前在跑的 `matrix-final/` 与其 /tmp/mf-* 现场、`/tmp/mf-mutations`。**待终局后**：裁剪 matrix-final 至小结（exits/timing/f2/freeze＋README），运行产物不入库也不入归档 |
| CI@bde7f394 归因 | race×2＋test×2 红（同一提交双触发）。原文提取：①`:661`＝**计数面 ±1**（injected 44 vs control 45 addressed value reads，“不动计数面”前提被破坏，疑 30s checkpoint 节奏在慢机多落/读漂移）；②`:858`＝按**墙钟**判世代平坦（违 bounded-recovery:104/118“墙钟/磁盘不得充当有界判据”）；③NativeRoundTrip 10s 超时复发（本地 3/10，按“复发即回修”）。**本地 matrix-final 已停**（新 .go 修复将使其证据过期，终审后统一重冻结）。已派专项单：A 计数漂移根因与节奏解耦（禁 ±1 放宽）、B 世代判据改计数面（保变异咬合）、C roundtrip flake 根因修复（禁以加超时掩盖） |
| 后继目标：磁盘有界 | 用户 2026-11-22 裁定：journal.log 磁盘无界不符预期，取 **A 档＝热盘有界＋归档可读**；现行“无界＝已接受成本”口径撤销。已立后继变更 `openspec/changes/bounded-hot-log/`（proposal/design/specs/tasks/ORCHESTRATION＋三子变更 18 文件，`validate --strict` 通过）：01 增长实测→有界判据（跨代平台）→归档契约/配置面→签发；02 段轮转/搬迁/manifest/GC/定位读；03 消费一致性与 S1/S2 锚；F1–F4。启动门＝本变更 CI 收口。**不在 runtime-coherence 内实施**，避免范围膨胀。用户随后校准层模型：checkpoint/tail 属逻辑层、WAL/段/归档属物理层；有界性是 P 的属性，L 承诺不变（已写入 bounded-hot-log D0，删除“修订 P1 措辞”说法） |
| 04.404 撞限→拆双续单 | 前单改 `assembly/symptom_anchor_longrun_test.go`（+56/−8）：A＝**夹具确定性**（`awaitNextStamp` 把每步 wall-clock 事实分到独立毫秒，两棵同形树内容一致；计数相等断言未放宽；A 本地 `-count=3` 绿）；B＝判据改计数面（新增 `historyProportionalCount`）但测试体未收尾、负腿未证；C 未动；证据未落。派 04.404c（B 收尾＋A/B 负载复验）∥04.404d（C roundtrip 根因修复）两单并行，写面不相交 |
| 结构门债（2026-11-23） | 技能 `validate.sh` 对本变更 review red：ORCHESTRATION 四层标题为 ①②③④（模板要求「一、结论/二、编排四问/三、明细/四、台账」）；四域任务行验证面为缩进续行（模板要求 `- [ ]` 行内含「验证：」）；α⁺/αᶜ 字面未冻结。属工件形态债，不影响工程证据。**与 04.404 收尾的台账回填同批一次修缮**（同文件并行写会冲突），修后 top/orch/review/逐域 sub 全 exit 0 方可进 F 归档 |
| 04.404c/d 收口（2026-11-23） | c 单输出停在 02:39 但产物完整（evidence 五节：A 探针 ±1、夹具毫秒确定性、B 计数判据与负腿红证 2733vs27、验证表）——按产物收口不重派。编排者裁定两待裁：①步内折叠对毫秒掷币＝推 CI 实测，禁 ±1 容差与重试，红了按设计层修；②墙钟 control-pair 带 1.3＝设计意图（控制三自检），负载超带红属「测量不可用」，不放宽。d 单（roundtrip）已亲验：产品判据由「空消息」改「含 result 段」（marker 与 delivery_fact.go:70 字节一致）、夹具剥 ticket 一层断言仍严格、`-overlay` 负锚咬合；编排者补 `consumer_settled_body_test.go` 封 marker 双拼写。测试名 Cost 错位随 F1。下一步：亲验完 → 重取矩阵 → 暂存提交推送（用户已授权）→ CI 观察 |
| 矩阵重取与用法错（2026-11-23） | 冻结重取（matrix-04404）：**十短首轮全部 rc=2 属编排者传参错**（runner 原文：「--repeat-index and --repeat are mutually exclusive」，CI 口径只传 `--repeat-index`）——非测试红；修正后单独串行重跑十短。六长首个 long-1-256 rc=0。亲验面已另行全绿：A/B 锚各 `-count=10`（90.9s/256.8s）、assembly 全包 359.6s、agent、execution `-short`、vet、gofmt（`/tmp/04404-verify.log`） |
| 矩阵终局与提交（2026-11-23） | **十短 10/10**（short-1 首轮＝负载 flake：孤儿 `yes`×8 清理前 load 8–11；串行×5 绿＋重跑 match run_id=679f204c，不修不放宽）；**六长 6/6；变异 survived=0；boot 墙钟高载/静默双绿；长跑锚 3/3**（计数面判决）。矩阵后仅注释/gofmt 形态改动（comment_policy 0 findings）。运行产物目录已删（小结入 evidence §七）。本批含 bounded-hot-log 四域立项（含 04-ci-throughput）与四域任务行结构修缮。提交推送（用户已授权）→ CI 观察 |
| CI 三轮归因（2026-11-23） | **43d34916**：race×2 绿（14.1/15.5m，三簇修复奏效）、event 17 job 绿、变异绿；唯一红＝test job 的 doc-budget 棘轮（22470>22463，+7 行来自 bde7f394 的 CHANGELOG 条目，被当时 Test 步骤的红遮蔽）。**5359c7ea**：删 Unreleased bullet 间空行 15 行＋`--tighten` 锁步 22455；push run **全绿**，PR run 红＝lint.sh 的 archive preflight `--commit HEAD` 分支（dev 相对 main 有归档目录新增）读到 test job 只含 mutations 的证据 bundle（10 short/6 long found 0）——结构假红。**be2e9ddb**：lint.sh 增 `--no-preflight`，test job Lint 传入；独立 `archive-preflight` job（needs event-integrity-required，全量下载组装）仍是权威门。观察中 |

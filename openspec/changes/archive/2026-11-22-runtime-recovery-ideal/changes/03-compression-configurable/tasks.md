> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 03 压缩可配置 · 孙任务

## 当前状态与行动单（本轮勘误，原预算目标保留）

03.R1/03.R2/03.R4 保留已完成记录；03.R3 已复验（凭据见文末「03.R3 复验记录」，四要素逐项核销、代码零偏差，两项文档叙述滞后与一项无锚场景已登记待派）。当前 checkpoint 已只输出未完请求、`judgments` 只留未答、构建遍历随未完量（03.R2 凭据见文末第三轮记录）。四叶的独立只读审仍未做，放行仍待非实施者审。

各叶的文件集均相对仓库根，另含本域 `tasks.md`/`design.md`；仅为后续实施白名单，不授权本轮代码写入。新增锚均标「拟新增、未运行」；结账逐名记录命中数与退出码，组合命令中每枚冻结锚都要命中，部分命中或零匹配不算通过。每叶修复后由非实施者独立只读审查三锚与受影响调用方，列出范围和未证项，不以旧绿自证放行。

- [x] 03.R1 定位并修复CI同一压缩请求出现两个产物的失败，先核同请求多产物是否违反现行规格、正常执行与恢复如何共用处理进度；前置09.R1，归因不由本地绿或文件在main推定。文件白名单：`assembly/compress_consumer.go`、`assembly/compress_checkpoint.go`、`assembly/recovery_judgment_test.go`，若需共享状态接线交12串行集成 —— 正/消费者锚：`go test ./assembly -run '^TestRestartExecutesTheChainJudgmentOnce$' -race -count=10 -v`（现有，CI已红）；负锚：`TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint` 已存在（本轮未跑），受控交错与重启反例以 `go test ./assembly -run '^TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint$' -race -count=10 -v` 验证；原版/候选同条件对照，不能把没有DATA RACE报告当作没有状态竞态。
- [x] 03.R2 清减压缩常驻态与快照构建工作量（待验）。产出：处理进度、未完请求及必要历史引用；已答证明按身份读链，不靠永久累积 `answered`，也不以最大序号跳过乱序未完。事实：`assembly/compress_checkpoint.go:57-64` 遍历全部 `filed` 且按历史长度预分配；`:104-106` 遍历会话历史；`assembly/compress_consumer.go:178/:190` 仍追加请求/答案。前置：G2、12.R2 的处理进度语义通过；共享 checkpoint 接线交 12 串行集成。精确文件集：`assembly/compress_consumer.go`、`assembly/compress_checkpoint.go`、`assembly/compress_checkpoint_test.go`、`assembly/recovery_judgment_test.go`（沿用镜像测试，不另建状态测试文件）。
  正锚：拟新增、未运行 `TestCompressionCheckpointTracksPendingWork`，固定未完量与新输入，递增已结历史，分别记录**构建遍历/分配工作量、常驻内存/状态项数、序列化输出字节**；既有 `TestCompressionSliceBodyIsBoundedByTheBatchItOwes` 只证输出面。负锚：拟新增、未运行 `TestCompressionCheckpointPreservesOutOfOrderPendingRequest` / `TestCompressionCheckpointDoesNotRepeatSettledRequest`，钉乱序完成、被清减旧身份重试不丢不重；完整命令：`go test ./assembly -run '^(TestCompressionCheckpointTracksPendingWork|TestCompressionSliceBodyIsBoundedByTheBatchItOwes|TestCompressionCheckpointPreservesOutOfOrderPendingRequest|TestCompressionCheckpointDoesNotRepeatSettledRequest)$' -race -count=10 -v`。真实入口锚：`go test ./assembly -run '^(TestRestartExecutesTheChainJudgmentOnce|TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint)$' -race -count=10 -v`，覆盖正式 Checkpoint→重启→再次提交；独立审逐项核三种成本，不能用输出小冒充读少。
- [x] 03.R3 对账原3.1–3.4预算、租约、默认值、模型摘要及全部消费者，前置03.R1/R2/R4和04.R1；产出当前复验凭据，保留原历史红绿证据。文件白名单：仅本域 `tasks.md`/`design.md`（复验叶；发现代码偏差另按精确文件集派发）—— 正锚：`go test ./api/config -run '^(TestSummaryBudgetReadsTheDeclaration|TestSummaryBudgetReadsTheDefault)$' -count=1 -v`；负锚：`go test ./assembly -run '^(TestBandSummaryGistReportsAMissingModel|TestCompressionConsumerDegradesAFailedSummary)$' -count=1 -v`；消费者锚：`go test ./tests/integration/host -run '^(TestCompressionSummaryObeysTheDeclaredBudget|TestCompressionSummaryAsksTheLeaseModel|TestCompressPipelineJudgesThenExecutes)$' -count=1 -v`，对应旧3.4未完项全部核销，独立审核真实宿主预算入口，不以核心声明路径替代。

- [x] 03.R4 接通宿主文档摘要预算。产出：`summary_budget_bytes` 从宿主 YAML 原样映射到已有 `AgentSpec.SummaryBudgetBytes`，复用既有默认值与摘要预算消费路径。事实：`assembly/host_document.go:90-117` 的 `hostAgent` 与 `:194-212` 的 `mapDocument` 均缺该字段，核心 `api/config/config.go:94-98` 已有；核心声明测试不能证明宿主入口可用。前置：3.1–3.3 接口核对、03.R1；与 02 的 `host_document.go` 写面串行，不等待 03.R2。精确文件集：`assembly/host_document.go`、`assembly/host_document_test.go`、`assembly/host_test.go`。
  正锚：已落实现 `TestHostDocumentCarriesTheSummaryBudget`（声明值 512 原样映射；缺省沿既有路径）；负锚：已落实现 `TestHostDocumentRefusesAMisspelledSummaryBudget`（`summary_budget_byte` 被严格解码具名拒绝）；真实入口锚：已落实现 `TestHostBootCompressionObeysTheDeclaredSummaryBudget`（合成配置经 `Boot`→`loadHostDocument`→`mapDocument`→真实压缩，落链摘要 ≤512，不在测试中补字段）。命令：`go test ./assembly -run '^(TestHostDocumentCarriesTheSummaryBudget|TestHostDocumentRefusesAMisspelledSummaryBudget|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=10 -v`。独立审逐段追踪配置→默认→提示词/裁剪→产物，并核缺省及租约降级不变。凭据见文末「03.R4 执行记录」；**独立只读审尚未进行**，故本叶仍待审。

- [x] 03.R5 compressSessions 接口隔离（concept_budget 4>3 清偿，不抬基线）：产出＝把 compressSessions 拆为 `compressSessionSource{SessionOf,ProjectionOf}` 与 `compressProductStore{FileCompressProduct,HoldsCompressProduct}` 两窄口（各 2 方法），CompressionConsumer 持两者，ageFoldSessions 实现两者；构造调用方级联更新（含 nil 测试与 resetCompression）。**语义零变化**（身份读/文件产物/投影窗不变），不得改判据/抬 baseline/加例外。写面＝`assembly/compress_consumer.go`、`assembly/age_fold.go`、`assembly/run.go`（构造点）、`assembly/compress_consumer_test.go`、`assembly/summary_gist_test.go`、`assembly/anchored_boot_parity_test.go`、本域 tasks/design。正锚＝03.R2 六锚＋既有压缩锚全绿；负锚＝`check_concept_budget.py` 的 compressSessions 4>3 error 消失（两接口各≤3）；消费者锚＝`go test ./assembly -count=1` 绿＋`python3 scripts/gates/check_concept_budget.py --json` exit0。

## 原执行任务与凭据（历史，不推导当前完成）

> verify 三要素随任务行冻结（R29）。正锚测试名与 `-run` 正则逐字符一致；负锚具名错误路径；消费者锚走正式入口（boot/声明面 = `assembly.Assemble`，assembly/run.go:491；压缩执行 = `CompressionConsumer.Execute`，assembly/compress_consumer.go:200）。
> **与 04（中段票据）合单原子（W3）**：共触 memory/replay/age_segments.go，不可并行派发（一级 design.md:58）。本域只动 `AgeFoldPlan`（:413）、`historySummaryOf`（:603-619）、`ageSummaryTextOf`（:636-674）与调用点 :509。
> P8 与 D6.17 为派发依据（一级 design.md:23、:83）；golden 重分类的 class/kind 约束见 [design.md](design.md) D3.5（scripts/gates/check_window_bounds.py:152-178 实测）。

- [x] 3.1 **摘要预算进声明面（`summary_budget_bytes`）**：`AgentSpec` 加 `SummaryBudgetBytes int`（api/config/config.go:82-90 段，`KeepRecentTasks` 后）；`agentKeys` 加 `"summary_budget_bytes"`（api/config/decode.go:158-165），`decodeAgent` 加 `countField`（对照 :208）；`DefaultSummaryBudgetBytes = 2048` 落 api/config/defaults.go 并在 decode 填缺省（对照 :24/:27、:40/:43）；`FoldWindow`（assembly/age_fold.go:25-33）加 `SummaryBudgetBytes`，`foldWindowOf`（assembly/request.go:606）从 spec 取、`normalizeFoldWindow`（:596）零值补缺省 —— 验证：正锚 `go test ./api/config/ -run 'TestSummaryBudgetReadsTheDeclaration|TestSummaryBudgetReadsTheDefault'`（新增：`summary_budget_bytes: 512` 解为 512；未声明读 `DefaultSummaryBudgetBytes`）＋ `go test ./assembly/ -run TestFoldWindowCarriesTheSummaryBudget`（新增：programmatic `AgentSpec{ReplayWindow:100}` 经 `normalizeFoldWindow` 得缺省，声明值原样透传）；负锚 `go test ./api/config/ -run TestStrictDecodeRefusesEveryUndeclaredShape`（既有 decode_test.go:48）且新增子例 `summary_budget_byte`（拼错）须被 `mapFields` 具名拒绝，SHALL NOT 静默忽略；消费者锚 `go test ./tests/integration/host/ -run TestCompressPipelineJudgesThenExecutes`（既有 compress_pipeline_test.go:351，经 `assembly.Assemble` 走声明面 + 真实压缩流程，本任务后仍绿证明字段未破坏声明面）。

- [x] 3.2 **去 2048 硬界：界经 plan 承载、提示词与裁剪同源、golden 重分类**：`AgeFoldPlan`（memory/replay/age_segments.go:413）加 `SummaryBudgetBytes` 并新增 `summaryBudgetOf(plan)`（并列 :474-478 的 keepRecent/threshold 解析、age_plan.go:81-88 的 `thresholdOf`）；`historySummaryOf(entries, limit)`（:603）与 `ageSummaryTextOf(lines, limit)`（:641）改形参，调用点 :509 与 age_plan.go:115 透传；常量 `DefaultAgeSummaryTextLimit`（:94-100）保留为**策略缺省值**并改写注释（不再是承载界的机制常量，理由见 design D3.2）；assembly/summary_gist.go 的 `bandSummaryInstruction(budget int)`（:40-43，去掉字面 2048）、`bandSummaryRequest(material, budget)`（:88-95）、`boundedSummaryGist(gist, budget)`（:114-124）；预算来源单一：compress_consumer.go:218 已取 `window`，:249 透传给 `summarizeProduct`（:275-291）再交 gist 面；两处 plan 构造（assembly/age_fold.go:175、compress_consumer.go:230-233）同取 `window.SummaryBudgetBytes`；golden/runtime/window_bounds.json 新增 `summary-budget-bytes`（policy/config_key）与 `summary-budget-default`（constant），改写 `age-summary-text-limit`（:108-114）reason 为策略缺省值；docs/wiki/agent/event-flow.md:29 的 2048 叙述同批改为「声明预算、缺省 2048」 —— 验证：正锚 `go test ./memory/replay/ -run TestHistorySummaryObeysThePlanBudget`（新增：同序列 `SummaryBudgetBytes:512` 正文 ≤512、缺省下更长，且两次折叠仍幂等）＋ `go test ./assembly/ -run 'TestBandSummaryPromptStatesTheDeclaredBudget|TestCompressionSummaryCutsToTheDeclaredBudget'`（新增：声明 512 时提示词含 512 且不含 2048；模型答长文本裁到 512）；负锚 把 `ageSummaryTextOf` 改回读常量后同一 `TestHistorySummaryObeysThePlanBudget` 必红（完整回退亦必红，两次输出入 run-notes 供一级 F3）＋ `go test ./memory/replay/ -run 'TestSymptomAnchorCompressionStaysBounded|TestFoldIsIdempotentOverTheSameMaterial'`（既有 age_segments_test.go:1264、fold_test.go:21）保持绿，界的形状与收敛不变；消费者锚 `go test ./tests/integration/host/ -run TestCompressionSummaryObeysTheDeclaredBudget`（新增：`Assemble` + 声明 `summary_budget_bytes: 512` + scripted provider 答长文本 → 落链 `compress.product` 正文 ≤512 字节，走 :249 真实执行链）＋ `python3 scripts/gates/test_check_window_bounds.py; python3 scripts/gates/check_window_bounds.py`（均 exit 0，新条目被校验器认账）。

- [x] 3.3 **摘要请求经租约 + 声明采样参数**：`bandSummaries.SummaryGist`（assembly/summary_gist.go:57-84）取模型改与 `suppliers`（assembly/tools.go:218-227）同一取法——读 `r.agentSpecOf(agent)`（assembly/run.go:996-999），`r.leaseOf(agent)`（:3315）非 nil 走 `withHostModelParams(rl.NewLeaseModel(held.lease), spec)`，否则回落 `withHostModelParams(native.Model, spec)`；缺模型仍答 `ErrNoSummaryModel`（:27）；`bandSummaryRequest` 不自带采样（由 host_model.go:53-70 只填请求未命名项） —— 验证：正锚 `go test ./assembly/ -run 'TestBandSummaryGistReadsTheLease|TestBandSummaryGistCarriesTheDeclaredSampling'`（新增：租约版本模型答 marker A、native 答 marker B → 落链摘要为 A；声明 `Temperature` 指针与 `MaxTokens` 后摘要请求携带该采样）；负锚 `go test ./assembly/ -run 'TestBandSummaryGistReportsAMissingModel|TestCompressionConsumerDegradesAFailedSummary'`（既有 summary_gist_test.go:91、:400：无租约无 native → `ErrNoSummaryModel`；provider 失败/终态非 completed → 保留机械有界型），降级口径不因租约改动放宽；消费者锚 `go test ./tests/integration/host/ -run TestCompressionSummaryAsksTheLeaseModel`（新增：`Assemble` + `assembly.WithModelLease`（run.go:3279）+ `WithLeaseRebuild`，`lease.Replace` 换版后 `ObserveCommit` 驱动的摘要由新版本产出，证明摘要请求真走租约而非裸 native）＋ `go test ./tests/integration/host/ -run TestCompressPipelineJudgesThenExecutes` 保持绿（无租约宿主的既有路径不变）。

- [ ] 3.4 **级联检查与门禁（合单内收尾）**：`grep -rn 'DefaultAgeSummaryTextLimit\|2048\|bandSummaryInstruction\|SummaryGist(\|SummaryBudgetBytes\|summary_budget_bytes' --include='*.go' --include='*.json' --include='*.md' .` 枚举全部消费者逐一回答影响（含 docs/api 生成物与 wiki），确认无第二处字面界限、无遗漏 plan 构造点；`AgeFoldPlan` 的三个字段解析同形（keepRecent/threshold/summaryBudget）；build/test 与 commit 永不串联，验证独立语句分号分隔 —— 验证：`go test ./api/config/ ./memory/replay/ ./assembly/ ./tests/integration/host/` 全绿分号分隔执行；`openspec validate runtime-recovery-ideal --strict` 与本域 spec Scenario 全绿；回退演练后「界不可配置」症状复红（3.2 负锚证据复用）。

## 执行证据（2026-11-15 · cleanup 复核）

- 3.1 正锚 `go test ./api/config/ -run 'TestSummaryBudgetReadsTheDeclaration|TestSummaryBudgetReadsTheDefault'` → `ok`；`go test ./assembly/ -run TestFoldWindowCarriesTheSummaryBudget` → `ok`。负锚 `go test ./api/config/ -run TestStrictDecodeRefusesEveryUndeclaredShape` → `ok`（拼错子例 `summary_budget_byte` 见 decode_test.go:76）。消费者锚 `go test ./tests/integration/host/ -run TestCompressPipelineJudgesThenExecutes` → `ok`。
- 3.2 正锚 `go test ./memory/replay/ -run TestHistorySummaryObeysThePlanBudget` → `ok`；`go test ./assembly/ -run 'TestBandSummaryPromptStatesTheDeclaredBudget|TestCompressionSummaryCutsToTheDeclaredBudget'` → `ok`（提示词无字面 2048，`bandSummaryInstruction` 只打印 budget）。**负锚演练**：`ageSummaryTextOf` 首行改回 `budget = DefaultAgeSummaryTextLimit` → 同一测试红（`summary carries 1996 bytes, want at most the declared 512: the bound comes from the plan`），恢复后绿。保持绿 `TestSymptomAnchorCompressionStaysBounded|TestFoldIsIdempotentOverTheSameMaterial` → `ok`。消费者锚 `go test ./tests/integration/host/ -run TestCompressionSummaryObeysTheDeclaredBudget` → `ok`；`python3 scripts/gates/test_check_window_bounds.py`（10/10 cases passed）与 `python3 scripts/gates/check_window_bounds.py`（ok）均 exit 0。
- 3.3 正锚 `go test ./assembly/ -run 'TestBandSummaryGistReadsTheLease|TestBandSummaryGistCarriesTheDeclaredSampling'` → `ok`；负锚 `TestBandSummaryGistReportsAMissingModel|TestCompressionConsumerDegradesAFailedSummary` → `ok`；消费者锚 `go test ./tests/integration/host/ -run TestCompressionSummaryAsksTheLeaseModel` 与 `TestCompressPipelineJudgesThenExecutes` → `ok`。
- 3.4 未勾：本域四份门禁中 `go test ./api/config/ ./memory/replay/ ./assembly/` 全绿（assembly 273s）、`openspec validate runtime-recovery-ideal --strict` valid、窗口界门禁 exit 0，但 `go test ./tests/integration/host/` 红（11 例，主要因 04 注入前缀改写呈现断言，另有 `TestCompressProductRestoresAfterRestart` 重启重铸 compress 产物）；红项归 04 注入扫尾与非本域重启幂等缺陷，不归 03，详见清理报告。

## 03.R2 停放交接账（本轮零代码写入 · 阻塞待裁）

**结论**：03.R2 的三件事是一枚原子改动，撞到一处白名单外读点，本轮不落代码、03.R2 仍 `[ ]`、不缩验收面。下文均为读码实证，**未跑任何命令**。

**现状复核（与任务行事实一致）**：常驻两账——`assembly/compress_consumer.go:66` 的 `judgments`（filing 序、永久累积，`:178` 追加）与 `:69` 的 `answered`（`:190` 的 `NoteProduct`、`:412` 的 `noteAnswered` 两处写入，永不摘除）。构建面按历史取量——`assembly/compress_checkpoint.go:58` 的 `make(..., 0, len(filed))`、`:104` 的 `make(..., 0, len(consumer.judgments))`、`:105-111` 遍历全部 `judgments`，`compressSliceOf`（`:181-188`）同形。输出只含未完（`owedJudgments` `:59-64` 过滤），故输出小 ≠ 读少 ≠ 常驻小。

**阻塞点（唯一，白名单外一行）**：`assembly/anchored_boot_parity_test.go:254` 的 `answeredCount` 返回 `len(run.compress.answered)`；调用点 `:226`（全走遍＝链上产物数）、`:229`（anchored 非零）、`:237`（fallback 同）。删 `answered` 该包即编译失败 ⇒ 整个 `assembly` 测试包不可跑，连带本叶三锚与并发单 07.R2.1（同包）一起失效。该文件不在本单写面。

**不出白名单但已解的两处**：①`NoteProduct(product)` 签名被 `assembly/skeleton_assert.go:148` 钉住，调用方在 `assembly/run.go:2545/2798/3115/3152`——本叶保持签名，按身份摘除时在 `c.judgments` 各会话切片内定位（O(未完量)），不动 run.go；②`compressAccountOf`/`requireSameAccount` 住本单文件，`assembly/checkpoint_test.go:1505/:2082` 仅经它们取数，删字段后仍可编译，只需把 `TestCompressionJudgmentsSurviveTheBootCut:151` 的 `len(live.Answered) == products` 改为「已结身份遗留 0」（本单文件内）。

**拟定实现（三件原子，均在白名单内可完成）**：
1. `judgments` 收窄为「未答判决，filing 序」：`Record` 去重不变；新增**按身份摘除**（禁前缀截断/最大序号水位，保乱序未完），切片空即 `delete(c.judgments, session)`，使 `len(c.judgments)` ＝有未完的会话数。
2. 删 `answered`（字段＋两处写入）：`pending`/`owedJudgments` 不再过滤，构建遍历与预分配都随未完量。
3. 已答证明按身份读链：`compressSessions` 增 `HoldsCompressProduct(ctx, session, product)`（实现落 `compress_consumer.go` 的 `ageFoldSessions` 方法：先查 live `r.facts[product.ID]`，miss 再 `r.chainHoldsFact`——后者在 `assembly/facts.go:441` 既有，不改）；`Execute` 在 `summarizeProduct`（模型面，`:250`/`:276-291`）**之前**判「产物是否已按身份在链」，命中即摘除、不调模型、不落第二条产物。产物身份可由 (request, `compressionProducer`, fold 产物 key) 算出（`api/event/compress_product.go:74-83`，正文不入标识），故身份读不需扫链。
4. 语义拒绝项：不以最大序号/前缀水位跳过未完；不以 TTL 或上限抹义务；不以「留着 `answered` 当缓存」冒充清减（12 域 `design.md:56` 计费纪律：删 `answered` 换常数成本属伪造达成——本叶的替换是**按身份读链**，非删除）。

**需要授权（二选一，本单不自裁）**：
- **乙（推荐·保验收面）**：`anchored_boot_parity_test.go:254` 一行改为 `run.compress.struckNotes()`，本单在 `compress_consumer.go` 实现 O(1) 计数（仅当一次 note 实际摘除了一条未完判决时自增，重复 note 不自增）；三处断言语义原样保留。
- **甲（删代理指标）**：删 `answeredCount` 与其三处断言，只留 `:232-236` 的 `pending` × `answeredOnChain` 交叉断言（该性质已被直接钉住）；此案形式上少一处验收面，须 12 域主笔按 R29 明示接受。

**已冻结但未落测试的锚**（本轮不写，避免在包不可编译或断言未成立时留红）：`TestCompressionCheckpointTracksPendingWork`、`TestCompressionCheckpointPreservesOutOfOrderPendingRequest`、`TestCompressionCheckpointDoesNotRepeatSettledRequest`，落 `assembly/compress_checkpoint_test.go`；三面读数（构建遍历/分配量、常驻项数、序列化字节）与命令随 03.R2 行不变。

**未做**：零 `.go` 写入；`go build`/`go vet`/`go test` 均未运行（无改动可验）；未勾 03.R2；未动 F/archive。

## 03.R2 停放交接账（第二轮 · R275 乙案已实施，撞走序前提待裁 · 2026-11-18）

**结论**：R275 乙案三件事与四枚锚已全部落地，`go build ./...` / `go vet ./assembly` exit 0，`assembly` 整包（345s）**仅 1 例红**——`TestAnchoredBootParity`口径的 `TestAnchoredBootMatchesFullBoot`（`anchored_boot_parity_test.go:234`，10/10 稳定红）。根因不在本单写面：两条恢复走遍**先 note 产物、后 seat 判决**，被删的 `answered` 恰是唯一与走序无关的"已答"陈述。03.R2 仍 `[ ]`，验收面未缩。

**前提验证（R275 要求先验）**：**成立**。产物身份 = `"cmpout-" + sha256(request + "\x00" + producer + "\x00" + fold 产物 key)[:8]`（`api/event/compress_product.go:74-83`，正文不入标识），故 `Execute` 侧按身份读链不需扫链、不需常驻索引——已实落为 `HoldsCompressProduct` + `holdsCompressProduct`（`assembly/compress_consumer.go:546/:555`，先 `r.facts[minted.ID]`，miss 再 `chainHoldsFact`）。**但该前提不覆盖 seat 侧**：`Record` 只有 request ID，fold 产物 key 只在 plan/产物事实里，seat 时不可算出身份。

**已落改动（四文件，均在白名单内）**：①`judgments` 只留未完，`strikeLocked`（`:222`）按身份摘除、切片空即删会话键（禁前缀截断/水位）；②删 `answered` 字段与两处写入，`owedJudgments`（`compress_checkpoint.go:58`）不再过滤、`compressSlice` 不再按历史长度预分配、`restoreCompressSlice`（`:124`）只 seat 非空判决；③`struckNotes()`（`:243`）O(1) 计数；④`compressAccountOf` 的 `Answered` 改按链读（`settledJudgments`），`TestCompressionJudgmentsSurviveTheBootCut` 的 `len(live.Answered)==products` 改为「已结身份遗留 0」。测试适配：`anchored_boot_parity_test.go:254` 的 `answeredCount` → `struckNotes()`，三处断言语义原样。

**新锚（已落、绿）**：`TestCompressionCheckpointTracksPendingWork`（三面读数 body=1046B/alloc=1352B 每调用/常驻 2 项，settled 0·64·256 三档同值；反向对照：`strikeLocked` 不摘除 → 常驻 66 vs 2 红）、`TestCompressionCheckpointPreservesOutOfOrderPendingRequest`（反向对照：改前缀截断 → 欠 `[r2]` 红）、`TestCompressionCheckpointDoesNotRepeatSettledRequest`（真实 run + 计数会话面；反向对照：去掉预检 → `held==0` 红；忽略答案 → `filed==1` 红）。

**阻塞实证（一场读数，`go test ./assembly -run '^TestAnchoredBootMatchesFullBoot$' -count=1`，临时探针取后已删净）**：`full struck=20 pending=0 / anchored struck=19 pending=1 / fallback struck=0 pending=20`。即 `struckNotes()` **不等于**原 `len(answered)`（fallback 侧 0 vs 20），R275 判定「语义==原 len(answered)」被实测推翻：
- anchored：`assembly/run.go:2798` 的 `r.compress.NoteProduct(product)`（锚产物）跑在 `replayChainPastCut`（`:2800`）之前，而该产物的 request 正落在后者重放的 `behind` 段内（探针：`note-miss cmp-3b93…` 紧接 `record cmp-3b93…`）。其余产物一律 record 先、note 后。
- fallback：`assembly/run.go:2817-2840` 的降级走遍按 `refs` 索引序（新→旧）重放，产物事实恒先于其 request 到达 ⇒ 20 条 note 全部落空、20 条判决全部 seat 成未完。

**生产后果（非仅测试）**：降级走遍后这 20 条产物已在链的判决永久留在未完账——`Execute` 的预检只摘 plan **命名**的判决，而它们的材料早已折完（`len(report.Products)==0` 早退），故永不摘除，且每次 checkpoint 重新序列化。旧 `answered` 虽同样 20 项，但不进 checkpoint、不驱动逐判决读链。这是删除 `answered` 暴露的真实走序依赖。

**已试并被否的白名单内替代（实测后回退，树内 0 残留）**：给 `NoteProduct` 加自清"待判答案"表（note 落空则暂存、`Record` 命中即消费并计入 struck）。锚定侧确实清账（曾让 parity 转绿），但正向走遍里每个产物被 note 两次（record→strike→**dup-note 落空**），暂存表于是永久累积：实测 `parked anchored=19 / full=20`——即换个名字把 `answered` 请回常驻态，与 12 域 `design.md:56` 纪律冲突。已删除该字段与全部探针（`grep -c 'awaiting\|PROBE'` = 0）。

**证明「走序是唯一解」（探针，已回退）**：正向走遍结束后把链上 20 条产物重 note 一遍 ⇒ `pending=0 struck=20`。即把 `run.go:2798` 的 `NoteProduct` 移到该块两次重放之后，锚定侧即自愈（一行移动，无依赖）。

**需要授权（本单不自裁，禁改 run.go）**：
- **A（推荐·保全验收面）**：由 run.go 属主（12 域）落两处走序——①`:2798` 一行移到 `replayChainPastCut` 与 `RestoreProducts` 之后；②`:2817-2840` 降级走遍改按 commit 序重放（注意 `if agent == "" { agent = owner }` 取的是首个到达 ref 的 agent，方向改变会换 agent 来源，须属主自判）。此后 `struckNotes()` 与原语义等价，四枚锚与 parity 三处断言全绿。
- **B（若 A 不受理）**：由 12 域主笔按 R29 明示接受 `anchored_boot_parity_test.go:237`（fallback 面）降级为可成立口径（例如只断言「fallback 欠账 ⊆ 链上产物所答」），属**验收面缩水**，本单不自行宣布。
- 不建议回退本轮改动（`answered` 复活）。

**本轮实跑凭据**：`go build ./...` exit 0；`go vet ./assembly` exit 0；四枚锚 `-race -count=10 -v` → 40 个 `--- PASS`、exit 0；真实入口锚（`TestRestartExecutesTheChainJudgmentOnce`＋`TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint`）`-race -count=10` → exit 0（61.7s）；`TestAnchoredBootMatchesFullBoot -race -count=10` → 10/10 红于 `:234`；`go test ./assembly -run 'TestCompression|TestCompress|TestAnchoredBoot' -count=1` → 仅该 1 例红；`go test ./assembly -count=1`（345s）→ 仅该 1 例红。

**未做**：未勾 03.R2；未动 F/archive；未改 run.go/主 spec/门禁/配置；未 git 写；未跑 `tests/integration/host`（不在本单验证面）。

## 03.R4 执行记录（实施单 · 2026-11-18）

- **改动（代码）**：`assembly/host_document.go:117` 的 `hostAgent` 增字段 `SummaryBudgetBytes int`，yaml 键 `summary_budget_bytes`（位于 `keep_recent_tasks` 之后）；`assembly/host_document.go:208` 的 `mapDocument` 原样透传 `entry.SummaryBudgetBytes`。宿主侧不填默认：未声明时映射 0，缺省仍由 `normalizeFoldWindow`（`assembly/request.go:602-605`）读 `config.DefaultSummaryBudgetBytes=2048`。`git diff -w` 显示该文件净增 2 行；其余 24 行变动为 gofmt 因新字段名长度重排的相邻对齐（无其他语义改动）。
- **新增锚（本单落地）**：`TestHostDocumentCarriesTheSummaryBudget`（`assembly/host_document_test.go:79`，512 原样映射＋未声明映射 0 且折叠窗读默认）、`TestHostDocumentRefusesAMisspelledSummaryBudget`（同文件 `:120`，`summary_budget_byte` 由严格解码具名拒绝，`errors.Is(ErrUnknownConfigKey)` 且错误串含 `agents.user.summary_budget_byte`）、`TestHostBootCompressionObeysTheDeclaredSummaryBudget`（`assembly/host_test.go:423`，合成文档经 `Boot`→`loadHostDocument`→`mapDocument`→真实折叠与模型摘要；测试不改写任何字段）。
- **凭据（本轮实跑，契约文件均在案）**：
  - 三锚组合 `go test ./assembly -run '^(TestHostDocumentCarriesTheSummaryBudget|TestHostDocumentRefusesAMisspelledSummaryBudget|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=10 -v` → **exit 0**，命中 **30** 个 `--- PASS`（3 叶×10）、**0** 个 `--- FAIL`（输出 `/tmp/hostbudget10.log`）。
  - 既有预算消费锚 `go test ./assembly -run 'TestFoldWindowCarriesTheSummaryBudget|TestSummaryBudget' -count=1 -v` → **exit 0**，`TestFoldWindowCarriesTheSummaryBudget` PASS；`go test ./api/config -count=1` → **exit 0**。
  - `go build ./...` → **exit 0**。
  - **反向对照（证明非空转）**：临时删去 `mapDocument` 的透传行后同命令复跑 → `TestHostDocumentCarriesTheSummaryBudget` 报「the mapped declaration carries the budget 0, want the declared 512」，`TestHostBootCompressionObeysTheDeclaredSummaryBudget` 报「the history summary carries 2048 bytes, want at most the declared 512」（即缺省 2048 落链）；恢复该行后复绿（`/tmp/r4_recheck.log` exit 0）。
- **整包与红项归属**：`go test ./assembly -count=1` → exit 1，**仅 1 例 FAIL**：`TestUnmountKeepsTheOwedItemsOfItsSlice`（`assembly/checkpoint_test.go:3461`，卸载后未完项丢失）。该例是已知 S5 有意红锚（一级 ORCHESTRATION.md:108「最后一次全量 assembly 仅 S5 红」；12.R3 承接）。**归属甄别**：把本单三文件还原为 `HEAD` 内容后同用例仍红（`/tmp/s5_baseline.log` exit 1）⇒ 非本单引入；同树另有并发单在改 `assembly/checkpoint_test.go`，其红不归本单。本单相关用例在整包中全绿，无非预期红。
- **在飞瞬态（收尾时观察到，归因非本单）**：收尾复核时 `go test ./assembly` 因测试包编译失败而无法执行——`assembly/facts.go`（并发单写面，`git status` 为 `M`）已把 `persistFact` 改为返回 `(bool, error)`，而 `assembly/facts_test.go` 尚未同步（4 处 assignment mismatch；`/tmp/r4_final.log` 与 30s 后重试的 `/tmp/r4_final2.log` 同）。同刻 `go build ./...` **exit 0**，本单三文件不在报错面。上文绿凭据取自此改动落地之前的完整运行（`/tmp/hostbudget10.log`、`/tmp/r4_recheck.log`）。按派发约定不改其文件、不以其红否定本单。
- **未验面（如实记录）**：①未跑 `-race`；②未跑 `tests/integration/host`（宿主文档落链证据由本单 `Boot` 锚覆盖，集成侧复验归 03.R3）；③未做独立只读审（本域纪律要求非实施者审三锚与受影响调用方），故本叶 checkbox 表示「实现与自验完成」，**放行仍待审**；④未改 docs/生成物：`docs/wiki/platform/org-hot-reload.md:45` 的指纹叙述未列 `summary_budget_bytes`（属既有叙述滞后，写面不在本单，另报）。

## 03.R2 收口记录（第三轮 · R278 裁定A案落地 · 2026-11-18）

**结论**：03.R2 的实现与自验完成，checkbox 勾选表示该口径；**独立只读审未做，放行仍待审**（本域纪律要求非实施者审三锚与受影响调用方）。验收面一字未缩。

**改动（两处走序，落在 12 域 `assembly/run.go` 写面，由一级 ORCHESTRATION R278「裁定取A」授权）**：
1. `assembly/run.go:2798` 的 `r.compress.NoteProduct(product)` 后移到 `replayChainPastCut`（`:2802`）与 `RestoreProducts`（`:2806`）之后。原序先 note 后 seat：锚产物所答判决落在 `replayChainPastCut` 重放的 behind 段内，note 时账户尚未持有该判决 ⇒ 落空、判决留未完（实测 anchored 19/1）。
2. `assembly/run.go` 降级走遍不再按索引遍历方向重放，改为先按 `ref.seq` 升序（提交序）排序再逐条重放。原实现 `for i := len(refs)-1; i>=0; i--` 只有在遍历方向确为「新→旧」时才是提交序；实测本 fixture 的空类型索引给出的是升序，于是重放变成新→旧，产物恒先于其请求到达 ⇒ 20 条 note 全落空（实测 fallback 0/20）。

**探针实测（临时探针已删净，`grep -c 'PROBEWALK\|PROBEFACES'` = 0）**：
- 降级走遍实际遍历序 `seq=88,87,86,…`（新→旧），相邻对恒为「`cmpout-…`(产物) 在前、`cmp-…`(其请求) 在后」⇒ 方向即缺陷根因，非 note 逻辑缺陷。
- `anchorRef.typ` 在该索引走遍下为空（`blankTypedTier` 注释所述「index carries no frozen type」），故走序不能靠 typ 判，只能靠 seq。
- **agent 来源验证（R278 要求）**：该 slot 全部 ref 经 `replayRef` 返回 `owner="agent-a"`，70 条无一为空、无第二个取值。故 `if agent == "" { agent = owner }` 虽把「首到达」从最新一条换成最旧一条，取值不变（同 slot 即同 partition，`partition = memory.PartitionIDFromName(agent)`）。独立佐证：`TestAnchoredBootMatchesFullBoot` 的 `fallbackSeeds[addr]` 按 `{AgentID:"agent-a", SessionID:"session-anchored-boot"}` 取回成功，agent 若变该断言即红。机制未改（未改为从产物/判决 owner 显式取）。

**三面读数（同一 fixture，`test.Logf` 探针，已删）**：修复前 full struck=20/pending=0、anchored 19/1、fallback 0/20；修复后 **full 20/0、anchored 20/0、fallback 20/0**。

**凭据（本轮实跑，逐条 exit 码与命中数）**：
- `go build ./...` → **exit 0**；`go vet ./assembly` → **exit 0**；`gofmt -l assembly/run.go` 输出空。
- `go test ./assembly -run '^TestAnchoredBootMatchesFullBoot$' -race -count=10 -v` → **exit 0**，**10/10 `--- PASS`**，0 FAIL。
- 03.R2 六锚 `go test ./assembly -run '^(TestCompressionCheckpointTracksPendingWork|TestCompressionSliceBodyIsBoundedByTheBatchItOwes|TestCompressionCheckpointPreservesOutOfOrderPendingRequest|TestCompressionCheckpointDoesNotRepeatSettledRequest|TestRestartExecutesTheChainJudgmentOnce|TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint)$' -race -count=10 -v` → **exit 0**，**60 PASS**（逐名 10/10，0 FAIL，66.7s）。
- A 恢复五锚 `-race -count=5` → **exit 0**，**25 PASS**（逐名 5/5，17.7s）。
- `go test ./assembly -count=1` 全量 → **exit 0**（302.2s，0 FAIL）。上一轮 03.R4 记录里的 S5 红（`TestUnmountKeepsTheOwedItemsOfItsSlice`）与墙钟噪声红本轮均未出现。
- **红转绿因果（非空转证明）**：仅修 ① 时红在 `anchored_boot_parity_test.go:234`（anchored 面）；①落地后红位移到 `:238`（fallback 面）；②落地后全绿。三态逐一实测，非一次成型。

**未改面（硬约束遵守）**：`anchored_boot_parity_test.go` 三处语义原样（仅沿用上一轮 `answeredCount`→`struckNotes()` 适配，本轮零改动，`git diff` 仅该 4 行）；未把 `answered` 请回常驻态（12-design:56）；`NoteProduct` 签名未动（`skeleton_assert.go:148` 钉住）；未改主 spec/门禁/配置/权限；未 git 写；未读真实数据根。

**未验证面（如实记录）**：① fallback 面只有经生产函数 `seedAnchoredReplay` 的直接调用（`blankTypedTier` 构造无冻结类型索引），无 Boot 级用例——生产入口 `Boot`→`seedReplay`（run.go:708）→`seedAnchoredReplay`（run.go:2461）同一实现，但「空类型索引经 Boot 触发降级」这条路径无端到端锚；anchored 面有 Boot 级锚 `TestDefaultBootRebuildsFromTheAnchor`（绿）。② 未跑 `tests/integration/host`（不在本单验证面）。③ 其余 tier（memory kv 的 `indexKeyOrder`/`CommitOrder` 两条走遍分支）的遍历方向未各自实测，②按 seq 排序对方向不敏感，故不依赖该实测。

## 03.R3 复验记录（复验核销叶 · 2026-11-18 · 零代码写入）

**结论**：原 3.1–3.4 的四要素（预算进声明面、去硬界、租约＋声明采样、级联与门禁）在现树逐项成立，代码零偏差；旧 3.4 未完项（四包门禁，含 `tests/integration/host` 红）本轮已消。**保留原历史红绿证据不改写**（3.2 负锚回退演练、3.4 回退演练仍以历史记录为准，本轮未重做——重做需改 `.go`，越本叶写面）。两项文档叙述滞后与一项无锚场景见下文偏差登记，均非代码偏差，**不在本叶白名单内，登记待编排者派发**。

### 对账表（原要求 → 现存实现 → 验证 → 核销）

| 原要求 | 现存实现（file:line） | 验证 | 核销 |
|---|---|---|---|
| 3.1 声明键进声明面 | `api/config/config.go:94-98`（`AgentSpec.SummaryBudgetBytes`）；`api/config/decode.go:158-163`（`agentKeys` 在 `keep_recent_tasks` 后）、`:212`（`countField`）；`api/config/defaults.go:29-30`（与两条既有策略同形的缺省填充）、`:47-50`（`DefaultSummaryBudgetBytes = 2048`）；`api/config/skeleton_assert.go:46`（字段元数编译级钉住） | 正锚 `TestSummaryBudgetReadsTheDeclaration`（512 原样）＋`TestSummaryBudgetReadsTheDefault`；负锚 `TestStrictDecodeRefusesEveryUndeclaredShape` 的 `agents[0].summary_budget_byte` 子例（`decode_test.go:76-77`） | 核销 |
| 3.1 折叠窗承载预算 | `assembly/age_fold.go:33-36`（`FoldWindow.SummaryBudgetBytes`）；`assembly/request.go:603-604`（`normalizeFoldWindow` 零值补缺省）、`:617`（`foldWindowOf` 原样透传） | `TestFoldWindowCarriesTheSummaryBudget`（`assembly/request_test.go:1565`） | 核销 |
| 3.1 **真实宿主**预算入口（03.R4） | `assembly/host_document.go:117`（`hostAgent` yaml 键）、`:208`（`mapDocument` 原样透传，宿主不填默认）；缺省仍由 `request.go:603-604` 读 `config.DefaultSummaryBudgetBytes` | `TestHostDocumentCarriesTheSummaryBudget`／`TestHostDocumentRefusesAMisspelledSummaryBudget`／**`TestHostBootCompressionObeysTheDeclaredSummaryBudget`（Boot→loadHostDocument→mapDocument→真实折叠与模型摘要）** | 核销（不以核心声明路径替代：宿主文档面单独取绿，见下「宿主入口」条） |
| 3.2 界由 plan 承载、常量降为策略缺省 | `memory/replay/age_segments.go:423-426`（`AgeFoldPlan.SummaryBudgetBytes`）、`:94-103`（常量改述为策略缺省）；`memory/replay/age_plan.go:83-88`（`summaryBudgetOf`，与 `thresholdOf:75-80` 同形）、`:98`/`:124`；`historySummaryOf`（`age_segments.go:611`）与 `ageSummaryTextOf`（`:650`）已改形参；调用点 `:487`/`:517` | 正锚 `TestHistorySummaryObeysThePlanBudget`（512 紧、缺省松、二次折叠同文）；保持绿 `TestSymptomAnchorCompressionStaysBounded`＋`TestFoldIsIdempotentOverTheSameMaterial` | 核销（原负锚「改回读常量必红」为历史证据，本轮未重做） |
| 3.2 提示词与裁剪同源、无第二处字面界 | `assembly/summary_gist.go:43-52`（`bandSummaryInstruction(budget)`，无字面 2048）、`:106`（`bandSummaryRequest(material, budget)`）、`:133`（`boundedSummaryGist(gist, budget)`）；`assembly/compress_consumer.go:345-354`（`summarizeProduct(..., budget)`→`SummaryGist(..., budget)`→`boundedSummaryGist`）、`:319`（透传 `window.SummaryBudgetBytes`）；`assembly/compress_consumer.go:290-294` 与 `assembly/age_fold.go:179-183` 两处 plan 同取 `window.SummaryBudgetBytes` | 正锚 `TestBandSummaryPromptStatesTheDeclaredBudget`（声明 512 时提示词含 512 且不含 2048）＋`TestCompressionSummaryCutsToTheDeclaredBudget`（响应裁到 512） | 核销 |
| 3.2 golden 重分类 | `golden/runtime/window_bounds.json:59`（`summary-budget-bytes`/policy/config_key）、`:93`（`summary-budget-default`/constant）、`:120`（`age-summary-text-limit` reason 改为策略缺省值） | `python3 scripts/gates/check_window_bounds.py` exit 0（4 classes／7 declared／20 constants／69 keys）；`python3 scripts/gates/test_check_window_bounds.py` 10/10 | 核销 |
| 3.3 摘要请求经租约＋声明采样 | `assembly/summary_gist.go:67-92`（`agentOfSessionLocked`＋`natives`＋`leases`；`:83` `rl.NewLeaseModel(lease.lease)`、无租约回落 `native.Model`、`:89` `withHostModelParams(serving, spec)`）；`:27` `ErrNoSummaryModel` 判据未放宽 | 正锚 `TestBandSummaryGistReadsTheLease`＋`TestBandSummaryGistCarriesTheDeclaredSampling`；负锚 `TestBandSummaryGistReportsAMissingModel`＋`TestCompressionConsumerDegradesAFailedSummary`；消费者锚 `TestCompressionSummaryAsksTheLeaseModel` | 核销 |
| 3.4 级联枚举 | 见下「消费者枚举」；生产侧字面 `2048` 只剩两处「策略缺省值」定义（`api/config/defaults.go:50`、`memory/replay/age_segments.go:103`），无第三处承载界；`AgeFoldPlan` 三字段解析同形（`age_plan.go:75-88`） | 见下命令 | 核销（两项文档叙述滞后另计） |
| 3.4 门禁四包 | — | `go test ./api/config`、`./memory/replay`、`./assembly`、`./tests/integration/host -short` 逐包 exit 0；`openspec validate runtime-recovery-ideal --strict` valid；窗口界门 exit 0 | 核销（旧 3.4 的 host 面红已消；非 `-short` 全包属长测未跑，见未证面） |

### 冻结三锚（本叶 verify 面，逐条实跑）

- **正锚** `go test ./api/config -run '^(TestSummaryBudgetReadsTheDeclaration|TestSummaryBudgetReadsTheDefault)$' -count=1 -v` → **exit 0**，2 `--- PASS`／0 FAIL（`/tmp/r3_pos.log`）。
- **负锚** `go test ./assembly -run '^(TestBandSummaryGistReportsAMissingModel|TestCompressionConsumerDegradesAFailedSummary)$' -count=1 -v` → **exit 0**，2 顶层 `--- PASS`＋2 子例 PASS／0 FAIL（`/tmp/r3_neg.log`；后者具名副例「the provider failed the stream」「the stream ended beside a terminal」）。
- **消费者锚** `go test ./tests/integration/host -run '^(TestCompressionSummaryObeysTheDeclaredBudget|TestCompressionSummaryAsksTheLeaseModel|TestCompressPipelineJudgesThenExecutes)$' -count=1 -v` → **exit 0**，3/3 `--- PASS`、0 FAIL、0 SKIP（`/tmp/r3_cons.log`）。三枚均 `t.TempDir()`／`ledger.StorageMemory`＋scripted provider（`entryModel`），**不需真实数据根、不需 `--source` 根、不需凭证、非长测**，故可跑（`summary_budget_test.go:52-70`、`compress_pipeline_test.go:356-370`）。

### 宿主入口（不以核心声明路径替代）

消费者锚两枚经 `assembly.Assemble`（正式装配入口，run.go:491）＋`config.Declaration.Agents[].SummaryBudgetBytes` 进入，属**核心声明面**；宿主 YAML→预算→真实压缩链由 03.R4 三锚单独取绿（本轮实跑）：

`go test ./assembly -run '^(TestHostDocumentCarriesTheSummaryBudget|TestHostDocumentRefusesAMisspelledSummaryBudget|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=1 -v` → **exit 0**，3/3 PASS。其中 `TestHostBootCompressionObeysTheDeclaredSummaryBudget`（`assembly/host_test.go:418-434`）走 `Boot`→`loadHostDocument`→`mapDocument`→真实折叠与模型摘要，测试不改写任何字段 ⇒ 「宿主文档声明 512 → 落链摘要 ≤512」在宿主入口成立，非由核心声明测试替代。

### 支撑锚（原 3.1–3.3 行内锚，本轮实跑）

- `go test ./assembly -run '^(TestFoldWindowCarriesTheSummaryBudget|TestBandSummaryPromptStatesTheDeclaredBudget|TestCompressionSummaryCutsToTheDeclaredBudget|TestBandSummaryGistReadsTheLease|TestBandSummaryGistCarriesTheDeclaredSampling|TestHostDocumentCarriesTheSummaryBudget|TestHostDocumentRefusesAMisspelledSummaryBudget|TestHostBootCompressionObeysTheDeclaredSummaryBudget)$' -count=1 -v` → **exit 0**，8/8 PASS／0 FAIL（`/tmp/r3_asup.log`；即 3.1–3.3 行内五锚＋宿主三锚同批）。
- `go test ./memory/replay -run '^(TestHistorySummaryObeysThePlanBudget|TestSymptomAnchorCompressionStaysBounded|TestFoldIsIdempotentOverTheSameMaterial)$' -count=1 -v` → **exit 0**，3/3 PASS（`/tmp/r3_replay.log`）。
- `go test ./api/config -run '^TestStrictDecodeRefusesEveryUndeclaredShape$' -count=1 -v` → **exit 0**，29/29 子例 PASS（含 `summary_budget_byte` 拼错具名拒绝，`/tmp/r3_dec.log`）。

### 门禁（旧 3.4 未完项）

- `go test ./api/config ./memory/replay -count=1` → **exit 0**（0.538s／0.319s，`/tmp/r3_full_cr.log`）。
- `go test ./assembly -count=1` → **exit 0**，322.57s，0 FAIL（`/tmp/r3_full_assembly.log`）。
- `go test ./tests/integration/host -count=1 -short` → **exit 0**，201.73s，0 FAIL（`/tmp/r3_full_host_short.log`）——**旧 3.4「host 面 11 红」在现树已消**。
- `python3 scripts/gates/check_window_bounds.py` → exit 0（ok）；`python3 scripts/gates/test_check_window_bounds.py` → exit 0（10/10 cases passed）。
- `openspec validate runtime-recovery-ideal --strict` → exit 0（valid）。

### 消费者枚举（原 3.4 的级联答案）

`SummaryBudgetBytes`／`summary_budget_bytes`／`DefaultAgeSummaryTextLimit` 的生产消费面共 5 层，逐层已答：①声明层 `api/config`（config/decode/defaults）；②**配置指纹** `api/config/fingerprint.go:70-73`（字段带 `omitempty`，登记进 `canonicalAgentCore` `:216`）——声明改预算会换代重发；③折叠窗与判定 `assembly/request.go:603-617`、`assembly/age_fold.go:179-183`；④执行与模型面 `assembly/compress_consumer.go:290-294/:319/:345-354`、`assembly/summary_gist.go:43-133`；⑤宿主文档 `assembly/host_document.go:117/:208`。纯读层 `memory/replay` 不读声明层（`age_plan.go:83-88` 只读 plan），符合 D3.2。生成物 `docs/api/*.md` 已随注释重跑同步（`github_com__SpellingDragon__tagent__memory__replay.md:95-104`、`__api__config.md:51-54`）。

### 偏差与未证面（如实登记，本叶未改）

1. **文档叙述滞后（代码无偏差）**：`docs/wiki/agent/event-flow.md:29` 仍写「按 `DefaultAgeSummaryTextLimit`（2048 字节…）裁到界内」，与现状（按声明预算裁，`compress_consumer.go:319→345-354`）不符——原 3.2 行内「docs/wiki/agent/event-flow.md:29 同批改」标 [x] 但现树未落地。**精确待派文件：`docs/wiki/agent/event-flow.md`（仅 §五 该句）**。
2. **文档叙述滞后（03.R4 已登记，本轮复核仍在）**：`docs/wiki/platform/org-hot-reload.md:45` 列 agent 参与指纹的字段时未含 `summary_budget_bytes`（实际在 `fingerprint.go:216`）。**精确待派文件：`docs/wiki/platform/org-hot-reload.md`（仅 §二 该句）**。
3. **spec 场景无具名锚**：本域 spec「界无硬编码来源」的 AND 子句（预算小于正文形状预留时正文仍不超预算且折叠不失败）无测试钉住；行为由 `ageSummaryTextOf` 的 `room := budget - ageSummaryDropReserve; if room < 0 { room = 0 }` 与末行 `cutBytes(..., budget)` 按构造成立（`age_segments.go:650-680`），但无锚。**待派为空缺锚（assembly/memory/replay 测试文件），非代码缺陷**。
4. **未跑面**：`tests/integration/host` 非 `-short` 全包未跑（含 `soak_continuity_test.go` 驱动的真进程 deployment soak，属长测需授权）；本轮以 `-short` 全包＋三枚具名消费者锚取证。历史回退演练（3.2 负锚、3.4 回退复红）未重做。

**硬约束遵守**：本叶零 `.go` 写入、零 git 写、未 archive、未勾 F、未编辑一级 ORCHESTRATION/tasks/design/spec、未读真实数据根、未触碰外来未跟踪目录及其门脚本、未抬 baseline／放宽界／删谓词。

## 03.R5 执行记录（实施单 · R297 裁定落地 · 2026-11-18）

### 产出：两窄口（语义零变化，只改归属不改方法体）

`assembly/compress_consumer.go:76-98` 的 4 方法 `compressSessions` 拆为两接口：

- `compressSessionSource`（`compress_consumer.go:76-87`）＝ `SessionOf` + `ProjectionOf`，消费者读一次提交所经的回放源。
- `compressProductStore`（`compress_consumer.go:89-104`）＝ `FileCompressProduct` + `HoldsCompressProduct`，消费者落一次执行所经的产物账本。

`CompressionConsumer` 字段 `sessions compressSessionSource` / `products compressProductStore`（`:57-58`）；`NewCompressionConsumer(sessions compressSessionSource, products compressProductStore)`（`:117`）；`ObserveCommit`/`Execute` 内部改调 `c.sessions.SessionOf|ProjectionOf`、`c.products.HoldsCompressProduct|FileCompressProduct`（`:176`/`:287`/`:317`/`:339`），判据、身份读（`holdsCompressProduct`：live ledger→`chainHoldsFact`）、文件产物与投影窗逻辑一字未动；nil 守卫由单面扩为两面（两参数下 nil 只可能成对出现，行为等价）。方法本体留在原处：`ageFoldSessions.FileCompressProduct/HoldsCompressProduct`（`compress_consumer.go:539-548`）、`ageFoldSessions.SessionOf/ProjectionOf`（`age_fold.go:223/238`）；归属由 `skeleton_assert.go:141-144` 的两条断言声明。

### 构造调用方级联清单（全量 grep `NewCompressionConsumer(`，无遗漏）

| 调用点 | 角色 | 改法 |
|---|---|---|
| `assembly/compress_mount.go:23` | **生产构造点**（唯一） | `face := ageFoldSessions{run: r}`；`NewCompressionConsumer(face, face)` |
| `assembly/anchored_boot_parity_test.go:249` | `resetCompression` | `NewCompressionConsumer(run.compress.sessions, run.compress.products)` |
| `assembly/compress_checkpoint_test.go:293/361/463` | 无面消费者 | `(nil, nil)` |
| `assembly/compress_checkpoint_test.go:555-556` | 计数替身 | 替身改 embed `compressProductStore` 并只覆写两 product 方法；`NewCompressionConsumer(face, counted)`（回放源不被计数，语义与改前“只数产物面”一致） |
| `assembly/compress_consumer_test.go:132`、`assembly/summary_gist_test.go:117/156/169` | 无面消费者 | `(nil, nil)` |

`assembly/run.go` **无构造点**（`r.compress` 由 `compress_mount.go` 装），本叶未动 run.go。`compress_consumer_test.go:23-28`、`compress_checkpoint_test.go:63/512` 的 `consumer.sessions` 字段读写因字段名与语义未变而无需改。

**白名单外必改落点（编译强制，已登记）**：`assembly/compress_mount.go`（任务行误记为 run.go，实为此文件）、`assembly/skeleton_assert.go`（两接口断言＋构造签名断言）、`assembly/compress_checkpoint_test.go`（替身 embed 与新构造）。三者均为签名级联，不含判据/断言改动。

### 冻结三锚（逐条实跑，原始 exit）

- 正锚（03.R2 六锚）`go test ./assembly -run '^(TestCompressionCheckpointTracksPendingWork|TestCompressionSliceBodyIsBoundedByTheBatchItOwes|TestCompressionCheckpointPreservesOutOfOrderPendingRequest|TestCompressionCheckpointDoesNotRepeatSettledRequest|TestRestartExecutesTheChainJudgmentOnce|TestCompressionJudgmentDoesNotExecuteTwiceAcrossCheckpoint)$' -race -count=10` → **exit 0**（`ok … 66.298s`）。
- 支撑锚（既有压缩/summary 面）`go test ./assembly -run 'TestCompression|TestBatchCheckpointMatchesPostBatchProjection|TestRestart' -race -count=2` → **exit 0**（138.439s）。
- 负锚 `python3 scripts/gates/check_concept_budget.py --json` → **exit 0**；`errors: []`（改前 exit 1，唯一 error `S1 iface-methods assembly/compress_consumer.go:compressSessions: 4 methods > 3 (…; 1 implementer(s)) (not baselined)`）；`findings.iface-methods` 15→14；两新接口各 2 方法，不产生 finding；`baseline_stale: 0`（未抬 baseline、未加例外、未删谓词）。
- 消费者锚 `go test ./assembly -count=1` → **exit 0**（314.478s，0 FAIL）；`go build ./...` → **exit 0**；`go vet ./assembly` → **exit 0**；`gofmt -l assembly/*.go` → 空。

### 偏差与未证面（如实登记）

1. **生产构造点错记**：任务行写面写 `assembly/run.go`（构造点），实为 `assembly/compress_mount.go:23`；run.go 内无 `NewCompressionConsumer` 调用。已按实文件改，run.go 零改动。
2. **白名单外三文件**：见上“白名单外必改落点”，均为接口签名的编译级联，不涉判据。
3. **未验面**：`docs/api/github_com__SpellingDragon__tagent__assembly.md:676` 仍写旧签名（`gen_godoc.sh --check` 在本叶改前即 exit 1、现仍 exit 1，多个包已 stale，属既有状态；`docs/api` 不在本叶白名单，未重生成）。若一级要求 docs/api 同步，需另派 `scripts/gates/gen_godoc.sh`。
4. **独立只读审未做**：本叶仅自证三锚，独立审与 03.R1–R4 同批待派。

**硬约束遵守**：零 git 写、未 archive、未勾 F、未编辑一级 ORCHESTRATION/tasks/design/spec、未读真实数据根、未触外来未跟踪目录、未抬 baseline／未加例外／未删谓词、未改 `HoldsCompressProduct` 身份读语义、未改判据或断言语义。


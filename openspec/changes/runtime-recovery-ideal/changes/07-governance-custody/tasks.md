# 07 governance 托管 · 孙任务

## 当前状态（本轮勘误）

07.R1/R2 仍待验。N16/N17/N18 与 denial 载荷清减已有正确性修复及历史红绿证据；不等于治理有效态、内存和恢复成本已收敛。最新裁决取**按身份读链上已决 answer**（design §十五之补），但 `approval_store.go:174` 仍陈述已决项、`goal.go:179` 仍陈述全部目标，`attention_log.go:83/:117` 绑定仍全扫。剩余清减统一落在下方 07.R2.1–07.R2.3，不另起域。

本轮仅改计划，所有下列验证命令未运行；拟新增锚不当作已存在或已红。旧 3.1–3.4 与执行记录保留为历史，不推导当前完成；其中「只剩消费者锚未跑」已不足以描述当前缺口，以本轮勘误为准。

## 原执行任务（历史，未完验收保留）

> verify 三要素随任务行冻结（R29）。正锚测试名与 `-run` 正则逐字符一致；负锚具名错误路径；消费者锚正式入口实测。**W2 并行**：触碰集 features/evolution/governance + assembly/host.go:608 + runtime/task/approval，与 boot 路径簇（01+02+04）不相交（一级 D4）；前置 W1 绿。承一级 D6.5 / §五（Stateful 接缝签名不动）。

- [x] 3.1 governance 收编为一个 Stateful 切片，携尾 seq（denial/goal 两流）+ 审批态 + 派生索引（实现 `StateName/SnapshotState/RestoreState`，runtime/state/checkpoint.go:36；`SnapshotState` 纯读，不 flush/commit/等自身提交锁）—— 验证：正锚 `go test ./features/evolution/governance/... -run TestGovernanceSliceSnapshot`（快照携两流尾 seq + 审批态，RestoreState 后派生索引逐项还原）；负锚 `SnapshotState` 在持自身提交锁时调用 SHALL 不阻塞（接缝契约 checkpoint.go:30-35，纯读非 flush）；消费者锚 `Run.RegisterStateSlice`（assembly/checkpoint.go:284）注册后 `StateRegistry().Names()` 含 governance 切片名（与 6 既有切片并列）。—— **待复验（本轮）**：切片在树内且携尾 + 全部记录/目标/审批证据；「有界于开放项」不成立（design E07-1/E07-2），本条锚须在 07.R2 后复跑仍绿，且载荷形状会变（追加字段，不改三方法）。
- [x] 3.2 `seedEventSeq`（denial.go:327）/`replayGovernanceEvents`（:304）/`rebuildFromStore`（denial.go:230 / goal.go:336）改读恢复后切片 + 尾增量，消 `log.Scan` 全流双扫；`seedEventSeq` 切片缺失/损坏改具名拒绝，取代 `Scan` 错即 `return 0`（denial.go:333）静默重置 —— 验证：正锚 `go test ./tests/integration/capabilities/... -run TestGovernanceRecovery`（双扫恢复回归锚绿，读切片结果与全扫逐项一致）；负锚 切片缺失/损坏 → 具名拒绝 boot，`seedEventSeq` SHALL NOT `return 0` 致尾 seq 重置 re-key（违 `session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；消费者锚 `Bind`（denial.go:128 / goal.go:132）恢复路径不再调 `log.Scan` 全流（Scan 调用计数为 0，尾 seq 取自切片）。—— **待复验（本轮）**：锚成立；但 `Query(0)`/`Count()` 的语义边界未定（design §七 题 3），本条的「读切片结果与全扫逐项一致」在索引成窗口后须重立。
- [x] 3.3 生产接线：host.go:621 `governance.Open` 挂 `WithLedger`(:624)+`WithGoals`(:625)（两者定义 policy.go:187/:166）+ 经 `Run.RegisterStateSlice` 注册 governance 切片；审批簿 `rebuild`（approval_store.go:481）的 `os.ReadDir`（:485）目录扫收编为切片派生 restore + 尾回放（保留 :512-525 attention 链对账，不复活已结/过期请求）—— 验证：正锚 `go test ./assembly/... -run TestGovernanceSliceWiredInProduction`（真实宿主 boot 后 governance 切片在生产状态接缝注册，ledger/goals 非 nil）；负锚 审批簿 boot 恢复 SHALL NOT 走 `os.ReadDir` 全目录扫（rebuild 读切片，目录扫调用计数为 0）；消费者锚 正式宿主入口 host.go:575/:576 建 ledger/goals、:621 `governance.Open` 挂 `WithLedger`(:624)+`WithGoals`(:625)、:799 `host.seatGovernance` → `assembly/governance_checkpoint.go:64` `seat.Bind(ctx, log)` → `governance/checkpoint.go:172/:177` `ledger.Bind`/`goals.Bind`（bind 报错即 `host.abortBoot`），且切片经 `Run.RegisterStateSlice` 注册（非测试手工补接线）；`WithLedger`/`WithGoals`/`ledger.Bind`/`goals.Bind` 生产调用方 ≥1（grep 实证脱离测试态；`BindStore` 兼容面已按 D6.14 退役，见 05·5.3.5）。—— **待复验（本轮）**：接线在树内（实测 host.go:621/:624/:625、:773，旧引 608-610 已漂移）；但审批簿 `rebuild` 的目录扫（approval_store.go:570/:574）仍存在，仅在「有切面」时不走——12.R3 的卸载/替换交接会改这条路径的存在前提，须复验。
- [x] 3.4 负锚 + 消费者锚收口：全扫回归→红 / seq 重置→红 / 真实宿主重启 governance 读切片 —— 验证：正锚 `go test ./tests/integration/capabilities/... ./assembly/... -run 'TestGovernanceRecovery|TestGovernanceSliceWiredInProduction'`（读切片恢复 + 生产接线全绿）；负锚 故意把 `seedEventSeq`/`rebuild` 回退为 `log.Scan`/`os.ReadDir` 全扫 → 有界恢复锚红，故意令切片缺失静默 `return 0` → seq 连续性锚红（re-key 检出）；消费者锚 真实宿主入口重启（host boot → `Run.RegisterStateSlice` → RestoreState → 尾回放）governance 读切片非全流，审批态恢复与运行期一致（`TestGovernanceRecovery` 逐项对齐）。—— **待复验（本轮）**：三锚成立；新增两条边界（有效期跨重启、历史查询边界）不在本条验收面内，由 07.R1 立、07.R2 实现，本条不得被读成「已覆盖有效期与查询边界」。

---

# 本轮重整孙任务（07.R1 / 07.R2）

> 当前行动单集中在本节，历史测试状态见原记录；本节的「拟新增、未运行」专指主树尚缺的验收。07.R1 → G2（与 05.R1、12.R1 共用判据表）→ 07.R2，J1 对齐 12.R2；共享面串行。每叶文件集相对仓库根，另含本域 `tasks.md`/`design.md`，仅为后续实施白名单，本轮不实施。结账逐名核每枚冻结锚命中数 > 0、原始退出码与修复前后证据，组合命令部分命中不算通过；每叶修复后由非实施者独立只读审查三锚、TTL/身份和真实入口，列范围与未证项。共同门仍为 `go build ./...`；不与提交串联。

- [ ] **07.R1 设计门 G2（治理域）：有效态 / 审批有效期跨重启 / 历史查询边界**。产出：§七三题判据与反例、与 05.R1/12.R1 共用的 G2 表、`Query(0)/Count()/List()` **29 处测试消费点＋3 处编译断言**处置清单（据 §十五之补裁定）；历史证据保留。前置：09.R1 的失败清单固定，本叶产出 G2，不等待 G2。精确文件集：`features/evolution/governance/checkpoint_test.go`、`features/evolution/governance/denial_test.go`；审批与拒绝锚已分别合并到这两个文件，不重建旧独立anchor文件。
  正锚（已存在，本轮未跑）：`go test ./features/evolution/governance/ -run '^TestGovernanceSliceStatesTheValidStateNotTheHistory$' -count=1 -v`，其历史 denial 红转绿不证明审批/目标清减。负锚：`go test ./features/evolution/governance/ -run '^(TestGovernanceSliceRefusesToDropAnOpenApproval|TestOpenApprovalIdentityMustSurviveTheCut)$' -count=1 -v`；按已决裁定撤去文件镜像，验证**原身份可裁决**，不是新身份 park。消费者锚：`go test ./features/evolution/governance/ -run '^(TestGovernanceSliceSnapshot|TestGovernanceRecoveryFromStateSlice|TestGovernanceApprovalRestoredFromStateSlice)$' -count=1 -v`；`go test ./assembly/ -run '^(TestGovernanceSliceWiredInProduction|TestGovernanceApprovalBootReadsStateSlice)$' -count=1 -v`。独立审对照既有反例核验判据收紧，不按历史「唯一缺口」描述勾选。

- [ ] **07.R2 治理有效状态与尾恢复（依 G2、12.R2 的 J1）**。产出仍为尾＋开放项、历史按身份可读、运行/恢复同 fold；已决 answer 的权威取链，不取长期常驻切面。19 位键、尾序号不重置、未应用事实不被覆盖不变。原未完验收保留，实施细分如下；禁止删除历史决定、TTL 删义务或把 `Count()` 改答尾序号。精确写面取三个子叶的并集；`assembly/host.go`、`assembly/checkpoint.go`、`memory/ledger/**`、主 spec、门禁/配置均不在写面。
  父叶结账：`go test ./features/evolution/governance/ -run '^(TestGovernanceSliceStatesTheValidStateNotTheHistory|TestGovernanceHistoryStaysReadableByIdentity|TestGovernanceCountAndQueryStateTheirBoundary)$' -count=10 -v`（后两枚拟新增、未运行）；`go test ./tests/integration/capabilities/ -run '^TestGovernanceRecovery$' -count=1 -v`；`go test ./assembly/ -run '^(TestGovernanceSliceWiredInProduction|TestGovernanceApprovalBootReadsStateSlice)$' -count=1 -v`；`go test ./tests/integration/host/ -run '^TestGateFailsClosedWithoutAttention$' -count=1 -v`。既有正锚只量 denial 历史，不替代下列审批/目标/绑定成本锚。

- [ ] **07.R2.1 已决审批按身份读链，清减常驻证据**。产出：快照/内存只留开放义务及必要身份，已决请求/别名、answer 按身份可查，索取后不再次把历史全留在快照。事实：`SnapshotEvidence`（`runtime/task/approval/approval_store.go:174-199`）仍保已决，§十五之补已选择链上 answer；N17 的保留是正确性修复，不是清减终态。前置：G2、12.R2 的 J1 与链上 request/answer 可读性确认。精确文件集：`runtime/task/approval/approval_store.go`、`runtime/task/approval/attention_store.go`、`runtime/task/approval/attention_log.go`、`runtime/task/approval/approval_store_test.go`、`runtime/task/approval/attention_log_test.go`、`features/evolution/governance/checkpoint_test.go`、`assembly/governance_checkpoint_test.go`。
  正锚（拟新增、未运行）：`go test ./runtime/task/approval/ -run '^TestSettledApprovalsStayReadableWithoutResidentEvidence$' -race -count=10 -v`，已结历史递增、开放量固定，核快照/内存及按身份读回。负锚（既有）：`go test ./features/evolution/governance/ -run '^(TestDeniedRulingMustSurviveItsValidityAcrossRestart|TestExpiredApprovalNeverRunsAfterRestart|TestOpenApprovalIdentityMustSurviveTheCut|TestApprovalAliasCollapsesAcrossTheCut)$' -race -count=10 -v`，覆盖 TTL 前/到期/之后及别名，不把「park 新请求」当原身份存活。真实入口锚（拟新增、未运行）：`go test ./assembly/ -run '^TestGovernanceBootReadsSettledAnswerByIdentity$' -count=10 -v`，两次正式 Boot 共用合成耐久根，已决项不在切面且撤去文件镜像，原请求身份仍读到链上裁决；独立审核持久 answer 不是由测试补接线。

- [ ] **07.R2.2 清减目标载荷并保留历史查询语义**。产出：`GoalOrder/GoalSeq`＋在飞目标，已结/过期目标移出常驻载荷；历史仍按身份可读，`Query(0)/Count()/List()` 不静默变短，逐项对账原 29 处消费点＋3 处编译断言。事实：`features/evolution/governance/goal.go:179-191` 仍复制全部目标；denial 已移出快照并不证明 goal 已完成。前置：G2 的历史查询判据、12.R2 的 J1；与 R2.1 共享验证包串行。精确文件集：`features/evolution/governance/goal.go`、`features/evolution/governance/denial.go`、`features/evolution/governance/checkpoint.go`、`features/evolution/governance/checkpoint_test.go`、`features/evolution/governance/denial_test.go`、`tests/integration/capabilities/governance_recovery_test.go`、`tests/integration/capabilities/critical_calls_hold_through_formal_path_test.go`、`tests/integration/capabilities/event_integrity_test.go`。
  正锚（拟新增、未运行）：`TestGovernanceGoalSliceTracksOnlyActiveWork`、`TestGovernanceHistoryStaysReadableByIdentity`；负锚（拟新增、未运行）：`TestGovernanceCountAndQueryStateTheirBoundary`（查询不伪装为空，尾号不冒充条数），正锚同时核恢复后 `Declare` 不重号、`HasActive/Current` 一致。命令：`go test ./features/evolution/governance/ -run '^(TestGovernanceGoalSliceTracksOnlyActiveWork|TestGovernanceHistoryStaysReadableByIdentity|TestGovernanceCountAndQueryStateTheirBoundary|TestDenialTailSeqIsNotTheRecordCount)$' -race -count=10 -v`。真实入口锚：`go test ./tests/integration/capabilities/ -run '^TestGovernanceRecovery$' -count=10 -v`；独立审对照历史逐条内容与身份，不以仅剩在飞项的列表替代原历史断言。

- [ ] **07.R2.3 移除绑定阶段的全历史恢复工作**。产出：正式 boot 的审批绑定只恢复有效态＋必要尾部；历史请求/answer 读取归索取者，分别量绑定扫描/解码/分配、常驻项与切片字节。事实：`runtime/task/approval/attention_log.go:83-108` 对请求族全扫并填 `evidence`，`:117-137` 经 `foldAttentionFacts` 全扫 attention；只改 SnapshotEvidence 会在 bind 补回。前置：R2.1/R2.2 的按身份读链成立、G2 尾/洞语义；同文件串行。精确文件集：`runtime/task/approval/approval_store.go`、`runtime/task/approval/attention_log.go`、`runtime/task/approval/attention_store.go`、`runtime/task/approval/attention_log_test.go`、`features/evolution/governance/checkpoint.go`、`features/evolution/governance/checkpoint_test.go`、`assembly/governance_checkpoint_test.go`。
  正/真实入口锚（拟新增、未运行）：`go test ./assembly/ -run '^TestGovernanceBootWorkTracksOpenItemsAndTail$' -count=10 -v`（正式 Boot，固定开放量/尾量、增加已结历史，禁止将全扫搬到首次 Judge 而宣称完成）。负锚（拟新增、未运行）：`go test ./runtime/task/approval/ -run '^TestApprovalHistoryReadFailureDoesNotMintANewRequest$' -race -count=10 -v`，链读失败具名拒绝且保留原义务、不铸新请求。既有负锚：`go test ./runtime/task/approval/ -run '^(TestAttentionChainRefusesAStoredBodyOfAnotherFact|TestRequestRecordKeepsTheIdentityItsKeyNames)$' -count=10 -v`。独立审从 host→StateSeat.Bind→BindAttention→首次及再次索取核总工作量，不以快照字节代替全链路成本。
---

## 07.R1 执行记录（本轮实测，2026-11-16；**未勾**，理由见下）

**源树**：全部证据取自隔离 HEAD 树 `git archive 9547faca | tar -x -C /tmp/recovery-g2gov/tree`（`go1.27.1 darwin/arm64`）。主树**零 `.go` 写入、零 `go build`/`go test`**；另一单在改的 `assembly/compress_consumer.go`、`assembly/compress_checkpoint.go`、`assembly/recovery_judgment_test.go` 零读零写。数据根一律 `t.TempDir()` 合成根。

**交付**：本域 `design.md` §十–§十五（三题逐条答＋判据表 G2-G1…G2-G13＋29 处消费点处置清单＋两兄弟缺陷判定＋未证项＋结账判定）。**本域 G2 部分＝FAIL**（九行红、两行绿、两行未证）。

**实测命令与原始退出码（全部在隔离 HEAD 树）**：

| 命令 | exit | 命中 | 结果 |
|---|---|---|---|
| `go build ./...` | 0 | — | 隔离树可编译 |
| `go vet ./features/evolution/governance/ ./g2govtier/` | 0 | — | 两个原型包无 vet 红 |
| `go test ./features/evolution/governance/ -run '^(TestGovernanceSliceSnapshot\|TestGovernanceRecoveryFromStateSlice\|TestGovernanceApprovalRestoredFromStateSlice)$' -count=1 -v` | **0** | 6 RUN（3 顶层＋3 子测试）/ 3 PASS | **冻结消费者锚保持绿** |
| `go test ./assembly/ -run '^(TestGovernanceSliceWiredInProduction\|TestGovernanceApprovalBootReadsStateSlice)$' -count=1 -v` | **0** | 2 RUN / 2 PASS | **冻结生产接线消费者锚保持绿** |
| `go test ./features/evolution/governance/ -run '^(TestGovernanceSliceStatesTheValidStateNotTheHistory\|TestGovernanceSliceRefusesToDropAnOpenApproval\|TestOpenApprovalIdentityMustSurviveTheCut\|TestDeniedRulingMustSurviveItsValidityAcrossRestart\|TestExpiredApprovalNeverRunsAfterRestart\|TestRefusedDurableWriteIsNamedNeitherToCallerNorLog\|TestDenialTailSeqIsNotTheRecordCount\|TestSeatedApprovalBookStillReadsTheRequestDirectory)$' -count=10 -v` | **1** | 80 RUN / 60 FAIL / 20 PASS | 六红二绿，各 10/10 同因，无 flaky |
| `go test ./features/evolution/governance/ -run '^TestApprovalAliasCollapsesAcrossTheCut$' -count=10 -v` | **1** | 10 RUN / 10 FAIL | 别名身份跨切面丢失 |
| `go test ./g2govtier/ -run '^(TestG2GovTailHoleRekeysOverAStoredEvent\|TestG2GovHistoryQueryNamesItsBoundary)$' -count=10 -v` | **1** | 20 RUN / 20 FAIL | 真实 localfile tier：重铸号覆盖已存储事件＋历史读静默变短 |
| `go test ./features/evolution/governance/ -run '^(TestGovernanceSliceStatesTheValidStateNotTheHistory\|TestGovernanceSliceRefusesToDropAnOpenApproval\|TestOpenApprovalIdentityMustSurviveTheCut\|TestDeniedRulingMustSurviveItsValidityAcrossRestart\|TestExpiredApprovalNeverRunsAfterRestart\|TestRefusedDurableWriteIsNamedNeitherToCallerNorLog\|TestDenialTailSeqIsNotTheRecordCount\|TestSeatedApprovalBookStillReadsTheRequestDirectory\|TestApprovalAliasCollapsesAcrossTheCut\|TestGovernanceSliceSnapshot\|TestGovernanceRecoveryFromStateSlice\|TestGovernanceApprovalRestoredFromStateSlice)' -count=1 -v` | **1** | 15 RUN / 12 顶层（7 FAIL / 5 PASS） | 探针与消费者锚同批合计 |

**原型文件（只在隔离树，主树不存在）**：`features/evolution/governance/g2_valid_state_anchor_test.go`（九枚）、`g2govtier/tier_hole_test.go`（两枚，独立包，只用导出面）。

**不勾 07.R1 的理由**：①派发令禁写主树 `.go`，两枚冻结锚只在隔离树建立，主树仍「拟新增、尚未建立」，勾选会把隔离树证据冒充主树落盘；②实际文件名 `g2_valid_state_anchor_test.go` 与独立包 `g2govtier/` 不在任务行候选白名单（原列 `valid_state_anchor_test.go` 单文件）内，须编排者确认改道；③冻结负锚按字面今天不红（闸门对缺失审批铸新请求并 park），其判据区分不了「原 hold 存活」与「新铸 hold」——**验收面勘误已上报，本叶不自裁**（design.md §十五）。

**上报编排者的三项（涉及验收面／跨域）**：
1. 冻结负锚 `TestGovernanceSliceRefusesToDropAnOpenApproval` 判据过弱且必须显式撤走 `<dir>/approvals`，否则被目录第二来源 defeating（design.md §十五勘误 1/2）。
2. `Query(0)`/`Count()` 消费点任务行写「六处」，实测 **29 处测试消费点＋3 处编译期形状断言**，逐文件处置表见 design.md §十一答 3。
3. **07.R2 候选甲不得先于洞语义具名化落地**（今天全量 `Records` 是洞的掩盖层）；**候选乙不得实施**（已结未过期审批决定闸门）。G2-G3 的修法有设计级分叉，须编排者冻结后再派。

## 07.R1 锚落盘（主树，2026-11-16 第二轮）

**源树绑定**：HEAD `9547faca` 主工作树；`features/evolution/governance/` 本轮开工前干净；该包生产文件与隔离树逐字节相同（`diff -rq | grep -v _test.go` 空），故 §十一/§十三 的行号与判定在落盘位置同样成立。

**落盘位置与改名（旧名→新名）**：

| 隔离树原型 | 主树落点 | 新名 | 本轮实测 |
|---|---|---|---|
| `g2_valid_state_anchor_test.go` / `TestGovernanceSliceStatesTheValidStateNotTheHistory` | `features/evolution/governance/checkpoint_test.go` | 同名（冻结名） | **FAIL（G2-G1，故意红）** |
| 同上 / `TestGovernanceSliceRefusesToDropAnOpenApproval` | `approval_channel_anchor_test.go`（新文件） | 同名（冻结名） | PASS |
| 同上 / `TestG2GovExpiredApprovalNeverRunsAfterRestart` | 同上 | `TestExpiredApprovalNeverRunsAfterRestart` | PASS |
| 同上 / `TestG2GovDeniedRulingMustSurviveItsValidityAcrossRestart` | 同上 | `TestDeniedRulingMustSurviveItsValidityAcrossRestart` | **FAIL（G2-G3，故意红）** |
| 同上 / `TestG2GovOpenApprovalIdentityMustSurviveTheCut` | 同上 | `TestOpenApprovalIdentityMustSurviveTheCut` | **FAIL（G2-G4，故意红）** |
| 同上 / `TestG2GovApprovalAliasCollapsesAcrossTheCut` | 同上 | `TestApprovalAliasCollapsesAcrossTheCut` | **FAIL（G2-G5，故意红）** |
| 同上 / `TestG2GovSeatedBookStillReadsTheRequestDirectory` | 同上 | `TestSeatedApprovalBookStillReadsTheRequestDirectory` | **FAIL（G2-G10，故意红）** |
| 同上 / `TestG2GovTailSeqIsNotTheRecordCount` | `denial_ledger_anchor_test.go`（新文件） | `TestDenialTailSeqIsNotTheRecordCount` | **FAIL（G2-G7，故意红）** |
| 同上 / `TestG2GovRefusedDurableWriteIsNamedNeitherToCallerNorLog` | 同上 | `TestRefusedDurableWriteIsNamedNeitherToCallerNorLog` | **FAIL（G2-G9，故意红）** |
| 辅助 `g2CriticalCall`/`g2Gate`/`g2Wire`/`g2Restated`/`g2WithdrawFiles`/`refusingLog` | 同文件 | `criticalCall`/`governanceGate`/`statedWire`/`restatedWire`/`withdrawApprovalFiles`/`refusingLog` | — |

**落位理由**：正锚的落点是 `checkpoint_test.go`（镜像 `checkpoint.go`、同锚 `#framework-checkpoint`），而非原候选的单一新文件 `valid_state_anchor_test.go`——同目录再开一个 `#framework-checkpoint` 非镜像文件会命中 `responsibility-fragmentation`（同锚成员≥2 且非镜像）。审批面锚归 `#approval-channel`、拒绝账锚归 `#denial-ledger`，各成一文件，各自单独拥有该锚。**`-run` 是包级正则，落点改名不改验收命令**。

**原始退出码（主树）**：

| 命令 | exit | 命中 |
|---|---|---|
| `go test ./features/evolution/governance/ -run '^TestGovernanceSliceStatesTheValidStateNotTheHistory$' -count=1 -v` | 1 | 1 RUN / 1 FAIL（`stated 20 records at generation 5 against 4 at generation 1`，点名 `denial.go:215 stated()`） |
| `go test ./features/evolution/governance/ -run '^(TestGovernanceSliceRefusesToDropAnOpenApproval\|TestExpiredApprovalNeverRunsAfterRestart\|TestDeniedRulingMustSurviveItsValidityAcrossRestart\|TestOpenApprovalIdentityMustSurviveTheCut\|TestApprovalAliasCollapsesAcrossTheCut\|TestSeatedApprovalBookStillReadsTheRequestDirectory\|TestRefusedDurableWriteIsNamedNeitherToCallerNorLog\|TestDenialTailSeqIsNotTheRecordCount)$' -count=1 -v` | 1 | 8 RUN / 2 PASS（G2-G11、G2-G2）/ 6 FAIL |
| 七枚红锚 `-count=3`（`red_gov_x3.log`） | 1 | 21 RUN / 21 FAIL，**每枚 3/3 同因** |
| `go run ./scripts/comment_policy -no-baseline runtime/state runtime/task features/evolution/governance` | 0 | **0 finding** |

**仍未勾 07.R1，理由（比上一轮更窄）**：原理由「主树零 `.go` 写入」已由本轮解除，且本包内**每个**冻结锚都已落盘并按判决行为（正锚红、负锚绿、六枚缺口红）。剩下的**唯一**缺口是任务行 §验证 里的生产接线消费者锚 **`go test ./assembly/ -run '^(TestGovernanceSliceWiredInProduction\|TestGovernanceApprovalBootReadsStateSlice)$'`——`assembly/` 不在本轮写面也不是本轮可跑面**（并发单正在写同包），我未运行因而无法声称该半条仍绿。按「每枚冻结锚都落盘且按判决行为才可勾」，本叶留未勾，并把这一条作为唯一残余检查交编排者。

**未落盘（明确排除，非缩水）**：`g2govtier/tier_hole_test.go` 的两枚（G2-G6 `TestG2GovHistoryQueryNamesItsBoundary`、G2-G8 `TestG2GovTailHoleRekeysOverAStoredEvent`）。理由两条：①它们住在一个**新的顶层包 `g2govtier/`**，不在本轮写面（`runtime/state`、`runtime/task`、`features/evolution/governance`）；②它们引 `memory/ledger`（`ledger.TierFacts` 真实 localfile 档），而本域三包当前对 `memory/ledger` 依赖为零（`go list -deps` 实证），塞进去会让本包测试闭包绑上并发单正在重写的包。要落盘须由编排者另开一单并给出真实档位夹具的归属。


## 07.R2 第一轮实施（2026-11-16，**本叶未勾**，见下）

**已落地（主树，逐条附实测）**：

- N16 写腿不洞＋失败具名（`denial.go`／`goal.go`）：`TestRefusedDurableWriteIsNamedNeitherToCallerNorLog` 红→绿 ×10；真实 localfile 档位红绿对见 design.md §十七（未修树 1 条正文被重键覆盖，已修树 0 条）。
- N17 已裁决请求跨有效期保留（`approval_store.go` `expiredWithoutRuling`）：`TestDeniedRulingMustSurviveItsValidityAcrossRestart` 红→绿 ×10；`TestExpiredApprovalNeverRunsAfterRestart` 保持绿 ×10。
- N18 座后书不从目录取 hold（`rebuild`）＋审批链经耐久面（新增 `attention_log.go`，`WithApprovalFactLog`／`BindAttention`，`StateSeat.Bind` 注入）：`TestSeatedApprovalBookStillReadsTheRequestDirectory`、`TestApprovalAliasCollapsesAcrossTheCut` 红→绿 ×10；新增 `TestAttentionChainOutlivesTheProcess`、`TestAttentionChainRefusesAStoredBodyOfAnotherFact` 绿。
- 面内门：`go build ./...` 0；`go vet ./features/evolution/governance/ ./runtime/task/approval/` 0；两包 `go test -count=1` 除下列三枚外全绿；`-race` 两包 0 DATA RACE；`scripts/comment_policy -no-baseline features/evolution/governance runtime/task/approval` **0 findings**。

**未勾 07.R2 的理由（三条，逐条可核）**：

1. **G2-G4（`TestOpenApprovalIdentityMustSurviveTheCut`）按现有夹具无法转绿**：篡改后夹具里没有承载该身份的任何耐久面（空 TempDir、纯内存链、无 fact log 注入）。本轮实现的耐久链就是该载体，但夹具未接线 ⇒ 需编排者裁定是否给该锚注入 `WithApprovalFactLog`（改锚，非本叶权限）。见 design.md §十七／§十八。
2. **G2-G7（`TestDenialTailSeqIsNotTheRecordCount`）是判定型锚**：其红即「`Count()` 不得改答尾序号」的结论（§十三 已裁）。转绿只有篡改记录、改答尾序号、或翻转断言三路，均不做，交编排者裁定处置。
3. **任务行消费者锚 `./assembly/` 两条未复跑**（并发单在写同包），故「生产接线消费者锚保持绿」未经本轮实测，不声称。

**偏离（已在 design.md §十七 记明）**：派发令允许的「`governanceTail` 具名报告洞」未以读侧全流扫实现——冻结读成本锚逐字符断言恢复只读 `cut+1…首次 miss`。改取另一条被允许的路：写侧使流不可能留洞，洞的具名落在拒绝该写的写腿（ERROR，含 key/subtype/subject）。

## 07.R2 第二轮（R61 三条裁定落地，2026-11-16；**本叶仍未勾**，理由见下）

**已落地（主树，逐条见 design.md §十九）**：

- 裁定 1（G2-G7）：`governanceEventsPast` 对坏正文**具名拒绝**（不再 `continue`），锚按新不变量重定（同名）。可构造失败两面均实测：降级为 `continue` → 红（`Count()=2 / DenialSeq=3`）；`Count()` 改答 `int(l.evtSeq)` → 红（`Count()=20` beside 尾 20 无记录）。
- 裁定 2（G2-G4）：夹具按裁定给两本书注入同一 fact log（控制组独立），断言逐字未动；**仅注入不足以转绿（实测原文记 §十九）**，故补 §十八 ① 的最小载体——`tagent/approval/v1/request:<asked>` 请求记录族（`Request` 写、构造与 `BindAttention` 折叠），负锚 `TestRequestRecordKeepsTheIdentityItsKeyNames` 以变异证有牙。
- 裁定 3（G2-G1）：`stateSliceWire` 删 `Records`，`stated()` 只答尾；索引由 `foldHistory` 按座定切面从流派生（`Query`／`Count` 先折叠，签名未改）。**载荷 977→2818 变为 532→533 bytes**（记录 4→20、字节恒定）；变异把 `Records` 加回 → **1004→2845** 且锚红。历史未丢：恢复后绑流 `Count()==20`。

**实测退出码（主树）**：`go build ./...` 0；`go vet` 两包 0；`go test` 两包 `-count=1 -v` **0**（28 顶层 RUN／28 PASS／0 FAIL，另 24 子测试）；三锚各 `-count=10` 10/10 绿；`-race -count=10` 两包 0 竞态；注释门 `-no-baseline` **0 findings**；下游 `./runtime/state/` 0、`./runtime/task/` 1（2 FAIL＝R63 已登记既有红，非本单）。

**仍不勾 07.R2 的理由（一条）**：任务行的消费者锚含 `go test ./assembly/ -run '^(TestGovernanceSliceWiredInProduction|TestGovernanceApprovalBootReadsStateSlice)$'` 与 `./tests/integration/host/ -run '^TestGateFailsClosedWithoutAttention$'`——本窗口 `assembly/` 有并发单在写、`tests/**` 明令禁跑，**未跑即不勾**。另：`tests/integration/capabilities/` 的 29 处 `Query(0)`／`Count()`／`List()` 消费点未改未跑（白名单外），其恢复断言在本轮语义下应仍成立但**不声称**。

**验收面改写（上报编排者）**：`TestGovernanceSliceSnapshot` 的两条记录断言改为「切面不载历史、绑流后按发生序可查同一两条」——判据未缩，来源从切面移到流（design §十九 末）。

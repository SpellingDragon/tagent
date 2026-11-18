# 07 governance 托管 · 设计

## 当前设计与缺口（本轮勘误）

**07.R1/R2 均待验；本轮只改计划，未运行测试。** 最新取向为 §十五之补：已决 answer 按身份读链，不以切面永久保存已结项。N16 的写失败/序号修复、N17 的裁决跨有效期保留、N18 的耐久 request/answer 与别名、denial 移出切面已有历史正确性证据；这些修复保留，不重做或回退为丢裁决。它们尚不证明有效态与恢复成本收敛。

| 当前静态证据 | 尚欠工作 | 行动归属 |
|---|---|---|
| `runtime/task/approval/approval_store.go:174-199` 只滤过期未裁决，已决项仍进 `SnapshotEvidence`；`RestoreEvidence` 仍按其重建问答 | 链承载历史决定，按请求/别名身份索取；快照与常驻态清减后，裁决及有效期答案不变 | 07.R2.1 |
| `features/evolution/governance/goal.go:179-191` 的 `stated()` 仍复制全部目标 | 载荷只携在飞目标与铸号/尾；历史按身份可答，原查询消费点逐项对账 | 07.R2.2 |
| `runtime/task/approval/attention_log.go:83-108/:117-137` 绑定仍全扫请求及 attention 事实，重新填充内存 | boot 恢复有效态＋必要尾；已决历史成本归实际索取者，不搬成首次 Judge 全扫 | 07.R2.3 |

清减同时验构建/绑定工作量、常驻内存及序列化字节；固定开放量与尾量、增加已结历史。`TestGovernanceSliceStatesTheValidStateNotTheHistory` 的既有证据只量 denial 历史，不能代表目标、审批或绑定面。历史决定不删除，未完义务不以 TTL 清除；TTL 继续参与闸门有效性判断，不作销账规则。按身份读取失败具名拒绝，不用空值铸新请求。

当前行动单、精确文件集、正负/真实入口锚、独立审要求集中于 `tasks.md`。下文保留各时点原始证据；「候选未冻结」「审批无耐久面」「只剩消费者锚未跑」等历史描述，均以本节及 §十五之补的最新判决为准，不作为当前放行结论。

## 原实施设计与证据（历史原文保留）

> 静态决策见一级 [design.md](../../design.md) D1（P3 全部托管 / P4 有界恢复）、D6.5（governance 托管＝切片携尾 seq）、D4（03 独立 W2 并行）、§五（Stateful 接缝冻结签名）。本域只列实施要点与实证触碰集，不擅改冻结签名。

## 技术要点（承一级 D6.5）

1. **governance Stateful 切片**：新增一个切片（StateName 如 `governance`），实现冻结接缝 `StateName/SnapshotState/RestoreState`（runtime/state/checkpoint.go:36）。`SnapshotState` 是纯读（接缝契约 :30-35：不 flush、不 commit、不等自身提交锁），携 **尾 seq**（denial 流 `evtSeq` denial.go:108 / goal 流 `evtSeq` goal.go:112）+ **审批态**（`ApprovalManager.evidence` approval_store.go:88）+ 重建索引所需的派生记录（`DenialLedger.records` / `GoalRegistry.goals`）。切片是派生快照，governance 事件链（`tagent/governance/v1/{denial,goal}:` denial.go:62-63）仍是真相与回放源；Offset 之后的事件经同一 reducer 回放（P2 单一 g(x)）。
2. **seedEventSeq 读切片**：`seedEventSeq`（denial.go:327）不再 `log.Scan`（:331）全流找 max，改读恢复后切片的尾 seq。`seedSeqLocked`（denial.go:196 / goal.go:301）从切片取尾 seq 续号，19 位零填充键（`%019d` denial.go:190 / goal.go:295）保字典序＝操作序不变。**切片缺失/损坏 SHALL 具名拒绝**，取代今天 `Scan` 错即 `return 0`（denial.go:333）的静默重置（re-key 覆盖已提交事实风险）。
3. **replayGovernanceEvents / rebuildFromStore 读切片**：`rebuildFromStore`（denial.go:230 / goal.go:336）不再经 `replayGovernanceEvents`（:304）`log.Scan`（:305）第二遍全扫，改读切片携带的派生索引 + 对 Offset 后尾部事件回放。`BindStore`（denial.go:121 / goal.go:126）的双全扫降为「读切片 + 尾增量」，与 tasks 切片（D6.4）同构。
4. **审批簿 rebuild 收编为切片派生**：`ApprovalManager.rebuild`（approval_store.go:481）的 `os.ReadDir`（:485）逐文件全扫改为从 governance 切片的审批态 restore + 尾回放。须保留既有对账语义：未问过的补 `fileQuestion`（:517）、已结的补 `fileAnswer`（:519/:524）、过期的不复活（:506-508 语义由切片 + 尾回放等价表达）。每请求文件镜像（`write` approval_store.go:455-469）作为审批的逐请求 durable 写保留，切片是其派生加速，boot 恢复读切片不读目录。
5. **生产接线**：host.go:608 `governance.Open` 在 `WithRiskRules`+`WithApprovals`（:609-610）之外挂 `WithLedger(ledger)`+`WithGoals(goals)`（policy.go:187/:166），ledger/goals 经 `BindStore` 绑定 durable 日志面；并经 `Run.RegisterStateSlice`（assembly/checkpoint.go:284，与 delivery 切片 host_delivery_checkpoint.go:152 同一接缝）把 governance 切片注册到生产状态接缝。**这一步使托管从测试态转为生产态**——今天 ledger/goals 生产零调用，切片若不接线则仍是死脚手架（违 P6）。

## 实证触碰集（✅ grep，不照搬一级清单）

- `features/evolution/governance/`：denial.go（`BindStore`:121 / `seedSeqLocked`:196 / `seedEventSeq`:327→Scan:331 / `rebuildFromStore`:230 / `replayGovernanceEvents`:304→Scan:305 / `eventKeyLocked`:188-190 / `putGovernanceEvent`:289-297 Sync 屏障）、goal.go（`BindStore`:126 / `seedSeqLocked`:301 / `rebuildFromStore`:336 / `eventKeyLocked`:293-295）、policy.go（`WithLedger`:187 / `WithGoals`:166 / `Open`）、**新增 governance 切片文件**（如 `checkpoint.go`，实现 Stateful 三面）。
- `assembly/`：host.go:562（`NewApprovalManager(dir,0)`）/ :608-610（`governance.Open` 挂 ledger/goals）/ :330（`hostApprovalsDir`）；切片注册经 `Run.RegisterStateSlice`（checkpoint.go:284）。
- `runtime/task/approval/`：approval_store.go（`rebuild`:481 / `os.ReadDir`:485 / `write`:455-469 / `evidence`:88 / 对账 :512-525）。
- `runtime/state/checkpoint.go:36`：Stateful 接缝**只读不改**（§五冻结）；切片携字段由本域声明，编排者核冲突。
- **测试**：tests/integration/capabilities/governance_recovery_test.go（`TestGovernanceRecovery`:33，双扫恢复回归锚）、governance_test.go:560-561、event_integrity_test.go:44、critical_calls_hold_through_formal_path_test.go:39/175。
- **不改**：skeleton_assert.go 的编译期断言形态（接线后 `WithLedger`/`WithGoals`/`BindStore` 签名不变，断言仍成立）；boot 路径簇（assembly/run.go / memory/ledger/journal.go / segment_store.go，属 01+02+04，与本域不相交）。

## 子步序（每步 go build 保持可编译）

3.1 切片携尾 seq + 审批态（Stateful 三面 + 派生索引）→ 3.2 seedEventSeq/replayGovernanceEvents/rebuild 读切片消全扫（含静默重置改响亮）→ 3.3 生产接线 host.go:608 挂 ledger/goals + RegisterStateSlice + 审批簿 rebuild 收编 → 3.4 负锚 + 消费者锚。（门绿是 W2 完成判据非每子步，build 绿才是每子步。）

## 风险与回退

- **测试态 vs 生产态须分清（核心精确点）**：今天事件账本双全扫（seedEventSeq/replayGovernanceEvents）**只在测试发生**——`BindStore`/`WithLedger`/`WithGoals`/`NewDenialLedger`/`NewGoalRegistry` 生产零调用（✅ grep：调用方仅 *_test.go + skeleton_assert.go 编译期断言）。生产真实恢复全扫是**审批簿 `rebuild` 目录扫**（host.go:562→approval_store.go:481/485）。**不得声称生产今天全扫 governance 事件**。本域既要把 ledger 接线生产（使切片托管真实生效），又要把接线后新活的事件账本 seed/replay 与审批簿 rebuild 一并改为切片派生。
- **seq 连续性**：19 位零填充键形（denial.go:190 / goal.go:295）与字典序＝操作序是契约，不改不重排；尾 seq 取自切片须等于已存储 max，重启不复用键。切片缺失/损坏静默 `return 0`（denial.go:333）会 re-key 覆盖已提交事实（违 `session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）→ 改具名拒绝。
- **crash 一致性**：既有 Sync 写屏障（denial.go:297）保 durable 写成功语义；切片快照在安静点纯读（接缝契约 checkpoint.go:30-35），Offset 后事件经同一 reducer 回放，按事件键/seq 幂等不重不漏（与 tasks 切片 D6.4 同构）。审批簿切片派生须与 attention 链对账（approval_store.go:512-525），不复活已结/过期请求。
- **回退**（承一级 D7：core 回退＝恢复全史读，症状回红）：本域回退＝host.go:608 卸 ledger/goals（回到只 `WithRiskRules`+`WithApprovals`）+ 注销 governance 切片 + seedEventSeq/replayGovernanceEvents 恢复 `log.Scan` 全扫 + 审批簿 rebuild 恢复 `os.ReadDir` 目录扫。切片是增量派生，回退不删任何已提交 governance 事件（链恒可按身份重放），回到今天「ledger 测试态 + 审批簿目录扫」基线。

---

# 本轮重整（07.R1 / 07.R2）

> 上文为 W2 时的实施要点与触碰集，保留为历史时点记录。本节起为本轮证据、设计门问题与候选设计；**新字段/签名一律候选、未冻结**。行号以当前树实测为准（旧引用 481/485/608-610 已漂移至 570/574/621，见 proposal 头注）。

## 六、本轮证据（✅ 读码）

| 编号 | 证据 | 含义 | 限制 |
|---|---|---|---|
| E07-1 | `features/evolution/governance/checkpoint.go:52-72`（`stateSliceWire`：Version/DenialSeq/GoalSeq/GoalOrder/Records/Goals/Approvals）、`:104-118` `SnapshotState` | 切片把「尾 + 全部 denial 记录 + 全部目标 + 全部审批证据」一起陈述 | 未测字节随历史曲线；R1 补红锚 |
| E07-2 | `denial.go:215` `stated()` 返回 `append([]DenialRecord(nil), l.records...)`；`goal.go:172` `stated()` 返回全部目标 | 有效态与历史态混在同一载荷里，无分界 | `Count()`（denial.go:183）以 `len(records)` 定义，清减后语义会变 |
| E07-3 | `approval_store.go:140-160` `SnapshotEvidence` 返回全部 `evidence`；`:162-188` `RestoreEvidence` 跳过过期条目、对已结条目仍 `fileQuestion`+`fileAnswer` | 有效期在**读取时**判定（`:488-497` `effectiveStatus`），但恢复会为已结审批重放 attention 事实 | 重放的去重语义（attention 链按身份幂等）未测其成本 |
| E07-4 | `denial.go:164-180` `Query(limit)`、`:183-189` `Count()`；生产侧无经该面的查询入口（消费方为 governance 与 integration 测试） | 历史查询边界**可定义**，但 `Query(0)`＝「全部」的既有断言面必须显式处置 | 若未来有生产查询入口，处置须重做 |
| E07-5 | `denial.go:229` `foldTail`、`:375` `governanceTail`、`:401` `eventSeqOf`；19 位零填充键（denial.go:190 / goal.go:295） | 尾回放与序号连续性机制齐备，清减不需要新回放通道 | `governanceTail` 的键枚举成本另属 06 |

## 七、07.R1 设计门：三个必答边界

1. **有效态边界**：切片的「开放项」到底是哪几类？候选＝两流尾 + 未结（pending 且未过期）审批 + 未终态目标 + 判重去复活所需的最小身份（已结请求 ID 是否必须陈述，取决于「不复活」能否只靠链判定）。判据必须给出反例：把某类从载荷移除后，哪条用户可观察行为变坏。
2. **审批有效期边界**：有效期以 chain 事实的 `ExpiresMs` 为准（`effectiveStatus` 已是读取时判定）；切片 MAY 只陈述未过期项，但 SHALL 保证「过期项不被复活」且「过期这件事按身份可查」。R1 须回答：已结（approved/denied）审批是否仍需陈述——若只为重放 attention 事实，则须证该重放不是第二真相。
3. **历史查询边界**：`Query(limit)`/`Count()` 在索引成窗口后的语义。三条候选（未冻结）：(甲) 索引保留最近 N 条，`limit>N` 时**按身份查链**补齐，`Count()` 改答「流内总数由尾序号推算 + 具名说明不是 denial 条数」；(乙) 索引保留窗口，`Query(0)` 具名拒绝并给出窗口上界；(丙) 不裁索引，只裁切面陈述（`stated()` 只返回窗口内），历史查询仍靠内存 —— 丙只削切面不削常驻内存，须在 R1 显式接受或否掉。**任选一条都必须改写既有断言面并上报编排者**（涉及验收面，不得代理自裁）。

产出：判据表（与 05.R1/12.R1 合并成一张）+ 处置清单（`Query(0)`/`Count()` 的 6 处测试消费点：checkpoint_test.go:242/:252/:382/:385/:411、governance_recovery_test.go:49/:52/:67/:70/:335/:434）+ 红着的锚：
- 正锚（拟新增、尚未建立）：`TestGovernanceSliceStatesTheValidStateNotTheHistory`——同有效工作、denial 历史递增 N 代，切片字节与审批条目数不随历史线性增长；今天红。
- 负锚（拟新增、尚未建立）：`TestGovernanceSliceRefusesToDropAnOpenApproval`——把一条未过期 pending 审批从载荷移除，恢复后该调用必须仍被 park 或具名拒绝，不得静默放行。

## 八、07.R2 候选设计（未冻结）

- **候选甲（推荐）· 载荷分界**：`stateSliceWire` 增「有效态」与「判重身份」两段（名字未定），`stated()` 收窄为尾 + 未结项，denial 记录与已结目标/审批移出载荷；恢复＝切片 + 尾回放（`foldTail` 既有路径），历史查询走链按身份。前提：`Count()`/`Query(0)` 语义由 G2 裁定。
- **候选乙·只裁审批面**：先只把 `SnapshotEvidence` 裁成「未过期未结」，denial/目标不动（成本降一部分，语义不变）。可作为甲的第一步子步，但**不得**宣称 07 达成有效态有界。
- **已否**：删除 denial 历史（违 P1 红线）；把审批目录镜像升级为权威（第二真相；`RestoreBoot` 只在无切面时兜底）。
- **一致性要求**：清减后恢复仍须与运行期同 fold；`RestoreEvidence` 不得为已结审批重放 attention 事实，除非 G2 证明该重放是幂等且非第二真相；19 位键与尾序号规则不变；清减 MUST NOT 覆盖「未被应用的事实」（12.R2 的 J1）。

## 九、风险与回退（本轮）

- **不复活失效**：把已结/过期的判定从「载荷里没有」改成「链上判」，若链读失败会误复活 → 必须具名拒绝而非按零值继续（沿用本域既有取向）。
- **查询面变空**：`Query(0)` 静默返回窗口内条目＝假的「历史上没有」→ 必须具名或在 G2 改写断言面，不得悄悄变空。
- **计数语义漂移**：`Count()` 从「denial 条数」变「尾序号」是语义变更，须显式登记并改写消费方，不得靠改测试文案通过。
- **回退**：本域回退＝载荷恢复全量陈述 + `stated()` 回到全量（成本回涨、无正确性回退）；不删任何治理事件；新增字段为追加键，回退后旧代码忽略未知键（`encoding/json` 既有行为）。回滚证据入 F3。
- **不越界**：治理事件链的物理搬迁（06/08）、跨域消费边界（12）、主规格同步（10）不在本域。
---

## 十、07.R1 执行记录与源树绑定（本轮实测，非规划）

**源树绑定三件（ORCHESTRATION §②，R19 起强制）**：

1. **本叶读/量的是 HEAD `9547faca` 的隔离副本**：`git archive 9547faca | tar -x -C /tmp/recovery-g2gov/tree`。全部 build/vet/test 与十一枚原型均在该隔离树执行。主树**零 `.go` 写入、零 `go build`/`go test`**。本节全部行号以隔离树为准。
2. **本叶不引用任何 CI 记录**，不用工作树结果解释 CI 红。
3. **工作树候选未被本叶消费、未被删改**。另一单在改的 `assembly/compress_consumer.go`、`assembly/compress_checkpoint.go`、`assembly/recovery_judgment_test.go` 本叶**零读零写**；`memory/ledger/segment_store.go` 只读隔离树副本（`keepKeysLocked` 的退位族枚举），未读工作树版本。

**写集**：主树只写本域 `design.md` 与 `tasks.md`。原型只写隔离树的 `features/evolution/governance/g2_valid_state_anchor_test.go`（九枚）与 `g2govtier/tier_hole_test.go`（两枚，独立包，只用导出面），二者**未进主树**——故任务行「拟新增、尚未建立」在主树仍然成立，本叶不勾 07.R1（理由见§十五）。

**隔离树环境**：`go1.27.1 darwin/arm64`；`go build ./...` exit 0；`go vet ./features/evolution/governance/ ./g2govtier/` exit 0。数据根一律 `t.TempDir()` 合成根，未读任何真实用户数据根。

**与 05/12 两域的关系**：本表只填 `G2-G` 行，不定义任务账水位（05.R1 的 G2-T4）与消费者生命周期（12.R1 的 C1–C6）。共用行只引用，不另立一版。

## 十一、三题逐条答（判据＋反例＋可复跑命令）

### 答 1 · 有效态边界：三类载荷各有不同的真相载体，审批切片不是派生加速而是唯一耐久载体

**座后在力的量（✅ 读码 + 实测）**：

| 载荷段 | 座后在力的部分 | 属于历史的部分 | 真相载体 |
|---|---|---|---|
| denial | **只有尾序号 `evtSeq`**（denial.go:110）。记录本身不参与任何裁决 | `records` 全部（denial.go:108） | 治理事件链 `tagent/governance/v1/denial:`（denial.go:62），热层恒可读 |
| goal | 未过期 active 目标（`inFlight` goal.go:318-320）＋ `GoalOrder`（身份铸号）＋ `GoalSeq` | achieved / abandoned / 已过期目标 | 治理事件链 `tagent/governance/v1/goal:`（denial.go:63） |
| approval | 未过期**且**未结（pending）＋ 未过期**已结**（approved/denied 仍决定闸门，policy.go:336/:342） | 已过期项（`RestoreEvidence` approval_store.go:170 丢弃） | **切片本身**——attention 链是纯内存（approval_store.go:124 `newAttentionLedger()`，无注入选项） |

**denial 记录是纯历史（实证零生产消费方）**：`Record`（denial.go:143-159）只追加，无按身份判重；`Query`/`Count` 的生产调用方为 **0**（✅ grep 全仓：`assembly/ runtime/ cli/ api/ memory/ features/` 非测试文件中对 `DenialLedger`/`GoalRegistry` 的引用只有 host.go:559/:560/:575/:576 的构造与接线，无一处调 `Query`/`Count`/`List`）。`scripts/`、`golden/`、CI 门亦零命中；唯一非测试引用是 `skeleton_assert.go:106/:124/:126` 的编译期形状断言（自指，元规则二）与生成的 API 文档。**故 denial 记录不承担任何不变量，可移出载荷**——但受答 3 的洞语义约束（见§十二）。

**goal 已结项可移出（⚠️ 读码判定，未构造反例）**：身份按 `g.seq++` 铸号且不复用（goal.go:194-195），`seat` 另以 `GoalOrder` 显式对齐铸号（goal.go:148-168），故已结目标不再被任何后续事件重新声明；`foldTail` 的 resolved 分支对不在册目标直接跳过（goal.go:404-406）。**定它需要**：一枚「声明 N 个目标并全部 resolve → 切片只陈述在飞项 → 恢复后 `Declare` 铸号不复用、`HasActive`/`Current` 与运行期逐项相等」的原型。本叶未跑，不声称已证。

**审批面的结构性事实（本轮新发现，两条）**：

- **切片是审批的唯一耐久载体，不是派生加速**。`approval_store.go:15` 的红线写「MUST answer presence and ruling from the attention facts alone, never from the request file or the evidence block」，运行期确实如此；但 attention 链本身在恢复时**由 evidence block 重建**（`RestoreEvidence` :175-182 逐条 `fileQuestion`/`fileAnswer`）。链无耐久面、无注入选项，故「链是真相、切片是派生」对 denial/goal 成立，**对审批不成立**。07.R2 若按「派生快照可清减」的直觉裁审批载荷，会删掉唯一载体。
- **请求文件目录是运行期的第二来源（实测 RED）**。`Lookup`（approval_store.go:358-360）在首次未批准读时调 `rebuild`，**即使 `WithApprovalSeat` 已设**；`rebuild`（:570-574）读目录下每个未过期文件并入 `evidence`。故「切片没陈述」≠「书里没有」。**只收窄 `SnapshotEvidence` 不能使审批簿有界**——目录扫会在第一次闸门读时把未过期请求全部补回。反例 `TestSeatedApprovalBookStillReadsTheRequestDirectory`：切面显式丢弃该 hold、恢复后 `Pending()` 为 0，一次 `Judge` 后 `Lookup` 答出原身份 `g2-dir-hold`。**RED ×10**。

**判据与反例**：见§十三 G2-G1（载荷随历史线性增长，RED ×10）、G2-G2（过期审批不复活，PASS ×10）、G2-G9（目录第二来源，RED ×10）。

### 答 2 · 审批有效期跨重启：过期不复活成立，但**人的拒绝不跨重启**

**成立的部分（✅ 实测 PASS ×10）**：有效期以链上事实的 `ExpiresMs` 为准，`effectiveStatus`（approval_store.go:491-496）在读取时判定，`now >= ExpiresMs` 一律答 `ApprovalExpired`，无论已批准与否。`RestoreEvidence`（:170）丢弃过期项。故**已批准且已过期的审批不会因重启而放行**：反例 `TestExpiredApprovalNeverRunsAfterRestart` 先证「有效期内同一调用确实放行」（`DispositionRecord`），再证过期＋重启后不放行。该判据可失败：若 `RestoreEvidence` 保留过期项且 `effectiveStatus` 忽略 `ExpiresMs`，同一夹具即放行。

**不成立的部分（本轮新缺陷，✅ 实测 RED ×10）**：**已拒绝（denied）的审批过有效期后，重启即失去终局性**。机理三段：

1. `RestoreEvidence`（approval_store.go:170）按 `ExpiresMs` 丢弃，**不区分 pending / approved / denied**；
2. attention 链纯内存（:124），拒绝这条 answer 事实无第二耐久载体；请求文件镜像亦不保存它——`rebuild`（:594-596）对过期文件执行 `os.Remove`；
3. 重启后 `Lookup` 对该 (tool, digest) 答 nil，`riskCritical`（policy.go:359-361）遂**铸一条全新请求**，新 TTL、新身份；批准它，同一参数即放行。

反例 `TestDeniedRulingMustSurviveItsValidityAcrossRestart` 失败原文：`a call a human explicitly refused became runnable after a restart … the restarted gate minted request "approval-45f09de4…" for the same digest and approved it (disposition="record"), while the same instant in the same process names "g2-denied-hold" and mints nothing (pending=0, disposition="hold")`。

**同一时刻两路答案不同（运行期 vs 恢复期）**：运行期 `evidence` 保留过期条目（`rebuild` 只删文件不删 map 项），`Lookup` 答 expired，`policy.go:359` 因 `req != nil` **不铸新请求** → 死等且不点名任何请求；恢复期丢弃 → 铸新请求 → 可批准。故本域不仅「重启复活了失效项」，而是**重启把一条死等换成了一条可批准的活请求**，两个方向都不是「拒绝保持终局」。

**身份不随切面存活（✅ 实测 RED ×10，两枚）**：

- `TestOpenApprovalIdentityMustSurviveTheCut`：切面丢弃一条未过期 pending → 恢复后闸门仍 park，但**改用新铸身份**；审批人拿着原告知的身份 `Decide` 答 `ErrApprovalNotFound`。原版（未篡改切面）同一 `Decide` 成功，故红由变异造成。
- `TestApprovalAliasCollapsesAcrossTheCut`：`Request`（approval_store.go:256-262）把第二个调用方身份别名到同一 standing 请求；`SnapshotEvidence`（:146-149）按 map 键逐条陈述 → 切面携 **2 份同 ID 副本**；`RestoreEvidence`（:174）按 `restored.ID` 建键 → **只剩 1 条**，别名身份消失。生产可达：`runtime/agent/fuse.go:363` 以 `batch.id`（前缀＋DecisionID）为每个批次注册，工具名恒为 `tagent:tool-call-batch`（fuse.go:55），同参数第二批次即走别名路径。

**边界瞬时不一致（⚠️ 读码判定，未构造反例）**：`RestoreEvidence` 用 `now >= ExpiresMs`（:170），`rebuild` 用 `ExpiresMs < now`（:594），`effectiveStatus` 用 `nowMs >= expiresMs`（:492）。在 `now == ExpiresMs` 这一瞬时，`rebuild` 会恢复该条并 `fileQuestion`，而读取面判它过期。**定它需要**：一枚把时钟钉在 `ExpiresMs` 整点、比较两条恢复路径 `evidence` 成员的原型。

**对 07.R2 的约束**：审批载荷**不可**只按「未过期未结」收窄（候选乙）。已结未过期项必须陈述（它们决定闸门）；已过期项若移出，必须另立「拒绝按身份可查且不可被新请求覆盖」的机制，否则 G2-G4 恒红。

### 答 3 · 历史查询边界：今天是约定而非机制，且恢复路径**已经**静默变短

**查询面无边界（✅ 读码）**：`Query(limit)`（denial.go:164-180）与 `Count()`（:183-190）唯一来源是内存 `records`；`limit<=0` 答全部；无窗口上界、无具名拒绝、无「这是窗口」的标记。生产消费方 0（答 1 已枚举，含 CI 门与脚本）。故边界**可以定义**，但今天**没有任何机制强制它**。

**恢复路径已经静默变短（本轮新缺陷，✅ 实测 RED ×10，真实 localfile tier）**：`governanceTail`（denial.go:384-396）逐序号读单键，**遇第一个存储不持有的序号即停**。一次被拒的 `Put` 就在流里留下一个洞，洞之后的事件虽在存储里，恢复却读不到。反例 `TestG2GovHistoryQueryNamesItsBoundary`：tier 持 3 条（键 1/3/4，洞在 2），恢复后 `Query(0)` 答 **1** 条、`Count()` 答 **1**，无错误、无窗口声明。读者无法区分「历史没有更多」与「恢复停在这里」。

**尾序号不是条数（✅ 实测 RED ×10）**：`governanceEventsPast`（denial.go:354-361）对读到的每个键取序号，对解析失败的正文 `continue` 跳过。故 malformed 正文使 `Count()` 与 `DenialSeq` 分叉：反例 `TestDenialTailSeqIsNotTheRecordCount` 实测 `Count()=2 / DenialSeq=3 / 存储 3 键`。**这直接否掉§七题 3 候选甲里「`Count()` 改答由尾序号推算的流内总数」**——它会按被跳过的 malformed 正文数多报。

**三候选处置（本叶给判定依据，不自裁验收面）**：

- **丙（只裁切面陈述、内存索引不裁）**：不降常驻内存，且**不修洞**——`Query(0)` 仍静默变短。本叶**否**：它把「有界」做成切面字节数的账面改善，而恢复正确性缺口原样保留。
- **乙（窗口＋`Query(0)` 具名拒绝并给出窗口上界）**：最小诚实面，可先行。它把 G2-G6 的静默变短换成具名答案。
- **甲（窗口＋按身份查链补齐）**：链侧前提**成立**——治理键 `tagent/governance/v1/*` 不属 `keepKeysLocked`（memory/ledger/segment_store.go:954-1009）结算的五类键（idx/evt/meta/ref/blob），故**永不搬冷**，按身份恒可读（与 05.R1 答 2 的任务族同构）。**但补齐读不得复用 `governanceTail` 的遇洞即停走法**，否则补齐同样停在洞前。甲须以「洞语义具名化」为前提。

**处置清单（`Query(0)`/`Count()`/`List()` 消费点）**：任务行写「六处」，**实测为 29 处测试消费点**（✅ grep，含 `goals.List()`），另加 3 处编译期形状断言。逐文件：

| 文件 | 行 | 面 | 处置建议（涉及验收面，**上报编排者，本叶不自裁**） |
|---|---|---|---|
| features/evolution/governance/checkpoint_test.go | 242 | `Query(0)` 逐项比对恢复索引 | **改写**：窗口化后须按身份查链补齐再比对 |
| 同上 | 246 | `goals.List()` 断言已结目标在册 | **改写**：已结目标移出载荷后此断言与候选甲相反 |
| 同上 | 252 / 382 / 411 | `Count()` 断言 2 / 8 / 9 | **改写**：`Count()` 语义须先由 G2 裁定（不得改答尾序号，G2-G7） |
| 同上 | 385 | `Query(1)` 取最新 | **保留**：窗口内最新条，语义不变 |
| tests/integration/capabilities/governance_recovery_test.go | 49 / 67 / 278 / 335 / 364 / 379 | `Count()` | **改写**（同上） |
| 同上 | 52 / 70 / 124 / 188 / 281 / 434 | `Query(0)` | **改写**：按身份查链或具名窗口 |
| 同上 | 325 / 329 / 364 / 365 / 447 | `goals.List()` | **改写**：已结目标移出后须改断言面 |
| 同上 | 150 / 156 / 157 / 160 / 272 | `HasActive`/`Active`/`Current` | **保留**：这三面是在力态，语义不变 |
| tests/integration/capabilities/critical_calls_hold_through_formal_path_test.go | 147 / 248 | `Query(0)` | **改写** |
| tests/integration/capabilities/event_integrity_test.go | 102 | `Query(0)` | **改写** |
| features/evolution/governance/skeleton_assert.go | 106 / 124 / 126 | 编译期形状断言 | **保留**：签名不改（§七题 3 已冻结「签名不改」） |

**退役**：本叶不建议退役任何一处——29 处全部钉着活要求（恢复一致性、失败不阻塞操作、内存降级），退役会丢掉回归面。

## 十二、两个兄弟缺陷在本域的判定

### 缺陷一（12.R1 的 C1：切面水位覆盖被拒绝的派生）——**不经事实水位到达本域，但本域有自己的同族实例**

**不直接共享（✅ 读码）**：`StateSeat` 只有四个方法（checkpoint.go:90/:97/:122/:165），**无 `Reduce`、无 `SnapshotSessionState`**；它只经 `Run.RegisterStateSlice`（assembly/governance_checkpoint.go:52）注册为 Stateful 切片，不是事实消费者。故 `assembly/facts.go:152-154` / `run.go:2995-2997` 的 `states.Reduce` 两路不对称**不经过治理派生**。治理事件也不走事实提交路径：`TierFactLog.Put`（memory/ledger/tier_facts.go:30-32）直接 `kv.Commit`，不进 `commitFact`，故不推进 `landedSeq`；治理的水位是切片自带的 `DenialSeq`/`GoalSeq`。

**本域的同族实例（本轮新缺陷，✅ 真实 tier 实测 RED ×10）**：`putGovernanceEvent`（denial.go:317-326）对 `Put` 失败 `return`、对 `Sync` 失败 `_ =`，而 `evtSeq` 已在锁内自增（denial.go:196）。故**一次被拒的耐久写在流里留下洞，尾序号却已越过它**。若检查点切面早于该洞，恢复的 `governanceTail` 停在洞前，`evtSeq` 低报，重启后的写者**重新铸号覆盖已存储事件**。反例 `TestG2GovTailHoleRekeysOverAStoredEvent` 失败原文（真实 localfile tier，两次 `OpenTier` 同根）：键 `…denial:0000000000000000004` 的正文由 `tool=fourth` 变为 `tool=after-restart-b`；存储键 before=[1 2 4 5]、after=[1 2 3 4 5]，索引只恢复 4 条中的 2 条。**这是 P1 红线「MUST NOT rewrite or drop a governance event already stored」（denial.go:27）的直接违反**，且 `journalKV.Commit`（memory/ledger/journal_kv.go:139-160）无内容门——`StoreEvent` 的 `ErrContentConflict` 门（segment_store.go:565-589）不在这条路径上。

**对 07.R2 的硬约束**：今天载荷里的全量 `Records` **恰好掩盖了这个洞**（洞后记录由切面而非尾回放供给）。**候选甲把 `Records` 移出载荷，就把掩盖层拆掉**：洞后记录既不在切面、也不在尾回放，永久不可达。故**甲不得先于洞语义具名化落地**。这与 12.R1 的 J1（水位不得覆盖未应用事实）是同一约束在治理域的实例，共用 G2-T4 一行，不另立水位定义。

### 缺陷二（12.R1 的 C6：运行期与恢复期失败语义不对称）——**共享，且比消费者域更重**

**✅ 实测 RED ×10**：同一个 governance double，写腿静默、读腿致命。反例 `TestRefusedDurableWriteIsNamedNeitherToCallerNorLog` 失败原文：`Record counted 1 record(s) the store does not hold and wrote 0 log line(s) naming it, while the same store's read refusal answers the boot with governance: read the denial stream past the state cut: … the store answered no read`。

- **运行期写腿**：`putGovernanceEvent`（denial.go:317-326）零返回、零日志。比 `facts.go:152-154`（至少有具名 ERROR）更弱——**零报告**。
- **恢复期读腿**：`Bind`→`foldTail`→`governanceEventsPast`→`Scan` 错上抛（denial.go:230-232 / :379 / :388）→ `StateSeat.Bind`（checkpoint.go:170-177）→ `seatGovernance`（governance_checkpoint.go:64）→ `host.abortBoot`（host.go:773-776）。既有绿锚 `TestGovernanceBindReportsAReadFailure`（checkpoint_test.go:459）钉住读腿。
- **审批面同构**：`RestoreEvidence` 的 `fileQuestion`/`fileAnswer` 失败上抛 → boot 失败；运行期 `Request` 的同类失败经 `errors.Join`（approval_store.go:293）返回，`policy.go:366-368` 把它拼进 wait 理由，**具名但不停**。

**处置归属**：写腿的具名化属本域（denial.go / goal.go 在 07.R2 白名单内）；读腿已具名，不动。**不得新造只喂日志行的生产出口**（R19 N2／R20 N6）：本域的建议是让 `Record` 的失败进入**已有的** `DeliveryGap` 形态或既有 `assemblyError` 通道，而不是新增一个只有日志消费者的计数字段。具体通道由 07.R2 定，本叶不冻结字段名。

### 切片 disposer ——**确认丢弃，本叶不声称活路径缺陷**

`assembly/governance_checkpoint.go:52` 为 `if _, err := run.RegisterStateSlice(seat); err != nil`，disposer 以 `_` 丢弃（与编排者下传情报一致：7 切片 5 注册点全丢）。故治理切片的卸载是**潜在能力**，无生产宿主。本叶**不声称**存在活的卸载丢态缺陷；12.R3 若给 disposer 找到生产宿主，治理切片是唯一按宿主注册的带义务切片，须同批复验。

## 十三、G2 判据表（治理域 portion，供编排者与 05.R1/12.R1 合并成一张）

行 id 前缀 `G2-G` 为本域专有。**水位定义共用 05.R1 的 G2-T4 一行，本域不另立一版。** 每条都附真的应用过的反例；「今日」列全部为隔离 HEAD 树实测。

| 行 id | 判据（须成立的不变量） | 可失败构造（真的应用过） | 可复跑命令（隔离 HEAD 树） | 本轮状态 |
|---|---|---|---|---|
| G2-G1 | 一次切面陈述在力态，其字节随有效工作计费，不随 denial 历史线性增长 | 恒定 1 在飞目标＋1 pending hold，denial 历史 5 代×4 条 → **改前**陈述记录 4→20、字节 977→2818，比值 5=代数 | `go test ./features/evolution/governance/ -run '^TestGovernanceSliceStatesTheValidStateNotTheHistory$' -count=10 -v` | ✅ **PASS ×10（§十九 裁定 3 后；532→533 bytes，记录不再进切面）** — 勘误：本行原记 RED |
| G2-G2 | 过期审批不因重启复活：有效期内放行的同一调用，过期＋重启后不得放行 | 先证有效期内 `DispositionRecord`，再 advance 2h＋切面＋重启 → 仍 hold | `go test ./features/evolution/governance/ -run '^TestExpiredApprovalNeverRunsAfterRestart$' -count=10 -v` | ✅ PASS ×10 |
| G2-G3 | 人的拒绝是终局的：同一 (tool,digest) 不得因重启过有效期而变成可批准的新请求 | denied → advance 2h → 切面 → 重启 → 闸门铸新请求 → 批准 → **放行**；同一时刻同进程不铸新请求 | `go test ./features/evolution/governance/ -run '^TestDeniedRulingMustSurviveItsValidityAcrossRestart$' -count=10 -v` | ❌ **RED ×10（本轮新缺陷）** |
| G2-G4 | 审批人被告知的身份跨切面仍可裁决 | 原版切面 `Decide(原身份)` 成功；丢弃该 hold 的切面 → 闸门改铸新身份、原身份答 `ErrApprovalNotFound` | `go test ./features/evolution/governance/ -run '^TestOpenApprovalIdentityMustSurviveTheCut$' -count=10 -v` | ❌ RED ×10 |
| G2-G5 | 一书多身份（别名）跨切面全部存活 | 同参数第二身份别名到 standing → 切面携 2 份同 ID 副本 → 恢复只剩 1 条 → `Decide(别名)` NotFound | `go test ./features/evolution/governance/ -run '^TestApprovalAliasCollapsesAcrossTheCut$' -count=10 -v` | ❌ **RED ×10（本轮新缺陷，生产可达 fuse.go:363）** |
| G2-G6 | 历史读在窗口外给具名答案，不得静默变短 | 真实 tier 持 3 条（洞在键 2）→ 恢复后 `Query(0)` 答 1、`Count()` 答 1，无错误无窗口声明 | `go test ./g2govtier/ -run '^TestG2GovHistoryQueryNamesItsBoundary$' -count=10 -v` | ❌ **RED ×10（本轮新缺陷）** |
| G2-G7 | 尾序号可作 denial 条数的代理（候选甲的 `Count()` 改答前提） | 键 2 正文 malformed → fold 跳过但取号 → `Count()=2 / DenialSeq=3` | `go test ./features/evolution/governance/ -run '^TestDenialTailSeqIsNotTheRecordCount$' -count=10 -v` | **判据不成立 ⇒ `Count()` 改答被否（结论不变）**；R61 裁定 1 已把锚重定为「坏正文具名＋两问一致」，见 §十九 — 勘误：本行原记 RED ×10 |
| G2-G8 | 重启后的写者不得重铸号覆盖已存储治理事件（P1 红线） | 切面@2 → 键 3 写被拒 → 键 4/5 落库 → 无新切面 → 重启走尾停在洞前 → 续写键 3/4，**键 4 正文被覆盖** | `go test ./g2govtier/ -run '^TestG2GovTailHoleRekeysOverAStoredEvent$' -count=10 -v` | ❌ **RED ×10（本轮新缺陷，真实 localfile tier）** |
| G2-G9 | 同一 governance double 的写腿与读腿失败语义一致（C6 在本域的实例） | 同一 store：`Put` 被拒 → `Record` 计 1 条、**0 行日志**；`Scan` 被拒 → `Bind` 上抛 → `abortBoot` | `go test ./features/evolution/governance/ -run '^TestRefusedDurableWriteIsNamedNeitherToCallerNorLog$' -count=10 -v` | ❌ **RED ×10（共享 C6，且写腿零报告）** |
| G2-G10 | 座后切面是审批的唯一权威：切面未陈述的请求不得由别的来源补回 | 切面丢弃 hold、恢复后 `Pending()`=0 → 一次 `Judge` 触发 `Lookup`→`rebuild` → 目录文件把原身份补回 | `go test ./features/evolution/governance/ -run '^TestSeatedApprovalBookStillReadsTheRequestDirectory$' -count=10 -v` | ❌ **RED ×10（本轮新缺陷，第二来源）** |
| G2-G11 | 冻结负锚：丢弃一条未过期 pending，恢复后该调用仍 park 或具名拒绝，不得静默放行 | 篡改切面 `Approvals=nil`＋撤走文件镜像 → 闸门 `DispositionHold`，铸新请求 | `go test ./features/evolution/governance/ -run '^TestGovernanceSliceRefusesToDropAnOpenApproval$' -count=10 -v` | ✅ **PASS ×10（锚本身不红，见§十五勘误）** |
| G2-G12 | goal 已结项可移出载荷而不破铸号与在力态 | — | — | ⚠️ **未证**（读码判定，反例未构造；定它见答 1） |
| G2-G13 | 有效期边界瞬时两条恢复路径成员一致（`>=` vs `<`） | — | — | ⚠️ **未证**（读码判定，反例未构造；定它见答 2） |

**G2-G1 的实测计数（失败原文摘录）**：`generation 1: stated records=4 approvals=1 bytes=977` … `generation 5: stated records=20 approvals=1 bytes=2818`；`the record ratio 20/4 equals the generation ratio 5/1`。**审批条目数恒 1，不随 denial 历史增长**——任务行正锚里「审批条目数不随历史线性增长」这半条今天成立，红的是记录与字节那半条。`bound = first.records*2` 是**候选容差、非冻结裁决**（R19 N1）；判定依据是比值等于代数，与倍数无关。墙钟未作断言面。

**非空转守卫（R24 N11 教训，逐条具名失败）**：G2-G1 核①存储事件数恰 20②目标确在飞③pending 恰 1④首代记录数非零；G2-G2 核有效期内确放行；G2-G3 核运行期确拒绝（`Denial != nil`）、切面确携 denied 条、同进程确不铸新请求；G2-G4 核原版切面同一 `Decide` 成功；G2-G5 核第二注册确别名（`second.ID == 原 ID`）、等待集确未变宽、切面确携 2 份；G2-G6/G2-G8 核 tier 确持 3/4 键且洞确不存在、被拒写恰 1 次；G2-G9 核被拒写恰 1 次、存储确 0 键、读腿确上抛；G2-G10 核文件确存在、恢复后 `Pending()` 确为 0；G2-G11 核首切面确携该 hold、确未过期、篡改后确为空、闸门确留下记录。**任一守卫不成立即具名失败，判据不会恒真。**

## 十四、未证项（如实登记，不推断为结论）

1. **goal 已结项移出载荷的安全性**（G2-G12）：读码判定，未构造反例。定它需答 1 末所述原型。
2. **有效期边界瞬时不一致**（G2-G13）：读码判定，未构造反例。定它需把时钟钉在 `ExpiresMs` 整点的原型。
3. **G2-G8 的生产触发频率未量**：洞由被拒的 `Put`/`Sync` 造成，本叶用注入构造，**未量真实存储上 `Put` 失败的发生率**，也未走完整宿主 boot→崩溃→重启路径（原型是两次 `OpenTier` 同根＋逻辑重启）。定它需一个跨进程夹具：真实宿主第一代注入写失败并落旧切面、第二代 boot 后续写。
4. **G2-G3/G2-G5 的宿主级端到端未跑**：两枚走 `Policy.Judge` ＋ `StateSeat.RestoreState`（真实闸门与真实恢复接缝），但**未经 `assembly.Boot` 两次真实宿主**。既有消费者锚 `TestGovernanceApprovalBootReadsStateSlice` 证明宿主级审批恢复路径可达，故合成路径的宿主可达性有间接证据，**但合成缺陷本身未在宿主级复现**。不得以「两段各自成立」推定宿主级合成成立（R30 教训）。
5. **别名路径的宿主级可达性**：`fuse.go:363` 的别名由读码确立（同参数第二批次），**未构造真实 tool-fuse 批次夹具**。
6. **未跑全量回归**：只跑 `features/evolution/governance/`、`g2govtier/`、`assembly/` 的选择器与 `go build ./...`。`tests/integration/capabilities/`（含 `TestGovernanceRecovery`）、`tests/integration/host/`（含 `TestGateFailsClosedWithoutAttention`）本轮**未跑**——它们是 07.R2 的消费者锚，不是 07.R1 的冻结锚。
7. **未读**：`assembly/compress_consumer.go`、`assembly/compress_checkpoint.go`、`assembly/recovery_judgment_test.go`（另一单在改，零读零写）；`memory/ledger/` 的工作树版本；`golden/`、`.github/`、`examples/wechat-bot/**` 的实现与测试；05/12 两域 plan 目录的**写**（只读其 design.md 的指定节）；任何真实用户数据根。
8. **G1/G3/G4 未判**，本域不声称它们已定；G2 整体判决由编排者合并三域 portion 后给出，本节只是治理域 portion。

## 十五、07.R1 结账判定

**本域 G2 部分＝FAIL**（G2-G1/G3/G4/G5/G6/G7/G8/G9/G10 九行红，G2-G2/G11 两行绿，G2-G12/G13 两行未证）。这是有效判决，不是叶失败。

**为什么不是 PARTIALLY-PROVEN**：三题全部给出了可判定判据并以真的反例实证，且**四枚新缺陷是正确性缺口而非成本症状**——人的拒绝跨重启失效（G3）、已存储治理事件被重铸号覆盖（G8，P1 红线）、历史读静默变短（G6）、切面权威被目录第二来源 defeating（G10）。任务域判 PARTIALLY-PROVEN 是因为其第四问 BLOCKED；本域无 BLOCKED 项，红项全部取得可复跑原始退出码。

**冻结锚的两处勘误（涉及验收面，上报编排者，本叶不自裁）**：

1. **冻结负锚 `TestGovernanceSliceRefusesToDropAnOpenApproval` 今天不红，且按字面无法红**。闸门对缺失审批的处置是「铸新请求并 park」（policy.go:359-361），故「仍被 park 或具名拒绝，不得静默放行」恒成立。该锚**区分不了「原 hold 存活」与「新铸了一个 hold」**。本轮实测：撤走文件镜像后 PASS ×10，闸门理由里的请求身份是**新铸的** `approval-0d9b937a…`，不是原 `g2-open-hold`。建议把判据加强为「原审批身份仍可裁决」（本叶以 G2-G4 独立表达，RED ×10），**是否改写冻结锚由编排者裁定**。
2. **同一锚若不撤走请求文件镜像则被第二来源 defeating**：本轮首版夹具共用目录，`Lookup`→`rebuild` 把原 hold 从文件补回，闸门理由点名原身份 `g2-open-hold`，锚 PASS 但量到的不是切面。故该锚**必须**显式撤走 `<dir>/approvals`（既有 `TestGovernanceApprovalRestoredFromStateSlice` checkpoint_test.go:504 已是此纪律）。此事实本身即 G2-G10。
3. **处置清单的「六处」是低估**：实测 29 处测试消费点＋3 处编译期形状断言（§十一答 3 逐文件表）。任务行与§七的枚举须按实测改写，**改写属验收面，上报编排者**。

**不勾 07.R1 的理由（正确纪律，非遗漏）**：任务行正锚/负锚的白名单是 `features/evolution/governance/valid_state_anchor_test.go`，而本轮派发令**禁止在主树写任何 `.go`**。十一枚原型已在隔离 HEAD 树建立并各自 ×10 复跑（九红二绿），但**主树仍不存在该文件**，且本叶实际用的文件名是 `g2_valid_state_anchor_test.go` 与独立包 `g2govtier/tier_hole_test.go`（后者不在原候选白名单内，须编排者确认改道）。故任务行「拟新增、尚未建立」的字面状态未变，勾选会把隔离树证据冒充主树落盘。待编排者释放写面后按白名单落入主树并复跑同命令，方可勾。

**放行建议（供编排者裁定，本叶不自裁）**：

- **07.R2 候选甲不得先于 G2-G8 的洞语义具名化落地**：今天全量 `Records` 是洞的掩盖层，拆掉它就把「重启丢历史＋重铸号覆盖」变成永久缺陷。
- **候选乙（只裁审批面为未过期未结）不得实施**：已结未过期项决定闸门（policy.go:336/:342），裁掉即把已批准调用降级为死等、把已拒绝调用降级为可批准新请求（G2-G3 同族）。
- **可先行的最小面**：①`Query(0)`/`Count()` 的窗口具名化（答 3 候选乙，G2-G6 转绿锚）；②写腿失败具名化（G2-G9，沿用既有通道，不新造只喂日志的出口）；③审批身份与别名的跨切面存活（G2-G4/G5）。三者都不删任何已提交事实，回退＝恢复全量陈述。
- **G2-G3 的修法有设计级分叉**（拒绝的终局性载体是「按身份可查的链上 answer」还是「切面保留已结项直到显式清结」），属编排者冻结后再派，本叶不自选。

### 十五之补：编排者裁定（R175，2026-11-17，回应上文三条上报）

**裁定一：冻结负锚 `TestGovernanceSliceRefusesToDropAnOpenApproval` 须改写为「原审批身份仍可裁决」。** 现判据「仍被 park 或具名拒绝，不得静默放行」**恒成立**（闸门对缺失审批铸新请求并 park，policy.go:359-361），故它**区分不了「原 hold 存活」与「新铸了一个 hold」**——按「anchor 为空＝没门在量它」与九种锚病，**一条永不可能红的断言不是在量东西**。实测闸门理由点名的是新铸 `approval-0d9b937a…` 而非原 `g2-open-hold`，正是假锚的证据。改判据为**原审批身份仍可裁决**（G2-G4 已独立表达且 RED ×10，故新判据可红、有区分力）。**这是收紧不是放宽**，不触「不得改锚迁就回归」——那条禁的是为让产品转绿而削弱断言，此处是为让断言真能判而加强它。

**裁定二：该锚必须显式撤走 `<dir>/approvals`。** 否则 `Lookup`→`rebuild` 从文件补回原 hold，锚 PASS 但量到的不是切面而是目录第二来源（此事实即 G2-G10）。既有 `TestGovernanceApprovalRestoredFromStateSlice`（checkpoint_test.go:504）已是此纪律，沿用。**夹具须自证「量的是切面」而非仅「结果是 park」。**

**裁定三：处置清单「六处」按实测改写为 29 处测试消费点＋3 处编译期形状断言。** 依「数字溯源」与「未跑不勾」，任务行与§七的枚举须以 §十一答 3 的逐文件实测表为准，**不得沿用低估数**。

**裁定四（G2-G3 设计级分叉）：取「按身份可查的链上 answer」，不取「切面保留已结项直到显式清结」。** 推导：①**链是唯一真相**（P1），拒绝的终局性若另存于切面即造第二载体；②本变更已冻结「检查点只携开放项」，切面长期保留已结项与该不变量同族冲突；③**已有同形先例**——任务账的已结项由 `hydrateLocked` 在按名索取时从链读回（G19/G22 已落地并实证「水合后快照仍 0 条」），治理审批的已结 answer 应走同一形态：**闸门需要已结项时按身份从链读，而不是让切面常驻它们**。④候选乙（只裁审批面为未过期未结）**不得实施**的既有理由仍成立（已结未过期项决定闸门，policy.go:336/:342），本裁定正是让闸门**读得到**已结项而非裁掉它们。
**实施前置（须先取证，不得直接改）**：确认链上**确实携带**可判定的拒绝 answer（若今天只写切面不落链，则须先补落链，那是产品改动而非夹具改动）；并确认按身份读的成本归索取者（闸门），不摊进 boot。

**裁定五：`g2govtier/` 新顶层包改道批准，但暂不派单。** 理由：两枚 tier 锚（G2-G6/G2-G8）引 `memory/ledger` 真实 localfile 档，而该包正被在飞单重写；且 G2-G8 触 P1 红线，须与洞语义具名化同批落地（见上文放行建议「07.R2 候选甲不得先于 G2-G8 落地」）。**待存储面收口后另开一单**，写面含 `g2govtier/`（新包，只用导出面）＋`features/evolution/governance/g2_valid_state_anchor_test.go`，并须同批处置 G2-G10 的第二来源。

**⇒ 07.R1 仍不勾**（主树无该两文件，隔离树证据不得冒充主树落盘）。**但四个正确性缺陷（G3/G6/G8/G10）是本变更未闭合的纯度缺口，不得因「设计门已取证」而视为已修**——取证只证明缺陷存在，修复与主树落锚是另一件事。

## 十六、锚名落盘勘误（主树，2026-11-16 第二轮）

本节只登记命名与落点；§十一/§十三/§十五 的判定、实测数字与退出码**一字未改**。

- **改名（旧名→新名）**：`TestG2GovExpiredApprovalNeverRunsAfterRestart`→`TestExpiredApprovalNeverRunsAfterRestart`；`TestG2GovDeniedRulingMustSurviveItsValidityAcrossRestart`→`TestDeniedRulingMustSurviveItsValidityAcrossRestart`；`TestG2GovOpenApprovalIdentityMustSurviveTheCut`→`TestOpenApprovalIdentityMustSurviveTheCut`；`TestG2GovApprovalAliasCollapsesAcrossTheCut`→`TestApprovalAliasCollapsesAcrossTheCut`；`TestG2GovSeatedBookStillReadsTheRequestDirectory`→`TestSeatedApprovalBookStillReadsTheRequestDirectory`；`TestG2GovTailSeqIsNotTheRecordCount`→`TestDenialTailSeqIsNotTheRecordCount`；`TestG2GovRefusedDurableWriteIsNamedNeitherToCallerNorLog`→`TestRefusedDurableWriteIsNamedNeitherToCallerNorLog`。**G2-G1/G2-G11 两枚冻结锚名未改。** 本文与 `tasks.md` 的 `-run` 命令已按新名改写（旧名会让正则零匹配而静默 exit 0，R29）。
- **落点**：G2-G1 并入镜像 `checkpoint_test.go`（锚 `#framework-checkpoint`）；审批面六枚并入新文件 `approval_channel_anchor_test.go`（锚 `#approval-channel`）；拒绝账两枚并入新文件 `denial_ledger_anchor_test.go`（锚 `#denial-ledger`）。原候选的单一文件 `valid_state_anchor_test.go` 会让两个非镜像文件争同一 `#framework-checkpoint` 锚而触发 `responsibility-fragmentation`。
- **未落盘（G2-G6/G2-G8）**：住在新的顶层包 `g2govtier/` 且引 `memory/ledger`，均在本轮写面外；理由与归属见 `tasks.md` §07.R1 锚落盘。
- **落盘后行为与 §十三 表逐行一致**：G2-G1/G2-G3/G2-G4/G2-G5/G2-G7/G2-G9/G2-G10 红，G2-G2/G2-G11 绿，各红锚 `-count=3` 同因。
- **G2-G11 的弱点原样保留**：本节 §十五 勘误 1（按字面无法红、区分不了原 hold 与新生 hold）**未被我改动或加强**——强化由 G2-G4 独立表达（红），冻结锚是否改写仍待编排者裁。

## 十七、07.R2 第一轮实施记录（N16／N17／N18，主树，2026-11-16）

**写面**：`features/evolution/governance/{denial,goal,checkpoint}.go`、`runtime/task/approval/{approval_store,attention_store}.go`、新增 `runtime/task/approval/attention_log.go` 与 `attention_log_test.go`、本 `design.md`／`tasks.md`。真实档位原型只写 `/tmp/recovery-07r2/**`（两张隔离 HEAD 树）。未写 `assembly/`、`runtime/state/`、`memory/ledger/`、`scripts/`、`golden/`。

**N16 修法（写腿不洞，取代读腿扫全流）**：

- `evtSeq` 语义改为「屏障确认过的最新序号」：序号在序列化的写腿内分配、确认后才前进，`stated()` 因此不再陈述一个存储里没有的号。
- `Put` 被拒 ⇒ 序号交回（`evtSeq+1` 下一次复用）；`Sync` 未确认 ⇒ 顺序号照记但那一路**停止继续编号**（后续每条都以具名失败拒绝），因为那事件可能已占键。
- 失败**具名**：`putGovernanceEvent` 返回 `(staged, err)`，`governanceWriteFailure` 以 ERROR 打出 `key=… subtype=… subject=… <store原文>`。goal 流同一纪律（并顺带修掉 `Resolve` 对未知 goal 也取号、不写事件这一确定性留洞）。
- **偏离（须记）**：派发令给的替代路「`governanceTail` 具名报告洞」**未以实现读侧全流扫达成**——冻结的读成本锚 `TestGovernanceRecovery*` 断言恢复只读 `cut+1…首次 miss` 且明禁整流前缀读（`checkpoint_test.go:366/:408` 逐字符比对 reads）。读侧一旦在 miss 处补一次全流扫，该锚即红。故取另一条同样被派发令允许的路：**写侧使流不可能留洞**，于是「遇首个 miss 即流尾」由机制而非约定成立；洞的具名落在唯一可知处——拒绝它的那次写。

**N16 真实档位红绿对（`ledger.StorageLocalFile`，键 1/2 落库 → 切面@2 → 键 3 写被拒 → 再写两条 → 无新切面 → 重开同根续写）**：

| 树 | 命令 | exit | 观测 |
|---|---|---|---|
| 未修（`git archive HEAD` 9547faca） | `go test ./g2tier/ -count=1` | **1** | 存量 4 键（洞在 3）；重启后键 4 正文由 `tool=fourth` 变 `tool=after-restart-b`，**重键覆盖 1 条** |
| 已修（HEAD＋本轮 `denial.go`/`goal.go`） | 同上 | **0** | 存量 4 键连续；重启续写 5/6，**存量正文逐字节不变**；`-count=10` exit 0 |

**N17 修法**：`RestoreEvidence` 的丢弃判据由「过期即丢」改为 `expiredWithoutRuling`——**已裁决（approved/denied）的请求跨有效期保留**，未裁决的过期 hold 照旧丢弃。理由：拒绝是人对这组参数的决定，重启后 `Lookup` 仍须答原请求，闸门才不会被铸出一条同一参数的新活请求。G2-G2 仍绿（已批准且过期的条目被保留但 `effectiveStatus` 判过期，闸门不 record）。

**N18 修法（两段）**：

1. **座后书不从目录取 hold**：`rebuild` 对 `seated` 只承认裁决（把外部审批人写进文件的 ruling 落成 answer 并撤走文件），**不再**由文件建 question／入 evidence。目录回到它声明的角色（外部审批人的写入口），不再是运行期第二真源。G2-G10 转绿。
2. **链经耐久面**：新增 `AttentionFactLog`（Put/Scan/Sync，仅内建类型，`ledger.TierFacts` 结构满足）＋ `tagent/attention/v1/fact:` 一流（一事实一键，无序号可跳）；`attentionLedger.RecordAttention` 在屏障后写事实、失败具名；`bind` 折叠已存事实（按落地时间戳、同戳按身份定序）；`WithApprovalFactLog` 选项＋`BindAttention` 方法；**注入点已在生产路径上**——`StateSeat.Bind` 把治理两流用的同一 `GovernanceEventLog` 交给审批簿（`assembly/governance_checkpoint.go:64` 既有调用，白名单外零改动）。同时修掉 N18 点名的构造缺陷：链在 opts 循环**之后**构造，选项因此能注入。
   **仍未达**：审批簿的 evidence（身份／有效期／预览）仍由切面陈述，链只承载 presence 与 ruling。补齐需一枚不经 lineage 解析的耐久请求记录（或链上的 evidence 事实），本轮未做——预算与设计判断见 §十八。

**锚状态（本轮实测，主树）**：

| 锚 | 前 | 后 | `-count=10` |
|---|---|---|---|
| `TestRefusedDurableWriteIsNamedNeitherToCallerNorLog`（G2-G9／N16） | RED | **PASS** | 10/10 绿 |
| `TestDeniedRulingMustSurviveItsValidityAcrossRestart`（G2-G3／N17） | RED | **PASS** | 10/10 绿 |
| `TestApprovalAliasCollapsesAcrossTheCut`（G2-G5／N18） | RED | **PASS** | 10/10 绿 |
| `TestSeatedApprovalBookStillReadsTheRequestDirectory`（G2-G10／N18） | RED | **PASS** | 10/10 绿 |
| `TestExpiredApprovalNeverRunsAfterRestart`（G2-G2） | PASS | PASS | 10/10 绿 |
| `TestGovernanceSliceRefusesToDropAnOpenApproval`（G2-G11） | PASS | PASS | 10/10 绿 |
| `TestOpenApprovalIdentityMustSurviveTheCut`（G2-G4） | RED | **RED** | 10/10 同因（**§十九 已转绿**） |
| `TestDenialTailSeqIsNotTheRecordCount`（G2-G7） | RED | **RED** | 10/10 同因（**§十九 已按 R61 重定并转绿**） |
| `TestGovernanceSliceStatesTheValidStateNotTheHistory`（G2-G1，07.R2 正锚） | RED | **RED** | 未动（本轮不做裁减载荷）——**§十九 已转绿** |

**两枚仍红的性质（非本轮缺陷未修）**：

- **G2-G7 是判定型锚，其红即结论**（§十三 已裁「判据不成立 ⇒ `Count()` 改答尾序号被否」）。要它转绿只有三条路：篡改正文伪造记录、把 `Count()` 改答尾序号（被本锚自身否定）、或翻转断言方向（＝改锚）。三者本轮均不做，**交编排者裁定该锚的处置**（按 R24 N11 同理：改判据或删）。
- **G2-G4 按现有夹具无法转绿**：篡改只置 `wire.Approvals=nil`，而该书建于**空 TempDir**、链为纯内存、无 `WithApprovalFactLog`。即「身份独立的耐久载体」在夹具里根本不存在，任何实现都无法让 `Decide("identity-hold")` 成功。本轮实现的耐久链正是该载体，但**夹具未接线**；需编排者裁定是否给该锚的两本书注入 `WithApprovalFactLog(同一 store)`（属改锚，非我权限）。

**未跑（如实）**：`./assembly/`（并发单在改）、`./tests/integration/**`（同上）。故任务行里两条生产接线消费者锚（`TestGovernanceSliceWiredInProduction`、`TestGovernanceApprovalBootReadsStateSlice`）**未复跑，不声称仍绿**；`StateSeat.Bind` 新增的 `BindAttention` 调用链在 `assembly/governance_checkpoint.go:64` 的同一 `log` 上，但端到端未实测。

## 十八、N18 的剩余缺口（供编排者裁）

- **事实**：审批簿的 presence／ruling 现由耐久链承载（本轮），**identity／有效期／evidence 仍只在切面**。故「链是唯一真相」对审批已达一半。**勘误（§十九）**：identity 已由 ① 的请求记录族承载，故 identity 不再只在切面；全量 evidence 仍在切面陈述，其唯一的额外载体是请求记录族。
- **要补齐的两条路**（互为替代，不可都做）：① 在链上另立一族「请求记录」事实（一条 `ApprovalRequest` 的 JSON，键＝调用方身份），恢复时折叠成 evidence；② 把 `ApprovalRequest` 的展示字段拆成「裁决」与「证据」两段，证据段由 `Request` 写进同一耐久面。
- **两路都要求改 `approval.QuestionFact` 之外的东西**——① 只需本包内新增一族键（白名单内），② 需 `runtime/task/attention.go` 的 `AttentionDetail` 增字段（**白名单外**）。若裁定走 ②，须另派一单并扩白名单到 `runtime/task/attention.go`。
- **`ApprovalKeys` 已进切面**（`stateSliceWire` 新字段 `approval_keys`，与 `approvals` 同序）：别名身份 G2-G5 靠它跨切面存活。载荷形状变化属 07.R2 范围内的追加，不改三方法。

## 十九、07.R2 第二轮（R61 三条裁定落地，主树，2026-11-16）

**写面**：`features/evolution/governance/{denial,checkpoint}.go`、`runtime/task/approval/{approval_store,attention_log}.go`、两包测试文件、本 `design.md`／`tasks.md`。未写 `assembly/`、`runtime/state/`、`memory/ledger/`、`scripts/`、`golden/`、`tests/**`、其它域 plan。数据根一律 `t.TempDir()`。

### 裁定 1（G2-G7）已落地：坏正文具名，`Count()` 不作尾序号

- **不变量（按 R61 逐字）**：`Count()` 与尾序号必须一致，且坏正文必须被**具名**而非静默跳过；`Count()` 不得被重定义为尾序号。
- **实现**：`governanceEventsPast`（denial.go）对解析失败的正文**返回具名错误**（`governance: read the stored event at <key>: …`）并放弃整次折叠——折叠是整体的，索引与尾序号因此永不各说一套。`governanceTail` 的「遇首个 miss 即流尾」由写腿保证（§十七）不变。
- **锚重定**（`denial_ledger_anchor_test.go`，同名）：三段——①通读整流的流两问同数；②坏正文被具名、折叠放弃、两问仍在切面处相等；③**切面只陈述尾而无记录时 `Count()` 答 0 而非 20**（`Count()` 若改答尾序号即红）。
- **可构造失败两面（实测）**：把 `return …err` 降级为 `continue` → `bind … = <nil>, want the key …0002 named: a skipped body leaves Count()=2 beside DenialSeq=3`；把 `Count()` 改为 `int(l.evtSeq)` → `Count() = 20 beside a cut that states a tail of 20 and no record`。复原后绿。
- **未伪造任何记录**：夹具三键原样（键 2 `{not json` 由夹具写入，未被改写）。

### 裁定 2（G2-G4）已落地：夹具补耐久载体 —— 并暴露出**载体本身缺一个族**

- **夹具按裁定改了**：`build(dir, face)`，首进程与「丢切面的进程」共用一份 `sliceLog`，控制组（未篡改切面）用**独立** face——否则控制组批准后其 answer 事实会污染被测书（实测：共用时 `disposition="record"`）。断言意图**逐字未动**。
- **仅注入事实不足（实测原文，必须记）**：`the approval identity the first process handed the approver no longer resolves after the restore: approval: no approval request names that identity: identity-hold`。**根因不是夹具，是产品缺口**：`Decide/Lookup/Pending` 全部从 `a.evidence` 起读，而 attention 事实只承载 presence／ruling，身份只能由切面陈述 —— §十八 ① 所名的缺口。
- **本轮补的最小载体（走 §十八 ①，非 ②）**：新增 `tagent/approval/v1/request:<asked>` 一族（`approvalRequestRecord{asked, request}`），`Request` 在注册（含别名路径）时经既有 `AttentionFactLog` 写入并屏障；`NewApprovalManager`／`BindAttention` 折叠进 evidence。**键＝调用方身份**，故别名身份各占一键，不需解析 lineage（`attention_store.go:16` 红线不动）。负锚 `TestRequestRecordKeepsTheIdentityItsKeyNames`：正文陈述他人身份即具名拒绝（变异删除该判据即红，实测）。
- **副作用与归属**：`TestAttentionChainOutlivesTheProcess` 原以 `len(store.stored)` 计数（现含新族），改为**按 `attentionFactPrefix` 计数**（量的是链的事实，未放宽）；`TestAttentionChainRefusesAStoredBodyOfAnotherFact` 原遍历 map 取末键，新族令其**顺序不定**，改为只取 attention 前缀键（消除新引入的非确定性）。

### 裁定 3（G2-G1）已落地：切面陈述在力态，历史按需从流派生

- **切面**：`stateSliceWire` 删除 `Records`（零残留，不留恒空的字段）；`stated()` 只答尾序号。
- **历史**：`DenialLedger.foldHistory` 在被问到时按**座定的切面**（`seatedCut`）从流派生索引，一次一读并缓存；`Query`／`Count` 先折叠。**未改三方法签名**（`skeleton_assert.go` 钉住）。
- **为何不是「单纯删记录」**：删掉即令重启后 `Query(0)`／`Count()` 静默变短——正是 G2-G6 判为缺陷的那件事，且 `tests/integration/capabilities/governance_recovery_test.go:278` 明写「the refusal kept across the restart」。派生读让切面变小而**历史仍可答**，是候选甲「窗口＋按链补齐」在记录面上的最小形态。
- **载荷实测（before/after，同夹具同命令）**：改前 `records 4→20、bytes 977→2818`（§十三 G2-G1 原文）；本轮锚口 5 代日志 **`bytes 532 → 533`，approvals 恒 1，尾 4→20**。变异（把 `Records` 字段与 `wire.Records = Query(0)` 加回）→ **1004 → 2845 bytes**、锚红并点名「the cut grew with the denial history」。复原后绿。
- **未丢任何欠项／存活裁决**：锚末新增两问——恢复后未绑流 `Count()==0`（切面不载历史），绑定后 `Count()==20`（历史不因裁减而丢）。审批面 `SnapshotEvidence` 只去掉**过期且未裁决**的条目（`expiredWithoutRuling`，恢复本就丢弃＝纯死重）；**已裁决项照旧陈述**（N17）。goal 面**未动**：G2-G12 仍未证（§十四.1），不做未证裁减。
- **验收面改写（上报编排者）**：`TestGovernanceSliceSnapshot` 原断言「切面陈述 2 条记录」「恢复即 2 条」；改为「切面陈述尾」、「未绑流时索引为 0，绑定后两条按发生序可查」——**判据未缩**（同样的两条、同样的序、同样的计数），只是来源从切面移到流。

### 本轮实测（主树，逐条可复跑）

| 命令 | exit | 命中 |
|---|---|---|
| `go build ./...` | 0 | — |
| `go vet ./features/evolution/governance/ ./runtime/task/approval/` | 0 | — |
| `go test ./features/evolution/governance/ ./runtime/task/approval/ -count=1 -v` | 0 | **28 顶层 RUN／28 PASS／0 FAIL**（另 24 子测试 RUN，合计 52） |
| 三锚各 `-count=10 -v` | 0 | 10／10、10／10、10／10（G2-G7 含 3 子测试＝40 RUN） |
| `-race -count=10` 两包 | 0 | 0 DATA RACE |
| `go run ./scripts/comment_policy -no-baseline features/evolution/governance runtime/task/approval` | 0 | **0 findings** |
| `./runtime/state/` | 0 | — |
| `./runtime/task/` | 1 | 2 FAIL＝R63 已登记的既有红（`TestConsumptionFailureIsReportedByName`＝12 域冻结负锚；`TestTaskSliceRefusesToDropAReceiptOfAnOwedNotice`＝05.R2 清减面未实施），与审批面无关 |

**仍未跑（不声称绿）**：`./assembly/`（并发单）、`./tests/integration/**`（并发单＋禁跑）。任务行 07.R2 的两条 `./assembly/` 生产接线消费者锚因此**未经本轮实测**；`tests/integration/capabilities/` 的 29 处 `Query(0)`／`Count()`／`List()` 消费点（§十一答 3 表）本轮**未改未跑**，其恢复断言在本轮语义下应仍成立（派生读补齐历史），但**未实测不声称**。

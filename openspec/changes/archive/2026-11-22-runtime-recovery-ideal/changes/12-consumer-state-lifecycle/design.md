# A 提交—应用—义务：设计门

> 2026-11-21：12.102 已填 [D4 签发区](#d4-方案比较与实施签发) 并新增 [D8 方案比较记录](#d8--12102-方案比较记录)，**待独立设计审后生效**；12.103/12.104 仍未派发、未勾选。权威与证据等级沿用 [顶层 design D1/D4](../../design.md)，派发只经 [ORCHESTRATION](../../ORCHESTRATION.md)。源码定位均为静态读取，不是运行结论。
>
> 2026-11-21 第二轮：12.103 首执行（[D9](#d9--12103-执行记录)）落 H02b，并按新证据把 H01/H02 回门；两项的正式裁定见 [D8.9](#d89--h01h02-重开裁定)，D8.1 的 H01/H02 行、D4 第 6/100 行、D4.2 与 D8.6 的 12.103 表已按裁定改写。12.103 仍未勾选。

## D1 行为与承接

| 来源 | 保留的行为 | 本单元承载 |
|---|---|---|
| 用户原问题 | 恢复后工作不漏、已完处理不重、失败不冒成功 | 12.101 反证，12.105/106 核销 |
| 旧 11 | 领域等价而非重编码字节判重；原文/引用/事实原子提交；新增、已存在、冲突、确定拒绝、结果未知可区分；单写者与原序 | 12.102/103；引擎实现归 B |
| 旧 05 | revisions/facts/mutations/specs 与欠账一致；consumed/attempts/rejected 可判重；洞不抹平；无/空/坏切片分开；账户先于邮箱恢复 | 12.103/104/105 |
| 旧 07 | 原审批身份、拒绝、过期、别名；目标有效态、流序续号、Query/Count/List 不伪装完整；真实宿主接线 | 12.104/105 |
| 旧 12 | 同一状态派生、已折叠输出重试不重开轮次、未完 holder 拒卸载/替换保旧对象、热更仍绑定常驻权威 | 12.103–106；公开挂点由 C 核定 |

任务/邮箱是全局账户，不因 session cut 而强塞进会话切片。事实链保留历史，切片承载有效态；缺失的派生材料可重建不等于缺失的事实可以补造。历史来源见 [旧稿快照](../../history/7b0cde16-before-replan/changes/12-consumer-state-lifecycle/)；逐项旧账由顶层承接表维护，不复制战报。

## D2 结构清单：逐项回答删掉谁会坏

下表是要审的结构，不是保留白名单。12.101 枚举实际生产引用，12.102 对每项给保留/合并/删除、独立需求及反事实；测试、日志和编译断言不算第二消费者。

| 结构及现行位置（相对仓库根） | 当前角色/需核的真实消费者 | 删除或合并的验题 |
|---|---|---|
| `assembly/facts.go` 的预查、落盘、live 登记；`landing`/`landedSeq` | 提交入口、投影、checkpoint；与 B 的提交结局衔接 | 同键是否重复登记，A/B 是否与耐久顺序相反 |
| `assembly/checkpoint.go` 的 watermark、快照、`tailFactKeys` | 全局/会话 cut、任务账户恢复 | 切片覆盖和尾读取是不是同一个截止点 |
| `runtime/state/checkpoint.go` 的 Reduce/ReduceRepeat、unapplied、注册名/pending | 投影、inputs、compress、delivery 等实际 holder | Repeat 是否有独立需要；拒 cut 范围是否由实际状态依赖决定 |
| `assembly/run.go` 的生产恢复分支与幸存者材料 | Boot/Assemble 统一经 `seedReplay` → `seedAnchoredReplay`；无可用锚、有效锚、允许降级的条件见 D3 | 幸存/非幸存合流保持同序；位置/原文角色不可一起误删；`seedFullReplay` 仅为算法参考对照 |
| `runtime/task/checkpoint.go`、`runtime/state/durable.go` 的切片/三族收据/欠账 | registry、mailbox、SettleDriver、历史任务读者 | 未陈述与空集合不同；删除常驻集合后仍能按身份判重 |
| `runtime/task/approval/{attention_store,attention_log,approval_store}.go` | Request/Decide/Lookup、审批通道、governance 状态 | 内存、attention 事实、请求文件的权威与修复义务分别是什么 |
| `features/evolution/governance/{goal,checkpoint,denial}.go` | 目标 gate、StateSeat、Query/Count/List | 有效态与历史索引角色分离，失败不改变有效许可/目标 |
| `assembly/facts.go` sink、`runtime/task/wrapper.go`、`runtime/agent/loop.go` Backflow | 工具结算、投递义务、进程内交接 | 返回成功所结清的是哪笔义务，余欠由谁恢复 |
| `assembly/tools.go`、`assembly/settle_turn.go`、`assembly/host_delivery_checkpoint.go` | 稳定 settle 事实、恢复泵、宿主投递切片 | 配对标记与消费收据不是同一事实；不能靠人工补标记闭环 |
| StateRegistry disposer、GenRegistry、插件目录、Host.Reload | 公开注册 API、builtin/扩展声明、常驻状态与代际对象 | 拒绝保留原对象；无生产替换路径不造迁移框架 |

## D3 真假前提与最小反证

判定使用“证实/证伪/未证”；证实再区分静态机制与动态缺陷。以下动态缺陷均未证，12.101 不要求每个猜想都红。

| 疑点 | 本轮静态证据 | 待反证/成立条件 |
|---|---|---|
| 快照读取后、checkpoint 落盘前的新任务遗漏 | `checkpointLocked` 先 SnapshotState 后提交；`seatDurableAccount` 的 `tailFactKeys` 在最新 checkpoint 物理位置停止 | 静态窗口证实；暂停 tasks 快照之后提交新任务及旧任务推进，再落 cut、真实重启，比较账户/邮箱；遗漏未证 |
| landing 锁外 A/B 应用失序 | `persistFact` 持 landing 只到落盘结束；live 登记/Reduce 在返回后 | 静态分段证实；以受控交错让 A 先落、B 先应用，比较耐久序、逐条派生序和最终态；失序未证 |
| 同键预查竞态 | live/chain 预查先于 persist；persist 的 AlreadyPresent 未向外带回分类 | 静态机制证实；两调用均预查未见，同键等价/冲突分别检查计数、应用及原文；重复应用未证 |
| attention/goal 写失败仍生效 | RecordAttention 先改 held/order；Decide 先改 req；Goal Declare/Resolve 先改 goals 后写事实 | 静态次序证实；在各写/屏障处失败后查 Lookup、真实 gate、目标预算、重启/重试；越权或目标漂移未证 |
| 收据/attempt 读错转 false 再驱动 | `readReceiptLocked` Scan 错、`attemptOf` 解码错均返回 false；MemoryStore.Attempt 消费它 | 静态错误丢失证实；在已落终态记录且未常驻的身份上拒读/坏字节，走真实恢复泵；二次驱动未证 |
| Backflow 保欠 nil 清投递义务、恢复缺 settle key | sink 拒收后 retainNoticeOwed 可返回 nil；Wrapper 随后 acceptDelivery；SettleDriver 查 `settleDeliveryKey`，而 Backflow 的 commitSettled 是另一写入面 | 静态链路证实；原生工具→拒写→停止→Boot 自主恢复，禁止手调 deliver 或补 settle 事实；永久挂起/重驱未证 |
| 前 holder 失败跳过 projection | `assembly/run.go` 先注册 projection，再 inputs/compress；StateRegistry 按注册序 fold | **证伪/已驳回**；不以此新增 projection 补偿机制。只有新可达次序证据才可重议 |

静态读码还不能判定全局拒 cut、Repeat、Backflow 次序的必要性。失败后有日志不等于保欠闭合，某 helper 已拒绝也不等于真实调用方停止。

### 真实恢复分支与非空验收

`assembly/run.go:714、2485–2486` 的生产恢复统一调用 `seedAnchoredReplay`；`:2539` 的 `seedFullReplay` 只作测试算法参考。三类是同一 Boot/Assemble 内的生产分支，不是三个入口；12.105 经公开 Boot/Assemble 验证，不在装配完成后手调 seed 代替，不新增生产 hook/API。以下前提来自 `:2717–2869`，仍待真实入口实跑。

| 分支 | 可达前提 | 12.105 的非空观测 |
|---|---|---|
| 无可用锚：未发现压缩锚 | 该 partition/session 的索引 refs 无 `CompressProductType`，且无已恢复投影时，`:2841` 对该链 refs 按 seq 重放；使用非空、从未折叠且无 checkpoint 的年轻链。无冻结 type 也可能不发现锚，但既有 `blankTypedTier` 手调不算 Boot 证据 | 非空业务事实逐条原序/原文、任务账户及邮箱欠账；已完凭证与未完义务分别核，冻结路由不变 |
| 无压缩锚的 checkpoint 子分支 | `restoredSeedOf` 命中且 `!hasAnchor` 时，`:2769–2784` 恢复幸存者并读其后尾段，不走全链兜底；不能把此案当作上一行 | 非空恢复投影、cut 及尾部提交，核账户先于邮箱、切片覆盖与尾段不漏不重 |
| 有效锚 | refs 实际发现该链最新压缩锚，按分区取到事实且事件可解码；`compressProductOf` 成立，checkpoint 可解成非空幸存者；位置、原文还原与后续重放均成功（`:2786–2833`） | 非空幸存者及尾段、原文/呈现原序；分别覆盖有/无框架 checkpoint 的接缝，已答压缩请求不重做，任务/邮箱及收据仍一致 |
| 允许降级：发现锚但元数据不可用 | 权威帧/事件可读；产品未陈述（缺 key/request、身份不符或 survivors 元数据不可解）、checkpoint 未陈述/不可解/空，使 `stated && len(survivors)>0` 不成立。用无覆盖 cut、索引含全链的合成根，确保兜底读到全部所需事实 | 记录具体不可用条件、既有降级提示及实际全链读/应用证据；非空呈现、原身份/路由、已完凭证和欠账与同业务基线一致，不能仅凭提示或 fallback 计数判成功 |

损坏拒绝另列负例：索引最终遍历/存储读取报错、锚事件解码失败、幸存者定位/读取/还原失败、状态应用失败都向 Boot/Assemble 返回错误。`:2837–2839` 的锚正文缺失虽先提示降级，随后 `replayRef`（`:3163–3174`）仍对该索引键报 `ErrSegmentBodyMissing`；它不是成功降级。未陈述可选锚元数据与损坏权威事实/任务切片不可混称。前提或非空观测缺失即保留未证，不以造新入口补足矩阵。

### 已有锚的证据边界

- `TestAnchoredBootMatchesFullBoot`（`assembly/anchored_boot_parity_test.go:163、171、180`）：装配后重置压缩消费者，再手调 full/anchored seed 和 `blankTypedTier`，仅是算法参考对照；不证明真实 Boot 的三类分支。完整消费覆盖由 12.105 的 `TestRecoveryEntriesPreserveStateAndObligations` 承担，B 06.105/106 签收同候选证据。

- `TestConsumerBehindTheChainSurvivesARestart`：确认旧 cut 保留、拒绝事实可读且重启被应用；当前含“拒 cut”策略断言，不能据它裁定全局拒绝唯一正确。
- `TestRetryOfFoldedOutputDoesNotReopenTheRound`：正式提交重试已移出投影的旧输出，检查呈现范围/序列/产物不变；不证明并发新提交保序。
- `TestUnmountKeepsTheOwedItemsOfItsSlice`：拒卸载后原 holder 新增义务仍入 cut，重启保两项，结清后可卸载；是公开注册 API 锚，不冒充产品动态禁用路径。
- `TestRedeliveredNoticeAfterRestartSettlesOnce`：三个切点有实际收据/驱动断言，但 `stopBesideTheCut` 预置 settle 事实；没有证明失败路径自己产出它。
- `TestDeliveryFactFailurePreservesTheNoticeObligation`：Invoke 失败路径及保欠已覆盖，重启后手动 `deliver`；不代替 Boot 自主恢复泵锚。现有多次 Assemble 不冒称独立 OS 进程。

## D4 方案比较与实施签发

先固定事实身份、提交完成、状态已应用、义务已履行四种含义；选择使整条生产链更简单的方案，不以少几个方法或某门转绿为判据。

| 候选（顶层三案；逐疑点选择见 D8） | 提交/失败/恢复安排 | 全局复杂度及否决条件 |
|---|---|---|
| 保留现状并限制未证声明 | 保留分段提交、Repeat、拒 cut 与现有义务恢复；用反证确认真实安全范围 | 格式/迁移成本最低；若任一真实漏事、重驱或失败放行成立，不能只改声明或告警而保留缺陷 |
| 最小修正现有责任边界 | 针对成立缺陷统一提交结果的消费、快照截止和失败可见性；在原持有者闭合义务 | 可能避免新持久字段/队列，但须计算锁时长、重入/死锁、补应用范围、调用方签名与跨根代价 |
| 删除/合并重复状态或适配层 | 让权威结果直接驱动现有消费者，消除可证明冗余的分类、保欠或重复派生层 | 只有独立消费者都可由同一语义服务才合并；删除后读回成本、原文/位置/隔离角色和故障恢复负担一起计入 |

有序队列或应用游标只是实现上述候选的备选手段，不是第四个既定目标。Repeat 保留/删并需同时过“已折叠旧身份”和“并发新事实”两面；不由 projection 的单一生产实现自动断言其多态接口必要或无用。逐疑点比较结果与选择见 [D8.1](#d81-六案比较表)（Repeat 见 [D8.3](#d83-repeat-保留删并裁定)，全局拒 cut 见 [D8.2](#d82-h06-全局拒-cut-三态比较)）；三案仍是顶层分类，D8 不新增第四类。

全局拒 cut 与按实际影响域限制分别比较无错、瞬时错、长期错的安全与受阻范围、热层增长、恢复成本；Owner/Target 不同不是分域证据。Backflow 的提交/交接先后分别比较存活推进、模型可见记录、收据前后崩溃、Wrapper 返回含义和真实泵续做，不预选先投或后投。

每个候选记录：独立消费者、结构角色、常驻条目/持久字节副本、读写/锁成本、正常与失败路径、公开面/格式迁移、测试/运维负担及反转探针。B 提供读成本/存储语义，C 提供能力边界；删一结构后级联核其伴生参数、接口、缓存、注释及测试是否仍有生产需要。

### 签发区（12.102 已填，待独立设计审生效）

> 填写口径：每格给"值 + 证据出处"。逐案三案同尺比较、否决理由与全局复杂度账的唯一所在是 [D8](#d8--12102-方案比较记录)，本表不复制；三锚完整 argv 与预期命中的唯一所在是 [D8.6](#d86-逐叶验证矩阵)。本表＋D8 是本叶唯一写入面；共享语义只列"所需事实与候选清单"（[D8.7](#d87-共享语义所需事实与候选清单未定版)），最终单一记录由编排者与 06.102 会签后单写。

| 签发项 | 值（含证据出处） |
|---|---|
| 当前候选/合同/工具指纹、原 PR 基线与工作树归属 | 候选沿用 ORCHESTRATION 声明的 `7b0cde162627ad37f1d364bbf3508b3bd0518431`；本工作树 `git rev-parse HEAD` 返回 `fatal: not a git repository`（与 D7.0 同），故身份以**内容指纹**钉住（11 文件，见下方 D4.1 代码块，命令 `shasum -a 256 <file>`）；工具 `go1.27.1 darwin/arm64`、`openspec 1.2.0`。原 PR 基线 `90fecc56…` 沿用顶层 D1，**本轮未复核，标未验**。工作树归属＝本 change 的规划文件；生产源码本轮只读（零 `.go` 改动）。 |
| 已证缺陷、驳回及未证清单；原始时序和失败证据 | 已证六项：H01（窗口漏提交）、H02（应用序≠耐久序）、H02b 等价案（重复登记与重复 fold）、H03a 裁决腿（写失败仍放行）、H03b（四腿全红：写失败仍改有效态）、H05（重启后 0 事实/0 收据/0 回合）。已驳回三项：H02b 异内容案（具名拒绝，现行为正确）、H04 坏字节重驱（该切点具名拒绝＋保欠）、"前 holder 失败跳过 projection"。未证两类：H04 残留（`attemptOf` 解码失败分支、`readReceiptLocked` Scan 错误分支，D8.4 裁定为实施期取证）、B 侧 H10/H11。原始时序、探针名与 exit 见 [D7.1/D7.2](#d71-逐疑点结论)。 |
| 所选方案及保留/最小修/删并的否决理由、全局复杂度账 | 六案选择：H01 **安静点标记定界**（恢复侧单一边界；2026-11-21 由"按陈述 offset 前移"改判，原版在事实层不可实现，见 [D8.9-H01](#d891-h01尾段边界的三案与证据)）；H02 **单一有序应用点**（`landing` 覆盖落盘＋登记＋fold）；H02b **提交结局分类收敛为单点**（且判重读与写同处一个原子单元）；H03a/H03b **耐久成功才可见**（store 内屏障）；H05 **投递义务单一事实源**（唯一身份＋唯一渲染器＋泵内补齐＋拒绝写路径保 claim）；H06 **保留全局拒 cut**（影响域限制被 delivery 跨会话目标反证）。三案否决理由与全局复杂度净账见 [D8.1](#d81-六案比较表)/[D8.2](#d82-h06-全局拒-cut-三态比较)/[D8.5](#d85-全局复杂度账净增净减)。 |
| 接口签名、返回语义、重入/并发约束、完整调用方适配 | 见下方 D4.2 表（逐项：改动对象、新语义、全部调用方）。五条硬约束：① 任何 holder 的 `Reduce`/`ReduceRepeat`/`SnapshotState` 内**不得**提交事实（否则 `landing` 自锁；现状三个持有者 reducer——projection、compress、delivery——均不提交，见 D8.1-H02）；② `SnapshotState` 纯读契约不变，"不持 run 锁读 holder"的既有 redline 不变；③ 泵在事实提交失败后**不得**消费该 seat；④ 2026-11-21 补：holder 的 `Reduce`/`ReduceRepeat` **不得等待任何依赖 landings 才能完成的动作**（有序单元把 fold 纳入 `landing`，反向等待即互锁）；⑤ 2026-11-21 补：`landing` 的取得**不得早于**两次落盘前读（`factReadCounter(want=4)` 钉住"每提交两次落盘前读"，提前加锁会使等价案锚失去前提，见 D8.9-H02）。 |
| 持久格式/字段/版本、旧无键/空/坏数据处理与迁移；无变化也需实证 | **既有记录格式、载荷与版本零变化；新增一条派生截止标记键族**（2026-11-21 由 [D8.9-H01](#d891-h01尾段边界的三案与证据) 重签，本行原写"无持久格式变化"）：`tagent/checkpoint/v1/quiet:<分区>:<序号>`，每个框架 cut 一条，单键单屏障，唯一读者是 `tailFactKeys`；`CheckpointPayload`/`CheckpointVersion=1`/`CheckpointOffset`/`SESSION_FORMAT`/`checkpoint.offset|slices` meta 键**均未改**（故 `api/event/**` 不在写面，内容寻址 ID 语义不变）。旧根处理：缺该键的 checkpoint ⇒ 尾段 `nil` ＝读全日志（有界性一次性退化，绝不漏项），由下一个新 cut 之后恢复有界；无 tier 与非 journal 档**不写**该键（既有"存储不陈述提交序"早退路径，不加耐久承诺）。其余实证不变：`event.CheckpointOffset`、checkpoint payload 形状、mailbox/三族收据 wire、goal/attention 事件体均未改（本轮零产品改动；提交后由 12.105 的 `TestBootSeatsTheMailboxAfterTheDurableAccount`＋`TestRecoveryEntriesPreserveStateAndObligations` 证明旧根可读）。唯一"取值集合"变化＝投递事实的身份此前由 backflow 铸造、此后统一为 `settle-<hex>`；旧根上的铸造身份事实仍按身份可读，不需迁移、不需回退。无键事件仍由 `mintEventID` 铸造；`Seat` 的 nil tail 仍是"无 cut"（读全日志），空 tail 仍是"cut 覆盖全部"；坏字节沿用既有具名拒绝。 |
| 12.103 精确生产/测试/文档/门写面，每文件理由与拥有者 | 生产：`assembly/facts.go`（H02 有序单元＋H02b 结局路由）、`assembly/checkpoint.go`（H01 安静点标记的写入、以及 `tailFactKeys` 的走停点改为最新标记；H02 `landing` 跨度越过登记与 fold）、`assembly/run.go`（`landing` 契约注释；**2026-11-21 更正**：原列的"`seatDurableAccount` 增参 + boot 侧 nil offset 传参"随 H01 改判消失，`seatDurableAccount`/`tailFactKeys` 签名不变）。测试：`assembly/facts_test.go`（H02 锚编排更新，断言不变）、`assembly/checkpoint_test.go`（标记停点的正/负锚与旧根回退）。文档：`docs/wiki/platform/platform-subsystems.md`（`#framework-checkpoint` 段**必改**：该段现写"晚于水位的由回放覆盖（按身份幂等，最多重复一次、绝不漏跳）"——此句对状态持有者为真、对任务账户为假，正是 H01 的书面成因；改为"状态切片按 offset 覆盖并由回放补，任务账户按安静点标记切分尾段"）。门：无新增；`scripts/gates/check_concept_budget.py` 只降不抬。拥有者 A（12.103）。**不改**：`runtime/state/checkpoint.go`（H06 保留全局拒）、`assembly/task_checkpoint.go`、**`assembly/task_checkpoint_test.go`**（其"已陈述任务不重复进尾段"的成本语义由本裁定保持，不得放宽、不需补签）、`assembly/commit.go`、`api/event/**`（载荷与版本未改）、B 的 `memory/**`。 |
| 12.104 精确生产/测试/文档/门写面，每文件同根因证明与拥有者 | 生产：`runtime/task/approval/attention_store.go`（H03a）、`features/evolution/governance/goal.go`（H03b）、`runtime/state/durable.go`＋`runtime/state/memory.go`（H04 残留：读失败具名）、`runtime/task/delivery_fact.go`（**新增** H05 单一身份＋单一渲染器）、`runtime/agent/loop.go`（H05 第二写入面收敛）、`assembly/tools.go`（H05 单一写入面）、`assembly/settle_turn.go`（H05 泵内补齐）、`assembly/facts.go`（H05 保 claim 不释放，只此一处）。测试：`runtime/task/approval/{approval_store,attention_store}_test.go`、`features/evolution/governance/checkpoint_test.go`、`assembly/host_test.go`、`assembly/settle_turn_test.go`、`runtime/state/durable_test.go`、`runtime/task/delivery_fact_test.go`（**拟新增**，D8.6 的 `TestDeliveryFactOfIsOneFactForBothWriteFaces` 落点，拥有者 A）。文档：`docs/wiki/agent/{governance-enforcement,agent-architecture,task-lifecycle}.md`（各自对应段落）。门：`scripts/gates/check_concept_budget.py`（基线只降不抬；抬即回设计门）。同根因证明：每文件对应 D7 一个已证切点（见 D8.1 各案"文件面"列）。**跨叶共享文件**：`assembly/facts.go` 同时出现在 12.103 与 12.104，按前置 12.103→12.104 串行，不并发；两叶各自的 hunk 必须在签发记录中分开。拥有者 A（12.104）。 |
| 12.104 补签（2026-11-21 收尾会话，超签审计判留） | 追加两文件：`runtime/state/mailbox.go`（`durableConsumption.settled` 由 `bool` 改 `(bool, error)`）与 `runtime/state/mailbox_checkpoint.go`（`RestoreState`/`owedOfWork` 把该错误传出）。判留依据（逐 hunk 同根因，非"编译要求"式追认）：①**编译强制**——把两文件回退到 HEAD 后 `go build ./runtime/state` 报 `*DurableCommit does not implement durableConsumption (wrong type for method settled): have settled(task.Notice) (bool, error), want settled(task.Notice) bool`；该接口的唯一实现者即本叶已签的 `durable.go`，接口声明与唯一消费者两处随签名必须同改。②**同根因**（D8.7.1 第 4 条"读失败与不存在"＋D7 的 H04 残余切点 `readReceiptLocked` Scan 错与 `attemptOf` 解码错转 false）：旧 `owedOfWork` 把 `settled` 的读错误折成"未结算"，恢复据此把已结算的通知重新入席并交给第二个回合——与 `durable.go` 修的是同一缺陷的**恢复侧出口**，不是新需求。③**独立锚**＝新增 `TestUnreadableReceiptFailsTheRestoreByName`（落 `runtime/state/durable_test.go`，D8.6 已列）：正例"无回执的通知入席"、负例"扫描被拒 ⇒ 恢复具名失败且零入席"。无接口/持久格式/能力声明变化；唯一消费者是 `RestoreState`（启动）。拥有者 A（12.104）。 |
| 12.105 验证文件、构建读取闭包与相邻回归完整选择集 | 验证文件沿用 tasks 12.105 已列清单，并由 D8.6 追加三枚（H05 泵补齐、H04 具名拒绝、H02 同链应用序）。构建读取闭包：`./assembly`、`./runtime/...`、`./features/evolution/governance`、`./memory/ledger`（无新增构建标签；smoke 标签不属本单元）。相邻回归：`go build ./...`、`go test ./assembly -count=1`、`go test ./runtime/... ./features/... -count=1`；性能/成本影响按 D5 冷热分列，不以本页最小命令冒充 F2 全量。 |
| 全部真实生产者/消费者及公开入口、无 tier/存储档位/生命周期范围 | 事实提交入口 `Run.commitFactChecked`/`commitFactNoCadence` 的真实消费者 15 处：`assembly/{facts.go:952(record),tools.go:130(settle),compress_consumer.go:602,age_fold.go:294,checkpoint.go:370/440,run.go:657(loop commit face)/1723/1945/2012/3910,host_reload.go:317(配置变更),request.go:335,invoke.go:49/59}`。approval：`Lookup` 由 `runtime/agent/fuse.go:355` 的 gate 读、`Decide` 由 `examples/wechat-bot/wechat_approval.go:162` 调。goal：`Declare`/`Resolve` 在本树**无生产调用方**（仅 `features/evolution/governance/checkpoint_test.go` 与 `tests/integration/capabilities/governance_recovery_test.go`），有效态读者是 `policy.go:263/510` → 记明"库契约修复，不宣称生产症状已消失"。投递事实：写入面 `assembly/tools.go:130` 与 `runtime/agent/loop.go:731`，读面 `assembly/settle_turn.go:523`。无 tier 档：`persistFact` 在 tier 为空或 Owner 为空时早退的路径保持"内存即权威"，不暗增耐久承诺；memory/file 档共用同一路径。生命周期范围：Boot/Assemble、Host.Reload、卸载/替换三类均由 12.105 既有锚覆盖，本表不新增入口。 |
| 每叶正/负/消费者三锚全名、完整 argv、预期命中数及子场景 | 唯一所在 [D8.6](#d86-逐叶验证矩阵)（12.103 / 12.104 / 12.105 三张表，含选择器、预期 `=== RUN` 数与子场景）。准入规则：`-run`/`-k` 零匹配即失败；"拟新增"者实施期先核存在性与命中数；不以父测试 PASS 代替子场景。 |
| 负变异落点、控制绿、主题失败判据及反转办法 | 逐案落点见 [D8.6](#d86-逐叶验证矩阵) 末两列（H01 回退位置边界、H02 fold 移出有序单元、H02b AlreadyPresent 走 filing、H03a/H03b 发布先于耐久、H04 解码错回 false、H05 缺失事实走 release）。控制绿＝未变异副本同一次执行内同名锚通过；主题失败判据＝该锚**主题断言**（漏投递/失序/重复应用/放行/有效态漂移/0 回合）报红，而非编译或夹具早退。反转办法＝`git checkout --` 属禁止动作，改为在隔离副本还原该文件后复跑，仓库内零 diff。 |
| 同条件基线、原始退出码、成本/安全影响与持久证据位置 | 基线＝D7.2 的原始 exit（1/1/1/1/1，六案在证据期全红）；成本基线＝06 D8.5 实测单键提交 4.1–4.7 ms/次（fsync 主导）。本单元无性能主张之外的费用承诺：H02 的锁内新增跨度＝登记＋fold（不改变 fsync 主导），但串行化面变大，12.105 须单列"提交等 fold"轴（D8.9-H02 未验面）；H01 的 tail 读取增量＝安静点标记与 checkpoint 之间的键数，即 H01 窗口本身（`stateSeatedAccount` 的 `tail=%d` 日志与原值可比），另加每个框架 cut 一个屏障；**已知被证伪的成本读数**：D9.2 的按 offset 版实测 `tail=42`、`chains=11 links=22`（锚要求 `chains=3 links=6 covered=0`），本裁定不得回到该形态。安全影响：无新增外部入口/权限面；H05 泵补齐走同一身份，不扩大可投递面；安静点标记只被 `tailFactKeys` 读、不含业务内容。证据位置：执行期 scratch 落 `/tmp/rr-12-103/`、`/tmp/rr-12-104/`，12.105 最终矩阵落独立产物目录（scratch 不作持久凭据）。 |
| 权限/数据边界、资源预算、共享写面释放 | 只写 A 白名单（本文件、`tasks.md`）；真实数据 `examples/wechat-bot/.wechat-config/data` 与用户 `--source` 根不读不写；夹具一律 `t.TempDir`／合成根；不改 git 索引/refs、不 commit/push/archive、不改权限；B 的 `memory/**`、C 的 `scripts/**` 与外来未跟踪面只读。资源预算：12.103/12.104 各自单包定向 `-race -count=10`；共享写面释放条件见 ORCHESTRATION 释放表第 2 行。 |
| A/B 共享语义确认、C 能力与规格承接确认 | 需 06.102 会签三条（[D8.7](#d87-共享语义所需事实与候选清单未定版)）：① 索引答 `(partition, seq)` 与 `pastCut` 判定可用；② cut/位置含义唯一（"陈述 offset"而非"checkpoint 落点"）；③ 读失败与不存在的分类一致（H04 残留）。**A 侧对 B 行5④（scoped 损坏作用域收窄为身份级）的立场**：本单元签发写面（`assembly/{facts,checkpoint,run,commit,task_checkpoint,tools,settle_turn,host_delivery}`、`runtime/state/*`、`runtime/task/*`、审批/目标文件）不调用 `GetScoped`/`ReadSegmentScoped`/`ensureLoaded` 族装载，义务与收据路径读键值与事实，不依赖 scoped 原文全族语义，故无依赖；实施期发现反例即回门重签。C 侧：13.102 的 C7 已生效，本单元选择不新增任何公开插件/能力声明（H05 的身份前缀属 A 内部事实），故不改 C7.1 写面；规格承接见本单元四份 delta 的相应 Scenario。 |
| 独立设计审者、扫描范围、结论、未解决项及签发人/时间 | **已执行（2026-11-21）**：审者非实施者、仓库只读；扫描 D7 全节、D8 全节、D4 表、`tasks.md` 12.102–12.105 行并逐项源码核验。结论：六案选择、三案否决理由、H06 证据链、锚矩阵全部成立，未见"为省事选最小案"；3 项低成本阻断（消费者计数 12→15、`delivery_fact_test.go` 补入 12.104 写面、A 侧 scoped ④ 立场缺失）已由编排者修正落盘。移交四问独立意见：共享会签可签（互补无冲突）；H06 同意保留全局拒（建议 12.105 增"拒 cut 持续时长×期间新增事实数"两轴）；H04 处置可接受且加严；H05 渲染器不触碰 plugin/catalog 契约。生效条件四条均已满足，D4 自会签记录补齐后生效。 |
| 回滚/反转触发、格式可读性证据与受影响证据失效规则 | 回滚＝丢弃该叶副本改动、仓库内零 diff、如实保留未完成（不勾选、不缩验收面）。反转触发七条：① H02 实施期发现某 holder 在 `Reduce` 内提交事实 → 停并回门选有序队列案；② H01 若需新增索引面或改变 `(partition,seq)` 含义 → 回门，交 06.102（**2026-11-21 判定不触发**：安静点标记不碰索引位置答案，D8.9-H01 已核）；③ H05 若共享渲染器迫使改 `plugin.Instance`／catalog 契约或新增公开面 → 回门；④ H03b 若失败返回 `""`/`false` 破坏既有工具契约而需新错误通道 → 回门；⑤ H04 若无可达入口且必须为测试新增生产入口 → **停止**，该面改记"未验"而不造入口；⑥ H06 若取得"长期拒 cut 致无界热层"的实际证据 → 回门重比 per-slice offset；⑦ 2026-11-21 新增：H02 若实施期发现有序单元使某个真实调用链互锁（不只冻结锚），或 H01 的安静点标记使某个既有成本锚转红且无法由窗口解释 → 停，回本门。格式可读性＝既有记录零变化（见第 6 行），证据为旧根 boot 两枚锚＋旧根无标记的尾段回退锚；任一覆盖字节变化使受影响证据失效，重跑而非手填摘要。 |

**D4.1 内容指纹（第 1 行展开；`shasum -a 256 <相对路径>`，2026-11-21 取值）**

```
657186f3519eaf6df2b6a0a95e2193c2ed7210476c5ea7f2d1e56c5c767ede86  assembly/facts.go
3a4f176b4cc40e5439bcef3e75caccf135fd1b20d613f01ae53bd00c668d5d62  assembly/checkpoint.go
910a253d9feb1f93ca8b03de09659cf8c64f300fbb06fb95458cb9802d55dcd4  assembly/run.go
16fa0c0c863cf82015015c8fbfc8c2176cc7d9f18674e2f0d0435cb0bc15b4fd  assembly/tools.go
4d190d7e38db9d61f5e745a186b2e1a2067b90463c0f3cd033f0b1d40afeca9b  assembly/settle_turn.go
75cb67277f31d65684bffae7e3608e8eb4ec52640bd5edb67fb3144deb3df85d  runtime/state/checkpoint.go
ccbde0dee31aeb8abcec276ba89151c30159543d488d6f73cc7be9fff12b725e  runtime/state/durable.go
8a40120e8f6723e195443b114a7bef3efbbcd455e81b71acdaa5f4abd8675e0f  runtime/task/approval/attention_store.go
d8049b1046f67215db801c173e311729bca44c7ce8ea433988bd383ece171f80  runtime/task/approval/approval_store.go
54a6e0e6e65d072078f9cefb3a4dae57383a7e021bb8bbd23196388c43a61235  features/evolution/governance/goal.go
94a54426dac9ffc6735267d6a1ae422df8dafdbc37882ef950f288eb81598863  runtime/agent/loop.go
```

**D4.2 接口/返回语义/重入约束与全部调用方（第 4 行展开）**

| 对象 | 新语义 | 全部调用方（适配） |
|---|---|---|
| `(*Run).persistFact` → `persistFactOrdered` | 返回 `(ledger.FactCommitOutcome, error)`：`FactCommitted`＝新落盘；`FactAlreadyPresent`／同内容 `FactConflict`＝**重复**（走 `ReduceRepeat`，不登记、不占 `factOrder`）；异内容 `FactConflict`＝具名 `ErrFactKeyConflict`；`FactUncertain`＝具名"结果未知"错误（不登记、不 fold、不答成功）；`FactRefused`＝具名错误。**锁（2026-11-21 更正）**：撤销原"不得自持锁"禁项——`landing` 的取得位置（`assembly/facts.go:451`，在 `chainHoldsFact` 与自身的 `GetEventInPartition` 两次落盘前读之后）可以留，但**释放必须越过 `commitFactNoCadence` 的登记与 `reduceCommitted`**；实现形状二选一（调用方持锁跨到 fold 结束，或把预判读上提后由调用方在 `facts.go:451` 等价位加锁），实施期选定并记入 D9。两条边界不得破：两次落盘前读在锁外（硬约束⑤）、登记与 fold 在锁内。 | 唯一调用方 `assembly/facts.go` 的 `commitFactNoCadence`（含 tier==nil／Owner=="" 早退路径返回"新落盘"）。 |
| `(*Run).landing` 契约 | 由"序号分配锁"扩为"落盘＋登记＋fold 的单一点"；字段名保留（它仍是落点序号的唯一分配点），文档补"应用在同一点内按同一顺序完成，且同一临界段覆盖登记与 fold"。硬约束：①holder 的 `Reduce`/`ReduceRepeat`/`SnapshotState` 内不得提交事实（三个持有者 reducer——projection、compress、delivery——均不提交）；④不得等待依赖 landings 的动作；⑤取得不早于两次落盘前读。 | `assembly/run.go:303 声明`、`run.go:723 boot`、`facts.go:421`、`checkpoint.go:300`；其余持有者 `checkpointWatermark`（`checkpoint.go:299`）与各 `CommitToSession` 入径（grep 已核）。新增等待面：任何提交在 fold 期间等该锁——12.105 须在成本轴单列（D8.9-H02 未验面）。 |
| `(*Run).seatDurableAccount`／`tailFactKeys` | **签名不变**（2026-11-21 更正，原列"增参 `event.CheckpointOffset`"随 H01 改判消失）：`seatDurableAccount(ctx, tier, slices)`、`tailFactKeys(ctx, tier)`。语义：尾段的走停点由"最新框架 checkpoint 的**落点**"改为"最新**安静点标记**键"（H01）；无标记（旧根、无 checkpoint、存储不陈述提交序）⇒ `nil` ＝读全日志，**不得**回落到 checkpoint 落点；空集合仍非 nil。`tailFactKeys` 因此不再调用 `ledger.CommittedIndexEntry` 判类型/会话（判词净删）。 | 两个调用方不变：`checkpoint.go:779`（传 `checkpoint.Slices`）、`run.go:1094`（传 `nil`）；`DurableCommit.Seat` 的 nil/空语义不变。 |
| `(*Run).Checkpoint` 的安静点陈述 | 新增一步：`flight.await` 返回后、`states.SnapshotState` 之前，在**实现 `ledger.CommitOrder` 的存储**上落一条标记键（`tagent/checkpoint/v1/quiet:<分区>:<序号>`，单键单屏障，值只记本 cut 键便于定位）。写失败＝整条 cut 拒落（与"任一切片快照失败整条拒落"同一条红线）；`CheckpointSession` **不写**标记（任务账户只按框架 cut 定界）。**不变式**：`offset ≤ 排水 ≤ 标记 ≤ 快照 ≤ checkpoint 落点`。 | 唯一调用方 `checkpointLocked`（`assembly/checkpoint.go:343`）；成本＝每个框架 cut 一个屏障（`CheckpointCommitInterval=64`）。 |
| `(*state.DurableCommit).attemptOf`／`readReceiptLocked`／`settled` | 读失败（Scan 错／解码错）返回具名错误，不再折成 `false`；`MemoryStore.Attempt` 透传。 | 包内 5 处：`attemptOf` ← `MemoryStore.Attempt`（`memory.go:383`）；`readReceiptLocked` ← `settled`（`durable.go:418/421`）、`attemptOf`（`:768`）；`settled` ← `mailbox_checkpoint.go:84` 的 `owedOfWork`（改返回 `(…, error)`，由 `RestoreState` 具名上返——读错不得把已消费通知重新记为欠）。两个导出面签名不变（`skeleton_assert.go:239` 的 `MemoryStore.Attempt` 保持 `(SettleAttempt, bool, error)`）。 |
| `(*task.ApprovalManager)` / `attentionLedger.RecordAttention` | 签名不变（`(bool, error)`）；语义＝耐久写成功后才发布到内存 `held/order`；并发同键在耐久写后回锁复查，已存在则不再追加 `order`。 | `Request`/`Decide`/`fileQuestion`/`fileAnswer` 不变；`Lookup` 的派生本就只读 facts（`stateOf` 读 `facts.answer`），无需回滚 `req.Status`。 |
| `(*GoalRegistry).Declare` / `Resolve` | 签名不变（`skeleton_assert.go:102/104` 不改）；语义＝治理事件落地才改有效态：失败 `Declare` 返回 `""`、`Resolve` 返回 `false`，内存 `goals/seq` 不变。 | 本树无生产调用方（仅测试）；有效态读者 `policy.go:263/510` 不变。 |
| `runtime/task` 新增 `DeliveryFactKey(Notice) event.EventID`、`DeliveryFactOf(...) event.Event` | 一条通知的投递事实的**唯一身份与唯一渲染**；两写入面共用，第二写成为重复而非第二事实。 | `assembly/tools.go:130`（`settleDeliveryKey` 变薄包装，测试调用面不变）、`runtime/agent/loop.go:731`（`commitSettled` 改用渲染器，删本地拼装）、`assembly/settle_turn.go:523`（读面改用同一函数）。 |
| `(*SettleDriver)` 泵的缺失事实分支 | 事实缺失时用同一渲染器从 seat 提交该事实（身份确定→重试收敛为一条），再走既有驱动；提交仍失败则保持 owed 并具名，不消费。 | `assembly/settle_turn.go` 的 `driveSeat`；`NewSettleDriver` 需多一个"提交投递事实"的 face，调用方 `assembly/settle_turn.go` 的 hostPluginScope 目录项（`assembly/plugin_catalog.go:149`）与 `assembly/settle_turn_test.go`。**2026-11-21 更正（13.104 接线时由编排者授权）**：原写 `assembly/host.go` 是笔误——`NewSettleDriver` 的唯一生产构造点是目录项，`host.go` 只经 `hostPlugins(seam)` 取 `seam.settle`，不在那里构造。 |
| `(*Run).retainNoticeOwed` 的两条路径分开 | 拒绝写路径：**保留 claim**（进程内已交接，claim＝"本进程已交接过"的记录；boot 由 mailbox 切片重建为 pending，故重启仍推进）；close 路径保持 `Release`（未交接、本进程不再驱动）。 | `assembly/facts.go:1005–1010` 两个分支；`assembly/host_test.go` 既有断言按新语义更新并留对照。 |

任何一项缺失或独立审仍有阻断，12.103/12.104 均不可派。候选列表不是全白名单；签名变化牵连的 skeleton_assert、文档或门不能以“编译要求”事后追认，先补精确签发及拥有者。

## D5 与 B/C 的共享语义

| 共享事实 | 唯一归属与交接 |
|---|---|
| 领域等价、五种提交结局、提交序/位置含义及可读性 | B 拥有存储语义；A 拥有提交调用方解释；12.102/06.102 在本 D4 共同签发，不各增一套分类 |
| 快照实际覆盖、尾部起点、任务账户入表先后 | A 拥有覆盖语义；B 提供能兑现的顺序读，不从尚未恢复的 checkpoint 反向构造读它所需的索引 |
| 原文/位置/产物的定位与缺失、拒读区别 | B 拥有读取；A 所有状态和义务消费者传递失败，不降级为空；成本由 06.105 验 |
| builtin/扩展的禁用替换、无 tier、真实恢复入口与证据判定 | C 的 13.102/104 核公开承诺，A 的 12.105 验义务/状态；13.105 合并旧 spec，不以符号删除取消行为 |

## D6 验证与回退

12.101 先以最小真实交错定性；12.103/104 兑现所选设计，12.105 覆盖全局/会话快照、同键/异键、跨 agent/session、经真实 Boot/Assemble 触发的无可用锚/有效锚/允许降级三类生产分支及 checkpoint 子分支、无 tier、审批和生命周期。分支前提、非空观测及损坏拒绝边界见 D3；全量 seed 只作算法参考对照。每条命令及预期命中在 [tasks](tasks.md)，不存在的锚标拟新增，不以零匹配验收。

不让快照恢复重放外部副作用；状态恢复后继续未完义务，以稳定身份核对收据和完成轮次。负变异分别破坏截止、应用顺序、错误传播、义务交接、拒卸载保旧对象，单项被杀不代表全部目标已证。

12.106 由非实施者只读核对旧目标、所选方案、完整候选及全部调用方，逐项核销结构和声明；不是只看测试。失败返回相应叶，签发变化重新审，受影响证据重取。回退不改已提交事实；格式/接口回退可行性在 D4 实证，不预断旧读者会忽略新字段。

## D7 · 12.101 取证记录

> 本节产生方式：12.101 的全部活动＝**只读取证＋在既有测试文件里建最小探针＋本节记录**。未改产品 `.go`、CI、主规格、规则/skill、权限，未 git 写、未 commit，未勾选 12.101 任务框（由编排者核验后代勾）。写面＝7 枚探针落 5 个既有测试文件（`assembly/{facts,checkpoint,host}_test.go`、`runtime/task/approval/approval_store_test.go`、`features/evolution/governance/checkpoint_test.go`）＋本节＋`tasks.md` 的一行指针。`assembly/settle_turn_test.go`、`runtime/state/durable_test.go`、`runtime/task/unsettled_test.go` 在授权写面内但本轮无需改动。

### D7.0 口径与身份

1. **判定词**：沿用 D3 的「证实／证伪／未证」。`证实` 再分「机制」（探针钉住的可观察行为）与「用户可见差异」（该行为对调用方意味着什么）。探针的主题红是取证产物，不是缺陷修复的验收。
2. **身份自查（git 视图不可用）**：`git rev-parse HEAD` 在 `/Users/pengweiye/Documents/codes/tagent` 返回 `fatal: not a git repository`（`.git` 在树但库不可读，符合 ORCHESTRATION「仓外对象视图仅作取证」的既有判定）。改以文件面自查：`find assembly runtime features -name '*_test.go' -newermt '-90 minutes'` 只列出本节 5 个测试文件；`find assembly runtime features memory api -name '*.go' ! -name '*_test.go' -newermt '-90 minutes'` 为空，即本轮**无产品源码改动**。
3. **测试二进制**：`go1.27.1 darwin/arm64`；所有命令从 `-C /Users/pengweiye/Documents/codes/tagent` 起跑，原始退出码见 D7.2，日志留在 `/tmp/rr-12-101/`（scratch，只作执行留档；下表已抄入关键行）。
4. **交错手段**：全部确定性，无 `sleep` 作机制——`cutHoldSlice`（切点内暂停 `SnapshotState`）、`factApplyRecorder`（暂停一条事实的 fold）、`factReadCounter`＋持 `run.landing`（让两个提交都停在落盘前）、`approvalWriteSeam`／`goalWriteSeam`（按写/屏障腿注入失败）。`waitScopeGate` 式 5ms 轮询只作**有界等待守卫**，不是交错手段。

### D7.1 逐疑点结论

| 疑点 | 结论（证实/证伪/未证） | 非空前提 | 用户可见差异 | 原始证据（测试名＋关键日志行＋exit） | 移交 12.102 的处置建议 |
|---|---|---|---|---|---|
| H01 快照先读状态、checkpoint 落盘窗口内的提交是否漏 | **证实**（机制＋用户可见） | 每案都坐实：切已读 watermark 并排空管道、tasks 切片已快照（`cutHoldSlice` 在快照序里排在切片之后）；对照组「切后提交」的同形通知被 boot 正常 seat | 窗口内提交的任务欠账在重启后**不进入信箱**：只要没有任何外部再投递，那次完成的工作永不推进；切片把任务记成 open（陈旧），尾段又不认它，两处都不覆盖 | `TestTaskSnapshotReplaysInterleavedCommit`：`checkpoint_test.go:5035: the booted mailbox seats [{… Event:fact-past-cut …}], want the notification the task that landed inside the cut window owes …`；`-race -count=10` → 10/10 红，原始 exit **1** | 这是「切面覆盖语义」的到期货，必须在 12.102 的「快照真实截止」一栏给出唯一覆盖语义（watermark／尾段起点／切片快照三点同源），并选是否以提交序游标代替「切片＋尾」两源 |
| H02 落盘锁外的 live 应用是否保序 | **证实**（机制＋用户可见） | A 已落盘（seq 更小）且其 fold 被暂停；B 在 A 未应用时完成落盘与应用；两序由存储 `Seq` 与状态缝合的 fold 序直接读出 | 同一链的两条事实在状态缝上的应用序与存储提交序相反：任何「按序陈述」的 holder（投影序列、通知读面、快照里的有序账）在活进程与重启后**不是同一派生状态**，与 `Reduce` 契约「重启派生等于进程前派生」直接冲突 | `TestConcurrentCommitPreservesApplicationOrder`：`facts_test.go:2058: the state seam applied [order-second order-first] while the storage committed [order-first order-second] …`；`-race -count=10` → 10/10 红，exit **1** | 12.102 需在「提交序／应用序一致性」上裁决：要么由单一有序应用点保证同序，要么明确「派生与序无关」并给出每类 holder 的证明；不能只改声明 |
| H02b 同键预查竞态是否重复登记/重复应用 | **证实**（等价案）／**证伪**（异内容案按现行为拒绝） | 两提交都在落盘前读到「链上无此身份」（`factReadCounter` 读到第 4 次时两提交均未落盘；landing 锁由检查持有） | 等价重放被**登记两次、fold 两次**（`factOrder` 同名两次、缝上 fold 两次）；异内容按 `ErrFactKeyConflict` 具名拒绝且只落一条（该半已正确）。重启后唯一记录只有一条，故活进程与重启的派生态不一致 | `TestConcurrentSameFactKeepsOneApplication`：等价案 `facts_test.go:2153: the state seam folded the identity 2 time(s), want one: the order [same-fact-once same-fact-once] …`；异内容案 10/10 `--- PASS`；`-race -count=10` → 父测试 10/10 红，exit **1** | 12.102 应把「提交结局的五值」用到登记点：`FactAlreadyPresent` 必须回到调用方分类，重复不再登记/不再 fold；`Repeat` 面按 11.R2「判重读与写在同一原子单元内」同尺比较 |
| H03a 审批写失败是否仍放行 | **证实**（裁决腿）／**证伪**（问询腿） | 四腿各自注入：问询写、问询屏障、裁决写、裁决屏障；每腿后先读 gate（`Lookup`）再重试 `Decide` | 裁决的写或屏障失败后，`Lookup` **仍读到 approved**：内存里的答案事实未经耐久即让 gate 放行；同一 root 上重启后的书读链答 pending，即「放行后再消失」。问询两条腿不放行（该半正确） | `TestApprovalWriteFailureDoesNotGrantPermission`：`approval_store_test.go:521: the gate approves the pair beside the failed ruling leg &{… Status:approved …}`（裁决写/屏障两子场景各 10/10 红；问询两子场景 10/10 绿）；父测试 `-count=10` exit **1**，`=== RUN` 50（10 父＋40 子） | 对应 07.R2.1「已决审批按身份读链、快照/内存只留开放义务」：答案事实的内存留存须与耐久分离——或按身份读链裁决，或在写失败时不写入 in-memory 裁决面 |
| H03b 目标写失败是否改变有效态 | **证实**（四腿全红） | 声明/解算 × 写/屏障四腿；每腿之后对比「活注册表的 `Active()`/`List()`」与「同一 store 上重新 bind 的注册表」 | 声明事件未落地，目标**照样在飞**（gate 按一条流里不存在的目标裁事）；解算事件未落地，目标**照样离开在飞集**（重启又回来）。运行与恢复两个有效态不同 | `TestGoalWriteFailureDoesNotChangeEffectiveState`：`checkpoint_test.go:1489: the registry holds [g-1:active g-2:active] in force while the store states [g-1:active] after the failed declaration …`（解算腿为 `[]` vs `[g-1:active]`）；`-count=10` → 父 10/10 红、子 40/40 红，exit **1** | 对应 07.R2「治理有效态有界于开放项＋尾，运行与恢复同一 fold」：`Declare`/`Resolve` 的「先改内存后写事实」须改为「写成功才改有效态」，或把失败做成可见的 in-force 例外并在 Query/Count/List 同源呈现 |
| H04 凭证读失败是否只会保欠、不会重驱 | **证伪**（本切点）／残留未证（`attemptOf` 解码失败分支） | 已落终态记录且该任务不在常驻账上的身份；attempt 记录被改写成不可解码的字节；settle 事实可读（故重驱若发生必可归因） | 该 root 上**启动即具名拒绝**（`seat the durable task account … invalid character 't'`），没有任何二次驱动；root 无消费收据，欠账保留。故「读错→false→重驱」在此切点不发生 | `TestUnreadableReceiptDoesNotDriveAgain`：`host_test.go:787: the boot refused the unreadable attempt record and kept the notification owed: tagent: restore the framework checkpoint checkpoint:16:3: … invalid character 't' looking for beginning of object key string`；`-race -count=10` → 10/10 绿，exit **1**（组内另名红，见下） | 怀疑的「静默重驱」在本 fixture 不可达：坏字节先被 seat 的前缀读抓到并具名拒绝。`attemptOf` 的「解码错→false」仍是静态代码反例，但需要一条 seat 不读该键、只有 `attemptOf` 按身份读的入口才能验证；建议 12.102 记「该分支保留或删除」为明确决定，并补该入口 |
| H05 Backflow 保欠是否自动形成可恢复投递 | **证实**（缺口） | 原生工具真实调用 → settle 事实提交被拒（sink 拒写）→ 干净停止 → 同 root 重启；检查**不调用** `deliver`、不补任何 settle 事实，只让产品自己的泵跑 | 重启后信箱确实欠着那条通知，但**没有任何自有路径把它送出去或结清**：0 投递事实、0 消费收据、0 后续轮次；欠账跨任意次重启停在原处，直到外部另有调用者调 `deliver` | `TestBackflowFailureRecoversWithoutManualSettle`：`host_test.go:896: the process after the restart answered 0 delivery fact(s), 0 receipt(s) and 0 follow-up turn(s) with no hand-over from this check … the notification the refused write left stands owed over every restart`；`-race -count=10` → 10/10 红，exit **1** | 对应 12.R2.5「义务保留而投递不停」：保留已成立、续投未成立。12.102 需二选一——补一条真实恢复入口（读欠账→交接→收据），或把「重启后需外部再投递」写成具名限制并给出用户可见代价；不得以「已保欠」代替「可恢复」 |

### D7.2 命令与原始终局

| 命令（`go -C /Users/pengweiye/Documents/codes/tagent test …`） | 选择器 | `=== RUN` | PASS／FAIL | 原始 exit |
|---|---|---|---|---|
| `./assembly -run '^(TestFactCommitLandsOriginalAndRecordInOneCommit\|TestRetryOfFoldedOutputDoesNotReopenTheRound)$' -count=1 -v`（正锚控制） | 2 名 | 2 | 2 PASS / 0 FAIL | **0** |
| `./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit\|TestConcurrentCommitPreservesApplicationOrder\|TestConcurrentSameFactKeepsOneApplication)$' -race -count=10 -v` | 3 名×10 | 50（30 父＋20 子） | 父 0 PASS / 30 FAIL；子 10 PASS（异内容案）/ 10 FAIL（等价案） | **1** |
| `./runtime/task/approval -run '^TestApprovalWriteFailureDoesNotGrantPermission$' -count=10 -v` | 1 名×10 | 50（10 父＋40 子） | 父 0 PASS / 10 FAIL；子 20 PASS（问询两腿）/ 20 FAIL（裁决两腿） | **1** |
| `./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -count=10 -v` | 1 名×10 | 50（10 父＋40 子） | 父 0 PASS / 10 FAIL；子 0 PASS / 40 FAIL | **1** |
| `./assembly -run '^(TestUnreadableReceiptDoesNotDriveAgain\|TestBackflowFailureRecoversWithoutManualSettle)$' -race -count=10 -v` | 2 名×10 | 20 | 10 PASS（H04）/ 10 FAIL（H05） | **1** |

命令未接管道掩盖退出码；红项按本叶性质是反证素材，零匹配／编译失败／夹具早退均未出现（各路 `=== RUN` 计数与预期相符）。

### D7.3 失败切点表逐行可达性

| 切点（任务行原文） | 可达性 | 承载探针与手法 | 备注 |
|---|---|---|---|
| 快照已读／未落盘插新任务 | **可达** | `TestTaskSnapshotReplaysInterleavedCommit`＋`cutHoldSlice`（切在 tasks 切片之后暂停） | 窗口内新任务被证漏 |
| 快照已读／旧任务推进 | **可达** | 同测试：窗口内把切片已陈述的任务推到终态并留欠账 | 与上同一漏点，两处断言分列 |
| A 落盘后暂停让 B 应用 | **可达** | `TestConcurrentCommitPreservesApplicationOrder`＋`factApplyRecorder` | 应用序与耐久序相反 |
| 同键双预查（等价／异内容） | **可达** | `TestConcurrentSameFactKeepsOneApplication`＋`factReadCounter` 与持锁 | 等价重复登记；异内容具名拒绝 |
| attention 每个写与屏障 | **可达** | `approvalWriteSeam` 按 Put/Sync 腿注入，覆盖问询两腿与裁决两腿 | 问询两腿绿、裁决两腿红 |
| 审批每个写与屏障 | **可达** | 同一探针（`Request`／`Decide` 两条真实入口） | 同上 |
| 目标每个写与屏障 | **可达** | `goalWriteSeam` 按腿注入（`Declare`／`Resolve` × 写/屏障） | 四腿全红 |
| consume/reject/attempt 拒读或坏字节 | **部分可达**：坏字节可达；**存储返回错误（拒读）未覆盖** | 坏字节＝`corruptTheLandedAttempt`（改写真实 attempt 键后 `Sync`）；拒读需 engine 读失败注入点，本轮未建 | 该 fixture 下坏字节先被 seat 前缀读抓到，故 `attemptOf` 按身份读的分支不可达；记为未证面而非证伪 |
| Backflow 标记失败 | **可达** | `TestBackflowFailureRecoversWithoutManualSettle`（`loop.SetFactCommit` 拒 `TypeTaskSettled`） | Live 半由既有 `TestDeliveryFactFailurePreservesTheNoticeObligation` 覆盖 |
| 交接后／收据前停止 | **已由既有锚覆盖**（本轮未新建） | `TestRedeliveredNoticeAfterRestartSettlesOnce` 三子场景（`cut_before_the_turn`／`cut_before_the_receipt`／`redelivery_after_the_receipt`） | 该锚的手工预置只作组件对照，不替代新锚 |

### D7.4 C8 行为身份对齐

取证结论按 C8.1 行号引用（口径见 C8.0）：

| 疑点 | 命中的 C8 行为身份 | 对齐说明 |
|---|---|---|
| H01 | **12.R2**（切面不覆盖未应用事实…）、**05.R3**（尾谓词三态：有检查点无更新＝合法空集）、**05.R1**（与事实提交的衔接） | 尾谓词按规格是对的（「切后」确实为空集），错的是它与切片快照合起来留下的窗口；12.R2 的「运行与恢复走同一派生」在此表现为账户漏项 |
| H02 | **12.R2**、**12.R2.1**（恢复按总提交序应用） | 探针只钉住「应用序≠耐久序」；重启后按耐久序重放，即两序分家的事实证据 |
| H02b | **11.R2**（判重读与写在同一原子单元内）、**12.R2** | 等价重放的重复登记/重复 fold 是「提交结局未回到登记点」的直接后果 |
| H03a | **07.R2.1**（已决审批按身份读链、快照/内存只留开放义务）、**07.R2**（运行与恢复同一 fold） | 内存答案在写失败后即放行，正是「内存越过了链」的形态 |
| H03b | **07.R2**、**07.R2.2**（目标载荷只在飞目标与序号） | 有效态在写失败后仍变，且与重启后不同源 |
| H04 | **12.R2.3**（同一通知跨进程至多一条耐久收据，失败仍欠且具名）、**05.R4**（旧信箱载荷按身份判已消费） | 具名拒绝＋保欠与 12.R2.3 一致；「静默重驱」在该切点被证伪 |
| H05 | **12.R2.5**（送达标记写失败后义务保留而投递不停）、**12.R2.3** | 保留成立、续投不成立；「具名但不停」在这条链上是「具名且停」 |

### D7.5 未覆盖面与移交

1. **未证面**：`attemptOf` 的「解码失败→false」分支（H04 残留）；`readReceiptLocked` 的 Scan 错误分支（需 engine 读失败注入点）。两者都需要一条「seat 不读该键、只有按身份读取」的入口，本轮无此入口，不以坏字节代填。
2. **未覆盖入口**：真实 OS 进程终止（本轮所有重启都是同进程内重新 `assemble`，与既有锚同一边界；独立进程切点属 12.105）；无 tier／file 档的同一批切点（12.105 的档位矩阵）。
3. **不修产品**：以上六条证实项都**未**在本轮修改任何产品源码，全部移交 12.102 的设计门；修复方案的选择、字段/签名/顺序语义与负变异落点由 12.102 的 D4 签发承担。
4. **已驳回前提未复立**：全程未以「前 holder 失败必跳过 projection」为任何探针或结论依据。

## D8 · 12.102 方案比较记录

> **产生方式**：只读复核 D7 的七枚探针、源码与既有锚；逐案三案同尺比较后填入 D4 签发区。本节与 D4 签发区是本叶唯一写入面（另加 `tasks.md` 一行指针）；产品 `.go`、测试、CI、主规格、规则/skill、权限**零改动**，未跑新执行、未 commit/push/archive、未勾选 12.102。身份与工具口径同 D4.1；逐案证据等级沿用 D1（已复现缺陷／设计建议／未证）。
>
> **三案口径（顶层 D4 固定，不新增第四类）**：R＝保留现状并限制声明；M＝最小修正现有责任边界；D＝删除/合并一层。逐案必答：独立业务消费者、结构角色、提交/失败/恢复语义、锁时长、重入/自锁、跨根与跨档成本、持久字节副本、格式迁移、测试与运维负担、反转探针。"有序队列／应用游标／预留序号"只是实现 M 或 D 的备选手段，不是第四案。

### D8.1 六案比较表

**H01 快照窗口漏提交**（规格：`session-checkpoint-material`「快照读取与落盘之间有提交」；探针 `TestTaskSnapshotReplaysInterleavedCommit`，红）

| 候选 | 提交/失败/恢复安排 | 锁时长·重入·跨根成本 | 消费者·结构角色·结论 |
|---|---|---|---|
| R 保留现状并限制声明 | 恢复侧仍以"checkpoint 落点"为尾段边界，只在文档记窗口 | 零成本 | 无独立消费者；漏投递已证、规格场景直接不成立 → **否决**（属缩减目标，D4 禁止） |
| **M 安静点标记定界（所选；本行 2026-11-21 由 D8.9-H01 取代原"按陈述 offset 前移"版）** | 提交框架 cut 时，**排水完成之后、`SnapshotState` 之前**落一条"安静点标记"键（`tagent/checkpoint/v1/quiet:<分区>:<序号>`，每 cut 一条、单键单屏障、永不改写）；尾段＝提交序上该标记**之后**的键，`tailFactKeys` 的走停点由"最新框架 checkpoint 的落点"改为"最新安静点标记"。无标记（旧根、无 checkpoint、存储不陈述提交序）⇒ `nil` ＝读全日志，**不得回落到 checkpoint 落点**。原版（按 `pastCut` 取界）已由 D9.2 证伪：五族任务键经 `ledger.TierFactLog` 直 `Put`，不在 `(partition,seq)` 轴上，实测 `chains=11 links=22 tail=42`（成本锚要求 `chains=3 links=6 covered=0`） | 锁/重入面零变化（`checkpointLocked:343` 的 watermark→drain→snapshot→落盘次序不变）；新增成本＝每个框架 cut 一次单键 commit（一个屏障；`CheckpointCommitInterval=64`）；跨根成本＝尾段恰为窗口内键数；持久增量＝每 cut 一条派生键，既有记录布局/版本零变化 | 消费者：`state.DurableCommit.Seat`（任务账户与三族收据的读取界）、mailbox 恢复；结构角色＝任务账户截止语义**唯一一处**（净删"框架 checkpoint 落点"这一第二截止，并净删 `tailFactKeys` 的 `CommittedIndexEntry` 类型/会话判词，`checkpoint.go:889–891`）→ **选中**（比较与证据见 [D8.9-H01](#d891-h01尾段边界的三案与证据)） |
| D 落盘原子化（快照期禁提交） | watermark→drain→snapshot→落盘 全程持同一把锁 | 锁跨度＝整段 holder 读取；holder 在自身锁内提交（任务注册表 durable commit、宿主 settlement 写）将等该锁 → 与 `Stateful` 契约「不持 run 锁读 holder」正面冲突 | 无新增消费者，把既有 redline 推向反面 → **否决** |
| D' 登记与快照同锁（预留序号） | checkpoint 先预留序号再快照，使"落点＝覆盖前缀+1" | snapshot 期间所有提交等预留锁（跨分区全局阻塞）；预留槽在提交失败时成空号，破坏 `coveredEntries`／`eachFactPastCut` 的计数界（`checkpoint.go:1078`） | 与 M 等价的安全结果、更高成本 → **否决**（列作备选手段） |

级联检查（元规则一）：同一"两个截止语义"的模式在**会话** checkpoint 侧另有一处——`sessionCuts`（`run.go:344–350` 声明，`checkpoint.go:449/1016` 写入）在包内**无读者**。本叶不据此宣称已修：记为未证面，交 12.103 实施期核（补读者或删写入，二者皆需在本门补签后才能动）。

**H02 应用序与耐久序失序**（规格「并发提交按同一顺序应用」；探针 `TestConcurrentCommitPreservesApplicationOrder`，红）

| 候选 | 提交/失败/恢复安排 | 锁时长·重入·跨根成本 | 消费者·结构角色·结论 |
|---|---|---|---|
| R 保留现状 | 落盘在 `landing`（`facts.go:421`）、登记与 fold 在锁外（`:125–136`），只改注释声明"派生与序无关" | 零成本 | 投影序列、`endedPrefix`、notice 读面都按位置陈述 → 声明即假 → **否决** |
| **M 单一有序应用点（所选；2026-11-21 D8.9-H02 补死锁证据与锚编排裁定）** | `landing` 内完成 落盘→登记→fold；同一链的 fold 序＝序号序。**锁内跨度与两次落盘前读的关系见 D4.2**（读在锁外、登记与 fold 在锁内） | 锁内新增"登记＋fold"（fsync 已主导，基线 4.1–4.7 ms/次）；重入硬约束两条：holder 的 `Reduce`/`ReduceRepeat`/`SnapshotState` 内不得提交事实（现状三个持有者 reducer —— projection、compress、delivery —— 均只改自身状态，已读码核实），且不得等待任何依赖 landings 才能完成的动作；no-tier 走同一单元，跨根不变 | 消费者：全部按序陈述的 holder，外加 boot 的 `replayChainPastCut`（同一派生函数）；结构角色＝唯一有序点；净增结构 0（复用既有锁） → **选中**。**已实测反证**：按本行原样实施会在冻结锚 `TestConcurrentCommitPreservesApplicationOrder` 上死锁（`panic: test timed out after 2m0s`，主 goroutine 等 `landing`，首提交停在 `factApplyRecorder.Reduce` 内持锁；D9.3 栈证据）。原因是**锚的编排**（`facts_test.go:2035–2043`：`release()` 在第二提交**返回之后**才发出）而非行为断言要求"提交不等应用"；两者分离后裁定为"更新锚编排、断言不变"，见 [D8.9-H02](#d892-h02锚编排假设与行为断言的分离) |
| D 有序应用队列 | 序号在锁内分配，另以 cursor＋pending 缓冲顺序应用 | 缓冲需定界与失败策略；失败 fold 阻塞队列；更关键：排空发生在他人 goroutine 上，会破坏 `enterSession` 的 per-session fence 记账（会话 cut 可能读到本会话的短状态） | 无独立需要 → **否决** |
| D' 由快照/Reduce 吸收乱序 | 声明派生与序无关，各 holder 自证 | 与投影（append 序）、`endedPrefix`（位置）直接冲突 | **否决** |

**H02b 同键等价重复登记**（规格「同一身份不重复产生已完成效果」；探针 `TestConcurrentSameFactKeepsOneApplication` 等价半红、异内容半绿）

| 候选 | 提交/失败/恢复安排 | 锁时长·重入·跨根成本 | 消费者·结构角色·结论 |
|---|---|---|---|
| R 保留现状 | `FactAlreadyPresent` 被折成 `nil`（`facts.go:441`），第二提交按"新事实"再登记、再 fold | 零成本 | 已证重复登记与重复 fold（活进程与重启派生不一致） → **否决** |
| **M 提交结局分类收敛为单点（所选）** | `persistFactOrdered` 回传 B 的既有五值；提交入口分派：Committed＝登记＋fold；AlreadyPresent／同内容 Conflict＝`ReduceRepeat`（不登记、不占 `factOrder`）；异内容 Conflict＝具名拒绝；Uncertain＝具名"结果未知"（不登记、不 fold、**不答成功**）；Refused＝具名错误。判重读与写同处一个有序单元（11.R2） | 签名变化 1 处调用方；无常驻结构；无格式 | 两个独立消费者：①登记点（同身份不入序两次）②H05 的两写入面（同一通知的第二写必须被答成重复而非第二事实）——第二个独立于本结构存在 → **选中** |
| D 调用方去重 | 每个调用方自记"是否已登记" | 把 B 已算出的答案复制到 N 个调用方 | 第二真相 → **否决** |

**H03a 审批写失败仍放行（裁决腿）**（规格「审批或目标持久写失败」；探针 `TestApprovalWriteFailureDoesNotGrantPermission`，裁决两腿红、问询两腿绿）

| 候选 | 提交/失败/恢复安排 | 锁时长·重入·跨根成本 | 消费者·结构角色·结论 |
|---|---|---|---|
| R 保留"内存先发布＋ERROR" | 写失败后内存 fact 仍在，`facts.answer` 答 approved，重读书链答 pending（已证） | 零成本 | 规格与 07.R2.1 直接禁止 → **否决** |
| **M 耐久成功才可见（store 内屏障，所选）** | `attentionLedger.RecordAttention` 先写耐久面、成功后才发布 `held/order`；并发同键在写后回锁复查，已存在则不再追加 `order` | 耐久写本就在锁外，锁跨度不变；无格式；失败面＝具名错误原样上返 | 消费者：`facts.answer`→`stateOf`→`lookupFacts`→真实 gate（`runtime/agent/fuse.go:355`）；结构角色＝单一可见性点 → **选中** |
| D 调用方回滚 | `Decide` 失败时回滚 `req.Status` | 需每个调用方各写一遍；且本轮读码证实 `Lookup` 的授权来源是 **facts**（`stateOf` 读 `facts.answer`），不是 `req.Status`，回滚为无效处 | **否决** |

**H03b 目标写失败仍改有效态**（同一规格；探针 `TestGoalWriteFailureDoesNotChangeEffectiveState`，四腿全红）

| 候选 | 提交/失败/恢复安排 | 锁时长·重入·跨根成本 | 消费者·结构角色·结论 |
|---|---|---|---|
| R 保留"内存条目照旧、gate 继续工作" | 声明/解算失败后有效态仍变（已证：运行 `[g-1:active g-2:active]` vs 重启 `[g-1:active]`） | 零成本 | 与 07.R2「运行与恢复同一 fold」冲突 → **否决** |
| **M 耐久成功才可见（所选）** | `Declare`/`Resolve` 先写治理事件；落地才改 `goals/seq` 并 prune。失败 `Declare` 返回 `""`、`Resolve` 返回 `false` | 签名不变（`skeleton_assert.go:102/104` 不动）→ 零级联；无格式；`writeGoalEvent` 的 `stopped` 具名停号语义保留 | 有效态读者 `policy.go:263/510`；**本树无生产调用方**（仅测试）→ 记明"库契约修复，不宣称生产症状已消失" → **选中** |
| D 把失败做成可见的 in-force 例外 | 保留目标在飞，另立"未落地"标记并在 Query/Count/List 同源呈现 | 每处读面都要渲染例外＝第二真相；重启不还原该态 | **否决** |

**H05 Backflow 恢复缺口**（规格「Backflow 失败由真实恢复泵闭合」；探针 `TestBackflowFailureRecoversWithoutManualSettle`，红）

| 候选 | 提交/失败/恢复安排 | 锁时长·重入·跨根成本 | 消费者·结构角色·结论 |
|---|---|---|---|
| R 保留"已保欠＝已恢复" | 重启后 seat 停在 pending，泵因查不到事实而 `releaseSeat` 返回 | 零成本 | 已证 0 事实/0 收据/0 回合 → **否决** |
| **M 投递义务单一事实源（所选，三处同根）** | ①身份：`delivery_fact.go` 定 `task.DeliveryFactKey`，两写入面（`assembly/tools.go:130`、`runtime/agent/loop.go:731`）共用；②渲染：`task.DeliveryFactOf` 产出唯一事实体，第二写因此成为**重复**而非第二事实；③恢复泵：`driveSeat`（`settle_turn.go:523`）在事实缺失时用同一渲染器**从 seat 提交该事实**再走既有驱动，提交仍失败则保持 owed 并具名；④不重复：拒绝写路径**保留 claim**（进程内已交接）而 close 路径仍 `Release`（未交接），使进程内唤醒看不到该 seat，boot 由 mailbox 切片重建为 pending 故重启仍推进 | 无新增锁；`NewSettleDriver` 增一个"提交投递事实"的 face（`host.go` 与 `settle_turn_test.go` 两处接线）；无格式；旧根上铸造身份的旧事实仍按身份可读 | 消费者：泵（`Unconsumed`→`Claim`→驱动→`Consume`）、宿主投递账户（settlement key）、intake 交接；结构角色＝一条通知的投递事实**唯一**；净删一个第二写入面 → **选中** |
| D 让泵按 task 反查任一 `TypeTaskSettled` | 泵不查确定身份，按任务查任一条终态事实 | 需按 task 的索引查询（成本随历史），且"哪一条算该通知的投递事实"无定义 | 第二真相 → **否决** |
| D' 新增 boot 步骤遍历 seat 再投递 | 在 `restoreUnconsumedEvents` 里另起一条投递循环 | 与既有 boot 唤醒（`host.go:790`）形成两个驱动点；sink 的活路径仍写第二身份 | **否决** |

### D8.2 H06 全局拒 cut 三态比较

判据面（`runtime/state/checkpoint.go`）：`unappliedCutRefusal():329` 被 `SnapshotState`（`:377`）与 `SnapshotSessionState`（`:411`）共同调用，任一 holder 未应用任一事实即拒**全部** cut。

| 态 | 全局拒（现状，所选） | 影响域限制（否决） |
|---|---|---|
| 无错 | 不拒，cut 正常；热层按 `CheckpointCommitInterval`/`Period` 有界 | 同 |
| 瞬时错（读错/一次 fold 失败后同身份重放成功） | 受阻域＝全部 cut，直到该身份成功重放；热层＝期间新增事实；恢复成本＝tail 重放 | 若按"事实所属会话"限制，需先证明该事实不影响他会话切片 |
| 长期错（确定性拒绝，永不成功） | 全部 cut 持续被拒，热层无界增长，恢复成本＝每次 boot 全 tail；失败具名（身份＋holder 错误），**安全形态不改** | 同风险下沉到会话粒度，但仍不能越过该事实 |

否决"影响域限制"的证据（元规则三，边界须画在语义单元上）：会话切片的四个持有面中，`projectionStateSlice`／`inputsStateSlice`／`compressStateSlice` 的状态确为该会话事实的函数，但 `deliveryStateSlice.SnapshotSessionState`（`host_delivery_checkpoint.go:132`）陈述的是"**目标**为该链的义务"，而义务可由另一条链的事实声明（跨会话义务有既有锚 `TestCrossSessionObligationsStayInTheirTargetSlice`）。故"按拒绝事实自身所属会话划界"会把 Y 的切片在不安全时放行 → 全局拒是当前语义下的必要形态。Owner/Target 不同不构成分域证据（本表即其反例）。
备选（per-slice offset／holder 声明的拒绝域）各自要改持久 payload 或新增持有者接口，均无第二消费者 → 不在本单元。长期错的无界增长作为**可观测项**交 12.105（不以"具名失败"当作成本已接受）。

### D8.3 Repeat 保留/删并裁定

| 面 | 证据 | 裁定 |
|---|---|---|
| 已折叠的旧身份 | `projectionStateSlice.ReduceRepeat`（`checkpoint.go:1187`）与其注释：序列丢弃了被折叠/重写材料的身份，重复若被当作 filing 会重述已折叠材料；既有锚 `TestRetryOfFoldedOutputDoesNotReopenTheRound`、`TestRestartRetryOfFoldedOutputKeepsThePresentedRange` | **保留**，并成为重复的**唯一**路径（H02b 修复后 `AlreadyPresent`／同内容 `Conflict` 也走它） |
| 并发新事实 | 不同键而同内容＝新 filing，必须登记与 fold（`TestConcurrentSameFactKeepsOneApplication` 的异内容半已证现行为正确；等价半正是被误当 filing） | 同上；分类由**提交点**产生，不由 holder 自判 |
| 是否并入主面（`Reduce(ctx, evt, repeat)`） | 三个实现中两个不关心该分类（compress、delivery）；仓内已有三例"按需声明面"（`emitter`、`unfinishedWork`、`sessionSnapshotter`） | **不并入**：把分类做成强制参数会把"谁序列化身份"的语义摊给所有持有者；声明面与既有模式同形 |
| 单实现是否自证（元规则二） | 需要独立于该接口而存在：投影确实会丢弃折叠身份（其 `ReduceRepeat` 为空实现即证据），接口只是"seam 如何问"的机制；且 H02b 的分类在提交点已必然存在 | 保留成立 |

### D8.4 D7 两个未证面的裁定

| 面 | 现状（读码） | 裁定 | 实施期取证要求 |
|---|---|---|---|
| `attemptOf` 解码失败（`runtime/state/durable.go:755`） | `json.Unmarshal` 失败 → `durableError` 后返回 `(SettleAttempt{}, false)`；`MemoryStore.Attempt` 因此答"无 attempt"，泵会记录新 attempt 并**再次驱动** | **不按"未证"悬置**：规格「收据拒读不等于不存在」要求"具名报告并停止依赖该判定的再次驱动"。故改为返回具名错误（`readReceiptLocked` 的 Scan 错同理，`:804`），`settled` 一并透传 | 需一条"seat 覆盖该任务（前缀读不触键）、只有按身份读取"的入口：以既有存储替身注入 Scan 失败／改写 attempt 键为不可解码字节。若实施期确认**无**可达入口，则停止实现该面，改记"未验"，**不得**为测试新增生产入口 |
| `readReceiptLocked` Scan 失败（`:804`） | 同上，`durableError` 后 `return nil,false`；其上的 `settled`（`:405`）同样折成 false，`mailbox_checkpoint.go:84` 因此可能把已消费通知重新记为欠 | 同上；`settled`／`owedOfWork` 一并透传（级联见 D4.2） | 同上（注入点可用 `owedOfWork` 的既有夹具） |

两条的共同依据是规格场景，不是对运行的猜测；其"已证"只到静态代码反例一级，故仍标"实施期取证"，不得在 F1 记成已修。

### D8.5 全局复杂度账（净增净减）

| 变动 | 方向 | 依据 |
|---|---|---|
| 删"checkpoint 落点"这一第二截止语义与 `tailFactKeys` 的类型/会话判词（改由安静点标记定界，函数保留） | **净减** 1 语义 + 2 判词；**净增**每 cut 一条派生标记键与一次屏障 | D8.1-H01 / D8.9-H01 |
| `landing` 覆盖登记＋fold（复用既有锁） | 净增 0 结构；新增 3 条硬约束（Reduce 内不提交、不等待 landings、锁不早于两次落盘前读） | D8.1-H02 / D8.9-H02 |
| `persistFactOrdered` 回传结局（B 既有枚举） | 净增 0 类型；删 1 处折叠（`AlreadyPresent→nil`） | D8.1-H02b |
| 三处"发布先于耐久"改为"耐久先于可见" | **净减** 三处隐式第二真相（内存领先链） | D8.1-H03a/H03b |
| 投递事实：两写入面收敛为一身份＋一渲染器，删 loop 侧本地拼装 | **净减** 1 个第二写入面；净增 1 个两消费方共享的纯函数文件 | D8.1-H05 |
| 泵内补齐分支 + `retainNoticeOwed` 保 claim | 净增 1 分支、删 1 次 `Release` 调用；无新增状态 | D8.1-H05 |
| 全局拒 cut 不变 | 0 | D8.2 |
| Repeat 保留（不并入主面） | 0 结构迁移 | D8.3 |

合计（2026-11-21 按 D8.9 重算）：生产结构净减（1 第二截止语义、1 第二写入面、3 处隐式第二真相、`tailFactKeys` 的 2 条判词），净增 1 个共享纯函数模块、1 条派生标记键族（每 cut 一条）与 5 条硬约束；无以测试门转绿为目的的接口。**成本净增两处**（标记屏障／"提交等 fold"串行化面）已列 D4 第 11 行，交 12.105 成本轴，不在本门宣称已抵消。

### D8.6 逐叶验证矩阵

> 命令一律 `go -C /Users/pengweiye/Documents/codes/tagent test …`，不接管道掩盖退出码；"现有"已核定义命中 1，"拟新增"当前零命中（实施期先复核）。预期命中＝选择器实际 `=== RUN` 次数（`-count=10` 即每人 10 次，子场景全部执行）。

**12.103（提交/应用/快照截止）**

| 面 | 锚（文件） | 完整命令 | 预期命中 | 负变异落点／控制绿 |
|---|---|---|---|---|
| 正 | `TestTaskSnapshotReplaysInterleavedCommit`（`assembly/checkpoint_test.go`，现有·当前红） | `./assembly -run '^TestTaskSnapshotReplaysInterleavedCommit$' -race -count=10 -v` | 10；子场景"窗口内新任务""窗口内旧任务推进"各 10 | 变异：尾段停点回退为 checkpoint 落点 → 必红；控制：同次未变异副本绿 |
| 正 | `TestConcurrentCommitPreservesApplicationOrder`（`assembly/facts_test.go`，现有·红；**编排按 D8.9-H02 更新，断言不变**） | `./assembly -run '^TestConcurrentCommitPreservesApplicationOrder$' -race -count=10 -v` | 10；三条断言全部保留：①`run.landing.TryLock()` 在首提交 fold 停靠期间**必须失败**（机制：fold 在有序单元内）②放行前 `appliedOrder()` 不含第二条（B 在 `landing` 上被阻塞）③放行后 `appliedOrder()==[first,second]` 且 `firstSeq<secondSeq`。**不得**再用"第二提交先返回再放行"的编排（该编排与有序单元互斥，D9.3 已实测 `panic: test timed out`） | 变异：fold 移出有序单元 → 断言①必红（`TryLock` 成功即报）；对照组见 [D8.9-H02](#d892-h02锚编排假设与行为断言的分离) 的必测清单 |
| 正 | `TestConcurrentSameFactKeepsOneApplication`（`assembly/facts_test.go`，现有·等价半红） | `./assembly -run '^TestConcurrentSameFactKeepsOneApplication$' -race -count=10 -v` | 10；等价案与异内容案各 10 | 变异：`AlreadyPresent` 走 filing → 等价案红 |
| 正（拟新增，H01 机制） | `TestQuietMarkerBoundsTheTailToTheWindow`（落 `assembly/checkpoint_test.go`） | `./assembly -run '^TestQuietMarkerBoundsTheTailToTheWindow$' -race -count=10 -v` | 10；三子场景：①标记前的任务不被尾段命名 ②排水后、快照前提交的任务被尾段命名 ③旧根无标记 ⇒ `tailFactKeys` 答 `nil`（读全日志），且不以 checkpoint 落点兜底 | 变异：走停点改回 checkpoint 落点 → ②红；变异：无标记时返回落点 → ③红 |
| 成本硬门（H01，绝对不得放宽；文件不在本叶写面） | `TestTaskRestoreBoundedByCheckpoint`（`assembly/task_checkpoint_test.go:186`，现有）、`TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint`、`TestASpellingBasedStopDropsTasksBehindASessionCheckpoint`、`TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks`（`assembly/checkpoint_test.go`，现有） | `./assembly -run '^(TestTaskRestoreBoundedByCheckpoint\|TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint\|TestASpellingBasedStopDropsTasksBehindASessionCheckpoint\|TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks)$' -race -count=10 -v` | 各 10；`TestTaskRestoreBoundedByCheckpoint` 的 `restore folded chains=3 links=6 covered=0` 必须成立（D9.2 的下界读数 `chains=11 links=22 recovered=11 tail=42` 就是本裁定的反例） | 变异：尾段边界提前到任一"索引位置"锚 → 该锚必红；本行三枚既有锚**不改一行**，是"已陈述任务不重复进尾段"的持有处 |
| 负 | `TestRefusedFactCommitPublishesNoKey`、`TestFactCommitJudgesTheDomainBeforeTheEngineBytes`、`TestRetryOfFoldedOutputDoesNotReopenTheRound`（`assembly/facts_test.go`／`checkpoint_test.go`，现有） | `./assembly -run '^(TestRefusedFactCommitPublishesNoKey\|TestFactCommitJudgesTheDomainBeforeTheEngineBytes\|TestRetryOfFoldedOutputDoesNotReopenTheRound)$' -race -count=10 -v` | 各 10 | 变异分别破坏原子性、领域等价、旧身份重试 |
| 消费者 | `TestBootSeatsTheMailboxAfterTheDurableAccount`、`TestRestartRetryOfFoldedOutputKeepsThePresentedRange`、`TestConsumerBehindTheChainSurvivesARestart`（现有） | `./assembly -run '^(TestBootSeatsTheMailboxAfterTheDurableAccount\|TestRestartRetryOfFoldedOutputKeepsThePresentedRange\|TestConsumerBehindTheChainSurvivesARestart)$' -race -count=10 -v` | 各 10；末枚含"拒 cut 策略"断言，按 D8.2 保留 | 不新增生产 hook |

**12.104（领域与义务失败闭合）**

| 面 | 锚（文件） | 完整命令 | 预期命中 | 负变异／控制绿 |
|---|---|---|---|---|
| 正 | `TestApprovalWriteFailureDoesNotGrantPermission`（`runtime/task/approval/approval_store_test.go`，现有·裁决两腿红） | `./runtime/task/approval -run '^TestApprovalWriteFailureDoesNotGrantPermission$' -race -count=10 -v` | 父 10＋子 40；问询两腿绿、裁决两腿转绿 | 变异：发布先于耐久 → 裁决腿红 |
| 正 | `TestGoalWriteFailureDoesNotChangeEffectiveState`（`features/evolution/governance/checkpoint_test.go`，现有·四腿红） | `./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -count=10 -v` | 父 10＋子 40 全绿 | 变异：内存先发布 → 四腿红 |
| 正（含既有消费者） | `TestGovernanceBootReadsSettledAnswerByIdentity`、`TestDeniedRulingMustSurviveItsValidityAcrossRestart`（现有） | `./assembly -run '^(TestGovernanceBootReadsSettledAnswerByIdentity)$' -count=10 -v`；`./features/evolution/governance -run '^TestDeniedRulingMustSurviveItsValidityAcrossRestart$' -count=10 -v` | 各 10 | — |
| 负 | `TestUnreadableReceiptDoesNotDriveAgain`（`assembly/host_test.go`，现有） | `./assembly -run '^TestUnreadableReceiptDoesNotDriveAgain$' -race -count=10 -v` | 10 | 变异：读错回 false → 红 |
| 负（拟新增） | `TestUnreadableAttemptRefusesTheDriveByIdentity`（落 `runtime/state/durable_test.go`） | `./runtime/state -run '^TestUnreadableAttemptRefusesTheDriveByIdentity$' -race -count=10 -v` | 10（实施期先核存在） | 变异：解码错回 false → 红；控制：可解码 attempt 仍答"已记录" |
| 负（补签锚，2026-11-21 收尾会话新增） | `TestUnreadableReceiptFailsTheRestoreByName`（落 `runtime/state/durable_test.go`） | `./runtime/state -run '^TestUnreadableReceiptFailsTheRestoreByName$' -race -count=10 -v` | 父 10；子 20（正例"无回执的通知入席"／负例"扫描被拒 ⇒ 恢复具名失败且零入席"） | 变异：`owedOfWork` 把读错误折回"未结算" → 负例子红；控制：未变异副本同次绿 |
| 正（拟新增） | `TestDeliveryFactOfIsOneFactForBothWriteFaces`（落 `runtime/task/delivery_fact_test.go`） | `./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=10 -v` | 10 | 变异：一写入面改回铸造身份 → 两写成为两事实，红 |
| 消费者 | `TestBackflowFailureRecoversWithoutManualSettle`（`assembly/host_test.go`，现有·红） | `./assembly -run '^TestBackflowFailureRecoversWithoutManualSettle$' -race -count=10 -v` | 10；三观测（投递事实/收据/回合）各自非零 | 变异：缺失事实走 release → 红（0 回合） |
| 消费者（拟新增） | `TestInProcessSettleFailureDrivesNoSecondTurn`（落 `assembly/settle_turn_test.go`） | `./assembly -run '^TestInProcessSettleFailureDrivesNoSecondTurn$' -race -count=10 -v` | 10 | 变异：`retainNoticeOwed` 改回 `Release` → 进程内唤醒驱动第二回合，红 |
| 消费者（现有） | `TestDeliveryFactFailurePreservesTheNoticeObligation`、`TestRedeliveredNoticeAfterRestartSettlesOnce`（`assembly/host_test.go`／`checkpoint_test.go`） | `./assembly -run '^(TestDeliveryFactFailurePreservesTheNoticeObligation\|TestRedeliveredNoticeAfterRestartSettlesOnce)$' -race -count=10 -v` | 各 10；末枚 3 子场景各 10 | 旧断言若含"释放回 pending"，按新语义更新并留对照，不删锚 |

**12.105（全路径回归，追加项；其余沿用 tasks 12.105 既有清单）**

| 面 | 锚 | 命令 | 预期 |
|---|---|---|---|
| 真实恢复三分支 | `TestRecoveryEntriesPreserveStateAndObligations`（拟新增） | `./assembly -run '^TestRecoveryEntriesPreserveStateAndObligations$' -race -count=10 -v` | 每类分支各 10；按 D3 前提与非空观测，`seedFullReplay` 仅算法对照 |
| 无 tier 顺序 | `TestNoTierCommitPreservesApplicationOrder`（拟新增） | `./assembly -run '^TestNoTierCommitPreservesApplicationOrder$' -race -count=10 -v` | 10 |
| 结构负锚 | `TestUnregisterRefusesASliceWithUnfinishedWork`、`TestUnregisterAllowsASettledSlice`、`TestSliceHandoverKeepsTheUnfinishedObligations`（现有） | `./runtime/state -run '^(TestUnregisterRefusesASliceWithUnfinishedWork\|TestUnregisterAllowsASettledSlice\|TestSliceHandoverKeepsTheUnfinishedObligations)$' -race -count=10 -v` | 各 10 |
| 相邻回归 | 包级 | `go build ./...`；`go test ./assembly -count=1`；`go test ./runtime/... ./features/... -count=1` | 原始 exit 0 |

### D8.7 共享语义：所需事实与候选清单（未定版）

> 按派单授权，A/B 共享语义**不在本节独自定版**；以下是 12.102 所需的事实与候选，交编排者与 06.102 会签后单写一条记录。

| 共享事实 | 本单元所需的具体答案 | 候选（供会签取舍） | 影响本单元的落点 |
|---|---|---|---|
| 提交分类与位置含义 | ①索引遍历必须答出每条目的 `(partition, seq)`（`EachIndexNewestFirstUntil` 已答，`memory/ledger/segment_store.go`）；②`seq` 为落点序号，恢复侧可据 `pastCut` 比较 | A：沿用现有答案（不改 B）；B：新增一个批量"按 cut 取键"读面 | H01 尾段边界 |
| 快照覆盖与尾部起点 | "覆盖"＝checkpoint 陈述的 `CheckpointOffset`；"尾段"＝该 offset 之后，"checkpoint 自身落点"不再是边界 | A：单一 offset 边界（本叶所选）；B：两边界并存（现状，已证漏） | H01、12.105 分支观测 |
| 提交结局分类 | 五值由存储给出（已具备），A 侧不再折叠 | A：回传并分派；B：另立语义 | H02b、H05 第二写 |
| 读取失败与不存在 | 读错必须与"不存在"区分（具名失败） | A：`attemptOf`/`readReceiptLocked` 透传错误；B：保持 false | H04 残留 |
| 原文/位置身份 | 本单元不新增原文读取；`Seat` 的 tail 仅用索引键，不读 body | — | H01 成本 |

### D8.7.1 A/B 共享语义会签记录（2026-11-21，编排者单写，两侧生效）

输入：本域 D8.7 候选与 06 域 D9 会签候选；两门独立设计审均已实质认可可签。以下为唯一记录，A/B 各自签发区不再另立版本：

1. **提交结局分类**：五值（新增/已存在/冲突/确定拒绝/结果未知）由存储层给出，A 侧回传分派、不再折叠 `AlreadyPresent`；B 侧 H08 修复使合法聚集不再落入 `FactUncertain`（缩减"结果未知"成因而非新增第六值）。互补无冲突。
2. **索引位置答案**：沿用 `EachIndexNewestFirstUntil` 现答的 `(partition, seq)`；`seq` 为落点序号，恢复侧以 `pastCut` 比较。B 侧顺序与早停位不变；A 侧不为 H01 新增批量读面。**2026-11-21 复核**：本条仍成立且**不需要改签**——D9.2 证伪的是"用索引位置给任务族键定尾段界"（五族键不在 `(partition,seq)` 轴上），而 [D8.9-H01](#d891-h01尾段边界的三案与证据) 的裁定不消费索引位置答案、不新增 B 读面，B 的 `EachCommittedNewestFirst`/`EachIndexNewestFirstUntil` 语义与早停位一字不改。
3. **快照覆盖与尾段边界**：**2026-11-21 修正**：`CheckpointOffset` 只定义**状态切片的覆盖水位**（"切片覆盖"＝该 offset 之前的状态已进快照），不再兼任任务账户的尾段边界；任务账户的尾段＝**提交序上的安静点标记之后**（D8.9-H01），因为五族任务键不经分区 slot、在 `(partition,seq)` 轴上无定义（D9.2 实测）。两者不构成两个截止语义，它们服务两条不同的事实轴且由顺序固定：**`offset ≤ 排水 ≤ 标记 ≤ 快照 ≤ checkpoint 落点`**——落在 `(offset, 标记]` 的事实由切片＋按身份回放覆盖（既有"最多重复一次、绝不漏跳"），落在 `(快照, 落点]` 的任务键由尾段覆盖（本裁定的修复点），"checkpoint 自身落点"两种都不用。B 侧偏移文件不参与发布资格、覆盖语义不变。
4. **读失败与不存在**：两侧一致——读错具名失败，绝不折为"不存在"（A：`attemptOf`/`readReceiptLocked` 透传；B：`ErrJournalCorrupt` 等具名）。
5. **原文身份**：本变更不新增原文读取身份；A 的 `Seat` tail 仅用索引键。
6. **B 行5④（scoped 损坏作用域收窄为身份级）**：A 侧立场见 D4 第 13 行——签发写面不消费 scoped 族装载，无依赖，**会签通过**；06 域 D6 生效条件中的 scoped 前置就此解除，06.103 可派 `kv_original_store.go`/`reference.go` 子项。
7. **H06 附注**：保留全局拒 cut；12.105 观测轴增加"拒 cut 持续时长×期间新增事实数"两轴（独立审建议，无界热层仍为回门触发⑥的开放面）。

1. **写面自查（已执行）**：本轮只改 `changes/12-consumer-state-lifecycle/design.md` 与同单元 `tasks.md`（一行指针）。非测试产品源码零改动，命令与结果：`find assembly runtime features memory api -name '*.go' ! -name '*_test.go' -newermt '-180 minutes'` → **空输出**；同期 `find ... -name '*_test.go' -newermt '-180 minutes'` 只列出 12.101/06.101 的六枚探针文件（非本叶写入）。未改 CI、主规格、`scripts/**`、权限、git 索引/refs。
2. **级联检查**：H01 的 `tailFactKeys` 唯一调用方已核（`seatDurableAccount`，两处调用）；H02 的 `landing` 持有者已 grep 全列（4 处）；H02b 的 `persistFact` 唯一调用方已核；H03a/H03b 的派生读者（`stateOf`、`policy.go`）已核；H05 的 `settleDeliveryKey` 读者/写者/测试面已核（`settle_turn.go:523`、`tools.go:130/149`、`checkpoint_test.go` 4 处）。**会话 checkpoint 的 `sessionCuts` 无读者**（同 H01 模式）记为未证面，交 12.103。
3. **未覆盖面（不得被下游当已证）**：真实 OS 进程终止（D7 全部为同进程重新 assemble）；无 tier／file 档同一批切点；H05 的"外部发送 exactly-once"（规格明确不承诺）；H10/H11（B 侧）；`90fecc56` 基线未复核；独立设计审未执行。
4. **不缩验收面**：六案的验收面沿用 D7 探针与规格场景，实施期若需缩面（例如 H04 无可达入口）须停并上报，不得"改判据后照勾"。

## D9 · 12.103 执行记录

> 产生方式：12.103 实施叶的一次执行会话。**结果：H02b 已实施并三锚转绿；H01、H02 两项因签发方案与冻结锚/既有成本契约冲突而中止**，产品改动只留 H02b 一组 hunk，其余文件回退到基线字节。本行不勾选 12.103；是否完成由编排者按本节凭据核。
>
> **2026-11-21 后续**：本节 D9.2/D9.3 的两条"待裁决"已由设计门裁定，见 [D8.9](#d89--h01h02-重开裁定)。D9 正文按原样保留（历史记录不改写）；D9.2 的"三选一"与 D9.3 的"三选一"以 D8.9 为准。

### D9.0 身份、工具与写面自查

| 项 | 值 |
|---|---|
| 工具 | `go1.27.1 darwin/arm64`；工作目录 `/Users/pengweiye/Documents/codes/tagent`；命令一律 `go -C … test …`，未接管道掩盖退出码 |
| 身份 | `git rev-parse HEAD` 仍为 `fatal: not a git repository`（同 D4.1），故用内容指纹（`shasum -a 256`）核**改前=改后** |
| 指纹 | `assembly/checkpoint.go` `3a4f176b4cc40e5439bcef3e75caccf135fd1b20d613f01ae53bd00c668d5d62`、`assembly/run.go` `910a253d9feb1f93ca8b03de09659cf8c64f300fbb06fb95458cb9802d55dcd4` —— **与 D4.1 逐字符相同**（H01 尝试已全体回退）；`assembly/facts.go` `696ae81e00776d2bd26453f78a93ea132d65e3d36f29cef3e9171178beb92dfe`（基线 `657186f3…`，本叶唯一产品改动） |
| 测试面指纹 | `assembly/checkpoint_test.go` `009b407c…`、`assembly/facts_test.go` `20d4f418…` |
| 写面 | 只写 D4 第 100 行白名单：`assembly/facts.go`（实施）、`assembly/facts_test.go`（4 处调用面改名）、本 `design.md` 与同单元 `tasks.md`。`assembly/checkpoint.go`/`run.go`/`checkpoint_test.go` 改动已整体回退，落盘为基线字节。未碰 `runtime/state/**`、`assembly/{task_checkpoint,commit}.go`、`memory/**`、`scripts/**`、主规格、CI、权限、git 索引/refs |
| 越界自查 | `find assembly -name '*.go' -newermt '-4 hours'` → `facts.go`、`facts_test.go`、`checkpoint.go`、`checkpoint_test.go`、`run.go`、`host_test.go`；前五者为本叶（checkpoint/run 两文件已回退到基线指纹），`host_test.go` 是 12.101 探针文件（非本叶写入） |

### D9.1 H02b 提交结局单点（**已实施、三面转绿**）

落点：`assembly/facts.go:407–490` `(*Run).persistFactOrdered` 由 `error` 改为 `(ledger.FactCommitOutcome, error)`，把存储层五值原样带出（早退路径 `FactCommitted`；预判同内容 `FactAlreadyPresent`／异内容 `FactConflict`；`CommitFact` 的 `Committed/AlreadyPresent/Conflict/Uncertain/Refused` 各自映射，冲突分支复核后同内容仍归 `AlreadyPresent`）；`assembly/facts.go:90–141` `commitFactNoCadence` 按结局分派：

- `FactCommitted` → 原有登记＋fold 路径（不变）；
- `FactAlreadyPresent` → **不登记、不占 `factOrder`、不再 fold**；仅当本提交落盘前链上已持有该身份（`carried`）时走 `reduceCommittedRepeat`（D8.3 的重复唯一路径，保留"重试补未完应用"语义）；另一提交在本提交查看后落地者由那一提交完成应用，本提交不重复；
- `FactConflict` → 具名 `ErrFactKeyConflict`（异内容）；`FactUncertain` → 具名"结果未知"错误（不登记、不 fold、不答成功）；`FactRefused` → 原错误。
- 级联：`persistFact` 唯一调用方（`commitFactNoCadence`）已改；`facts_test.go` 四处直调点补 `_` 接收结局（断言不变）。`landing` 跨度与 `chainHoldsFact`＋`persistFactOrdered` 内预判两次索引读**均未改**（冻结锚 `TestConcurrentSameFactKeepsOneApplication` 的 `factReadCounter(want=4)` 钉住"每提交两次落盘前读"，见 D9.3）。

证据：`./assembly -run '^TestConcurrentSameFactKeepsOneApplication$' -race -count=10 -v` → 原始 exit **0**，`=== RUN` **30**（10 父＋20 子），父 10/10 PASS，等价案与异内容案各 10 PASS；D7.2 基线同名为 exit 1、等价案 10/10 红。

### D9.2 H01 尾段以陈述 offset 为界（**尝试→回退；方案在事实层不可实现**）

已实现并验证的版本（留档 `/tmp/rr-12-103/h01-attempt/`，仓库内零残留）：`seatDurableAccount` 增 `cut` 参、`restoreCheckpoint` 传 `checkpoint.Offset`、`run.go:1094` 传 `nil`；`tailFactKeys` 由"走到最新框架 checkpoint 的落点"改为"走到 cut 的跨越锚"——先 `walkIndexPastCut` 取"每个 cut 命名分区都已答到 ≤ 自己 cut 的条目"的末条（`bounded` 为假、`len(walked)==0`、末条仍 `pastCut`、或 `past` 与 `EventCount()-coveredEntries(cut)` 不符时回 `nil` 走全量），再以该条目的 `(partition,eventKey)` 为锚在 `EachCommittedNewestFirst` 上走一次，收集比锚更新的**全部**键（含非索引键）。此版 `TestTaskSnapshotReplaysInterleavedCommit -race` **PASS**（基线与回退后 10/10 红）。

**为什么必须回退（三条证据，均可复跑）**：

1. **任务键不在 cut 轴**：`DurableCommit` 的五族键（`tagent/task/v1/{spec,fact,consume,attempt,reject}:`）经 `ledger.TierFactLog` 直接 `Put`，不取分区 slot，`EachIndexNewestFirstUntil` 的 `(partition,seq)` 与 `pastCut` 对它们无定义。故 D8.7.1 第 2 条"沿用索引位置答案、A 侧不为 H01 新增批量读面"在尾段这一用法上不成立：**D8.1-H01 的"按 `pastCut` 过滤尾段键"无法实现**，能落的只有"在索引轴上找 cut 跨越锚、再在提交序上取更新的全部键"。
2. **该锚必然早于 cut 时刻**：锚是"最新一条 ≤ cut 的已定位事件"，而 cut 时刻（`checkpointWatermark` 读到 drain 结束之间）之间还会发生**无定位的任务键写入**。于是尾段必然把"cut 之后、checkpoint 快照之前"的已陈述任务也带上，`DurableCommit.Seat` 会为它们各付 5 次按身份读。
3. **成本契约被打破**：`TestTaskRestoreBoundedByCheckpoint`（`assembly/task_checkpoint_test.go:219`，不在本叶写面）在尝试版下红：`want the fold of the three chains past the cut beside no chain the checkpoint covers`，实测 `chains=11 links=22 recovered=11 tail=42`（期望 `chains=3 links=6 covered=0`，即 8 个已陈述任务零枚举）。另两枚既有成本锚同时红（`TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint`、`TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks` 的"尾段不命名已陈述任务"子断言）。回退后三者全绿（`-race -count=1` exit 0）。

**待裁决（建议回 12.102 与 06.102 会签）**：要让尾段既"以陈述 offset 为界"又"不命名已陈述任务"，二者之一必须由 B/共享语义给出——(a) 五族任务键取得 cut 轴上的位置（或提供"按 cut 取键"的读面）；(b) checkpoint 陈述其 cut 时刻在提交序上的位置（载荷新增 → 属格式变化，需重签）；(c) 由 `runtime/state` 提供"键→任务"归属，使 A 侧能从尾段键里减去 `stated` 集合（`tailTaskIDs` 现为未导出，A 不得复制其键形）。在 (a)/(b)/(c) 任一到位前，A 侧只能二选一：保留现状（窗口漏项，H01 缺陷）+ 成本不变，或修正漏项 + 成本退化；**本次选择保留现状，不擅自缩成本契约**。

### D9.3 H02 单一有序应用点（**未实施；签发方案与冻结锚互斥**）

按 D8.1-H02-M 把 `landing` 扩到"落盘→登记→fold"并在 `/tmp/rr-12-103/exp/`（`go test -overlay` 隔离副本，仓库零 diff）实测：`./assembly -run '^TestConcurrentCommitPreservesApplicationOrder$' -race -timeout 120s` → **`panic: test timed out after 2m0s`**，原始 exit **1**。栈证据：主 goroutine 停在 `commitFactNoCadence` 的 `sync.Mutex.Lock`（等 `landing`），首个提交停在 `factApplyRecorder.Reduce`→`reduceCommitted`→`commitFactNoCadence` 内持锁。

原因是锚自身的交错脚本：`TestConcurrentCommitPreservesApplicationOrder` 在**第二条提交返回之后**才 `recorder.release()`（`facts_test.go:2035–2040`）。若 fold 在有序单元内，第二条提交必须等第一条的应用完成（有序），而第一条的应用要等 `release()`，`release()` 又要等第二条返回——**死锁**。反过来，要让第二条提交"先返回、后应用"，应用必须被**延迟**（=队列/游标，D8.1 已否决，本次任务书亦明令不复活）。即：该锚同时要求"应用序=耐久序"与"提交不必等应用"，只有延迟应用能同时满足。此结论与 H02b 的读数一致（锚要求两条提交在落盘前各读两次索引，故有序单元也不能前移到预判之前）。**未改一行代码**，`assembly/facts.go` 的 `landing` 仍是"只覆盖落盘"的原跨度；`commitFactNoCadence` 顶部 `carried`＋`persistFactOrdered` 预判的两次读保持原位。

待裁决：(a) 允许改锚的交错脚本（`release()` 由独立 goroutine 在 `awaitArrival` 之后发出）——则有序单元可落；(b) 采纳延迟应用（有序队列/游标，需重开 D8.1-H02 的比较并重算 `enterSession` per-session fence 记账）；(c) 改签"派生与序无关"并给每类 holder 证明（D8.1-D' 已否决，需新证据）。三选一都必须回 12.102；本次不动。

### D9.4 命令与原始终局（`go -C /Users/pengweiye/Documents/codes/tagent`）

| 命令 | 选择器/计数 | 结果 | 原始 exit |
|---|---|---|---|
| `test ./assembly -run '^TestConcurrentSameFactKeepsOneApplication$' -race -count=10 -v` | `=== RUN` 30（10 父＋20 子） | 父 10 PASS / 0 FAIL；两子案各 10 PASS | **0** |
| `test ./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit\|TestConcurrentCommitPreservesApplicationOrder)$' -race -count=10 -v` | `=== RUN` 20 | 0 PASS / **20 FAIL**（H01 10/10 红：窗口内任务与推进的通知不进信箱；H02 10/10 红：`[order-second order-first]` vs `[order-first order-second]`） | **1** |
| `test ./assembly -run '^(TestRefusedFactCommitPublishesNoKey\|TestFactCommitJudgesTheDomainBeforeTheEngineBytes\|TestRetryOfFoldedOutputDoesNotReopenTheRound)$' -race -count=10 -v`（负锚） | `=== RUN` 30 | 30 PASS / 0 FAIL | **0** |
| `test ./assembly -run '^(TestConsumerBehindTheChainSurvivesARestart\|TestRestartRetryOfFoldedOutputKeepsThePresentedRange\|TestBootSeatsTheMailboxAfterTheDurableAccount)$' -race -count=10 -v`（消费者锚） | `=== RUN` 30 | 30 PASS / 0 FAIL | **0** |
| `build -buildvcs=false ./...` | — | 全仓编译 | **0**（裸 `go build ./...` 因 `git` 不可读报 `error obtaining VCS status: exit status 128`，属既有环境条件，非本叶） |
| `test ./assembly -count=1` | 全包 | 唯一红：`TestTaskSnapshotReplaysInterleavedCommit`、`TestConcurrentCommitPreservesApplicationOrder`（本叶未解两项）、`TestBackflowFailureRecoversWithoutManualSettle`（H05，属 12.104，D7 基线同红）；**H02b 锚已由红转绿**，无新增红 | 1 |
| `test ./runtime/... ./features/... -count=1` | — | 红：`runtime/task/approval`（`TestApprovalWriteFailureDoesNotGrantPermission` 裁决两腿）、`features/evolution/governance`（`TestGoalWriteFailureDoesNotChangeEffectiveState` 四腿）——H03a/H03b，属 12.104，D7 基线同红；其余包绿 | 1 |

### D9.5 负变异（隔离副本，仓库零 diff）

| 案 | 变异 | 控制（未变异同名锚） | 结论 |
|---|---|---|---|
| H02b | `facts.go` 的 `FactAlreadyPresent` 分支去掉提前返回（走 filing）：`go test -overlay=… -run '^TestConcurrentSameFactKeepsOneApplication$' -race -count=3 -v` → exit **1**，`facts_test.go:2153: the state seam folded the identity 2 time(s), want one: the order [same-fact-once same-fact-once]` | 同批 `-count=3` exit **0** | 变异红在**主题断言**（重复 fold），控制绿，判据成立 |
| H01 | 尾段边界由"陈述 offset"回退为"checkpoint 落点"（=回退本尝试） | 尝试版同锚 `-race -count=1` **PASS**；回退后 **10/10 红** | 同一锚随边界翻转，判据成立；但该尝试版因 D9.2 的成本冲突不能留 |
| H02 | `landing` 扩到 fold（有序单元） | 未变异副本红（应用序≠耐久序），变异副本 `panic: test timed out after 2m0s` | **不可用于"变异红/控制绿"**：有序单元本身使锚死锁，无法产出"控制绿"一侧 → 该锚与签发方案互斥（回门） |

### D9.6 越界与未验面

1. **越界**：无。`assembly/checkpoint.go`、`assembly/run.go` 已回退到 D4.1 基线指纹；`assembly/checkpoint_test.go` 的临时适配已整体回退（现 `tailFactKeys` 仍为包函数两参形态，三个尾段成本锚绿）。`scripts/recover-kv/main.go:5` 的注释仍写旧名 `assembly's persistFact`（`scripts/**` 属 C 写面，本叶未改，记为已知文档漂移）。
2. **未验面**：H01 的正确性版本只做过单点验证（`-race -count=1` PASS），未跑 -count=10；其成本结论用三枚既有锚的失败证明，未做基准计时。
3. **未勾选**：12.103 任务框保持未勾。

### D9.7 续作：H01 安静点标记＋H02 单一有序应用点（2026-11-21，第二执行会话）

> 产生方式：12.103 的续作执行，内容＝落地 [D8.9](#d89--h01h02-重开裁定) 的裁定。**H01 与 H02 均已实施**，正/负/消费者三面与成本硬门转绿，另有一项设计门未列的跨所有者遗留（D9.7.5-②）。12.103 任务框仍**不勾**：是否完成由编排者按本节与 D9.1 的合并凭据核。

#### D9.7.0 身份、工具与写面

| 项 | 值 |
|---|---|
| 工具 | `go1.27.1 darwin/arm64`；工作目录 `/Users/pengweiye/Documents/codes/tagent`；命令一律 `go -C … test …`（下表 argv 省略 `-C`），未接管道掩盖退出码 |
| 身份 | `git rev-parse HEAD` 仍为 `fatal: not a git repository`（同 D4.1/D9.0），改以内容指纹核改前改后 |
| 改前指纹 | `assembly/facts.go` `696ae81e…`（H02b 后基线，同 D9.0）、`assembly/checkpoint.go` `3a4f176b…`、`assembly/run.go` `910a253d…`（后两者＝D4.1 基线）、`assembly/task_checkpoint_test.go` `5493be2a…` |
| 改后指纹 | `assembly/facts.go` `748aebfa1403bd9233f108a0fa01c1963e4e5f3df901d699cdb65380151e5c0c`、`assembly/checkpoint.go` `e4785a0b7b5ab8315cff139e9a07adb1c439f4a455d7421818cacf1b34fa5f72`、`assembly/run.go` `6f61e242d9aa8d09194c93df222f239688ad1f1ab40f5146f0dc059c22acfe79`、`assembly/facts_test.go` `8fc1bfbd…`、`assembly/checkpoint_test.go` `0fe412a5…`；`assembly/task_checkpoint_test.go` **`5493be2a…` 逐字符未变**（D8.6 成本硬门三枚既有锚零改动） |
| 写面 | 生产 `assembly/{facts,checkpoint,run}.go`；测试 `assembly/{facts,checkpoint}_test.go`；文档 `docs/wiki/platform/platform-subsystems.md`（D4 第 7 行「必改」）；门输入 `golden/runtime/durable_truth.json`（见 D9.7.5-③）；记录本 `design.md`＋`tasks.md` |
| 未碰 | `runtime/state/**`、`memory/**`、`assembly/{commit,task_checkpoint,task_checkpoint_test}.go`、`api/event/**`、`scripts/**`、CI、主规格、权限、git 索引/refs。`find assembly runtime features memory api -name '*.go' ! -name '*_test.go' -newermt '-90 minutes'` 只列本叶三个生产文件 |

#### D9.7.1 实施落点

**H02＝D4.2 的第二种形状**（「把预判读上提后由调用方在 `facts.go:451` 等价位加锁」；第一种「callee 取锁、caller 释放」无法在 `(outcome, error)` 的签发返回值里交出「锁是否已持有」，只能靠调用侧重算 callee 条件，属隐式协议，故不选）：

- `assembly/facts.go:91–180` `commitFactNoCadence`：两次身份读（`chainRecord`，:118 与 :122）留在锁外（等价案锚的 `factReadCounter(want=4)` 前提），其后 `r.landing.Lock()`（:127）一直覆盖 落盘（`persistFactOrdered`，:129）、结局分派、登记（:163–169）、`movement.Advance`、`attachDeclaredReplay`、fold（`reduceCommitted`，:174）与压缩切面请求／`trimFacts`；`defer r.landing.Unlock()` 注册在 `defer r.flight.leaveSession` 之后，LIFO 使「安静点无在飞单元」仍等价于「已应用」。
- `assembly/facts.go:437` `persistFactOrdered(ctx, evt, stored ledger.StoredEvent, chainHolds bool)`：不再取锁、不再自读身份；重复/异内容判定（`storedFactConflict`）仍在函数内。两次读不可并为一次：`carried`（读一）区分「链上原本就有」与「另一提交在两次读之间落的」，合并即把并发等价案折两次。
- `assembly/facts.go:508` `chainRecord`（新）＝原 `chainHoldsFact` 的那一次索引读，返回记录本身；`:523` `chainHoldsFact` 保留为薄读面，其第二消费者是 `assembly/compress_consumer.go:576`（独立于本结构存在）。
- `assembly/run.go:303–311` `landing` 字段契约注释扩为「落盘＋登记＋fold 的单一有序点」，写明它仍是唯一的序号分配点、holder 的 `Reduce` 不得在单元内提交。

**H01＝安静点标记定界**（不改 checkpoint 载荷、不改 `(partition,seq)` 答案、不新增 B 读面）：

- `assembly/checkpoint.go:346` `checkpointLocked`：`flight.await` 之后、`SnapshotState` 之前调用 `stateQuietPoint`（:363）；写失败即返回错误，整条 cut 拒落。
- `assembly/checkpoint.go:400` `stateQuietPoint`：只在实现 `ledger.CommitOrder` 的存储上写（无 tier／非 journal 档走既有早退），**在 `r.landing` 内**单键单屏障提交，键 `tagent/checkpoint/v1/quiet:<partition>:<seq>`、值＝本 cut 键。取锁是必需的：安静点排水后仍有提交会落盘，取 `landing` 才使「标记之前的键全部已应用」成立（fold 在 H02 的同一单元内），从而「标记之前 ⇒ 必在快照内」。
- `assembly/checkpoint.go:903` `quietPointKeyPrefix`、`:908` `quietPointKey`、`:916` `quietPointCut`（由标记键答出 partition 与 cut 键）、`:942` `quietPointBoundsTheAccount`、`:967` `tailFactKeys`：走停点＝提交序上最新标记，**净删** `CommittedIndexEntry` 的类型/会话判词；缺标记／标记不可解析／标记所指 cut 未落盘 ⇒ `nil`（读全日志），绝不回落 checkpoint 落点；空集合仍非 nil。
- `CheckpointSession`／`checkpointSessionLocked` 零改动：会话 cut 不写标记，任务账户只按框架 cut 定界。

#### D9.7.2 命令与原始终局（`go -C /Users/pengweiye/Documents/codes/tagent`，日志在 `/tmp/rr-12-103-r2/`，scratch 不作持久凭据）

| 命令 | 选择器／计数 | 结果 | 原始 exit |
|---|---|---|---|
| `test ./assembly -run '^TestTaskSnapshotReplaysInterleavedCommit$' -race -count=10 -v`（H01 正锚） | `=== RUN` 10 | 10 PASS / 0 FAIL（D7.2 基线同名 10/10 红） | **0** |
| `test ./assembly -run '^TestTaskRestoreBoundedByCheckpoint$' -count=10 -v`（成本硬门） | `=== RUN` 10 | 10 PASS / 0 FAIL（D9.2 尝试版同锚红） | **0** |
| `test ./assembly -run '^TestConcurrentCommitPreservesApplicationOrder$' -race -count=10 -v`（H02 正锚，三断言） | `=== RUN` 10 | 10 PASS / 0 FAIL（D7.2 基线 10/10 红） | **0** |
| `test ./assembly -run '^TestConcurrentSameFactKeepsOneApplication$' -race -count=10 -v`（H02b 回归＋`want=4` 前提） | `=== RUN` 30（10 父＋20 子） | 30 PASS / 0 FAIL | **0** |
| `test ./assembly -run '^TestQuietMarkerBoundsTheTailToTheWindow$' -race -count=10 -v`（H01 机制锚，拟新增） | `=== RUN` 40（10 父＋30 子） | 40 PASS / 0 FAIL | **0** |
| `test ./assembly -run '^(TestRefusedFactCommitPublishesNoKey\|TestFactCommitJudgesTheDomainBeforeTheEngineBytes\|TestRetryOfFoldedOutputDoesNotReopenTheRound)$' -race -count=10 -v`（负锚） | `=== RUN` 30 | 30 PASS / 0 FAIL | **0** |
| `test ./assembly -run '^(TestBootSeatsTheMailboxAfterTheDurableAccount\|TestRestartRetryOfFoldedOutputKeepsThePresentedRange\|TestConsumerBehindTheChainSurvivesARestart)$' -race -count=10 -v`（消费者锚） | `=== RUN` 30 | 30 PASS / 0 FAIL | **0** |
| `test ./assembly -run '^(TestTaskRestoreBoundedByCheckpoint\|TestSeatTailNamesEveryTaskYoungerThanTheNewestFrameworkCheckpoint\|TestASpellingBasedStopDropsTasksBehindASessionCheckpoint\|TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks)$' -race -count=10 -v`（成本硬门四枚） | `=== RUN` 40 | 40 PASS / 0 FAIL（后三枚零改动） | **0** |
| `test ./assembly -count=1`（相邻回归） | 全包 | 唯一红 `TestBackflowFailureRecoversWithoutManualSettle`（H05，属 12.104，D7/D9.4 基线同红）；H01/H02/H02b 三锚已绿，无新增红 | **1**（仅该红） |
| `test ./runtime/... ./features/... -count=1` | — | 红：`runtime/task/approval`（H03a 裁决两腿）、`features/evolution/governance`（H03b 四腿）——均属 12.104，与 D9.4 基线逐项相同；其余包绿 | **1**（仅该两红） |
| `build -buildvcs=false ./...` | — | 全仓编译 | **0** |

成本读数（同一 fixture，`-v` 的 INFO 行）：修复版 `restore folded chains=3 links=6 covered=0 recovered=3 dropped=0`、`boot seated the task account tasks=3 tail=15 reads=15 keys=9`；D9.2 被证伪的按 offset 版为 `chains=11 links=22 recovered=11 tail=42`。H01 行为锚的 boot 读数为 `tasks=4 tail=15 reads=20 keys=11`、`restore folded chains=4 links=6 recovered=4`。尾段＝窗口键数，未回到 42 键形态。

#### D9.7.3 负变异（`/tmp/rr-12-103-r2/` 的 `go test -overlay` 隔离副本；仓库内零 diff）

| 案 | 变异 | 主题判据 | 结果 |
|---|---|---|---|
| H01 删标记回落落点 | `tailFactKeys` 的走停点还原为 `CommittedIndexEntry`（类型/会话）判词＝checkpoint 落点 | `TestTaskSnapshotReplaysInterleavedCommit` 的「窗口内欠账进信箱」 | **红 10/10**（原始 exit 1）：`checkpoint_test.go:5218: the booted mailbox seats [{… fact-past-cut …}], want the notification the task that landed inside the cut window owes beside the one that landed past it`；控制（未变异同锚 `-race -count=10`）**绿 10/10** |
| H02 fold 移出有序单元 | `commitFactNoCadence` 在 `persistFactOrdered` 返回后即解锁（fold 与登记留在锁外） | 断言①「首提交 fold 停靠期间 `landing.TryLock()` 必须失败」 | **红 10/10**（确定性，每轮约 0.04s，无超时；原始 exit 1）：`facts_test.go:2072: the landing fence stood free beside the parked fold of the first fact, want it held`；控制**绿 10/10** |
| H01 边界回退 | 见 D9.5 第二行（同锚随边界翻转） | 同上 | 引用 D9.5：尝试版 PASS→回退 10/10 红；本节的 H01 变异即其同族（落点回落） |
| H02b 变异 | 见 D9.5 第一行（`FactAlreadyPresent` 走 filing） | 等价案「同身份一次 fold」 | 引用 D9.5：`facts_test.go:2153` 折两次红；控制绿。本轮 `-race -count=10` 复跑该锚 **30/30 绿**，`want=4` 前提未被破坏 |

#### D9.7.4 旧根回落与标记零改写

- **缺标记＝读全日志**：`TestQuietMarkerBoundsTheTailToTheWindow/no quiet point reads the whole log` 删掉标记后仍保留 filed cut，`tailFactKeys` 答 `nil`（不回落落点）；`TestAnEmptyNonNilTailMustNotSeatZeroYoungTasks` 的无 checkpoint 根同答 `nil`；`TestConsumerBehindTheChainSurvivesARestart`（客户端回放路径）与 `TestBootSeatsTheMailboxAfterTheDurableAccount` 全绿。
- **标记零改写**：同一锚的 `the quiet point bounds the tail to the window` 子场景断言两件事——一次 cut 只有一条标记且值＝该 cut 键；第二个 cut 之后旧标记字节不变、家族键数 +1。标记键由 `<partition>:<seq>` 构成，而 cut 自身的落盘使 `landedSeq` 前进，故同一分区不会重写同一位置。

#### D9.7.5 偏离、新发现与未验面

1. **偏离（D8.9 未列的一步）**：`tailFactKeys` 增加「标记所指 cut 必须已落盘」的配对检查（`quietPointBoundsTheAccount`）。理由：`SnapshotState` 的拒绝（H06 全局拒 cut）或 checkpoint 提交失败都发生在标记写之后，于是留下一条**无对应 checkpoint 的标记**；若照 D8.9 §4 只按「标记之后」定界，最新 landed cut 与该标记之间的任务键既不在切片（cut 早于它们）也不在尾段（标记晚于它们），任务账户没有按 offset 的回放，重启即丢那条欠账——正是 H01 要消除的漏项。配对检查使这种位置退化为 `nil`（读全日志），有界性一次性退化而无漏项，且只加一次索引读、不碰载荷/接口/`(partition,seq)` 答案。证据见 `TestQuietMarkerBoundsTheTailToTheWindow/a quiet point beside no filed cut bounds no account`。**提请设计门复核**：若裁定要求别的解法（例如 refuse 路径删标记），本步可替换。
2. **新发现·跨所有者（未修，属 B/设计门）**：`check_dead_producers.py` 报 `memory/ledger/CommittedIndexEntry has no consumer`。该函数原唯一消费者是 `tailFactKeys`，本轮按 D8.5「净删判词」后成为死生产者；删除它或给豁免行需 `memory/**`（B 写面），本叶不得动。D8.6 门只核三锚，故不影响本叶锚面，但 `scripts/gates/lint.sh` 会因此转红，需在 06 单元或设计门补一条。**已清（2026-11-22，12.106 阻断④修复单）**：已在 B 写面按「删除」处置（该函数无接口满足面，不取豁免行），`check_dead_producers.py` exit 0；级联裁定与证据见 06 域 design.md D12.8。
3. **门输入的必要改动**：`check_durable_truth_registry.py`（ADR 0006 R3）对新增持久前缀 fail-closed，故在 `golden/runtime/durable_truth.json` 增登记 `assembly.checkpoint-quiet-point`（kind `kv-store`、file `assembly/checkpoint.go`、prefix `quietPointKeyPrefix`、reason 写明「派生位置数据不是事实」、review_by 2027-05-05）。改后该门 `errors=0` 绿。D4 第 10 行的「门：无新增」指不新增门脚本，此登记是既有门的输入，不改变任何判据。
4. **文档**：`docs/wiki/platform/platform-subsystems.md#framework-checkpoint` 的「晚于水位的由回放覆盖（按身份幂等，最多重复一次、绝不漏跳）」已按 D4 第 7 行改为「状态切片按 offset 覆盖并由回放补／任务账户按安静点标记切分尾段」，并记标记键族与缺标记退化。`check_doc_links.py`：42 文档、130 链接、56 锚、0 error。
5. **未验面**：① 标记屏障的真实耗时与「提交等 fold」串行化量未计时（D4 第 11 行交 12.105 成本轴）；② 真实 OS 进程终止仍未覆盖（同 D7）；③ `assembly/run.go:350–355` 的 `sessionCuts` 仍无读者（D8.1 级联检查的未证面，改动需设计门补签，本轮未动）**——已清（2026-11-22，D12.0.2）**：死写已删；同族 `sessionFiled`／`sessionSlices` 仍无读者，见 D12.0.2 级联观察；④ `check_concept_budget.py`、`check_doc_budget.py` 在本工作树**不可运行**（基线/文件清单取自 `git ls-files` 与基线提交，而 `git` 在本树不可读，fail-closed），本轮以「未新增导出命名类型/接口」静态自查代替，标未验（**2026-11-22 勘误**：本树 `git` 已可读，`check_concept_budget.py` 实跑 exit 0，见 D12.0.2 证据表；`check_doc_budget.py` 本轮仍未跑——其语料由 `git ls-files` 索引定义，本树索引只见 1 条，读数无意义）；⑤ `check_runtime_architecture.py --implementation` 因 `go list` 需 VCS 状态而 fail-closed（同 D9.4 的裸 `go build` 环境条件）；`--check-policy` 模式绿；⑥ `check_runtime_structure.py` 报 `tool/subagent/tool.go: 655 lines > 500`，该文件 2026-11-12 后未改动，属既有红、非本叶。
6. **未勾选**：12.103 任务框保持未勾；本节不主张 12.103 完成。

## D8.9 · H01/H02 重开裁定

> 编号属 D8 系列（12.102 的设计门记录）；因 D9 是更早一次执行会话的冻结记录且不改写，本节物理上排在 D9 之后。

> 产生方式：**只读读码 ＋ 复算 D9 的实测读数 ＋ 本节记录**。产品/测试/锚零改动（本裁定不解锁任何实施动作，只定形状与写面）；本叶写面＝本 `design.md`（D4 第 3/4/6/11/13/100 行、D4.2 三行改写＋一行新增、D8.1 的 H01/H02 两行、D8.5、D8.6 的 12.103 表、D8.7.1 第 2/3 条、D9 首段指针）与同单元 `tasks.md` 的一行指针；**未勾选任何任务框**。证据等级沿用 D1：D9.2/D9.3 的读数是实测（红），本节的机制判断是静态读码。

### D8.9.0 结论先行

| 案 | 裁定 | 一句话依据 | 会签 |
|---|---|---|---|
| H02 单一有序应用点 | **取 (a) 更新锚编排、断言不变** | 死锁由锚的编排（`release()` 排在第二提交返回之后）触发，而不是由任何"提交不等应用"的行为要求触发；spec 的 `并发提交按同一顺序应用` 只要求运行与恢复同序，不要求提交先返回 | 不需要（A 内部） |
| H01 尾段边界 | **取 (b) 的语义、以"安静点标记"落地**（A 侧单写，**不需 B 新事实、不改 checkpoint 载荷**） | 实测 42 键 = 锚点落在"最新已定位事件"，而五族任务键在 `(partition,seq)` 轴上无定义；可减除集合恰为空（`covered=0`）；唯一能同时满足"闭窗"与"不重复命名已陈述任务"的信息是**排水后、快照前**在提交序上的位置，而提交序上的位置只有落一次提交才能无歧义地陈述 | **不需要 B（也不需 C 改）**；见 D8.9.4 |

### D8.9.1 H01 尾段边界的三案与证据

**已定事实（D9.2，实测）**：按"陈述 offset／最新已定位事件"定界的版本使 `TestTaskRestoreBoundedByCheckpoint` 红，读数 `chains=11 links=22 recovered=11 tail=42`，该锚要求 `chains=3 links=6 covered=0`。三个候选的成本必须同尺对这个读数。

**本节补证（静态读码，决定三案的可行性）**：

1. **切片只陈述"未结"任务**：`runtime/task/checkpoint.go:328 settledAtCut()` 把"终态且无义务"的任务排除出切片，`:307 CheckpointCoverage()` 也只答切片恢复过的任务。故 baseline 的 `covered=0`（`assembly/task_checkpoint_test.go:218` 断言）说明那 8 个已结（cut 已覆盖）任务**既不在切片、也不在覆盖集**里——(c) 的减数 `stated` 恰好不含这些任务，**可减除的集合是空集**。
2. **账户枚举的是"有事实记录"的任务**：`runtime/state/durable.go:1116 TaskIDs()` 只遍历 `d.facts`；`assembly/run.go:1121` 用它做 fold 的枚举面，`:1127` 按 `coverage` 跳过覆盖项。于是那 42 个尾段键让 8 个已结任务进入 `d.facts`，`chains` 从 3 涨到 11、`links` 从 6 涨到 22——**成本退化与"fold 不必要"同源，全部来自"尾段命名了已结任务"**。
3. **任务族键无位置**：`memory/ledger/segment_store.go:1011 CommittedIndexEntry` 明确"false beside a key that carries none, such as a fact the tier's fact log committed on the same engine"。故任何"按 `(partition,seq)` 给任务键定界"的方案（含 B 新增读面）都只能退化为"先找一个已定位事件作锚，再在提交序上取其后全部键"——而该锚在"已定位事件稀疏"时必然远早于 cut（本 fixture 只有 1 条已定位事件）。**这条路的天花板就是 42 键。**
4. **安静点存在且已被文档化**：`assembly/checkpoint.go:354–360` 的次序＝读水位 → `flight.await`（排水到无在飞提交、无半应用）→ `SnapshotState` → 落 checkpoint。排水返回之后的那一瞬是唯一"键集合与切片状态严格互补"的时刻：此后提交的键一定不在快照里，此前的键一定已应用。

| 候选 | 是否闭窗（`TestTaskSnapshotReplaysInterleavedCommit`） | 是否守住成本锚 | 成本/结构 | 判 |
|---|---|---|---|---|
| (a) B 侧新增"任务族键按 cut 取键"读面 | 能 | **不能**：见补证 3，任何以"已定位事件"为锚的实现都回到 42 键；除非同时由 B 交出提交序位置，那就已经是 (b) | B 接口/实施/会签＋B 叶未开工 | **否**（不足以独立成立） |
| (b-literal) checkpoint 载荷新增"提交序位置"字段 | 能 | 能 | `api/event/checkpoint.go:86 CheckpointPayload` 的内容寻址输入与 `CheckpointVersion`（`:28`）随之变化；且位置值有两个子形态：**值是键**（须选一个"永不改写"的键，而最新提交键可能被改写 → 早停即漏项）、**值是序数**（需 B 在 `CommitOrder` 回调里交出序数 → 接口涟漪到 `journalKV`、`repeatWorkStore`、`examples/contract/memory_ledger/CommitOrder_example.go`） | **否**（见下"为什么不用载荷字段"） |
| (c) A 侧以 `stated` 减去已归属任务 | 能 | **不能**：补证 1，可减除集合为空（`covered=0` 即证据），减除后仍是 42 键 | 零新增结构，但无效 | **否** |
| **M 安静点标记定界（所选）** | 能（标记在快照之前） | **能**：尾段＝标记之后的键＝窗口本身；fixture 里 8 个已结任务全在标记之前 | 生产：`checkpointLocked` 多一步单键 commit（一个屏障）；`tailFactKeys` 的走停点换成标记前缀（净删 `CommittedIndexEntry` 类型/会话判词）；持久：每 cut 一条派生键，**既有记录/载荷/版本零变化** | **选中** |

**所选案的语义（必须成立的三段，实施期逐条验）**。记 `offset ≤ 排水 ≤ 标记 ≤ 快照 ≤ checkpoint 落点`。**陈述点取安静点而非 cut 时刻**（这一点比 (b) 的字面更紧）：cut 早于排水，把 `(cut, 排水]` 的键也塞进尾段只会重复读切片已陈述的任务，故取排水之后的位置——既不漏窗也不白读。

1. **标记之前**：键已全部应用（排水保证无半应用），若属未结/欠义务任务则在切片里 ⇒ 由切片入账；若属已结且无义务任务则该任务不需要入账（`settledAtCut` 既有语义，读者按身份读回）。**不漏**。
2. **标记之后、快照之前**：键在尾段 ⇒ 按身份读；同一任务若恰好也在切片里则读两遍（幂等，成本小）。**不重**到"第二事实"一级，只有一次重复读。
3. **快照之后、checkpoint 落点之前**：键在尾段 ⇒ 按身份读。**这是 H01 的窗口本身，本裁定唯一新增的覆盖。**
4. **无标记**（旧根、无 checkpoint、存储不陈述提交序）：`nil` ⇒ 读全日志，**不得**回落到"checkpoint 落点"（那会重现窗口漏项）。有界性因此一次性退化，由下一个新 cut 恢复。

**为什么不用载荷字段**：键值形态引入"标记键可能被改写"的漏项面（`memory/ledger/atomic_store.go:74` 的"names a key once, at its newest commit"会使早停在改写后过早）；序数形态要改 `CommitOrder` 的公开回调签名，涟漪到 B 的 `journalKV`、本叶测试替身 `assembly/symptom_anchor_longrun_test.go:567` 与 `examples/contract/memory_ledger/CommitOrder_example.go`，且 B 的 `06.103` 尚未开工。**标记把"位置"直接写成一次提交，位置在提交序上的含义由存储自己给出**，因此零接口面、零载荷变化。

**成本账（同尺）**：被否的 (a)/(c) 的读数已知（42 键，red）；所选案的净增＝每框架 cut 一个屏障（`CheckpointCommitInterval=64`，≈1/64 次提交的 fsync）＋每 cut 一条派生键；尾段读取量恰为窗口键数。**未做基准计时**，故本门只断言"不回到 42 键形态"，性能归 12.105 成本轴。

### D8.9.2 H02：锚编排假设与行为断言的分离

**死锁（D9.3 实测，本节复核代码路径）**：`assembly/facts_test.go:2035–2043` 的次序是——首提交在独立 goroutine 里跑、`recorder.awaitArrival(t)` 把它的 fold 停在 seam 上；**主 goroutine 同步调用第二提交**，返回后才 `recorder.release()`。锁一旦覆盖 fold，第二提交的 `landing.Lock()`（`assembly/facts.go:451`）必然等首提交的 fold，而首提交的 fold 等 `release()`、`release()` 等第二提交返回 ⇒ 三方互等，实测 `panic: test timed out after 2m0s`。

**区分**：

| 面 | 内容 | 是不是用户目标 |
|---|---|---|
| 行为断言 | `appliedOrder() == [first, second]` 且 `firstSeq < secondSeq`（应用序＝耐久序） | 是：spec `并发提交按同一顺序应用`（"运行与恢复 SHALL 按权威提交序得到相同逐条派生及可观察状态"） |
| 编排假设 | "第二提交必须在首提交应用之前**返回**" | **不是**：没有任何 spec 要求提交先返回；所选语义恰恰是"提交返回时其事实已应用" |

**裁定 (a) 的具体编排（三条断言，一次比一次更强）**：

1. 首提交在 goroutine 里跑，`recorder.awaitArrival(t)` 停住它的 fold（保留）。
2. `run.landing.TryLock()` **必须失败**（`run.landing` 是 `sync.Mutex`，同文件 `facts_test.go:2113` 已在用直接加解锁的写法）。这条把"fold 在有序单元内"作为**机制**断言，且对"fold 移出单元"的变异**确定性**报红，不依赖超时。
3. 第二提交在独立 goroutine 里跑；用 `factReadCounter`（`facts_test.go:1959`）等它的两次落盘前读，证明它**已在飞**；此刻 `recorder.countOf(second.Key) == 0`（B 被 `landing` 挡住，未应用）。随后 `release()`，等两条提交都返回，仍断言 ①。

**被否的两种"最小改动"**：(i) 只把 `release()` 移进独立 goroutine（`awaitArrival` 之后立即放行）——两个提交退化为串行，**基线也能绿**，锚失去咬合力；(ii) "改在应用侧放行"（B 的应用边沿触发）——修复版下 B 永远到不了应用边沿，只能靠超时判定，把有界等待变成判据，违反 D7.0-4 的口径。**被否的 (b) 延迟应用**：D8.1 已否决，本次无新证据，不复活。**被否的 (c) 弃序**：违 P2 目标。

**必测对照清单（归 12.103 续作，实测后写入 D9）**：①未变异修复版 `-race -count=10` 绿（10/10）；②基线（无修复）同锚在断言 2 上确定性红（`TryLock` 成功即报，无超时）；③变异"fold 移出有序单元"同锚红；④变异"`persistFactOrdered` 提前解锁"同锚红；⑤`TestConcurrentSameFactKeepsOneApplication`（`want=4` 两次落盘前读）在修复版下仍绿——这条钉住"锁不早于两次读"（D4 硬约束⑤）。

### D8.9.3 与 D4/D8 的改动索引

| 位置 | 改法 |
|---|---|
| D8.1 H01 行 | 原"按陈述 offset 前移"整行替换为"安静点标记定界"，并记 42 键的实测反证 |
| D8.1 H02 行 | 补死锁证据、"编排假设≠行为断言"的区分、指向本节 |
| D4 第 3 行 | 六案选择里 H01 改名并指向本节 |
| D4 第 4 行 | 硬约束 3 条 → 5 条（补"Reduce 不得等待 landings"、"锁不早于两次落盘前读"） |
| D4 第 6 行 | "无持久格式变化" → "既有记录/载荷/版本零变化＋新增每 cut 一条派生标记键；旧根缺键⇒读全日志" |
| D4 第 11 行 | 成本行：补标记屏障与"提交等 fold"串行化面，并记 42 键为被证伪形态 |
| D4 第 13 行 | 反转触发 6 条 → 7 条（补"有序单元致真实调用链互锁""标记使成本锚红"），并标注 H01 的 ② 不触发 |
| D4 第 100 行 | 写面更正：`seatDurableAccount` 增参取消；`api/event/**` 与 `assembly/task_checkpoint_test.go` 明列不改；文档段由"条件"升为"必改" |
| D4.2 | `persistFactOrdered` 锁条款重写、`landing` 契约与新增等待面、`seatDurableAccount`/`tailFactKeys` 重写、新增 `Checkpoint` 安静点陈述行 |
| D8.5 | 两行重算＋合计（成本净增两处单列） |
| D8.6（12.103 表） | H02 锚行改为三条断言并禁旧编排；新增 H01 机制锚行与"成本硬门"行（三枚既有锚不改一行） |
| D8.7.1 第 2/3 条 | 第 2 条复核为仍成立且不需改签；第 3 条改为"offset 管状态覆盖、标记管账户尾段"并写出不变式 |
| D9 首段 | 加一行指向本节（D9 正文不改写） |

### D8.9.4 会签与跨域

1. **B（06.102）**：两案均**不需要**会签。H02 不动 B；H01 不改 `EachCommittedNewestFirst`/`EachIndexNewestFirstUntil` 语义、不改 `(partition,seq)` 答案、不新增读面、不碰 B 的偏移/探测面（D8.7.1 第 2 条维持原判）。
2. **C（13.102/13.105）**：无需改 C7.1 写面。`scripts/scenarios/run_runtime_scenarios.py:198` 的 `tail=none` 口径不变（旧根仍答 `none`）；四份 delta 的 Scenario 未改——`快照读取与落盘之间有提交` 由本裁定兑现，`并发提交按同一顺序应用` 的语义未动。
3. **共享事实**：H01 会在提交序上多一类键，B 若日后按命名空间枚举须知道它只被 A 读；这条只记于此，不改 D8.7.1 的六条正文。

### D8.9.5 未验面与续作验收门

1. **未实测**：标记案的三个成本锚与 `TestTaskSnapshotReplaysInterleavedCommit` 的转绿、标记屏障的真实耗时、旧根回退锚。**本门只定形状，不宣称已修。**
2. **H02 的串行化面**：`landing` 覆盖 fold 后，"提交等 fold"的等待量未测（D4 第 11 行已列为 12.105 必测轴）。
3. **停止条件**：若 12.103 续作实测 (i) 三枚成本锚仍红、(ii) 标记步使 watermark→drain→snapshot 次序出现新窗口、(iii) 有序单元使任一真实调用链互锁（非冻结锚）——**任一条成立即停**，回本门重比，不得就地改判据或缩小验收面。
4. **不得做的替代**：不得为过锚而把 `assembly/task_checkpoint_test.go` 的 `chains=3 links=6 covered=0` 放宽；不得把 `TryLock` 机制断言换成超时判据；不得删 `factReadCounter(want=4)` 的前提（两次落盘前读在锁外）。

### D8.9.6 长跑回归与无屏障标记（2026-11-22，编排者裁决）

> 编号属 D8.9 系列（派令原称 D8.9.2，该号已被 H02 裁定占用，顺延至此）。产生方式＝编排者 2026-11-22 已证的根因与已裁方案（交接事实）＋本节推导记录；实施与逐项复跑凭据见 [D9.11](#d911--长跑回归修复无屏障标记2026-11-22f2-回叶)。本节**取代** D8.9.1 成本账的「所选案的净增＝每框架 cut 一个屏障」一项与 D4 第 11 行同一项（正文不改写，按本节读）。

#### 事实与根因（已证，勿重做）

| # | 事实 | 证据 |
|---|---|---|
| 1 | 候选树 seed-1×256 长跑两次确定性红（第 8 代断言失败：结算跟进被截＋收尾预算超时） | `~/Documents/codes/tagent-f2-evidence/2026-11-22-f2/event-long/seed-1-window-256{,-r2}/repeat-1/evidence.json`（`go_test_exit_code=1`；`test-log.jsonl` 记 `smoke_test.go:3718 generation 8 failed`） |
| 2 | HEAD 基线同分片绿 | `/tmp/f2-head-s1w256/repeat-1/`，树＝`/tmp/f2-head-tree`（`assembly/checkpoint.go` 与 `git cat-file -p HEAD:assembly/checkpoint.go` 同哈希，无 `stateQuietPoint` 步） |
| 3 | 无标记探针绿（仅跳过 `stateQuietPoint`） | `/tmp/rr-lr-b`：与候选同 `assembly/facts.go`/`memory/ledger/journal_kv.go` 字节，`stateQuietPoint` 在 `CommitOrder` 早退之后插入 `if true { return nil }`（注释原文 `probe: skip the marker write; a missing marker reads the whole log`）⇒ `match`（`/tmp/rr-lr-b-out/repeat-1/evidence.json`，`go_test_exit_code=0`） |
| 4 | 根因 | `assembly/checkpoint.go` 的 `stateQuietPoint` 在 `r.landing` 锁内调一次完整 `tier.Store.Commit`；按 `memory/ledger/atomic_store.go:45` 契约「A commit takes exactly one barrier」，等于**每 cut 在 landing 锁内多付一次 fsync**。256 窗高频折叠下全部事实提交排在这一屏障之后，收尾预算被吃掉 |

#### 方案推导：顺序与耐久分离

1. **标记要的只是顺序**：H01 窗口语义只要求标记记录在**快照之前**进入提交序（排水后的安静点），使「标记之后、快照之前」落地的键落进尾段；它不要求自身在 cut 返回前耐久。
2. **耐久可由后续屏障承担**：journal 是单文件追加序，`fsync` 刷的是文件前序全部字节；标记之后的任何一次成功 `Commit`（本 cut 自己的 checkpoint 事实提交即是）把标记一并刷盘。故标记只需 `journalLog.append`，不必 `file.Sync`。
3. **撕裂与缺席都是已定义读法**：崩溃在标记与切面提交之间 ⇒ 帧缺席或尾撕裂（open 截断到末完整帧）⇒ `tailFactKeys` 走不到该 key，答 `nil`（读全日志，保守正确）；标记在而 cut 未落 ⇒ `quietPointBoundsTheAccount` 答 false，同样 `nil`。两者都不是「部分应用」。
4. **窗口内「提交完成未登记」仍由 landing 锁闭**：H02 的登记（注册＋fold）仍在 `r.landing` 内完成（`assembly/facts.go`）；标记只追加、不登记，故锁序不变、无新增等待面。
5. **存储面**：既有 `Commit` 面必带一次屏障，不能用作本用途 ⇒ 新增**单方法窄口**`AppendOrdered(ctx, batch)`（只入追加序、不屏障、错误如实），实现在 journal 引擎；签发记 [06 域 D6](../../06-bounded-open-index/design.md#d6-签发区06102-已填待独立设计审生效) 的 F2 补签行。无新面的引擎退回带屏障 `Commit`，故既有锚与成本面逐字节不变，也不会出现「静默不写标记」。

#### 不变量保持

| 不变量 | 论证 |
|---|---|
| H01 窗口语义（切片与尾段互补） | 标记仍在 `flight.await` 之后、`SnapshotState` 之前，且仍在 `r.landing` 内追加：它之前落地的事实全部已应用并入切片，之后落地的键在尾段 |
| 双陈述容忍（幂等读） | 「标记之后、快照之前」落地的任务可能既在切片又在尾段；该读法即 D8.9.1 第 2 段，未改 |
| 撕裂/缺席读法 | 见推导 3；两种答案都是 `nil`＝读全日志，不新增第三态 |
| 提交结局五值 | 标记面独立于 `Commit` 的结局分类；append 失败如实归 `ErrCommitUncertain`，cut 具名失败、不落 checkpoint |
| 成本 | 每 cut 由「1 帧＋1 屏障」降为「1 帧＋0 屏障」（屏障由 checkpoint 事实自己的提交承担）；D8.9.1 的屏障项作废 |

#### 未验面（本节不宣称）

- 本节只定形状与推导；match 结论、抽片、锚矩阵与负变异见 D9.11。
- 屏障去掉后的实际耗时未做基准计时；F2 六长与 preflight 仍是权威成本面。

## D9.8 · 12.104 执行记录（2026-11-21，收尾会话）

> 编号属 D9 系列；因 D9 是更早一次执行会话的冻结记录且不改写，本节物理上排在 D8.9 之后（与 D8.9 同一处置）。产生方式：收尾一个撞轮次上限的前置单——实施已落盘、三锚已绿但无记录；本节补做「超签写面审计 → 冻结源码后的完整矩阵 → 负变异 → 记录」。**本叶不勾选**；是否完成由编排者按本节凭据核。

### D9.8.0 结论先行

| 项 | 结论 |
|---|---|
| H03a 审批写失败不放行 | 已实施（`attention_store.go`：耐久先于入链，hold 例外）；正锚 `-race -count=10` 10/10 父、40/40 子，exit 0 |
| H03b 目标写失败不改有效态 | 已实施（`goal.go`：声明/解析先写链、后改内存，废弃号归还）；正锚 `-race -count=10` 10/10 父、40/40 子，exit 0 |
| H04 读失败具名 | 已实施（`durable.go` 三读面＋`memory.go` 透传＋`mailbox.go`/`mailbox_checkpoint.go` 恢复侧出口）；尝试锚 3 子场景、恢复锚 2 子场景，`-race -count=10` 20 父/50 子全绿 |
| H05 投递义务单一事实源 | **组件面**已实施（唯一身份＋唯一渲染器＋泵内补齐）；**生产驱动接线不在本叶**：`assembly/plugin_catalog.go` 那一行按审计已回退，归 13.102/13.104 补签（D9.8.1-①）。故 H05 只能按"组件绿＋生产入口待接"记账，不得宣称生产症状已消失 |
| 相邻回归 | `./assembly ./runtime/... ./features/...` 一抹红：`./runtime/agent TestOrphanSettlementDrivesNewSegment`，A/B 归因本叶 `loop.go`（D9.8.5-①）→ **待裁决** |
| 门 | `scripts/gates/check_concept_budget.py` 由红转绿：内部化 `settleDriverOption` 后 `domain_surface.new=57` 未抬冻结上限（D9.8.5-②） |
| 12.104 完成 | **否**：一条相邻回归红未处置、生产驱动接线待 13.104、D8.6 的 `TestInProcessSettleFailureDrivesNoSecondTurn` 未建立（D9.8.5-③） |

### D9.8.1 超签写面审计（逐 hunk 定夺）

**① `assembly/plugin_catalog.go`（不在 12.104 签发面，属 13.104）→ 回退**

hunk 内容仅一行：`WithSettleFactCommit(seam.run.commitFactChecked)`（settle 驱动构造处，`plugin_catalog.go:160`）。定夺过程按派单规定的经验判据执行：用对象视图 `git show HEAD:assembly/plugin_catalog.go` 的内容恢复该文件（恢复后 `shasum -a 256`＝`80349ee0f9dfe1d037702633cbe19809805ba79831a18c81f7b877f35126d658`，`git status` 不再列为修改），复跑 H05 消费者锚 `./assembly -run '^TestBackflowFailureRecoversWithoutManualSettle$' -count=1` ⇒ **exit 0（2.585s）**。锚未转红 ⇒ 该 hunk 不承载本叶证据 ⇒ **回退成立**，不在本叶直接落地。

**后果必须随记录走（不得被下游读成已修）**：回退后生产装配路径上 settle 驱动的 `commit` 面为空，泵遇到链上缺失的投递事实会答 `ErrSettleDriverUnwired` 并 `releaseSeat` ⇒ **H05 的"重启 0 事实/0 收据/0 回合"在生产入口仍未修**。本叶消费者锚之所以仍绿，是因为检查自己用同一公开构造函数接上了 commit 面（`withSettleFactCommit(booted.commitFactChecked)`），这是**组件级**证据，不是生产入口证据。回 13.102/13.104 补签的验收面：在该处补 `withSettleFactCommit(seam.run.commitFactChecked)`，并把 H05 消费者锚改为（或新增为）经 `builtinPluginCatalog` 装配的入口；在此之前的本轮结论只能是"机制已实现、生产未接线"。

**② `runtime/state/mailbox.go`、`runtime/state/mailbox_checkpoint.go`（D4 只签了 `durable.go`＋`memory.go`）→ 判留并补签**

逐 hunk 同根因证明（判留不是"编译要求"式追认）：

1. **编译强制**：把两文件回退到 HEAD、保留已签的 `durable.go` 改动后 `go build ./runtime/state` 报 `runtime/state/memory.go:122:24: cannot use consumptions[0] ... *DurableCommit does not implement durableConsumption (wrong type for method settled): have settled(task.Notice) (bool, error), want settled(task.Notice) bool`。接口 `durableConsumption` 的唯一实现者就是本叶已签的 `durable.go`，**接口声明（`mailbox.go`）与唯一消费者（`mailbox_checkpoint.go`）必须随签名同改**，不存在只改 `durable.go` 的形态。
2. **同根因**（D8.7.1 第 4 条"读失败与不存在"＋D7 的 H04 残余切点）：旧 `owedOfWork` 把 `settled` 的读错误折成"未结算"，恢复据此把**已结算**的通知重新入席并交给第二个回合；`durable.go` 修的是同一缺陷的**判据侧**（`readReceiptLocked` Scan 错、`attemptOf` 解码错不再转 false），这两文件是它在**恢复侧**的唯一出口，不是新需求。
3. **独立锚**（补签的可咬证据）：新增 `TestUnreadableReceiptFailsTheRestoreByName`（落已签的 `runtime/state/durable_test.go`），正例"无回执的通知入席"＋负例"扫描被拒 ⇒ 恢复具名失败且零入席"；`-race -count=10` 父 10、子 20 全绿；变异把该错误折回 false 时**只有负例红**（D9.8.4-m5）。

补签行已写入 D4（"12.104 补签"行），本叶无接口/持久格式/能力声明变化，唯一消费者是 `RestoreState`（启动）。

### D9.8.2 实施落点与已证切点的对应（本叶 hunk）

| 文件 | 落点 | 已证切点 |
|---|---|---|
| `runtime/task/approval/attention_store.go` | `RecordAttention` 先 `storeAttention` 再入 `held`/`order`；`statesAHold` 只对 hold 保留"写拒仍留簿" | D7.1 H03a 裁决腿 |
| `features/evolution/governance/goal.go` | `Declare`/`Resolve` 先 `recordsOperation` 后改内存；`unmint` 归还废弃声明号 | D7.1 H03b 四腿 |
| `runtime/state/durable.go` | `settled`/`attemptOf`/`readReceiptLocked` 三读面把失败具名，冲突/不存在/读失败三分 | D8.4 H04 残余两分支 |
| `runtime/state/memory.go` | `Attempt` 透传账户错误 | 同上（消费者面） |
| `runtime/state/mailbox.go`＋`mailbox_checkpoint.go` | 恢复侧把该错误传出到 `RestoreState` | D9.8.1-②（补签） |
| `runtime/task/delivery_fact.go`（新增） | `DeliveryFactKey`（`settle-<hex>`）与 `DeliveryFactOf`（唯一渲染器） | D8.1 H05 |
| `runtime/agent/loop.go` | backflow 改用同一渲染器、删 `resultFactOnChain` | D8.1 H05 第二写入面（含 D9.8.5-① 的未申报语义变化） |
| `assembly/tools.go` | `observeSettleDelivery`/`settleDeliveryKey` 改用同一渲染器 | H05 第一写入面 |
| `assembly/settle_turn.go` | `commit` 字段＋`withSettleFactCommit`＋`deliveryFactOf` 泵内补齐＋`terminalDelivery` | H05 泵 |
| `assembly/facts.go` | **本叶 hunk 为零**（`retainNoticeOwed` 在候选 `7b0cde16` 里已存在；该文件工作树差异全属 12.103） | — |
| 测试 | `approval_store_test.go`、`governance/checkpoint_test.go`、`host_test.go`、`durable_test.go`、`delivery_fact_test.go`（新增） | D8.6 12.104 表 |
| 文档 | `docs/wiki/agent/agent-architecture.md` §四（四字段→五字段、"永不再点火"改判为"靠账不靠事实"、attempt 栅栏读失败具名）、`docs/wiki/agent/governance-enforcement.md`（审批簿写入次序＋hold 例外）；`task-lifecycle.md` 已核无对应段落（`席位`/`attempt`/`读取失败` 三词零命中） | 随代码走 |
| 门 | `scripts/gates/check_concept_budget.py` | D9.8.5-② |

### D9.8.3 命令与原始终局

> 全部命令 cwd＝`/Users/pengweiye/Documents/codes/tagent`，前缀 `env GOFLAGS=-buildvcs=false`；日志在 `/tmp/rr-12-104/logs/`（scratch，不作持久凭据）。取被测进程原始 exit，无管道。

| 命令 | 终局 | 命中 |
|---|---|---|
| `go test ./runtime/task/approval -run '^TestApprovalWriteFailureDoesNotGrantPermission$' -race -count=10 -v` | 0 | 父 RUN/PASS 10/10，子 RUN/PASS 40/40，0 FAIL |
| `go test ./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -race -count=10 -v` | 0 | 父 10/10，子 40/40，0 FAIL |
| `go test ./runtime/state -run '^(TestUnreadableAttemptRefusesTheDriveByIdentity\|TestUnreadableReceiptFailsTheRestoreByName)$' -race -count=10 -v` | 0 | 父 PASS 20，子 PASS 50 |
| `go test ./runtime/task -run '^(TestConsumptionFailureKeepsTheNoticeOwed\|TestSettlementFailureIsReportedBeforeDelivery)$' -race -count=10 -v` | 0 | 各 10/10 |
| `go test ./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=10 -v` | 0 | 10/10 |
| `go test ./assembly -run '^TestBackflowFailureRecoversWithoutManualSettle$' -race -count=10 -v`（冻结源码后终跑） | 0 | 10/10（22.772s） |
| `go test ./assembly -run '^(TestUnreadableReceiptDoesNotDriveAgain\|TestGovernanceSliceWiredInProduction\|TestGovernanceBootReadsSettledAnswerByIdentity)$' -race -count=10 -v`（终跑） | 0 | 各 10/10（57.369s） |
| `go build ./... -buildvcs=false` | 0 | 零输出 |
| `go test ./assembly ./runtime/... ./features/... -count=1` | **1** | 仅 `runtime/agent TestOrphanSettlementDrivesNewSegment` 红；其余包全绿（含 `runtime/state`、`runtime/task`、`runtime/task/approval`、`features/evolution/*`） |
| `python3 scripts/gates/check_concept_budget.py`（内部化前） | 1 | `ERROR ... settle_turn.go:SettleDriverOption: new exported named type has no concept binding`；`concept ratchet: domain_surface.new=58 exceeds the frozen ceiling 57` |
| `python3 scripts/gates/check_concept_budget.py`（内部化后） | 0 | `counts.domain_surface.new: 57`／`RESULT: ok` |

### D9.8.4 负变异（`go test -overlay`，隔离副本 `/tmp/rr-12-104/mut/`，仓库零 diff）

| 变异 | 落点（overlay 替换） | 主题锚终局 | 主题失败行 |
|---|---|---|---|
| m1 发布先于耐久 | `attention_store.go`：`RecordAttention` 回到"先入 `held`/`order`，再写、失败也 return true" | `TestApprovalWriteFailureDoesNotGrantPermission` **FAIL**（exit 1）；问询两腿仍绿，裁决两腿红 | `approval_store_test.go:521: the gate approves the pair beside the failed ruling leg ... want it unruled` |
| m2 内存先发布 | `goal.go`：`Declare` 先落 `g.goals` 再写链、`Resolve` 先改状态再写链（都忽略失败） | `TestGoalWriteFailureDoesNotChangeEffectiveState` **FAIL**（exit 1） | 四腿全红：`the declaration event is refused`／`the declaration barrier does not confirm`／`the resolution event is refused`／`the resolution barrier does not confirm` |
| m3 读错回 false | `durable.go`：`readReceiptLocked` Scan 错与 `attemptOf` 解码错都 `return ..., false, nil` | `TestUnreadableAttemptRefusesTheDriveByIdentity` **FAIL**（两个负子场景红）＋`TestUnreadableReceiptFailsTheRestoreByName` 负例子红；两个正例子仍绿 | `durable_test.go:1031`／`:1055`／`:1106` |
| m4 缺失事实走 release | `settle_turn.go`：`deliveryFactOf` 删去泵内补齐，缺失即答 `ErrSettleDriverUnwired` | `TestBackflowFailureRecoversWithoutManualSettle` **FAIL**（14.11s，非超时） | `host_test.go:897: ... answered 0 delivery fact(s), 0 receipt(s) and 0 follow-up turn(s) ...` |
| m5 恢复侧折回"未结算" | `mailbox_checkpoint.go`：`owedOfWork` 的 `settled` 错误折成 `answered=false` | `TestUnreadableReceiptFailsTheRestoreByName` **FAIL**（只有负例子红） | `durable_test.go:1106: the restore beside a refused receipt read = <nil>, want the log refused the read of the receipt family` |

控制绿＝同一批锚在未变异副本上的终局（D9.8.3 各行 exit 0，同一台机、同一候选）；仓库内零 diff（变异只改 `/tmp/rr-12-104/mut/` 下的副本，`git status` 未见这些路径）。

### D9.8.5 偏离、决策与未验面

1. **未申报的可观察后果＋级联遗漏（偏离，待裁决）**：设计确实声明了"一渲染器"与"删 loop 侧本地拼装"（D8.1-H05 第③行、D8.5 对应行），单一渲染器按定义也不可能让事实体随写入面变化；但设计**没有声明其可观察后果**——孤儿结算驱动的模型请求不再携带工具结果——也**没有把受影响的 `runtime/agent/consumer_test.go` 纳入写面**，于是相邻回归转红。A/B（同机、同候选、只换一个文件）：现行 `loop.go` ⇒ `./runtime/agent -run '^TestOrphanSettlementDrivesNewSegment$'` FAIL（`consumer_test.go:1989: the request the late settlement drove carries no observation`，请求里只有输入与 `tagent: task task-late-settlement settled` 两行）；`-overlay` 把 `loop.go` 换回 HEAD ⇒ 同锚 **ok**。归因确定为本叶改动，非既有红（D9.4/D9.7 基线该包全绿）。该测试文件不在本叶写面，故未改；机制是 `consumer.go:1330–1360` 的 `pairResults` 只把 `delivered` 与 `s.pending` 的**在飞 call** 配对，孤儿结算无 pending call ⇒ 永不走 `observationOf`。两条候选处置：(i) 把 `consumer_test.go` 纳入 12.104/12.105 写面，把第 4 条断言改为"观测由通知指名的那条**结果事实**在链上读回"，fixture 改成真落结果事实；(ii) 在 `consumer.go` 给孤儿结算补 intake 配对。两者都改本叶外文件，须编排者裁定；本轮不得以"测试过期"自行改判据。
2. **决策：`SettleDriverOption` 内部化为 `settleDriverOption`**。门报 `new exported named type has no concept binding: mechanism is not a domain concept ... or internalize it`，且 `domain_surface.new=58 > 57`。该类型是构造步骤而非领域概念，唯一构造方（catalog 与同包检查）都在包内 ⇒ 按门的建议内部化（`withSettleFactCommit` 同改），`new` 回到 57，**未抬 ceiling、未加绑定表条目**。若 13.104 判需对外暴露，须回设计门补绑定，不得直接导出后追认。
3. **未验面**：① `TestInProcessSettleFailureDrivesNoSecondTurn`（D8.6 12.104 表"消费者（拟新增）"）**未建立**——`assembly/settle_turn_test.go` 仍是候选字节，该行的变异判据（`retainNoticeOwed` 改回 `Release`）因此无锚；② 生产驱动接线（D9.8.1-①）未落地，H05 不得宣称生产已修；③ `go test ./tests/integration/...` 未跑（不在本叶相邻回归清单）；④ 三份 delta 未改（本叶未新增 Requirement）；⑤ H05 的"外部发送 exactly-once"仍不在承诺面（沿用 D7 口径）。
4. **回退与反转**：本轮无 `git checkout`、无 `git restore`；`plugin_catalog.go` 用对象视图内容恢复（sha `80349ee0…`＝HEAD）；`mailbox.go`/`mailbox_checkpoint.go` 自审计前字节未变（`72c076fd…`／`98de1fd3…`）。反转触发：若 13.104 接线后 H05 消费者锚需改经 catalog 装配而转红，说明本叶的泵内补齐形态不足，回 D8.1 H05 行重比。
5. **设计行陈旧（留给 13.102/13.104 的债）**：D4.2 第 143 行把 `NewSettleDriver` 新 face 的接线方写成 `assembly/host.go` 与 `assembly/settle_turn_test.go`，而当前真实构造点是 `assembly/plugin_catalog.go` 的 hostPluginScope 目录项（该目录化改造晚于 D4.2 落笔）；两处都不在 12.104 写面。补签时按真实构造点接线，并顺手改正 D4.2 该行的调用方名单，不要把 12.104 的写面当作它的授权。

### D9.8.6 收尾会话（第三单）：孤儿结算回归、遗漏锚与移交（2026-11-21）

> 产生方式：12.104 收尾的第三次执行会话。范围＝编排者裁定的三件（孤儿结算回归按方案 i、补建 D8.6 遗漏锚、H05 生产装配缺口封账）＋终验。本轮**改 1 个测试文件、探针后还原 1 个测试文件、记录 2 个文件**；生产 `.go` 零改动，未 git 写、未 commit/push/archive、**未勾选 12.104**。
>
> **第四次执行会话（2026-11-21，同一节续写）**：按 D4 第 12.104 行原签实施 ② 的 H05 保 claim hunk——生产改 `assembly/facts.go` 一处、测试改 `assembly/settle_turn_test.go` 与 `assembly/host_test.go`、记录本文件与 `tasks.md`；未 git 写、未 commit/push/archive、**未勾选 12.104**。详情见下方 ② 全部条目。

#### 结论先行

| 项 | 结论 |
|---|---|
| ① 孤儿结算回归 `TestOrphanSettlementDrivesNewSegment` | **已修复转绿**：fixture 真落结果事实、第 4 条断言改为"从链上结果事实读回"；`-count=10` 10/10，补充 `-race -count=5` 5/5，exit 0 |
| ② 补建 `TestInProcessSettleFailureDrivesNoSecondTurn` | **已实施并转绿**（2026-11-21 第四单，按 D4 原签授权）：拒写分支保 claim（`assembly/facts.go:1058–1061`，close 路径仍 `Release`）＋锚落 `assembly/settle_turn_test.go` ＋`host_test.go` 两处断言同步；`-race -count=10` 10/10，变异 m1 3/3 红。前单"未成绿，阻塞上报"的记录见本节 ② 第 1 条保留段 |
| ③ H05 生产装配缺口 | **如实封账并移交 13.104**（见下）；H05 仍只按"组件绿＋生产入口待接"记账，不得宣称生产症状已消失 |
| 全包终态 | `go build ./...` exit 0；`go test ./assembly ./runtime/... ./features/... -count=1` **exit 0、11 包 ok、0 FAIL**（第四单复跑同终局，assembly 485.819s） |
| 概念预算门 | `check_concept_budget.py` exit 0（`counts.domain_surface.new: 57`／`RESULT: ok`，未抬冻结上限） |
| 12.104 完成 | **否**：②已建立、③未接线 ⇒ 本行仍不勾选 |

#### 身份、工具与写面

| 项 | 值 |
|---|---|
| 候选 | `git rev-parse HEAD` → `7b0cde162627ad37f1d364bbf3508b3bd0518431`（本轮**可读**，与 D4.1/D9.0 的 `fatal: not a git repository` 已不同）；`git status`/`git diff` 仍把全树报为已删除（index 为空的既有环境态，不按 ORCHESTRATION「不重建 index」处置），故身份以内容指纹＋`git show HEAD:<path>` 对象视图核 |
| 工具 | `go1.27.1 darwin/arm64`；cwd `/Users/pengweiye/Documents/codes/tagent`；命令前缀 `env GOFLAGS=-buildvcs=false`，不接管道掩盖退出码；日志 scratch `/tmp/rr-12-104/logs/`（不作持久凭据） |
| 指纹（`shasum -a 256`） | `runtime/agent/consumer_test.go` `334fa085…` → **`e26568a5309e1a074a32e44802f2ad62ed694d94c634e453798fa2fe5adc8929`**（本叶唯一改动）；`assembly/settle_turn_test.go` `b23e39c8…` → 探针后**还原**为 `b23e39c88e08c8f9d4868449027fdf4da9e90505444711668dbb6dd5f2a4ae1e`（＝HEAD 字节）；`runtime/agent/consumer.go` `3a3da55c…`＝HEAD（未改）；`assembly/facts.go` `748aebfa…`＝D9.7.0 改后值（本轮未改）；`assembly/plugin_catalog.go` `80349ee0…`＝HEAD（未改） |
| 写面自查 | 只写 `runtime/agent/consumer_test.go`、本 `design.md`、同单元 `tasks.md`；`assembly/settle_turn_test.go` 写入探针后已还原。未碰 `assembly/{facts,host_test,plugin_catalog}.go`、`runtime/task/**`、`memory/**`、`scripts/**`、主契约、CI、权限、git 索引/refs |

#### ① 孤儿结算回归（编排者裁定方案 i，已实施）

落点全在 `runtime/agent/consumer_test.go`：

- **fixture 真落结果事实**：新增 `residentInstanceRig.landResult`，把工具结果作为 `TypeActionCommand`＋`TaskID` 的结果事实落链（`Owner/Source/Target`＝地址、`CallID` 取 `ToolID`），与既有 `landInput/landNotice/landAnswer` 同形并列；删去 `rig.loop.facts` 的桩（进程内第二份拷贝）。
- **断言改为从链上结果事实读回**：新增 `residentInstanceRig.settledFactsOf`；第 4 条断言改为 `resultFactOf(rig.history.SessionFacts(addr), lateTask)` 读回 `lateFact` 的 `Content`／`ToolID`（用的正是拉取面按 task 查结果事实的**生产**函数），并断言该任务只有一条投递事实且 `Message.ToolID == ""`（观察不随投递事实同行，投递事实只有结算行）。
- **未弱化的语义**：第二回合仍由孤儿结算单独驱动——`InvocationID`＝`<session>#c2`、模型请求 2 条、`agent_output` 2 条三条断言原样保留。
- **未改生产**：`runtime/agent/consumer.go` 指纹仍＝HEAD（方案 ii「给孤儿结算补 intake 配对」未签，不采纳）。
- **语义后果（必须随记录走，不得被下游读成"观测消失"）**：孤儿结算驱动的模型请求**不再携带工具结果**（实测请求只含输入与 `tagent: task task-late-settlement settled` 一行）；工具结果仍可从通知指名的那条结果事实在链上读回。这是单一渲染器（`task.DeliveryFactOf`，D9.8.2）的直接后果，即 D9.8.5-① 报备的"未申报可观察后果"，经编排者裁定按方案 i 处理后，本轮把它写进锚的主题断言而不是留在散文里。**契约口径**：规格要求的是"进入模型请求的内容可由日志重建"（模型可见⇒落账），不是"链上每条事实都必须进请求"；后者会与"已折叠旧身份不重开"（D8.3）冲突。
- 证据：`go test ./runtime/agent -run '^TestOrphanSettlementDrivesNewSegment$' -count=10 -v` → exit 0、`=== RUN` 10、`--- PASS` 10、`--- FAIL` 0；基线（修复前）同名锚 exit 1、`consumer_test.go:1989: the request the late settlement drove carries no observation`（D9.8.5-① 的 A/B 已归因到本叶 `loop.go`）。

#### ② `TestInProcessSettleFailureDrivesNoSecondTurn`：已实施并转绿（2026-11-21，第四单，按 D4 原签授权）

0. **第四单身份与写面**：候选 `7b0cde16`；生产只写 `assembly/facts.go` 一处（D4 第 12.104 行"H05 保 claim 不释放，只此一处"），测试写 `assembly/settle_turn_test.go`（新增锚）与 `assembly/host_test.go`（两处 `ClaimPending` 断言同步）；记录本文件与 `tasks.md` 一行。未 git 写、未 commit/push/archive、**未勾选 12.104**。落定指纹：`assembly/facts.go` `1cc37e0f…`、`assembly/host_test.go` `ea24b890…`、`assembly/settle_turn_test.go` `d4c84384…`（矩阵前后两次 `shasum -a 256` 相同）。
1. **两路径切分核对（先读 D8 原文，不改设计）**：三处签发同旨——D4.2 末行「`retainNoticeOwed` 的两条路径分开」＝拒写路径**保留 claim**／close 路径保持 `Release`；D8.1-H05 ④ 同句（"进程内已交接，claim＝本进程已交接过；boot 由 mailbox 切片重建为 pending，故重启仍推进"）；D8.6 12.104 表该行变异列要求"`retainNoticeOwed` 改回 `Release` → 进程内唤醒驱动第二回合，红"。与前单探针**不矛盾，而是同一因**：探针红的成因正是拒写后 `Release` 把席位送回 pending，`drain` 每轮读全部 pending 席位因而在同一进程重读它；本 hunk 关掉的正是这条回投。前单阻断事实（保留）：`assembly/facts.go:1054`（close）与 `:1057`（拒写）**同调**恒 `Release` 的 `retainNoticeOwed`，`host_test.go:527` 断言 pending，按 D8.6 定义实测 `PROBE turns=1 seats=[]`（`/tmp/rr-12-104/logs/inproc-evidence.log`）。
2. **生产 hunk（只此一处）**：`assembly/facts.go:1058–1061`——拒写分支不再调 `retainNoticeOwed`，改为记 debug 并 `return nil`（claim 不释放）；`retainNoticeOwed` 仍是 close 路径的**唯一**调用者，其 `store.Release` 体逐字未动；`sink` 与 `retainNoticeOwed` 的文档注释改写为两路径各自语义（新增一条 bullet 说明 claim 为何不回投）。与 HEAD 的差异合计＝该分支 2 行换 1 行＋文档注释；`bookNotice`、`Consume`、close 路径零改动。**重启仍推进的依据（读码核实）**：`OwedNotices` 只在消费/拒绝时删键（`runtime/state/durable.go:867/910`），claim 状态不入该集合，故保 claim 的通知仍在 checkpoint 的 mailbox 切片里，boot 由 `acceptLocked`（`runtime/state/mailbox.go:182`）重建为 `ClaimPending`。
3. **锚**：`assembly/settle_turn_test.go` 新增 `TestInProcessSettleFailureDrivesNoSecondTurn`——真 `Run`（`deliveryFactObligationRun`）＋`run.loop.SetFactCommit` 只拒 `TypeTaskSettled`；前提断言（拒 1 次、`KindCompleted`／2 决策＝进程内已交接）→ 同进程以 13.104 的最终接线 `NewSettleDriver(..., withSettleFactCommit(run.commitFactChecked))` 唤醒泵（两次 wake：第二次读数只在第一次返回后取得，故读到的是交接后的账户位置而非仍在飞的排水）→ 主题断言：泵读账户 ≥2 次、**0 claim**、**0 回合**、席位仍 `ClaimClaimed`、链上 0 投递事实、根上 0 消费回执。消费者面＝泵的真实入口（`Unconsumed`→`Claim`→驱动→`Consume`）与真实存储，不是组件替身。
4. **同锚断言同步（不弱化）**：`assembly/host_test.go` 两处 `ClaimPending` 断言改 `ClaimClaimed`（`TestDeliveryFactFailurePreservesTheNoticeObligation`、`TestBackflowFailureRecoversWithoutManualSettle`）；"欠账仍成立"由 `Unconsumed()` 仍列出该席位承载（未消费即欠账），两处测试文档 bullets 各留一条"旧语义：拒写后回 pending"的对照，未删锚、未改 `waitScopeGate` 的判据形态。
5. **命令与原始终局**（`env GOFLAGS=-buildvcs=false`，cwd 根；日志 `/tmp/rr-12-104-f/`；取被测进程原始 exit、无管道）：

| 命令 | 终局 | 命中 |
|---|---|---|
| `go test ./assembly -run '^TestInProcessSettleFailureDrivesNoSecondTurn$' -race -count=10 -v` | **0** | `=== RUN` 10／`--- PASS` 10／`--- FAIL` 0／`DATA RACE` 0（22.4s） |
| 同锚 `-count=3 -v`（变异比对的控制臂） | **0** | RUN 3／PASS 3 |
| `go test ./assembly -run '^(TestDeliveryFactFailurePreservesTheNoticeObligation\|TestBackflowFailureRecoversWithoutManualSettle)$' -race -count=10 -v` | **0** | 两名各 RUN 10／PASS 10（合计 20/20）／FAIL 0／`DATA RACE` 0 |
| `go test ./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=3 -v` | **0** | RUN 12（父 3＋子 9）／PASS 12 |
| `go build ./... -buildvcs=false` | **0** | 零输出 |
| `go test ./assembly ./runtime/... ./features/... -count=1` | **0** | 11 包 ok／0 FAIL（assembly 485.819s；runtime/state 0.501s、runtime/task 1.476s、runtime/task/approval 1.282s、runtime/agent 4.312s、governance 2.514s） |
| `python3 scripts/gates/check_concept_budget.py` | **0** | `counts.domain_surface.new: 57`（冻结上限 57）／`RESULT: ok` |
| `go run ./scripts/comment_policy -no-baseline -v assembly` | **0** | 34 finding(s)＝本单前同数（首稿曾引入 5 条：两处函数体内注释与测试文档形态超限，已全部改为文档 bullet／断言串），本单零新增 |

6. **负变异（`go test -overlay`，隔离副本 `/tmp/rr-12-104-f/`，仓库零 diff）**：m1＝把拒写分支回退为 `return r.retainNoticeOwed(ctx, frame, notice, seq)`（＝恒 `Release`；变异副本 `/tmp/rr-12-104-f/mut/facts.go` `5c235125…`，与原文件 diff 5 行）→ 同锚 `-count=3 -v` **exit 1、3/3 红**，失败行 `assembly/settle_turn_test.go:308`："the pump of this process drove 1 follow-up turn(s) beside the refused delivery fact (1 claim(s)), want none"——与前单探针 `turns=1` 同果，且主题断言（第二回合）而非编译/夹具早退命中。控制绿＝未变异副本同锚 `-count=3` 3/3 与 `-race -count=10` 10/10（同候选、同命令形态）。
7. **未勾选与残留未验面**：12.104 仍**不勾**——③（H05 生产装配接线）在 13.104；`go test ./tests/integration/...` 仍未跑；"外部发送 exactly-once"仍不在承诺面。本 hunk **不**改变 ③ 的封账结论：生产构造点未接 `withSettleFactCommit` 时泵遇缺失事实仍答 `ErrSettleDriverUnwired` 并 `releaseSeat`。

#### ③ 移交 13.104：H05 生产装配接线（如实封账）

- **缺口**：`assembly/plugin_catalog.go:149` 的 hostPluginScope 目录项构造 `NewSettleDriver(...)` 时**没有**接 `withSettleFactCommit(seam.run.commitFactChecked)`（该 hunk 已按 D9.8.1-① 的超签审计回退）。face 为空 ⇒ 泵遇链上缺失的投递事实答 `ErrSettleDriverUnwired` 并 `releaseSeat`，**H05 的"重启后 0 事实／0 收据／0 回合"在生产入口仍未修**。
- **补接线的验收面**：13.104 在真实构造点补 `withSettleFactCommit(seam.run.commitFactChecked)`，并把 H05 消费者锚改为（或新增为）**经 `builtinPluginCatalog` 装配**的入口。现探针是检查自己用同一公开构造函数接上 commit 面（`withSettleFactCommit(booted.commitFactChecked)`），属**组件级**证据，**不得冒充**生产入口证据。
- **设计行事实更正（对派单描述的纠正）**：派单称"D4.2 第 143 行已由前单改正"，实测**未改**——该行仍把新 face 的接线方写成 `assembly/host.go` 与 `assembly/settle_turn_test.go`。真实构造点是 `assembly/plugin_catalog.go` 的 hostPluginScope 目录项。本轮不改签该行（属设计门，非本叶记录面），移交 13.104 补签时一并改正（即 D9.8.5-⑤ 的债）。
- **反转触发**（沿用 D9.8.4-4）：若 13.104 接线后 H05 消费者锚需改经 catalog 装配而转红，说明泵内补齐形态不足，回 D8.1 H05 行重比。

#### 终验（原始 exit／RUN 计数，全部 `env GOFLAGS=-buildvcs=false`，cwd `/Users/pengweiye/Documents/codes/tagent`）

| 命令 | 终局 | 命中 |
|---|---|---|
| `go build ./...` | **0** | 零输出 |
| `go test ./assembly ./runtime/... ./features/... -count=1` | **0** | **11 包 ok／0 FAIL**（assembly 337.161s、runtime/agent 4.374s、runtime/state 0.660s、runtime/task 1.236s、runtime/task/approval 1.357s、features/evolution 0.748s、features/evolution/governance 3.070s、features/evolution/meditation 1.700s、features/rl 2.835s、features/rl/sample 2.270s、features/telemetry 1.972s） |
| `go test ./runtime/agent -run '^TestOrphanSettlementDrivesNewSegment$' -count=10 -v` | **0** | `=== RUN` 10／`--- PASS` 10／`--- FAIL` 0 |
| 同锚 `-race -count=5 -v`（补充，非 D8.6 行列） | **0** | `=== RUN` 5／`--- PASS` 5／`DATA RACE` 0 |
| `go test ./assembly -run '^TestBackflowFailureRecoversWithoutManualSettle$' -race -count=3 -v`（抽样复跑） | **0** | `=== RUN` 3／`--- PASS` 3／0 FAIL |
| `go test ./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=3 -v`（抽样复跑） | **0** | 父 `--- PASS` 3；`=== RUN` 12（父 3＋子 9） |
| `python3 scripts/gates/check_concept_budget.py` | **0** | `counts.domain_surface.new: 57`（冻结上限 57）／`RESULT: ok` |

负变异：**本轮不新增**，引用 D9.8.4 的五案（m1–m5，`-overlay` 隔离副本，仓库零 diff）。①的回归由"基线红→修复绿"的同锚对照承载（D9.8.5-① 的 A/B 与本节证据）。

#### 未勾选与未验面

1. **未勾选**：12.104 任务框保持未勾；本节不主张 12.104 完成（②已建立于第四单、③仍未接线）。
2. **未验面**：②的完整 10/10 与 `-race` **已于第四单取得**（同锚 `-race -count=10` 10/10，见 ② 第 5 条）；仍缺 `go test ./tests/integration/...`（沿用 D9.8.5-③）；H05 的"外部发送 exactly-once"仍不在承诺面。
3. **未改判据**：①的验收面沿用编排者裁定的方案 i；②不做任何代理化（不写"驱动一个回合"的断言、不以无接线生产路径当绿锚）。

## D9.10 · 残项七红裁定与修复（2026-11-21，第五单）

> 派单：修复 `tests/integration` 七红（history 2、capabilities 1、host 4）。本节是逐红裁定、改动、命令终局、负变异与未验面的唯一记录；`tasks.md` 12.104 行追加指针。**生产 `.go` 零改动**（本单四文件全为测试），未 git 写、未 commit/push/archive、**未改 12.104 勾选状态**。

### D9.10.0 结论先行

| 项 | 结论 |
|---|---|
| 七红裁定 | **7/7 判为 (b) 过时断言**：全部钉的是 12.104 已签实现废除的"两写面两形状"模型（交付事实无 notice 元数据／无正文）与 H03b 废除的"内存领先链"。无一枚是生产行为回归 |
| 归因（A/B，`-overlay` 隔离副本） | 回退 `runtime/agent/loop.go`→HEAD：host 4 红 + history 2 红转绿、`TestGovernanceRecovery` 仍红；回退 `features/evolution/governance/goal.go`→HEAD：`TestGovernanceRecovery` 转绿（另引入 3 枚新红）。故 6 红属 `loop.go`（H05 单渲染器）、1 红属 `goal.go`（H03b） |
| 全命令终局 | 七红 **0 FAIL**；同一命令仍有第 8 枚红 `TestEventIntegrityRestart`（非七红之内，见 D9.10.5-②） |
| 第 8 枚红（后补） | **已清**（第六单，见 [D9.10.6](#d9106-残项第-8-枚红清零2026-11-21第六单)）：判 (b) 过时断言，触发者是 13.104 已签的 catalog 接线而非 12.104 生产；生产 `.go` 仍零改动 |
| 生产改动 | **无**。四条红线（不在 `loop.go`/`goal.go` 改判据）未触发 |

### D9.10.1 逐红裁定（旧断言 → 新断言，语义不弱化）

| # | 红 | 裁定 | 依据（D8 已签语义 / spec） | 旧断言 → 新断言 |
|---|---|---|---|---|
| 1 | `history/TestEventIntegrityConsumptionRecovery/handover_before_confirmation`（30.0s 超时） | (b) | D8.1-H05 ②"`task.DeliveryFactOf` 产出唯一事实体"：两写面写的是同一事实，"哪一面写的"不再可由事实体区分 | `handoverProbe` 以 `Meta[MetaKeySettleTriggerSource] == ""` 认"backflow 那一条"（H05 后无任何事实满足 ⇒ 探针永不落）→ 改以**投递族身份**（`settle-` 键前缀，`task.DeliveryFactKey` 的族标识）认该事实，hold 点不变 |
| 2 | `history/TestEventIntegrityRestoreUse/next_request_uses_restored_material` | (b) | 同上＋D9.8.6 已记"投递事实携带结算行正文进请求"；`DeliveryFactOf` 正文由 `deliveryFactText` 唯一渲染 | 位置期望 4 项（input/call/result/answer）→ 5 项，把**投递事实行**（`tool`＋`task_settled`＋正文＝`tagent: task <id> settled`）钉在其原位；原四项逐字保留 |
| 3 | `host/TestRequestFidelity/request_after_compression` | (b) | 同 #2：投递事实有正文后不再被 fold 丢弃 | 计"全部 role=tool 消息＝1"→ 改计 **`action_command` 票种＝1**（结果仍恰好一条）＋新增 **`task_settled` 票种＝1 且正文为结算行**（更强，且把新事实钉住） |
| 4 | `host/TestReenteredTaskServesAfterRestart`（20.1s） | (b) | D8.1-H05：本地工具调用自身的结算也走同一渲染器 ⇒ 每条结算事实都带 `MetaKeyNoticeKind`，"任一 `TypeTaskSettled` + kind"不再是"收到通知"的判据 | `sendRigSettledTaskID` 取"最后一条 `TypeTaskSettled`"（H05 后取到 send 调用自己的任务 ⇒ 该任务永无通知）→ 取**链上另有一条 `notice:` 后续材料的结算任务**；helper 契约同步改写 |
| 5 | `host/TestReactivatedSettleFiresOnce`（20.3s） | (b) | 同 #4 | 同 #4（同一 helper） |
| 6 | `host/TestCommittedReentryIsNotRetroactivelyDenied`（三子场景） | (b) | 同 #4 | 同 #4（同一 helper；三子场景各自的主题断言未改） |
| 7 | `capabilities/TestGovernanceRecovery/a_failing_store…` | (b) | D8.1-H03b M 行：**"失败 `Declare` 返回 `""`、`Resolve` 返回 `false`"已签发**，且该行明记"本树无生产调用方（仅测试）→ 零级联"；被否决的候选 R 恰是"内存条目照旧、gate 继续工作" | "写失败仍留下声明且进 `List()`"→ 改为"写被拒答**无身份**、`List()` 随链（空），并新增**运行 vs 重启同源**断言（重绑同 store 的注册表与活注册表 `List()` 等长）＋ denial 账本仍记 1；nil-store 退化腿（`memGoals`）逐字保留 |

**反向触发④（H03b 返回 `""`/`false` 破坏既有工具契约而需新错误通道 → 回门）判定不触发**：`GoalRegistry.Declare` 在候选树无生产调用方（`grep '\.Declare(' ` 仅 `runtime/state/durable.go` 的同名异类方法与测试），签名 `skeleton_assert.go:102/104` 未动，无新错误通道；且 D8.1-H03b 已把 `""`/`false` 写进签发行。故按"库契约修复"处理，`TestGovernanceRecovery` 的旧断言属被否决候选 R 的钉子。

### D9.10.2 改动落点与指纹

| 文件 | 改动 | 对应红 |
|---|---|---|
| `tests/integration/history/event_integrity_test.go` | `handoverProbe.ObserveCommit` 族身份判据＋`deliveryFactKeyPrefix`；`restoreAssertNextRequest` 位置表增投递事实行＋`settleLineOf` | #1 #2 |
| `tests/integration/host/request_fidelity_test.go` | `fidelityCountToolTickets`／`ticketType`／`fidelitySettleMaterial`（净删 `fidelityCountToolMessages`）；压缩请求三条断言；两处"非空"守卫改用 `action_command` 票种（更强） | #3 |
| `tests/integration/host/send_reentry_rig_test.go` | `sendRigSettledTaskID` 增"链上有 `notice:` 材料"判据 | #4 #5 #6 |
| `tests/integration/capabilities/governance_recovery_test.go` | 失败存储腿改签后语义＋运行/重启同源断言；子场景名与文档 bullet 同步 | #7 |

**越界自查**：未碰 `tests/integration/acceptance/scenario_s11_test.go`、`tests/integration/host/projection_assembly_test.go`、`tests/integration/history/symptom_anchor_midband_test.go`（13.104 在飞）；生产 `.go`、CI、门脚本、主规格、权限、git 索引/refs 零改动。

### D9.10.3 命令与原始终局

> cwd＝`/Users/pengweiye/Documents/codes/tagent`，前缀 `env GOFLAGS=-buildvcs=false`；取被测进程原始 exit，无管道；scratch `/tmp/rr-12-104-r7/`（不作持久凭据）。

| 命令 | 终局 | 命中 |
|---|---|---|
| `go test ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host -count=1 -v`（复现，改前） | 1 | 七红：`TestEventIntegrityConsumptionRecovery`、`TestEventIntegrityRestoreUse`、`TestGovernanceRecovery`、`TestCommittedReentryIsNotRetroactivelyDenied`、`TestRequestFidelity`、`TestReenteredTaskServesAfterRestart`、`TestReactivatedSettleFiresOnce` |
| 同上（改后） | 1（仅剩第 8 枚，非七红） | 七红全绿；`--- FAIL: TestEventIntegrityRestart` 见 D9.10.5-② |
| `go test ./tests/integration/history -run '^(TestEventIntegrityConsumptionRecovery\|TestEventIntegrityRestoreUse)$' -count=10 -v` | 0 | `--- PASS` **20**／`--- FAIL` 0 |
| `go test ./tests/integration/host -run '^(TestCommittedReentryIsNotRetroactivelyDenied\|TestRequestFidelity\|TestReenteredTaskServesAfterRestart\|TestReactivatedSettleFiresOnce)$' -count=10 -v` | 0 | `--- PASS` **40**／`--- FAIL` 0 |
| `go test ./tests/integration/capabilities -run '^TestGovernanceRecovery$' -count=10 -v` | 0 | `--- PASS` **10**／`--- FAIL` 0 |
| `go test ./assembly ./runtime/... ./features/... -count=1`（相邻回归护栏） | 0 | 11 包 ok／0 FAIL（assembly 见 D9.10.4-注） |
| H03b 主锚 `./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -race -count=3 -v` | 0 | 见 D9.10.4 |
| H05 主锚 `./assembly -run '^(TestBackflowFailureRecoversWithoutManualSettle\|TestInProcessSettleFailureDrivesNoSecondTurn)$' -race -count=3 -v`＋`./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=3 -v` | 0 | 见 D9.10.4 |

**耗时对照（同锚、同机、同候选）**：`TestReenteredTaskServesAfterRestart` 20.11s→0.59s、`TestReactivatedSettleFiresOnce` 20.27s→0.67s——改前 20s 是等错任务的窗口超时，改后即刻命中，说明判据真的纠正而非放宽。

### D9.10.4 负变异（`go test -overlay`，隔离副本 `/tmp/rr-12-104-r7/mut/`，仓库零 diff）

| 变异 | 落点（overlay 替换） | 主题锚终局 | 主题失败行 |
|---|---|---|---|
| m1 族判据回退 | `event_integrity_test.go`：`handoverProbe` 回到 `Meta[MetaKeySettleTriggerSource] != ""` 判据 | `TestEventIntegrityConsumptionRecovery` **FAIL**（30.92s） | `handover_before_confirmation` 30.02s 超时（探针永不落，与改前同名红同果） |
| m2 位置表回退 | 同上：位置表删投递事实行 | `TestEventIntegrityRestoreUse` **FAIL** | `event_integrity_test.go:4902: … message 3 = tool "[evt_settle-…\|task_settled] tagent: task … settled", want assistant "the watcher sits on the east shelf"` |
| m3 任务判据回退 | `send_reentry_rig_test.go`：`sendRigSettledTaskID` 回到"最后一条 `TypeTaskSettled`" | 三枚 **FAIL**（27.21s／20.13s／20.31s） | `reentry_no_retroactive_denial_test.go:697/:745`、`send_reentry_restart_test.go:52` |
| m4 计数回退 | `request_fidelity_test.go`：计回"全部 role=tool" | `TestRequestFidelity` **FAIL** | `request_fidelity_test.go:420: the compressed request carries 2 role=tool call-result message(s) …` |
| m5 失败腿回退 | `governance_recovery_test.go`：回到"写失败仍留声明并进 List" | `TestGovernanceRecovery` **FAIL** | `governance_recovery_test.go:365: a failing store must not swallow the declaration` |

控制绿＝未变异副本上的同名锚（D9.10.3 三行 `-count=10` 终局 exit 0，同机同候选）；仓库内零 diff（变异只写 `/tmp/rr-12-104-r7/mut/`）。

### D9.10.5 偏离、观察与未验面

1. **单一渲染器的三项可观察后果（须随记录走，不得被下游读成"既有行为"）**：①每条结算的交付事实都带 `MetaKeyNoticeKind`/`MetaKeyTaskID`/`MetaKeySettleTriggerSource`（旧 loop 侧事实无 Meta）；②因此**每个被结算的调用**的投递事实都进模型请求（role=tool，票种 `task_settled`，正文＝结算行）——不再限于跨目标投递；③由此 `hostDeliveryRecordOf`（`assembly/host_tools.go:1006`）现在会把**本地调用自身**的结算事实也读成一条 `DeliveryRecord`（旧世界因缺 notice 元数据而被排除）。三条都是 D8.1-H05 单渲染器的直接后果，**本单未改生产**；③是否有既有计数断言/规格依赖"只算跨目标投递"，本单**未验**，交 12.105 全路径回归核对。
2. **第 8 枚红：`TestEventIntegrityRestart`（非七红之内）**。事实：干净 HEAD 树（`git archive HEAD`）同锚 **PASS**（9.011s）；候选树 **FAIL**（2.9–3.2s），改前改后同果。逐文件 A/B（`-overlay` 回退至 HEAD）**全仍红**：`runtime/agent/loop.go`＋`features/evolution/governance/goal.go`、`assembly/settle_turn.go`（含其测试）、`runtime/state/{durable,memory,mailbox,mailbox_checkpoint}.go`、`assembly/tools.go`、`assembly/checkpoint.go`、`assembly/facts.go`、以及仅回退 `facts.go` 的保 claim hunk；`memory/ledger/*` 回退因 06.104 在飞 API 漂移**编译失败、无法 A/B**。⇒ 归因**不在**本单两文件与四测试文件；剩余差异集为 06.104 在飞的 `memory/ledger/{journal,journal_kv,segment_store,kv_original_store,reference}.go`、12.103 的 `assembly/{run,read,compress_consumer,summary_gist,plugin_catalog}.go` 与 13.104 的 catalog 接线。**按写面禁令不修、上报编排者**；本单不据此判任何一叶。
   **勘误补记（第六单）**：该条的剩余差异集**已收敛到一项**——`assembly/plugin_catalog.go:163` 的 13.104 已签接线 `withSettleFactCommit(seam.run.commitFactChecked)`；单文件回退即转绿（D9.10.6-①）。结论同为 (b) 过时断言，已改断言，见 [D9.10.6](#d9106-残项第-8-枚红清零2026-11-21第六单)。
3. **未验面**：①`go test ./tests/integration/...` 全量仍未跑（只跑派单点名的 three packages）；②D9.10.5-1③的计数依赖未核；③七红的证据均为同进程/子进程集成面，未覆盖真实远端 provider。

### D9.10.6 残项第 8 枚红清零（2026-11-21，第六单）

> 派单：清掉 D9.10.5-② 遗留的第 8 枚红，并补做前单未落地的负变异。裁定口径与 D9.10 一致（真回归修生产／过时断言改断言，语义不弱化）。**生产 `.go` 零改动**；本单只改 `tests/integration/history/event_integrity_test.go` 与该单元两份记录；未 git 写、**12.104 勾选状态不变**。

#### D9.10.6.0 结论先行

| 项 | 结论 |
|---|---|
| 第 8 枚红裁定 | **(b) 过时断言。** 它钉的是 H05 修好之前的"重启后欠账永留"世界（欠账 `Owed=1`、边界仍 `accept`、drain 报未完、0 后续回合），而已签场景 `consumer-loop-convergence`／「回合完成而消费收据尚未写入」要求重启 **SHALL 继续未完工作**、仅完成剩余义务、或确认已消费 |
| 触发者 | **13.104 已签的 catalog 接线**（`assembly/plugin_catalog.go:163` `withSettleFactCommit(seam.run.commitFactChecked)`，该文件唯一生产 diff）。单文件 `-overlay` 回退该行 ⇒ 锚复现 HEAD 旧读数并 **PASS**；故 12.104 生产无回归，前单"未接线"缺口现已在生产入口闭合 |
| 为何现在才红 | 旧断言里"已投递"的唯一判据是**出站 sender 正文**。同会话通知（`Target==Source`）本就不产生出站发送，故 `Owed==0` 分支在 HEAD 从未被走到（HEAD 走 `Owed==1` 分支，after_confirmation 走 `restartAssertConfirmed`）——它是**未被执行过的潜伏判据**，泵补齐全后首次被走到即暴露口径错误 |
| 改法 | 5 处：投递判据改"真实模型请求携带通知材料"、`restartAssertOwed` 去掉"欠账仍可留"分支、`cross_boundary` 改按**做过的工作**区分 claim/confirmation、消费者锚改按**每次驱动**计数、`restartAssertConfirmed` 补"确认侧不得再投递"夹板 |
| 全命令终局 | three packages **0 FAIL**；该锚 `-count=10` **10/10 绿**；相邻回归 11 包 ok |

#### D9.10.6.1 归因三步（cwd＝`/Users/pengweiye/Documents/codes/tagent`，前缀 `env GOFLAGS=-buildvcs=false`）

| 步 | 命令 | 终局 | 读数 |
|---|---|---|---|
| 复现（改前） | `go test ./tests/integration/history -run '^TestEventIntegrityRestart$' -count=1 -v` | **1**（3.76s） | 红 6 枚：`positive/{after_commit,after_claim,before_confirmation}`、`negative/{unconfirmed_not_lost,cross_boundary_not_misjudged}`、`consumer` |
| 旧世界（`git archive HEAD` 解到 `/tmp/rr-12-104-r7f/head`） | 同锚 | **0**（9.020s） | 欠账三相当为 `notices=1 requests=1 sends=1 settles=1 drain={Drained:false …the mailbox holds this notification unconsumed} boundary={Owed:1 Accept:accepted}`；`after_confirmation` 为 `notices=0 settles=0 drained` |
| 候选读数 | 同锚 | 1 | 欠账三相为 `notices=1 settles=1 drain={Drained:true} boundary={Owed:0 Accept:pending AcceptErr:state: the notification carries no delivery obligation}`；`after_confirmation` 与旧世界同 |
| 单文件 A/B | `-overlay` 只把 `assembly/plugin_catalog.go` 换回 HEAD | **0**（9.081s） | 三相读数逐字回到旧世界（`Owed:1 accepted`、drain 未完） ⇒ 归因＝那一行接线 |

**归因为何不是"库/夹具抖动"**：同一命令、同机、同候选，只换一个文件的字节，结论翻转且翻转方向与"泵被接上/未被接上"一致；被回退的那一行是 13.104 的签发物（`ORCHESTRATION` ②「13.104 完成已勾（H05 经 catalog 装配取证）」）。

#### D9.10.6.2 语义裁定：旧断言钉的是 H05 缺陷，不是已签契约

| 依据 | 原文要点 | 对旧断言的判定 |
|---|---|---|
| `changes/12-consumer-state-lifecycle/specs/consumer-loop-convergence/spec.md`「回合完成而消费收据尚未写入」 | 停止发生在未驱动前／已落终态 attempt 之后／已落消费收据之后 ⇒ 重启 **SHALL 分别继续未完工作、仅完成剩余义务、或确认已消费**；不重新生成已完成回合，不丢收据前的未完项 | 旧断言把"重启后欠账仍在（`Owed=1`）且边界仍可接受"当**合格**结果 —— 那正是"不继续未完工作"，与 SHALL 相反 |
| D8.1-H05（投递义务单一事实源：唯一身份＋唯一渲染器＋泵内补齐＋拒写路径保 claim） | 修的就是"重启后 0 事实／0 收据／0 回合" | 旧断言只认**出站 sender** 为"真投递"；单渲染器之后投递的面是**模型请求里的投递事实行**（D9.10.5-1②③ 已记该后果） |

**"不弱化"的证据（三重）**：①欠账三相当由"可为 `Owed=1`"收紧为**必须** `Owed=0`＋drained＋边界由回执关闭；②新增"真实模型请求必须携带通知材料"（旧判据在外发面无从满足）；③保留并无条件执行 exactly-once 面（`settles==1`、通知回合≤1、确认侧不得再投递）。

#### D9.10.6.3 改动落点（全在 `tests/integration/history/event_integrity_test.go`）

| 落点 | 旧 → 新 | 对应红 |
|---|---|---|
| `restartNoticeReachedModel`（:3529，原 `restartNoticeDelivered`） | `Sends` 含通知正文 → `NoticeReqs>0`（真实请求的任一消息携带材料） | ③ ④ ⑥ |
| `restartChildReport`（:2824）＋`recoverRestartProcess`（:3085–3089） | `Prompt string` → `Prompts []string`＋`NoticeReqs int`，且在 `Close` **之后**读（交付回合可能落在排水期，见 D9.10.6.5-①） | ③ ④ ⑥ |
| `restartProvider.prompts`／`requestsCarrying`（:3173） | 新增：逐请求的最后一条 user 消息／携带材料的请求数 | 同上 |
| `restartAssertOwed`（:3496） | 两分支（欠账可留／出站正文已投递）→ 单向：欠账必须做完、恰一条投递事实、材料进模型请求 | ①②③ |
| `restartAssertCrossBoundary`（:3616） | "claim 侧必须 `Owed=1` 且 `accept`" → "claim 侧必须**做过**投递（`notices=1`＋`settles=1`＋材料进请求），确认侧必须什么都没做" | ⑤ |
| `restartAssertFirstRequest`（:3649） | 总请求数 `==1` → 按**每次驱动**计数（重启自身输入恰一次、通知回合≤1、每次发送只带 provider 作答） | ⑥ |
| `restartAssertConfirmed` | 增：确认侧 `NoticeReqs==0`（确认过的消费不得再进模型请求） | ③ 反向面 |

#### D9.10.6.4 命令与原始终局

| 命令 | 终局 | 命中 |
|---|---|---|
| `go test ./tests/integration/history ./tests/integration/capabilities ./tests/integration/host -count=1` | **0** | history 9.783s／capabilities 3.443s／host 202.255s，全 `ok` |
| `go test ./tests/integration/history -run '^TestEventIntegrityRestart$' -count=10 -v` | **0** | `PASS` 行 **120**＝父 10＋组节点 30（positive／negative／consumer）＋叶 80；`FAIL` **0** |
| `go test ./tests/integration/history -run '^TestEventIntegrityRestart$' -count=1 -v`（改后） | **0**（3.01s，原 3.76s 红） | 欠账三相 `requests=2 sends=2 settles=1 drained Owed=0`；`before_commit`／`after_confirmation` 仍 2 相未变 |
| `go test ./assembly ./runtime/... ./features/... -count=1` | **0** | 11 包 `ok`（assembly 351.462s）／0 FAIL |

#### D9.10.6.5 偏离、观察与未验面

1. **同一相位的出站请求总数是竞态（观察，须随记录走）**：欠账三相在 `Close` 前读到的总请求数为 **1 或 2**（3 次采样：`after_commit` 1/2/1、`after_claim` 2/1/1、`before_confirmation` 1/1/2）——泵的交付回合与调用回合抢先后。故消费者锚**不得**钉总数；改为"每次驱动各计一次"（本单改法）。此前的 `Requests==1` 判据在泵接通后必然间歇红，属旧世界遗留的时序假设，不是新缺陷。
2. **`cross_boundary` 判词换面（偏离，已在此申报）**：旧末条"两侧 owed 必须不同"在新世界恒等（两侧都由回执关闭为 0），若保留即永红；改用"做过的工作"区分（claim 侧必须交付、确认侧必须不交付）。该面**更强**：旧世界从未要求 claim 侧真的把欠账做完。
3. **`restartAssertConfirmed` 的夹板是新增不变量**：它复用 `NoticeReqs` 度量，其正向咬合由变异 n2 证明；反向（确认侧多投递）无对应生产变异可造（须造一个"不落投递事实却把材料再喂给模型"的回归），**未验**如实记此。
4. **`payload` 探针不入库**：为判定"材料是否真的进请求"，本单把测试文件复制到 `/tmp/rr-12-104-r7f/probe/` 打补丁（在每个请求体落盘）后 `-overlay` 运行；实测欠账三相当请求里都有 `[evt_settle-<hex>|task_settled] tagent: task <id> settled … result:\nrestarted result` 的 tool 行与一条 `[evt_notice…]` user 唤醒行，确认相两者皆无。仓库内该探针零残留（`find -newer` 只列被改的测试文件）。
5. **未验面**：①`go test ./tests/integration/...` 全量仍未跑；②未覆盖真实远端 provider；③本条不声称 06.104 的 `memory/ledger/*` 与本红无关——只是本红已被单文件 A/B 定死在那行接线上。

#### D9.10.6.6 负变异（`go test -overlay`，隔离副本 `/tmp/rr-12-104-r7f/mut/`，仓库零 diff）

| 变异 | 落点（overlay 替换） | 主题锚终局 | 主题失败行 |
|---|---|---|---|
| n1 生产接线回退 | `assembly/plugin_catalog.go` → HEAD（去 `withSettleFactCommit`） | `TestEventIntegrityRestart` **FAIL** | `... restart left 1 notification(s) owed, want the duty it found owed finished before the run closed`（positive 三相） |
| n2 投递判据回退 | 同测试文件：`restartNoticeReachedModel` 回到"Sends 含通知正文" | 同锚 **FAIL** | `the after_commit restart closed the duty while no model request carried "restarted result"` |
| n3 跨界判词回退 | 同文件：claim 侧回到 `Boundary.Owed != 1 || Accept != "accepted"` | 同锚 **FAIL** | `cross_boundary_not_misjudged`: `the claimed side of the boundary owes 0 and answered "pending" … want it owed and still acceptable` |
| n4 消费者计数回退 | 同文件：回到 `Requests != 1` | 同锚 **FAIL** | `the after_commit restart made 2 requests, want its first actual request carrying the restart input` |
| n5 确认侧夹板失去度量 | 同文件：`restartNoticeReachedModel` 恒 `true` | 同锚 **FAIL** | `positive/after_confirmation`: `the restart carried "restarted result" into 0 model request(s), want a confirmed consumption delivered no second time`（另 `cross_boundary` 确认侧同红） |
| p1 族判据回退（补前单） | `handoverProbe` 回到 `Meta[MetaKeySettleTriggerSource] != ""` | `TestEventIntegrityConsumptionRecovery` **FAIL**（30.97s） | `handover_before_confirmation` 30.02s 超时（探针永不落） |
| p2 位置表回退（补前单） | 位置表删投递事实行 | `TestEventIntegrityRestoreUse` **FAIL** | `event_integrity_test.go:4955: … message 3 = tool "[evt_settle-…\|task_settled] tagent: task … settled", want assistant "the watcher sits on the east shelf"` |
| p3 任务判据回退（补前单） | `sendRigSettledTaskID` 回到"最后一条 `TypeTaskSettled`" | 三枚 **FAIL**（27.17s／20.13s／20.25s） | `reentry_no_retroactive_denial_test.go:697/:745/:812` |
| p4 计数回退（补前单） | `fidelityCountToolTickets` 计回全部 role=tool | `TestRequestFidelity` **FAIL** | `request_fidelity_test.go:408: the compressed request carries 2 action_command tool message(s), want the one result …` |
| p5 失败腿回退（补前单） | 失败存储腿回到"写失败仍留声明并进 `List`" | `TestGovernanceRecovery` **FAIL** | `governance_recovery_test.go:365: a failing store must not swallow the declaration` |

控制绿＝同机同候选的未变异副本：D9.10.6.4 三行（three packages／`-count=10`／相邻 11 包）全 exit 0；变异只写 `/tmp/rr-12-104-r7f/mut/`，仓库内零 diff（`find . -newer` 只列 `tests/integration/history/event_integrity_test.go`）。p1–p5 是 D9.10.4 已记五案的复跑补证（前单记录在执行期未全部落盘），失败行与 D9.10.4 表一致。

## D12 · 12.105 回归记录

> 执行者：12.105 验证单（网络中断重派，接续前单已建成的两枚新锚）。被测对象＝**工作树**（含未提交的 .103/.104/13.104 实现产物；`git archive HEAD` 与工作树 `diff -r --brief` 在 `assembly/`、`runtime/`、`tests/` 三处列出差异，含新增 `runtime/task/delivery_fact.go`），HEAD＝`7b0cde162627ad37f1d364bbf3508b3bd0518431`，go1.27.1 darwin/arm64。全部命令前缀 `env GOFLAGS=-buildvcs=false go -C /Users/pengweiye/Documents/codes/tagent test`，逐条取原始 exit 与 `^=== RUN`／`^ *--- PASS`／`^ *--- FAIL` 计数（父行不缩进、子场景缩进四格，故两式并计）；日志留 `/tmp/rr-12-105/`（执行 scratch）。本单只跑命令与写记录，未改产品/测试，写面仅本文件与 `tasks.md` 的记录行。

### D12.0 结论先行

矩阵 9 组：**第 1–8 组全部 exit 0 且逐名非零**（组 1 两枚新锚六子场景各 10/10；组 8 八条抽验全绿，其中两条派发命令的包归属偏差已按真实包补跑取证）。第 9 组全包终态**未全绿**：`./assembly ./runtime/... ./features/...` 隔离复跑 exit 0（11 包 ok）；`./tests/integration/...` 隔离复跑 exit 1，唯一红在 `tests/integration/execution` 三枚（其余 13 包 ok）。三红经 HEAD 树基线 A/B 判定**由本候选引入**，机制落在 H05 交付事实的身份与渲染面，归属 12.104（生产接线 13.104），按派单**不修**，交拥有叶裁定。（后续：三红已于 D13 裁定清零，全包 24 包 0 FAIL。）

**D8.7.1-⑦／D4 行 11 三观测轴补账（12.106 审①，2026-11-21）**：H06 附注指定的三观测轴（拒 cut 持续时长、期间新增事实数、提交等 fold）**未在 D12 矩阵落账**，如实记**未验**：归属 06.105（B 侧成本/观测面在飞）与 F2 矩阵终取；不因未列而视作已测。

### D12.0.1 签发补账（12.106 审③，编排者）

`runtime/agent/consumer_test.go` 的改动（孤儿结算断言改从链上结果事实读回，经生产 `resultFactOf`）在 D9.8.6 由编排者裁定方案 i 授权，但未在 D4 写面留签发行——现补签：该文件列入 12.104 测试写面（同根因＝H05 删除"观测随投递事实同行"的级联，见 D9.8.6 ①）；此前无签发行属签账缺口，实际改动未越权。

### D12.0.2 阻断②处置：`sessionCuts` 死写删除（12.106 复审单，2026-11-22）

> 产生方式：`12.106` 初轮阻断②的修复单（`06.105` 测量已收，生产改动解禁）。写面＝`assembly/run.go`、`assembly/checkpoint.go`（仅②的死写行）与本文件；未改测试、门、主规格、golden；未 git 写。执行期位置较签发单漂移：签发写 `run.go:344–350`／`checkpoint.go:449/1016`，现树为 `run.go:350–356`／`checkpoint.go:481/1115`（12.103 期间文件增长），符号同一。

**处置前引用清单**（`grep -rn "sessionCuts" --include="*.go" .`，全仓恰 6 处，无第 7 处）：

| 位置 | 形态 |
|---|---|
| `assembly/run.go:350` | 声明注释提及 |
| `assembly/run.go:355` | 字段声明 `map[event.SessionID]event.CheckpointOffset` |
| `assembly/run.go:694`／`:695` | `nil` 判与初始化 |
| `assembly/checkpoint.go:481` | 写：会话 checkpoint 落盘后的账户更新 |
| `assembly/checkpoint.go:1115` | 写：恢复会话 checkpoint 后的账户更新 |

**证死**：下标右值读取（`sessionCuts[...]`）、`len(...)`、`range`、以该字段为实参的调用均零命中；`*_test.go` 同样零命中。生产与测试都无读者，删除纯属删除死状态，零行为差。

**删除（4 hunk：`run.go` 2、`checkpoint.go` 2；删 10 行增 3 行）**：`run.go` 去掉字段声明与 `nil` 初始化块，并把声明注释改写为只陈述留存的两个字段（删去「cut 对应」与「读框架 cut 兜底」两句——后者在包内无消费者承接，属未证断言，随字段一并退出）；`checkpoint.go` 去掉两处赋值。伴生类型零改动：`event.CheckpointOffset` 仍由 `checkpointOffset`／`checkpointCut` 使用。

**级联观察（本单未动，交 12.106 复审裁定）**：同族的 `sessionFiled`、`sessionSlices` 在整树**亦无读者**（写入 4 处、初始化 2 处、读取 0 处），与②同形。②的签发面只点名 `sessionCuts`，故保留不删；若设计门认定这三张表整体无消费者，应另单删除并同期补签，而不是在本单顺手扩大写面。

**证据（2026-11-22，串行执行，无并发负载；scratch `/tmp/rr-12-106-blk2/`）**：

| 项 | 命令（工作树根，参数原样） | 终局 |
|---|---|---|
| 构建 | `go build -buildvcs=false ./...` | **exit 0** |
| 包回归 | `env GOFLAGS=-buildvcs=false go test ./memory/ledger -count=1` | **exit 0**，`ok memory/ledger 83.378s` |
| 包回归 | `env GOFLAGS=-buildvcs=false go test ./assembly -count=1` | **exit 0**，`ok assembly 369.772s` |
| 锚存在性 | `go test ./assembly -list 'TestRecoveryEntriesPreserveStateAndObligations\|TestPredecessorQueryBoundsTheWalkToTheChain'` | 两名字均列出（非零匹配，排除空转） |
| 代表锚 | `go test ./assembly -run '^(TestRecoveryEntriesPreserveStateAndObligations\|TestPredecessorQueryBoundsTheWalkToTheChain)$' -count=3 -v` | **exit 0**：RUN 24／PASS 24／FAIL 0／SKIP 0；`TestRecoveryEntries…` 父 3＋六子场景各 3（`no_usable_anchor`、`checkpoint_without_a_compress_anchor`、`valid_anchor_without_a_checkpoint`、`valid_anchor_beside_a_checkpoint`、`allowed_degradation`、`unrestorable_survivor_is_refused`），`TestPredecessor…` 3 |
| 死生产者门 | `python3 scripts/gates/check_dead_producers.py` | **exit 0**（②不涉；④一侧见 06 域 design.md D12.8） |

命令形式偏离：签发写 `go build ./... -buildvcs=false`，Go 拒绝（`malformed import path "-buildvcs=false": leading dash`）——构建 flag 必须在包模式之前，故用 `go build -buildvcs=false ./...`，语义相同。


**复审关闭轮（2026-11-22，编排者亲收）**：裁定 A 的 `sessionFiled`/`sessionSlices` 死写对已删（run.go 字段声明＋初始化临界区整删、checkpoint.go 两处构建＋写整删；grep 零命中、gofmt 空、build 过、`TestRecoveryEntriesPreserveStateAndObligations|TestTaskSnapshotReplaysInterleavedCommit|TestConsumerBehindTheChainSurvivesARestart` -race -count=3 绿、check_dead_producers exit 0）。裁定 B（`IndexEntry` 死类型＋golden binding＋docs/api 陈旧条目）确认归 13.105 一窗承接（13 域候选写面已列 golden 与 docs/api）。A 单元闭环条件就绪。
### D12.1 矩阵逐条（组 1–8，全部 `-v`，日志同目录）

| 组 | 命令（`… test` 后） | exit | RUN | PASS | FAIL | SKIP | 终局 |
|---|---|---|---|---|---|---|---|
| 1 | `./assembly -run '^(TestRecoveryEntriesPreserveStateAndObligations\|TestNoTierCommitPreservesApplicationOrder)$' -race -count=10` | **0** | 80 | 80 | 0 | 0 | `ok assembly 9.470s` |
| 2 | `./runtime/state -run '^(TestUnregisterRefusesASliceWithUnfinishedWork\|TestUnregisterAllowsASettledSlice\|TestSliceHandoverKeepsTheUnfinishedObligations)$' -race -count=10` | **0** | 30 | 30 | 0 | 0 | `ok runtime/state 1.542s` |
| 3 | `./assembly -run '^(TestUnmountKeepsTheOwedItemsOfItsSlice\|TestPendingSliceIsRefusedByName\|TestGenerationSwitchKeepsTheSameStateAuthority\|TestDisabledBuiltinHandsOverItsObligations)$' -race -count=10` | **0** | 40 | 40 | 0 | 0 | `ok assembly 6.491s` |
| 4 | `./runtime/task -run '^(TestTaskSliceReadsBackTheReceiptItDoesNotState\|TestTaskSliceTrustsAStatedEmptyReceiptSet\|TestTaskSliceRefusesAReceiptOfAnotherTask)$' -race -count=10` | **0** | 30 | 30 | 0 | 0 | `ok runtime/task 1.333s` |
| 5 | `./assembly -run '^(TestCrossSessionObligationsStayInTheirTargetSlice\|TestYoungLogBootRestoresTheOwedMailboxThroughTheSlice)$' -race -count=10` | **0** | 20 | 20 | 0 | 0 | `ok assembly 5.413s` |
| 6 | `./features/evolution/governance -run '^(TestDeniedRulingMustSurviveItsValidityAcrossRestart\|TestExpiredApprovalNeverRunsAfterRestart\|TestApprovalAliasCollapsesAcrossTheCut\|TestGovernanceCountAndQueryStateTheirBoundary)$' -race -count=10` | **0** | 40 | 40 | 0 | 0 | `ok governance 1.455s` |
| 7 | `./assembly -run '^(TestUnreadableReceiptDoesNotDriveAgain\|TestBackflowFailureRecoversWithoutManualSettle\|TestRedeliveredNoticeAfterRestartSettlesOnce)$' -race -count=10` | **0** | 60 | 60 | 0 | 0 | `ok assembly 232.052s`；末枚三子场景各 10 |
| 8 | 见 D12.3（八条抽验与两条归位补跑） | **0**×10 | — | — | 0 | 0 | 逐条见下表 |

组 1 分母核对：`TestRecoveryEntriesPreserveStateAndObligations` 父 10＋六子场景各 10＝70，`TestNoTierCommitPreservesApplicationOrder` 10，合计 **80**。组 7：`TestUnreadableReceiptDoesNotDriveAgain` 10＋`TestBackflowFailureRecoversWithoutManualSettle` 10＋`TestRedeliveredNoticeAfterRestartSettlesOnce` 父 10＋三子（`cut_before_the_receipt`／`cut_before_the_turn`／`redelivery_after_the_receipt`）各 10＝**60**。

### D12.2 两枚新锚：三分支子场景覆盖表（组 1 的 `-v` 输出留 `/tmp/rr-12-105/g1.log`）

| D3/任务要求的类 | 实际子场景名（`TestRecoveryEntriesPreserveStateAndObligations/…`） | RUN | PASS | 前提／观测（执行期实据） |
|---|---|---|---|---|
| 无可用锚（年轻链全走读） | `no_usable_anchor` | 10 | 10 | 压缩产品计数 0；`CheckpointFiled()==""`；`restore folded chains=2 links=2 covered=0`；`boot seated the task account tail=none`；无降级告警；欠账＝链陈述的请求数 |
| checkpoint 子分支（有 cut 无锚） | `checkpoint_without_a_compress_anchor` | 10 | 10 | cut 已落且幸存者非空；产品计数 0；boot 读到同一 cut；呈现键＝幸存者＋尾段；cut 覆盖的判定不被重走；欠账＝cut 陈述的请求数 |
| 有效锚（无框架 checkpoint） | `valid_anchor_without_a_checkpoint` | 10 | 10 | 产品计数 1；`CheckpointFiled()==""`；呈现＝载荷陈述的幸存者＋尾段；欠账＝请求数−1；`struckNotes()==1`；锚本身不作链事实入席 |
| 有效锚＋框架 checkpoint | `valid_anchor_beside_a_checkpoint` | 10 | 10 | cut 陈述全部 `slot.mapped`；产品计数 1；boot 读到同一 cut；cut 覆盖材料不入席；呈现＝载荷幸存者＋尾段；欠账＝请求数−1 |
| 允许降级（锚载荷无可读幸存者） | `allowed_degradation` | 10 | 10 | 产品计数 1；boot 不读 checkpoint；warn 行含锚键＋`states no readable survivor payload`＋`falling back to the full replay`；全链可回放且欠账＝全部请求数 |
| 拒绝负例（幸存者不可还原） | `unrestorable_survivor_is_refused` | 10 | 10 | 产品计数 1；`assembleRecovery` 返回 error 且 `errors.Is(err, ErrCheckpointSurvivorUnstated)`、错误文本含幻影幸存者键——损坏权威**不作降级** |
| 无 tier 提交顺序（第二枚新锚，无子场景） | `TestNoTierCommitPreservesApplicationOrder` | 10 | 10 | `run.tier==nil`、`durableAccount()==nil`；首提交 fold 停靠期间 `landing.TryLock()` **失败**；`appliedOrder()==[first,second]`；2 条会话事实；`ReplayEntries` 2 条 |

入口真实性证据（非 `seedFullReplay` 对照）：`assembleRecovery`（`assembly/checkpoint_test.go:5261–5270`）走生产 `assemble(...)` 入口并带 `WithGovernance(snapshotTestGovernance{})` 与真实存储根（`recoveryStorage(root)`）；`bootRecovery` 仅是该入口的取错包装（`:5274–5282`）。第二枚锚的 run 由 `noTierChainRun`（`assembly/facts_test.go:2228`）建立，`run.stopCheckpointFiler()` 之后无 checkpoint 落点。

### D12.3 回归闭包抽验表（组 8；前七条按派发命令，后两条为包归属纠正）

| # | 命令（`… test` 后） | exit | RUN | PASS | FAIL | 终局 |
|---|---|---|---|---|---|---|
| 8.1 | `./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit\|TestConcurrentCommitPreservesApplicationOrder\|TestConcurrentSameFactKeepsOneApplication\|TestTaskRestoreBoundedByCheckpoint\|TestOrphanSettlementDrivesNewSegment\|TestInProcessSettleFailureDrivesNoSecondTurn)$' -race -count=3` | **0** | 21 | 21 | 0 | 5 名生效各 3（`TestConcurrentSameFactKeepsOneApplication` 另二子各 3）；`TestOrphanSettlementDrivesNewSegment` **零匹配**（见 8.10） |
| 8.2 | `./runtime/task/approval -run '^TestApprovalWriteFailureDoesNotGrantPermission$' -count=3` | **0** | 15 | 15 | 0 | 父 3＋四子各 3 |
| 8.3 | `./features/evolution/governance -run '^TestGoalWriteFailureDoesNotChangeEffectiveState$' -count=3` | **0** | 15 | 15 | 0 | 父 3＋四子各 3 |
| 8.4 | `./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=3` | **0** | 12 | 12 | 0 | 父 3＋三子各 3（归属纠正，见 8.9） |
| 8.5 | `./memory/ledger -run '^(TestLegalClusteredKeysSurviveCommitAndReopen\|TestJournalOpenRefusesCorruptionBehindAStatedWatermark\|TestOpenIntegrityScanIsTheWholeLogAndGrowsWithIt)$' -count=3` | **0** | 9 | 9 | 0 | 各 3；`ok memory/ledger 34.042s` |
| 8.6 | `./assembly -run '^(TestPredecessorQueryBoundsTheWalkToTheChain\|TestHistoricalReadsAvoidUnrelatedHistory)$' -count=3` | **0** | 6 | 6 | 0 | 各 3 |
| 8.7 | `./tests/integration/history -run '^TestEarlyFactRemainsDiscoverableAfterRepeatedCompression$' -count=3` | **0** | 3 | 3 | 0 | 3 |
| 8.8 | `./assembly -run '^TestDeclaredPluginChoiceSurvivesRestart$' -count=3` | **0** | 3 | 3 | 0 | 3 |
| 8.9 | `./runtime/agent -run '^TestOrphanSettlementDrivesNewSegment$' -count=3 -race`（归位补跑） | **0** | 3 | 3 | 0 | 3 |
| 8.10 | `./assembly -list 'TestOrphanSettlementDrivesNewSegment\|TestDeliveryFactOfIsOneFactForBothWriteFaces'` 与 `./assembly -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -count=3`（零匹配取证） | **0**／**0** | 0／0 | 0／0 | 0 | `-list` 无输出；后者原文 `testing: warning: no tests to run` `[no tests to run]`——**零匹配不计通过** |

### D12.4 全包终态（组 9）：并发测量作废，以隔离复跑为准

| 命令 | exit | 依据 |
|---|---|---|
| `go test ./assembly ./runtime/... ./features/... -count=1`（与集成全量**并发**） | 1 | **作废**：`assembly` 唯一红 `TestSymptomAnchorPerGenerationCostFlatOverALongRun`（36.67s）`symptom_anchor_longrun_test.go:909`：`the commits stage … generation 247 beside generations 1-5: the middle generation of the last 5 costs 66.906167ms beside the 34.522625ms of the first 5, over the bound of 44.879412ms`——性能锚与被测负载无关的外界并发争用，测量无效 |
| `go test ./assembly ./runtime/... ./features/... -count=1`（**隔离**复跑） | **0** | 11 包 `ok`（`assembly 356.714s`、`runtime/{agent,state,task,task/approval}`、`features/evolution{,,/governance,/meditation}`、`features/{rl,rl/sample,telemetry}`）／0 FAIL |
| `go test ./tests/integration/... -count=1`（与上条**并发**） | 1 | **作废**：3 包红，其中 `acceptance`／`capabilities` 为时序超时红，隔离单跑均转绿（见下） |
| `go test ./tests/integration/... -count=1`（**隔离**复跑） | **1** | 13 包 `ok`；唯一红 `tests/integration/execution`（41.362s）三枚，见 D12.5 |
| `go test ./tests/integration/acceptance -count=1`（单独） | **0** | `ok 3.467s`；并发时红 `scenario_s03_test.go:97: the settled bodies of map[call-a:background-marker-a call-b:background-marker-b] did not reach a model view within the wait` |
| `go test ./tests/integration/capabilities -count=1`（单独） | **0** | `ok 1.656s`；并发时红 `native_tools_test.go:102: native tool pairs did not all arrive: missing=[call-save]` |
| `go test ./tests/integration/execution -count=1`（单独） | **1** | 三红确定性复现；同包 `TestNativeConversationRoundTrip` 并发时曾红（`never reached a model request in 10s`），单独运行转绿——负载诱发 |

结论：`./tests/integration/...` 的红**不是**并发噪声，而是 `execution` 包内三枚确定性断言；`acceptance`／`capabilities`／`TestNativeConversationRoundTrip` 三处为并发污染，已以隔离复跑排除。

### D12.5 缺口归属（三枚确定性红）

基线 A/B（只读取证，仓库零写）：`git archive HEAD | tar -x -C /tmp/rr-12-105/headtree`，在 HEAD 树跑同三测试 → **exit 0（`ok 1.188s`）**；同三测试在工作树（候选实现）→ **exit 1**。且三条测试文件与 HEAD **逐字节相同**（`diff -r --brief` 的 `tests/` 差异只列 `capabilities/governance_recovery_test.go`、`history/event_integrity_test.go`、`history/symptom_anchor_midband_test.go`、`host/request_fidelity_test.go`、`host/send_reentry_rig_test.go`）。故红由本候选的未提交实现引入，探针未随产品语义更新。

| 编号 | 锚（文件:行） | 原文关键句 | 机制证据 | 归属 |
|---|---|---|---|---|
| R1 | `TestInstancesSymptomTwoEntriesOneAccount`（`delivery_contract_test.go:225`） | `deliveries the account lists for "session-delivery-peer-one" = 3, want the body a session sent beside the settle the framework delivered` | 列出的三条为 `settle-…:1:settled:0` ×2（两个不同结算键）＋`tagent-session:user:meditation#c1`（正文）——每次结算各持一个投递事实 | 12.104 H05 交付事实唯一身份（生产接线 13.104） |
| R2 | `TestReceiptAndBackgroundStaySeparate`（`receipt_background_separation_test.go:274`） | `second invocation message ledger = map[fast settled body:1 first input:1 second input:1 slow settled body:1 third input:1 tool call turn:1 unexpected:2]` | `unexpected:2` 即两条独立 `role=tool`、正文以 `[evt_settle-…|task_settled]` 起头的结算通知消息（并发红时同包 `native_tools_test.go` 与 `native_conversation_roundtrip_test.go` 的请求体显示同一形状）——通知按每次结算各自成条渲染，旧断言只容许它折在结果体内 | 12.104 H05 渲染面（`runtime/agent/loop.go`＋`assembly/tools.go` 两写入面收敛） |
| R3 | `TestSendReentersAgentTask`（`send_reentry_test.go:104`） | `send{target:{task}} result = "[send_denied] agent: the task is no delegation this ledger holds: task-0827d5e4d68f97df210913af1a00ee3a", want the bare "ok" of a re-entered task` | 拒绝文本出自 `runtime/agent/delegate.go:256 ErrDelegationUnknown`；已结算任务在委托账里不再被认作可重入委托 | 待拥有叶裁定：12.104 交付/结算身份改造（次选 05 task-custody 旧项）——本单不修 |

处置建议（供 12.106/编排者裁定，不由本单执行）：先按 H05 已签语义判"新世界正确、断言过时"还是"产品缺陷"，同类先例见 [D9.10.6](#d9106-残项第-8-枚红清零2026-11-21第六单)（旧断言钉的是修好之前的世界）；若判过时，更新点限于 `tests/integration/execution/` 三文件，且须保留反面对照（`cross_boundary` 式的更强夹板），不得宽断言追认。

### D12.6 偏离、环境与未验面

1. **派发命令两处包归属偏差（已按真实包补跑，记录在案）**：`TestOrphanSettlementDrivesNewSegment` 实际在 `./runtime/agent`（`consumer_test.go:1974`），`TestDeliveryFactOfIsOneFactForBothWriteFaces` 实际在 `./runtime/task`（`delivery_fact_test.go:21`）；派发单把它们列入 `./assembly` 选择器。以 `-list` 无输出与 `no tests to run` 取证零匹配，并按真实包补跑（8.9、8.4）取得证据。零匹配**不计通过**。
2. **并发测量污染（已作废并复测）**：首轮组 9 两条命令并发执行，导致性能锚（`judgeGenerationStages`）与三处时序断言红；隔离单跑全部转绿。后续同类全包终态须顺序测量。
3. **`git status` 在本仓不可用（环境问题）**：`fatal: mmap failed: Operation timed out`；`.git/index` 为 128 字节桩。改以 `git archive HEAD` 提取只读副本 + `diff -r --brief` 取证（`/tmp/rr-12-105/headtree`），不修改 `.git/`。
4. **越界自查（应零产品/测试改动）**：`find assembly runtime features memory tests -name '*.go' -newer /tmp/rr-12-105/g1.log` **无输出**；`12-consumer-state-lifecycle/` 目录在本记录落盘前**无更新文件**。工作树相对 HEAD 的差异全部是本轮之前的候选实现，见本节开头身份说明。
5. **未验面**：①本单未跑负变异（派发矩阵不含，D8.6 负变异属各拥有叶已签发证据，本单只抽验正/负/消费者锚）；②未覆盖真实远端 provider；③12.106 的三锚原始记录核对与结构核销不在本单；④`TestNoTierCommitPreservesApplicationOrder` 为单一场景（无子场景），"无 tier"与"memory tier"的区分只由 `run.tier==nil`／`durableAccount()==nil` 两条前提固定。

## D13 · execution 三红裁定与修复（2026-11-21，第七单）

> 派单：处置 [D12.5](#d125-缺口归属三枚确定性红) 的三枚确定性红（拥有侧 A）。裁定口径同 [D9.10](#d910--残项七红裁定与修复2026-11-21第五单)：真回归修生产／过时断言改断言，语义不弱化。**生产 `.go` 零改动**（三红全判 (b)），`runtime/agent/delegate.go` 的委托安全判据、`assembly/host_tools.go` 的拒绝入口、`assembly/plugin_catalog.go` 的 13.104 接线**一字未改**。写面＝`tests/integration/execution` 三个测试文件与本单元两份记录；未 git 写、未勾选 12.105。全部命令 cwd `/Users/pengweiye/Documents/codes/tagent`，前缀 `env GOFLAGS=-buildvcs=false`，scratch `/tmp/rr-12-105-r3/`（不作持久凭据）。

### D13.0 结论先行

| 项 | 结论 |
|---|---|
| 三红裁定 | **3/3 判 (b) 过时断言**。三枚钉的都是 H05 修好之前的"本地调用自身的结算不可见"世界：投递账本不含它、模型请求不含它、`MetaKeyNoticeKind` 只属于真通知。无一枚是生产行为回归；R3 的拒绝文本是既有具名契约（见 D13.1-R3 依据列） |
| 机制（同一根因三面） | `task.DeliveryFactOf` 成为**唯一**渲染器后，每次结算（含本地工具调用的入职任务）都写一条带 `MetaKeyNoticeKind`／`MetaKeyTaskID` 的 `task_settled` 投递事实，身份统一为 `settle-<hex>`：①`hostDeliveryRecordOf` 因此多读一条 `DeliveryRecord`；②该事实以 `task_settled` 票种进模型请求，成为每次结算一条独立结算行；③`MetaKeyNoticeKind` 不再是"通知"的判别式 |
| 全命令终局 | 三红 **0 FAIL**（`-count=10 -v`：父 30＋子 20＝**50 PASS／0 FAIL**）；`./tests/integration/execution -count=1` **exit 0**（39.333s） |
| 护栏 | `go test ./assembly ./runtime/... ./features/... ./tests/integration/... -count=1` **exit 0**，24 包 ok（assembly 375.432s／execution 49.870s／host 216.457s） |
| 未修的相邻红 | `TestNativeConversationRoundTrip` 为**既有 flake**（候选 5/10、HEAD 树 2/10，单跑 3/3 绿），不属三红、不在本单写面 → 见 D13.6-① |

### D13.1 逐红裁定（旧断言 → 新断言，语义不弱化）

| # | 红（改前 file:line） | 裁定 | 依据 | 旧断言 → 新断言 |
|---|---|---|---|---|
| R1 | `delivery_contract_test.go:225`：`deliveries the account lists for "session-delivery-peer-one" = 3, want the body a session sent beside the settle the framework delivered` | **(b)** | D8.1-H05 ②「`task.DeliveryFactOf` 产出唯一事实体」；D9.10.5-1③ 已把"本地调用自身的结算事实也被读成一条 `DeliveryRecord`"列为单渲染器的直接后果并**交 12.105 核**；账本定义见 `docs/wiki/reliability/durable-delivery.md:87`「两族投递…账目把本 run 提交过的投递事实按源事件、目标与投递身份回读…它是回读面，不是第三条门」 | `len(byTarget)==2`（正文＋一条结算）→ **逐族**：`sent==1`（正文）＋`settles==2`（被委托的回合＋承载它的那次调用的入职），并由 `deliverySplitSettles` 按事实正文分开两者（回合那条带 `result:\n<review answer>`，入职那条只有结算行）；"属哪族"改读身份的 `settle-` 家族前缀而不是正要断言的 `FactType`，故类型断言不再自证 |
| R2 | `receipt_background_separation_test.go:274`：`… map[… slow settled body:1 … tool call turn:1 unexpected:2]` | **(b)** | D9.10.5-1②「每个被结算的调用的投递事实都进模型请求（role=tool，票种 task_settled，正文＝结算行）——不再限于跨目标投递」 | 账目六族 → **八族**：新增 `call-a settle notice`／`call-b settle notice` 各 1（`role=tool`＋调用身份＋票种 `task_settled`＋正文为结算行且**无** result）；"每调用 ≤1 槽"判据由"任一 tool-role 消息"改为"**除结算行外**的消息"（`sepToolSlots`），扫描面由已记录请求**扩到 flush 视图**（旧代码未覆盖 flush 视图） |
| R3 | `send_reentry_test.go:104`：`send{target:{task}} result = "[send_denied] agent: the task is no delegation this ledger holds: task-ca9a…"` | **(b)** | D8.1-H05 ①＋`assembly/host_tools.go:218–243` 既有契约原文「A delegation the ledger holds no entry of is rebuilt from the task the record already carries… **A task of another family, or one whose admission was not a delegation, stays refused by name**」——被拒绝的是"承载 send 的那次调用自己的入职任务"，不是委托族，拒绝正确 | `reentryTerminalTask` 判据由"最后一条 kind 匹配的结算事实"改为"**带回合结果正文**的结算事实"（＝被委托回合，而非承载它的调用入职）；同时把三枚 send 的编排顺序改为按"端点已答过的调用"排序，不再把"模型先读到上一条拒绝材料"当作序列前提（理由见 D13.6-④）。**委托安全判据不动** |

### D13.2 改动落点与指纹

| 文件 | 改动 | 对应红 |
|---|---|---|
| `tests/integration/execution/delivery_contract_test.go` | `:184–190` 文档 bullet（`admission_beside_the_round`）；`:225–264` 逐族计数＋两条结算的分离断言；`:266–274` `deliverySettleIdentityPrefix`＋`deliverySplitFamilies`（改按身份族前缀分组）；`:296–322` 新增 `deliverySplitSettles`；`:423–437` `deliverySettledTask` 判据加"回合结果正文" | R1 |
| `tests/integration/execution/receipt_background_separation_test.go` | `:21–27` 文档 bullet；`:178–188` 槽检查改用 `sepToolSlots`；`:232–240` flush 视图槽检查（新增覆盖）；`:253–293` 账目八族＋结算行收集与"两调用两任务"区分；`:345–395` 新增 `sepToolSlots`／`sepPresentedTicketKind`／`sepSettleNotice` | R2 |
| `tests/integration/execution/send_reentry_test.go` | `:283–292` 三枚 send 的顺序判据；`:295–310` 新增 `reentryProvider.answered`；`:331–352` `reentryTerminalTask` 判据加"回合结果正文" | R3 |

**机制实据（`-overlay` 探针，仓库零 diff）**：①R1 的两条结算正文分别为 `tagent: task task-<round>:1… settled | result: | delivery ledger: the review settled` 与 `… settled`（无 result），隔 2s 复读仍为 3 条（非竞态）；②R2 的 flush 视图 10 条消息中，第 3、6 条为 `[evt_settle-…|task_settled] tagent: task task-… settled`（`role=tool` 且挂 `call-b`／`call-a`），正是旧断言落进 `unexpected` 的两条；③R3 链上五条结算事实，旧判据取到的是 `call-reentry-send-nowhere` 自己的入职任务，被委托回合是唯一带 `result: | reentry: the first round settled` 的那条。

### D13.3 命令与原始终局

| 命令 | 终局 | 读数 |
|---|---|---|
| 三锚（改前复现）：`./tests/integration/execution -run '^(TestInstancesSymptomTwoEntriesOneAccount\|TestReceiptAndBackgroundStaySeparate\|TestSendReentersAgentTask)$' -count=1 -v` | **1** | 三红原文即 D13.1 第 2 列 |
| 三锚（改后同命令） | **0** | 父 3 PASS＋`TestSendReentersAgentTask` 两子 PASS |
| 三锚 `-count=10 -v` | **0** | `PASS` **50**（父 30＋子 20）／`FAIL` **0**；`ok … 8.602s` |
| `./tests/integration/execution -count=1` | **0** | `ok … 39.333s`；该包全量共跑四次：三次绿，唯一红见 D13.6-①（既有 flake） |
| R3 单锚 `-count=20`（顺序改造后，两轮） | **0** | 40/40（两轮共 40 次全绿）；HEAD 树同锚 `-count=40` **0**（21.011s，40/40） |
| H05 主锚 `./assembly -run '^(TestBackflowFailureRecoversWithoutManualSettle\|TestInProcessSettleFailureDrivesNoSecondTurn)$' -race -count=3` | **0** | `PASS` **6**／`DATA RACE` 0 |
| `./runtime/task -run '^TestDeliveryFactOfIsOneFactForBothWriteFaces$' -race -count=3` | **0** | `PASS` **12**（父 3＋三子 9）／`DATA RACE` 0 |

### D13.4 护栏（全包终态）

`go test ./assembly ./runtime/... ./features/... ./tests/integration/... -count=1` → **exit 0**，24 包全 `ok`：assembly 375.432s、runtime/{agent,state,task,task/approval}、features/evolution{,,/governance,/meditation}、features/{rl,rl/sample,telemetry}、`tests/integration{,,/acceptance,/capabilities,/config,/evolution,/execution,/history,/host,/hotupdate,/inspection,/journey,/learning,/state}`（execution 49.870s、host 216.457s、history 21.186s）。

**该次护栏后**，本单又在 `tests/integration/execution` 内改了两处（R1 的族前缀分组、R2 的区分式收敛），故对**最终字节**补跑该包：`./tests/integration/execution -count=1` exit 0（39.333s）；其余 23 包的被测代码与护栏那次**逐字节相同**（本单只改 `tests/integration/execution`）。

**一次由负载诱发的红（记录在案，非缺口）**：首次护栏（同一命令、改动中间态）出现唯一红 `tests/integration/execution TestResponseCallCapFuse/consumer_path_holds_the_batch_for_approval`（**95.28s**，`response_call_cap_fuse_test.go` 的等待界被 27 包同跑的负载击穿）；该锚单独跑 **7.36s** 且全绿，第二次护栏同命令 **exit 0**（含该包 49.870s）。按 D12.4 口径记为并发测量污染。

### D13.5 负变异（`go test -overlay`，隔离副本 `/tmp/rr-12-105-r3/{m2f,m3f,m5f,mut3}`，仓库零 diff）

| 变异 | 落点 | 主题锚终局 | 主题失败行 |
|---|---|---|---|
| n1 旧判据回退 | `send_reentry_test.go`：`reentryTerminalTask` 删掉"回合结果正文"判据（回到 kind-only） | `TestSendReentersAgentTask` **FAIL 3/3** | `send_reentry_test.go:104: … [send_denied] agent: the task is no delegation this ledger holds: task-…`（原文红复现） |
| n2 结算计数回退 | `delivery_contract_test.go`：`len(settles) != 2` → `!= 1` | `TestInstancesSymptomTwoEntriesOneAccount` **FAIL** | `delivery_contract_test.go:227: … = 1 sent beside 2 settles …` |
| n3 账目丢结算行 | `receipt_background_separation_test.go`：`wantCounts` 回六族 | `TestReceiptAndBackgroundStaySeparate` **FAIL** | `receipt_background_separation_test.go:286: … map[call-a settle notice:1 call-b settle notice:1 …]` |
| n4 结算行计入调用槽 | 同上：`sepToolSlots` 不排除 `task_settled` | 同锚 **FAIL** | `receipt_background_separation_test.go:235: the flush view carries 2 tool-role messages under call call-b` |
| n5 回合正文改名 | `delivery_contract_test.go`：把 `deliveryReviewAnswer+" (mismatch)"` 传给 `deliverySplitSettles` | `TestInstancesSymptomTwoEntriesOneAccount` **FAIL** | `delivery_contract_test.go:258: settle "settle-…" states a result that is not the review answer` |

控制绿＝未变异副本同命令（D13.3 第 2/3/4 行）exit 0；变异只写 `/tmp/rr-12-105-r3/`，仓库内零 diff。

### D13.6 观察、环境与未验面

1. **`TestNativeConversationRoundTrip` 是既有 flake，不属三红、未修**：候选树 `-run '^TestNativeConversationRoundTrip$' -count=10` → **5/10 红**（`native_conversation_roundtrip_test.go:93: settled observation(s) of [call-1] never reached a model request in 10s`）；把本单三文件用 `-overlay` 换回 HEAD 字节后同锚仍 **5/10 红**；`git archive HEAD` 树 **2/10 红**（20.384s）；单跑 3/3 绿。⇒ 与本单改动无关，也非本候选独有；写面外，**不修**，交编排者与 12.106 处置（按 R23 口径：不是"冷跑失败因热跑通过就归噪声"，此处有三组计数）。
2. **`git status`/`git diff` 在本仓不可用**（`.git/index` 为桩，`git status --short` 把全树列为 staged delete，`git diff --stat <file>` 对已改文件报空）；身份与 A/B 一律以 `git archive HEAD` 树（`/tmp/rr-12-105/headtree`）与 `find -newer` 取证，未改 `.git/`。
3. **结算行不带 result 正文（观察，不判缺陷）**：native 调用的结算通知不携带文本，故模型读到的是裸结算行 `tagent: task <id> settled`；这与 `deliveryFactText` 的契约一致（文本非空才追加 `result:`），且该调用的返回值已在同调用的 `action_command` 行上。记录在案，供 12.106 判"是否需要给本地结算行补正文"。
4. **R3 的顺序改造（偏离，已在 D13.1 第 4 列申报）**：旧编排把"模型先读到上一条拒绝材料"当序列前提；H05 之后每次结算各落一条材料，拒绝材料与结算材料的投影先后是 pacing，实测同一候选下时而连续、时而跨 2–3 个决策（探针 `PROBE3`），旧编排因此 1/10–4/10 超时。改造后三枚 send 按"端点已答过的调用"排序，每条拒绝仍由**它被回答的事实账户**钉住（`reentryAssertRefusalFiled` 读 `DeliveryLedger` 的 `denied` 记录），故不弱化任何已列 bullet；改造后 40/40 绿，旧判据变异 n1 仍 3/3 咬。
5. **未验面**：①未覆盖真实远端 provider；②`./tests/integration/execution` 全包只按 `-count=1` 跑（四次）；③三锚证据全在同进程内，未作独立 OS 进程重启；④12.106 的独立审与结构核销不在本单。
6. **越界自查**：`find . -name '*.go' -newer /tmp/rr-12-105-r3/repro.log` **只列**三个测试文件；`find . -newer …`（全类型）另列本单元两文件与 `./.better-quotes/lineage.json`——后者是会话期间**外来工具**写入的产物（内容指向仓库外的无关文档 `…/data/Agentic AI漫游者指南.24937`，23:17 生成），非本单写入、未修改。生产 `.go`、CI、门脚本、主规格、权限、`openspec/changes/archive/**`、git 索引/refs **零改动**；12.105 勾选状态未动。

## D9.11 · 长跑回归修复：无屏障标记（2026-11-22，F2 回叶）

> 编号属 D9 系列（D9/D9.8/D9.10 为冻结记录，本节物理上排在 D9.10 之后，与 D8.9 同一处置）。产生方式＝按 [D8.9.6](#d896-长跑回归与无屏障标记2026-11-22编排者裁决) 签发实施＋逐项复跑。**未勾任何任务框**（F2 勾选归编排者）；未 commit／push／archive。执行根 `/Users/pengweiye/Documents/codes/tagent`，Go 1.24 工具链、darwin/arm64，前缀 `env GOFLAGS=-buildvcs=false`；取被测进程原始 exit，不经管道。

### D9.11.0 结论先行

| 项 | 结论 |
|---|---|
| 修复 | `stateQuietPoint` 改走新窄口 `AppendOrdered`（只入追加序、不屏障）；`Commit` 面、`tailFactKeys` 读法、`quietPointKey` 族与 checkpoint 载荷一字未改 |
| ① 主树长跑 seed-1×256 | **match**：`repeat-1/evidence.json` `go_test_exit_code=0`、`verdict=match`、三子场景 pass、`durable_scan=attested logs=121`、`wall=216.83s inputs=1200 restarts=120`（run_id `58610631-a642-4bdd-92d7-ee48f7dc2ff4`） |
| ② 抽片 seed-42×256 | **match**：`seed-42-window-256-fix/repeat-1/evidence.json` `go_test_exit_code=0`、`verdict=match`、`durable_scan=attested logs=121`（run_id `89da1887-4a6c-49f2-bc94-c57736f19b4a`） |
| ③ 锚矩阵 | 三锚 `-race -count=3` **9/9 PASS／0 FAIL**；`./memory/ledger -count=1` **ok**；`TestLegalClusteredKeysSurviveCommitAndReopen -count=3` **3/3 PASS** |
| ④ 负变异 | 四枚见 D9.11.3：m2／m3b 确定性红，m1 语义等价（锚绿），m3（推迟一次提交）为**零结果并已定性**（不丢键，见 D9.11.4-③） |
| 生产消费者 | `stateQuietPoint`→`AppendOrdered` 在真实宿主路径实跑：`seed-1-window-256-fix/generations/logs/gen79.log`／`gen100.log` 记 `tagent: the quiet point tagent/checkpoint/v1/quiet:262:8166`（`…:10241`）`names no filed cut, so the boot reads the whole task log`——标记由新面写入、被 boot 读回，且「cut 未落」的保守读法被真实触发（子进程在标记与 checkpoint 提交之间被 SIGKILL）；同族 `gen00.log` 记 `the commit order states no quiet point of a cut`，证明该判词路径本身工作。`durable_scan` 记 attempt 48／consume 96／fact 192／spec 96 键（121 代），任务账户确由切片＋尾段读回 |
| 仓库字节 | 变异全部走 `-overlay`；`memory/ledger/journal_kv.go` md5 `825fe48db102f12626df821e0b5b92c6`、`assembly/checkpoint.go` md5 `a97eb1a814a5e77e283edaed8ca183df`，变异前后一致 |

### D9.11.1 改动落点

| 文件 | 落点（实施后行号） | 改动 |
|---|---|---|
| `memory/ledger/journal_kv.go` | `journalRecordOf`:175、`AppendOrdered`:243 | 新方法：`k.mu` 内 `journalLog.append` 后**不调** `k.log.sync()`，不写 resident；空批量零操作、Close／无写权具名拒绝、帧界具名拒绝、其余归 `ErrCommitUncertain`。帧构造由 `Commit` 的同一段代码提为 `journalRecordOf`（`Commit`:214 改为调用它，行为逐字不变） |
| `assembly/checkpoint.go` | `orderedAppend`:394、`stateQuietPoint`:415 | 消费者侧单方法窄口（未导出、结构化满足）；`stateQuietPoint` 仍持 `r.landing`，先断言 `ledger.CommitOrder`（缺失＝零操作早退），有新面则 `AppendOrdered`，否则回退既有 `Store.Commit`（装饰器/测试替身保持原行为） |

### D9.11.2 命令与原始终局

| 命令 | 终局 | 读数 |
|---|---|---|
| ① `TAGENT_CALIBRATION_RUN_DIR=…/seed-1-window-256-fix/generations python3 scripts/scenarios/run_runtime_scenarios.py --contract golden/runtime/event_integrity_scenarios.json --scope event-long --seed 1 --window 256 --repeat 1 --timeout 6270 --generation-logs …/seed-1-window-256-fix/generations --out …/seed-1-window-256-fix` | **0** | `verdict=match`、`repeats: passed=1/1`、`[long-run] seed=1 window=256 wall=216.83s inputs=1200 restarts=120`、三子场景 `PASS`（positive/negative/consumer）；对照红证：同参数两次红（`seed-1-window-256{,-r2}`，`generation 8 failed`、`FAIL wechat-bot 94.186s`） |
| ② 同命令换 `--seed 42 --out/…/seed-42-window-256-fix` | **0** | `verdict=match`、`durable_scan: attested logs=121` |
| ③ `go -C /Users/pengweiye/Documents/codes/tagent test ./assembly -run '^(TestTaskSnapshotReplaysInterleavedCommit\|TestQuietMarkerBoundsTheTailToTheWindow\|TestRecoveryEntriesPreserveStateAndObligations)$' -race -count=3 -v` | **0** | `--- PASS` **9**、`--- FAIL` **0**、子测试 PASS 27；耗时 5.22s/0.43s/1.03s（三轮一致） |
| ③ `go -C … test ./memory/ledger -count=1` | **0** | `ok github.com/SpellingDragon/tagent/memory/ledger 91.465s` |
| ③ `go -C … test ./memory/ledger -run '^TestLegalClusteredKeysSurviveCommitAndReopen$' -count=3 -v` | **0** | `--- PASS` **3**、`--- FAIL` **0** |
| 编译／静态 | **0** | `go build ./memory/ledger ./assembly` exit 0；`gofmt -l` 空；`go vet ./memory/ledger ./assembly` exit 0 |

### D9.11.3 负变异（`go test -overlay`，副本在 `/tmp/rr-f2-marker/mut/`，仓库零写）

| 变异 | 落点与做法 | 期望 | 实测（原始 exit） |
|---|---|---|---|
| m1 屏障恢复 | `journal_kv.go`：`AppendOrdered` 追加后补 `k.log.sync()`（＝候选逐 cut 一屏障的行为） | 语义等价，锚不红 | **0**：三锚 `-race -count=3` 9 PASS／0 FAIL；长跑同参数跑到 **9 代／574 秒**未及失败点即停（对照固定树 121 代／213 秒），见 D9.11.4-① |
| m2 删标记写 | `checkpoint.go`：`stateQuietPoint` 在 `CommitOrder` 早退后 `if true { return nil }`（探针同形） | H01 读面红 | **1**：`TestQuietMarkerBoundsTheTailToTheWindow` **FAIL**（三子场景全红，首红 `checkpoint_test.go:2880: the walk of a root holding a filed cut answered nil`）；`TestTaskSnapshotReplaysInterleavedCommit` **PASS**，见 D9.11.4-② |
| m3 追加序推迟一次提交（异步化最小形态） | `journal_kv.go`：`AppendOrdered` 只把记录挂到 view 上，由下一次 `Commit` 在自己之后追加 | 期望 H01 锚红 | **0**（两锚绿）：零结果并已定性，见 D9.11.4-③ |
| m3b 标记后置于快照（顺序破坏的确定性形态） | `checkpoint.go`：`checkpointLocked` 里先 `SnapshotState` 后 `stateQuietPoint` | H01 锚红 | **1**：两锚全红——`checkpoint_test.go:2889: the tail names no key of the task … that settled inside the cut's window beside the 6 key(s) it does name`；`TestTaskSnapshotReplaysInterleavedCommit` FAIL |

### D9.11.4 偏离、未验面与越界自查

1. **m1 的长跑未跑完（如实记录）**：屏障恢复后每代成本回到秒级以上的量级（9 代 574 秒，且尚未到事件断言），继续跑需小时级；该代码路径的红证已由 F2 既有证据绑定两次（`seed-1-window-256{,-r2}`，同参数、同断言），故 m1 只作"锚对屏障不敏感"的对照，不作因果证明的主证。因果主证＝①的 match 与两次红证的对照。
2. **m2 的期望红未落在 `TestTaskSnapshotReplaysInterleavedCommit`**：该锚在无标记时仍绿——`tailFactKeys` 答 `nil` 后 boot 读全日志，窗口任务照样入账（保守正确）。真正咬住"标记缺席"的是 `TestQuietMarkerBoundsTheTailToTheWindow`（`tail==nil` 即红）。**未改任何锚、未放宽断言**；只把观察如实登记，供编排者判锚的覆盖分工。
3. **m3（推迟一次提交）是零结果，不是锚失效**：标记被移过窗口内第一个提交后，窗口任务的后续键仍落在标记之后，故不丢任何键、锚绿是正确的。语义不变量是"快照与标记之间不落键"，m3 并不违反它；违反它的形态是 m3b（标记后置于快照），m3b 两锚确定性红。故"顺序破坏"的咬合证据取 m3b。
4. **未验面**：屏障去掉后的实际 fsync 节省未做基准计时（不宣称具体倍数）；F2 六长分片与 preflight 仍是权威成本面；独立复审与 F2 勾选归编排者。
5. **越界自查**：本单写面＝`memory/ledger/journal_kv.go`、`assembly/checkpoint.go`、06 域 D6 的 F2 补签行、本文件 D8.9.6＋D9.11、13 域 C11.16。零改动：`golden/**`（D1 概念预算零新绑定）、主 spec／delta、`scripts/**`、`docs/**`、`.github/**`、`examples/wechat-bot/**`、`api/**`、其余 `memory/` 与 `assembly/` 文件、`CARRYOVER.md`／`ORCHESTRATION.md`；未 commit／push／archive。工作树既有未提交面（`assembly/facts.go`、`assembly/facts_test.go`，mtime 05:50/05:51，早于本单）未触碰。

# Design: session-checkpoint-material-unification

> 冻结状态：D2–D5 与 D6 签名为派发依据，实施不得擅改；偏离上报编排者。
> 用户终裁「不起新变更，一笔做完」——本变更 scope 并入 PR #37（hot-update-and-purity-closure），不另起变更。本文件是该 scope 的设计冻结件，与 `../hot-update-and-purity-closure/ORCHESTRATION.md` W8-UNIFY 轨同账。
> 前置已落地（不再规划）：B4 past-cut＋cadence②③＋carrier 有界（7de9f08f/36d328a4/e1c5a436）；answered 切片三方 parity（5.1）；carrier 有界不删事实（5.2）；`NewestOfType` 逆扫早停无拷贝排序（2.3）；`RestoreOwed` 退役邮箱单路径（96290ef8）；开账随 cut 存活（73492f23，P1）；模型可见量有界（P2）。

## D1 第一性原理与纯度判据

链是真相，checkpoint 是派生态的载体而非第二真相；恢复＝读最近载体＋对尾部重跑同一个 g(x)，不另立派生实现。本变更只叠加三件事：协调者下沉到会话粒度、`Stateful` 收编消费者读态、六端口收敛为一个 `Material` 契约。三者共用同一组纯度判据，按相关度排序：

1. **单一真相（single truth）**：一个可恢复状态只有一个派生来源。运行期增量与「载体＋尾部重跑」必须经同一 reducer 得到逐条相等的读态；差异是缺陷不是可接受偏差（spec「恢复与运行期派生一致」）。反事实检验：删掉恢复期的第二派生实现（delivery 的 `collectDeliveryRecoveryFact`/`restoreEventifiedDeliveries`），改走运行期同一 reducer，谁坏？——无人坏，且消除了「重启后判决集与运行期不等」的症状根因。
2. **单一 g(x) 路径（single derivation path）**：`seedFact` 隔离重建复用消费者的 reducer，不另起一条读路径。emitter（observation 驱动者）与 reducer（纯派生面）是同一消费者的两个声明面，恢复期只跑 reducer、关闭 emitter。
3. **统一多态契约胜多端口（unified polymorphic contract）**：时机与失败语义是声明的策略字段，端口名不再承载语义。一个 `Material` 契约经策略分派覆盖六端口的全部行为；新增消费者/插件不新增端口。反事实：删掉端口名上的语义编码、改读策略字段，六端口行为逐条不变（4.2 兼容锚钉死）。
4. **结构不自证（structure non-self-justifying）**：每个新结构须指向独立于它而存在的需求。会话级协调者的第二使用方是「压缩本就按会话发生」这一领域事实（compress 切片已按 session 键控），不是协调者自己制造的；`Material` 策略字段的第二使用方是 spec 场景「同材料两种失败语义各按声明执行」，独立于端口名存在。
5. **词汇纪律（vocabulary discipline）**：复用 GLOSSARY 既有词（checkpoint/切片/安静点/义务/投影/结算）。新概念仅 `Material`（spec 已冻结其名）；reducer/emitter 不作为新注册公共概念，而是 `Stateful` 接缝的方法面（见 D6），按 GLOSSARY §四「写成既有能力的字段」处理。导出名受 `golden/runtime/architecture.json` concept_ratchet 约束（new_max=44、exported_interfaces_max=83，每新增导出名按名判红），故 D6 签名以「扩展既有 `Stateful`/`Instance`、策略枚举内化」为形态，不抬升棘轮。

## D2 会话粒度子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| U1 | session-coordinator | session-checkpoint-material（协调者按会话触发） | 切面由会话上下文请求，只读只写本会话派生切片，安静点只排水本会话在飞单元 | 无（在 P1/P2 已落的 checkpoint.go 上叠加） |
| U2 | stateful-gx | session-checkpoint-material（Stateful 收编读态） | reducer＝g(x) 纯派生面、emitter＝observation 驱动者；seedFact 复用 reducer，新消费者只注册切片 | U1（会话级切面读 reducer 输出） |
| U3 | material-contract | session-checkpoint-material（Material 时机×失败语义） | 六端口收敛为一个 Material 声明，时机与失败语义为策略字段，端口名留兼容读法 | 无（与 U1/U2 触碰集不相交，可并行） |
| U4 | bounded-derived | session-checkpoint-material（派生态有界不删事实） | 开放项＋水位表达，已结留链；boot 只读最新载体 | 已满足（5.1/5.2），本变更只复核会话粒度下仍成立 |

**D2 注记**：U4 的实体工作（answered 切片、carrier 有界、past-cut）已在 closure 轨落地，本变更不重做；U4 在 tasks.md 标已满足并附证据，会话级切面落地后复核「载体只含本会话切片且仍有界」。U1/U2 同触 `assembly/checkpoint.go`·`run.go`·各切片持有者，串行或合单；U3 触 `api/plugin/*`＋各 Instance 实现者，与 U1/U2 不相交。

## D3 波次表（波次仅汇报分组；解锁＝依赖入树）

| 波 | 内容 | 解锁条件 |
|---|---|---|
| WU0 | 症状锚 1.1 先红＋消费者枚举 1.3＋本设计冻结 1.2 | 立案即跑 |
| WU1 | U3 Material 契约（4.1/4.2）；U1 会话级协调者（2.1/2.2） | 触碰集不相交实测（api/plugin 簇 vs assembly checkpoint 簇）；2.3 已满足沿用 |
| WU2 | U2 Stateful 收编 g(x)（3.1/3.2/3.3） | U1 会话级切面入树（reducer 输出按会话读） |
| WU3 | U4 会话粒度复核＋收口 6.1 全门文档 | WU1/WU2 全绿 |

## D4 合并规则

- 同文件簇合单或串行：`assembly/checkpoint.go`·`run.go`·`checkpoint_inputs.go`·`compress_checkpoint.go`·`host_delivery_checkpoint.go`·`task_checkpoint.go`·`runtime/state/checkpoint.go`·`mailbox_checkpoint.go` 归 U1/U2 串行簇（U1 先落会话级切面与排水，U2 后收编 reducer）；或编排者单写。
- `api/plugin/material.go`·`phases.go`·`present.go`·`observe_duty.go`·`governance.go`＋各 Instance 实现者（assembly/features/runtime/agent）归 U3，独立派发；U3 改 `Instance` 接缝须与 U1/U2 的 `Stateful` 扩展错开导出名（D6 已内化策略枚举，不撞棘轮）。
- 共享面 `assembly/run.go` 装配缝、`api/event/checkpoint.go` 归编排者单写；U1/U2/U3 经 D6 冻结签名只改本包＋登记调用。
- 每子域入树才解锁下游；跨子域缺陷由编排者唯一裁决。本变更整体并入 PR #37，不单独合并；回退＝整 scope 回卷，与 closure 基线零 diff 为验收。

## D5 三级验收

- **叶任务**：verify 三锚 exit 0——正锚（点名测试＋`-run` 逐字符＋非空转用例数>0）／负锚（具名错误路径硬门）／消费者锚（正式入口 `Run.Checkpoint`/boot/`Pipeline.After`/`Run.Invoke` 真实路径＋负例）。三锚逐字符冻结于 tasks.md 各叶行。
- **子域**：`specs/session-checkpoint-material/spec.md` 四个 Requirement 的全部 Scenario 映射到测试或命令（会话级切面只写本会话／恢复只重跑本会话尾部／新消费者只注册切片／恢复与运行期派生一致／同材料两种失败语义／已结不进载体／有界化不打断全史读者）。
- **变更级 DoD**：症状锚 1.1 红转绿＋整 scope 回滚必红双证据；`go test ./...`、race、lint、`openspec validate session-checkpoint-material-unification --strict` 逐条 exit 0；concept_ratchet 门绿（新增导出名按名核，不抬升）；GLOSSARY 无未登记新词；LongRun 每代 boot 成本平坦。

## D6 冻结签名

### D6.1 会话级协调者（U1）

会话级切面以 `event.Address` 为粒度。协调者仍是 `Run` 的方法（不新增第七个公共协调者类型），安静点排水下沉到会话桶。

```go
// CheckpointSession 取本会话的一次切面：先读本会话水位，排水本会话在飞单元到安静点，
// 无 run 锁纯读本会话各切片，聚合为只含本会话切片的载体落链。
// 键族复用 checkpoint:<partitionSeq>，载体在索引上带本会话 session 标签（不入模型投影）。
func (r *Run) CheckpointSession(ctx context.Context, addr event.Address) (event.EventID, bool, error)

// requestSessionCheckpoint 由会话上下文（压缩产物落链/输入轮变化/会话关闭）请求一次切面；
// 是信号不是切面本身（调用者站在切面要排水的在飞单元内），filer 在自己的 goroutine 取切面。
func (r *Run) requestSessionCheckpoint(addr event.Address)
```

会话级安静点：`commitFence` 增会话键控桶，提交按其事实的 `sessionOf(evt)` 入桶；无会话事实（配置链/checkpoint 载体）入全局桶。

```go
// commitFence 的会话面（内化，不导出新类型）：
func (f *commitFence) enterSession(session event.SessionID)
func (f *commitFence) leaveSession(session event.SessionID)
func (f *commitFence) awaitSession(ctx context.Context, session event.SessionID) error
```

既有提交点按会话入桶：`commitFactNoCadence`（facts.go:86，`sessionOf(evt)`）、`fileCompressRequest`/`fileCompressProduct`（session 显式）、`writeDeliverySettlement`（target session）。`Run.Checkpoint`（全局切面）保留为兼容读法＝对全部活跃会话依次取切面或 close 收敛用；cadence filer 改为按会话到期唤醒。

最新载体读取沿用 `NewestOfType` 逆扫早停机制（2.3 已冻结），谓词增会话过滤：索引条目本就带 session（`EachIndexNewestFirst` 报 session），boot 在其已有的单遍 newest-first 索引 walk（`seedAnchoredReplay` run.go:2539）中按会话取各自最新载体身份，不拷贝不排序、只答身份。`newestCheckpointRef` 增会话变体：

```go
// newestSessionCheckpointRef 答某会话最新 checkpoint 身份，机制与 newestCheckpointRef 同
//（逆扫早停、无拷贝排序、只读身份命名的一条 body）。
func newestSessionCheckpointRef(ctx context.Context, tier *ledger.Tier, session event.SessionID) (anchorRef, bool, error)
```

恢复：`restoreCheckpointState` 按会话读最新载体→`RestoreState` 交还本会话切片→只对本会话尾部（`landingSeq > 本会话 Offset`）经 `seedFact` 重跑 g(x)，回放期 emitter 关闭（既有隔离：不调 `commitFactChecked`、不跑 After pass）。

### D6.2 g(x) 状态读面（U2，替换 Stateful-as-special-port）

`Stateful` 不成为第七端口：它仍是状态接缝，收编消费者读态 via 两个可选方法面（按类型断言读，如既有 `driverObservation` 形态，不新增导出接口、不抬棘轮）。reducer 是纯派生面，emitter 是 observation 驱动者声明。

```go
// Stateful 既有三方法不变（StateName/SnapshotState/RestoreState，runtime/state/checkpoint.go:36）。
// SnapshotState 仍是纯读：只陈述 reducer 已派生的内存态，禁 flush/commit/重入锁。

// reducer 是消费者的 g(x) 纯派生面（内化接口，不导出）：把一条已提交事实应用到派生读态。
// 运行期增量与 seedFact 尾部重跑共用此面，消除第二派生实现。
type reducer interface {
    // Reduce 把一条事实折入派生态；幂等按事实身份（同身份重放不改读态）。
    Reduce(ctx context.Context, evt event.Event) error
}

// emitter 是消费者的 observation 驱动者声明（内化接口，不导出）：恢复期框架不驱动它。
// 与 reducer 同属一个消费者；emitter 的失败语义由 Material 策略表达（D6.3）。
type emitter interface {
    // DrivesObservation 标记此消费者带 observation 驱动 duty；恢复期关闭。
    DrivesObservation()
}
```

收编映射（消除第二路径）：
- delivery：`collectDeliveryRecoveryFact`/`restoreEventifiedDeliveries`（host_delivery.go:1742/1830）降为兜底，恢复改走与 `declareObligation`/`noteDeliveryState` 同一 reducer；开账声明与结算态应用是同一 `Reduce`。
- compress：`noteSeededJudgment`（compress_consumer.go:415）已调 `Record`，收编为 `Reduce` 直接折 `compress.request`/`compress.product`，去掉从事件字段重建 `CompressRequest` 的旁路。
- projection：`recordMapped`（facts.go:464）与 `replayRef`（run.go:2851）已共用 `mappedEntryOf`＋`Append`＋`trimReplayProjection`，收编为同一 reducer 调用，trim/window 逻辑不再两份。
- inputs：`agent.PendingInputs`（consumer.go:832）已是纯 g(x)，运行期与恢复期共用，无需改写。
- mailbox：`foldOwed`（mailbox_checkpoint.go:68）与 `AcceptNotice` 按通知身份幂等，收编为同一 reducer  seating。
- tasks：`registry.Restore` 折链链接，运行期与恢复期同一 fold，无需改写。

`seedFact`（run.go:2905）改为：写内存账本后，对每个注册了 reducer 的消费者调 `Reduce(evt)`（替代当前只 `noteSeededJudgment` 的特例）；emitter 一律不驱动。新消费者只经 `Run.RegisterStateSlice` 注册一个带 reducer 的切片即获得切面＋恢复，框架不按消费者名新增挂点（3.3 负锚钉死）。

### D6.3 Material 契约策略字段（U3）

一个 `Material` 声明承载材料身份＋时机＋失败语义。`Instance` 增 `Material()` 面，`Port()` 降为兼容读法（由 Material 推导，迁移期保留）。策略枚举内化为 `Material` 的字段类型，导出名仅 `Material` 一个（撞棘轮按名核一次）。

```go
// Material 是一个面消费什么材料、何时消费、失败如何处理的统一声明。
// 端口名不再是语义的唯一载体；分派读 Timing/Failure 策略字段。
type Material struct {
    // Reads 是消费的材料身份（call/fact/request）＋产物种类，沿用端口所拼的 routing。
    Reads Port
    // Timing 是消费时机。
    Timing Timing
    // Failure 是失败语义。
    Failure Failure
}

// Timing 是材料被分派的时机（正交于失败语义）。
type Timing string
const (
    TimingPreSideEffect Timing = "pre-side-effect" // 副作用前，阻塞：pass 结果决定副作用是否运行
    TimingPostCommit    Timing = "post-commit"     // 提交后，旁路：绝不阻塞提交
    TimingRead          Timing = "read"            // 读取/装配时，幂等派生：可重算，无耐久效果
)

// Failure 是一个面失败时的语义（正交于时机）。
type Failure string
const (
    FailClosed         Failure = "fail-closed"         // 拦截：拒 pass，工作不运行/不落账
    RecordGap          Failure = "record-gap"          // 记缺口继续：具名 gap，事实保持已提交
    PreserveObligation Failure = "preserve-obligation" // 保义务：工作留在驱动者耐久账，报 ErrObservationObligationRetained
    ReportLoss         Failure = "report-loss"         // 上报欠账/丢弃：报 drop，事实保持，不重试
)

// Instance 既有 Name() 不变；Port() 保留为兼容读法。
type Instance interface {
    Name() string
    Port() Port              // 兼容读法：迁移期由 Material().Reads 推导
    Material() Material      // 新声明面：未实现者按 Port() 读兼容映射（D6.4）
}
```

### D6.4 六端口→Material 兼容映射（4.2 行为逐条不变，冻结）

| 端口 | 窄接口 | Timing | Failure | 行为证据 |
|---|---|---|---|---|
| call→call | TransformCall | PreSideEffect | FailClosed | phases.go:327 Before：err 拒 pass，call 不执行不落账 |
| call→ruling | RiskAssessor＋Governor | PreSideEffect | FailClosed | RiskAssessor 只答 grade 无 err；Governor.Judge err 拒（governance.go:141） |
| request→environment | EnvironmentInject | Read | FailClosed | phases.go:388：err 拒装配，不携未渲染块 |
| fact→observation（observer-only） | PostCommitObserver | PostCommit | RecordGap | present.go:33：err 记 `view.ObservationGap`，继续后续面 |
| fact→observation（driver） | PostCommitObserver＋driverObservation | PostCommit | PreserveObligation | observe_duty.go:64：err 包 `ErrObservationObligationRetained`，义务留驱动者账 |
| fact→view | FinalPresenter | PostCommit | ReportLoss | present.go:37：dropped 报 `RenderedView.Dropped`，不撤事实 |
| fact→compression | CompressTrigger | PostCommit | PreserveObligation | phases.go:364：err 拒 pass 于该面，request 留 pending（义务在链上） |

**映射裁定（与任务书措辞的偏差，实证纠正）**：任务书把 fact→view/compression 归「read-time idempotent derivation」。代码实证：二者当前都在提交后分派（view 在 `Pipeline.After` present.go:27；compression 在 `TriggerCompress` 由已落链 request 驱动 phases.go:364），其「幂等派生」性质来自内容寻址身份/可重渲染，而非分派时机。故 Timing 冻结为 PostCommit（与现行为一致，4.2 不变）；「读取时幂等派生」是 TimingRead 留给未来真正读时分派的面（如 request→environment 的装配读），不追认到 view/compression。此偏差按「恢复现实」处理：映射表写真实机理，不写任务书的推测分类。

## D7 决策点与风险（不隐藏）

### DP1 送达账会话归属（U1 最大风险，真实设计阻断）

**阻断事实**：送达账按实现是宿主全局（`Host.pendingDeliveries`/`declaredOpen`/`deliveryStates` 按 identity 字符串键控，host.go:194-218），按语义跨两会话耦合——开账**声明**落在源会话答案链（`declareObligation` 读 `declaredObligationOf(evt)`，host_delivery.go:1796），**结算**落在目标会话链（`writeDeliverySettlement` Owner/Source/Target=target，host_delivery.go:696-698，经 `CommitToSession(target)`）。`heldSeq` 是全局单一排序。故「会话 S 的安静点」无法只排水 S 的在飞单元就保证送达账一致：声明应用发生在源会话提交的在飞单元（`applyObligation` facts.go:148），结算发生在目标会话提交的在飞单元。

**裁定（甲案，target-session 分片）**：送达账按**目标会话**分片（`map[session]map[identity]*heldDelivery`），会话 S 的切片＝Target.SessionID==S 的义务。跨会话声明应用计入**目标**会话的 fence 桶：`applyObligation` seat 一条 targeting S 的义务时，在 S 桶内持飞至账户应用落定（声明事实本身落源链入源桶，但账户应用计目标桶）。则会话 S 的安静点＝S 链上的结算＋targeting S 的声明应用，切片一致。boot 时目标会话载体恢复义务，源链重放声明按身份幂等（`declareObligation` 已 `if stated return`）。`heldSeq` 跨分片保全局单调（取各分片 max＋全局计数器），retry consumer/settle pump 跨分片读账。

**替代方案（乙案，送达切片留宿主全局）**：送达切片不分片，每次会话切面仍读整个开账（O(开账数)，有界非 O(历史)），但安静点须排水全局 flight。**未采用理由**：重新引入整宿主安静点，违背 spec「SHALL NOT 为另一会话的状态付出读取或落链成本」与会话粒度判据；且开账虽非历史，跨会话读取仍使会话 A 的切面成本随会话 B 的开账增长。甲案符合纯度判据但触碰 host_delivery.go（2000+ 行）分片重构，是 U1 的最大实施风险与最长工时项。

**风险处置**：甲案分片重构独立成叶（tasks 2.1a），先以「两会话交错＋跨会话交付」负锚钉一致性，再推广；若分片重构在预算内不可收绿，上报编排者裁决是否退乙案并显式勘误 spec 边界（不得静默缩水）。

### DP2 每会话载体 vs 全局最新载体（U1）

**阻断事实**：`NewestOfType` 答单一全局最新；spec 场景要求载体只含触发会话切片。若每会话各落只含本会话切片的载体，全局最新是某一键会话的，boot 读全局最新会丢其余会话切片。

**裁定**：载体在索引上带 session 标签（`WithChain(TypeCheckpoint, session)`；checkpoint 事实仍不入模型投影——`mappedEntryOf` 按 `CompressionMechanism` 拒 checkpoint，facts.go:750）。boot 在既有单遍 newest-first 索引 walk 中按会话取各自最新载体身份（`newestSessionCheckpointRef`），机制仍是逆扫早停、无拷贝排序、只答身份（2.3 不变）。全局 `NewestOfType`/`Run.Checkpoint` 保留为兼容读法（close 收敛/无会话框架态）。**代价**：boot 须枚举要恢复的会话——既有 `seedAnchoredReplay` 已从索引 walk 收集全部 slot（run.go:2556-2570），按会话取最新载体搭同一 walk，不增遍历。

**勘误（回归实证回写）**：会话标签使 `persistFact` 把载体索引进触发会话链，故「checkpoint 不入投影」须守住**每一条**派生入口，不止 `mappedEntryOf`。附录 A.1 projection 行原漏列 `EachSessionFact` 冷启动持久读面——该支无类型过滤会把载体 payload 当链上事实流回 `RebuildProjectionFromWAL`，使投影渲染框架状态切片（冷启动重建 5196→11813 字节）。已在 `EachSessionFact` 持久分支按冻结类型拒 checkpoint（assembly/run.go），负锚 `TestSessionCarrierNamesTheChainWithoutEnteringIt` 钉死，索引正向锚证排除不损伤恢复。消费者枚举补：projection 的派生入口＝recordMapped/replayRef/**EachSessionFact**，三者皆须拒 checkpoint。

### DP3 Material 收敛的导出名预算（U3）

concept_ratchet exported_interfaces_max=83，每新增导出名按名判红。裁定：仅新增 `Material`/`Timing`/`Failure` 三个导出名（一个契约＋两个策略枚举），reducer/emitter 内化不导出；`Instance` 增方法不增接口数。若棘轮判红，按 W8-VOCAB 流程登记 canonical concept＋kind 并显式抬升（附理由），不静默绕过。

### D7 回退

整 scope 并入 PR #37，回退＝整 scope 回卷与 closure 基线零 diff。U3 迁移期六端口兼容读法保留，回退后 `Port()` 仍是语义载体（基线行为）。U1 会话级切面回退后 `Run.Checkpoint` 全局切面仍工作（兼容读法）。DP1 甲案若退乙案须显式勘误，不得静默。

## D8 被否方案

1. **每会话载体但仍读全局最新单一载体**（否：丢其余会话切片，DP2）。
2. **会话触发但载体仍聚合全宿主切片**（否：违背 spec「只携带触发会话切片」，为另一会话付出落链成本）。
3. **Stateful 升为第七端口**（否：任务书明令不得；状态接缝收编读态即可，新增端口违背统一契约判据）。
4. **恢复期另立派生实现**（否：违背单一 g(x) 路径；delivery 第二路径正是症状根因）。
5. **送达账留宿主全局＋全局安静点**（乙案，DP1 未采用：重新引入整宿主排水，违背会话粒度）。
6. **把 view/compression 追认为 TimingRead**（否：代码实证二者提交后分派，4.2 要求行为不变；写真实机理不写推测分类）。
7. **为每个新消费者新增专用挂点/端口**（否：3.3 负锚钉死；新消费者只注册切片）。

## 附录 A 消费者枚举（task 1.3，逐项 file:line，无「其余同理」）

### A.1 g(x) 派生消费者（持有六切片，切面写其派生态）

| 消费者 | 切面写什么 | 恢复重跑什么 | 副作用如何关闭 | 运行期 g(x) | 恢复期第二路径（收编目标） |
|---|---|---|---|---|---|
| projection | 每会话投影幸存者（identity/type/summary/fold 正文/moment），按地址序（checkpoint.go:666 projectionSlice） | restoreSurvivors 按身份水合＋replayChainPastCut 尾部（run.go:2730/2780） | 纯派生无副作用；emitter 不适用 | recordMapped（facts.go:464）＝mappedEntryOf＋Append＋trim | replayRef（run.go:2851）已共用 mappedEntryOf；trim/window 收编为同一 reducer |
| delivery | 开账清单（identity/state/body/target/lineage/heldAt/attempt）＋heldSeq＋pins，已结由水位表达（host_delivery_checkpoint.go:161 deliverySlice） | restoreDeliverySlice 载切片＋deliveryRecoveryFacts 读切面后事实（host_delivery_checkpoint.go:221/host_delivery.go:1650） | 恢复期不 Send；retry consumer 由 restoredDelivery 守卫只补送开账 | declareObligation（host_delivery.go:1796）＋noteDeliveryState（writeDeliverySettlement host_delivery.go:650） | collectDeliveryRecoveryFact/restoreEventifiedDeliveries（host_delivery.go:1742/1830）＝第二派生，收编为同一 Reduce（DP1） |
| mailbox | 欠账通知按冻结路由（mailbox_checkpoint.go:40 SnapshotState，OwedNotices） | foldOwed 按通知身份幂等 seating（mailbox_checkpoint.go:60/68） | 恢复期不 Backflow/不 Consume；sink facts.go:929 仅运行期 | AcceptNotice/Consume（runtime/state/memory.go） | foldOwed 与 AcceptNotice 两路径按身份幂等，收编为同一 reducer |
| tasks | 任务账户＋链修订（task_checkpoint.go:30 SnapshotCheckpoint） | restoreChains 按 CheckpointCoverage 只折修订后链接（run.go:1049） | 纯账户 fold 无外部副作用 | registry.Restore 折链链接（runtime/task） | 运行期与恢复期同一 fold，无需改写 |
| inputs | 每会话未应答输入轮 identity＋instance（checkpoint_inputs.go:67 pendingInputsSlice） | restoredInputFacts 按身份读回轮次事实（checkpoint_inputs.go:134） | 纯读派生；驱动由 rest criterion 运行期判 | agent.PendingInputs（consumer.go:832）纯函数 | 运行期/恢复期共用 PendingInputs，已单一路径 |
| compress | 每会话判决集（ filing 序）＋已答 identity 集（compress_checkpoint.go:69 compressSlice） | restoreCompressSlice 载判决/已答＋尾部 seedFact 补账户（compress_checkpoint.go:99） | 恢复期不 Execute 折叠；欠判决留待办运行期执行 | Record/noteAnswered（compress_consumer.go:155/396） | noteSeededJudgment（compress_consumer.go:415）从事件重建 request，收编为 Reduce 直折 |

### A.2 observation 驱动者（After pass 面＋计时/wake 泵，恢复期关闭）

| 驱动者 | 端口/duty | 切面陈述什么 | 恢复重跑什么 | 副作用如何关闭 | 证据 |
|---|---|---|---|---|---|
| telemetry commit-observer | fact→observation／observer-only（RecordGap） | 无切片（只读 observation，不持有可恢复态） | 不重跑；seedFact 不跑 After pass | 恢复期无 ObserveCommit 调用 | facts.go:1067/1084；commit_observer.go:46 |
| meditation driver | fact→observation／observer-only（RecordGap） | 无切片（reflection 锚由 meditation.AnchorStore 自管，非 checkpoint 切片） | 不重跑 reflection；boot 后由新提交驱动 | 恢复期 generationReflection 不被 seedFact 驱动 | host_tools.go:1391；driver.go:102/127 |
| settle driver | fact→observation／driver（PreserveObligation） | 义务在 mailbox 切片（settle 本身无独立切片，读 mailbox 账） | 不重跑 fire；boot 调 Wake 一次读恢复后账（host.go:786 restoreUnconsumedEvents） | 恢复期 pump 不 fire 直到 Wake；义务由 mailbox 切片存活 | settle_turn.go:297/389；DrivesObligation |
| age-fold plugin | fact→observation／observer-only（RecordGap） | 判决落 compress.request 链上事实（非独立切片） | 不重跑 judge；尾部 compress.request 由 compress reducer 补账户 | 恢复期不 FileCompressRequest | age_fold.go:175/190 |
| compress consumer（执行） | fact→observation／observer-only（RecordGap） | compress 切片（判决＋已答） | 尾部判决经 reducer 补账户，欠判决运行期 Execute | 恢复期不 Execute（seedFact 只 Reduce） | compress_consumer.go:133/138 |
| final-round planner | fact→observation／observer-only（RecordGap） | 判定落 final_round.request 链上事实（族 9） | 不重跑 judge；链上判定按身份读回 | 恢复期不 FileFinalRoundRequest | final_round_planner.go:92/102 |
| lease observer | fact→observation／observer-only（RecordGap） | 无切片（model 版本由声明＋lease 自管） | 不重跑 rebuild；boot 从声明建 lease | 恢复期不 ObserveCommit 驱动 rebuild | lease_observer.go:76/88 |
| delivery retry consumer | 非端口（计时泵，读开账） | delivery 切片（attempts 在载体） | restoreDeliverySlice 载 attempts→schedule 续 backoff（host_delivery_checkpoint.go:290） | 恢复期不立即 fire；按 restored attempt 续 bounded backoff | host_delivery.go:1221/1283 |

**枚举完备性核对**：六切片持有者＝A.1 六行（projection/delivery/mailbox/tasks/inputs/compress）；After pass fact→observation 面＝A.2 前七行（telemetry/meditation/settle/age-fold/compress-exec/final-round/lease，皆经 tools.go:246 plugins 工厂＋run.go:580＋host.go:679 装配）；计时/wake 泵＝delivery retry（A.2 末行）＋settle pump（settle 行）。无「其余同理」条目。

## 附录 B 症状锚（task 1.1）

两句用户可观察断言，第一天存在且红，修复后转绿，整 scope 回滚必红：

1. **「同一会话跨代 boot 的检查点恢复成本不随代数增长」** → `TestSymptomAnchorSessionBootCostFlatAcrossGenerations`（新增，assembly）：同一会话连续 N 代重启，每代 boot 只读本会话最新载体＋尾部，恢复读取量/耗时平坦；负锚＝退回逐站点全史扫必红。工程佐证锚＝LongRun `TestEventIntegrityLongRun` 每代 boot 成本平坦（既有合并门）。
2. **「重启后压缩判决集与运行期逐条相等」** → `TestSymptomAnchorCompressionJudgmentParityAfterRestart`（新增，assembly）：live≡booted≡full 三方判决集逐条相等；既有 `TestCompressionJudgmentsSurviveTheBootCut`（compress_checkpoint_test.go:151）已钉此 parity，症状锚是其用户视角直译，复用同一 requireSameAccount 判据。负锚＝跳过 boundary 判决集缺项必红。
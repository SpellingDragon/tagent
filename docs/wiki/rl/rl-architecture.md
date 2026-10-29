> **活契约页**：逐条对齐当前码面（`features/rl/`、`runtime/agent/loop.go` 的窄采集面、`assembly/host_capture.go`），代码内 `// 契约:` 注释指向本页锚点。口径统一于[存储概念地图](../../demos/storage-concept-map.html)：**链**管身份与因果 · **原文仓**管字节 · **账本**管责任 · **其余一切现算**——训练样本是现算的派生物，不另立一套身份。

# 训练观测面：执行核只交三个事实，样本归训练侧

训练形态**外移**了。执行核不再持有 `agent.Recorder`——episode/decision 的记录组织、样本组装、导出全部搬进 `features/rl`。执行核只剩一个三方法采集端口，**它是唯一的记录点**：请求开始前 `Snapshot` 固定实际输入，每次观测到的 attempt 由 `Record` 连同产生它的响应一起交出去，一轮结束 `Seal` 封账。中间 decision 的 outcome 只经这条路落地——`commitOutput` 仍只提交**最后一条** decision 的 `agent_output` 到链上，训练侧要的中间过程只能从采集证据里取。

验收面因此是 **attempt-only**：`tests/integration/learning/formal_invoke_records_test.go` 的 `TestFormalInvokeProducesTrainingRecords` 断言一次真实调用产出 **0 条 episode、0 条 decision、2 条 attempt**，且两条 attempt 带同一 episode 身份、共享同一 terminal。

<a id="trajectory-capture"></a>
## 一、采集端口：三个方法，四条红线

执行核的 `agent.Capture`（`runtime/agent/loop.go`）与训练侧的 `rl.Capture`（`features/rl/capture.go`）同名三方法，中间由 `CaptureScope` 桥接：

| 方法 | 交出去的东西 | 不许做的事 |
|---|---|---|
| `Snapshot` | 请求开始**之前**固定：模型名、prompt 版本、可见工具与技能的模型视图，加请求本身 | 不得用已完成调用的 options 反推成品调用（`ErrRebuildRefused`）；请求只被观测，不被编辑 |
| `Record` | 一次观测到的 attempt 与产生它的 native response；episode 身份随 ready input 走，terminal 是模型约简后的那一个 | 不得二次推导 terminal；被拒的受理必须记成**计数损失**，不许静默丢 |
| `Seal` | 一轮产出停止后的封账；manifest 由真实写者确认 | 手里还有在途记录时不许报"完整" |

另两条：**采集失败绝不进模型决策**（记录是观测，不是执行），**受理绝不等待写者**。失败按 kind 进 `RecordingLosses()` 三档：`capture-snapshot`／`capture-attempt`／`capture-seal`。

**组合根只开一个账户**：`openHostCapture` 在 `<root>/trajectories` 下开一次 capture（id `host-trajectory`），`hostRunCapture` 把**每轮 `Seal` 变成 no-op**（一轮结束不等于账户结束），真正封账发生在 run 排空后的 `Close`，只封一次。

**有界受理与封账**：`CaptureSink` 三条界（默认单条 1 MiB／在途 4096／总预算 512 MiB；负值拒绝，不当"不限量"），超界即按 `CaptureBound` 之一（`record_size`／`in_flight`／`bytes`／`closed`）计数拒收，**绝不靠删除或轮转已有历史腾地方**。`CaptureManifest` 报 `Sealed`／`Partial`／`Records`／`Dropped`／`InFlight`／`Files`／`Sync`／`Reasons`，落成 `seal.json`（`ReadCaptureSeal` 读回）；`max_open_files` 超过硬上限 16 被夹取而不是服从。

<a id="trajectory-recorder"></a>
## 二、记录形态：一行一记录，版本自守

- **schema 自守**：当前 `tagent.record.v1`；`capture.v2`／`trajectory.v1` 只走 legacy 导入、绝不回写；未知版本 `ErrSchemaUnknown` 拒绝，**不按已知版本读**。
- **一行恰一种 kind**（`episode`／`decision`／`attempt`）：`ErrLineShape` 拒 kind 与载荷不符的行；`legacyMarkerFields` 九条旧字段（`schema_version`／`capture_scope`／`run_id`／`call_id`／`llm_call`／`tool_calls`／`tool_results`／`missing_reasons`／`binding_status`）出现在 v1 行里即 `ErrSchemaMisuse`（声明 v1、形状是旧采集）。
- **attempt 记录**：native response 原样、terminal 与模型约简共享、`LogprobsState`（`unsupported` 必须带声明来源，否则 `ErrLogprobsSourceMissing`）、`ModelVersion`、快照引用。
- **`RecorderAdapter`（`produce.go`）只写 attempt**：`Snapshot`／`Seal` 接受但不写一行（快照与封账归 capture 自己），**episode 边界与 decision 的存储输入不在这个受理点被观测，所以这里既不组装也不发明它们**——需要者从采集证据加链上事实自行推导。这正是"训练形态外移"的落点。

<a id="training-export"></a>
## 三、训练侧自组样本与 SFT 导出

训练侧的面是 `rl.Recorder` 三方法（episode／decision／attempt），由组合根的 `assembly.WithRecorder` 挂上；一次写入被拒计入这次 run 的**记录损失账**，不是静默丢弃。样本由训练侧自己组：`RecorderAdapter` 供 attempt，episode／decision 由它连同链上事实推导。

导出走 `ChainExporter`（`export_chain.go`）：

| 契约 | 规则 |
|---|---|
| 只读 | 只握 `FactReader.SessionFacts`，按**提交顺序**报已提交事实；不重建、不重分类、不写回 |
| 作用域 | `ExportOptions{SessionIDs, Limit}`：空会话表**不导出任何样本**（不扫全库）；负 `Limit` 拒绝 |
| 行形状 | `SFTSample{SessionID, Turn, Input, Label, EpisodeID}`——一轮一行，`Turn` 在 episode 内从 1 起 |
| 配对 | 一轮的输入与**同一次 invocation** 的 assistant 输出配对；缺输入或缺输出就**不造样本**，绝不拼凑 |

验收锚：`TestChainExporterProducesSamples`、`TestFormalChainSealsCapture`（封账可读且已 seal）、`TestCaptureAdmissionAndSeal`、`TestCaptureFidelityAndRedaction`、`TestFormalRequestSnapshotHasNoSecrets`、`TestCaptureSealsAttemptsFile`、`TestScenario10EpisodeObservationAndReplacement`。

<a id="swappable-model"></a>
## 四、可换模型：换版本，不换句柄

- **身份**：`ModelIdentity` 是 `name@version`（分隔符单一）。`ParseModelIdentity` 只在**恰一个分隔符且两侧非空**时切分，其余整名当无版本——provider 名里自带 `@` 时绝不被切成一个从没声明过的版本。无版本记为 unversioned，不假定当前版本。
- **句柄稳定**：`LeaseModel` 是绑定的稳定模型句柄，跨替换不变，所以一次发布就够了；每次调用借用**调用开始时生效的版本**，并按该版本的流租约覆盖整条返回流——替换绝不在活请求底下换模型。
- **三次尝试才算换**：`Replace` 的顺序是**重建 → 发布 → 才安装**，整体拒绝：重建失败/无模型即拒绝且不触发布；发布拒绝则释放重建出的模型恰好一次、生效版本原封不动；成功才把新版本设为"此后开始的请求所借用"，并把被替换的那个标为退役——退役版本继续服务它在途的流，最后一条流结束时关闭**恰好一次**。
- **历史是账不是开关**：`ReplacementRecord{From, To, State, Reason, At}` 与 `Replacements()` 记录尝试；`Restore` 拒绝"持久历史最后一版 ≠ 当前持有版本"（`ErrLeaseRestoreMismatch`），重启的租约绝不把一条请求串到上一个进程服务的版本上。
- **事实落链**：`lease_observer.go` 把 `model.replacement` 事实接进租约，`TestDeclaredModelChangeFilesReplacementFact`／`TestReplacementFactInstallsNewVersion`／`TestFailedReplacementKeepsOldVersion`／`TestModelReplacementLifecycle` 钉住"失败保旧版本"。

<a id="http-api"></a>
## 五、对外 HTTP 面：只剩宿主那四个路由

`features/rl` 不再持 HTTP 服务器、鉴权或限流——对外面整体交给宿主（`cli`）。`CLI.handler()` 只有四条路由：`GET /healthz`（回裸 `ok`，不做状态渲染）、`GET /query`、`GET /diagnostics`、可选 `POST /feedback`；**单一鉴权门在路由解析之前**，未鉴权一律 401 且带 `WWW-Authenticate`，没有任何端点旁路。

反馈面挂同一道门后（`WithFeedbackEndpoint`，端点自己在自己的位置再核一次凭据），训练侧对外只剩三个计数：`FeedbackAttribution{Bound, Unbound, Ambiguous}`，由 `GET /diagnostics` 的 `Training.Feedback` 读回——`Bound` 归到有据可查的证据，`Unbound` 是没有可定位引用的流量，`Ambiguous` 是证据自相冲突。三者都是现算读数，别指望用它们反推单条反馈的归属。

## 已知缺口与演进方向

| 缺口 | 现状与防线 | 候选方向 |
|---|---|---|
| episode／decision 记录无人组装 | `RecorderAdapter` 只写 attempt；需要者自己从采集证据推导 | 把"从采集证据重建 episode 边界"写成一份公开配方 |
| 记录损失只在 run 内存里 | `RecordingLosses()` 随 run 生命周期，重启即失 | 损失账落链，供离线核账 |
| 无在线训练环 | 只余 `features/rl` 的 Go 面与离线导出；AReaL 桥已退役 | 按现行 `areal.*` 布局重写适配器，先过 `codetools dotted-refs` 门 |

延伸：[存储概念图](../../demos/storage-concept-map.html)（链／原文仓／账本／现算的分工）· [存储时光机](../../demos/storage-time-travel.html)

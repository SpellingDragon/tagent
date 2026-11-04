# Changelog

本项目所有显著变更记录于此。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Documentation

- README 将 tagent 定位为可嵌入的 Go Super-agent 框架，补充事件驱动、插件、公开技术阅读参考、使用场景与运行挑战，并提供英文入口。
- 公开 2026-11-01 开发快照的冒烟与吞吐证据：6 个基准场景 × 3 次重复（18 组样本）通过；完整长冒烟反思失败、消费红锚和真实 provider 跳过状态分别记录，不混作通过。
- 中英文 README 将 DeepSeek 材料移至文末参考与致谢，澄清引用不表示项目隶属、合作或背书，感谢实际底层依赖项目 tRPC-Agent-Go，并关联实验 fork、保留官方上游归属。

### Breaking Changes（模型面：send 收敛为唯一跨 task、会话通信工具；`deliver_summary`、`resume_task` 退役）

五条均按四要素记（**内容 / 影响范围 / 触发条件 / 自查方法**）；可检索关键字：`send`、`deliver_summary`、`resume_task`、`reactivate`。契约归属见 `wechat-host-send-tool`、`task-reentry`、`async-task-execution`、`cross-session-delivery`、`meditation-delivery-tool`。

#### 1. send 职责重定义：跨 task、会话通信，不再出网

- **内容**：`send` 的唯一职责是跨 task、会话通信。参数面收敛为 `target` 对象（二选一：`{"task": …}` 重入/续行 task，或 `{"agent": …, "session": …}` 投递同级会话）+ `content`（上限 8KiB，超限具名拒绝）；Declaration 只承诺这两类寻址，不再承诺“送达本次调用的用户会址”。回执两态：成功裸 `ok`（语义是送达——任务形态待该轮首个 commit 落地、会话形态待入链，拒绝即 `[send_denied] <reason>`，拒收也落一条 delivery 事实）；治理挂起不在 send 方言内，走受理回执的 `task.HoldReceipt` 形状；error 只留给参数形状非法。
- **影响范围**：任一 agent 的模型工具面；教模型“用 send 说话到渠道”的部署提示、skill 与自动化；`assembly/host_tools.go` 的 send 声明、参数解析与分派面。
- **触发条件**：模型调 `send`；参数面仍按旧的单参数形态（只给 `content`）调用——缺 `target` 现以 `[send_denied]` 拒绝，不再默认投给本次调用的用户会址；部署提示仍把 send 说成对用户说话的通路。
- **自查方法**：`go test ./assembly -run '^TestSendDeclarationRewrite$' -count=1`（负锚子测 `declaration_has_no_channel_wording`：声明出现“user address”/“deliver to the user”即红）与 `go test ./assembly -run '^TestSendTargetResolution$' -count=1`（负锚：双目标同现 / 皆缺 / 未知字段逐个具名拒绝）；工具面观察 `send` 的入参只有 `target`+`content`。

#### 2. `deliver_summary` 删除归并（DEL-2）

- **内容**：冥想跨会话写不再走 `deliver_summary`，改经 `send{target:{agent, session}}`；`DeliverSummaryTool`（`features/evolution/meditation/deliver.go`）、`meditation.DeliverSummaryToolName` 与工具面挂载点一并删除——清除式归并，不并存、不设运行时开关（回退=回滚提交）。`deliver_to` 白名单语义保留，只换运输工具。
- **影响范围**：冥想会话的模型工具面与其交付实现；提示词、smoke 场景与自动化中“用 deliver_summary 汇报”的写法；`hostMeditationPort` 随之降为单使用方或无使用方而回卷。
- **触发条件**：冥想回合尝试跨会话写；提示词/自动化仍按工具名 `deliver_summary` 调用（该工具已不存在）；`grep` 到残留代码引用即未迁移完。
- **自查方法**：`grep -rn "deliver_summary\|DeliverSummaryTool" --include=*.go .` 零命中；`grep -rn "deliver_summary" resources/ examples/wechat-bot/resources/` 零命中；冥想跨会话写由 `send{agent,session}` 承载（入链+驱动目标一轮）。

#### 3. `resume_task` 作为模型工具删除归并（DEL-3）

- **内容**：task 重入不再以 `resume_task` 名义存在，改经 `send{target:{task}, content}`——main agent 面前的 agent tool 与异步命令统一按 task 寻址。`resume_task` 的模型工具面（`task-reentry` 主 spec 的声明与 prompt 资产里的调用写法）删除，模型语境下不再有第二个通信工具名；被 send 复用的底层 Resume/注入通道保留（新机制的实现基座，不沿用旧工具名）。
- **影响范围**：`resources/prompts/`、`examples/wechat-bot/resources/prompts/` 中的重入指引；按 `resume_task(task_id, input)` 签名编排的提示与自动化。
- **触发条件**：提示词/自动化仍按 `resume_task(task_id, input)` 调用（该工具名已不在任何模型语境）；或模型面仍出现第二个通信工具。
- **自查方法**：`grep -rn "resume_task" resources/ examples/wechat-bot/resources/` 零命中；`grep -rn "resume_task" --include=*.go .` 零命中（保留的底层通道不沿用该名）；重入改由 `send{target:{task}}` 承载。

#### 4. settled task 可被重新激活（`reactivate` 状态迁移）

- **内容**：对 settled task 发 `send{target:{task}}` 触发重激活：状态 settled→活跃（registry 新增 `reactivate` 迁移，`MutationID("reactivate:<task>:<seq>")` + `ExpectedRevision` 乐观并发，两路并发只有一路占坑成功），以新 Round 续行、可再次 settle；翻转以链上事实可追溯（前一条 holding settled，本条 holding active）。新 Round 的结算通知是新身份（TaskID+Round+Kind+Seq 不与旧合并），发起方后续回合恰好被驱动一次。failed/cancelled 具名拒绝并引导 relaunch / 重新 spawn；round 在飞（running/suspect）拒绝并引导等待。
- **影响范围**：task 状态机与其审计事实；消费结算通知的驱动面；既有 `task-reentry`/`async-task-execution` 中“完成态（含 **failed**）可重入”条款按用户裁定翻转——failed 不再原地重入。
- **触发条件**：对已 settle 的 task 发 `send{target:{task}}`（task 不再“一轮即终结”）；对 failed/cancelled task 发同一形态（必须得到具名拒绝，而非重入）。
- **自查方法**：`go test ./runtime/task -run '^TestTaskReactivation$' -count=1`（负锚：`rejects_failed_reactivation`、`rejects_cancelled_reactivation`、`rejects_concurrent_reactivation`）；`grep -c "reactivate:" runtime/task/reactivate.go` > 0；端到端锚 `go test ./tests/integration/execution -run '^TestSendSymptomReentersAgentTask$' -count=1`。

#### 5. 旧出网 send 语义移除（DEL-1）

- **内容**：`hostSendTool` 不再持有出网交付缝——`deliver` 字段与 send 侧的 `plugin.Delivery` 构造删除；出网唯一路径是回合正文经发送插件自动投递（v0.4.0 既定，本次不变）。send 指向本 host 未接线的寻址面时具名拒绝，绝不回退成“投给本次调用的用户会址”（那会回答模型没问的问题）。
- **影响范围**：`assembly/host_tools.go` 的装配面；`tests/integration/host` 的 send 出网场景（场景迁移不删场景、不改断言语义）；依赖“send 即出网”的部署提示。
- **触发条件**：部署提示仍以 send 作为对用户说话的手段；或对未接线的寻址面调 send——应得 `[send_denied]`，而不是投递。
- **自查方法**：`grep -rn "hostSendLineage" --include=*.go .` 零命中（若保留须有注释写明保留理由）；`grep -c "user address of this call" assembly/host_tools.go` 为 0；对照 `assembly/host_tools.go` 的 dispatch：task 走重入面、其余寻址面在未装配时拒绝而非出网。

### Breaking Changes（运行时：一条事实链、一个消费循环；`170ef267` squash）

同提交 `170ef267`（2026-11-04）落地 150 轮编排的运行时收敛，下面的公共面变化此前未记入 CHANGELOG，现按四要素补齐。概念净账见 `openspec/changes/archive/2026-11-03-runtime-architecture-convergence/evidence/04-concept-net-ledger.md`。

#### 1. 十个公共导出面删除（概念净减）

- **内容**：`Writers`、`PresentedFace`、`Movement`、`Continuations`、`Channel/ChannelRow/Change/Dimension/Router`、`ModelReplacement`（事实族）整体退役；`domain_surface.removed=10`、`renames=0`、`exported_interfaces 78→77`，无兼容别名。
- **影响范围**：直接引用上述符号的嵌入方；`golden/runtime/architecture.json::concept_ratchet` 冻结 `removed≥10 / new≤1 / interfaces≤77`，回加任一公共面即判红。
- **触发条件**：升级后引用旧公共面编译失败；或以 `--skip-specs` 绕过规格同步。
- **自查方法**：`python3 scripts/gates/check_concept_budget.py --json` → `errors: []` 且 `domain_surface.removed=10`；`grep -rnE 'TagentAgent|RegisterBuiltinTools|ToolRegistry' --include=*.go .` 零命中。

#### 2. `Loop.Drive` 退出公共面，装配根与关闭入口正名

- **内容**：`Assemble` 从模块根包迁至 `assembly/run.go:368`；`Run.Close` 形状为 `(ctx, agent.CloseRequest)`；`hostFace` 的 `/healthz` 与 `/task` 共用同一鉴权门（`TAGENT_RL_AUTH_TOKEN` 非空时回环也必须带 Bearer）。`runtime/agent/shutdown.go` 不存在。
- **影响范围**：按 `tagent.Assemble`／`tagent.NativeAgent` 写的示例与部署脚本；`examples/cli-host/README.md`、`examples/wechat-bot/README.md` 已同步正名。
- **触发条件**：旧 `tagent.*` import 路径；或 healthcheck 不传 Bearer 而容器设了 token（恒 unhealthy）。
- **自查方法**：`go build ./...`；`grep -rn 'tagent.Assemble' --include=*.go .` 零命中；`grep -c 'CMD-SHELL' examples/wechat-bot/docker-compose.yml` > 0。

#### 3. 每地址一个常驻消费循环，同步入口等待闭合段

- **内容**：`Host.Invoke`／`Run.Invoke`／`Run.InjectEvent` 只提交事实并唤醒目标地址，调用方等待其输入所属闭合段的不可变结果；`Loop.drive` 无生产调用者（仅测试），`tagent: consumer drive start/end` 只在这条死面发出。现行观察面为 `consumer segment open/close` 与 `consumer decision`（见 `docs/OPERATION.md` §七·E）。
- **影响范围**：把「调用即驱动一次模型」当作同步语义的嵌入方；依赖 `consumer drive start/end` 日志行的运维脚本。
- **触发条件**：升级后按旧同步语义假设调用返回即回合结束；或按 `consumer drive start` 抓日志为空。
- **自查方法**：`go test ./tests/integration/host -run '^TestQuiesceCancelledCallerAnswersCut$' -count=1`；`grep -rn 'consumer drive start' --include=*.go .` 仅命中 `Consumer.driveInvocation`。

#### 4. CI 事件门十分片 + 症状锚六例

- **内容**：`event-integrity-short` 由 `--repeat 10` 改为 10 路 matrix `--repeat-index`（artifact `event-integrity-short-1..10`）；本地 `scripts/gates/preflight.sh` 仍用 `--repeat 10`，两个入口不可混写。`event-semantics-symptoms` 选择六个 `TestLocalSmokeSymptom*`（含 NoSendTool、DeliveryHold）。
- **影响范围**：按旧 artifact 名 `event-integrity-short` 取证据的脚本；`docs/OPERATION.md` §5.4 已同步。
- **触发条件**：CI 里出现裸 `--repeat`（`test_check_runtime_workflow.py` 明禁）；或按「四个」症状锚计数。
- **自查方法**：`python3 -m unittest discover -s scripts/gates -p 'test_check_runtime_workflow.py'`；`grep -c 'func TestLocalSmokeSymptom' examples/wechat-bot/smoke_test.go` = 6。

### Documentation

- 逐模块代码核对（api/runtime/assembly、memory/tool/features、examples/tests/scripts/workflows、specs/wiki 四簇）：修正热更结构拒绝范围、model replacement 退役符号、投递五键、注意力冻结类型与元数据键、`DecisionCount`／`CloseOutcome` 四值、`Assemble`／`shutdown.go` 等假指针、wechat/cli-host/bench 示例 README 的 phantom 符号与鉴权口径；补 7 篇历史页锚点、manifest 声明、`docs/wiki/README.md` 分类与门禁口径。证据与逐条缺口见 `openspec/changes/archive/2026-11-03-runtime-architecture-convergence/evidence/07-11` 与 `10-doc-gap-register.md`。

## [v0.5.0] - 2026-10-30

> 事后补齐（2026-11-04）：tag `v0.5.0`（`21713e47` → `c111d7a2`）打 tag 时未定版 CHANGELOG（`git show v0.5.0:CHANGELOG.md` 从 `[Unreleased]` 直接跳 v0.4.0）。本节据 tag message 与当期提交补齐，**不改变 tag 指向**。注意 tag 目标是 feature squash（#32 的 `c111d7a2`）而非独立 release commit；`[Unreleased]` 在打 tag 时未定版，故本节与 `[Unreleased]` 存在时间重叠，发布流程偏差登记在 `openspec/changes/archive/2026-11-03-runtime-architecture-convergence/evidence/09-ci-run-attestation.md`。该提交信息整段照抄了 #31 的冥想条目（`PendingCount`/`summary_timeout` 等符号在树中不存在），仅消息误植，非工作丢失。

### Added（tag message 所列）

- **intake 每个决策边界读链**：在途输入在链上受理，不再 park。
- **冥想让位**：会话活跃时让位并在既定节奏重评（`TickDisposition` 五值、`DeferredCount()`／`LastDeferredAt()`）。
- **稳定前缀 + 尾部动态 system 段**：工具定义随 `ToolOrder` 传递（tag message 记 fork `v1.11.2-tagent.2`；当前 `go.mod:116` 已推进到 `v1.11.2-tagent.3`）。
- **可见面运行期收窄**：工具/MCP/skill 面是配置子集，默认全集。

### Documentation

- 中文 README 重写为可嵌入的 Go Super-agent 框架（Super-agent 实践对照表、架构 mermaid 图、6 行吞吐实测与可复现证据）；英文版保留「完整长冒烟 FAIL / provider SKIPPED」披露。
- 16 篇活契约 wiki 页按当时代码重写式修订；`docs/archive/wiki/PRE-REWRITE-MANIFEST.md` 记录保全对象（其「131→131 一个未删」为写时结论，`1ac7cf88` 后现行 106/131，见该 manifest 的 2026-11-04 注记）。

## [v0.4.0] - 2026-10-26

### Breaking Changes（事件语义边界与发送/审核解耦；发送不再是模型工具）

- **entry 不再授予 `send`**：模型对用户的说话唯一路径是回合正文，经发送插件（投递唯一出口）送达；`send` 仅保留为跨代理通信工具（当前无消费者，不装配）。影响：任何依赖“模型调 send 说话”的部署提示需改写；模型工具面见 `TestLocalSmokeSymptomNoSendTool`。
- **受理回执收敛为裸 `ok`**（`admissionReceiptText`）：task 身份/状态/承诺不再进模型上下文（`accepted task-` 文本退役）；委派工具描述同步（不再承诺回执携带 task 身份）。
- **send 成功回执收敛为裸 `ok`**：`[send_ok] delivered to ...` 退役；`[send_denied] <reason>` 具名拒绝保留。
- **progress 通知只落事实**（`commitProgressFact`）：不再 park 进 intake、不进任何模型请求材料；终态通知（settled/failed/cancelled）照旧驱动后续回合。
- **exec 中间态（stable/suspect/exited）不驱动模型回合、不进材料**：只落无载荷审计事实（`exec-state: <phase>` 前缀 + resource/phase/exit_code），读面走状态查询；终态由 settle 通知承担。

### Added

- **失败告知**（`failure-notice:` 事实 + sender 直投）：模型真失败且回合无可交付正文时，框架直投**原始错误**（2KiB 截断+「（已截断）」标注）给用户；不编造原因、不静默（`TestLocalSmokeSymptomFailureNotice`）。
- **投递唯一出口 + 投递前审核**（`api/plugin` Delivery/DeliveryRuler + `host.deliverOne`）：回合正文、失败告知、跨代理 send 共用一出口；投递语义（内容/血统/目标）交审核插件裁决，三态 allow/record/**hold**（hold=批准后补投，拒绝丢弃+审计）；默认 allow，规则显式触发。
- **独立注入会话**（`POST /task` 可选 `session` 字段）：注入驱动 `injected:` 命名空间的独立会话，无需微信 admit；通知回投该会话。
- **症状锚六组 + CI job `event-semantics-symptoms`**：受理静默 / 完成恰一条 / 失败告知 / exec 中间态静默 / entry 无 send / 投递 hold-批准补投-拒绝；红转绿双证据随 F 收口。
- **每 MR 门禁三档落地**：`event-integrity-short`（十轮独立凭据）、`event-integrity-long`（六组合 7200 输入/720 重启）、`event-integrity-mutations`（11 变异 killed）+ `event-integrity-required` 汇总（bundle 17 份证据）。
- **exec 声明可枚举**（`tool/exec`）：kind 枚举 `["", "tmux"]`、resource Pattern `[A-Za-z0-9_-]{1,64}`+描述同源（`kindSchema`/`resourceSchema` 双 Declaration 同词表）。
- **embedding 空文本具名拒绝**（`ErrEmptyText`）：空查询在请求前短路，检索降级 keyword-only，不再 3 次无效 provider 重试。

### Fixed

- EVID-02 双路材料（`fireNoticeTurn` 去 Input）、EVID-03 忙时丢通知（retained+rearm）、EVID-04 折叠边界越界（`endedPrefix` 身份边界）、EVID-05 重启重折回归（`restoreFoldForm` 同形重折）。
- tmux 3.4 负载下不记 `pane_dead_status`（drain 包装写退出记录+Stop 回读）；tmux server 退出判 confirmed stop。
- evidence 记相对路径（可搬迁 bundle 校验）；`pipelineProgress` 判据显式化（Round 语义钉死）；`admitNotice` 死代码清理。

## [v0.3.0] - 2026-10-11

### Breaking Changes（merge-review-remediation：dev→main 合并前评审修订，45 项发现全量落地；pre-release 姿态，无迁移承诺）

- **谱系投递白名单改正向集合**（event.DeliverableLineage）：仅 user/task/reincarnation/system_alert/meditation 五类可投，余 fail-closed；task-unstamped 与未知值不漏投，旧前缀启发式退役。
- **结算折叠资格改结构化标记**（settle_notice，D10）：正文形状启发式退役；无标记事件（伪造体、旧事件）逐字保留，纯结构模式按内部。
- **LocalFileKV 快照分区分片**（kv-<pid>.json / kv-global.json）：Sync 只重写脏桶；旧 kv.json 不迁移、忽略并报一次，冷启动重建。
- **诊断面死字段删除**：WalQuarantined 四层链（config/引擎桥/诊断/HTTP）与字段消失，消费方按 absent 处理。
- **`memory.fsync` 与 fsync 写面整体删除**：Sync 屏障=原子 tmp+rename 快照（flush-only，不称抗掉电）；含 `fsync:` 键的配置因严格装载拒启动（KnownFields）。

### Added

- 测试面 47 个新用例：worktree 隔离门禁矩阵、活 tmux 全路径 rebind、分区快照脏桶隔离/跨进程读回/旧格式忽略、回收竞态终局语义、nil-channel 三分判据。
- **入站意图声明**（openspec:delivery-intent-and-receipts P1，`POST /task`）：可选 `trigger_source` 声明 `user` 血统（值域仅 `user`）；无 auth 拒 `declaration_requires_auth`、缺信封能力 501，fail-closed；入 Metadata，`Source` 留通道标签。
- **投递终态统一回执**（wechat-bot 分发层，K2）：已送达不回执；send 失败 ERROR；未知/未声明血统、冥想含交付特征、error、无目标四类 WARN；回执以 `delivery_receipt` 血统入持久总线（转生后仍在账、输出静默、不武装新颖门、不递归）——两起「感知成功/发送未发生」闭环。
- **结算血统一级键**（`settle_trigger_source`，K3）：结算派生血统升为事实链一级可读键（`source_snapshot` 保留），与回合级 `trigger_source` 对账。
- **per-call 子 agent 覆盖层**（openspec:subagent-config-hotreload 域 03）：`system_prompt_override`/`model_override`/`tools_subset`/`context_refs` 限 invocation 作用域、随调用消亡；`tools_subset` 越界具名拒绝；随 `Declarative.Overrides` 冻结，relaunch 还原同一视图。**参数 3→7 项，锁参数总数断言需同步**。
- **回环源受理入站意图声明**（openspec:declaration-trust-loopback）：受理前提由「已鉴权」扩为「已鉴权 或 回环源」——无 token 只绑 127.0.0.1 时邮件轮不被扣（原症状：产出不可投递）；判据取 TCP 源地址，值域不因源放宽，豁免受理记 remote；mail-poller 条件同步「携凭或回环」。

### Fixed

- **热更同步入口可空退**（`tagent.go` `reload()`）：`lastSeenMtime` 锁外早退 → `CheckOrgReload` 读旧值（约 5%/轮、~1ms 自愈，race 门复现）；现锁内判定，test-only 屏障守 applied⇒读到该值。
- **空回合血统写入空串一级键**（`buildBusFact`，K3②）：`trigger_source` 原无条件写 `cm.triggerSource`，为空不写（原“键存在但为空”误判未盖章，email-inbound 现场），同 `buildEventAttributes`。
- **压实跨折叠覆盖丢历史**（memory，soak 回归、基线同形红）：同日二次 L1→L2（同周 L2→L3）自 seq=0 写入取整窗不查既存段、逐键覆盖（字节消失、meta 计数失真、idx 悬空）→ fresh 重启召回为空（soak 连续性 promise 从未真绿）；现既存窗并入 merge 读取源、按 EventKey 去重封死 crash-retry，测例入 CI（非 soak tag），soak 30×30 全绿。
- **并发竞态与 inbox/TTL/租约存储耐久**（A 7 + B/C 9 + C-P2/E 9 项）：durable inbox 句柄、租约拒绝先于 live 注册（私有 CM 不悬挂、owner 义务归零）、drain defer 序关住 loop-exit→unbind、总线投递/退役路由静默面、退役归因盖信号 Lineage、`Spec.Origin` spawn 时不可变（数据竞争、resumed 谱系污染）、per-task 路由、run 级折叠豁免（任一 Active 全 run 保留）；隔离 rename 失败不穿透容量记账、跨重启恢复 TTL 退役带静默泄漏告警、spill 键释放排在重写落盘后恰好一次、journal 关闭幂等、分区发现/墓碑重放防线/扫段与列表失败 fail-loud。
- **rl 周边与 tool 护栏**（E-P2 4 + D 3 项）：`SwappableModel` 换回竞态以锁内新鲜 current 判定；trajectory 对 (nil,nil) 不起 nil-channel 协程（Close 死锁）且落错误记录；restart 脚本归档+截断移入交接窗；smuggle 判据双层化（词形+位置），`&&` 链与 URL query 误报清零、既有告警集（nohup 配 &/disown/重定向）不变。
- **工程化与对账门禁**（F 4 项 + scripts/codetools 两修）：comment-only 门禁按 git status 分类（删除侧硬拒、未跟踪纳入、`--` pathspec 与 R 状态行盲区）；CI 补 mod verify 与 soak 超时；tmux `RebindCallback` 换供 resume；`CreateSession` 有界重试（3 次×150ms，已存在即幂等）。①归一器（空白折叠/空行删除/行尾裁剪/别名致盲）不懂字面量边界→假阴，import/类型定义行尾注释→假阳（历史误诊实为 TypeSpec 槽未清）；归一止步字面量、注释剥离按槽位类型穷举，五批 A/B 复跑新增报项 0。②merge-check 三道静默通过改硬拒（rc=2）：重复 `--map`/`--explain` 取末值、零效豁免 `--explain`、表读不进按空表读作 intact。
- **wechat-bot 投递面两修**：①转世通报/SYSTEM_ALERT 借 "meditation" 章被 fail-closed 静默扣留 → 改专用章（`reincarnation`/`system_alert`）显式路由（B-fix）；②固定 5s 探测错过慢写保险链脚本（s67 通报缺席）→ 60s 轮询等 NOTICE（新鲜度门不变）。
- **文档规格与 CI 面**（docs/wiki, openspec/specs, .github/workflows）：裸坐标 `D5`/`F7` 19 处清除（定义在被 gitignore 的设计稿且同号三义）；wiki 变更名引用与"历史上曾有…已移除"叙述清除；`agent/task` 双墙注释改单一 TTL 陈述；主 specs 补立 `async-task-lifetime`、撤死需求（WAL 死条款 REMOVED、Purpose 回填）、README/wiki 对齐耐久定位；localfile 耐久张力记 `docs/storage-durability-positioning.md` 待裁；dev 直推漏 PR 门致 `0a31e46`（agent/compress 105 处重名不可编译）入库 → 触发分支补 `dev`。

（冻结期维持：治理/自进化闭环需在真实部署连续运行一个月后方启动下一轮功能迭代；本节仅收缺陷修复。）

## [v0.2.0] - 2026-09-14

### implementation-hardening（收尾加固；0.x 阶段含行为变化，部署前阅「迁移注意」）

- **安全收口**：RL HTTP API Bearer token（`TAGENT_RL_AUTH_TOKEN`；ServeHTTP 单点强制、无豁免）+ `ValidateListenAddr` loopback fail-closed；wechat-bot 无 token 只绑 127.0.0.1。**部署注意**：`:port` 全接口监听需设 token 或接受回环约束。
- **耐久收口**：LocalFileKV WAL/快照/目录三级 fsync（`memory.fsync` 默认开，关闭留降级告警）；冷分区启动发现（重启后未触碰分区纳入 TTL/容量/压实遗忘扫描）。
- **正确性收口**：RestoreTask 生命周期通道补全（修重启后 resume `close(nil)` panic，fail-before 回归）；Spawn/Resume/watch nil detector 防御；KeepRecentTasks 改每调用参数（消除共享字段竞态）；**StopLoop 终结化**——二次 StartLoop 报错（原返回已关通道、二次 Stop 必 panic）。
- **资源回收**：SwapExecutor/SwappableModel.Swap 旧 runner/model 延迟 Close（in-flight 门控）；lastEventKeys 封顶 4096。
- **配置健壮**：YAML/JSON 严格解析（未知字段启动报错并列名——**含未知键的旧配置将启动失败**）；agent 引用环构建期检测。
- **死代码二清**：TypeToolUse 幽灵抽象、modelref 死导出删除；IsTmuxAvailable 真 PATH 探测。
- **架构立法**：分层依赖方向断言测试；I2 投影完备性真实管线钉测；LEDGER 红色耦合台账；soak 连续性骨架（`-tags soak`）。
- **race 门禁扩面**：agent 核心包纳入 CI race（上游竞态经 `raceEnabled` 豁免挂账，LEDGER U2/U3；上游 issue 待报）。
- **文档对齐**：「事件永久入库」实述为「不可变入库 + 默认 TTL（3-30 天）可配永久」；治理关闭时评估输出声明信号不可用。

## [v0.1.0] - 2026-09-10（历史版本）

> 以下为 v0.1.0（tag f1beefa，evals backlog-final-closeout）至 v0.2.0 前的历史记录，随 v0.1.0 首发未含本段标题。

### Changed（设计返工，self-evolution-git-native 2026-09-07）

- **变更控制特性整体返工为 git 原生**（维护者裁定：原 bundle/发布道违反哲学四原则——文件即真源/复用 git/默认自迭代/信号建议式）：
  - **退役**：BundleStore、VersionedSource（prompt 遮蔽层）、ReleaseManager 状态机（Lane/Stage/审批门/ProtectedPrompts/预算 Gate）、refine propose/diff——文件回归唯一真源（mtime 热重载），改进 commit 带 `[self-improve]` 标记进 git。
  - **新增**：refine register/status/rollback（受控路径/结构化 commit/安全 revert 仅限改进标记）；GET /feedback/wait long-poll（AReaL）；improvement/evaluation 双轨台账；后验评估锚迁至登记 commit；劣化**只出建议**（P4，不 revert）。
  - **保留**：judge/guardrail/证据链（TurnCount=真实 turn 数）、feedback 因果边 join（版本章=最新 improvement sha）。

### Added

- **evals 组件级行为评估**：evals/ 一等目录（票据可召回率 suite/工具选择 suite/Bad Case 资产化——tests/README「静默存活多日」教训转回归）。
- **诊断快照消费面**：GET /diagnostics（DiagnosticsSnapshot，含 wal_quarantined——F3 隔离计数经装饰链可达）。
- **溢出票据取回指引**：票据与登记事件含「可 exec cat 取回」指引。
- **审批直投通道**：WithApprovalChannel option + example（审批直送微信，不经 agent 转述）。
- **混合语义召回（T-A）**：Embedder + InMemoryEngine（hybrid RRF/分区隔离/异步嵌入 worker）+ engineBridge（C6：IndexBuilder+Retriever+io.Closer，引擎仅在组合根）+ 向量 KV 跨重启 + recall hybrid 降级链（引擎错/零命中/分区全滤/全悬挂→关键词）+ TracedEmbedder（GenAI semconv span）。
- **统一可观测（T-B）**：turn root span（`tagent.turn`，noop provider 安全）+ trace_id/span_id 三投影互链（事件 Metadata + trajectory LLMCallRecord + OTel span）+ 异步 task span link（Origin trace 经 task_settled 回流）。
- **自进化（T-EVO/TC0，初版；退役与返工见 Changed）**：BundleStore、VersionedSource（prompt.Getter）、refine（propose/diff/status/rollback，无 activate）、ReleaseManager（DiffLaneRouter：模型/参数→慢道门后、提示词→快道后验；双回滚=MetricGuardrail + LLMJudgeEvaluator）、后验评估闭环（Evidence 从治理事件收集 canary 证据）。
- **治理（T-G）**：RiskClassifier（C5 四级纯函数）+ GovernanceGate（classify→critical 批准门→goal→预算闸→记账）+ GovernanceTool 装饰器 + BudgetManager（滑动窗口 epoch 防刷限）+ ApprovalManager（args_digest 绑定）+ DenialLedger（持久化）+ GoalRegistry。
- **可靠性（T-G）**：DegradationManager 五依赖全 LIVE（memory/disk/rustviking→ErrorTrackingStore、model→event_loop、mcp→mcp_call）+ ErrorTrackingStore（C2 最外层，D3 挂点补齐）+ mem_spill 兜底（StoreEvent 失败事件落 JSONL、按原 key 重放，at-least-once 延伸存储层）+ ReliableBus 溢出（channel 早于 spill、pending 背压）+ AnchorStore 冥想锚点持久化。
- **记忆策展（T-D）**：证据门控巩固（服务端 SHA1 指纹防伪造 + receipts 收据）+ MemoryDiagnostics 维度锚定诊断 + `memory_consolidate`/`memory_health` agent 工具。
- **RL 轨迹（rl）**：TrajectoryRecorder JSONL 流水（含 trace 锚点、final sync on close）。
- **CI**：GitHub Actions（build + vet + 全量 short 测试 + 新子系统 `-race`，全 mock 无需 key）。
- **工作根 `working_dir`**：file 工具 `base_dir` 与 exec cwd 共同基准（`ToolRef.properties` > `working_dir` > 进程 cwd），可经 `TAGENT_WORKING_DIR` 覆盖（部署时指向项目 clone 根）；空值=零变化。
- **裸机部署资产（examples/wechat-bot）**：`wizard.sh` 七步向导（依赖/密钥不回显/工作根/`.env` chmod 600/POSIX ACL/连通性/下一步）、`deploy/tagent-wechat.service`（非 root + `ProtectSystem=strict` + `ReadWritePaths` + `Restart=always` + SIGTERM + 资源上限）、`deploy/README.md`；`run.sh` 加 `build`/`systemd`。
- **治理审计来源归属**：`DenialRecord.AgentName` → 事件 `metadata["agent"]`（omitempty）→ `rebuildFromStore` 回读；多子 agent 共享 Ledger 时按来源区分。

### Changed

- **memory 包按职责拆分**：语义引擎适配器（bridge/hybrid RRF/embedder/诊断）入 `memory/engine/`，KV 后端（localfile/rustviking）入 `memory/kv/`；`MemoryEngine`（C6 解耦缝）与 `KVStore` 契约仍居核心包（`memory/engine.go`、`memory/kv.go`）。
- **wechat-bot 启用全平台子系统**：治理闸（`enforcement=warn` + per-agent 预算 + critical 恒审批）、自进化（refine 发布道 + `protected_prompts` 慢道）、可靠性（bus/mem spill + 冥想锚点 + 五依赖退化）、记忆引擎（zhipu embedding-3，512 维；tagent/knowledge/recall 三 agent 共享）；`log_level` debug→info。
- **`tagent.yaml` 编排精简**（547 → 249 行，语义零变化，归一化等价测试逐字段验证）：`provider`/`model` 默认继承 + `x-anchors` 消除 engine/monitor 重复 + 注释外移 wiki。
- **文档代码交叉印证修订**：清除 wiki 39 处源码行号；`prompt-architecture.md` 修 16 处子节编号偏移、表头列数与 fallback 分支；`plugin-architecture.md` Runner 装配按当前实现重写；`agent-architecture.md` `runEventLoop` 伪码对齐；`memory-architecture.md` 与两份 README 管线三→两条；README_EN 同步 2026-09 架构（Go 1.24）；`docs/config-migration.md` 重写（经 `LoadConfig` 验证）；`tests/README.md` 补清单。

### Fixed

- F1、F2、F3、F4 四条历史缺陷：`FullEvent.Metadata` 生产未填充（双路径盖章）、RunFlow 内 outputCh 发送无限阻塞（2s 宽限→落盘）、replayWAL 中间坏行致启动失败（跳过坏行+计数上报）、`DefaultConfig()` 的 `id:"action"` 与注册表 `"exec"` 不匹配（TestDefaultConfigBuildable 守漂移）。
- 四轮 gate-3 CodeReview 修复（含发布道与治理账本）：事件时序倒置、critical 无批准通道绕过、judge 缺 score 误回滚、canary ctx 取消假通过、后验评估 Limit+asc 静默失效、file 后端同 path 多实例（因果链断链 + 双 Compactor 覆盖）、DegradationManager 计数塌缩、knowledge 吞存储错误等（详见 `openspec/changes/LEDGER.md`）；`releases.jsonl` 历史持久化 + active 基线 seed（修回滚恒被拒、`InitBaseline` 崩溃窗）；子 agent 审计复用 entry 持久 Ledger；无 active 基线时 `Submit` 拒绝（防孤儿 draft 滞留）；`DenialLedger.Record` 锁内快照 store/partitionID；审批重扫节流可注入时钟；删语义与 `BindStore` 相反的死代码 `BindLedger`。
- 文档失真修正：README 退化追踪条件误记「随 `governance.dir`」，实为 `reliability.degradation_enabled`（`mem_spill_dir` 亦依它）；`docs/config-migration.md` 示例字段全失效（`tagent:`/`name`/`type`/`system_prompt_file`/`memory.data_dir`）；`prompt-architecture.md` 误用他项目名；`plugin-architecture.md` 引用已删源文件；`rustviking-client` 构造签名不符（写作单 `cfg` 参数，实为两串）；`wiki-code-sync` 以行数清单为契约已重写为持久规则。

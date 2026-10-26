# Changelog

本项目所有显著变更记录于此。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。

## [Unreleased]

（当前无未发布变更。)

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

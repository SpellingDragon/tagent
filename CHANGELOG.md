# Changelog

本项目所有显著变更记录于此。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### Breaking Changes（merge-review-remediation：dev→main 合并前评审修订，45 项发现全量落地；pre-release 姿态，无迁移承诺）

- **谱系投递白名单改正向集合**（event.DeliverableLineage）：宿主可投递谱系（user/task/reincarnation/system_alert/meditation）之外一律 fail-closed 扣留，负名单时代漏配谱系（task-unstamped）与未知未来值不再可能被投递；旧前缀启发式退役。
- **结算折叠资格唯一来源改为结构化标记**（settle_notice，D10）：正文形状启发式退役，无标记事件（含伪造体与标记前的旧事件）逐字保留；纯结构模式（无库可验）下通知一律按内部处理。
- **LocalFileKV 快照按分区分片**（kv-<pid>.json / kv-global.json）：Sync 只重写脏桶，单分区提交成本与全库规模解耦；旧单文件 kv.json 不迁移、存在即忽略（报告一次），冷启动重建。
- **诊断面死字段删除**：WalQuarantined 四层链（config/引擎桥/诊断/HTTP）与对应字段整体消失，消费方按 absent 处理。
- **`memory.fsync` 旋钮与 fsync 写面整体删除**：诚实 flush-only 语义——Sync 屏障是原子 tmp+rename 快照提交，不声称抗掉电；配置含 `fsync:` 键将因严格装载拒启动（KnownFields）。

### Added

- 测试面 47 个新用例：worktree 隔离的门禁矩阵、活 tmux 全路径 rebind、分区快照脏桶隔离/跨进程读回/旧格式忽略、回收竞态终局语义、nil-channel 三分判据等。
- **入站意图声明**（`POST /task`，openspec: delivery-intent-and-receipts P1）：请求体可选 `trigger_source`，受信集成（如 mail-poller 代表人类通信者）声明 `user` 血统；值域仅 `user`、无 auth 拒 `declaration_requires_auth`、缺信封能力 501 拒收，一律 fail-closed；声明入事件 Metadata 而 `Source` 保留通道标签，投递白名单语义不变。缺省行为逐位同于改前。
- **投递终态统一回执**（wechat-bot 分发层，K2）：已送达不回执；send 失败 ERROR；未知/未声明血统消化、冥想血统含交付特征扣留、error、无目标四类 WARN 回执；冥想纯叙事维持契约内静默。回执以 `delivery_receipt` 血统入持久总线（转生后仍在账、自身输出静默、不武装冥想新颖门、与用户消息同批可被当场补投、不递归）——两起“感知成功/发送未发生”事故的宿主面闭环。
- **结算血统一级键**（`settle_trigger_source`，K3）：事实链持久化时把结算事件自带的派生血统提升为一级可读键（`source_snapshot` 无损快照保留），与回合级 `trigger_source` 并存可对账，投递争议不再依赖解码知识取证。

### Fixed

- **空回合血统写入空串一级键**（`buildBusFact`，openspec: delivery-intent-and-receipts K3②）：`trigger_source` 原无条件写 `cm.triggerSource`，回合血统为空时事实链留下“键存在但为空”的一级键，被读方误判为“未盖章”（email-inbound 事故当时取证失败的现场形态）；现为空则不写，与 `buildEventAttributes` 既有保护同形。
- **压实跨折叠覆盖丢历史**（memory，收口阶段 soak 回归暴露，基线同形红）：同日第二次 L1→L2（或同周 L2→L3）把选定窗从 seq=0 写入取整目标窗时不检查目标窗既存段，逐键覆盖前一批历史（盘上字节消失、meta 计数失真、idx 悬空指向他人内容）；fresh 进程重启召回为空即此断裂（soak 连续性 promise 自入档以来从未真通过）。现将既存目标窗并入 merge 读取源，merge 按 EventKey 去重封死 crash-retry 交叠；回归测例入 CI（非 soak tag），soak 30×30 全绿。
- **agent 核心运行时**（A 组 7 项）：构造失败路径的 durable inbox 句柄关闭；租约拒绝先于 live 注册（私有 CM 不悬挂、owner 义务可归零）；终态 drain 的 defer 序关住 loop-exit 到 unbind 的窗口；事件总线投递与退役路由的静默面清零。
- **谱系与结算链路**（B/C 组 9 项）：退役归因盖在信号 Lineage 上、Spec.Origin 保持 spawn 时不可变（消除与无锁读者的数据竞争与 resumed 任务谱系永久污染）；有归属退役绕开批折叠走 per-task 路由，父循环投递记账屏障可达静默退出；run 级折叠豁免（任一 Active 成员整 run 保留）。
- **inbox/TTL/租约**（C-P2/E 组 9 项）：隔离 rename 失败不穿透容量记账；跨重启恢复任务 TTL 退役携带静默泄漏告警（单事件承载）；spill 键释放严格排在重写落盘之后恰好一次；journal 关闭幂等短路。
- **存储耐久与诊断**（E 组）：分区发现、墓碑保留重放防线、扫段/列表失败一律 fail-loud（对"实际删了什么"诚实）。
- **rl 周边**（E-P2 组 4 项）：SwappableModel 换回竞态以锁内新鲜 current 判定；trajectory 对 (nil,nil) 返回不再起 nil-channel 转发协程（Close 死锁堵住）且落错误记录；restart 脚本的归档+截断移入交接窗。
- **tool 护栏**（D 组 3 项）：smuggle 告警判据双层化（词形+位置），`&&` 链与 URL query 误报清零而既有告警集（nohup 配 &/disown/重定向收尾）不变。
- **工程化**（F 组 4 项）：comment-only 门禁以 git status 分类重写（删除侧硬拒、未跟踪纳入、`--` pathspec 盲区堵死，另修 R 状态行取旧路径的盲区）；CI 补 mod verify 与 soak 超时；tmux 监控 RebindCallback 供跨重启 resume 换供；tmux CreateSession 改有界重试（3 次×150ms 退避，重试中发现会话已存在则幂等收敛为成功）——所有创建调用方的瞬态失败语义变化。
- **文档卫生**（G 组）：主 specs 与码面背离清零（无锚恢复不静默截断条款对齐、WAL 死条款 REMOVED）、Purpose 回填、旧版整份残留删除、绝对路径脱敏、README/wiki 对齐耐久定位。
- **投递门禁吞没宿主通报**（wechat-bot）：转世通报与 SYSTEM_ALERT 曾借用 "meditation" 章——fail-closed 门禁上线后其输出会被静默扣留；现改专用章（`reincarnation`/`system_alert`）并在门禁显式路由投递（B-fix）。
- **转世通报时序竞态**（wechat-bot）：固定 5s 探测对慢写的保险链脚本静默错过（s67 通报缺席实证）；改为 60s 轮询等待 NOTICE 出现（新鲜度门不变）。
- **等价对账仪器吞字面量内容**（scripts/codetools）：归一器（空白折叠／空行删除／行尾裁剪／别名致盲）不知字符串字面量边界，会把被改动的测试固件读成未改动——假阴且不可见；类型定义与 import 项的行尾注释未清则把纯注释修改误报为正文变化（假阳）。归一一律止步于字面量、注释剥离改按**槽位类型穷举**；严格化后对历史五批做 A/B 复跑，新增报项 0，唯一变化是一处历史误诊被纠正（"空白敏感"实为 TypeSpec 槽未清）。
- **merge-check 三道静默通过收口为硬拒**（scripts/codetools）：重复传 `--map`/`--explain` 原静默取末值、丢弃前一张表（被丢的改名会反噬成批次违规）；登记了却在本批毫无豁免效力的 `--explain` 条目原样通过（不压制任何差异的豁免正是掩盖映射表错误的面）；表文件读不进时 warn 后按空表继续、干净包直接读作 intact。三者现均 rc=2 拒绝并指明原因——豁免只在**实际压下**一次本应报出的差异时才算生效，未传旗标与传了张读不进的表是两种调用。
- **注释与规格背离码面现实**（docs/wiki, openspec/specs）：裸坐标 `D5`/`F7` 19 处清除（其唯一"定义"在被 gitignore 的设计稿且同号三义、引号形式引文在出处零命中）；wiki 三行变更名引用与"历史上曾有…已移除"过程叙述清除；`agent/task` 四处注释仍在描述已删除的双墙、与统一 TTL 契约正面矛盾，改为单一 TTL 陈述；主 specs 按现行码面补立 `async-task-lifetime` 能力并撤除符号已消失的死需求；localfile 耐久定位的两面张力另记 `docs/storage-durability-positioning.md` 待裁。
- **CI 漏听 dev 分支**（.github/workflows）：dev 直推不经 PR 门，`0a31e46`（agent/compress 105 处重名不可编译）正是由此入库、后续一切对账读的都是未验证基线；触发分支补 `dev`。

（冻结期维持：治理/自进化闭环需在真实部署连续运行一个月后方启动下一轮功能迭代；本节仅收缺陷修复。）

## [v0.2.0] - 2026-09-14

### implementation-hardening（收尾加固；0.x 阶段含行为变化，部署前阅「迁移注意」）

- **安全收口**：RL HTTP API Bearer token 认证（`TAGENT_RL_AUTH_TOKEN`；ServeHTTP 单点强制、全端点无豁免）+ `ValidateListenAddr` loopback fail-closed（无 token 非 loopback 拒绝监听）；wechat-bot 未设 token 自动仅绑 127.0.0.1。**部署注意**：原 `:port` 全接口监听的部署需设 token 或接受回环约束。
- **耐久收口**：LocalFileKV WAL/快照/目录三级 fsync（`memory.fsync` 默认开，关闭留降级告警）——已确认写入自此抗掉电；冷分区启动发现（重启后未触碰分区纳入 TTL/容量/压实遗忘扫描）。
- **正确性收口**：RestoreTask 生命周期通道补全（修复重启后 resume `close(nil)` panic，fail-before 回归）；Spawn/Resume/watch nil detector 防御；KeepRecentTasks 改每调用参数（消除共享字段竞态）；**StopLoop 终结化**——二次 StartLoop 显式报错（原实现重启返回已关通道且二次 Stop 必 panic；重启语义=新建 agent 实例）。
- **资源回收**：SwapExecutor/SwappableModel.Swap 换下的旧 runner/model 延迟 Close（in-flight 计数门控）；lastEventKeys 封顶 4096。
- **配置健壮**：YAML/JSON 严格解析（未知字段启动报错并列名——**含未知键的旧配置将启动失败，请迁移**）；agent 引用环构建期检测。
- **死代码二清**：TypeToolUse 幽灵抽象、modelref 死导出删除；IsTmuxAvailable 真 PATH 探测。
- **架构立法**：分层依赖方向断言测试；上游内部行为假设钉（I2 投影完备性真实管线钉测）；LEDGER 红色耦合台账；soak 连续性测试骨架（`-tags soak`，CI 手动触发）。
- **race 门禁扩面**：agent 核心包纳入 CI race（上游内部竞态经 `raceEnabled` 豁免挂账，LEDGER U2/U3；上游 issue 待报）。
- **文档对齐**：「事件永久入库」实述为「不可变入库 + 默认类型 TTL（3-30 天）可配永久」；治理关闭时评估输出显式声明信号不可用。

## [v0.1.0] - 2026-09-10（历史版本）

> 以下为 v0.1.0（tag f1beefa，evals backlog-final-closeout）至 v0.2.0 前的历史记录，随 v0.1.0 首发未含本段标题。

### Changed（设计返工，self-evolution-git-native 2026-09-07）

- **变更控制特性整体返工为 git 原生**（维护者裁定：原 bundle/发布道设计违反哲学四原则——文件即真源/复用 git/默认自迭代/信号建议式）：
  - **退役**：BundleStore（不可变快照）、VersionedSource（prompt 遮蔽层）、ReleaseManager 发布状态机（Lane/Stage/审批门/ProtectedPrompts/预算 Gate）、refine propose/diff——文件回归唯一真源（mtime 热重载直生效），改进 commit 以 `[self-improve]` 标记进 git。
  - **新增**：refine register/status/rollback 三 op（受控路径约束/结构化 commit/安全 revert 仅限改进标记）；GET /feedback/wait long-poll（AReaL 拉取）；improvement/evaluation 事件双轨台账；后验评估锚迁移至登记 commit 时刻；劣化**只出建议**（P4，框架永不动手 revert）。
  - **保留**：judge/guardrail/证据链（口径修正：TurnCount=真实 turn 数，原全事件数稀释判据可达性）；feedback 因果边 join（版本章=最新 improvement sha，键名兼容）。

### Added

- **evals 组件级行为评估**：evals/ 一等目录（票据可召回率 suite/工具选择 suite/Bad Case 资产化——tests/README「静默存活多日」教训转回归）。
- **诊断快照消费面**：GET /diagnostics（DiagnosticsSnapshot JSON，含 wal_quarantined——F3 隔离计数经装饰链可达）。
- **溢出票据取回指引**：票据与登记事件均含「可 exec cat 取回」行动指引（agent 侧可恢复溢出全文）。
- **审批直投通道**：WithApprovalChannel option + example 装配（审批请求不经 agent 转述，直送微信）。

- **混合语义召回（T-A）**：Embedder + InMemoryEngine（hybrid RRF 融合 / 分区隔离 / 异步嵌入
  worker）+ engineBridge 解耦缝（契约 C6：IndexBuilder+Retriever+io.Closer，引擎仅在组合根出现）
  + 向量 KV 持久化跨重启重建 + recall hybrid 逐跳降级链（引擎错/零命中/分区全滤/全悬挂→关键词）
  + TracedEmbedder（GenAI semconv span）。
- **统一可观测（T-B）**：turn root span（`tagent.turn`，noop provider 安全）+ trace_id/span_id
  三投影互链（事件 Metadata + trajectory LLMCallRecord + OTel span 树）+ 异步任务 task span
  link（Origin trace 锚点经 task_settled 事件回流，跨 turn 关联不侵入 task 包）。
- **自进化（T-EVO/TC0）**：BundleStore（不可变内容寻址 + 原子 active 切换）+ VersionedSource
  （实现 prompt.Getter，回合边界生效）+ refine 工具（propose/diff/status/rollback，无 activate，
  agent 无直接激活权）+ ReleaseManager 风险分级发布道（DiffLaneRouter：模型/参数→慢道门后、
  提示词→快道后验；双回滚 = MetricGuardrail 确定性闸 + LLMJudgeEvaluator 模型决策回滚）+
  后验评估闭环（Evidence 从治理事件收集 canary 证据）。
- **治理（T-G）**：RiskClassifier（契约 C5：纯函数四级分级）+ GovernanceGate 决策管线（classify
  →critical 批准门→goal 检查→预算闸→记账）+ GovernanceTool 装饰器 + BudgetManager（滑动窗口
  epoch 防重启刷限）+ ApprovalManager（异步文件通道 args_digest 绑定）+ DenialLedger 治理账本
  （持久化）+ GoalRegistry。
- **可靠性（T-G）**：DegradationManager 五依赖退化状态机全 LIVE（memory/disk/rustviking 经
  ErrorTrackingStore 存储栈、model 经 event_loop、mcp 经 mcp_call）+ ErrorTrackingStore（契约
  C2 最外层，报告 D3 设计的挂点补齐）+ mem_spill 退化兜底（StoreEvent 失败事件落 JSONL，恢复
  按原 key 重放，at-least-once 延伸存储层）+ ReliableBus 磁盘溢出（channel 恒早于 spill 全序 +
  pending 背压）+ AnchorStore 冥想锚点持久化。
- **记忆策展（T-D）**：证据门控巩固（服务端 SHA1 指纹防伪造 + receipts 收据）+ MemoryDiagnostics
  维度锚定诊断 + `memory_consolidate`/`memory_health` agent 工具。
- **RL 轨迹（rl）**：TrajectoryRecorder JSONL 流水（含 trace 锚点、final sync on close）。
- **CI**：GitHub Actions（build + vet + 全量 short 测试 + 新子系统 `-race`，全 mock 无需 key）。
- **框架级 agent 工作根 `working_dir`**：file 工具 `base_dir` 与 exec 命令 cwd 的共同基准
  （`ToolRef.properties` > `working_dir` > 进程 cwd），二者恒一致以保持模型单一文件系统视图；
  可经 `TAGENT_WORKING_DIR` 环境变量覆盖（部署时指向项目 clone 根而无需改 YAML）。空值 = 现状零变化。
- **裸机部署资产（examples/wechat-bot）**：`wizard.sh` 七步初始化向导（依赖检查 / 密钥不回显收集 /
  工作根引导 / 生成 `.env` chmod 600 / 工作区 POSIX ACL 授权 / 连通性验证 / 下一步），
  `deploy/tagent-wechat.service` systemd 单元（非 root + `ProtectSystem=strict` + `ReadWritePaths`
  白名单 + `Restart=always` + SIGTERM 优雅关闭 + 资源上限）与 `deploy/README.md` 部署指南；
  `run.sh` 新增 `build` / `systemd` 子命令。
- **治理审计来源归属**：`DenialRecord.AgentName` → 事件 `metadata["agent"]`（omitempty）→
  `rebuildFromStore` 回读；多子 agent 共享同一 Ledger 时治理事件可按来源 agent 区分。

### Changed

- **memory 包按职责拆分**：语义引擎适配器（bridge / hybrid RRF / embedder / 诊断）迁入 `memory/engine/`，
  KV 存储后端（localfile / rustviking）迁入 `memory/kv/`；`MemoryEngine`（C6 解耦缝）与 `KVStore`
  契约仍居核心包（`memory/engine.go`、`memory/kv.go`，后者附「接入新引擎/后端」两路径指南）。
  新增实现只进对应子包，核心存储/压缩/事件代码不需改动。
- **wechat-bot example 启用全平台子系统**：治理闸（`enforcement=warn` 记账放行 + per-agent 预算 +
  critical 恒审批）、自进化（refine 发布道 + `protected_prompts` 走慢道）、常驻可靠性
  （bus/mem spill + 冥想锚点 + 五依赖退化状态机）、记忆引擎（zhipu embedding-3，512 维，
  tagent/knowledge/recall 三 agent 共享同一引擎实例）；`log_level` 由 debug 改 info（远端不落 LLM 明文）。
- **example `tagent.yaml` 编排精简**（547 → 249 行，语义零变化，经归一化等价测试逐字段验证）：
  全局 `provider`/`model` 默认继承 + `x-anchors` 共享锚点消除 engine/monitor 重复 + 注释外移到 wiki。
- **文档全量代码交叉印证修订**：清除 wiki 中 39 处已腐化的源码行号标注（撰写约定禁列行号）；
  修正 `prompt-architecture.md` 16 处子节编号偏移、文件清单表头列数破损、代码块缺失的 fallback 分支
  与错误的项目名示例；`plugin-architecture.md` 的 Runner 装配代码块重写为当前实现（原文引用已不存在的
  文件名）；`agent-architecture.md` 的 `runEventLoop` 伪码对齐实际签名与控制流；`memory-architecture.md`
  与两份 README 的事件管线数由三条更正为两条现役管线；README_EN 同步 2026-09 架构（六项新特性、
  环境依赖、部署路径、模块表、配置表、平台子系统表、Go 1.24）；`docs/config-migration.md` 重写
  （原文示例字段全部失效，示例经真实 `LoadConfig` 验证）；`tests/README.md` 补齐测试文件清单。

### Fixed

- F1：`FullEvent.Metadata` 在生产代码从未填充（归因地基双路径盖章修复）。
- F2：RunFlow 内 outputCh 发送无限阻塞（2s 宽限→落盘）。
- F3：replayWAL 对中间坏行直接报错导致启动失败（跳过坏行+计数上报）。
- F4：`DefaultConfig()` 的 `id:"action"` 与注册表 `"exec"` 不匹配（TestDefaultConfigBuildable
  永久守护配置-注册表漂移）。
- 四轮 gate-3 CodeReview 修复：事件时序倒置（回复路由错误会话）、critical 无批准通道绕过、
  judge 缺 score 零值误回滚、canary ctx 取消假通过、后验评估 Limit+asc 静默失效、file 后端
  同 path 多实例（跨 agent 因果链断链 + 双 Compactor 并发覆盖）、DegradationManager 计数
  语义塌缩/无恢复路径、knowledge 吞存储错误等（详见 `openspec/changes/LEDGER.md`）。
- 发布道与治理账本的一批修复：发布历史持久化到 `releases.jsonl` 并对当前 active 基线补 seed
  （rollback 白名单跨重启有效，修「回滚到基线恒被拒」与 `InitBaseline` 崩溃窗口）；子 agent 治理
  审计复用 entry 持久 Ledger（不再是重启即失的内存账本）；无 active 基线时 `Submit` 直接拒绝
  （防孤儿 draft 滞留 active 且无回滚锚点）；`DenialLedger.Record` 锁内快照 store/partitionID
  （消除与延迟绑定的数据竞争）；审批重扫节流间隔可注入时钟（消除 CI 重载下的假失败）；
  删除语义与 `BindStore` 相反的死代码 `BindLedger`。
- 文档失真修正：README 把退化追踪的启用条件误记为「随 `governance.dir` 启用」，实为
  `reliability.degradation_enabled` 独立开关（`mem_spill_dir` 亦仅在它为真时接线）；
  `docs/config-migration.md` 全文示例字段失效（`tagent:` 根键、`name`/`type`、`system_prompt_file`、
  `memory.data_dir` 等均已不存在）；`prompt-architecture.md` 示例误用他项目名；
  `plugin-architecture.md` 引用已重命名删除的源文件；`rustviking-client` 规格的构造函数签名
  与实现不符（写作单 `cfg` 参数，实为两个字符串参数）；`wiki-code-sync` 规格以一次性行数修正清单
  为契约、且要求与 wiki 撰写约定（禁列行数）冲突，已重写为持久校验规则。

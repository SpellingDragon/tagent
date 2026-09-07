# Changelog

本项目所有显著变更记录于此。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。首个 tag（v0.1.0）打出前的变更统一记于
`[Unreleased]`（按 roadmap 裁决 C11，tag 时点待定）。

## [Unreleased]

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

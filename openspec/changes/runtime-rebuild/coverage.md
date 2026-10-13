# runtime-rebuild · 主规格覆盖映射（coverage）

> 角色：本文件把现存 **120** 个主规格（`openspec/specs/<cap>/spec.md`）逐一映射到新计划的 **10 个二级新域（00–09）与 65 个新叶任务**。
> 依据：只读计划 `运行时_重设_实施计划_304e54c3.md`（§6 域布局、§7 叶任务清单）。
> 边界：只读 + 新增本 md；**不写 spec**、不改主规格、不改旧 change、不生成产品。
> 收口状态：10 域四件套与本表已由串行集成（批次 2）收口；delta 实际放置以各域 `specs/` 为准，**主消费任务**与**唯一 delta 写者**的分列登记见 §7.1。

## 0. 方法与诚实声明

- 复核粒度 = **能力级**：逐文件读 `## Purpose` + 全部 `### Requirement:` 标题（并在疑点处下钻正文）。**未**对全部 **601** 条 Requirement 逐条做语义判定（1217 个 Scenario 亦然）；故本表不声称"601 条已审完"。
- 处理判据只看**新 goal 是否真实消费**该能力，**不因 source 文件存在就复活**其旧机制、旧类名或旧包结构（计划 §2「保留的用户要求 8」「不做」）。
- **旧 names 不是全部恢复**：新计划的类名/包结构/算法不默认沿用历史；主规格标题命中 ≠ 语义适用。
- 「行为需修改」若目标域的 delta 尚未由该域写者生成，本表写 **「需对齐 <域> delta」**，不做机械继承改写。
- 主表 `新域.task` 列为该能力的**主责任务（消费/验收）**；该能力的**唯一 delta 写者**以各域 `specs/` 实际放置为准。二者不同域时在 §7.1 **单列登记**（主消费任务与唯一 delta 写者分开登记/引用），不混同、不迁文件求表面一致。
- **不自行写 spec**：新增独立能力 `runtime-inspection` 已由 **05-inspection** 建立（05.1/05.2 只读总图/快照/游标/订阅）；本表只给域位与消费者，不给条款。

## 1. 集合校验（可复跑）

- 本表行数 = **120**，与 `ls openspec/specs | wc -l` 相等；每行 `路径` 唯一、且 `openspec/specs/<cap>/spec.md` 存在（集合可核）。
- 每个主规格目录恰好 1 个 `spec.md`（无遗漏目录）。
- task 编号取自同目录并行产出的 `task-graph.json`（**65** 个 id：00×6 / 01×6 / 02×9 / 03×8 / 04×9 / 05×4 / 06×8 / 07×4 / 08×5 / 09×6），本表所引 id 全部落在图内。

## 2. 处置图例

| 代号 | 含义 |
|---|---|
| `R` 保留能力待实现 | 能力保留，按新计划从零实现；旧文本仅背景，**非继承契约** |
| `M` 行为需修改 | 行为须按新 goal 调整；旧文本仅参考，**最终以该域 delta 为准**（未生成则记「需对齐 <域> delta」） |
| `H` 历史参考 | 机制已退役/被替代，仅作设计记录，**不复活** |
| `D` 设计待核 | 是否保留/语义未定，**保持 design-blocked，不猜** |

## 3. 新域 ↔ 叶任务（来自新 65 图；本表 task 编号只从此取）

- `00-foundation`：00.1 残件/保护面盘点 · 00.2 120 主规→新任务映射 · 00.3 G1 共享合同+tRPC 复用矩阵 · 00.4 架构/场景/观测 golden · 00.5 检查器+负控 · 00.6 CI/lint/Go 约束迁移
- `01-state-binding`：01.1 config 严格解析 · 01.2 state agent/session 身份+授权 · 01.3 mailbox 记账 · 01.4 组合根活绑定 · 01.5 组织发布/换代/退役 · 01.6 prompt/skill 热读+结构变更
- `02-execution`：02.1 event 身份+task 提交端口 · 02.2 task 生命周期 · 02.3 原生 wrapper/审批/Receipt/后台结果 · 02.4 model 单一终局协议 · 02.5 根包装配驱动 Agent · 02.6 插件相位 · 02.7 子委派 · 02.8 Handoff · 02.9 关闭/排空
- `03-history`：03.1 原文引用/隔离/召回 · 03.2 localfile/file 后端+rustviking · 03.3 持久提交 · 03.4 投影/预算装配 · 03.5 保留/墓碑/回收 · 03.6 workspace 副本 · 03.7 副本清理 · 03.8 大结果外置+重启恢复
- `04-capabilities`：04.1 原生 file/skill/FunctionTool 注册 · 04.2 exec/tmux 资源租约 · 04.3 MCP · 04.4 A2A+send · 04.5 warn/strict 治理 · 04.6 预算 epoch/拒绝账本/恢复 · 04.7 授权检索 · 04.8 价值评估/整理 · 04.9 召回质量报告
- `05-inspection`：05.1 只读总图 · 05.2 授权过滤/快照/分页/订阅 · 05.3 提交后 OTel/审计 · 05.4 阻塞解释
- `06-learning`：06.1 版本化记录生产点 · 06.2 请求快照/脱敏 · 06.3 有界队列/封账 · 06.4 授权导出/reward/manifest · 06.5 SFT 清洗 · 06.6 真实 SFT 消费者 · 06.7 RL HTTP 鉴权 · 06.8 模型替换
- `07-evolution`：07.1 反思范围/水位 · 07.2 refine register/status/rollback · 07.3 激活证据窗 · 07.4 结构评估 Eval/Benchmark/ApplyGate
- `08-host`：08.1 公开 SDK+CLI · 08.2 微信 module 迁移 · 08.3 文件收发边界 · 08.4 接 Query/反思/训练诊断 · 08.5 真实微信验证
- `09-acceptance`：09.1 scenario runner+golden · 09.2 旧数据导入迁移 · 09.3 两 module build/vet/test/lint+race+负控 · 09.4 soak/崩溃恢复 · 09.5 真实资源验收 · 09.6 主规格 delta/文档/API 闭合

## 4. 主表：120 能力 → 新域/任务/处置

> `路径` 均为 `openspec/specs/<cap>/spec.md`。`下游 consumer` 为**可选**，非主 writer。

| 路径（cap） | 能力名 | 新域.task | 处理 | 下游 consumer（可选） | 理由（一句，绑新 goal 真实消费方） |
|---|---|---|---|---|---|
| `action-tool-config` | action 工具配置面 | 04.2 | M | — | tmux exec 工具构造期消费 monitor 超参；compress 子配置段已退役 |
| `adaptive-poll-scheduling` | 自适应轮询调度 | 02.2 | M | — | task 受理/detach/settle 消费 poll schedule，sync_wait 旋钮移除 |
| `adaptive-segment-size` | 段级容量封段 | 03.2 | D | — | 段级封段未实现；消费方=存储分段/lifecycle，需设计核验，不猜 |
| `agent-thinking-activation` | 思考激活行为 | 02.4 | R | 01.4 | 模型请求装配消费 thinking 触发字段，配了必须真产生推理 |
| `approval-channels` | 审批消息流 | 04.5 | R | 08.2 | 审批门消费 CLI/微信通道；主写 04.5 |
| `architecture-guardrails` | 架构护栏 | 00.4 | R | 00.5 | 分层方向/钉测断言由 00.4 场景 golden+00.5 检查器消费 |
| `architecture-review-standards` | 架构审查标准 | 00.5 | H | — | 审查清单服务新检查器；多机制已重构，勿据旧清单逐条复活 |
| `async-task-execution` | 异步任务执行 | 02.2 | M | 03.3 | task 受理/spawn/detach/settle 消费方=task 层 |
| `async-task-lifetime` | 异步任务寿命 | 02.2 | M | 05.1 | 统一 TTL 唯一来源消费方=task 层 |
| `async-tool-event-fix` | 异步工具事件修复 | 02.3 | M | 03.4 | 事件角色归一/结算分类消费方=受理与投影 |
| `causal-chain-isolation` | 因果链隔离 | 03.1 | R | 04.8 | session 级因果隔离消费方=memory 原文/召回 |
| `code-documentation` | 代码文档双层真源 | 00.6 | R | — | 注释政策=gofmt/godoc CI 门消费 |
| `cognitive-asset-guard` | 认知资产防线 | 04.5 | D | — | 写入审批/漂移审计；脚手架拆除条件未定，保持 design-blocked |
| `committed-event-attribution` | 已提交事实归因 | 02.1 | R | 05.1 | 提交成功才可召回票据；消费方=提交端口与 Query 只读面 |
| `compaction-scheduler` | 压实调度 | 03.2 | M | 03.5 | 分段压实调度消费方=存储生命周期 |
| `compress-event-enrichment` | 压缩事件富集 | 03.4 | H | — | 富集注记已被骨架卡片行替代，勿复活；投影为消费方 |
| `compress-quality-fix` | 压缩质量修复 | 03.4 | M | — | 引用解析/角色填充消费方=投影装配（需对齐 03 delta） |
| `compression-user-message` | 压缩保留未完成驱动 | 03.4 | M | — | 保留最后 user message 消费方=投影（需对齐 03 delta） |
| `concurrency-hardening` | 并发加固 | 02.5 | R | 04.2 | 并发安全消费方=agent/exec 状态访问 |
| `config-hot-reload` | 配置热重载 | 01.5 | M | 02.5 | 热更发布/回滚消费方=state 发布代（需对齐 01 delta） |
| `config-state-twinship` | 配置-state 孪生 | 01.4 | R | 02.5 | state 持有生效 system prompt；消费方=请求装配 |
| `consolidation-triggers` | 巩固触发 | 04.8 | R | — | 巩固建议消费方=memory 固化 |
| `context-scoping` | 上下文还原/范围 | 01.2 | M | 02.5 | resume 上下文还原+因果链挂载；授权范围消费方=state |
| `conversation-self-heal` | 会话投影自愈 | 03.4 | R | — | 投影按 EventKey 幂等追加，消费方=投影 |
| `cross-session-delivery` | 跨 session 投递 | 04.4 | M | 07.1 | 投递白名单/寻址消费方=投递器（需对齐 04 delta） |
| `decision-capture` | 决策快照采集 | 06.1 | R | 06.3 | 决策快照=版本化记录生产点 |
| `degradation-behaviors` | 依赖退化响应 | 04.6 | R | — | 退化响应默认关；消费方=治理/资源恢复 |
| `delivery-dispatch-receipts` | 投递回执 | 04.4 | R | 05.1 | 投递回执消费方=投递器/Query |
| `deterministic-compress-level` | 确定性压缩分级 | 03.4 | R | — | 段龄纯函数消费方=压缩管线 |
| `error-handling-consistency` | 错误处理一致性 | 00.6 | M | — | log 统一/错误必检=CI lint 门消费 |
| `evals-harness` | 组件级评估 harness | 09.1 | M | 09.5 | 票据可召回/工具选择评估消费方=验收 runner |
| `event-compaction` | 事件压实 | 03.4 | M | — | 段边界/双路径退役消费方=压缩管线（需对齐 03 delta） |
| `event-key-visibility` | 事件键可见性 | 02.5 | M | 03.4 | event key 注入消费方=请求装配 |
| `event-lifecycle` | 事件生命周期 | 03.5 | M | — | TTL/容量淘汰消费方=存储生命周期 |
| `event-metadata-contract` | 事件元数据契约 | 02.1 | R | 06.1 | MetaKey/EventKey canonical 消费方=事件身份 |
| `event-relation-store` | 事件关系库 | 03.1 | M | 04.8 | 关系图持久化消费方=recall/Query |
| `event-segment-store` | 事件分段存储 | 03.2 | M | — | 分段存储消费方=存储后端（需对齐 03 delta） |
| `event-sourced-projection` | 事件溯源投影 | 03.4 | R | 05.1 | 纯回放一等不变量消费方=投影/冷启重建 |
| `event-timeline-rendering` | 时间线渲染 | 03.4 | R | 02.5 | 投影为 LLM 输入唯一装配源，消费方=请求装配 |
| `event-type-unification` | 事件类型单一来源 | 02.1 | R | 03.1 | 常量单一来源消费方=event 包跨包引用 |
| `evolution-evaluation` | 后验评估 | 07.3 | M | 07.4 | 评估窗口锚消费方=evolution（bundle 体系退役） |
| `example-file-memory-wiring` | 示例本地持久接线 | 08.2 | M | 08.3 | 微信示例本地持久接线消费方=bot module |
| `example-local-file-delivery` | 示例本地文件投递 | 08.3 | M | — | 本地文件投递消费方=bot module |
| `example-rl-visibility` | 示例 RL 可见面 | 06.7 | M | — | RL HTTP API 消费方=AReaL |
| `failure-polarity` | 失败极性透传 | 04.2 | R | 02.9 | 退出码极性消费方=exec/tmux 结算 |
| `feedback-binding` | 反馈绑定 | 06.4 | R | 07.3 | 反馈因果绑定消费方=RL 导出/评估 |
| `framework-flow-adapter` | 框架流适配器 | 02.5 | M | — | agent 装配消费方=请求执行；双路径退役+同标题重复需收敛 |
| `framework-prompt` | 框架 prompt 前置 | 02.5 | R | 01.4 | framework prompt 注入消费方=请求装配 |
| `generation-bound-model-references` | 绑定代模型引用 | 01.5 | M | 02.4 | 覆盖解析与执行同面消费方=绑定换代 |
| `git-native-refine` | git 原生自我改进 | 07.2 | R | 07.3 | refine register/status/rollback 消费方=evolution |
| `goal-tools` | goal 工具族 | 04.6 | R | 05.1 | goal 工具+回放重建消费方=治理 |
| `inbound-intent-declaration` | 入站意图声明 | 04.4 | R | 02.2 | /task 声明血统消费方=投递/task |
| `llm-event-valuation` | LLM 事件估值 | 03.4 | H | — | LLM 估值已被骨架纯函数替代，勿复活 |
| `llm-summary-generator` | LLM 摘要生成 | 03.4 | H | — | legacy 摘要管线退役，勿复活 |
| `maintainability-audit` | 可维护性审计 | 00.6 | H | — | 审计方法服务新门禁；旧发现台账不导入 |
| `mcp-call-tool` | MCP 执行网关 | 04.3 | R | — | MCP 执行网关消费方=工具注册 |
| `mcp-discovery` | MCP 发现 | 04.3 | R | — | MCP 发现消费方=工具注册 |
| `mcp-server-registry` | MCP 注册表 | 04.3 | M | 01.4 | 注册表生命周期消费方=组合根 |
| `meditation-agent-partition` | 冥想分区 | 07.1 | M | 04.4 | 授权分区观察面消费方=reflection 生产者 |
| `meditation-delivery-tool` | 冥想投递工具 | 07.1 | R | 04.4 | 冥想投递缝消费方=投递器 |
| `meditation-idle-gating` | 冥想空闲门控 | 07.1 | R | — | 双闸门触发消费方=reflection 调度 |
| `meditation-self-state-digest` | 冥想自状态 digest | 07.1 | R | — | 自状态 digest 消费方=reflection（与 memory-curation 同标题，见 §6） |
| `memory-curation` | 记忆策展 | 04.8 | M | 07.1 | 三原语/固化级联消费方=memory（需对齐 04 delta） |
| `offline-training-export` | 离线训练导出 | 06.4 | M | 06.6 | 授权导出/SFT 边界消费方=SFT 消费者 |
| `ops-deployment-integrity` | 运维部署完整性 | 09.5 | R | 06.7 | 运维探针认证消费方=部署/验收 |
| `orchestration` | 编排契约 | 00.3 | M | — | 顶层只承载编排契约，消费方=规划/调度（需对齐 00 delta） |
| `partition-local-storage-access` | 分区局部访问 | 03.2 | D | — | 性能主张需同条件证据（R13）；局部性语义待设计核 |
| `per-call-subagent-overrides` | 调用级子 agent 覆盖 | 01.6 | M | 02.7 | 调用覆盖解析消费方=subagent 委派（需对齐 01 delta） |
| `persistent-event-loop` | 常驻事件循环 | 02.5 | M | — | 常驻循环消费方=agent 调度（需对齐 02 delta） |
| `plan-agent` | plan agent 能力 | 02.5 | D | 00/09 | 新计划未承诺 plan agent；旧双模式 Run 消费者未定，不猜 |
| `production-wiring-fix` | 生产接线修复 | 08.1 | M | 03.2 | 存储生命周期栈装配消费方=生产入口 |
| `production-wiring` | 生产接线 | 08.1 | M | 03.2 | 生产装配消费方=生产入口 |
| `prompt-loader-fallback` | prompt 装载回退 | 01.6 | R | 02.5 | 嵌入 prompt 回退链消费方=prompt 装载 |
| `prototype-mvp` | 原型可运行 MVP | 00.1 | H | — | prototype 为行为参照，不作恢复清单；消费方=00 设计参照 |
| `race-gate-coverage` | race 门覆盖 | 09.3 | R | — | race 全包覆盖消费方=CI 门 |
| `readme-enhancement` | README 增强 | 09.6 | M | — | README 文档消费方=文档门 |
| `recall-hybrid-fusion` | 召回混合融合 | 04.7 | R | — | RRF 融合消费方=recall |
| `recall-protocol` | 召回标准协议 | 04.7 | M | 03.4 | 两段式召回协议消费方=recall |
| `relation-store-lru-rocks` | 关系库 LRU+Rocks | 03.1 | M | 04.8 | LRU+RocksDB 关系存储消费方=recall/Query |
| `relation-store-provider` | 关系库 provider | 03.1 | R | — | provider 抽象消费方=memory 消费者 |
| `remote-agent-communication` | 远端 agent 通信 | 04.4 | M | 02.7 | agent.Agent 接口/A2A 映射消费方=远端 agent |
| `request-budget-accounting` | 请求预算记账 | 03.4 | R | 02.5 | 预算组装消费方=请求装配 |
| `resident-continuity` | 常驻连续性 | 01.5 | M | 02.9 | 状态/执行器分离不变量消费方=state 发布 |
| `resident-release-evidence` | 发布证据保真 | 09.5 | M | 09.3 | 发布证据保真消费方=验收门槛 |
| `resident-session-continuity` | 常驻会话连续 | 04.2 | M | 03.3 | tmux 会话跨重启消费方=exec/session |
| `rl-feedback` | RL 反馈通道 | 06.7 | M | 06.4 | 诊断/反馈 long-poll 消费方=AReaL |
| `roadmap-governance` | 路线图治理 | 00.6 | H | — | 旧阶段门禁被新设计闸 G0–G3 取代；勿循旧 policy |
| `runtime-resource-ownership` | 运行时资源所有权 | 01.5 | M | 02.9 | 资源租约/关闭消费方=state 发布/关闭 |
| `rustviking-client` | rustviking 客户端 | 03.2 | R | — | KV range/batch 消费方=存储后端 |
| `rustviking-config` | rustviking 配置 | 03.2 | R | — | 二进制路径配置消费方=memory 装配 |
| `rustviking-range-scan` | rustviking 范围扫描 | 03.2 | R | — | 原生 range scan 消费方=存储后端 |
| `self-improvement-meditation` | 冥想即自我改进 | 07.1 | M | 07.2 | 产物必登记闭环消费方=evolution |
| `self-telemetry-audit` | 自管遥测审计 | 05.3 | R | 05.4 | 遥测分级动作消费方=观测 |
| `semantic-search` | 语义检索 | 04.7 | R | 03.1 | 向量检索消费方=recall |
| `snowflake-overflow-handling` | 雪花 ID 溢出 | 03.1 | M | — | 事件 ID 生成消费方=event 存储；时钟回拨方案已替代 |
| `startup-active-partition-bitmap` | 启动活动分区位图 | 03.2 | D | — | 位图/游标未实现、以等价形态达成，需设计核，不猜 |
| `storage-lifecycle-completion` | 存储生命周期补全 | 03.2 | M | 03.5 | L1→L2→L3 压实消费方=存储生命周期 |
| `subagent-turn-execution` | 子 agent turn 执行 | 02.7 | M | 02.3 | 一次调用=一 turn 消费方=委派 |
| `summary-memory-rag` | 摘要记忆 RAG | 03.4 | M | 04.7 | 摘要入 MemoryStore/召回消费方=投影 |
| `swappable-executor` | 可换执行器 | 01.5 | M | 02.5 | 执行器热换消费方=发布代 |
| `task-reentry` | 任务重入 | 02.7 | M | 02.2 | resume_task 重入消费方=task |
| `task-registry-and-board` | 任务注册表与看板 | 02.2 | M | 05.1 | 注册表/看板消费方=task |
| `task-registry-rebuild` | 任务注册表重建 | 02.2 | M | 03.3 | 冷启动重建消费方=task/持久 |
| `task-skeleton-compression` | 任务骨架压缩 | 03.4 | M | 02.5 | 骨架压缩主机制消费方=投影（需对齐 03 delta） |
| `telemetry-channel` | 遥测通道 | 05.3 | M | 03.4 | 遥测通道身份消费方=观测/投影 |
| `thinking-plan-event-classification` | thinking_plan 分类 | 02.1 | R | 03.4 | thinking_plan 分类消费方=事件分类 |
| `tombstone-partition-isolation` | 墓碑分区隔离 | 03.5 | R | — | per-partition 墓碑消费方=存储生命周期 |
| `tool-lifecycle-management` | 工具生命周期 | 04.1 | M | 01.4 | 内置工具注册消费方=工具注册 |
| `tool-output-interception` | 工具输出拦截 | 03.8 | M | 04.1 | 大结果拦截/外置消费方=结果外置路径 |
| `tool-security-alignment` | 工具安全对齐 | 04.2 | R | — | exec 安全配置传递消费方=exec/tmux |
| `trajectory-recording` | 轨迹录制 | 06.2 | M | 06.3 | 轨迹录制消费方=RL 采集 |
| `trajectory-trace-correlation` | 轨迹-trace 关联 | 06.2 | M | 05.3 | 轨迹/trace 双向关联消费方=RL+观测 |
| `trajectory-verification` | 轨迹重启验证 | 06.2 | M | 09.5 | 重启验证器消费方=RL 采集 |
| `turn-tracing` | turn 追踪 | 05.3 | R | — | turn trace 树消费方=观测 |
| `unified-task-semantics` | 统一任务语义 | 02.1 | M | 02.2 | 任务统一语义消费方=task/event |
| `unified-tool-registry` | 统一工具注册 | 04.1 | M | 01.4 | 工具注册中心消费方=工具注册 |
| `view-transform-stateless` | 视图变换无状态 | 03.4 | M | — | 无段返回原消息消费方=压缩管线 |
| `wechat-host-send-tool` | 微信宿主 send 工具 | 08.3 | M | 04.4 | 宿主 send 通道消费方=bot module |
| `wiki-code-sync` | wiki-源码同步 | 09.6 | M | 00.5 | wiki/源码交叉校验消费方=文档门 |
| `workflow-config-compilation` | 工作流配置编译 | 01.4 | M | 01.5 | 配置编译为执行绑定消费方=组合根 |

## 5. 域分布与处置汇总

| 域 | 能力数 | 主任务面 |
|---|---|---|
| 00-foundation | 8 | 00.1/00.3/00.4/00.5/00.6 |
| 01-state-binding | 10 | 01.2/01.4/01.5/01.6 |
| 02-execution | 20 | 02.1/02.2/02.3/02.4/02.5/02.7 |
| 03-history | 31 | 03.1/03.2/03.4/03.5/03.8 |
| 04-capabilities | 22 | 04.1/04.2/04.3/04.4/04.5/04.6/04.7/04.8 |
| 05-inspection | 3 | 05.3 |
| 06-learning | 8 | 06.1/06.2/06.4/06.7 |
| 07-evolution | 7 | 07.1/07.2/07.3 |
| 08-host | 5 | 08.1/08.2/08.3 |
| 09-acceptance | 6 | 09.1/09.3/09.5/09.6 |
| **合计** | **120** | — |

处置汇总（主表 120 行实测）：`R` ×44 · `M` ×64 · `H` ×7 · `D` ×5 = 120。未映射/漏映射 = 0。

## 6. 重复 Requirement 标题（聚合提醒）

| 标题 | 出现位置 | 提醒 |
|---|---|---|
| 冥想总结以高亮卡片行沉淀 | `meditation-self-state-digest`(07.1) + `memory-curation`(04.8) | **跨 spec 重复**：聚合到 07.1/04.8 边界统一，勿各写一份 |
| PlanProgressTracker removed | `plan-agent` 内 ×2 | 同 spec 内重复；plan-agent 已 `D`，随 02 设计一并裁 |
| FrameworkPrompt does not hardcode plan tool | `plan-agent` 内 ×2 | 同上 |
| Adapter converts a batch of AgentEvents into a framework Invocation | `framework-flow-adapter` 内 ×2 | 同 spec 内重复，随 02.5 收敛 |
| Adapter executes the framework Flow and forwards outputs | `framework-flow-adapter` 内 ×2 | 同上 |
| Adapter preserves tmux asynchronous semantics | `framework-flow-adapter` 内 ×2 | 同上 |
| Adapter supports sub-agent invocation mode | `framework-flow-adapter` 内 ×2 | 同上 |

## 7. 待核 / 待裁（design-blocked，不猜）

| cap | 新域.task | 待核点 |
|---|---|---|
| `adaptive-segment-size` | 03.2 | 段级封段是否复活为设计预案 |
| `cognitive-asset-guard` | 04.5 | 脚手架拆除条件是否已满足 |
| `partition-local-storage-access` | 03.2 | 局部性/性能主张需同条件证据 |
| `startup-active-partition-bitmap` | 03.2 | 位图/游标是否以等价形态采纳 |
| `plan-agent` | 02.5 | plan agent 是否进入新默认实施（消费者未定） |

> `runtime-inspection` 已由 **05-inspection** 于本批建立（05.1/05.2），**不再待裁**；其 delta 见 `changes/05-inspection/specs/runtime-inspection/spec.md`。

### 7.1 主消费任务 与 唯一 delta 写者（分列登记，按实际责任）

> 主表 `新域.task` 是**主责任务（消费/验收）**；下表登记 delta **实际落在与主责任务不同域**的能力。二者不同域时**不迁文件求表面一致**，而按真实职责分列登记（主消费任务与唯一 delta 写者分开登记/引用）；其余能力的 delta 写者与主责任务**同域一致**。

| 能力 | 主消费任务（主表） | 唯一 delta 写者（实际责任） | 论证（真实职责，非文件求一致） |
|---|---|---|---|
| `adaptive-poll-scheduling` | 02.2 | 04.2 | 轮询节奏/停止凭证语义落在 exec/tmux 执行路径（`changes/04-capabilities/specs/adaptive-poll-scheduling`）；02.2 作为 task 受理/detach/settle 消费（02 design 明示 TTL/轮询“04.2 落地”） |
| `async-task-lifetime` | 02.2 | 04.2 | 统一 TTL 锚点落在外部进程/mux 会话生命（`changes/04-capabilities/specs/async-task-lifetime`）；02.2 消费 |
| `failure-polarity` | 04.2 | 02.4 | 模型/RL 单终局规则落在 model 终局归约（`changes/02-execution/specs/failure-polarity`，02 design 覆盖映射 02.4）；04.2 消费退出码极性 |
| `recall-protocol` | 04.7 | 03.1 | 索引卡召回标准协议落在原文/召回（`changes/03-history/specs/recall-protocol`，03 design 覆盖映射 03.1）；04.7 作为检索消费者 |
| `request-budget-accounting` | 03.4 | 02.4 | usage 尾包归约落在 model 终局（`changes/02-execution/specs/request-budget-accounting`，02 design 覆盖映射 02.4）；03.4 消费预算组装 |
| `task-registry-rebuild` | 02.2 | 03.3 | 冷启恢复按当前编排绑定落持久提交（`changes/03-history/specs/task-registry-rebuild`，03 design 覆盖映射 03.3）；02.2 消费 |
| `committed-event-attribution` | 02.1 | 05.1 | 提交成功才可召回票据的只读归因读面由 05.1 承载（`changes/05-inspection/specs/committed-event-attribution`） |
| `degradation-behaviors` | 04.6 | 05.4 | 退化/阻塞可解释性与“下一推进者”由 05.4 承载（`changes/05-inspection/specs/degradation-behaviors`） |
| `trajectory-trace-correlation` | 06.2 | 05.3 | 轨迹/trace 关联在 Query 读面可解释，由 05.3 观测承载（`changes/05-inspection/specs/trajectory-trace-correlation`） |
| `example-rl-visibility` | 06.7 | 08.4 | 诊断可见性与宿主生命周期由 08.4 承载（`changes/08-host/specs/example-rl-visibility`，08 proposal） |

> 说明：本表不含「无 delta」能力（`H`/`D` 与继承类，如 `code-documentation`、`plan-agent` 等），其处置以主表与各域 design 为准；`runtime-inspection` 主写 05.1/05.2。

> 另注：10 域 `specs/` 已由本批串行集成收口（00–09 全在）；`changes/` 的 delta 放置即为**实际责任**，主表相关行按 §7.1 分列登记、不再标“待对齐”。

## 8. 勿循旧 policy / 勿越权（本表边界）

1. 新域 00–09 对应**新计划**，**不**沿用旧 `rebuild-formal-runtime` 的 C/E/I/D 域码、W01/W02 Wave 字母、RV 编号与 DEC 表（计划 §8）。
2. `M` 处一律「**需对齐 <域> delta**」；在目标域写者产出 delta 前，**不做机械继承改写**。
3. **old names 不全部恢复**；标题命中不代表语义适用；不因 source 存在即复活旧类名/包结构/算法。
4. 本单**不自行写 spec**、不改主规格、不改旧 change、不生成产品。
5. 可读并行中的其它新域产物，但**不抢占修改**。

## 9. 验证命令（供复跑核对集合，不充当语义认证）

```sh
# 行数/唯一性：主规格目录集合
ls openspec/specs | wc -l              # 期望 120
find openspec/specs -name spec.md | wc -l   # 期望 120
# 计数（仅统计，非语义认证）
grep -rh '^### Requirement:' openspec/specs/*/spec.md | wc -l   # 601
grep -rh '^#### Scenario:' openspec/specs/*/spec.md | wc -l     # 1217
```

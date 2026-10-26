# runtime-rebuild · 主规格覆盖映射（coverage）

> 角色：本文件把现存 **120** 个主规格（`openspec/specs/<cap>/spec.md`）逐一映射到新计划的 **10 个二级新域（00–09）与 65 个新叶任务**。
> 依据：只读计划 `运行时_重设_实施计划_304e54c3.md`（§6 域布局、§7 叶任务清单）。
> 边界：只读 + 新增本 md；**不写 spec**、不改主规格、不改旧 change、不生成产品。
> 收口状态：10 域四件套与本表已由串行集成（批次 2）收口；delta 实际放置以各域 `specs/` 为准，**主消费任务**与**唯一 delta 写者**的分列登记见 §7.1。

## 0. 方法与诚实声明

- 复核粒度 = **能力级**：逐文件读 `## Purpose` + 全部 `### Requirement:` 标题（并在疑点处下钻正文）。**未**对全部 **601** 条 Requirement 逐条做语义判定（1217 个 Scenario 亦然）；故本表不声称"601 条已审完"。
- 处理判据只看**新 goal 是否真实消费**该能力，**不因 source 文件存在就复活**其旧机制、旧类名或旧包结构（计划 §2「保留的用户要求 8」「不做」）。
- **旧 names 不是全部恢复**：新计划的类名/包结构/算法不默认沿用历史；主规格标题命中 ≠ 语义适用。
- 「行为需修改」：10 域 delta 已由串行集成收口（本批），本表按 **「以 <域> delta 为准（已落地）」** 登记；delta 改后条文**完整覆盖**主源语义、**旧条不再并存**——**不误称旧条继承**，也不做机械继承改写。
- 主表 `新域.task` 列为该能力的**主责任务（消费/验收）**；该能力的**唯一 delta 写者**以各域 `specs/` 实际放置为准。二者不同域时在 §7.1 **单列登记**（主消费任务与唯一 delta 写者分开登记/引用），不混同、不迁文件求表面一致。
- **不自行写 spec**：新增独立能力 `runtime-inspection` 已由 **05-inspection** 建立（05.1/05.2 只读总图/快照/游标/订阅）；本表只给域位与消费者，不给条款。

## 1. 集合校验（可复跑）

- 本表行数 = **121**，与 `ls openspec/specs | wc -l` 相等（含本批新建 `runtime-inspection`）；每行 `路径` 唯一、且 `openspec/specs/<cap>/spec.md` 存在（集合可核）。
- 每个主规格目录恰好 1 个 `spec.md`（无遗漏目录）。
- task 编号取自同目录并行产出的 `task-graph.json`（**65** 个 id：00×6 / 01×6 / 02×9 / 03×8 / 04×9 / 05×4 / 06×8 / 07×4 / 08×5 / 09×6），本表所引 id 全部落在图内。

## 2. 处置图例

| 代号 | 含义 |
|---|---|
| `R` 保留能力待实现 | 能力保留，按新计划从零实现；旧文本仅背景，**非继承契约** |
| `M` 行为需修改 | 行为须按新 goal 调整；旧文本仅参考，**最终以该域 delta 为准**（已落地；改后条文完整覆盖主源语义、旧条不再并存，**不误称旧条继承**） |
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
| `runtime-inspection` | 运行时只读观测面（本批新建能力，05-inspection 建立） | 05.1/05.2 | R | — | 只读总图/快照/游标/订阅；消费者=08.4 诊断面与验收 runner |
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
| `compaction-scheduler` | 压实调度 | 03.2 | R | 03.5 | 分段压实调度消费方=存储生命周期 |
| `compress-event-enrichment` | 压缩事件富集 | 03.4 | H | — | 富集注记已被骨架卡片行替代，勿复活；投影为消费方 |
| `compress-quality-fix` | 压缩质量修复 | 03.4 | R | — | 引用解析/角色填充消费方=投影装配（以 03 delta 为准，已落地） |
| `compression-user-message` | 压缩保留未完成驱动 | 03.4 | R | — | 保留最后 user message 消费方=投影（以 03 delta 为准，已落地） |
| `concurrency-hardening` | 并发加固 | 02.5 | R | 04.2 | 并发安全消费方=agent/exec 状态访问 |
| `config-hot-reload` | 配置热重载 | 01.5 | M | 02.5 | 热更发布/回滚消费方=state 发布代（以 01 delta 为准，已落地） |
| `config-state-twinship` | 配置-state 孪生 | 01.4 | R | 02.5 | state 持有生效 system prompt；消费方=请求装配 |
| `consolidation-triggers` | 巩固触发 | 04.8 | R | — | 巩固建议消费方=memory 固化 |
| `context-scoping` | 上下文还原/范围 | 01.2 | M | 02.5 | resume 上下文还原+因果链挂载；授权范围消费方=state；域声明继承，见 §10.2 |
| `conversation-self-heal` | 会话投影自愈 | 03.4 | R | — | 投影按 EventKey 幂等追加，消费方=投影 |
| `cross-session-delivery` | 跨 session 投递 | 04.4 | M | 07.1 | 投递白名单/寻址消费方=投递器（以 04 delta 为准，已落地） |
| `decision-capture` | 决策快照采集 | 06.1 | R | 06.3 | 决策快照=版本化记录生产点 |
| `degradation-behaviors` | 依赖退化响应 | 04.6 | R | — | 退化响应默认关；消费方=治理/资源恢复 |
| `delivery-dispatch-receipts` | 投递回执 | 04.4 | R | 05.1 | 投递回执消费方=投递器/Query |
| `deterministic-compress-level` | 确定性压缩分级 | 03.4 | R | — | 段龄纯函数消费方=压缩管线 |
| `error-handling-consistency` | 错误处理一致性 | 00.6 | R | — | log 统一/错误必检=CI lint 门消费 |
| `evals-harness` | 组件级评估 harness | 09.1 | M | 09.5 | 票据可召回/工具选择评估消费方=验收 runner；域声明继承，见 §10.2 |
| `event-compaction` | 事件压实 | 03.4 | R | — | 段边界/双路径退役消费方=压缩管线（以 03 delta 为准，已落地） |
| `event-key-visibility` | 事件键可见性 | 02.5 | R | 03.4 | event key 注入消费方=请求装配；主编排者收口：全条改 R（对位不成立） |
| `event-lifecycle` | 事件生命周期 | 03.5 | M | — | TTL/容量淘汰消费方=存储生命周期 |
| `event-metadata-contract` | 事件元数据契约 | 02.1 | R | 06.1 | MetaKey/EventKey canonical 消费方=事件身份 |
| `event-relation-store` | 事件关系库 | 03.1 | M | 04.8 | 关系图持久化消费方=recall/Query；域声明继承，见 §10.2 |
| `event-segment-store` | 事件分段存储 | 03.2 | M | — | 分段存储消费方=存储后端（以 03 delta 为准，已落地） |
  （注记 2026-10-15：03.2「已落地」=存储屏障面；**QueryEvents 读面已随 04.7 落地**（memory/query_events.go 读端口+MemoryEventQuery 确定性实现；真实存储源接线随 Body schema 落定）——见 unblock-04.6-04.7.md §二.2）
| `event-sourced-projection` | 事件溯源投影 | 03.4 | R | 05.1 | 纯回放一等不变量消费方=投影/冷启重建 |
| `event-timeline-rendering` | 时间线渲染 | 03.4 | R | 02.5 | 投影为 LLM 输入唯一装配源，消费方=请求装配 |
| `event-type-unification` | 事件类型单一来源 | 02.1 | R | 03.1 | 常量单一来源消费方=event 包跨包引用 |
| `evolution-evaluation` | 后验评估 | 07.3 | M | 07.4 | 评估窗锚=实际请求消费版本的激活证据；GitSHA 仅因果关联、register 追加映射不改已存事实、旧数据无证据标 unknown |
| `example-file-memory-wiring` | 示例本地持久接线 | 08.2 | M | 08.3 | 微信示例本地持久接线消费方=bot module；域声明继承，见 §10.2 |
| `example-local-file-delivery` | 示例本地文件投递 | 08.3 | M | — | 本地文件投递消费方=bot module |
| `example-rl-visibility` | 示例 RL 可见面 | 06.7 | M | — | RL HTTP API 消费方=AReaL |
| `failure-polarity` | 失败极性透传 | 04.2 | R | 02.9 | 退出码极性消费方=exec/tmux 结算 |
| `feedback-binding` | 反馈绑定 | 06.4 | R | 07.3 | 反馈因果绑定消费方=RL 导出/评估 |
| `framework-flow-adapter` | 框架流适配器 | 02.5 | H | — | agent 装配消费方=请求执行；双路径退役+同标题重复需收敛 |
| `framework-prompt` | 框架 prompt 前置 | 02.5 | R | 01.4 | framework prompt 注入消费方=请求装配 |
| `generation-bound-model-references` | 绑定代模型引用 | 01.5 | M | 02.4 | 覆盖解析与执行同面消费方=绑定换代；域声明继承，见 §10.2 |
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
| `memory-curation` | 记忆策展 | 04.8 | M | 07.1 | 三原语/固化级联消费方=memory（以 04 delta 为准，已落地） |
| `offline-training-export` | 离线训练导出 | 06.4 | M | 06.6 | 授权导出/SFT 边界消费方=SFT 消费者；用途矩阵三态（unknown/ineligible/qualified） |
| `ops-deployment-integrity` | 运维部署完整性 | 09.5 | R | 06.7 | 运维探针认证消费方=部署/验收 |
| `orchestration` | 编排契约 | 00.3 | M | — | 顶层只承载编排契约，消费方=规划/调度（以 00 delta 为准，已落地） |
| `partition-local-storage-access` | 分区局部访问 | 03.2 | D | — | 性能主张需同条件证据（R13）；局部性语义待设计核 |
| `per-call-subagent-overrides` | 调用级子 agent 覆盖 | 01.6 | M | 02.7 | 调用覆盖解析消费方=subagent 委派（以 01 delta 为准，已落地） |
| `persistent-event-loop` | 常驻事件循环 | 02.5 | M | — | 常驻循环消费方=agent 调度（以 02 delta 为准，已落地） |
| `plan-agent` | plan agent 能力 | 02.5 | D | 00/09 | 新计划未承诺 plan agent；旧双模式 Run 消费者未定，不猜 |
| `production-wiring-fix` | 生产接线修复 | 08.1 | R | 03.2 | 存储生命周期栈装配消费方=生产入口 |
| `production-wiring` | 生产接线 | 08.1 | M | 03.2 | 生产装配消费方=生产入口 |
| `prompt-loader-fallback` | prompt 装载回退 | 01.6 | R | 02.5 | 嵌入 prompt 回退链消费方=prompt 装载 |
| `prototype-mvp` | 原型可运行 MVP | 00.1 | H | — | prototype 为行为参照，不作恢复清单；消费方=00 设计参照 |
| `race-gate-coverage` | race 门覆盖 | 09.3 | R | — | race 全包覆盖消费方=CI 门 |
| `readme-enhancement` | README 增强 | 09.6 | R | — | README 文档消费方=文档门 |
| `recall-hybrid-fusion` | 召回混合融合 | 04.7 | R | — | RRF 融合消费方=recall |
| `recall-protocol` | 召回标准协议 | 04.7 | M | 03.4 | 两段式召回协议消费方=recall |
| `relation-store-lru-rocks` | 关系库 LRU+Rocks | 03.1 | R | 04.8 | LRU+RocksDB 关系存储消费方=recall/Query |
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
| `self-improvement-meditation` | 冥想即自我改进 | 07.1 | M | 07.2 | 产物必登记闭环消费方=evolution；域声明继承，见 §10.2 |
| `self-telemetry-audit` | 自管遥测审计 | 05.3 | R | 05.4 | 遥测分级动作消费方=观测 |
| `semantic-search` | 语义检索 | 04.7 | R | 03.1 | 向量检索消费方=recall |
| `snowflake-overflow-handling` | 雪花 ID 溢出 | 03.1 | R | — | 事件 ID 生成消费方=event 存储；时钟回拨方案已替代 |
| `startup-active-partition-bitmap` | 启动活动分区位图 | 03.2 | D | — | 位图/游标未实现、以等价形态达成，需设计核，不猜 |
| `storage-lifecycle-completion` | 存储生命周期补全 | 03.2 | M | 03.5 | L1→L2→L3 压实消费方=存储生命周期 |
| `subagent-turn-execution` | 子 agent turn 执行 | 02.7 | M | 02.3 | 统一事件调度（同 invocation 可多 decision）；受理≠结清；原生 CallableTool 兼容 |
| `summary-memory-rag` | 摘要记忆 RAG | 03.4 | R | 04.7 | 摘要入 MemoryStore/召回消费方=投影 |
| `swappable-executor` | 可换执行器 | 01.5 | H | 02.5 | 执行器热换消费方=发布代 |
| `task-reentry` | 任务重入 | 02.7 | R | 02.2 | resume_task 重入消费方=task；主编排者收口：全条改 R（对位不成立） |
| `task-registry-and-board` | 任务注册表与看板 | 02.2 | M | 05.1 | 注册表/看板消费方=task |
| `task-registry-rebuild` | 任务注册表重建 | 02.2 | M | 03.3 | 冷启动重建消费方=task/持久 |
| `task-skeleton-compression` | 任务骨架压缩 | 03.4 | R | 02.5 | 骨架压缩主机制消费方=投影（以 03 delta 为准，已落地） |
| `telemetry-channel` | 遥测通道 | 05.3 | R | 03.4 | 遥测通道身份消费方=观测/投影 |
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
| `view-transform-stateless` | 视图变换无状态 | 03.4 | R | — | 无段返回原消息消费方=压缩管线 |
| `wechat-host-send-tool` | 微信宿主 send 工具 | 08.3 | M | 04.4 | 宿主 send 通道消费方=bot module；域声明继承，见 §10.2 |
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

处置汇总（主表 120 行实测）：`R` ×57 · `M` ×49 · `H` ×9 · `D` ×5 = 120。未映射/漏映射 = 0。（空洞处置批次后：A 2 能力 M→H、B 13 能力 M→R；见 §10.6。）

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
2. `M` 处一律「**以 <域> delta 为准（已落地）**」；delta 改后条文完整覆盖主源语义、旧条不再并存，**不误称旧条继承**，不做机械继承改写。
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

## 10. 601 条 Requirement 归属核验（00.2 补证）

> **目的（防「集合覆盖冒充语义审定」）**：§4 只做**能力级**映射（120 行），本节对其下**逐条** `### Requirement:`（实测 **601** 条，可复跑 `grep -rh '^### Requirement:' openspec/specs/*/spec.md | wc -l`）给出**归属位置/结论**，并对无承载项**具名**。
> **核验深度（诚实声明）**：本轮为**标题级为主 + 声明级**——① 同名 delta 逐条比对 **Requirement 标题**与 delta **正文 `### Requirement:` / `>` 头注继承声明**；② 无同名 delta 的能力，核对各域 `proposal.md`/`design.md` 的**继承 / 只读 / 他域落地**声明。**未**逐条下钻每条 Scenario 正文，**未**做跨标题**语义同义判定**（正文级语义对位另列「待裁」，不计为覆盖）。
> **判据（每条 M 条 Requirement 归入一档）**：
> | 档位 | 含义 | 是否算「有承载」 |
> |---|---|---|
> | `改写` | 同名 delta 正文出现该 Requirement **标题** | ✅ 显式（标题级） |
> | `头注继承` | 同名 delta **头注**显式声明继承该条（或其主干） | ✅ 显式（头注级） |
> | `Delta未列` | 同名 delta 存在但**未列**该条；按 OpenSpec delta 约定「未列项=保留」 | ⚠️ 隐式（约定保留，非逐条列名） |
> | `域声明` | **无同名 delta**，但某域 `proposal/design` 显式声明该能力继承/只读/他域落地 | ⚠️ 声明级（**非 delta**） |
> | `正文对位` | **无同名 delta**，但预期 delta 正文经逐条实证对位覆盖该条（对位批次新增） | ✅ 显式（正文级，已核） |
> | `空洞` | **无同名 delta 且无任何域声明** | ❌ **归属空洞（具名）** |

### 10.1 汇总（实测）

| 处置 | 条数 | 归属分档 | 条数 |
|---|---|---|---|
| `R` 保留待实现 | 242 | 改写（同名 delta 正文标题命中） | 42 |
| `M` 行为需修改 | 268 | 头注继承（同名 delta 头注声明） | 93 |
| `H` 历史参考 | 60 | Delta未列（同名 delta 约定保留，隐式） | 90 |
| `D` 设计待核 | 31 | 域声明（无同名 delta，proposal/design 声明继承/只读） | 39 |
| — | — | 正文对位（无同名 delta，预期 delta 正文逐条对位已核） | 4 |
| — | — | **归属空洞（无同名 delta、无域声明）** | **0** |
| **合计** | **601** | **合计** | **601** |

**三类「应为 0」核验**（本任务核心价值）：

| 项 | 期望 | 实测 | 结论 |
|---|---|---|---|
| **M 类归属空洞** | 0 | **0 条** | ✅ 已由空洞处置批次清零（A 2 能力→H、B 13 能力→R、C 17 条对位/改 R，见 §10.3/§10.6） |
| **H 类误恢复** | 0 | 0 | ✅ 抽查通过（§10.4） |
| **D 类未登记** | 0 | 0 | ✅ 5 项全部具名于 §7（§10.5） |

> 说明：M 类 268 条中，**显式承载（改写+头注继承）共 135 条**；`Delta未列`（90）为**约定保留的隐式继承**；`域声明`（39）为**无 delta 的域级声明**（其中 9 能力 §4 标 `M` 而域声明为「继承/只读」，属**标签张力**，见 §10.3 与 §10.6）；`正文对位`（4）为**预期 delta 正文逐条实证覆盖**（见 §10.2 与 §10.3 备注）；`空洞`（0）已由本批处置清零。**切勿把 `Delta未列`/`域声明`/`正文对位` 读作「已审定覆盖」。**

### 10.2 逐条归属（601 行，按能力分组；`能力 | 主源 Requirement 标题 | 处置 | 归属位置/结论`）

| 能力 | 主源 Requirement 标题 | 处置 | 归属位置/结论 |
|---|---|---|---|
| action-tool-config | TmuxMonitor configuration is exposed via ActionProperties | M | `changes/04-capabilities/specs/action-tool-config/spec.md#TmuxMonitor configuration is exposed via ActionProperties` |
| action-tool-config | ActionProperties supports compress configuration | M | `changes/04-capabilities/specs/action-tool-config/spec.md#ActionProperties supports compress configuration` |
| action-tool-config | tmux 可用性真实探测 | M | `changes/04-capabilities/specs/action-tool-config/spec.md#tmux 可用性真实探测` |
| adaptive-poll-scheduling | Per-task piecewise poll schedule | M | `changes/04-capabilities/specs/adaptive-poll-scheduling/spec.md`（未列；按 delta 约定=隐式保留） |
| adaptive-poll-scheduling | Settle-or-detach contract | M | `changes/04-capabilities/specs/adaptive-poll-scheduling/spec.md`（未列；按 delta 约定=隐式保留） |
| adaptive-poll-scheduling | Task spawn waits on settle-or-detach without a sync_wait knob | M | `changes/04-capabilities/specs/adaptive-poll-scheduling/spec.md`（未列；按 delta 约定=隐式保留） |
| adaptive-poll-scheduling | Per-session adaptive scheduling | M | `changes/04-capabilities/specs/adaptive-poll-scheduling/spec.md`（未列；按 delta 约定=隐式保留） |
| adaptive-poll-scheduling | alive-detached sparsest cadence | M | `changes/04-capabilities/specs/adaptive-poll-scheduling/spec.md`（未列；按 delta 约定=隐式保留） |
| adaptive-poll-scheduling | Default behavior preserved | M | `changes/04-capabilities/specs/adaptive-poll-scheduling/spec.md`（未列；按 delta 约定=隐式保留） |
| adaptive-segment-size | Segment seals early when event count reaches threshold | D | §7 待核·段级封段是否复活为设计预案 |
| adaptive-segment-size | Compaction honors max_events_per_segment at L2/L3 | D | §7 待核·段级封段是否复活为设计预案 |
| adaptive-segment-size | Configuration is per-partition overridable | D | §7 待核·段级封段是否复活为设计预案 |
| agent-thinking-activation | hy3 模型经 reasoning_effort 触发思考 | R | → `02.4` 从零实现；旧文仅背景 |
| agent-thinking-activation | hy3 agent 运行行为符合思考配置预期 | R | → `02.4` 从零实现；旧文仅背景 |
| agent-thinking-activation | 思考修复不回归其它模型 agent | R | → `02.4` 从零实现；旧文仅背景 |
| approval-channels | 审批消息流抽象 | R | → `04.5` 从零实现；旧文仅背景 |
| approval-channels | CLI 批准入口 | R | → `04.5` 从零实现；旧文仅背景 |
| approval-channels | 微信消息注入入口 | R | → `04.5` 从零实现；旧文仅背景 |
| architecture-guardrails | 分层依赖方向可机械断言 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 上游内部行为假设钉 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 变更局部性准绳（立法） | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 唯一编排发布权与中性契约 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 编排验收与 runtime 不变量分开 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 常驻状态与执行装配分离而非复制完整 shell | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 测试文件族与职责同位 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 测试标识不承载迭代编号 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 组合根物理边界与追踪卫生 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-guardrails | 机器消费的点号模块引用必须可解析 | R | → `00.4` 从零实现；旧文仅背景 |
| architecture-review-standards | 审查 SHALL 验证事件驱动一致性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证内存中心一致性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证视图转换完整性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证因果关系完整性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证上下文隔离完整性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证分层存储与生命周期一致性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证并发安全 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证代码一致性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证接口契约 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证生产接线完整性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| architecture-review-standards | 审查 SHALL 验证工具执行安全 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| async-task-execution | 工具调用经 spawn + dense 窗口执行 | M | `changes/02-execution/specs/async-task-execution/spec.md#工具调用经 spawn + dense 窗口执行` |
| async-task-execution | 并行 dense 窗口 | M | `changes/02-execution/specs/async-task-execution/spec.md#并行 dense 窗口` |
| async-task-execution | 按任务类型的 settle 探测器与分档 | M | `changes/02-execution/specs/async-task-execution/spec.md#按任务类型的 settle 探测器与分档` |
| async-task-execution | 服务型任务转 alive-detached | M | `changes/02-execution/specs/async-task-execution/spec.md#服务型任务转 alive-detached` |
| async-task-execution | task_settled 为通知类 input 事件 | M | `changes/02-execution/specs/async-task-execution/spec.md#task_settled 为通知类 input 事件` |
| async-task-execution | 任务状态机 resume 边 | M | `changes/02-execution/specs/async-task-execution/spec.md`（头注继承声明） |
| async-task-execution | 终态事实四端一致 | M | `changes/02-execution/specs/async-task-execution/spec.md`（头注继承声明） |
| async-task-execution | 迟到信号不得复活终态 | M | `changes/02-execution/specs/async-task-execution/spec.md`（头注继承声明） |
| async-task-execution | 生命周期类只决定编排语义，不决定是否受年龄约束 | M | `changes/02-execution/specs/async-task-execution/spec.md`（头注继承声明） |
| async-task-execution | 后台执行保留编排引用至实际停止 | M | `changes/02-execution/specs/async-task-execution/spec.md`（头注继承声明） |
| async-task-execution | spawnGate 的审计触发源与豁免放行 | M | `changes/02-execution/specs/async-task-execution/spec.md`（头注继承声明） |
| async-task-lifetime | 统一 TTL 是所有任务的绝对寿命上限 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-task-lifetime | 写入型重入续期锚点，只读观察不续期 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-task-lifetime | 看板只呈现事实与剩余寿命，不诱发重复裁决 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-task-lifetime | 年龄机制只有一个来源，已删除的旧墙不留别名 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-task-lifetime | 回收由唤醒驱动，不为到期引入常驻计时器 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-task-lifetime | 结算通知以有界票据进入投影并可被折叠回收 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-task-lifetime | 静默探测与 TTL 正交 | M | `changes/04-capabilities/specs/async-task-lifetime/spec.md`（未列；按 delta 约定=隐式保留） |
| async-tool-event-fix | 冻结批次消费时归一化外部消息角色 | R | → `02.3` 从零实现；对位不成立 |
| async-tool-event-fix | Non-interactive commands complete on stable | M | changes/02-execution/specs/async-task-execution/spec.md#按任务类型的 settle 探测器与分档（正文对位，已核） |
| causal-chain-isolation | Causal chain is isolated by session | R | → `03.1` 从零实现；旧文仅背景 |
| code-documentation | 代码注释形态白名单 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | go doc 注释的契约边界 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 文档索引语法与目标限制 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 索引必须落到具体章节 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 测试文件的注释政策 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 注释剥离等价证明 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 注释政策扫描与豁免台账 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 生成式 API 文档与新鲜度 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 残留判据只收无歧义词，且英文词必须整词匹配 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 棘轮只能在其基线记录的扫描面上使用 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 等价对账仪器的判据完整性 | R | → `00.6` 从零实现；旧文仅背景 |
| code-documentation | 源文件职责索引 | R | → `00.6` 从零实现；旧文仅背景 |
| cognitive-asset-guard | 认知资产漂移审计（终态） | D | §7 待核·脚手架拆除条件是否已满足 |
| cognitive-asset-guard | 认知资产写入需人工审批（脚手架——拆除条件：权限域分离落地） | D | §7 待核·脚手架拆除条件是否已满足 |
| cognitive-asset-guard | 走私形态入舱引导（脚手架——拆除条件：failure-polarity-passthrough 上线且观察期走私未复发） | D | §7 待核·脚手架拆除条件是否已满足 |
| cognitive-asset-guard | 权限域分离方向（终态锚定，实现属独立运维变更） | D | §7 待核·脚手架拆除条件是否已满足 |
| cognitive-asset-guard | 防线默认态与拆除账本 | D | §7 待核·脚手架拆除条件是否已满足 |
| committed-event-attribution | 提交成功才发布可召回票据 | R | → `02.1` 从零实现；旧文仅背景 |
| committed-event-attribution | 因果域内线性化 | R | → `02.1` 从零实现；旧文仅背景 |
| committed-event-attribution | 不完整回溯可辨 | R | → `02.1` 从零实现；旧文仅背景 |
| compaction-scheduler | Compaction scheduler triggers L1→L2 compaction automatically | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compaction-scheduler | Compaction scheduler triggers L2→L3 compaction automatically | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compaction-scheduler | Scheduler discovers segments by layer | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compress-event-enrichment | buildCompressEvent outputs key, type, summary, value score, and processing strategy | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| compress-event-enrichment | Compress notice warns against repeating archived operations | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| compress-quality-fix | resolveReferenceToMessage infers Role from EventType | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compress-quality-fix | BuildEventReference infers Role when Response is nil | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compress-quality-fix | SessionProjection Append is idempotent by event key | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compress-quality-fix | resolveReferenceToMessage logs distinguish error types | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compression-user-message | 压缩后保留未完成的 User message | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compression-user-message | ensureUserPrompt 使用 pending user 策略 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| compression-user-message | 摘要使用 System role | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| concurrency-hardening | TagentAgent session context is thread-safe | R | → `02.5` 从零实现；旧文仅背景 |
| concurrency-hardening | TmuxMonitor.running is atomically accessed | R | → `02.5` 从零实现；旧文仅背景 |
| concurrency-hardening | TmuxMonitor.checkSession modifies session fields under lock | R | → `02.5` 从零实现；旧文仅背景 |
| concurrency-hardening | InMemRelationStore.truncateJournal is locked | R | → `02.5` 从零实现；旧文仅背景 |
| config-hot-reload | 配置热更统一应用模型 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-hot-reload | 压缩参数同代快照 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-hot-reload | 热更回执报告 effective 状态 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-hot-reload | 执行代绑定完整性 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-hot-reload | 首代可回滚 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md#首代可回滚` |
| config-hot-reload | 数值与结构共享完整有效配置回滚记录 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-hot-reload | 重入的执行视图与目标解析同源 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-hot-reload | 配置热更维度矩阵 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md#配置热更维度矩阵` |
| config-hot-reload | 提交点记录即消费值 | M | `changes/01-state-binding/specs/config-hot-reload/spec.md`（未列；按 delta 约定=隐式保留） |
| config-state-twinship | 生效 system prompt 由 state 持有 | R | → `01.4` 从零实现；旧文仅背景 |
| config-state-twinship | config→plan→govern→state 管道 | R | → `01.4` 从零实现；旧文仅背景 |
| consolidation-triggers | 建议式触发（两路） | R | → `04.8` 从零实现；旧文仅背景 |
| consolidation-triggers | 硬门控 | R | → `04.8` 从零实现；旧文仅背景 |
| context-scoping | resume 任务链上下文还原器 | M | **无 delta**；域声明：`changes/01-state-binding/proposal.md`「本域不改：resume 任务链还原由 04/03 恢复路径消费；以证据映射（继承源块）登记，不出 delta」（域声明继承，见 §4 备注） |
| context-scoping | 固化物因果链挂载 | M | **无 delta**；域声明：`changes/01-state-binding/proposal.md`「本域不改：resume 任务链还原由 04/03 恢复路径消费；以证据映射（继承源块）登记，不出 delta」（域声明继承，见 §4 备注） |
| conversation-self-heal | 投影按 EventKey 幂等追加 | R | → `03.4` 从零实现；旧文仅背景 |
| cross-session-delivery | 投递前白名单校验且空表拒绝一切 | M | `changes/04-capabilities/specs/cross-session-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| cross-session-delivery | 目标 loop 未运行具名拒绝 | M | `changes/04-capabilities/specs/cross-session-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| cross-session-delivery | 投递收口于既有注入入口并保持 meditation 谱系 | M | `changes/04-capabilities/specs/cross-session-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| cross-session-delivery | 寻址范围限定进程内常驻表 | M | `changes/04-capabilities/specs/cross-session-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| decision-capture | 可选SDK决策快照 | R | → `06.1` 从零实现；旧文仅背景 |
| decision-capture | 精确可选归因 | R | → `06.1` 从零实现；旧文仅背景 |
| decision-capture | 非阻塞有界录制 | R | → `06.1` 从零实现；旧文仅背景 |
| decision-capture | 封账完整性可证明 | R | → `06.1` 从零实现；旧文仅背景 |
| decision-capture | 采集不改变运行权责 | R | → `06.1` 从零实现；旧文仅背景 |
| degradation-behaviors | 依赖退化行为响应（警告级、默认关） | R | → `04.6` 从零实现；旧文仅背景 |
| delivery-dispatch-receipts | 预期外静默终态必回执 | R | → `04.4` 从零实现；旧文仅背景 |
| delivery-dispatch-receipts | 回执走持久总线且血统内部 | R | → `04.4` 从零实现；旧文仅背景 |
| delivery-dispatch-receipts | 回执不递归 | R | → `04.4` 从零实现；旧文仅背景 |
| deterministic-compress-level | 确定性压缩分级纯函数 | R | → `03.4` 从零实现；旧文仅背景 |
| error-handling-consistency | Log package is unified | R | → `00.6` 迁移适用检查规则时逐条评估，旧文仅背景 |
| error-handling-consistency | Error return values are checked | R | → `00.6` 迁移适用检查规则时逐条评估，旧文仅背景 |
| error-handling-consistency | Stale comments referencing removed mechanisms are cleaned | R | → `00.6` 迁移适用检查规则时逐条评估，旧文仅背景 |
| evals-harness | 票据可召回率评估 | M | **无 delta**；域声明：`changes/07-evolution/proposal.md`「票据可召回率、工具选择评估与 Bad Case 资产——继承（本域重建其 harness 挂点，不改条款）」（域声明继承，见 §4 备注） |
| evals-harness | 工具选择评估与 Bad Case 资产 | M | **无 delta**；域声明：`changes/07-evolution/proposal.md`「票据可召回率、工具选择评估与 Bad Case 资产——继承（本域重建其 harness 挂点，不改条款）」（域声明继承，见 §4 备注） |
| event-compaction | TaskSegmenter provides unified task boundary segmentation | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| event-compaction | Compactor triggers from BeforeModel callback instead of Preprocessor.Process | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| event-compaction | SmartCompressor triggers from BeforeModel callback instead of Preprocessor.Process | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| event-key-visibility | Event key injection in BeforeModel | R | → `02.5` 从零实现；对位不成立 |
| event-lifecycle | Time-based TTL expiration | M | `changes/03-history/specs/event-lifecycle/spec.md`（头注继承声明） |
| event-lifecycle | Capacity-based eviction | M | `changes/03-history/specs/event-lifecycle/spec.md`（头注继承声明） |
| event-lifecycle | 生命周期策略经声明式配置注入 | M | `changes/03-history/specs/event-lifecycle/spec.md#生命周期策略经声明式配置注入` |
| event-lifecycle | TTL 扫描为周期性全量扫描 | M | `changes/03-history/specs/event-lifecycle/spec.md`（头注继承声明） |
| event-lifecycle | 容量淘汰基于进程内事件计数与最旧段优先 | M | `changes/03-history/specs/event-lifecycle/spec.md`（头注继承声明） |
| event-metadata-contract | 元数据 key 统一定义 | R | → `02.1` 从零实现；旧文仅背景 |
| event-metadata-contract | 注入点职责归一 | R | → `02.1` 从零实现；旧文仅背景 |
| event-metadata-contract | 元数据解析 API | R | → `02.1` 从零实现；旧文仅背景 |
| event-metadata-contract | bundle_id 归因键 | R | → `02.1` 从零实现；旧文仅背景 |
| event-metadata-contract | 结算血统一级键可查询 | R | → `02.1` 从零实现；旧文仅背景 |
| event-metadata-contract | 空血统不写空串一级键 | R | → `02.1` 从零实现；旧文仅背景 |
| event-relation-store | Set and retrieve parent-child event relationships | M | **无 delta**；域声明：`changes/03-history/proposal.md / design.md`「由主 spec 继承或他域落地，仅作证据映射，不出 delta、不声称已认证」（域声明继承，见 §4 备注） |
| event-relation-store | Remove all relationships for an event | M | **无 delta**；域声明：`changes/03-history/proposal.md / design.md`「由主 spec 继承或他域落地，仅作证据映射，不出 delta、不声称已认证」（域声明继承，见 §4 备注） |
| event-relation-store | Persist relations via append-only WAL journal | M | **无 delta**；域声明：`changes/03-history/proposal.md / design.md`「由主 spec 继承或他域落地，仅作证据映射，不出 delta、不声称已认证」（域声明继承，见 §4 备注） |
| event-relation-store | Recover relation graph from snapshot and journal on restart | M | **无 delta**；域声明：`changes/03-history/proposal.md / design.md`「由主 spec 继承或他域落地，仅作证据映射，不出 delta、不声称已认证」（域声明继承，见 §4 备注） |
| event-relation-store | Create and load snapshots | M | **无 delta**；域声明：`changes/03-history/proposal.md / design.md`「由主 spec 继承或他域落地，仅作证据映射，不出 delta、不声称已认证」（域声明继承，见 §4 备注） |
| event-relation-store | RelationStore is in-memory with bounded size | M | **无 delta**；域声明：`changes/03-history/proposal.md / design.md`「由主 spec 继承或他域落地，仅作证据映射，不出 delta、不声称已认证」（域声明继承，见 §4 备注） |
| event-segment-store | Store event as JSON Lines segment by time window | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | seqCounter recovered from existing segment data | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | seq 槽位与事件身份一一对应 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | resolvePartitions 默认隔离（无分区参数返回空） | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | eventCount 在进程生命期内反映实际事件数 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | QueryEvents 召回正确性（最新优先不丢失） | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 时间过滤毫秒契约与窗口剪枝换算 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 窗口剪枝跨度按段层级取值 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 段边界可信性（键范围元数据与 memtable 语义） | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 早停判据以实际事件时间戳为基准 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 时间真相源单一 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 跨窗查询按 EventKey 去重且保留更高层版本 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 双实现查询语义一致与全序确定性 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | FileSegmentStore supports graceful shutdown via Close | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | LocalFileKV Sync 为原子快照屏障 | M | `changes/03-history/specs/event-segment-store/spec.md#LocalFileKV Sync 为原子快照屏障` |
| event-segment-store | 后端不可变与隔离一致性 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 存储失败不可冒充正常缺失 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | compaction 源窗口读失败必须中止本轮 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | TTL 扫描读失败必须可观测 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | ReplayEvent 窗口重入与 StoreEvent 同做 sealed 降级 | M | `changes/03-history/specs/event-segment-store/spec.md`（头注继承声明） |
| event-segment-store | 未确认恢复材料的有限保留租约 | M | `changes/03-history/specs/event-segment-store/spec.md#未确认恢复材料的有限保留租约` |
| event-segment-store | 恢复能力经过包装层真实透传 | M | `changes/03-history/specs/event-segment-store/spec.md#恢复能力经过包装层真实透传` |
| event-sourced-projection | 投影是事实链的纯回放（一等不变量） | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 折叠向事实链追加 compaction 事件 | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | compaction 事件滚动 supersede | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 冷启动回放逐字节重建投影 | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 折叠原文冷存不删、回放按 retained-ref 跳过 | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | compaction 事件可召回且承载综述正文 | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 移除游离快照补丁与 Replace-over-live | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 无锚恢复不静默截断 | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 恢复观测覆盖全误差面 | R | → `03.4` 从零实现；旧文仅背景 |
| event-sourced-projection | 投影的通道分区与统一装配 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | 投影为 LLM 输入的唯一装配源 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | 投影写入与事件存储同点同步 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | BeforeModel 时投影完整性为构造保证 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | 回合内原生、跨回合通知的时间线渲染 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | 任务看板为 user 级独立虚事件 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | 输出侧协议卫生 | R | → `03.4` 从零实现；旧文仅背景 |
| event-timeline-rendering | 渲染合法性不变量 | R | → `03.4` 从零实现；旧文仅背景 |
| event-type-unification | 事件类型常量单一来源 | R | → `02.1` 从零实现；旧文仅背景 |
| event-type-unification | memory 包可引用 event 包无循环依赖 | R | → `02.1` 从零实现；旧文仅背景 |
| event-type-unification | feedback 类型注册 | R | → `02.1` 从零实现；旧文仅背景 |
| event-type-unification | governance 事件非骨架 | R | → `02.1` 从零实现；旧文仅背景 |
| evolution-evaluation | 后验评估锚点(improvement 事件) | M | `changes/07-evolution/specs/evolution-evaluation/spec.md#后验评估锚点(improvement 事件)` |
| evolution-evaluation | 建议式劣化信号(框架不动手) | M | `changes/07-evolution/specs/evolution-evaluation/spec.md`（头注继承声明） |
| evolution-evaluation | bundle 体系退役 | M | `changes/07-evolution/specs/evolution-evaluation/spec.md`（头注继承声明） |
| evolution-evaluation | 改进版本章(feedback join 保持) | M | `changes/07-evolution/specs/evolution-evaluation/spec.md#改进版本章(feedback join 保持)` |
| evolution-evaluation | guardrail 信号可用性显式声明 | M | `changes/07-evolution/specs/evolution-evaluation/spec.md`（头注继承声明） |
| example-file-memory-wiring | FileSegmentStore 作为 memory backend（使用 LocalFileKV） | M | **无 delta**；域声明：`changes/08-host/proposal.md`「localfile memory backend、LocalFileKV、recall 共享 store——继承」（域声明继承，见 §4 备注） |
| example-file-memory-wiring | LocalFileKV 实现 | M | **无 delta**；域声明：`changes/08-host/proposal.md`「localfile memory backend、LocalFileKV、recall 共享 store——继承」（域声明继承，见 §4 备注） |
| example-file-memory-wiring | recall agent 共享 file store | M | **无 delta**；域声明：`changes/08-host/proposal.md`「localfile memory backend、LocalFileKV、recall 共享 store——继承」（域声明继承，见 §4 备注） |
| example-file-memory-wiring | 旧 FileBackend 残留清理 | M | **无 delta**；域声明：`changes/08-host/proposal.md`「localfile memory backend、LocalFileKV、recall 共享 store——继承」（域声明继承，见 §4 备注） |
| example-local-file-delivery | 从 agent 回复中解析本地文件路径 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| example-local-file-delivery | 按扩展名选择微信发送接口 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| example-local-file-delivery | 投递文件并保留原始文本 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| example-local-file-delivery | 文件发送失败不阻断文本与其他文件 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| example-local-file-delivery | workspace_dir 配置驱动相对路径解析 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（头注继承声明） |
| example-local-file-delivery | 通过接口抽象与 mock 实现可测试性 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| example-local-file-delivery | 引导 agent 输出文件路径而非文件内容 | M | `changes/08-host/specs/example-local-file-delivery/spec.md`（未列；按 delta 约定=隐式保留） |
| example-rl-visibility | HTTPAPI 服务启动 | M | `changes/08-host/specs/example-rl-visibility/spec.md`（头注继承声明） |
| example-rl-visibility | 健康检查端点 | M | `changes/08-host/specs/example-rl-visibility/spec.md`（头注继承声明） |
| example-rl-visibility | run.sh 可观测环境默认值 | M | `changes/08-host/specs/example-rl-visibility/spec.md`（头注继承声明） |
| failure-polarity | 进程死亡按退出码分流极性 | R | → `04.2` 从零实现；旧文仅背景 |
| failure-polarity | 探测不可辨超限报失败极性 | R | → `04.2` 从零实现；旧文仅背景 |
| failure-polarity | kill 失败强拆报失败极性并提示逃逸 | R | → `04.2` 从零实现；旧文仅背景 |
| failure-polarity | 结算信号与通知携带 exit_code | R | → `04.2` 从零实现；旧文仅背景 |
| failure-polarity | 纯空白载荷通知降级 | R | → `04.2` 从零实现；旧文仅背景 |
| failure-polarity | 工具描述引导退出码透传 | R | → `04.2` 从零实现；旧文仅背景 |
| feedback-binding | feedback 事件类型 | R | → `06.4` 从零实现；旧文仅背景 |
| feedback-binding | 回执-反馈因果绑定 | R | → `06.4` 从零实现；旧文仅背景 |
| feedback-binding | guardrail 负反馈判据 | R | → `06.4` 从零实现；旧文仅背景 |
| framework-flow-adapter | Adapter converts a batch of AgentEvents into a framework Invocation | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter executes the framework Flow and forwards outputs | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter registers SmartCompressor as BeforeModel callback | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter registers Compactor as second BeforeModel callback | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter preserves tmux asynchronous semantics | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter supports sub-agent invocation mode | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter converts a batch of AgentEvents into a framework Invocation | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter executes the framework Flow and forwards outputs | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter registers SmartCompressor and Compactor as BeforeModel callbacks | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter preserves tmux asynchronous semantics | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-flow-adapter | Adapter supports sub-agent invocation mode | H | 机制退役：无 flow adapter/第二工具循环；行为面由 02 persistent-event-loop delta（请求装配/代绑定）承载 |
| framework-prompt | tagent framework prompt injected before user system prompt | R | → `02.5` 从零实现；旧文仅背景 |
| generation-bound-model-references | 覆盖解析与实际执行同面 | M | **无 delta**；域声明：`changes/01-state-binding/design.md（覆盖映射 01.5 继承源块）`「模型引用绑定代，只读」（域声明继承，见 §4 备注） |
| generation-bound-model-references | 模型缓存标识完整 | M | **无 delta**；域声明：`changes/01-state-binding/design.md（覆盖映射 01.5 继承源块）`「模型引用绑定代，只读」（域声明继承，见 §4 备注） |
| generation-bound-model-references | 不支持的热更明确拒绝 | M | **无 delta**；域声明：`changes/01-state-binding/design.md（覆盖映射 01.5 继承源块）`「模型引用绑定代，只读」（域声明继承，见 §4 备注） |
| generation-bound-model-references | 动态性不扩张执行权 | M | **无 delta**；域声明：`changes/01-state-binding/design.md（覆盖映射 01.5 继承源块）`「模型引用绑定代，只读」（域声明继承，见 §4 备注） |
| git-native-refine | refine register 登记自我改进 | R | → `07.2` 从零实现；旧文仅背景 |
| git-native-refine | refine status 改进台账 | R | → `07.2` 从零实现；旧文仅背景 |
| git-native-refine | refine rollback 安全回滚 | R | → `07.2` 从零实现；旧文仅背景 |
| git-native-refine | 治理门控可配置且默认宽松 | R | → `07.2` 从零实现；旧文仅背景 |
| git-native-refine | 溢出全文取回面 | R | → `07.2` 从零实现；旧文仅背景 |
| git-native-refine | 审批直投通道(宿主装配) | R | → `07.2` 从零实现；旧文仅背景 |
| goal-tools | goal 工具族（entry only） | R | → `04.6` 从零实现；旧文仅背景 |
| goal-tools | goal 持久化重建 | R | → `04.6` 从零实现；旧文仅背景 |
| inbound-intent-declaration | /task 注入可选声明意图血统 | R | → `04.4` 从零实现；旧文仅背景 |
| inbound-intent-declaration | 声明受理 fail-closed 三重校验 | R | → `04.4` 从零实现；旧文仅背景 |
| inbound-intent-declaration | 白名单语义与声明能力隔离 | R | → `04.4` 从零实现；旧文仅背景 |
| llm-event-valuation | EventValuator interface evaluates events in batch | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| llm-event-valuation | LLM-based valuation outputs structured JSON | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| llm-event-valuation | Batch valuation combines with summary in one LLM call | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| llm-event-valuation | Value score respects event type floor | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| llm-summary-generator | LLMSummarizer implements SummaryGenerator interface | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| llm-summary-generator | Batched LLM calls reduce round-trip overhead | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| llm-summary-generator | Configuration gates LLM summarization | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| maintainability-audit | 审计只读性 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| maintainability-audit | 逐包评分卡 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| maintainability-audit | 发现登记纪律 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| maintainability-audit | 汇总台账与改进 backlog | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| maintainability-audit | 审计方法可复现 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| mcp-call-tool | 恒定声明的通用执行网关 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-call-tool | 注册表解析与直调 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-call-tool | 自纠错误反馈 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-call-tool | 空注册表行为 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-discovery | 基于实时注册表的发现 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-discovery | 如实的调用指引 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-discovery | 空注册表与无匹配行为 | R | → `04.3` 从零实现；旧文仅背景 |
| mcp-server-registry | 声明式 MCP server 配置 | M | `changes/04-capabilities/specs/mcp-server-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| mcp-server-registry | transport 归一化 | M | `changes/04-capabilities/specs/mcp-server-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| mcp-server-registry | 认证 header 组装 | M | `changes/04-capabilities/specs/mcp-server-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| mcp-server-registry | 运行时注册表 | M | `changes/04-capabilities/specs/mcp-server-registry/spec.md#运行时注册表` |
| mcp-server-registry | 配置文件热同步 | M | `changes/04-capabilities/specs/mcp-server-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| mcp-server-registry | 构建期装配与生命周期 | M | `changes/04-capabilities/specs/mcp-server-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| meditation-agent-partition | 冥想 agent 经配置声明并以授权分区为观察面 | M | `changes/07-evolution/specs/meditation-agent-partition/spec.md`（未列；按 delta 约定=隐式保留） |
| meditation-agent-partition | novelty 判据为跨分区非自管谱系新事件 | M | `changes/07-evolution/specs/meditation-agent-partition/spec.md`（头注继承声明） |
| meditation-agent-partition | 门控与锚点沿用既有语义 | M | `changes/07-evolution/specs/meditation-agent-partition/spec.md`（未列；按 delta 约定=隐式保留） |
| meditation-agent-partition | 冥想 agent 的产出为普通事件 | M | `changes/07-evolution/specs/meditation-agent-partition/spec.md`（未列；按 delta 约定=隐式保留） |
| meditation-agent-partition | 反思事件注入本 agent 的循环 session | M | `changes/07-evolution/specs/meditation-agent-partition/spec.md`（未列；按 delta 约定=隐式保留） |
| meditation-delivery-tool | deliver 工具仅授予有回流授权的冥想 agent | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-delivery-tool | 调用时过投递缝全部安全门且拒绝以结果文本回模型 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-delivery-tool | 投递内容有界 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-idle-gating | 冥想触发采用双闸门判定 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-idle-gating | 门控不依赖输出侧血统追踪 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-idle-gating | 混合批次中丢弃冥想事件 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-idle-gating | 水位执行语义与让位补位 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-self-state-digest | 冥想事件携带自我状态 digest | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-self-state-digest | digest 覆盖任务层健康与空闲时长 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-self-state-digest | digest 有界渲染 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-self-state-digest | 无任务层时优雅降级 | R | → `07.1` 从零实现；旧文仅背景 |
| meditation-self-state-digest | 冥想总结以高亮卡片行沉淀 | R | → `07.1` 从零实现；旧文仅背景 |
| memory-curation | 记忆三原语与固化级联 | M | `changes/04-capabilities/specs/memory-curation/spec.md`（未列；按 delta 约定=隐式保留） |
| memory-curation | 卡片序列（历史表示的唯一对象） | M | `changes/04-capabilities/specs/memory-curation/spec.md`（未列；按 delta 约定=隐式保留） |
| memory-curation | 冥想总结以高亮卡片行沉淀 | M | `changes/04-capabilities/specs/memory-curation/spec.md`（未列；按 delta 约定=隐式保留） |
| offline-training-export | 授权的只读事实导出 | M | `changes/06-learning/specs/offline-training-export/spec.md`（未列；按 delta 约定=隐式保留） |
| offline-training-export | 精确关联不等同自动奖励 | M | `changes/06-learning/specs/offline-training-export/spec.md`（未列；按 delta 约定=隐式保留） |
| offline-training-export | 工具感知SFT与可靠loss边界 | M | `changes/06-learning/specs/offline-training-export/spec.md`（未列；按 delta 约定=隐式保留） |
| offline-training-export | 数据集完整性与隔离 | M | `changes/06-learning/specs/offline-training-export/spec.md`（未列；按 delta 约定=隐式保留） |
| offline-training-export | 真实本地消费验收 | M | `changes/06-learning/specs/offline-training-export/spec.md#真实本地消费验收` |
| ops-deployment-integrity | 运维探针认证集成 | R | → `09.5` 从零实现；旧文仅背景 |
| ops-deployment-integrity | 部署状态链完整 | R | → `09.5` 从零实现；旧文仅背景 |
| ops-deployment-integrity | HTTP 服务宿主持有 | R | → `09.5` 从零实现；旧文仅背景 |
| orchestration | 隔离并发与新语义优先 | M | `specs/orchestration/spec.md#隔离并发与新语义优先` |
| orchestration | 只有集成路径作为行为证据 | M | `specs/orchestration/spec.md#只有集成路径作为行为证据` |
| partition-local-storage-access | 分区访问局部性与等价回退 | D | §7 待核·局部性/性能主张需同条件证据 |
| partition-local-storage-access | 查询优化不改事实语义 | D | §7 待核·局部性/性能主张需同条件证据 |
| partition-local-storage-access | 同步屏障与磁盘格式不变 | D | §7 待核·局部性/性能主张需同条件证据 |
| partition-local-storage-access | 关系失败不发布虚假成功 | D | §7 待核·局部性/性能主张需同条件证据 |
| partition-local-storage-access | 性能主张具有同条件证据 | D | §7 待核·局部性/性能主张需同条件证据 |
| per-call-subagent-overrides | 调用时覆盖子 agent 视图 | M | `changes/01-state-binding/specs/per-call-subagent-overrides/spec.md#调用时覆盖子 agent 视图` |
| per-call-subagent-overrides | 最大工具域硬上界 | M | `changes/01-state-binding/specs/per-call-subagent-overrides/spec.md`（头注继承声明） |
| per-call-subagent-overrides | 覆盖冻结与重放 | M | `changes/01-state-binding/specs/per-call-subagent-overrides/spec.md`（头注继承声明） |
| persistent-event-loop | Loop goroutine 持续运行不退出 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 批量 drain mailbox | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | mergeBatch 合并批量消息 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | InjectMessage 双模式 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | task_settled 作为一等事件触发回收 turn | M | `changes/02-execution/specs/persistent-event-loop/spec.md#task_settled 作为一等事件触发回收 turn` |
| persistent-event-loop | 循环不依赖 bus echo 自触发 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | outputCh 宽限与溢出落盘 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 循环停止为终结态 | M | `changes/02-execution/specs/persistent-event-loop/spec.md#循环停止为终结态` |
| persistent-event-loop | 可判定的接收结果 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 可靠输入全序持久化 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 输入处理确认与幂等 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | LLM 端点重定向逐跳受 allowlist 约束 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 业务 turn 开始执行时绑定编排版本 | M | `changes/02-execution/specs/persistent-event-loop/spec.md#业务 turn 开始执行时绑定编排版本` |
| persistent-event-loop | 瞬时提交失败的重排覆盖完整冻结接收集 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 不可解码 slot 触发整封隔离 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | per-turn echo 凭据随 turn 清理统一回收 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 可靠模式的恢复能力与格式准入 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 启动直接核对未确认完成证据 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| persistent-event-loop | 模型入口错误以失败极性呈现 | M | `changes/02-execution/specs/persistent-event-loop/spec.md#模型入口错误以失败极性呈现` |
| persistent-event-loop | inbox 写者唯一性与目录单属主 | M | `changes/02-execution/specs/persistent-event-loop/spec.md`（头注继承声明） |
| plan-agent | plan agent implements dual-mode Run via custom Run method | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | plan tool uses structured action parameter | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | tagent does not directly operate openspec files | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | PlanProgressTracker removed | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | FrameworkPrompt does not hardcode plan tool | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | plan sub-agent manages openspec work plans via dual-mode operation | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | plan_tool_desc.md documents dual-mode operation | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | PlanProgressTracker removed | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | FrameworkPrompt does not hardcode plan tool | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | plan agent 创建流程产出符合 openspec 官方模板 | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | plan agent 按任务性质自主决策合规级别 | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | tasks.md 遵循官方模板并按级别收尾 | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | plan agent 产出物边界（不代工） | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | 同名计划任务单飞 | D | §7 待核·plan agent 是否进入新默认实施 |
| plan-agent | 文件工具路径基准双列对照 | D | §7 待核·plan agent 是否进入新默认实施 |
| production-wiring | Production wiring creates and starts all lifecycle subsystems | M | `changes/08-host/specs/production-wiring/spec.md`（未列；按 delta 约定=隐式保留） |
| production-wiring | Subsystem references injected via setter methods | M | `changes/08-host/specs/production-wiring/spec.md`（未列；按 delta 约定=隐式保留） |
| production-wiring | Close follows strict shutdown order | M | `changes/08-host/specs/production-wiring/spec.md`（头注继承声明） |
| production-wiring | 配置严格解析 | M | `changes/08-host/specs/production-wiring/spec.md`（头注继承声明） |
| production-wiring | agent 引用环构建期检测 | M | `changes/08-host/specs/production-wiring/spec.md`（头注继承声明） |
| production-wiring-fix | 生产入口创建并启动生命周期组件 | R | → `08.1` 根包从零装配；旧 setter 注入/Close 顺序修复仅背景 |
| production-wiring-fix | FileSegmentStore 支持构造后注入 TombstoneSet | R | → `08.1` 根包从零装配；旧 setter 注入/Close 顺序修复仅背景 |
| production-wiring-fix | 优雅关闭流程 | R | → `08.1` 根包从零装配；旧 setter 注入/Close 顺序修复仅背景 |
| production-wiring-fix | wiki 记录 SessionService AppendEventHook | R | → `08.1` 根包从零装配；旧 setter 注入/Close 顺序修复仅背景 |
| production-wiring-fix | wiki 记录 resolveMemoryStore 完整组装 | R | → `08.1` 根包从零装配；旧 setter 注入/Close 顺序修复仅背景 |
| prompt-loader-fallback | 框架默认 prompt 随二进制嵌入 | R | → `01.6` 从零实现；旧文仅背景 |
| prompt-loader-fallback | 缺失 prompt 回退到嵌入默认 | R | → `01.6` 从零实现；旧文仅背景 |
| prompt-loader-fallback | 目录级回退（整目录） | R | → `01.6` 从零实现；旧文仅背景 |
| prompt-loader-fallback | 未配置 fallback 时行为不变 | R | → `01.6` 从零实现；旧文仅背景 |
| prototype-mvp | 可运行的事件循环与协作式退出 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | 工具任务、错误与调用归属 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | state 的单一运行时权威 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | 批次级热更新快照 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | 事实记录、真实票据和有界投影 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | 手动反思与来源保真 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | 每文件边界说明与 MVP README | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| prototype-mvp | 可复跑的行为验证 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| race-gate-coverage | race 门覆盖第一方全包 | R | → `09.3` 从零实现；旧文仅背景 |
| readme-enhancement | README 包含项目架构概览 | R | → `09.6` 文档闭合时重写 |
| readme-enhancement | README 包含核心数据流说明 | R | → `09.6` 文档闭合时重写 |
| readme-enhancement | README 包含快速启动指引 | R | → `09.6` 文档闭合时重写 |
| recall-hybrid-fusion | query 模式 RRF 融合 | R | → `04.7` 从零实现；旧文仅背景 |
| recall-hybrid-fusion | 两段式召回哲学保持 | R | → `04.7` 从零实现；旧文仅背景 |
| recall-hybrid-fusion | recall 工具声明恒定 | R | → `04.7` 从零实现；旧文仅背景 |
| recall-protocol | 索引卡为召回标准协议（输入形态分流） | M | `changes/03-history/specs/recall-protocol/spec.md#索引卡为召回标准协议（输入形态分流）` |
| recall-protocol | RecallAgent 定位收窄 | M | `changes/03-history/specs/recall-protocol/spec.md`（头注继承声明） |
| recall-protocol | 查询类召回结果的诚实截断提示 | M | `changes/03-history/specs/recall-protocol/spec.md`（头注继承声明） |
| recall-protocol | memory_turn 因果链召回工具 | M | `changes/03-history/specs/recall-protocol/spec.md`（头注继承声明） |
| recall-protocol | 卡片行标注可追溯提示 | M | `changes/03-history/specs/recall-protocol/spec.md`（头注继承声明） |
| recall-protocol | items 批量水合有界 | M | `changes/03-history/specs/recall-protocol/spec.md`（头注继承声明） |
| relation-store-lru-rocks | RelationStore uses LRU hot cache backed by RocksDB | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| relation-store-lru-rocks | GetChildren uses reverse index with range scan + limit | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| relation-store-lru-rocks | RelationStore drops journal/snapshot methods | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| relation-store-lru-rocks | RelationStore Close flushes dirty LRU entries | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| relation-store-provider | RelationStoreProvider interface defined | R | → `03.1` 从零实现；旧文仅背景 |
| relation-store-provider | Callers use RelationStoreProvider instead of anonymous interface | R | → `03.1` 从零实现；旧文仅背景 |
| remote-agent-communication | AgentToolWrapper accepts agent.Agent interface | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | Context passed via RuntimeState | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | ExternalContextEntry serialization format | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | TagentAgent.Run reads RuntimeState | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | Remote sub-agent via ToolRef.Remote | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | A2A metadata automatic mapping | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | TagentAgent as A2A Server | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| remote-agent-communication | Configuration layer separation | M | `changes/04-capabilities/specs/remote-agent-communication/spec.md`（未列；按 delta 约定=隐式保留） |
| request-budget-accounting | 单次请求声明快照 | R | → `03.4` 从零实现；旧文仅背景 |
| request-budget-accounting | 完整预算输入与诚实精度 | R | → `03.4` 从零实现；旧文仅背景 |
| request-budget-accounting | 单一压缩及安全消费 | R | → `03.4` 从零实现；旧文仅背景 |
| request-budget-accounting | 同步摘要可界定等待 | R | → `03.4` 从零实现；旧文仅背景 |
| resident-continuity | 状态与执行器分离不变量 | M | changes/01-state-binding/specs/config-state-twinship/spec.md#生效 system prompt 由 state 持有（正文对位，已核） |
| resident-continuity | 状态层事件溯源模式 | R | → `01.5` 从零实现；对位不成立 |
| resident-continuity | 执行器可换语义 | M | changes/01-state-binding/specs/config-state-twinship/spec.md#候选活绑定准备与同代借用（正文对位，已核） |
| resident-continuity | 路线图阶段治理 | R | → `01.5` 从零实现；对位不成立 |
| resident-continuity | 请求开始执行时固定编排执行绑定 | M | changes/01-state-binding/specs/config-state-twinship/spec.md#候选活绑定准备与同代借用（正文对位，已核） |
| resident-continuity | 发布回滚与常驻身份保持 | R | → `01.5` 从零实现；对位不成立 |
| resident-continuity | 编排热更不要求跨进程现场恢复 | R | → `01.5` 从零实现；对位不成立 |
| resident-continuity | 入口身份变更在资源获取前拒绝 | R | → `01.5` 从零实现；对位不成立 |
| resident-continuity | owner 收敛后退役与组织关闭 | R | → `01.5` 从零实现；对位不成立 |
| resident-release-evidence | 验证工具必须对底层失败保真 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（未列；按 delta 约定=隐式保留） |
| resident-release-evidence | 证据分层与可复现性 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-release-evidence | 双模块与真实消费者验收 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-release-evidence | 常驻准出证据 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-release-evidence | 发布与外部操作权限 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-release-evidence | 编排热更以生产委派变化验收 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-release-evidence | 安全验收不由统计或豁免替代 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-release-evidence | 验收覆盖完整装配链与复杂度收敛 | M | `changes/09-acceptance/specs/resident-release-evidence/spec.md`（头注继承声明） |
| resident-session-continuity | 常驻会话元数据完备并入事实链 | M | `changes/04-capabilities/specs/resident-session-continuity/spec.md`（未列；按 delta 约定=隐式保留） |
| resident-session-continuity | 会话枚举修复（n- 与 prefix 不相交死代码） | M | `changes/04-capabilities/specs/resident-session-continuity/spec.md`（未列；按 delta 约定=隐式保留） |
| resident-session-continuity | 存活探测三态化（dead/unknown/dead 可辨） | M | `changes/04-capabilities/specs/resident-session-continuity/spec.md`（未列；按 delta 约定=隐式保留） |
| resident-session-continuity | 启动重挂与任务重关联 | M | `changes/04-capabilities/specs/resident-session-continuity/spec.md`（未列；按 delta 约定=隐式保留） |
| resident-session-continuity | 长驻会话无年龄死刑 | M | `changes/04-capabilities/specs/resident-session-continuity/spec.md`（未列；按 delta 约定=隐式保留） |
| resident-session-continuity | 重挂后信号链真实接通 | M | `changes/04-capabilities/specs/resident-session-continuity/spec.md`（未列；按 delta 约定=隐式保留） |
| rl-feedback | 诊断快照消费面 | M | `changes/06-learning/specs/rl-feedback/spec.md`（未列；按 delta 约定=隐式保留） |
| rl-feedback | RL 反馈 long-poll 通道 | M | `changes/06-learning/specs/rl-feedback/spec.md`（未列；按 delta 约定=隐式保留） |
| rl-feedback | RL HTTP API 认证与监听约束 | M | `changes/06-learning/specs/rl-feedback/spec.md`（未列；按 delta 约定=隐式保留） |
| rl-feedback | HTTP 输入与通知资源有界 | M | `changes/06-learning/specs/rl-feedback/spec.md`（未列；按 delta 约定=隐式保留） |
| rl-feedback | 请求取消与接收回执 | M | `changes/06-learning/specs/rl-feedback/spec.md`（未列；按 delta 约定=隐式保留） |
| rl-feedback | 动态端点是显式授权管理能力 | M | `changes/06-learning/specs/rl-feedback/spec.md#动态端点是显式授权管理能力` |
| rl-feedback | 认证与运维同源 | M | `changes/06-learning/specs/rl-feedback/spec.md`（未列；按 delta 约定=隐式保留） |
| roadmap-governance | 阶段依赖与准入条件 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| roadmap-governance | 阶段准出门禁 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| roadmap-governance | 真实 LLM 测试失败的定性义务 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| roadmap-governance | 不变量声明义务 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| roadmap-governance | sub-agent 分工惯例 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| roadmap-governance | 归档义务与父路线图状态同步 | H | 不复活（旧机制退役；§10.4 抽查无 delta 误恢复） |
| runtime-resource-ownership | 运行时资源租约与关闭 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 持久目录单 writer | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | agent 身份隔离 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 构建失败回收登记先于可失败接线 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 可等待关闭与未确认停止保有 owner | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 组织版本引用覆盖实际执行 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 实际生产者完成独立于处理后事件流 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 递归获取责任与关闭责任唯一 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 版本持有全部合法本地依赖的使用权 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 有界关闭返回不丢失最终释放责任 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| runtime-resource-ownership | 恢复登记先于遗忘生产者 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力只只读呈现资源持有者，不做获取/释放」（张力未决，见 §10.6） |
| rustviking-client | KV range scan for time-based queries | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-client | KV batch operations for compaction | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-client | RustViking CLI exposes kv range subcommand | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-client | RustVikingClient is CLI-only | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-client | Batch operations are atomic | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-config | MemoryConfig supports RustViking binary path | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-config | Production MemoryStore wiring uses real RustVikingClient | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-range-scan | RustViking CLI exposes native range scan | R | → `03.2` 从零实现；旧文仅背景 |
| rustviking-range-scan | Go client uses range command instead of client-side filtering | R | → `03.2` 从零实现；旧文仅背景 |
| self-improvement-meditation | 冥想产物纪律(登记闭环) | M | **无 delta**；域声明：`changes/07-evolution/proposal.md`「产物纪律/登记闭环、负反馈回顾、digest 让位欠账——继承」（域声明继承，见 §4 备注） |
| self-improvement-meditation | 冥想定位叙事(自我改进引擎) | M | **无 delta**；域声明：`changes/07-evolution/proposal.md`「产物纪律/登记闭环、负反馈回顾、digest 让位欠账——继承」（域声明继承，见 §4 备注） |
| self-improvement-meditation | 负反馈回顾(反思素材) | M | **无 delta**；域声明：`changes/07-evolution/proposal.md`「产物纪律/登记闭环、负反馈回顾、digest 让位欠账——继承」（域声明继承，见 §4 备注） |
| self-telemetry-audit | 自管遥测占比指标与分级动作 | R | → `05.3` 从零实现；旧文仅背景 |
| self-telemetry-audit | L3 冻结与 disk block spawn 同构 | R | → `05.3` 从零实现；旧文仅背景 |
| self-telemetry-audit | 保护性任务豁免白名单 | R | → `05.3` 从零实现；旧文仅背景 |
| semantic-search | 异步向量生成流水线 | R | → `04.7` 从零实现；旧文仅背景 |
| semantic-search | 向量持久化与启动重建 | R | → `04.7` 从零实现；旧文仅背景 |
| semantic-search | SearchByEmbedding 落地 | R | → `04.7` 从零实现；旧文仅背景 |
| semantic-search | embedder 配置与降级 | R | → `04.7` 从零实现；旧文仅背景 |
| semantic-search | 向量链路可观测 | R | → `04.7` 从零实现；旧文仅背景 |
| snowflake-overflow-handling | Snowflake blocks on same-millisecond sequence exhaustion | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| snowflake-overflow-handling | Clock regression aborts generation | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| startup-active-partition-bitmap | Active partition bitmap drives Init discovery | D | §7 待核·位图/游标是否以等价形态采纳 |
| startup-active-partition-bitmap | Per-partition cursor replaces seq scanning | D | §7 待核·位图/游标是否以等价形态采纳 |
| startup-active-partition-bitmap | Init cold-start latency bounded by active partition count | D | §7 待核·位图/游标是否以等价形态采纳 |
| storage-lifecycle-completion | L1 to L2 compaction is automatically scheduled | M | `changes/03-history/specs/storage-lifecycle-completion/spec.md`（头注继承声明） |
| storage-lifecycle-completion | L2 to L3 compaction is automatically scheduled | M | `changes/03-history/specs/storage-lifecycle-completion/spec.md`（头注继承声明） |
| storage-lifecycle-completion | filterTombstoned removes tombstoned events | M | `changes/03-history/specs/storage-lifecycle-completion/spec.md`（头注继承声明） |
| storage-lifecycle-completion | TTL check uses actual event timestamp | M | `changes/03-history/specs/storage-lifecycle-completion/spec.md`（头注继承声明） |
| subagent-turn-execution | Sub-agent 调用执行恰好一个 turn | M | `changes/02-execution/specs/subagent-turn-execution/spec.md#Sub-agent 调用执行恰好一个 turn` |
| subagent-turn-execution | 最终响应识别区分 assistant 响应与工具结果 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md`（未列；按 delta 约定=隐式保留） |
| subagent-turn-execution | Sub-agent 保持并发隔离与调用契约不变 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md#Sub-agent 保持并发隔离与调用契约不变` |
| subagent-turn-execution | Sub-agent 调用超时对多轮工作宽容 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md`（头注继承声明） |
| subagent-turn-execution | 子 agent 调用可作为异步任务执行 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md#子 agent 调用可作为异步任务执行` |
| subagent-turn-execution | 子 agent 委派使用同代目标绑定 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md`（头注继承声明） |
| subagent-turn-execution | 真实委派上下文穿透工具装饰层 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md`（未列；按 delta 约定=隐式保留） |
| subagent-turn-execution | 投递对账屏障的 booking 与投递期望一一配对 | M | `changes/02-execution/specs/subagent-turn-execution/spec.md#投递对账屏障的 booking 与投递期望一一配对` |
| summary-memory-rag | Compressed summaries are persisted to MemoryStore | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| summary-memory-rag | Context replaces compressed events with reference messages | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| summary-memory-rag | Recall retrieves archived summaries by summary key | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| summary-memory-rag | Summary storage deduplicates identical inputs | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| swappable-executor | 执行器可热换（cm.runner 级换代，非重启） | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | fingerprint 检测与 memory 拒绝（懒检查先序） | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | build-validate-then-swap 可逆（fail-closed，副作用 ownership） | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | 触发时机与观测 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | 退役执行器与模型延迟回收 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | Rollback 手动触发面 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | 逐 agent 执行代与有效配置一致 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | 整份编排执行绑定发布 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | 单一应用记录与请求获取一致 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| swappable-executor | SwappableModel 迭代路径与 channel 路径错误面一致 | H | 机制退役：state 生效绑定+借用替代可交换执行器抽象；换代语义由 01 workflow-config-compilation delta 承载 |
| task-reentry | resume_task 重入原语 | R | → `02.7` 从零实现；对位不成立 |
| task-reentry | 特异出入口 | R | → `02.7` 从零实现；对位不成立 |
| task-reentry | plan 计划生命周期经 resume_task 续行 | R | → `02.7` 从零实现；对位不成立 |
| task-reentry | 重建任务的 resume nil 安全 | R | → `02.7` 从零实现；对位不成立 |
| task-reentry | nil 接口模式级审计留痕 | R | → `02.7` 从零实现；对位不成立 |
| task-registry-and-board | TaskManager 内存 registry 与幂等 spawn | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力是跨维度只读总图 + 授权/快照/游标/订阅，不渲染看板、不改 registry」（张力未决，见 §10.6） |
| task-registry-and-board | live 任务看板投影 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力是跨维度只读总图 + 授权/快照/游标/订阅，不渲染看板、不改 registry」（张力未决，见 §10.6） |
| task-registry-and-board | 自包含的 task_settled 事件 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力是跨维度只读总图 + 授权/快照/游标/订阅，不渲染看板、不改 registry」（张力未决，见 §10.6） |
| task-registry-and-board | LLM 任务操作工具 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力是跨维度只读总图 + 授权/快照/游标/订阅，不渲染看板、不改 registry」（张力未决，见 §10.6） |
| task-registry-and-board | 看板渲染数据源跨重启连续 | M | **无 delta**；域声明：`changes/05-inspection/proposal.md`「本能力是跨维度只读总图 + 授权/快照/游标/订阅，不渲染看板、不改 registry」（张力未决，见 §10.6） |
| task-registry-rebuild | 重建任务不得因缺失探测器而崩溃 | M | `changes/03-history/specs/task-registry-rebuild/spec.md`（头注继承声明） |
| task-registry-rebuild | 转世孤儿任务必须可回收 | M | `changes/03-history/specs/task-registry-rebuild/spec.md`（头注继承声明） |
| task-registry-rebuild | 任务身份与世系跨重启保真 | M | `changes/03-history/specs/task-registry-rebuild/spec.md`（头注继承声明） |
| task-registry-rebuild | 恢复与显式重投按当前编排绑定 | M | `changes/03-history/specs/task-registry-rebuild/spec.md#恢复与显式重投按当前编排绑定` |
| task-skeleton-compression | 以 agent_output 为段边界切分任务回合 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 段内事件按事件类型二分骨架与中间事件 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 中间事件按 tool > assistant 优先级丢弃 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 骨架仍超预算时触发多段压缩归档 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 进行中段永不归档且始终保留 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 保留消息携带 event key 前缀以衔接 rolling summary | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | LLM 文摘作为工程层之上的可选叠加层 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 滚动摘要消息豁免 L3 压缩（常驻可见） | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 段定级采用指数段龄边界且封顶于 L2 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 压缩配置参数公式化默认值 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 纯工具调用 thinking_plan 生成工具调用摘要 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 老化工具运行折叠为工具链合成引用 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 活跃前沿与近期配对不折叠 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 上下文维护五项不变量 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 压缩触发器单维化（token 容量阈值） | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 整理间渲染冻结（前缀字节稳定） | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 浓缩卡片票据机器校验 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 卡片预算与失去导航显式化 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | task settled 类外部输入纳入票据化折叠 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 批量退役在源头汇总为单事件 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | 浓缩导航丢失可观测 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| task-skeleton-compression | L3 预算升级的未消费遥测豁免 | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| telemetry-channel | 遥测通知的通道身份与卡片形态 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| telemetry-channel | 消费状态决定遥测退出时点 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| telemetry-channel | compaction 豁免未消费遥测 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| telemetry-channel | 召回暂存的生命周期闭环 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| telemetry-channel | 跨重启消费状态重建 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| telemetry-channel | 结算通知识别依据结构化标记 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| telemetry-channel | 外显判定与投递门同源 | R | → `05.3` OTel/审计从零接入；旧通道身份条款仅背景 |
| thinking-plan-event-classification | thinking_plan event type classification | R | → `02.1` 从零实现；旧文仅背景 |
| thinking-plan-event-classification | thinking_plan as special event type | R | → `02.1` 从零实现；旧文仅背景 |
| thinking-plan-event-classification | Event view transformation displays thinking_plan type | R | → `02.1` 从零实现；旧文仅背景 |
| tombstone-partition-isolation | TombstoneSet is per-partition | R | → `03.5` 从零实现；旧文仅背景 |
| tombstone-partition-isolation | TombstoneSet auto-recovers from KV on initialization | R | → `03.5` 从零实现；旧文仅背景 |
| tool-lifecycle-management | 内置工具通过 RegisterBuiltinTools 统一注册 | M | `changes/04-capabilities/specs/tool-lifecycle-management/spec.md#内置工具通过 RegisterBuiltinTools 统一注册` |
| tool-lifecycle-management | buildAgent 对所有内置 agent 走 config-driven 路径 | M | `changes/04-capabilities/specs/tool-lifecycle-management/spec.md`（未列；按 delta 约定=隐式保留） |
| tool-output-interception | 工具输出大小拦截包装器 | M | `changes/03-history/specs/tool-output-interception/spec.md#工具输出大小拦截包装器` |
| tool-output-interception | 拦截阈值与 MaxTokens 对齐 | M | `changes/03-history/specs/tool-output-interception/spec.md`（头注继承声明） |
| tool-output-interception | OutputLimitTool 实现 trpctool.Tool 接口 | M | `changes/03-history/specs/tool-output-interception/spec.md`（头注继承声明） |
| tool-security-alignment | tmux_exec 模式接收并使用安全配置 | R | → `04.2` 从零实现；旧文仅背景 |
| tool-security-alignment | exec 与 tmux_exec 安全对称 | R | → `04.2` 从零实现；旧文仅背景 |
| tool-security-alignment | RestartSession 保持安全上下文 | R | → `04.2` 从零实现；旧文仅背景 |
| trajectory-recording | TrajectoryRecorder wraps model.Model | M | `changes/06-learning/specs/trajectory-recording/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-recording | JSONL trajectory format | M | `changes/06-learning/specs/trajectory-recording/spec.md#JSONL trajectory format` |
| trajectory-recording | Async disk writing | M | `changes/06-learning/specs/trajectory-recording/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-recording | Configuration via tagent.yaml | M | `changes/06-learning/specs/trajectory-recording/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-recording | Composability with SwappableModel | M | `changes/06-learning/specs/trajectory-recording/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-recording | Conversion to AReaL SFT dataset | M | `changes/06-learning/specs/trajectory-recording/spec.md#Conversion to AReaL SFT dataset` |
| trajectory-trace-correlation | 轨迹记录携带 trace 关联字段 | M | `changes/05-inspection/specs/trajectory-trace-correlation/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-trace-correlation | trace 反向定位轨迹 | M | `changes/05-inspection/specs/trajectory-trace-correlation/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-verification | 重启点身份配对 | M | `changes/06-learning/specs/trajectory-verification/spec.md`（头注继承声明） |
| trajectory-verification | 截尾即失败 | M | `changes/06-learning/specs/trajectory-verification/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-verification | 动态系统段单列 | M | `changes/06-learning/specs/trajectory-verification/spec.md`（未列；按 delta 约定=隐式保留） |
| trajectory-verification | 验收失败必须可见 | M | `changes/06-learning/specs/trajectory-verification/spec.md`（未列；按 delta 约定=隐式保留） |
| turn-tracing | turn 级 root span | R | → `05.3` 从零实现；旧文仅背景 |
| turn-tracing | 异步任务 span link | R | → `05.3` 从零实现；旧文仅背景 |
| turn-tracing | noop 降级零影响 | R | → `05.3` 从零实现；旧文仅背景 |
| turn-tracing | 内部路径轻量 span | R | → `05.3` 从零实现；旧文仅背景 |
| unified-task-semantics | 任务统一语义 | M | `changes/02-execution/specs/unified-task-semantics/spec.md#任务统一语义` |
| unified-task-semantics | 中间输出为内部输入事件 | M | `changes/02-execution/specs/unified-task-semantics/spec.md`（头注继承声明） |
| unified-task-semantics | 可选聚合窗不控制粒度 | M | `changes/02-execution/specs/unified-task-semantics/spec.md`（头注继承声明） |
| unified-task-semantics | 演进命名与治理插件分工 | M | `changes/02-execution/specs/unified-task-semantics/spec.md#演进命名与治理插件分工` |
| unified-tool-registry | ToolRegistry 统一工具注册中心 | M | `changes/04-capabilities/specs/unified-tool-registry/spec.md#ToolRegistry 统一工具注册中心` |
| unified-tool-registry | PlainToolFactoryConfig 携带运行时依赖 | M | `changes/04-capabilities/specs/unified-tool-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| unified-tool-registry | 内置 sub-tool 通过 RegisterSubTools 注册 | M | `changes/04-capabilities/specs/unified-tool-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| unified-tool-registry | knowledge/recall 为 config-driven agent | M | `changes/04-capabilities/specs/unified-tool-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| unified-tool-registry | ValidateToolAccess 验证 config 引用的 tool 已注册 | M | `changes/04-capabilities/specs/unified-tool-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| unified-tool-registry | 三阶段工具生命周期 | M | `changes/04-capabilities/specs/unified-tool-registry/spec.md`（未列；按 delta 约定=隐式保留） |
| view-transform-stateless | SmartCompressor returns original messages when no segments to compress | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| view-transform-stateless | protectPendingAsyncSegments removed from Compress | R | → `03.4` 从零实现投影策略；旧算法条款（L0–L3/骨架压缩/LRU-Rocks/时钟回拨）为背景，不逐条复活；相关存储压实由 `03.2` 按后端能力另行定义 |
| wechat-host-send-tool | send 工具是宿主侧显式送达通道 | M | **无 delta**；域声明：`changes/08-host/proposal.md`「send 工具仅显式声明挂载、参数仅 {content}、8KiB 上界、复用唯一发送通路——继承」（域声明继承，见 §4 备注） |
| wechat-host-send-tool | 被动通道语义零变化 | M | **无 delta**；域声明：`changes/08-host/proposal.md`「send 工具仅显式声明挂载、参数仅 {content}、8KiB 上界、复用唯一发送通路——继承」（域声明继承，见 §4 备注） |
| wiki-code-sync | 文件引用必须指向仓内真实存在的文件 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md#文件引用必须指向仓内真实存在的文件` |
| wiki-code-sync | 禁止行号与行数标注 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 标识符与代码一致 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 代码示例与实现一致 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 文件清单完整 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 章节编号自洽 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 已移除机制只留历史注记 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 内部审计编号不进入 wiki | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 末章声明已知缺口 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| wiki-code-sync | 头条承诺与默认行为对齐 | M | `changes/09-acceptance/specs/wiki-code-sync/spec.md`（头注继承声明） |
| workflow-config-compilation | 既有配置构造为真实执行绑定 | M | `changes/01-state-binding/specs/workflow-config-compilation/spec.md`（未列；按 delta 约定=隐式保留） |
| workflow-config-compilation | 发布指纹与版本代 | M | `changes/01-state-binding/specs/workflow-config-compilation/spec.md#发布指纹与版本代` |
| workflow-config-compilation | 唯一原子发布入口 | M | `changes/01-state-binding/specs/workflow-config-compilation/spec.md#唯一原子发布入口` |
| workflow-config-compilation | 单一编排表示 | M | `changes/01-state-binding/specs/workflow-config-compilation/spec.md`（未列；按 delta 约定=隐式保留） |
| workflow-config-compilation | 执行配置访问器隔离可变数据 | M | `changes/01-state-binding/specs/workflow-config-compilation/spec.md`（未列；按 delta 约定=隐式保留） |
| workflow-config-compilation | 候选所有权与在线可见性分离 | M | `changes/01-state-binding/specs/workflow-config-compilation/spec.md`（未列；按 delta 约定=隐式保留） |

### 10.3 归属空洞清单（原 19 能力 / 103 条；已处置清零）

> 以下能力的 M 类 Requirement 原**既无同名 delta、亦无任何域 proposal/design 声明承载**。**本批已全部处置清零，§10.2 不再有「归属空洞」行**：A 类改 H、B 类改 R、C 类逐条正文对位（保留 M）或改 R。「处置结果」列记该能力最终落点。

| 能力 | 域.task | 空洞条数 | 同域现有 delta（供人工判定，**非覆盖证据**） | 备注 | 处置结果 |
|---|---|---|---|---|---|
| async-tool-event-fix | 02.3 | 2 | async-task-execution, event-metadata-contract, event-type-unification, failure-polarity, persistent-event-loop, request-budget-accounting, subagent-turn-execution, thinking-plan-event-classification | 同域(02)另有 delta（async-task-execution / persistent-event-loop 等），但**无同名 delta**，§4 仅给 consumer=03.4，未点名承载条款。 | 对位成立 1 条（→ async-task-execution#按任务类型的 settle 探测器与分档）+ 改 R 1 条（→ 02.3） |
| compaction-scheduler | 03.2 | 3 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | 同域(03)有 storage-lifecycle-completion delta（L1→L2→L3 压实），但其头注/正文**未点名** compaction-scheduler。 | M→R |
| compress-quality-fix | 03.4 | 4 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | §4 自称「以 03 delta 为准（已落地）」，但 03 域**无同名 delta**；压缩语义疑似落在 event-sourced-projection 正文（未列名）。 | M→R |
| compression-user-message | 03.4 | 3 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | 同上：§4 自称「以 03 delta 为准（已落地）」，03 域**无同名 delta**。 | M→R |
| error-handling-consistency | 00.6 | 3 | architecture-guardrails | 同域(00) 有 architecture-guardrails delta（「质量门迁移不得放宽判据」），但**未点名**本能力条款。 | M→R |
| event-compaction | 03.4 | 3 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | §4 自称「以 03 delta 为准（已落地）」，但 03 域**无同名 delta**；段边界/双路径退役未列名。 | M→R |
| event-key-visibility | 02.5 | 1 | async-task-execution, event-metadata-contract, event-type-unification, failure-polarity, persistent-event-loop, request-budget-accounting, subagent-turn-execution, thinking-plan-event-classification | 同域(02) 有 persistent-event-loop delta（请求装配/代绑定），**未点名** event key 注入条款。 | 对位成立 0 条 + 改 R 1 条（→ 02.5） |
| framework-flow-adapter | 02.5 | 11 | async-task-execution, event-metadata-contract, event-type-unification, failure-polarity, persistent-event-loop, request-budget-accounting, subagent-turn-execution, thinking-plan-event-classification | 同域(02) delta 均未点名本能力；且本能力含 4 组**同 spec 内重复标题**（§6），收敛无落点。 | M→H |
| production-wiring-fix | 08.1 | 5 | example-local-file-delivery, example-rl-visibility, production-wiring | 同域(08) 有 production-wiring delta；其头注继承块（FileSegmentStore 生命周期装配 / setter 注入 / Close 顺序）与本地前 3 条语义邻近，但**未列名**。 | M→R |
| readme-enhancement | 09.6 | 3 | race-gate-coverage, resident-release-evidence, wiki-code-sync | 同域(09) 有 wiki-code-sync delta，但只覆盖**文件引用交叉校验**，README 正文内容无承载。 | M→R |
| relation-store-lru-rocks | 03.1 | 4 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | 同域(03) 有 recall-protocol / causal-chain-isolation delta，均**未点名**关系库 LRU/Rocks 条款。 | M→R |
| resident-continuity | 01.5 | 9 | config-hot-reload, config-state-twinship, per-call-subagent-overrides, workflow-config-compilation | 同域(01) 有 workflow-config-compilation / config-hot-reload delta（发布/回滚/换代），**未点名**状态-执行器分离不变量。 | 对位成立 3 条（→ config-state-twinship）+ 改 R 6 条（→ 01.5） |
| snowflake-overflow-handling | 03.1 | 2 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | 同域(03) delta 均**未点名**；§4 理由「时钟回拨方案已替代」，但无替代条款落点。 | M→R |
| summary-memory-rag | 03.4 | 4 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | 同域(03) 有 event-sourced-projection delta，**未点名**摘要入 store/召回条款。 | M→R |
| swappable-executor | 01.5 | 10 | config-hot-reload, config-state-twinship, per-call-subagent-overrides, workflow-config-compilation | 同域(01) 有 workflow-config-compilation delta（发布代/热换），**未点名**本能力 10 条；旧 change（rebuild-formal-runtime/d02）曾有同名 delta，本轮未重建。 | M→H |
| task-reentry | 02.7 | 5 | async-task-execution, event-metadata-contract, event-type-unification, failure-polarity, persistent-event-loop, request-budget-accounting, subagent-turn-execution, thinking-plan-event-classification | 同域(02) delta 未点名；03 域 task-registry-rebuild 场景「恢复与显式重投按当前编排绑定」语义邻近，但**未列名**。 | 对位成立 0 条 + 改 R 5 条（→ 02.7） |
| task-skeleton-compression | 03.4 | 22 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | §4 自称「以 03 delta 为准（已落地）」，但 03 域**无同名 delta**；22 条骨架压缩条款全部无逐条落点。 | M→R |
| telemetry-channel | 05.3 | 7 | committed-event-attribution, degradation-behaviors, runtime-inspection, self-telemetry-audit, trajectory-trace-correlation, turn-tracing | 同域(05) 有 self-telemetry-audit delta，**未点名**遥测通道身份条款。 | M→R |
| view-transform-stateless | 03.4 | 2 | causal-chain-isolation, event-lifecycle, event-segment-store, event-sourced-projection, recall-protocol, rustviking-config, storage-lifecycle-completion, task-registry-rebuild | 同域(03) 有 event-sourced-projection delta，**未点名**「无段返回原消息」条款。 | M→R |

### 10.4 H 类「不复活」抽查（误恢复 = 0）

9 个 H 能力的**能力名**在全部 delta（`changes/*/specs/**`）中**零命中**（本批新增 `framework-flow-adapter`、`swappable-executor` 两个 M→H，已复跑 grep 确认两能力名在 `changes/*/specs/**` 零命中）；关键机制词抽查：`Replace-over-live` 仅作为主 spec `event-sourced-projection` 的**待移除**条款名出现于其 delta 头注（属删除方向，非复活）；`prototype` 仅作为 `production-wiring` delta 的**禁止项**（MUST NOT import `prototype`）出现。

| H 能力 | delta 命中 | 结论 |
|---|---|---|
| `architecture-review-standards` | 无（0） | ✅ 不复活 |
| `compress-event-enrichment` | 无（0） | ✅ 不复活 |
| `framework-flow-adapter`（本批 M→H） | 无（0） | ✅ 不复活 |
| `llm-event-valuation` | 无（0） | ✅ 不复活 |
| `llm-summary-generator` | 无（0） | ✅ 不复活 |
| `maintainability-audit` | 无（0） | ✅ 不复活 |
| `prototype-mvp` | 无（0） | ✅ 不复活 |
| `roadmap-governance` | 无（0） | ✅ 不复活 |
| `swappable-executor`（本批 M→H） | 无（0） | ✅ 不复活 |

### 10.5 D 类 §7 登记核对（未登记 = 0）

| D 能力 | §7 待核点 | 登记 |
|---|---|---|
| `adaptive-segment-size` | 段级封段是否复活为设计预案 | ✅ 已具名（§7） |
| `cognitive-asset-guard` | 脚手架拆除条件是否已满足 | ✅ 已具名（§7） |
| `partition-local-storage-access` | 局部性/性能主张需同条件证据 | ✅ 已具名（§7） |
| `startup-active-partition-bitmap` | 位图/游标是否以等价形态采纳 | ✅ 已具名（§7） |
| `plan-agent` | plan agent 是否进入新默认实施 | ✅ 已具名（§7） |

### 10.6 空洞处置完成声明与张力处置结果

**（一）空洞处置完成声明（103 条 M 类归属空洞 → 0）**

本批对 §10.3 所列 **19 能力 / 103 条** M 类归属空洞逐条处置，§10.2 已**无「归属空洞」行**：

- **A（机制退役）**：`framework-flow-adapter`(11) + `swappable-executor`(10) = **21 条 M→H**（§4 亦改 H）。
- **B（能力保留从零实现）**：压缩族 9 能力（`task-skeleton-compression`22 / `compaction-scheduler`3 / `compress-quality-fix`4 / `compression-user-message`3 / `event-compaction`3 / `view-transform-stateless`2 / `summary-memory-rag`4 / `relation-store-lru-rocks`4 / `snowflake-overflow-handling`2 = 47）+ `error-handling-consistency`3 + `production-wiring-fix`5 + `readme-enhancement`3 + `telemetry-channel`7 = **65 条 M→R**（§4 亦改 R）。
- **C（正文对位补登）**：4 能力 17 条——**对位成立 4 条**（`resident-continuity` 3 → `config-state-twinship`；`async-tool-event-fix` 1 → `async-task-execution`）保留 M；**对位不成立 13 条**改 R 并给新任务（`resident-continuity`→01.5 6 条、`task-reentry`→02.7 5 条、`async-tool-event-fix`→02.3 1 条、`event-key-visibility`→02.5 1 条）。
- **核验**：21 + 65 + 17 = 103 ✓；**更新后空洞数 = 0**（§10.2 残留「归属空洞」行实测 = 0）。
- **处置后分布**：§4 主表 `R`×57 · `M`×49 · `H`×9 · `D`×5 = 120；§10.1 逐条 `R`×242 · `M`×268 · `H`×60 · `D`×31 = 601（口径：改写42 + 头注继承93 + Delta未列90 + 域声明39 + 正文对位4 + 空洞0 = 268 条 M）。

**（二）张力处置结果（9 项 M↔继承张力，以域声明为准）**

| 能力 | 域声明 | 处置 |
|---|---|---|
| `context-scoping` | 01-state-binding/proposal「本域不改…继承源块」 | 维持 M + §4 补「域声明继承」 |
| `generation-bound-model-references` | 01-state-binding/design「模型引用绑定代，只读」 | 维持 M + §4 补「域声明继承」 |
| `evals-harness` | 07-evolution/proposal「…——继承」 | 维持 M + §4 补「域声明继承」 |
| `event-relation-store` | 03-history/proposal+design「主 spec 继承/他域落地」 | 维持 M + §4 补「域声明继承」 |
| `example-file-memory-wiring` | 08-host/proposal「…——继承」 | 维持 M + §4 补「域声明继承」 |
| `self-improvement-meditation` | 07-evolution/proposal「…——继承」 | 维持 M + §4 补「域声明继承」 |
| `wechat-host-send-tool` | 08-host/proposal「…——继承」 | 维持 M + §4 补「域声明继承」 |
| `runtime-resource-ownership` | 05-inspection/proposal「只只读呈现资源持有者，不做获取/释放」 | **未决**：维持 M + §10.2 标「张力未决，见 §10.6」 |
| `task-registry-and-board` | 05-inspection/proposal「只读总图…不渲染看板、不改 registry」 | **未决**：维持 M + §10.2 标「张力未决，见 §10.6」 |

> 说明：前 7 项域 `proposal/design` 显式声明「继承/只读/不改」，继承语义未变故**维持 M**（§4 备注列补「域声明继承，见 §10.2」）；后 2 项 `05-inspection` 声明为「只读呈现/不渲染看板、不改 registry」，属**非重合声明**而非「继承/不实现」——不能据此改 H/R 也不与其等价，故**保持原标签 M 并登记于本处**，不猜。

**（三）诚实边界**

1. **深度**：`Delta未列`（90）与 `域声明`（39）为**弱证据**，不得读作「已审定」；`正文对位`（4）为本批逐条实证（已读 `changes/01-state-binding/specs/config-state-twinship/spec.md`、`changes/02-execution/specs/async-task-execution/spec.md` 正文条款）。仍**未**逐条审读 1217 个 Scenario 正文。
2. **本单边界**：只改本 `coverage.md`（§4/§5/§10.1/§10.2/§10.3/§10.4/§10.6）；未改 `openspec/specs/**`、未改任何 delta/主规格/task-graph/tasks.md 勾选；不跑产品 Go 命令；不提交。
3. **复跑**：`python3 scripts/check_runtime_plan.py --json`（本批后仍 ok）；计数 `grep -rh '^### Requirement:' openspec/specs/*/spec.md | wc -l`（=601）。

# Design: docs-objective-review

## 静态决策总览

以下 D0–D11 保留前两阶段的方法与范围记录；第三阶段实施只以 D12–D20 及 O1–O6 产品增量为准。前两阶段报告不构成修复授权，强制凑问题数、以随机性定训练缺陷等历史判据不沿用。当前状态：实施计划已修订，代码尚未修改。

### D0 评审框架与第一性判准（六域统一，汇总可比的前提）

**六维评价框架**（每份域报告必备章节，缺一即四查不通过）：

| 维度 | 判定问题 |
|---|---|
| 1. 预期特性 | 该域承诺了哪些用户可感知能力？特性→机制→配置的映射是否成立 |
| 2. 架构设计 | 分层/职责/依赖方向/数据流是否自洽；模块边界是否清晰 |
| 3. 过度设计嫌疑 | 复杂度是否超出场景需求：投机性抽象、默认关闭却持续付维护成本、单消费者泛化 |
| 4. 缺陷设计嫌疑 | 逻辑漏洞、边界遗漏、机制间矛盾、文档声明与代码不符 |
| 5. 推理友好性 | 见判准 R |
| 6. 训练友好性 | 见判准 T |

**第一性判准**（评审代理必须以此为准绳，不得用"业界都这么做"替代论证）：

- **判准 R（推理友好）**：一个有限上下文的自回归模型在每一步决策时——可见信息是否**充分**（决策所需状态都在上下文里）、**稳定**（同状态同输入→相似上下文，利于缓存与学习）、**低噪声**（机制注入的摩擦是否值得其收益）；机制是否减少了模型必须凭空记住的状态。
- **判准 T（训练友好）**：agent 行为轨迹能否完整重放出 `(state, action, reward)`；上下文构造是否**确定可复现**（否则同一策略梯度方向漂移）；探索空间是否被机制约束得**可采样**（治理闸/审批等非确定环节是否有确定旁路）；reward 信号（任务成败、票据召回率等）是否**机器可读可提取**。

**评分纪律**：每维给 `A/B/C/D` 四档 + 一句话置信度声明（高/中/低，低须说明缺什么证据）。禁止无证据的 A 或 D。

**被否方案**：不设 0-10 数字分（伪精度，域间不可比）；不设加权总分（权重无依据，汇总时由编排者呈现原文）。

### D1 子域表（二级变更划分 + 接口登记）

| # | 子域 | 文章（行数） | spec 能力 | 一句话边界 | 外部依赖（只读） |
|---|---|---|---|---|---|
| D1 | 记忆存储 | memory/memory-architecture.md (1628)、storage-durability-positioning.md (25)、platform/evaluation-suites.md (51) | memory-storage-review | FullEvent/分层存储/因果链/TTL/召回与评估 | `memory/`、`memory/engine/`、`memory/kv/`、`evals/` |
| D2 | 事件插件 prompt | event/event-architecture.md (577)、plugin/plugin-architecture.md (1030)、prompt/prompt-architecture.md (589) | event-plugin-review | 事件类型系统、插件管线、prompt 装配 | `event/`、`plugin/`、`prompt/` |
| D3 | 工具任务 | tool/tool-architecture.md (1261)、tool/tmux-action.md (87)、agent/task-lifecycle.md (90)、agent/compression-and-telemetry.md (64) | tool-task-review | ActionTool/召回体系/任务生命周期/压缩遥测 | `tool/`、`tool/action/`、`agent/task/`、`agent/compress/` |
| D4 | agent 引擎 | agent/agent-architecture.md (520)、agent/event-flow.md (230)、agent/execution-generations.md (117)、agent/governance-enforcement.md (54)、agent/prototype-skeleton.md (41) | agent-engine-review | 事件驱动引擎/turn 原语/执行代际/治理处置 | `agent/`、`agent/governance/` |
| D5 | 平台子系统 | platform/platform-subsystems.md (221)、platform/agent-behavior-matrix.md (281)、platform/org-hot-reload.md (272)、platform/resource-ownership.md (104)、platform/cognitive-asset-guard.md (90)、platform/reincarnation-notice.md (54)、evolution/evolution-architecture.md (113)【P1 教训回写：自审补入，原漏分配】 | platform-review | 治理/自进化/可靠性/热更/资源所有权的平台面 | `agent/org/`、`agent/resources/`、`evolution/`、`agent/reliability/` |
| D6 | 外围运行 | rl/rl-architecture.md (145)、reliability/durable-delivery.md (150)、examples/wechat-bot-runtime.md (103)、comment-gate-tooling.md (121) | runtime-periphery-review | RL 接口/持久投递/wechat 运行面/文档门禁 | `rl/`、`examples/wechat-bot/`、`scripts/` |

`README.md` (411) + `docs/wiki/README.md` (59) 由**编排者亲审**作为全局对照基线（已完成初读）：README 的四场景承诺是全部域报告的"特性声明对照物"——域报告须核对 README 场景承诺在本域是否落纸为机制。

### D3 波次表

| 波次 | 内容 | 说明 |
|---|---|---|
| W1 | D1–D6 六域并发评审 | 全部只读（文档+源码抽查），互不依赖、无共享写文件（各写各的报告），可全并发 |
| W2 | 编排者四查 + 交叉汇总 + 总报告 + 对话呈现 | 依赖 W1 全部产物 |

**波次语义澄清**：波次仅为汇报分组；本变更解锁条件 = 依赖域报告文件全部落盘并通过四查。无跨域孙任务级产物引用（各域报告独立成篇，交叉引用由 W2 编排者完成）。

### D4 合并规则（产物归置）

- 评审任务**不产生代码合并**；源码、`docs/wiki/**`、`openspec/specs/**`、`README.md` 一律零改动（收尾 F1 断言）。
- 唯一写面：`docs/.dev/20261007-wiki-review-D<N>-<slug>.md` 六份 + `docs/.dev/20261007-wiki-review-summary.md` 一份 + 本 change 目录内规划/勾选文件。
- 域报告文件名 slug：D1-memory / D2-event-plugin-prompt / D3-tool-task / D4-agent-engine / D5-platform / D6-runtime-periphery。

### D5 三级验收

| 层 | 验收标准 |
|---|---|
| 孙任务 | 报告对应章节存在且非占位（含实质分析段落）——编排者 grep 锚点核验 |
| 子域（二级变更） | 域报告文件落盘 + 六维章节齐备 + 至少 2 个代码抽查断言带 `文件:符号` 级佐证 + 六维各有评分与置信度 |
| 变更级（DoD） | F1 契约零改动断言通过；F2 总报告落盘且六域结论均被引用；F3 对话呈现完整汇总（预期特性/架构/过度设计/缺陷设计/推理友好/训练友好六部分）；F4 归档 |

### D6 评审客观性保障（防"读完只说好话"）

- 每份域报告必须包含 **"本域最尖锐的三个问题"** 小节（可以是缺陷、过度设计或风险，必须具体到机制）；
- 过度设计/缺陷设计结论必须区分**文档证据**（wiki 怎么说）与**代码证据**（源码在哪），两者冲突时明示冲突；
- 允许且鼓励负结论；无法定论时写"证据不足 + 缺什么证据"，不许含糊带过。

---

## 第二阶段决策（代码探索，2026-10-07 追加）

### D7 探索子域表（E1-E12 → X1-X6 映射 + 假设来源登记）

| # | 探索域 | 承载议题 | 主读代码面 | 核验假设（来源：第一阶段报告） | 现有验证命令候选 |
|---|---|---|---|---|---|
| X1 | 身份与事实归属 | E1 | event/、agent/event_bus.go、context_manager.go、rl/trajectory_recorder.go、memory/ | 事件 Metadata 是否真携 trace_id（D2）；轨迹行是否真无 event_key（D6）；BuildInvocation 是否不按 Timestamp 排序（D2）；幽灵前驱 memory_plugin.go:179-184（D2） | event/plugin 包白盒测试 |
| X2 | 编排动态性 | E2+E3 | agent/org/、tagent.go 热更编排、agent/task/、settle_routing.go | 统一壳 loopSpec 单字段差异（D4）；reload/rollback 共用候选事务（D5）；任务队列背压/优先级/抢占现状（评审未覆盖，本轮补白）；执行代际快照能力面 | agent/org、agent/task 包测试 |
| X3 | 持久化与索引 | E4+E5+E6 | memory/（含 kv/、engine/）、plugin/memory_plugin.go、tool/action 转储路径 | 双生产者单写路径（D1）；LocalFileKV 锁内全量 marshal（D1）；QueryEvents 无 Metadata 过滤→latestCompactionKey 窗口扫描（D1）；tool-output/ 无生命周期治理（D3）；TTL 扫描 O(事件数)（D1） | memory 包全量白盒测试 |
| X4 | 推理效率 | E7+E8 | agent/compress/、context_manager.go 装配链、config 预算线 | token 估值双常数（/3 vs 2.0）与 ~15% 低估（D1/D2）；tools-schema 未入预算（D1）；面板注入位置对缓存的影响面（D3）；压缩触发唯一性（D3）；召回性能与关键词腿时间序（D1） | evals 包测试 |
| X5 | 训练采集 | E9+E10+E11 | rl/ 全包、memory/feedback.go、scripts/convert_trajectories.py | TrajectoryRecord 字段全集（D6）；feedback 绑 event_key（D6）；三跳 join 中间键存在性（D2 vs D6 对撞）；转换器无 chat template/无 reward 列（D6）；ModelEndpoint 录制与分布漂移（D6） | rl 包测试 |
| X6 | 最小形态与收敛 | E12（贯穿） | 全仓依赖图、config 默认值、消费者面 | 五子系统代码量与唯一消费者（D5）；wf.* 退役重力（D2）；stub/死枚举清单（D1/D2/D3）；消费 X1-X5 报告交叉收敛 | go build ./... + go vet ./... |

### D8 探索波次与解锁条件

| 波次 | 内容 | 解锁条件 |
|---|---|---|
| W3a | X1-X5 五域并发探索 | 本计划落盘（无代码依赖，全部只读） |
| W3b | X6 收敛探索 | X1-X5 探索报告全部落盘并通过四查（**孙任务级前置，非整波栅栏**：X6 消费的是报告文件而非 git main） |
| W3c | 编排者交叉汇总 + 探索总结报告 + 对话呈现 | X6 落盘 |

### D9 探索期权限边界（深读+现有验证档）

**允许**：Read/Grep/Glob 深读；`go build ./...`、`go vet ./...`、`go test <指定包> -run <指定测试>`（现有测试作为证据）；`go list -deps` 等只读工具。
**禁止**：写/改任何 .go/测试/配置/文档文件；写 spike/benchmark 新代码；`go test ./...` 全量长跑（按需选包）；任何 git 写操作。
**写面**：仅 `docs/.dev/20261007-code-exploration-X<N>-<slug>.md` 六份 + `docs/.dev/20261007-code-exploration-summary.md` 一份 + 本 change 目录内规划/勾选文件。
**命令账纪律**：凡运行验证命令必在报告记录命令全文与退出码（凡跑必录）；命令失败不阻断探索（失败本身可以是证据）。

### D10 探索报告契约（区别于第一阶段六维评审）

统一章节：〇探索范围与命令账 / 一现状证据（机制地图，文件:符号 ≥15 处）/ 二假设核验表（≥5 行：假设→证据→成立|不成立|证据不足）/ 三改造面草图 / 四候选方案与代价（含保留现状选项）/ 五最小验证方式 / 六跨域线索。硬性下限：`grep -c '^## ' ≥ 7`、`grep -c '文件' 佐证密度由四查抽样复核。

### D11 阶段账目处理（扩展旧 change 的复活规则）

- 第一阶段 F1-F4 保持已勾状态，标注"第一阶段"；第二阶段新增 F5-F8，互不覆盖。
- 第一阶段产物（六份 wiki-review 报告）是第二阶段的**假设输入**而非结论真源——X 域报告对其有推翻权（核验为"不成立"时以代码证据为准并回写汇总）。
- 探索不修改第一阶段任何报告文件（历史留档不可变）；推翻结论记录在探索汇总报告中。

---

## 第三阶段：协调优化实施（I0–I3）

### D12 哲学复核与范围裁决

基线为 `57ed0a8bbaeaabbcb8323dc7f94660a1d48cd47a`；真实实施前重取 HEAD/diff，不覆盖用户既有改动。用户选择：六域整体优化、允许必要架构调整；采集保真加离线 SFT 消费；本地质量门、性能基准及真实模型测试均需完成。本轮只规划，实施必须由后续 apply 启动。

**继承契约**：`architecture-guardrails`、`event-sourced-projection`、`config-hot-reload`、`per-call-subagent-overrides`、`event-segment-store`、`trajectory-recording`、`trajectory-trace-correlation`。

- 同构自治、每 agent 自有任务域；组合根唯一发布，不新增 DAG、集中调度、第二路由表或持久执行现场。
- 存储成功才发布事实引用；投影是历史装配唯一来源，动态通知有界外显；索引/训练样本为派生物，不能反写运行事实。
- 结构 FP、数值 SRC、文件 FILE 与 RESTART 拒绝分工不变；不统一冻结所有参数，不因训练记录新增执行权。
- 随机摘要和异步到达是环境事实，不强制时间重排；endpoint 不是策略版本；trace 可空且仅辅助关联。
- TTL 差异、无反向索引、单消费者、零仓内调用均不单独构成缺陷。拒绝盲删 prototype/wf.*、统一 TTL、强制 OTel、把静态计数当收益。
- 允许抽取请求快照/提交可见性/导出转换这些有两个真实消费者的窄职责；不做按行数拆包，不以“统一”为由再建万能 manager。

### D13 子域与产品契约（相当于本阶段 D2）

| 域 | 目标 | 能力 spec | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| O1-fact-consistency | 可信事实及失败可见性 | committed-event-attribution | 成功提交、因果游标、关系失败和回溯 partial | 现有 StoreEvent/RelationStore；与 O5 共享插件文件由集成者单写 |
| O2-runtime-coherence | 灵活动态委派与真实热更 | generation-bound-model-references | 接好现有 model_override、拒绝无法热应用的字段 | 现有代际/Overrides；不得改变任务状态机 |
| O3-request-efficiency | 完整预算与有界摘要等待 | request-budget-accounting | 本次请求声明快照、全输入成本、单一压缩权与同步摘要时限 | modelutil 快照供 O5 消费；组合根配置由集成者接线 |
| O4-storage-efficiency | 降低读写 CPU/分配成本 | partition-local-storage-access | 分区扫描、过滤解码、原格式快照编码 | 屏障/TTL/排序原样；无新磁盘格式或通用索引 |
| O5-capture-fidelity | 可用训练采集 | decision-capture | SDK 边界快照、调用关联、丢失统计和封账 | O3 快照；O1 提交后归因；O2 实际模型身份 |
| O6-training-export | 离线 SFT 样本消费 | offline-training-export | 授权事实快照、反馈 join、模板/mask/分割与拒绝清单 | O5 v2 格式；O1 partial；本地 tokenizer |

六域各自四件套含 8 个叶任务。新增 Go API 必须证明外部消费必要性；测试名称按行为命名，不含 O1/I0 等过程编号。

### D14 接口与责任定稿（启动常数，不等待彼此报告）

**S1 请求快照**：在 `modelutil` 增加窄的 `RequestSnapshot`/`ToolDeclarationSnapshot` 数据结构及快照函数，只依赖框架 model/tool 和标准库，不持有存储、执行器或调度器。保存深拷贝的 messages、GenerationConfig、排序后的工具声明（注册键、声明名、description、InputSchema/OutputSchema）。快照范围明确为 SDK 输入，不声称捕获 provider 私有系统提示或 wire body。O3 创建、O5 消费；原工具执行注册表与 Close/Iter 能力不变。本次模型请求使用同一份冻结声明视图，不能让预算/采集与 provider 各重新读一次可热变 Source。

**S2 可选调用关联**：O5 生成 recorder run 内唯一 `call_id`（随机 run 前缀+原子递增序号）；已有 agent/session/invocation/task/input keys 为归属，generation 只作观测标签，不恢复旧租约。每个 runner 尝试在 ctx 安装独立有界关联对象；模型响应在交给框架前注册 `(invocation_id,response_id)` 及同作用域 tool_call_id→call_id。MemoryPlugin 只能按精确键取得归因，成功提交才发出 fact-link。无 ID、冲突、晚到已释放、直调 summary 均具名 unbound，禁止“最近调用”猜测；子 agent 新 scope，不继承父调用身份。该关联只在采集 enabled 时存在，每尝试最多4096项；容量不足显式unbound_capacity并计数，不淘汰活跃映射冒配。仅在真实runner生产者停止且事件回调排空后释放，不以首个assistant或响应通道关闭猜结束；它不是持久会话系统。I0 必须用锁定框架真实 Runner 验证 ID/ctx 传递，失败即阻断 O5 关联任务，不转向 trace 补丁。

**S3 v2 采集**：新 `trajectory_capture.enabled=false`；true 时要求 `trajectory_dump=true`，其余参数 `max_record_bytes=8MiB`、`max_pending_bytes=64MiB`（含采集拥有的在途副本、响应累积、排队记录和序列化缓冲的总额），队列继续 256，`max_run_bytes=512MiB` 限定单次capture的落盘数据量、同时打开文件最多16个；达磁盘限额停止接纳新的采集数据并计数告警，不删历史文件、不阻塞业务。以上均为构造期配置，不假称热更。保留旧记录字段，新增 schema_version=2、capture_scope=sdk_request、run_id/call_id、owner、tools、响应分片/终态、binding_status、missing_reasons、request_digest。分片按收到顺序深拷贝，不重复拼接累计全文；终态归约有明确测试。trace 继续可选。超限、满队列、关闭、编码/写入失败分别累计计数；统计不依赖数据队列。封账 manifest 记录 started/written/dropped/failed/inflight、已同步状态与文件摘要；缺 manifest 或未同步为 unknown，不能报完整。默认不新增全局 wire 拦截、HTTP body 留档或在线训练服务。

**S4 归因/反馈导出**：先读 feedback.Content.parent_key，再按显式授权分区读 parent.FullEvent.Metadata 中的 call_id；必要时按已记录 fact-link 合并，无 Metadata 索引也能有限分页扫描。导出是静态只读事实快照，包含 cutoff、来源摘要、分区允许表和缺失原因，不改变 TTL/墓碑/保留租约。跨分区父键逐跳检查，禁止“空分区=全库”；missing/forbidden/ambiguous/expired-or-missing 分列，不猜已删除原因。不自动把 task 成功、governance 拒绝或多条反馈变成数值 reward。

**S5 离线样本**：strict 导出以本次完整 SDK messages+tools+目标 assistant 为输入；整体套用本地 tokenizer 模板（local_files_only、trust_remote_code=false），仅目标 assistant 内容/工具调用的可靠 token 边界计 loss。无可信 assistant mask 的模板明确拒绝，不拼 role 字符串、不分别编码猜边界。保存 input_ids/attention_mask/loss_mask/labels 及可追溯 manifest；按 root session 分 train/test，不按行随机分。旧 JSONL 可读但缺字段为 legacy/unknown，不能无提示进入 strict 集。多模态尚无可靠处理器时留拒绝清单，不丢弃部件伪装纯文本。

### D15 波次与共享文件归属（相当于 D3/D4）

| 波次 | 工作 | 解锁条件 |
|---|---|---|
| I0 | 编排者核基线/真模型与 tokenizer 前置；O1.1、O3.1、O4.1、O5.1、O6.1 建回归/基准/格式钉 | 用户后续 apply；无生产会话与共享 tmux 操作 |
| I1 | O1、O2、O3、O4 的独占文件并行；O5 格式/队列、O6 纯转换逻辑可按 S1–S5 fixture 先行 | 相关 I0 钉测成立；依赖接口常数已明确 |
| I2 | 编排者集成 O1/O2/O3 接线，再 O5 精确归因，最后 O6 真实导出 | O5.5 依赖 O1.6/O2.6/O3.6；O6.6 依赖 O5.6；不是整波等待 |
| I3 | 双模块门禁、真实模型、真实 tokenizer、前后基准、用户导向总结 | 各域实现与相关集成通过，所有真实必需项 PASS 而非 SKIP |

波次仅为汇报分组；解锁=前置补丁已进入本次统一集成工作树并验证（用户授权 git 合并时对应入 main）。本任务不包含 commit/push。重型基准与真实模型各独占一个资源槽，等待表随资源登记，释放立即消费；常规独立单测可并行。

| 唯一写入者 | 白名单（含对应测试） |
|---|---|
| O1 | plugin/memory_plugin.go、tool/recall/*；其余共享点提交变更清单交编排者 |
| O2 | agent/tool_agent.go、agent/exec_lease.go、agent/org/* |
| O3 | agent/compress/*、modelutil/* |
| O4 | memory/kv/*、memory/segment_store.go、memory/lifecycle.go、memory/relation_store.go |
| O5 | rl/trajectory_recorder.go、rl 中新 capture 文件及测试 |
| O6 | rl/training_export.go 及测试、scripts/convert_trajectories.py 及其新 unittest |
| 编排者 | event/metadata.go、plugin/attribution.go、agent/context_manager.go、agent/execution_gate_model.go、agent/agent.go、agent/session.go、agent/task/task_manager.go、所有根包源码/测试、config/*、testutil/*、tests/integration_test.go、tests/llm_contract_test.go、tests/offline_bench/*、tests/README.md、README*、CI/质量脚本、所有 docs/wiki/api 与主 specs |

O1/O5 对同一 memory_plugin.go 的需求由 O1 先落提交闸，O5 给集成者归因接入清单，禁止两代理同写。请求装配/模型解析/热更文件只由编排者写；可从 context_manager 抽出 request_assembly.go、从 tool_agent 抽出调用覆盖帮助函数，但不得出现两份装配入口或新顶层包。没有代码 diff 与行为测试证明净减少重复职责，不进行抽取。

【执行期回写（R17 变体，W2 实证）】Go 同树并发的污染按**编译传递依赖闭包**传导而非包名直觉：tool/recall → agent → {rl, modelutil, agent/compress}，且 agent 直接 import rl——任一 rl/compress 半编辑态会击穿全部下游测试（O1、O3b 各实测一次，等待窗口约 3.5 分钟）。后续派发令固定三条：①代理保持其窗口内包可编译（先加新文件后接线，收尾前 `go build ./<pkg>` 绿）；②被他人半成品波及时的标准处置=轮询目标包 `go build` 恢复后复跑，红/绿采证用 overlay/备份复原法做实现无关性自证（O1/O5 示范）；③波次划分依据改为闭包不相交而非 import 表直觉。

### D16 目标—任务与消费者覆盖

| 用户目标/边界 | 承载叶任务 | 验收消费者 |
|---|---|---|
| 协调统一、可信失败语义 | O1.2–O1.6、O2.2–O2.6、O3.6 | 入口/子 agent/recall/宿主 diagnostics |
| 更灵活的已有编排 | O2.2–O2.7 | 默认调用/覆盖/重入/回滚/在途委派 |
| 更高效的持久化和查询 | O4.2–O4.7 | 点读/时间与类型查询/关键词/TTL/重启 |
| 更高推理效率 | O3.2–O3.7 | 普通/压缩/超预算/摘要降级/流式/迭代 |
| 随运行采集保真 | O5.2–O5.7 | entry/子 agent/summary/错误/取消/满队列 |
| 离线反馈与 SFT | O6.2–O6.7 | 导出者/tokenizer/训练 collator/分割验证器 |
| 真实模型在本地跑通 | O2.7、O3.7、O5.7、O6.7、F11 | 本地测试进程→真实 provider→无副作用 nonce 工具→样本 |
| 文档与 API 一致 | 每域 .8、F12 | 使用者/生成 API/未来维护者 |

每个改变数据的任务追踪生成→提交/存储→读取→最终消费，并覆盖默认/启用、成功/失败、满队列/取消、重启/热更、授权/越权、legacy/新格式。模糊失败不折算成成功或空结果。

| 原探索议题 | 实施承载与明确裁剪 |
|---|---|
| E1 身份/事实归属 | O1.2–O1.6、O5.5；不新建持久turn系统 |
| E2 动态编排 | O2.2/O2.3/O2.7；复用覆盖/重入，不新增DAG/paused |
| E3 版本/调度边界 | O2.4–O2.6、O5.5；维持FP/SRC/FILE，无新抢占器 |
| E4 提交/写放大 | O4.4/O4.6/O4.7；编码CPU优化，不放松逐事件屏障 |
| E5 查询索引 | O4.2/O4.3/O4.7；分区访问优化，通用Metadata索引延后 |
| E6 生命周期 | O5.4/O5.6、O6.2/O6.5；运行采集磁盘上限与导出快照，TTL不统一 |
| E7 上下文预算 | O3.2/O3.3/O3.5；单一压缩权 |
| E8 压缩召回收益 | O3.4/O3.7、O4.3/O4.7；先基准，不改关键词产品排序 |
| E9 决策记录 | O5.2–O5.7；SDK层保真与资源上限，不承诺wire |
| E10 奖励关联 | O5.5、O6.2/O6.3；保留原反馈，不自动credit assignment |
| E11 样本转换 | O6.4–O6.7；严格SFT可消费，不做在线RL/权重训练 |
| E12 最小形态/收敛 | O2.6/O3.6/O5.6、15.3/F9；证明默认关和单点责任，不盲删死码 |


### D17 验证档位、性能证据与真实路径

所有下列测试/脚本中新增名称均为**计划产物**，尚未运行。叶任务命令为回归筛选，完成后还必须运行对应整个包及受影响下游；测试不存在或零匹配不算通过。

- 单元/集成：基线与最终均运行 `go test ./... -short -count=1`；独立 bot 模块同门；`GOMAXPROCS=1 go test . -short -count=1`；`go build ./...`、`go vet ./...`、`bash scripts/lint.sh`、`bash scripts/check-openspec.sh`。
- race：改动域先 `go test -short -race`；收尾按现有 `scripts/race_check.sh . ./memory/... ./agent/... ./evolution/ ./event/ ./tool/... ./plugin/ ./rl/` 口径。tmux 会话族必须在隔离临时 HOME/TMPDIR/TMUX_TMPDIR 的测试进程运行、串包；未证实隔离前不启动，不能收割用户默认 tmux 会话。报告区分 PASS/既有 WAIVED/失败，不增豁免、不凭三次通过抹掉偶发 FAIL。
- 基准：扩充现有 tests/offline_bench，seed 固定；不做笛卡尔积：常规档1k/10k事件×1KiB正文×1/16分区（事件数为全库总数）；规模档100k紧凑事件×1/16分区；大正文档512事件×64KiB×1/16分区。真实预装每档数据，100k不用20k样本冒充；预装可用既有KVBatch+Sync构造等价固定fixture，另验store冷启动与查询等价，不把预装时间计为在线写入吞吐。单run fixture≤256MiB、总写盘预算≤3GiB，超过则该档阻塞并报告，不缩样本伪达标。before/after 同工具链、同数据；先预热，再至少5次测 ns/op、B/op、allocs/op、读写字节、p50/p95、峰值内存、缓存驻留。区分重启成本、应用冷缓存与热缓存，OS 缓存不假称冷盘。
- O4 扫描候选必须消除指定分区查询对无关桶的访问；快照编码缓存只有目标10k cell中位数改善≥10%、其余同类 cell 退化≤10%、总附加缓存≤8MiB、写盘字节不增才保留。未达门保留负结果并撤该缓存路径；定向扫描与语义修正不因此取消。绝不声称减少磁盘写放大（格式与逐事件屏障不变）。
- O3 的目标是完整计量及摘要等待可界定，不承诺 task成功率提高；单测用受控阻塞模型证 deadline，真实模型记录摘要耗时/usage/票据保持及总调用成本，不以墙钟偶发抖动单独判失败。
- O5 以不开启为基线，开启后报告额外 CPU/allocs、最大 pending bytes、丢失数；禁止把录制完整性换成默认阻塞推理。

**真实本地验收**：在测试机调用用户明确提供的真实 provider，可为 loopback 服务或已有 API；“本地跑测试”不强制下载权重。统一开关 `TAGENT_REQUIRE_REAL_MODEL=1`；显式读取 `TRPC_CLAW_API_ENDPOINT`、`TRPC_CLAW_MODEL_NAME`、`ZAI_API_KEY`（loopback 无 key 可用），不 source shell、不扫描其他凭据。缺端点/模型或配置非法、用例 Skip、零调用均令必需验收失败。每次 run 最多24次调用、单调用输出≤2048 token、请求估计≤32768 token、请求超时≤120s；真实预算/使用量及模型名入账，超过限额停止并留未完成，不重试刷绿。涉及第二模型时用 `TAGENT_TEST_ALT_MODEL` 显式指定；未提供时可验证同模型不同配置代际，但不宣称跨模型覆盖完成。

新增验收脚本 `scripts/verify_runtime_acceptance.py`（O6实现、编排者集成）核对 Go -json 中必需用例确实 PASS/无 SKIP、真实调用数>0、完整采集manifest、SFT样本与mask、session分割；不引用 change 路径。`TAGENT_TEST_TOKENIZER` 指定已准备的本地工具感知模板；无可信assistant mask即 fail，不网上下载模型/执行远程代码。transformers/datasets 是既有转换器依赖，复用本地已装版本并记录，不新增训练后端。本轮交付样本/一个 collator batch 验证，不声称完成权重训练或取得训练增益。

### D18 安全、回滚与五轴运行检查

| 轴 | 必须保持/新增的证据 |
|---|---|
| 到被看见 | 提交后票据可取回；partial原因可见；新覆盖到真实请求；采集到SFT样本而非只写JSON |
| 不可见 edge | 门禁拒发不吞恢复提示；超限/满队列有统计；legacy/无ID/缺父不假完整 |
| 高频成本 | 声明每请求取一次；缓存可丢可重建；采集字节上界；不用全局O(n)锁包住跨session存储 |
| 重启恢复 | 事件格式/TTL不变；采集manifest未封账为unknown；旧记录可读；不恢复指针/旧租约 |
| 构造时序 | 可选采集在模型包裹前装好；同一共享writer封账按cutoff确认且最终关闭一次；candidate失败释放，提交不含慢IO |

新模式输出权限为目录0700/文件0600，不录 Authorization、API key 或完整带凭据URL。不将 raw HTTP body 纳入默认采集。内容脱敏放导出阶段，策略明确、变换留摘要；未知PII不宣称已匿名。仅用合成fixture/nonce做模型验收，不发送真实聊天历史。关 `trajectory_capture` 回到旧录制；不删除已产文件。O4原格式可回退二进制；O2新增引用语义只在配置驱动实例启用，独立 Register/Lookup API保留。F阶段不自动提交、推送、部署。

### D19 三级验收与结论门（相当于 D5）

孙任务：先红后绿的对应契约测试及命令真实退出码；每条证据含基线、作用域、实际匹配测试名。子域：spec每个Scenario映射任务/用例；整个受影响包通过，负结果明示。变更：F9–F14逐项完成；真实模型+本地tokenizer一条未过则阶段不能称完成。CodeReview仅在用户明确要求代码评审时派唯一代理；不以它替代编排者运行证据核账。没有本轮真实性复验的历史报告数字不可写成新证据。

### D20 文档、规格收口及规划自审

文档唯一长期落点沿用 docs/wiki，对应README/生成API同步；修复原型Compact与compaction载荷叙述、noop条件、转储Cleaner、面板位置及已撤回建议，不能把历史报告全文搬入wiki。脚本/测试注释只指长期文档，不指变更编号；不新增过程编号测试名。

产品增量仅在 O1–O6 specs。验收且用户批准归档后，将各 O 目录提升为独立顶层 change（名称 optimize-committed-facts / align-runtime-overrides / budget-full-model-requests / optimize-partition-storage / capture-decision-snapshots / export-offline-sft）；移交必须四件套无损、只保留一个权威副本、原父账目更新索引。逐个 `openspec validate <name> --strict` → `scripts/check-openspec.sh` → 正常 `openspec archive <name> --yes` → 再门禁；禁止删 delta、skip-specs、no-validate。父过程change保留审计索引，不把orchestration投为产品能力。归档未授权时明确pending，不伪勾。

规划自审已纠正：A 类——旧“全程只读”覆盖第三阶段、训练强制trace/TTL推断、真实测试Skip假绿、O1/O5共写、根包模型装配多写、O6无消费验收、过程spec污染主spec、基准笛卡尔积超出本机资源、只限排队未限在途采集内存；B 类——过程编号测试名、仅计标题当质量、20k冒充100k、静态写放大冒充测速、把不符合局部职责的wire全捕获扩大为默认能力。候选曾考虑全面wire录制/通用索引/强制多provider分词/训练权重更新，均因非当前消费者必需而不采纳。

外部资料核验：WebSearch检索到 Hugging Face官方 tokenizer/chat-template API（https://huggingface.co/docs/transformers/main_classes/tokenizer）；两次正文抓取超时，未据此声称模板细节已验证。O6.1以实际本地tokenizer能力钉测为启动门，不能猜版本接口；无通过证据则阻断strict导出验收。

本轮规划校验：一级 `openspec validate docs-objective-review --strict` 通过；O1–O6 用本机 OpenSpec 1.2.0 的 `Validator(true).validateChangeDeltaSpecs` 分别校验，均valid且0 errors/0 warnings；Glob核对24份O工件齐备、每域8条未勾任务。产品spec已移除过程任务编号，验收映射保留在tasks/design。代码diff为空；未执行产品测试、基准、真实模型或tokenizer。CLI的artifact 4/4仅表示文档齐备，不代表实施完成。

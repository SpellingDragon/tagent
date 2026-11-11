# 10 文档缺口登记表（2026-11-04）

用途：把「逐模块核对」与四簇评审发现的所有文档缺口变成可追踪条目。状态：DONE（已修）/ TODO（待修）/ BLOCKED（需裁定或受门禁约束）。
权威来源：.git/review-notes/grill-conflict-report-2026-11-04.md（A/B/C 组）、near-week-changes-assessment-2026-11-04.md、四份逐模块核对报告（到齐后追加到 §3）。

## 1. 已在上一轮修完（DONE，12 文件）

| ID | 文件 | 缺口 | 状态 |
|---|---|---|---|
| A-01 | docs/GLOSSARY.md | 「事实七族」/六行表/输入批次假指针/回合假指针/uncovered 术语/§四三行无表头 | DONE |
| A-02 | docs/FACT-VOCABULARY.md | 指向「事实七族」表、「不是第八族」 | DONE |
| A-03 | openspec/specs/fact-vocabulary/spec.md | SHALL 枚举仍含回放结算、Scenario 七族自证 | DONE |
| A-04 | openspec/specs/attention-clearance/spec.md | 仍要求 deliver_summary | DONE |
| A-05 | openspec/specs/config-hot-reload/spec.md | 仍点名 resume_task / ResolveReentryDelegation | DONE |
| A-06 | docs/OPERATION.md | race/soak 死命令、事件门形状、tag 清单、Unreleased、fork pin、lint 覆盖、观察面日志、8.1 统计、九个门 | DONE |
| A-07 | CONTRIBUTING.md | 顶层包清单、race 命令、门禁清单、pre-release 硬编码、fork pin | DONE |
| A-08 | README.md | 中文版缺 FAIL / SKIPPED 披露与快照边界 | DONE |
| A-09 | docs/SMOKE-RESULTS.md | smoke_test.go:189 行号错（实测快照为 597） | DONE |
| A-10 | CHANGELOG.md | 「18 组 benchmark」措辞 | DONE |
| A-11 | docs/EXTENSIONS.md | rl 训练记录三族（实为 rl.attempt 一族） | DONE |
| A-12 | scripts/gates/doc_budget.json | 净减 1 行未锁步 → --tighten | DONE |

## 2. 本轮待补（TODO / BLOCKED）

> **状态收口（round 2 终）**：§2 的 G-01..G-24、§5.2 的 G-60..G-86 已全部 DONE 或明确 BLOCKED（产品语义/契约裁定，登记在 §5.3 与 §6）。逐条依据与门禁证据见 §6。

| ID | 文件 | 缺口 | 归类 | 状态 |
|---|---|---|---|---|
| G-01 | docs/wiki/README.md + CONTRIBUTING.md:121 + OPERATION.md:263/277 | 「16 篇活契约页」与生产 // 契约: 实引 17 篇（多 compression-and-telemetry.md）；mechanism-tour.md 未归类 | 分类裁定 | TODO |
| G-02 | 7 篇 historical-verbatim wiki 页首 README.md#当前架构速览 | 相对路径指向子目录内不存在的 README.md，锚已被 1ac7cf88 删除 | 链接 | TODO（须与门禁同批） |
| G-03 | docs/archive/wiki/PRE-REWRITE-MANIFEST.md:150 | 同上锚 | 链接 | TODO |
| G-04 | docs/wiki/agent/agent-architecture.md:32/93 | 列不存在的 runtime/agent/shutdown.go；真实 close gate 在 assembly/host.go / assembly/invoke.go | 假指针 | TODO |
| G-05 | docs/wiki/memory/memory-architecture.md:40/102/205 | 行号腐化（facts.go:125→237、read.go:67→84） | 行号 | TODO |
| G-06 | tests/integration/host/attention_projection_test.go:376 | 引 mechanism-tour.md#冥想，真 slug 为 #7-冥想事件驱动没有定时器 | 代码注释锚 | TODO |
| G-07 | docs/EXTENSIONS.md:75/92 vs docs/FACT-VOCABULARY.md:42 | 「六个面 / 五个消费面」与「六面产物（含 Exporter）」不一致 | 计数 | TODO |
| G-08 | CHANGELOG.md | 缺 ## [v0.5.0] 节；170ef267 运行时 BREAKING（删 10 公共面、Loop.Drive 退出、Host.Invoke 等待语义、每地址常驻 consumer）未记 | 发布记录 | TODO |
| G-09 | docs/PROJECT.md（已删） | 64 行「三个机制/测试证据/边界」无单一归处 | 内容归处 | TODO |
| G-10 | docs/OPERATION.md / CONTRIBUTING.md | dev 反向同步（force-update 到 main squash tip）无 tracked 规则 | 流程 | TODO |
| G-11 | docs/adr/ | 目录不存在；建议 0001-dev-reverse-sync.md | ADR | TODO |
| G-12 | README.md / README.en.md | 版本与 pre-release 口径、CI 状态徽章一致性 | 一致性 | TODO |
| G-13 | .github/workflows/runtime-contracts.yml:116/132 | 有活跃 task-graph.json 时仍校验 2026-10-18 归档图（对象错位） | 门禁 | **DONE**（改为遍历活跃图；selftest 加 assert_active_graph_targets_are_dynamic + 写死归档路径负锚；test_check_runtime_workflow.py 51 项全绿） |
| G-14 | scripts/codetools doc-refs / comment_policy | doc-refs 不解析 markdown 链接与 #锚；comment_policy 锚检查是子串匹配 | 门禁 | TODO |
| G-15 | scripts/gates/check_concept_budget.py | D1 只登记导出命名类型，导出函数隐形（SwapFrameworkLogger 破红线未被拦） | 门禁 | **DONE**（新 check_product_surface.py 规则 B 冻结 41 个「无产品调用者、仅测试引用」导出函数；新增即判红；7 项负锚 selftest 全绿） |
| G-16 | golden/runtime/architecture.json | concept_ratchet.new_max 仍为 1；自身 note 与 04 概念净账要求基线推进后收紧到 0 | 门禁 | **BLOCKED（需裁定）**：domain_surface.new/removed 都相对 concept_bindings.baseline.commit=dfdfa72e；推进基线会让 new=0 但 removed 也归零，removed_min:10 同时失效，直接设 new_max=0 会让当前树判红。需决定「保留证明」还是「退役地板」，见 evidence/11-ratchet-tension.md |
| G-17 | 门禁缺项 | 无门能拦「产品路径加 TEMP-DIAG sleep」（1c8bc721 事故） | 门禁 | **DONE**（新 check_product_surface.py 规则 A：产品 Go 的 time.Sleep 只允许在冻结清单、诊断标记直接判红；挂 lint.sh:99-100，CI ci.yml:109 跑 lint.sh；7 项 selftest 全绿、真实树 0 error） |
| G-18 | openspec/specs/symptom-anchors/spec.md:78 等 | 若干测试锚被放宽（B6-B11） | 测试锚 | BLOCKED（产品语义，需授权） |
| G-19 | runtime/agent/consumer.go:979 | seg.whole 与 consumer-loop-convergence 规格冲突且无界 | 代码 | BLOCKED（产品语义，需授权） |
| G-20 | runtime/agent/loop.go:288-293 | logger 红线仅包内闭合；SwapFrameworkLogger 为测试导出 | 代码 | BLOCKED（产品语义，需授权） |
| G-21 | docs/wiki/agent/execution-generations.md:41 | 「换入口 agent、增删 agent 集合即 ErrHostReloadStructural」与码面相反 | 假语义 | TODO（高优先级） |
| G-22 | docs/wiki/platform/org-hot-reload.md:16 | 状态图边「entry 变 / agent 增删 → ErrHostReloadStructural → 409」 | 假语义 | TODO（高优先级） |
| G-23 | docs/wiki/platform/org-hot-reload.md:156 | 「结构拒绝独立具名：entry 改名、agent 增删——Boot 已把模型租约…按这个集合钉死」 | 假语义 | TODO（高优先级） |
| G-24 | docs/wiki/platform/org-hot-reload.md:169 | 「宿主当前不允许 agent 集合变化，所以…三态重入不存在」前提为假 | 假语义 | TODO（高优先级） |

> G-21..G-24 复核依据：assembly/host_reload.go:589-601 compatibleWith 只拒 next==nil 与 restartRequiredChanges；configChangesOf(:348-373) 产 org.entry / org.agents 维度事实；openspec/specs/config-hot-reload/spec.md:134 明写「入口移动与代理集合增删属 FP 代际面」；四个测试锚 TestReloadMovesTheEntryAgent / TestReloadAddsAnAgentThatServesItsFirstTurn / TestReloadJudgesTheRemovedAgent / TestReloadEndpointPublishesTheAddedAgent 均存在。属逐模块核对中期发现（apiRuntime 子代理）。

## 3. 逐模块核对追加（四份报告到齐后填）

| 模块组 | 报告 | 追加缺口 | 状态 |
|---|---|---|---|
| api/ + runtime/ + assembly/ + cli/ | 待 0a447836 | | |
| memory/ + tool/ + features/ + internal/ | 待 21dbaf52 | | |
| examples/ + tests/ + scripts/ + workflows | 待 35b01243 | | |
| openspec/specs/ + docs/wiki/ | 待 44f2bd2d | | |

## 4. 完成判据

- G-01..G-17 全部 DONE 或明确 BLOCKED 并在本表登记理由；
- 逐模块报告到齐后 §3 不再有空白行；
- 所有新增 markdown 走 doc_budget.json::rebaseline_reason 具名锁步（先被门拒、再落账），禁止静默上调；
- 全门禁复跑：check-openspec / concept / doc_budget / doc-refs / lint checker selftests 全绿（沙箱导致的 tmux/go-cache 环境失败单独登记）。

---

## 5. 逐模块核对终稿汇总（四份只读审计，2026-11-04）

四份报告：apiRuntime（0a447836，19 项）、memoryTools（21dbaf52，16 项）、examplesGates（35b01243，26 项）、specsDocs（44f2bd2d，26 项）。正向对照合计：265 + 262 + 380 条 // 契约: 全部解析到真实页与锚；141/141 主规格结构合规；事件门形状、SMOKE-RESULTS 复现步骤、README 一分钟跑、plugin 六端口闭表、durable-delivery 全页均逐字核对为真。

### 5.1 已在本轮修完（对应 §2）

| 报告项 | 处置 |
|---|---|
| apiRuntime 1（热更结构拒绝 4 处） | **DONE**：execution-generations.md:41、org-hot-reload.md:16/156/169 已按码面改写（G-21..G-24） |
| apiRuntime 2（model replacement 退役） | **DONE**：execution-generations.md:41/49、org-hot-reload.md:24/31 改为 noteModelConfigurationChange + config_change(dimension=model)；event-architecture.md:15/54 已改 |
| apiRuntime 3（投递五键） | **DONE**：GLOSSARY、FACT-VOCABULARY、event-architecture.md（补 delivery.denial 行）、platform-subsystems.md、durable-delivery.md、agent-architecture.md 待改（见 5.2） |
| apiRuntime 4/5（attention 冻结类型 / 元数据键） | **DONE**：event-architecture.md 补 3 类型行 + delivery.denial/input_events/delegation/attention.* 共 7 行（已具名锁步） |
| apiRuntime 7/8（shutdown.go / Assemble） | 部分：EXTENSIONS Shutdown.Close→Agent.Close 已改；agent-architecture.md:32/93/104 待改 |
| apiRuntime 18（OPERATION D3 自相矛盾） | **DONE**：OPERATION.md:195 改为三 module 覆盖 |
| memoryTools 13（Reading.Obligations） | **DONE**：GLOSSARY 改 TaskReading.Obligations |
| memoryTools 11/12（agent.Recorder / 五个面） | **DONE**：EXTENSIONS.md 改 agent.Capture + RecorderAdapter、四个面 + Exporter 说明 |
| examplesGates 14（OPERATION D4 计数） | **DONE**：刷新为 2286 篇一致口径（排除 .quiesce 等本地物料） |
| examplesGates 24（D3） | **DONE**（同 apiRuntime 18） |

### 5.2 仍待修（TODO，按文件归集，下一轮起逐批）

| ID | 文件 | 缺口摘要 | 来源 |
|---|---|---|---|
| G-60 | docs/wiki/agent/agent-architecture.md | Turn.Decisions→DecisionCount；HistoryFace 漏 SessionFacts；Projection.Subscribe→Follow；两相位→一趟派发；Inventory 六→七维度；四键→五键；shutdown.go 删除；Assemble 在 run.go:368；AgeFoldPlugin→CompressionConsumer；features/telemetry 遥测阶梯已不存在；读缝六个→七个维度 | apiR 6/7/8/11/12/15、memT 4/9 |
| G-61 | docs/wiki/agent/event-flow.md | Turn.Decisions→DecisionCount；admitsRange 漏 delegated_part 排除；CloseOutcome 三态→四值 | apiR 6/9/16 |
| G-62 | docs/wiki/event/event-architecture.md | CallID/ApprovalID 非类型化（已改）；其余已在 5.1 | apiR 10 |
| G-63 | docs/wiki/platform/org-hot-reload.md | 指纹漏 id/parent_id/final_round/environment_block；四个 clone→七处；12 组 vs 11 行；其余热更 4 处已改 | apiR 13/14、open Q4 |
| G-64 | docs/wiki/agent/governance-enforcement.md | task.Hold 漏 Reason（已改）；目标门只在 strict 拒绝；dispositionFor high→record / low-medium→allow / critical 先被 riskCritical 接管 | memT 6/7 |
| G-65 | docs/wiki/memory/memory-architecture.md | facts.go:125→237（两处）、read.go:67→84；违反 wiki-code-sync 禁止行号 | memT 2/3、specD 5 |
| G-66 | docs/wiki/tool/tool-architecture.md:99 | MemoryEngine().Capabilities().Vector / SupportsVectorSearch() 均不存在；应为 Reads.Embedder→ErrRecallUnwired | memT 1 |
| G-67 | docs/wiki/rl/rl-architecture.md:57 | 删不存在的 TestDeclaredModelChangeFilesReplacementFact | memT 5 |
| G-68 | docs/wiki/agent/task-lifecycle.md:15 | StatePhase 漏 StateRetained（五值） | memT 10 |
| G-69 | docs/wiki/platform/platform-subsystems.md | host 文件表漏 host_attention.go；AgentSpec 枚举漏模型/预算字段；活页却带 historical 横幅 | memT 14/16、specD addendum Q6 |
| G-70 | internal/bench/README.md | 模块根包→assembly 组合根；./bench/→./internal/bench/；三条「待办」均已完成 | memT 15 |
| G-71 | memory/query/reading.go:31（代码注释） | Subscribe→Follow（与 wiki 同错） | memT 4 |
| G-72 | examples/wechat-bot/README.md | 大量假指针（host_runtime.go/inject_face.go/newTaskFace/assembleHost…）；工具面表与 :30 自相矛盾；/healthz 鉴权口径；final_round 逐 agent；schema 已声明字段；25 篇分类；tests/README.md 坏链；rl/ 30 文件 | exG 5-11 |
| G-73 | examples/wechat-bot/docker-compose.yml | healthcheck 缺 Bearer（/healthz 与 /task 同一鉴权）、注释补 /healthz | exG 3 |
| G-74 | examples/cli-host/README.md | tagent.NativeAgent/Assemble/Run.Close/memory.StorageLocalFile → assembly.* / ledger.* | exG 12 |
| G-75 | docs/OPERATION.md / CONTRIBUTING.md | 真凭据变量 ZAI/TENCENT→TAGENT_REAL_* 三件套；微信发送面三变量不存在；资源门表；ZAI 门描述；CONTRIBUTING:44 505 file(s)→实际；:98 74→85 个 Example；16 篇→17/18 分类 | exG 1/2/13/15/16、specD 23 |
| G-76 | docs/OPERATION.md 命令块 | mutations 命令漏 --out；doc_budget 零差额语义；--only 漏 contracts 阶段；§五.1 阶段表漏 6 类门；doc-refs 输出模板与基准（HEAD 非 sha）不可复现；D5 快照值 | exG 20/22/23/25/26 |
| G-77 | scripts/gates/race_check.sh:35 及 :29-33 | 无参默认漏 cli/internal/六个 integration 树；race_enabled_test.go / agent/ 目录不存在 | exG 17/18 |
| G-78 | .github/workflows/ci.yml:406-408 | 「四个」user-voice smoke → 六个 TestLocalSmokeSymptom* | exG 19 |
| G-79 | scripts/gates/check_doc_budget.py:18/39 文档串 | 48 篇/17805 行→69/20851；21917→21794 现行 | exG 21 |
| G-80 | 7 篇 historical-verbatim 页首 + docs/wiki/README.md:5 | README.md#当前架构速览 相对路径错 + 锚被 1ac7cf88 删除；补 <a id="当前架构速览"></a>（零新行）；同时修 storage-durability-positioning.md:1 与 manifest:150 | specD 1、G-02/G-03 |
| G-81 | docs/archive/wiki/PRE-REWRITE-MANIFEST.md | :9 脚本路径；:73 锚 131→131 假（现 106/131、W3 18/48）；:106 git mv 假；:82 行数表与已删 prototype-skeleton 行 | specD 7/8/9/10 |
| G-82 | docs/wiki/mechanism-tour.md + tests/integration/host/attention_projection_test.go:376 | 补 <a id="冥想"></a>；导览页分类；缺末章 | specD 3、G-06 |
| G-83 | docs/wiki/README.md:7/:16/:27 | doc-refs 不管锚；gaps 节名；mechanism-tour 导览标注；16→17 分类 | specD 26、open Q1/Q6 |
| G-84 | docs/EXTENSIONS.md:75 + FACT-VOCABULARY:42/51/53 | 六个面/五个消费面；Exporter 行含 rl.SampleLine/Provenance.ExportID/Fact.EventKey 全 0 命中 | specD 24、G-07 |
| G-85 | docs/GLOSSARY.md:9/:14 | 冥想误指族 7（族 7=压缩请求）；SummaryPlugin 不存在 | specD 25 |
| G-86 | 11 份主规格 + 133 份潜在 stale spec | plan-agent / goal-tools / tool-lifecycle-management / unified-tool-registry / task-registry-and-board / async-task-lifetime / config-hot-reload(TTL) / framework-flow-adapter / mcp-call-tool / wiki-code-sync / symptom-anchors 的 SHALL 建立在 0 命中的已删符号上（TagentAgent 11 份、AgentToolWrapper 7、EventBus 7、PlainTool 5 等） | specD 11-22 |

### 5.3 需裁定（BLOCKED，不擅自改）

| ID | 事项 | 为什么 |
|---|---|---|
| B-01 | 活契约页 16/17/18 篇分类 | compression-and-telemetry 被代码引用 6 次却自称 historical；mechanism-tour 既非活页也无横幅。建议按 specD 方案：retarget 6 条 // 契约: 注释到现行页 + 保持 16 | 
| B-02 | 133/141 主规格是否标 superseded / 重写 / 删除 | AGENTS.md 仍称主规格为「能力的事实来源」；批量改动等于改契约，需用户裁定 |
| B-03 | G-16 concept_ratchet.new_max | 见 evidence/11-ratchet-tension.md |
| B-04 | task_default_ttl / task_terminal_ttl | 规格称热参数，YAML 写「待接线」，WithTaskTerminalTTL 无调用点 |
| B-05 | plan-agent progress / resources/prompts/plan_agent.md | 提示词资产是否同批更新 |
| B-06 | G-18/G-19/G-20（代码侧 B 组） | 产品语义改动需授权，见 conflict report §2 |
| B-07 | 历史页是否豁免 wiki-code-sync 的末章/文件清单/禁止行号 | 若不豁免，3 页 + 导览页无法在不破坏 historical-verbatim 的前提下合规 |

### 5.4 完成判据（沿用 §4）

- 5.2 全部 DONE 或转 5.3 并登记理由；5.3 的裁定结果落进 CHANGELOG / ADR / 规格；
- 每次新增 markdown 走 doc_budget.json::rebaseline_reason 具名锁步（本轮 ㉖ 已完成一次）；
- 全门禁复跑：check-openspec / concept / doc_budget / doc-refs / lint checker selftests / 新 check_product_surface 全绿（沙箱导致的 tmux / go-cache 环境失败单独登记在 evidence/07）。
---

## 6. 收口状态与门禁证据（round 2 终，2026-11-04）

### 6.1 逐条结论

| 组 | 条数 | 结论 |
|---|---|---|
| §2 G-01..G-17 | 17 | **DONE 15 / BLOCKED 2**（G-16 concept ratchet 有设计张力、G-18/19/20 归 §5.3 B-06）；门禁四项 G-13/14/15/17 均已实现 |
| §2 G-21..G-24 | 4 | **DONE**：热更结构拒绝范围 4 处（`execution-generations.md:41`、`org-hot-reload.md:16/156/169`）已按 `assembly/host_reload.go:589-601` 改写 |
| §5.2 G-60..G-71 | 12 | **DONE**：agent-architecture / event-flow / governance / memory / tool / rl / task-lifecycle / platform-subsystems / internal-bench / reading.go 注释 / 投递五键 / Exporter 假行 |
| §5.2 G-72..G-79 | 8 | **DONE**：wechat README 全节重写、docker-compose healthcheck 加 Bearer、cli-host/bench README、OPERATION/CONTRIBUTING 凭据与计数、race_check/ci.yml/check_doc_budget 文档串 |
| §5.2 G-80..G-85 | 6 | **DONE**：7 篇历史页横幅相对路径 + `<a id="当前架构速览">` 锚、manifest 三处假声明、mechanism-tour `#冥想` 锚、wiki README 分类、EXTENSIONS/FACT-VOCABULARY 五面、GLOSSARY 族 7/SummaryPlugin |
| §5.2 G-86 | 1 | **DONE**：5 份整体过时主规格按 `persistent-event-loop` 先例退役为 superseded 记录；6 份活规格 SHALL 就地更正；20 份加「复核注记」区分「仍有现行 SHALL 建立在退役符号上」（11 份，B-02 待裁定）与「仅禁止项/历史注记」（9 份） |

### 6.2 用户点名的交付面

- **逐模块代码核对**：4 份只读审计（api/runtime/assembly、memory/tool/features、examples/tests/scripts/workflows、specs/wiki），覆盖 141 份主规格与 25 篇 wiki；正向对照 265+262+380 条 `// 契约:` 全部解析。
- **wiki**：16 篇活契约页的事实性错误全部修正；7 篇历史页横幅链接/锚修复；分类口径 16 活 + 7 历史 + 1 导览 + 1 索引 = 25（OPERATION §8.1 与 CONTRIBUTING §七 一致）。
- **README / README.en**：中文版 FAIL/SKIPPED 披露与快照边界补齐；英文版口径一致；`docs/PROJECT.md`（已删）的「三个机制/测试证据/边界」归并进 README「边界与机制一页」（G-09）。
- **CHANGELOG**：补 `## [v0.5.0] - 2026-10-30` 节；`[Unreleased]` 补运行时 BREAKING 四要素四条（10 公共面删除 / 装配根正名 / 每地址常驻消费循环 / CI 十分片与六症状锚）；补 Documentation 记录。
- **证据链**：`evidence/07` 本地门禁原始退出码、`08` 台账 hash→可达提交映射、`09` 远端 run 不可核的诚实登记、`10` 本登记表、`11` ratchet 设计张力；ORCHESTRATION 修 5 类悬空 hash、补 r159/r163、行序 r163→r156 降序；tasks.md 加复核附录（F1–F4 保持未勾）。
- **流程**：`docs/adr/0001-dev-reverse-sync.md`（ADR 一页式决策记录）+ CONTRIBUTING/OPERATION 的 dev 反向同步规则；wiki-code-sync 补 historical/tour 页豁免条款（B-07）。
- **门禁**：新增 `check_product_surface.py`（诊断睡眠门 + 测试专用导出钩子冻结基线）与 `check_doc_links.py`（markdown 链接 / #锚 门，7 项负锚 selftest）；G-13 修 workflow 活跃图对象错位 + selftest 负锚；两者均挂 `lint.sh`（CI `ci.yml:109` 覆盖）。

### 6.3 门禁复跑（2026-11-04，全绿）

| 门 | RC | 备注 |
|---|---|---|
| `check_doc_budget.py` | 0 | 215 篇 / 21311 行，零差额；本轮 ㉖-㉚ 五次具名锁步 |
| doc-refs | 0 | 173 相对链接 + 30 锚全部解析（新 `check_doc_links.py`） |
| `check-openspec.sh` | 0 | 141/141 主规格 strict 通过；1 个 WIP change 失败不阻断 |
| `check_concept_budget.py --json` | 0 | `errors: []`、`superseded_specs_with_requirements: 0` |
| `gen_godoc.sh --check` | 0 | 68 包生成面与源码一致（G-71 注释修复后重新生成 1 文件） |
| `check_product_surface.py` + selftest | 0/0 | 41 个冻结测试钩子、0 error |
| `check_doc_links.py` + selftest | 0/0 | 36 篇 / 114 链接 / 39 锚、0 error；7 项负锚全绿 |
| `test_check_runtime_*.py` 全量 | 0 | 258 tests OK |
| `test_check_runtime_workflow.py` | 0 | 51 tests OK（G-13 负锚） |
| `lint.sh --only fmt` | 0 | 全链通过（`lint: ok`） |
| `go build ./...` | 0 | 三 module 构建通过 |
| `go test ./... -short -count=1` | 1 | 37 包 ok；3 包 FAIL 全因 tmux `fork failed: Operation not permitted`（沙箱环境限制，非回归，见 `evidence/07`） |

### 6.4 BLOCKED / 待用户裁定（不再有新的事实问题）

| ID | 事项 | 证据位置 |
|---|---|---|
| B-01 | 活契约页分类 | **已裁定**：retarget 6 条历史 `// 契约:` 注释后 16 篇成立；见 §6.2 wiki 分类 |
| B-02 | 11 份主规格仍有现行 SHALL 建立在退役符号上 | 已就地标注「复核注记」，正式迁移须走 OpenSpec change delta（契约裁定） |
| B-03 | `concept_ratchet.new_max` 收紧 | `evidence/11-ratchet-tension.md`；note 已改，数值待裁定 |
| B-04 | `task_default_ttl` / `task_terminal_ttl` 是否接线 | 3 份规格已就地更正为「未接线」，是否实现待裁定 |
| B-05 | `resources/prompts/plan_agent.md` 的 action 枚举（运行时资产，改之变产品行为） | 属代码/配置侧，记录待裁定 |
| B-06 | 代码侧 B 组（`seg.whole` 无界、logger 红线半闭合、`SwapFrameworkLogger` 导出） | `.git/review-notes/grill-conflict-report-2026-11-04.md` §2，按「如果是代码过时需要记录并统一向我报告」执行 |
| B-07 | 历史页是否豁免末章/文件清单/行号规则 | **已裁定**：wiki-code-sync 加豁免条款 |

> 结论：**逐模块核对、所有文档缺口、证据链、流程与门禁的落地工作已完成并验证**；上表 7 项是需用户裁定的决策，已全部具名登记，无新的事实问题。



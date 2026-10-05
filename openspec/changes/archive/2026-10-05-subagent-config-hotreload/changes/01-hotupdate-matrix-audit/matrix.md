# 热更维度矩阵（01-hotupdate-matrix-audit）

> 取证法＝四问：① 配置落点（schema 字段→结构字段）② 通道归属（fp 子集？OrgHotParams？两不沾？）
> ③ 消费点（谁在何时机读）④ 现有契约测（有＝登记测试名，无＝本域补齐并登记新测试名）。
> 证据一律给 `文件:行` 或测试名，禁止推读。通道代号：**FP**＝fp 代际面（视图变更，下回合生效、在途钉定）；
> **SRC**＝源拉取面（OrgHotParams 携带，下一次消费即生效）；**FILE**＝文件即真源＋mtime 懒读通道（既有第三种，见跨域情报 C1）；
> **RESTART**＝明确不参与换代（改了必须重启）；**FOREIGN**＝异域自有机制，不入 org 配置面。
>
> 基线：dev @ 68eb8bc。红线判据：**源里携带而消费点未读＝假热更**。

## 一、源拉取面（OrgHotParams 五轴）

源面结构：`agent/context_manager.go:189-195` `OrgHotParams{ThresholdPct, MaxTokens, KeepRecentTasks, TaskTerminalTTL, TaskDefaultTTL}`。
恒装源：`agent/agent.go:569`（`staticHotSource(initialHotParams(cfg))`，无无源状态）→ 解析点 `tagent.go:397-429 hotParamsFor` → 提交点 `tagent.go:456-487 applyHotAll`（只换源＋记账，零写入）。

| 维度 | 配置落点 | 通道 | 消费点（谁/何时读） | 契约测 | 红线判定 |
|---|---|---|---|---|---|
| 压缩阈值 | `agents.*.compress_threshold`（config/config.go:265）→ `OrgHotParams.ThresholdPct`（context_manager.go:190） | SRC | 每个压缩边界一次读：`compress.ContextCompressor.liveNums`（context_compressor.go:183-200）→ `Threshold()`（:244）、`BudgetLine()`（:206）、`Compress()`（:362） | 既有：`TestApplyOrgHotParams_RoutesAndGuards`（agent/context_manager_test.go:192，含"零读数回落构造值"守卫）、`TestHotSourcePullRotatesWithoutPush`（agent/compress/context_compressor_test.go:1373）、`TestSourceRotation_PassThroughBoundary`（:1311，行为级：宽窗后同一回合原样通过）、`TestFullConfigAndRollback`（org_hotreload_test.go:1841 在真实消费者处交叉验证 `9000×0.5`） | 真热更：机制＋消费点断言齐备，无缺口 |
| 上下文预算 max_tokens | `agents.*.max_tokens`（config/config.go:263）→ `OrgHotParams.MaxTokens`（:191） | SRC | `liveNums`→`BudgetLine()`（context_compressor.go:206-209）＝`maxTokens×threshold`；`OrgBudgetLine()`（context_manager.go:201-206）暴露同一解析值 | 既有：`TestApplyOrgHotParams_BudgetLineMoves`（agent/context_manager_test.go:205）、`TestResidentBudgetHotAppliesToRealConsumer`（:237）、`TestSourceRotation_BudgetLineMoves`（compress:1268）、`TestSourceRotation_ShrinkWindow_RealCompressionFollows`（compress:1332，缩窗后真压缩跟随）、`TestHotParamSnapshotSeededAtConstruction`（agent:330） | 真热更。边界注记：由 max_tokens 派生的工具输出上限**刻意不热**（`TestOutputCapIsConstructionDerivedBoundary`，agent:223） |
| keep_recent_tasks | `agents.*.keep_recent_tasks`（config/config.go:266）→ `OrgHotParams.KeepRecentTasks`（:192） | SRC | `liveNums`→`KeepRecentValue()`（:213）；并在每次 `Compress()` 里随 `CompressOptions.KeepRecentTasks` 下发给真实压实（:362,382-386）→ `deterministicLevel`（smart_compress.go:189-205） | 既有（读数级）：`TestSourceRotation_BudgetLineMoves`（compress:1282 断言 `KeepRecentValue()==5` 且内层构造值未被写）、`TestHotSourcePullRotatesWithoutPush`、`TestFreshSubCallSeededFromSnapshot`（agent:361-363）、`TestInFlightSubCallAppliesAtBoundary`（agent:391-393）、`TestFullConfigAndRollback`（org_hotreload_test.go:1840）。**本域补齐（行为级）**：`TestHotSourceKeepRecentReachesRealCompaction`（agent/compress/hotupdate_matrix_source_test.go） | 真热更；补齐前只有"读数"证据、缺"压实结果随热值变化"的行为级证据，正是红线要防的"读得到而没用上"空窗 |
| task_terminal_ttl | `agents.*.task_terminal_ttl`（config/config.go:272，字符串）→ `OrgHotParams.TaskTerminalTTL`（:193，`hotParamsFor` 解析） | SRC | `TaskManager.SetTTLSource`（task/task_manager.go:610）→ `effTerminalTTL()`（:621）在 `pruneTerminal()`（:1387）每次清扫读；`TerminalTTL()`（:1257）暴露同一解析值 | 既有：`TestTerminalRecordSurvivesConsumerRestart`（task/task_manager_test.go:663，含"零读数不得归零在用周期"守卫）、`TestFullConfigAndRollback`（org_hotreload_test.go:1799，真实 TaskManager 处交叉验证 :1842、回滚恢复 :1861） | 真热更 |
| task_default_ttl | `agents.*.task_default_ttl`（config/config.go:278）→ `OrgHotParams.TaskDefaultTTL`（:194） | SRC | `effDefaultTTL()`（task/task_manager.go:635）在 `reconcileTTL()`（:1047，统一回收器每轮）与看板读 `DefaultTTL()`（:1269）；负读数封顶到 `defaultManagerTTL`（寿命回收不可关） | 既有：`TestTaskDefaultTTLReachesLiveConsumerWithoutOverwritingExplicit`（task/task_manager_test.go:634，走真实 `remainingLifetime` 计算且证明显式 TTL 不被改写）、逐属主交叉验证（org_hotreload_test.go:2470-2477） | 真热更 |

## 二、fp 代际面（`agent/org/fingerprint.go:19-31` orgSubset ＋ `:104-133` agentSubset）

变更语义＝推进结构代（整代原子替换，在途调用钉定旧代）。下表"契约测"列中**本域补齐**者见 `agent/org/hotupdate_matrix_fp_test.go`（逐维度一行一名，验证面 `go test ./agent/org/ -run Fingerprint`）。

| 维度 | 配置落点 | 通道 | 消费点（谁/何时读） | 契约测 | 红线判定 |
|---|---|---|---|---|---|
| prompt 声明 | `agents.*.system_prompt{inline,files,dir}`（config/config.go:250）、`agents.*.prompt_dir`（:247）、顶层 `prompt_dir`（:41） | FP | 声明经代际重建 → `prompt.NewSource`（build_agent.go:398 装配 `SystemPromptSource`）→ 每次模型调用前 `BeforeModel` 重读（agent/context_manager.go:426-445） | **本域补齐**：`TestFingerprintPromptDimension`（含 inline 与 files/dir 两种书写）。既有相邻：`TestOrgFingerprint_AuditsEveryAgentConfigField`（org_hotreload_test.go:41，字段归类审计） | 补齐前该维度**只有归类审计、没有"变更⇒换代"直指断言**＝回归保护空窗（改 prompt 声明不触发换代即假热更） |
| tools | `agents.*.tools`（config/config.go:258）＋ `ToolRef` 全维度（:515-） | FP | 代际重建时解析为工具集与拓扑（`config.ReachableAgents`，tagent.go:457） | 既有：`TestOrgFingerprint_CoversFullToolRef`（org_hotreload_test.go:80，逐 ToolRef 字段）、`TestOrgFingerprint_ChangesOnOrgFields`（:2700 "tools"）。**本域补齐（维度行）**：`TestFingerprintToolsDimension` | 真热更，证据双层 |
| 模型 ID | `agents.*.model`（:239）＋ 全局 `model`（:45） | FP | 实例解析 `runtimeConfig.resolveAgentModel`（wiring.go:35-）/`resolveGlobalDefaultModel`（:104-），缓存键含 provider＋model＋端点 | 既有：`TestOrgFingerprint_ChangesOnOrgFields`（:2723 per-agent model）、`TestOrgFingerprint_ChangesOnGlobalModelDefaults`（:2755 全局）。**本域补齐**：`TestFingerprintModelIDDimension` | 真热更 |
| provider | `agents.*.provider`（:243）＋ 全局 `provider`（:49） | FP | 同上（协议名与注册表键） | 既有：`TestOrgFingerprint_ChangesOnGlobalModelDefaults`（:2766 全局 provider）。**本域补齐**：`TestFingerprintProviderDimension`（per-agent provider，此前无直指断言） | per-agent provider 补齐前是空窗 |
| 端点 | `providers.*.api_endpoint`（:228）→ `providerSubset.APIEndpoint`（fingerprint.go:38） | FP | `provider.WithBaseURL`（wiring.go:64-66/117-119）；缓存键含端点故不会命中旧实例 | 既有：`TestOrgFingerprint_ChangesOnOrgFields`（:2717 "provider_endpoint"）。**本域补齐**：`TestFingerprintAPIEndpointDimension` | 真热更。**边界**：顶层 `api_endpoint`（config/config.go:71）刻意不入指纹（进程级），见第五节 |
| 拓扑与入口 | `entry`（:30）、`agents` 增删（:37） | FP | 重建整代执行器与属主缓存；入口改名走拒绝路径 | 既有：`TestOrgFingerprint_ChangesOnOrgFields`（"entry"/"agent_added"/"agent_removed"，:2715-2722）、入口拒绝用例（org_hotreload_test.go:3345 区段） | 真热更（入口改名＝需重启，属有界拒绝） |
| 摘要策略 knob | `agents.*.compress.summary_max_tokens`（:349）、`compress.summary{model,provider,reasoning_effort,...}`（:366，ModelRef）＋弃用平面别名 `summary_model/summary_provider/summary_effort`（:358/362/354，`config/modelref.go:16-33` 折叠） | **FP** | `buildCompressorOpts`（agent/agent.go:603-624：`WithSummaryModel`:616／`WithSummaryEffort`:619／`WithSummaryMaxTokens`:622）→ `SmartCompressor.summaryMaxTokens`（smart_compress.go:40）／`summaryEffort`（:33）→ 每次摘要 LLM 调用 `effectiveSummaryMaxTokens()`（:408）＋ `generatePlainSummary()`（:359-373，`modelutil.Knobs`） | 既有（部分）：`TestFingerprintFoldedFieldIsLive`（org_hotreload_test.go:3374，证明折叠后的 `summary_model` 真参与指纹）、`TestModelRefAliasesFoldToStableFingerprint`（:3355）。**本域补齐（逐 knob）**：`TestFingerprintSummaryKnobsMoveFingerprint`（summary_max_tokens / summary.reasoning_effort / summary.model / card_max_chars / compact_keys_listed / recent_full_count 各一针） | 见第三节结论：**摘要 knob 属 FP 面，已热（代际粒度）**；`OrgHotParams` 无此字段，故"下一次消费即生效"这一更细粒度缺失——非假热更，属粒度归属，域 02 据此改判 |
| 综述工程参数 | `compress.card_max_chars`（:342）、`compress.compact_keys_listed`（:334）、`compress.recent_full_count`（:338） | FP | `NewContextCompressor` 构造期定窗（context_compressor.go:323-337）→ `resolveRefs` 全量窗口（:497）与卡片策展（:1031） | **本域补齐**：`TestFingerprintSummaryKnobsMoveFingerprint`（同函数内逐字段） | 真热更（代际粒度）。跨代边界：`recent_full_count` 缺省由**构造期** `keepRecent×DefaultRefsPerTurn` 派生（:335-337），故不随 keepRecent 热轴移动——见跨域情报 C2 |
| 生成参数（不做注脚①） | `agents.*.temperature`（:264）、`thinking_enabled`（:286）、`thinking_tokens`（:287）、`reasoning_effort`（:288）、`reasoning_content_mode`（:289） | FP | `agentSubset` 逐字段携带（fingerprint.go:111,126-129）→ 代际重建并入 `model.GenerationConfig` | **本域补齐**：`TestFingerprintGenerationKnobsDimension`（五项各一针） | 实证注脚：已在 FP 子集内，代际粒度已热；D6 已裁"不挪源面"，本行是该裁决的证据锚 |
| 模型路由（不做注脚②） | provider 注册表＋`ModelRef` 别名折叠＋`WithModelOverrides` 注入（tagent.go:229） | FP | `resolveAgentModel` 解析序：per-name override → agent 自身 model → 全局默认 model → `WithModel` 注入（wiring.go:30-31,35-） | 既有：`TestFingerprintFoldedFieldIsLive`、`TestFingerprintFoldIsIdempotentUnderRepeat`（org_hotreload_test.go:3386，折叠不动点⇒指纹不漂移）。**本域补齐**：`TestFingerprintRouteRefDimension`（`compress.summary.provider` 路由项参与指纹） | 实证注脚：路由变更全部落在 FP 面；`SwappableModel` 属异域自有机制，见本节末行 FOREIGN 证据 |
| 其余 FP 声明项 | `max_tool_iterations`（:262）、`resume_context_rounds`（:281）、`meditation.{enabled,interval,min_gap,prompt_file}`（:296/492-503）、`workspace_root`（:302）、`description`（:305） | FP | `agentSubset`（fingerprint.go:110,124,130-132）→ 代际重建装配 | 既有（归类）：`TestOrgFingerprint_AuditsEveryAgentConfigField`（:41）逐名对齐 | 真热更（代际粒度）；无逐维度直指断言，归类审计已足以阻止"新字段隐身"，故不补针（避免与既有审计重复制定） |
| 五轴不入指纹（通道互斥证明） | `max_tokens`/`compress_threshold`/`keep_recent_tasks`/`task_terminal_ttl`/`task_default_ttl` | SRC（非 FP） | 消费即生效故不得触发换代：`agentSubset` 以 `json:"-"` 排除（fingerprint.go:112-124 注释＋:122） | 既有：`TestOrgFingerprint_StableAcrossEmptyChanges`（:2673 含 `compress_threshold`）、排除表白名单 `fingerprintExcludedFields`（:1059-1066）＋归类审计（:41）。**本域补齐（逐轴一针）**：`TestFingerprintHotParamAxesStayOutsideSubset` | 满足"归属且仅归属一条通道"的排他性证明——补齐前只证了 threshold 一轴 |

## 三、摘要 knob 归属结论（域 02 唯一前置事实）

**排查三条路（design 顺序）逐一实证：**

| 路 | 排查动作 | 实证结果 |
|---|---|---|
| 1 配置字段 | 全仓搜 `summary/digest/abstract/condense` 关键词到 config | 摘要 knob **确有配置落点**：`config.CompressConfig`（config/config.go:331-367）＝ `SummaryMaxTokens`(`summary_max_tokens`, :349)、`Summary`(`summary` ModelRef, :366)、`CardMaxChars`(:342)、`CompactKeysListed`(:334)、`RecentFullCount`(:338)，加三个弃用平面别名 `summary_model`(:358)/`summary_provider`(:362)/`summary_effort`(:354)，由 `config/modelref.go:16-33` 在装载期折入 `Summary` |
| 2 SmartCompressor 构造参数 | 追构造项及其 config 来源 | `compress.WithSummaryModel/WithSummaryEffort/WithSummaryMaxTokens`（smart_compress.go:61/106/113）＝**仅构造期选项**；来源是 `agent/agent.go:603-624 buildCompressorOpts(cfg *TagentConfig)`，即代际重建时的装配，无运行期写入面 |
| 3 prompt 侧摘要指令 | 看摘要提示词是否 prompt 模板 | 摘要指令**硬编码于 Go**：卡片浓缩串 `context_compressor.go:1227-1230`、滚动综述串 `:1244-1300`（`generatePlainSummary` 的系统串 `smart_compress.go:365`），`resources/prompts/` 下无摘要模板文件 ⇒ 摘要文案不是配置维度 |

**结论（接口常数，交域 02）**：

| 项 | 结论 |
|---|---|
| 通道归属 | **FP 代际面**——`AgentConfig.Compress`（config/config.go:282）整体作为 `agentSubset.Compress` 参与指纹（`agent/org/fingerprint.go:125`，无 `json:"-"`），任一摘要 knob 变更⇒指纹变⇒整代重建⇒新 `SmartCompressor` 携带新 knob |
| 是否假热更 | **不是**：机制（子集携带）＋消费点（装配期读取）齐备，且有既有直指证据 `TestFingerprintFoldedFieldIsLive`（org_hotreload_test.go:3374 用 `summary_model` 证明折叠字段真参与指纹） |
| 真实缺口 | 粒度缺口而非有无缺口：摘要 knob 只在**换代**时生效，`OrgHotParams`（context_manager.go:189-195）**无摘要字段**、`compress.HotNumbers`（context_compressor.go:151-155）只带 `ThresholdPct/MaxTokens/KeepRecent` 三项 ⇒ 域 02 的题面从"补摘要热参"缩水为"把摘要 knob 从代际粒度下移到消费边界粒度"（可选做，非必做） |
| 域 02 若仍要做，接口常数 | 字段名建议随 `OrgHotParams` 语义取 `SummaryMaxTokens int`（与 `config.CompressConfig.SummaryMaxTokens` 同名，零新词汇）；解析点＝`tagent.go:397-429 hotParamsFor`（现码在此把 `ac.CompressThreshold/MaxTokens/KeepRecentTasks/TaskTerminalTTL/TaskDefaultTTL` 折成 `OrgHotParams`，摘要 knob 若挪源面走同一处）；源面需把 `compress.HotNumbers`（context_compressor.go:151）同步扩项并在 `liveNums`（:183）解析；消费点＝`SmartCompressor.effectiveSummaryMaxTokens()`（smart_compress.go:408，当前只读构造字段）与 `generatePlainSummary()`（:359-367 的 `modelutil.Knobs`）——这两处现在是构造字段直读，挪源面即改判域 02 的实现面，**本域不预支** |
| 本域交付的测试 | `TestFingerprintSummaryKnobsMoveFingerprint`（agent/org/hotupdate_matrix_fp_test.go）：逐 knob 钉"变更⇒指纹变"，即 FP 归属的正向证据；因结论为 FP 面而非"两不沾"，故**不需要**摘要现状行为红基线测试（域 02 改前基线由本测试承担） |

## 四、红线与契约（矩阵即 spec 的可执行面）

| 编号 | 判据 | 落点 |
|---|---|---|
| R1 | SRC 维度必须有"消费点读到解析值"的契约测，测试名登记于本矩阵行 | 第一节五行测试名（keepRecent 由本域补齐行为级一针） |
| R2 | FP 维度必须有"变更触发代际推进（指纹变化）"的直指断言 | 第二节 `TestFingerprint*Dimension` 六针（prompt/tools/model/provider/endpoint/摘要）＋既有归类审计 |
| R3 | 一维度只属一条通道：SRC 五轴必须证明不入指纹 | `TestFingerprintHotParamAxesStayOutsideSubset` |
| R4 | 派生量（输出上限、全量窗口、卡片上限缺省）不得被算作热轴，也不得被静默当缺陷 | `TestOutputCapIsConstructionDerivedBoundary`（agent:223）＋ C2 待裁决 |

## 五、FILE / RESTART / FOREIGN 通道（现状登记，不新增）

| 维度 | 配置落点 | 通道 | 消费点 | 契约测 | 判定 |
|---|---|---|---|---|---|
| prompt 正文（文件内容） | 磁盘 `*.md` 文件本体（非 YAML 字段） | FILE | `prompt.Source.Get()` mtime 比较后重读（prompt/source.go:46-80,104）→ 每次 `BeforeModel` 应用（agent/context_manager.go:430-444） | 既有：`TestSource_HotReload`（prompt/source_test.go:47）、`TestCheckModTimesDetectsFileNewerThanLastLoad`（:194） | 已热且**不入 fp、不入源**：语义＝文件即真源＋消费边界懒读，与 SRC 同族（pull），但载体不同 ⇒ 跨域情报 C1 |
| MCP 服务器声明 | 顶层 `mcp_servers`（config/config.go:68） | FILE | `tool/mcp.Registry` 懒检查绑定文件 mtime 后 diff-apply（registry.go:201 `maybeSyncLocked`、:209 取 mtime、:88 播种基线） | 既有：`TestRegistry_HotSync`（tool/mcp/registry_test.go:115）、`TestRegistry_Seed_BaselinesMtime`（:203） | 同上：懒读文件通道，不入两轴；`ComputeOrgFingerprint` 注释明确排除 `mcp_servers`（fingerprint.go:45） |
| 各 agent 记忆存储段 | `agents.*.memory.*`（config/config.go:254） | RESTART | 无热消费点：实例切换会分裂历史 | 既有：`TestMemoryFingerprint_DetectsMemoryOnlyChanges`（org_hotreload_test.go:2783）＋ `fingerprintExcludedFields["memory"]`（:1060） | 刻意不入任何热通道＝需重启，属有界拒绝 |
| 治理／可靠性／进程级 | `governance.*`（:115）、`reliability.*`（:124）、顶层 `api_endpoint`（:71）、`resident_meta_dir`（:34）、`working_dir`（:104）、`log_level`（:80）、`trajectory_*`（:91/95）、`evolution.*`（:118） | RESTART | 无（目录/端点类资源不可迁移） | 既有：`TestOrgFingerprint_StableAcrossEmptyChanges`（:2673，`governance.dir`/`reliability.bus_spill_dir`/顶层 `api_endpoint` 三针证明不入指纹） | 假热更防线：这些字段若入指纹会造出"热更成功而旧资源仍在用"，故拒绝是正确行为 |
| SwappableModel | 非 YAML 字段（`rl/swappable_model.go:12-17`，由宿主注入，见 examples/wechat-bot/main.go:185） | FOREIGN | 运行期换内层实例（`rl/http_api.go:28`） | `rl/` 包自有测试 | 异域语义（训练换装）与 org 配置路由不同，D6 已裁不统一入源；本行是边界证据 |

## 六、跨域情报（交编排者裁决）

| 编号 | 事实 | 影响 | 建议 |
|---|---|---|---|
| C1 | 域 spec 要求"每个维度 SHALL 归属且仅归属一条通道：FP 或 SRC"，但现场存在**第三种既有通道 FILE**（文件即真源＋mtime 懒读）：`prompt 正文`（prompt/source.go:46）与 `mcp_servers`（tool/mcp/registry.go:201）都属此类，且各有契约测 | 归档时该需求文案会把两个既有机制判为"不合规"，而它们的 pull 语义与 D6"不得新增 push/订阅/广播"并不冲突 | 请裁决：spec delta 增列"文件即真源懒读"为 FP/SRC 之外的合法第三通道，或把 FILE 归并为 SRC 的一个子类（载体＝文件而非记录）。**本域未擅自改 spec.md（超出写入白名单）** |
| C2 | `compress.recent_full_count` 缺省由**构造期** `keepRecent×DefaultRefsPerTurn` 派生（context_compressor.go:335-337），热更 `keep_recent_tasks` 不重算该窗口；同类先例 `outputCapForMaxTokens` 有"刻意不热"的直指测试（agent/context_manager_test.go:223），此路径无 | 若按红线"源里有而消费点没读＝缺陷"字面判，full-render 窗口一侧可被读作假热更；若按"派生量属构造边界"判，则与 outputCap 同类、只是缺一条直指测 | 请裁决归属（缺陷 or 刻意边界）。本域**只登记不写测试**：现状行为若被锁死成契约，会与域 02 的改判方向相互锁定，越权 |
| C3 | 摘要 knob 经排查判定为 **FP 面已热**（第三节），与一级 D1"真缺口仅摘要 knob＋per-call"的表述存在强度差：摘要侧缺的是**粒度**而非**通道** | 域 02 的题面可能从"补维"改判为"粒度下移（可选）"，其 proposal/design 的必要性论证需据此重写 | 已按任务书"据实回退"要求写入本域矩阵第三节；一级 tasks 02 条目的措辞请由编排者在集成时定 |
| C4 | 本波次 `./agent` 包内的红全部来自同波域 03 在途交付，红点随其推进而迁移：同波文件 `agent/percall_override_test.go`（先为该文件两条改前红，后迁移到 `TestAgentToolWrapper_Declaration_NoExtraParams`、`TestPerCallOverride_InFlightCallSurvivesGenerationReload`）——覆盖层实现中，红属其域 | 本域 1.6 的验证命令 `go test ./agent/... ./agent/org/... -count=1` 在 W0 集成前无法 exit 0，与本域交付无关 | 本域以 `-skip 'TestPerCallOverride_'` 证明其余全绿（见第七节），不改同行文件、不放宽判据；域 03 实现转正后该命令自然净 |
| C5 | `comment_policy` 门禁在 W0 窗口内的回归全部落在 `agent/percall_override_test.go`：先为 `index-anchor-unknown`（锚缺失），其补 wiki 锚后迁移为 6 条 `free-standing` | 门禁 exit 1 若按波次记账会被误算到本域头上；本域两文件在全量 `-no-baseline -v` 扫描下零命中 | 处置权在域 03（改指既有锚）或编排者集成时补 wiki 锚——本域不写 wiki、不动同行文件 |

## 七、执行注记（验证凭据摘要，供集成时核账）

| 孙任务 | 验证命令 | 实测输出 |
|---|---|---|
| 1.1 | `grep -c "^|" matrix.md` | 62（≥ 10；本注记节的表行同样计入） |
| 1.2 | `go test ./agent/org/ -run Fingerprint -count=1 -v` | `ok ... 0.447s`；9 个顶层用例＋30 个子用例全 PASS（0 FAIL），五维度＝Prompt/Tools/ModelID/Provider/APIEndpoint 逐名在册 |
| 1.3 | `go test ./agent/... -run 'HotParam\|BudgetLine\|KeepRecent\|TTL' -count=1` | 7 个包全 `ok`；新针 `TestHotSourceKeepRecentReachesRealCompaction` 实测同输入（16 refs、1800 tokens、预算线 480）下 keepRecent=1 保 2 条消息、keepRecent=5 保 5 条，`-count=3` 复跑稳定 |
| 1.4 | `go test ./agent/compress/ -count=1` | `ok ... 0.588s`（全包净） |
| 1.5 | `grep -n "生成参数\|模型路由" matrix.md` | 第二节两行各带补齐针（`TestFingerprintGenerationKnobsDimension` 五项、`TestFingerprintRouteRefDimension`） |
| 1.6 | `go test ./agent/... ./agent/org/... -count=1` | 未净：仅 C4 所述同波域 03 的在途红；`-skip 'TestPerCallOverride_'` 后 7 包全 `ok`（agent 52.5s／compress 2.4s／governance 2.0s／org 0.8s／reliability 17.2s／resources 1.5s／task 3.7s） |
| 自查 | `gofmt -l agent/org/ agent/compress/`；`go run ./scripts/comment_policy -no-baseline -v . examples/wechat-bot \| grep hotupdate_matrix` | gofmt 输出空；grep 零命中（本域两文件零违规） |

# 09.7 终态回溯 review — 03-history × 04-capabilities（26 叶）

> 审查方式：**只读**。本文件是本次审查唯一写入的产物；仓库其余文件零改动（`git status` 的非空内容是审查前既存工作区状态，不是本轮产生）。
> 审查对象：`03-history` 10 叶（03.P0.1、03.P0.2、03.1–03.8）＋ `04-capabilities` 16 叶（04.P0.1–04.P0.4、04.1–04.9、04.21–04.23），共 **26 叶**，逐叶对着 `task-graph.json` 节点 / `tasks.md` 任务行 / `midterm-review-response.md` §二 补差合同 / `prototype/` 原型语义 / 终态实现落点。
> 结论形态：**不做 PASS 汇总**。每叶给"接缝是否在正式路径上 + 有无生产消费者"的分层判定；分布计数见 §1.1（那是消费者分层分布，不是通过率）。

---

## 〇 方法与口径

### 0.1 判定分层（本报告的唯一判据）

| 档 | 含义 |
|---|---|
| **成立（真实接线）** | 该叶语义在**正式公共入口**（`tagent.Assemble` → `Run.Invoke` / `Run.Recall` / `Run.Publish` / `Run.Shutdown`）上被消费，且有具名负向锚。 |
| **接缝成立、装配未接** | 库提供导出接缝，但根不自动使用；必须由宿主显式调用才生效。语义正确，交付边界比任务行字面窄。 |
| **组件成立、无消费者** | 组件级测试通过、语义自洽，但库内与示例均无非测试消费者（"孤岛"）。 |
| **骨架残留** | 该叶产物（接口/编译断言）仍在树内，但已被后续实现覆盖或已成死面；文档仍自称为骨架。 |

"生产消费者"一律以 `grep --include='*.go'` 实测 **排除 `*_test.go` 与非测试 rig** 为准；`examples/contract/**`（编译用范例，另一入口）单独标注，不计为产品消费者。

### 0.2 本轮独立复跑（全部 `-count=1`，命令可复跑）

```
go test -race ./tests/integration/history      -count=1 -json   → 11 个顶层测试 pass / 0 fail / 1 skip
go test -race ./tests/integration/capabilities -count=1 -json   → 13 个顶层测试 pass / 0 fail / 2 skip
go test          ./tests/integration/host      -count=1 -json   → 12 个顶层测试 pass / 0 fail / 0 skip
python3 scripts/check_concept_budget.py                          → RESULT: ok，productVerified: null
python3 scripts/check_runtime_plan.py                            → RESULT: ok（仅 notes 字段 warn）
```

三处 Skip 的性质（必须点名，不得以其余绿抹平）：

1. `TestStorageCapabilities/file_tier_runs_the_real_rustviking_CLI_and_names_its_absence` — 环境无 rustviking，03.2 的 **file 档**在本机只走到"缺 CLI 具名报告"，真实 CLI 往返未验。归 09.5。
2. `TestSearchRecallsFromRealStore/the_real_embedding_endpoint_carries_the_same_recall` — 真实 embedding 端点未授权。归 09.5。
3. `TestRecallEvaluation/真实检索资源与启用门:_R24_三态` — 04.9 的**真实资源腿**未跑。归 09.5。

概念预算脚本计数（本轮实测）：`code_files 176 / interfaces 74 / packages 18 / structs 545`。

### 0.3 环境异常（如实登记）

审查前期一次终端输出读取返回了**另一并行审查会话**的输出（`python3 scripts/run_runtime_scenarios.py --scope deterministic --out /tmp/review097`，verdict: drift；S03 `message-role-sequence`、S08 `identity-relation`、S09 `tokenizer_capability` 三处 drift）。判定为共享终端串台，**未计入本报告任何结论**；本报告全部测试数字来自 §0.2 的独立复跑。

附注（旁证，不在本次结论内）：该 drift 与本域无关，但它与 09.1 notes 已登记的"golden S03 期望值过时"同向，可作为 09.1 那笔回写项属实的旁证。

### 0.4 台账维度提醒（防混用）

`ORCHESTRATION.md` 第 178–222 行那张表是**阶段一 G3 收敛口径表（规划期快照）**，其"状态"列写的是 G3 白名单是否收敛（09.1 行仍是 `blocked（未收敛）`），**不是任务完成状态**。任务状态以 `task-graph.json` 为准（26 叶全 `done`）。用该表判"09.x 未完成"、或用 task-graph 判"G3 已闭合"，两者都错。见 G12。

---

## 一 逐孙任务表（26 行）

**列义**：台账 = `task-graph.json` 的 `status` / `evidence.run_id`；本轮复跑 = §0.2 全包复跑中命中该叶的测试与子测；消费者 = 非测试调用方实测。

### 1.1 03-history（10 叶）

| # | 叶 | tasks.md 行要点 | 台账 | 本轮复跑（命中该叶） | 生产消费者实测 | 判定 |
|---|---|---|---|---|---|---|
| 1 | **03.P0.1** | memory 骨架：`OriginalRef`/`OriginalStore`/`Projection`/`Guard` 只声明 | done / `skeletonB-03p01` | 无行为可跑；`skeleton_assert.go` 仍在树内引用各构造函数 | — | **骨架残留**（接口已被实现覆盖，`memory/reference.go:10` 仍自称 "phase 1 skeleton"，见 G11） |
| 2 | **03.P0.2** | workspace 骨架：`Replica`/`Externalizer`/`Cleaner` 签名只声明 | done / `skeletonC-03p02` | 同上 | — | **骨架残留**（已被 03.6/03.7/03.8 实现覆盖） |
| 3 | **03.1** | 原文引用 + owner/format 隔离 + 精确召回 + 分段读 | done / `phase2-03.1` | `TestOriginalReferenceIsolation` 8 子测全 PASS：`same_blob_keeps_one_reference_per_owner_and_format`、`one_key_keeps_every_scope_registered_on_it`、`missing_and_expired_recall_are_named`、`a_read_runs_inside_the_scope_it_is_bound_to`、`a_read_that_fails_is_named`、`segmented_reading_keeps_whole_lines_byte_identical`、`byte_bounded_segments_stay_bounded_and_resume_exactly`、`protection_holds_outlive_one_holder` | `memory.OriginalStore` ← `workspace.ResultExternalizer`；`ScopedReader` ← `Materializer`；`examples/wechat-bot` 走同一 store | **成立** |
| 4 | **03.2** | localfile/file 后端 + 提交屏障（内存档明确不耐久） | done / `phase2-03.2` | `TestStorageCapabilities` 4 PASS + 1 SKIP（file 档真实 rustviking） | `memory.OpenTier` ← `run.go:194`（`WithStorage` 正式路径） | **成立**；file 档真实腿 **SKIP**（G3） |
| 5 | **03.3** | task/state 持久提交 + 幂等接纳 + 未确认投递重放 + 按当前绑定解析 owner | done / `dm1-03.3`（M2 重开） | `TestDurableTaskRoundTrip` G2.1–G2.5 全 PASS；`TestRestoreRefusesChainWithoutSpec` PASS | `state.NewDurableCommit` ← `run.go:199`；`Register` → `specDeclarer.Declare`（`task/lifecycle.go:135`）；`restoreChains` ← `Assemble` | **成立**（补差闭合：Spec 自包含 + 缺 Spec 具名拒） |
| 6 | **03.4** | 原文/呈现决策/预算组装/投影回放进入实际 Agent 请求 | done / `dm4-03.4`（M3 重开） | `TestProjectionReplayAndBudget` PASS；host `TestRequestCarriesProjectedBudget` PASS | `Run.AttachReplay` **仅测试调用**；`RebuildProjectionFromWAL` **零非测试调用**；`requestBoundary` 消费 `r.replays` | **接缝成立、装配未接**（G8；W2 已登记） |
| 7 | **03.5** | 保留/墓碑/恢复清点与回收协调 | done / `mreg-03.5`（M 回归重验） | `TestRecoveryBeforeReclamation` PASS | `NewLifecycle` / `NewRetention` **零非测试调用**（仅定义 + `skeleton_assert`） | **组件成立、无消费者**（G7） |
| 8 | **03.6** | 副本物化引用稳定 + 授权分段读 + 缺失重物化 | done / `mreg3-03.6`（M 回归重验） | `TestReplicaMaterialization` PASS | `NewMaterializer` ← `examples/wechat-bot/file_replica.go:77`（**另一 module**）；产品包内零消费者 | **组件成立、产品包内无消费者** |
| 9 | **03.7** | 启动/周期清理 + 活跃读写保护 | done / `dm6-03.7`（M4 重开） | `TestReplicaCleanup` PASS；`TestReplicaLifecycleProtectsActiveUse` S0–S6 八子测全 PASS（含 S5 TOCTOU、S4 父级链接拒、S6 遗留暂存件） | `ReplicaCleaner` ← `Materializer(WithCleaner)`；`Open` 无 cleaner 即 `ErrReplicaProtectionUnavailable` | **成立**（补差闭合：保护随 `Open`→`ActiveReplica.Close`） |
| 10 | **03.8** | 四格式接入同一外置路径 + 清副本后独立进程恢复 | done / `m5-03.8` | `TestLargeResultRestart` 8 子测全 PASS：G2.1/G2.2/G2.3/G2.4/G2.5/G2.6 + `原文未提交不发预览` + `副本写失败仍保留原文` | `NewResultExternalizer` ← **仅** `examples/wechat-bot/file_replica.go:81`；`NewBoundedTool` **零非测试构造**；`Exceeds` ← `examples/wechat-bot/file_replica.go:110` + 测试 | **路径成立、生产者无主**（G1；且 `NativeAgent` 红线3 使"根内自动 wrap"结构性禁止，见 §5 G1） |

### 1.2 04-capabilities（16 叶）

| # | 叶 | tasks.md 行要点 | 台账 | 本轮复跑（命中该叶） | 生产消费者实测 | 判定 |
|---|---|---|---|---|---|---|
| 11 | **04.P0.1** | tool/exec 骨架：执行声明/资源标识/停止凭证只声明 | done / `skeletonC-04p01` | 无行为可跑 | `NewRunTool` / `NewReleaseTool` **零调用** | **骨架残留 + 组件未接**（G4） |
| 12 | **04.P0.2** | tool/mcp 与 tool/remote 适配接口声明 | done / `skeletonA-04p02` | 无行为可跑 | 仅 `examples/contract/tool_{mcp,remote}/**`（编译范例） | **骨架残留** |
| 13 | **04.P0.3** | plugin 骨架：相位接口/Before/After/治理声明 | done / `skeletonB-04p03` | 无行为可跑 | `plugin` 被 `run.go`/`run_tool.go` 真实消费 | **已被实现覆盖**（非残留） |
| 14 | **04.P0.4** | memory/search 骨架：检索端口/混合召回 | done / `skeletonC-04p04` | 无行为可跑 | `search.StoreSearcher` ← `read.go:86,108` | **已被实现覆盖**（非残留） |
| 15 | **04.1** | 原生 file/skill/FunctionTool 进 `Request.Tools` 真实执行；加载态回 state；文件变化下一请求可见 | done / `dm4-04.1`（M1/M3 重开） | `TestNativeToolsAndSkillState` PASS；host `TestNativeToolFamiliesAdmitted` PASS；`TestSkillBodyReachesNextRequest` PASS | `registerNativeTools` 统一 wrap 三族（`prepare.go`）；`skillLoadTool.FixateSkillLoad` 每请求读新版本 | **成立**（补差闭合：统一受理 + 技能正文入下一请求） |
| 16 | **04.2** | exec/tmux 生命周期：停止凭证/重挂/TTL/资源租约；`unknown≠dead`；`IsTmuxAvailable` 真探测 | done / `dm9-04.2` | `TestExecutionResourceLifecycle` 8 子测 PASS（含 unknown 不判死、TTL 续行、停止未确认 retained） | `NewManager` **零产品调用**；`WithExecManager` 仅 `ExecLeaseExplainer`（`run.go:224`）；`RunTool`/`ReleaseTool` **零调用** | **组件成立、执行链未接**（G4） |
| 17 | **04.3** | MCP 连接/发现/固定网关/刷新失败/关闭 | done / `mreg3-04.3`（M 回归重验） | `TestMCPProtocolLifecycle` PASS | `tool/mcp` 仅 `examples/contract`；正式根零引用 | **组件成立、无产品消费者** |
| 18 | **04.4** | A2A 与内部 send 的显式目标/归属/权限/重试 + 接入外置路径 | done / `m5-04.4` | `TestAddressedDelivery` 7 子测 PASS | `state.NewSendFace` **零调用**；`Run.deliver` 走 `TrackDelivery`/`AcceptNotice`（mailbox）；`tool/remote` 仅 `examples/contract` | **组件成立、正式投递绕开**（G5） |
| 19 | **04.5** | warn/strict + 风险规则 + 异步审批（Hold≠Denied）+ 拒绝事实与原生参数一致 | done / `dm5-04.5`（M1 重开） | `TestGovernanceBeforeExecution` PASS；`TestCriticalCallsHoldThroughFormalPath` PASS | `policyRuling` 优先于 approver（`run_tool.go:106`）；strict 无 approver → `denyWithoutApprover` | **成立**（补差闭合：正式 wrapper 唯一裁决点） |
| 20 | **04.6** | 预算 epoch + goal 状态 + 拒绝账本 + 经事件回放恢复 | done / `mreg4-04.6`（M 回归重验） | `TestGovernanceRecovery` PASS | `DenialLedger.BindStore` / `GoalRegistry.BindStore` **仅定义**，根内无 `GovernanceEventLog` 适配器 | **组件成立、根内未接**（G6） |
| 21 | **04.7** | 时间/类型/关系检索 + 混合融合 + 真实检索源与 embedder | done / `dm8-04.7`（M4 重开） | `TestRecallConsumer` PASS；`TestSearchRecallsFromRealStore` PASS + 1 SKIP（真实 embedding） | `StoreQuerySource.ObserveStore` ← `read.go:81`；`StoreSearcher` ← `read.go:86,108` | **读面成立**；写入侧缺（D2/G2） |
| 22 | **04.8** | 价值评估 + 整理触发 + 固化物因果关联（不改写原始事实） | done / `dm10-04.8` | `TestMemoryCuration` 5 子测 PASS | `NewCuration` **零非测试调用**；唯一上游 03.5 亦零消费者 | **组件成立、无消费者**（G7） |
| 23 | **04.9** | 用已授权实际检索路径形成召回质量与开销报告，保留零增益与失败 | done / `dm10-04.9` | `TestRecallEvaluation` PASS + 1 SKIP（真实检索资源与启用门） | `supplyRetrieval` ← `Assemble`；`Run.Recall` ← `read.go` | **评测面成立**；真实资源腿 SKIP（G3）；D2 致正式链召回为空（G2） |
| 24 | **04.21** | Policy 零选择构造 + ApprovalRequest 拆块 | done / `wS1-04.21` | `check_concept_budget --rule struct-fields`：plugin 包 **0 命中**；实测 `Policy` 9 字段**全未导出**、`Open` + 8 个 `With*`；`ApprovalRequest` = `ApprovalSubject`/`ApprovalEvidence`/`ApprovalState` **3 匿名块** | `plugin.Open` ← `examples/wechat-bot/host_runtime.go:305` | **成立（构造面）**；回归锚本轮未逐个重跑（台账 4 锚 31/41/111/31） |
| 25 | **04.22** | 五空相位限期接或删 + 每窄口范例 | done / `wS2-04.22` | `ExternalizePolicy`/`Enricher`/`ViewSelector` 全树 **grep 0 残留**；plugin 相位 4 / 窄口 5 | `workspace.DecideExternalize` 成**孤儿**（仅测试调用），doc 仍称"plugin pipeline 的 overflow 决策面" | **成立但留边界成本**（G9） |
| 26 | **04.23** | memory 端口归并审视（≤7 接口） | done / `wS2-04.23` | `check_concept_budget` 全量 `RESULT: ok`；`productVerified: null` | —（静态口径，无运行时语义面） | **成立（静态口径）**；见 G10 规则未反射 |

### 1.3 分布（消费者分层，非通过率）

| 档 | 叶 | 计数 |
|---|---|---|
| 成立（真实接线，有负锚） | 03.1、03.2、03.3、03.7、04.1、04.5、04.21、04.22、04.23 | 9 |
| 接缝成立、装配/自动未接 | 03.4、03.6、04.2、04.4、04.6、04.9 | 6 |
| 组件成立、无消费者（孤岛） | 03.5、04.3、04.8 | 3 |
| 路径成立、生产者无主 | 03.8、04.7（读面成立、写入侧缺） | 2 |
| 骨架残留 | 03.P0.1、03.P0.2、04.P0.1、04.P0.2 | 4 |
| 已被实现覆盖（P0 正常退场） | 04.P0.3、04.P0.4 | 2 |

> 这张表不是"通过 9 叶"的意思：第二、三、四档的**域内机制**都在测试层成立，问题一律是"正式路径是否消费它"。第五档是 P0 骨架叶的正常退场形态，只有其中两条（03.P0.1/03.P0.2）留下了与实现不符的包级文档（G11）。

---

## 二 原型对照（`prototype/` 语义 vs 终态实现）

对照物取 `prototype/{memory,task_manager,config,plugins}.go` 四文件；实现取 `memory/**`、`task/**`、`state/**`、`run*.go`、`prepare.go`、`plugin/**`、`workspace/**`、`tool/**`。

### 2.1 `memory.go`「锁保护顺序票据 map + 快照」→ `memory` 包

**原型语义（代码事实）**：`seq++` 后 `key = strconv.Itoa(seq)` 作为**写入次序票据**；单张 `data map[string]Event`；`Put` 存 `cloneEvent` 深拷贝；`Recall` 缺键返回 `ErrMemoryMiss`；`Snapshot()/Restore()` 全量深拷贝。生产注释自陈未实现：落盘持久化、索引、淘汰策略、语义检索。

**实现语义**：
- **键语义整体更换**：`OriginalRef.Key` 是**内容寻址**身份（`workspace.OriginalRefFor` 用 sha256），不再表达写入次序。
- **作用域三元组**：`refScope{owner, format, retentionPolicy}` 作为去重作用域——"同 blob 不同 owner/format/保留策略**不得互相覆盖**"（`OriginalRef` doc 原文：MUST NOT overwrite one another）。
- **完整性**：`Integrity` + `originalBlob.verify()`，`ErrIntegrityMismatch` / `ErrContentConflict` 具名分离。
- **分段读**：`SegmentRequest/SegmentResult`，`DefaultSegmentMaxBytes = 64KiB`，`ErrSegmentRange` / `ErrSegmentPastEnd`。
- **授权读**：`ReadScope` 由可信状态一次性绑定（"不是调用方的声称"），`ErrScopeRequired`；`scopedRecall` 对跨 scope 的键**不泄露存在性**。
- **分类结果**：`RecallOutcome` 五值，且 `RecallUnclassified` 是零值并明写"never reported as a successful recall"——把"没判定"和"命中"分开，这是原型 `ErrMemoryMiss` 单值做不到的。
- **快照语义换位**：原型 `Snapshot/Restore` 是**进程内**整体深拷贝；实现把耐久性下沉为档位（`StorageMemory` 明写"declares no recovery across process exit"，`localfile` 分片快照 `.tmp`+rename 原子提交，`file` 走 rustviking CLI），并用 `StoreCapabilities.Durable` 把"能否跨进程"变成**可查询能力**。原型没有表达"不耐久"的词汇，只能靠调用方自己知道。

**语义迁移的实质**：原型票据的唯一功能是"能取回"；实现要的是"跨 owner/格式/保留策略不互覆盖 + 可校验 + 可分段 + 可授权"。**顺序性在两处残留**：`VectorSeam` 的提交队列（`Submit`/`Dropped`）与 `task.Notice` 的 `Seq`（通知身份）。即"顺序"从**存储键**迁移成了**投递身份**——不是同一语义的加强，是目标换了。

### 2.2 `task_manager.go`「owner 任务表」→ `task/**` + `state/durable.go`

**原型语义（代码事实）**：`TaskManager{owner, window, ctx, cancel, tasks, pending, wg}`；`Spawn` 先 append `running` 再起 goroutine；`Complete` 只允许 `running`/`unknown` 终结；`Restore` **丢弃 owner 不匹配**、`running → unknown` 并补发 notice；`Close` = cancel + `wg.Wait` + flush pending；`notify` 按 `window` 攒批 `PublishBatch`。

**实现语义**：
- **权威从进程内表换成持久 mutation 链**：`Commit` 先落盘后可见；幂等接纳（重复提交返回已存在事实，且**不等于通知已交付**）。
- **恢复不再"宽容"**：`TaskRecord{Spec, Fact}` 自包含，`Register` 经 `specDeclarer.Declare` 落 Spec（`task/lifecycle.go:135`），缺 Spec 的链被 `ErrSpecMissing` **具名拒绝**，而不是像原型那样一律 `running → unknown` 补个通知就算恢复。
- **owner 解析换向**：原型靠 `OwnerID` 字段相等即保留；实现按**当前编排绑定**解析（`OwnerBinding`），所以重启后新通知走新目标、旧通知仍旧实例。
- **义务显式化**：`awaiting-tool` / `awaiting-delivery` / `handed-off` 三种义务，terminal 态关执行义务；`Notice` 身份 = `TaskID + Round + Kind + Seq`。
- **攒批 → 三时点**：原型的 `window` 是吞吐优化（攒够再发）；实现的 mailbox 记 `AcceptSeq` / `Claim` / `Receipt` 三时点，是**可核对的交付账**。

**语义迁移的实质**：原型的 owner 任务表**不承诺跨进程**，所以 `running → unknown` 是"承认不知道"；实现的持久链**承诺跨进程**，于是"不知道"必须变成**具名拒绝**（缺 Spec 的链不许恢复成 unknown 冒充已恢复）。这正是 03.3 被 M2 重开的根因，也是本域相对原型唯一**语义变严**而非"变多"的地方。

### 2.3 `config.go`「ResolveTool + recall/agent 适配」→ `prepare.go` + `run*.go`

**原型语义（代码事实）**：`agent.ResolveTool = func(name)` 做**名字解析表**：`declaresTool` 前置校验；名字 `"recall"` → `owner.Memory.Recall`；`"agent:<id>"` → `AgentTask`（委派）；`validateRegistry` 校验被引用的 tool/child 存在；`buildSystem` 在 Apply 期把 skill 正文与 tools 行**固化**进 prompt。

**实现语义**：
- **没有名字解析表**：`registerNativeTools` 把 contributed / file / skill 三族用**同一个 wrap** 受理，`NativeAgent.Tools` 就是模型可见工具面；`NativeAgent` 红线3 明写 "MUST NOT replace, cache or reorder the tools and plugin elements the host declares"。
- **`recall` 换位**：`Run.Recall` 是**检索面**（`searcher.Search`，未装配返回 `ErrRetrievalUnwired`），不是"内存按键取回"。
- **`agent:<id>` 换位**：委派走 `agent.Delegator` + `Shutdown` 登记，不再靠字符串前缀。
- **skill 正文不再固化**：`skillLoadTool.Call` → `resources.FixateSkillLoad(requestID, agent, name, read)`，`read` 里 Refresh + Get + sha256 version，**每次请求边界**读新版本（负锚 `TestFileChangesReachNextRequest` / `TestSkillBodyReachesNextRequest`）。

**语义迁移的实质**：原型的三条硬编码分支（`recall`、`agent:<id>`、其余）被换成**统一受理 + 端口注入**；`recall` 从"Memory 的读方法"升格为"检索子系统"，委派从"名字前缀"升格为"端口"。同时 skill 正文从"装配期固化"改为"请求期读取"——这一条是**行为反转**（原型保证同一代绑定内正文不变；实现保证文件变化下一请求可见），不是加强。

### 2.4 `plugins.go`「Kinds 前后变换 + 结果外置折叠」→ `plugin` 包

**原型语义（代码事实）**：`ApplyPlugins(ctx, plugins, e, before)` 按 `p.Kinds` 匹配 `e.Type`，Before/After 各走一遍，顺序由调用方切片固定；空 `Kinds` 与 `Type==0` 不执行；`DenylistPlugin` 在 **Before** 把 `ToolCall` **改成** `denied ToolResult`，于是核心按变换后的 Type 路由，工具零执行、模型读到拒绝；`ResultMemoryPlugin` 只对 eligible `ToolResult` 存全文并注 `memory_key`（**正文照旧**）；`CompactResults` 在**模型消费后**把带 key 的 ToolResult 折成 80 rune 预览 + `[memory:key]`，幂等（`compacted` 标记）。

**实现语义**：
- **4 相位 / 5 窄口**：`PhaseCallTransform` / `PhaseRiskPolicy` / `PhasePostCommitObserve` / `PhaseFinalPresentation`；窄口 `TransformCall` / `RiskAssessor` / `Governor` / `PostCommitObserver` / `FinalPresenter`；一个 `Plugin{Name(), Phase()}` 只报一个相位、只实现该相位的窄口。
- **路由权收归核心**：`TransformCall` 红线明写 "MUST keep the ID the call was resolved under"、"MUST NOT execute the call itself"；变换后的调用**必须重走** admission/approval/dedup，且"被执行与记录的是变换后的那次调用"。
- **After 只读已提交事实**：`PostCommitObserver` 三条红线（MUST observe a fact whose commit already happened / MUST NOT roll back / MUST NOT write）；`FinalPresenter` 必须回答"是否被通道丢弃"，不许静默丢弃。
- **判决从示例升格为根内唯一裁决点**：`policyRuling(r.policy)` 优先于宿主 approver；`EnforcementStrict` 且无 approver → `denyWithoutApprover`。

**语义迁移的实质**：原型的插件是**内容变换器**——通过改 `Event.Type` 就能劫持路由，这是"插件特权"；实现把插件降级为**声明式钩子**，拒绝只有两条合法出路：`TransformCall` 返回 error（整 pass 拒绝，调用不执行不记录）或 `Governor` 出 `Hold`。`DenylistPlugin` 那种"改 Type 完成拒绝"的写法在新红线下属**违规**。即：原型里"顺手可做"的权限，在实现里被**显式收走**。

### 2.5 深化专节：外置折叠 → 03.8 统一外置

这一节是本域相对原型**唯一改变信息流拓扑**的改动，也是本次审查发现最多落差的地方。

**原型外置的三拍**：
1. `ResultMemoryPlugin`（Before，`Kinds=ToolResult`）：全文存 `Memory`，注 `memory_key`，**正文原样保留**；
2. 模型已经读到全文并消费；
3. `CompactResults` 事后遍历 `owner.Context()`，把带 key 的结果就地折成 80 rune + 票据，`ReplaceContext` 整表替换。

原型的根本缺陷（代码即证据，不需推测）：**折叠发生在模型读过全文之后**——token 成本已付、全文曾出现在上下文里；而且折叠是全局扫描 + 整表替换，不是"首入模前"的有界交付。

**实现外置的四拍（顺序被重排）**：
1. **先提交原文**：`Externalize` 先 `originals.Put`，`Put` 成功才生成预览；负锚 `原文未提交不发预览` 断言 `puts==1 && 零预览`；
2. **有界预览 + 票据带分段入口**：`boundedPreview` 输出 `"... N characters in total, original <key> kept; read it in segments of at most 65536 bytes from offset 0"`，预览上限取 `min(previewRunes, threshold/4)`；
3. **单一阈值来源**：`ThresholdForMaxTokens`（`MaxTokens/2*4`），`Exceeds`/`DecideExternalize`/`BoundedTool` 共用，负锚 `阈值与 MaxTokens 对齐`（31998 放行 / 31999 拦截）；
4. 可选**物化副本**：`Materializer` + `ReplicaCleaner` 保护，副本写失败报 `ErrReplicaWriteFailed` 但**原文保留**。

**实质深化（一句话）**：外置时点从"模型消费后折叠"前移到"首入模前提交"，模型**从未见过全文**。原型方案里省下的只是"上下文体积"，实现方案省下的是"从未付出的 token"——这是拓扑改变，其余（去重/完整性/分段/作用域）都是 2.1 的内存层能力。

**终态落差（必须记下，证据齐全）**：

- 03.8 的 evidence 自陈："S3 注明: task/tmux/overflow 生产者调用点接线=09.1(组合根, 架构禁边 workspace-natural task), 本叶交付消费端口+G2.6"。`ORCHESTRATION.md:698` 同句。
- **09.1 现已 `status: done`**（run_id `scenario-runner-09.1`，`--scope deterministic` 9/9 attest），其 evidence 与 notes 中**没有**这条接线（notes 只有 S03 golden 期望值过时一项）。
- 实测：`workspace.NewBoundedTool` 全树**零非测试构造**（唯一构造点是 `tests/integration/history/large_result_restart_test.go` 的 G2.6 自建），`NewResultExternalizer` 的唯一产品侧消费者是 `examples/wechat-bot/file_replica.go:81`，其用途是**投递副本**（`FormatOverflow`，在 `OpenReplica` 里按文件大小判 `Exceeds`），**不是工具结果进模型**。
- 因此"四生产者（tool / task / tmux / overflow）"里：`overflow` 有宿主侧一条（非工具结果路径），`tool`/`task`/`tmux` **零生产者**。
- 更关键的是**结构上做不成**：`NativeAgent` 红线3 "MUST NOT replace, cache or reorder the tools ... the host declares" 明确禁止组合根包装宿主工具；`workspace → task` 又是架构禁边。两条约束叠加，**根内自动接线是被设计禁止的**，合法接线点只有宿主自己。⇒ 这不是"少做了一步"，而是"义务被放错了地方"（详见 §5 G1）。
- 伴随的**文档失真三处**（同一叶、同一机制）：
  - `workspace/externalize.go:241` `DecideExternalize` doc：*"answers the overflow decision face of the plugin pipeline"* —— 04.22 已删 `ExternalizePolicy` 相位，全树 `ExternalizePolicy` grep 0；该函数唯一调用方是 `large_result_restart_test.go:392/395/400`。
  - `workspace/externalize.go:237` `Exceeds` doc：*"The four producers decide here, so no producer carries a bound of its own"* —— 四个生产者里三个不存在。
  - `workspace/externalize.go:305` `BoundedTool` doc：*"the composition root builds it over the native callable tool of an agent"* —— 组合根并未构造它（且按红线不许构造）。
- 连带：03.8 evidence 里那句 *"DecideExternalize 结构化满足 plugin.ExternalizePolicy 零新边"* 在 04.22 之后**已不成立**，但该句仍留在台账里，未见回写（G9）。
- 附带发现（同属"文档宣称的组合根接线"这一类）：`build.go` 的 `Deps` 声明了 `Externalizer` / `Cleaner` / `Capture` / `Memory` / `Telemetry`，doc 描述"Assembly draws the cross-boundary ports from Deps ... dispatches each plugin by phase"，而 `Build()` 恒返 `errNotImplemented`（`build.go:78-79`），全树 `tagent.Build(` **零调用（含测试）**。真正可用的入口是 `tagent.Assemble` → `*Run`。

---

## 三 辩证分析

### 3.1 治理/审批：原型"无"，实现"必需"——是不是本质需求？

**原型事实**：治理只是一个**可选插件示例**（`DenylistPlugin`），注释明写 "owner 为现场接缝（HITL 同机制：变换前先问人），原型不实现"；装配可以完全不带治理，工具照样跑。

**实现事实**：`Assemble` 无 `cfg.governance` 即返 `ErrGovernanceMissing`（`run.go:166-168`），负锚 `TestFormalAssemblyRequiresGovernance`；`policyRuling` 是正式 wrapper 的**唯一裁决来源**（优先于宿主 approver）；strict 且无 approver → `denyWithoutApprover`；`Hold ≠ Denied`；拒绝事实与原生参数一致（`ArgsDigest` / `Result.ToolID`）；`DenialLedger` 持久 + 回放重建。

**判定：是本质需求，不是装饰。** 三条理由都能落到代码上：

1. **副作用不可逆**。工具会删文件、发消息、写库。受理与执行之间若没有可暂停点，"先执行、后审计"永远追不回。原型靠"插件改 Type"实现的拒绝，恰恰证明这个暂停点是**可被插件绕过的**——能绕过就不叫约束。
2. **审批需要身份，不是布尔**。`ApprovalRequest` 由 `ApprovalSubject` / `ApprovalEvidence` / `ApprovalState` 三块组成（04.21 实测 3 匿名块），回答"谁批的 / 批的什么 / 批在哪个状态"。原型连审批对象都没有，`HITL` 只是注释里的一句话。
3. **拒绝必须是事实，不是异常**。`DenialLedger` 持久化并可经 `GovernanceEventLog` 回放重建，所以"某次调用被拒"重启后仍可审计；若拒绝只是 Go error，重启即失忆，审计归零。

**反面（如实）**：把治理设成**装配必需品**有真实代价——宿主哪怕只想跑一个最简 agent，也必须构造 `evolution.StructuralGate` 形状的治理对象；`examples/wechat-bot/host_runtime.go:298` 就为此写了 `evolution.NewPolicy(evolution.NewStructuralGate())`。库内**没有** `WithPassthroughGovernance` 之类的破窗，"要么有治理，要么没有运行时"。我倾向保留这个硬约束（它把"忘记治理"从运行时隐患变成装配期错误），但这是一个**应由用户裁决**的取向，不是技术上唯一解。

补充一点反讽：`Build(deps Deps)` 的 doc 把"dispatches each plugin by phase"写在组合根上，而真正装配插件相位分发的是 `Assemble`；`Build` 本身是桩。治理"必需"这个结论只在 `Assemble` 这条真实入口上成立。

### 3.2 `tool/exec` 的 TTL：它的特性本质是什么？

**现象**：`Manager` 有 lease / `Renew` / `Sweep` / `Adopt` / `Explain`；`MonitorConfig` 四档默认 30s / 60s / 90s / 120s；`DegradeError{Key, Rhythm, Reason}` 把 monitor 节奏随拒绝一起带走。

**特性本质：TTL 在这里不是"超时裁剪"，而是"租约的可解释性"。**

- `Sweep` 只处理**lease 到期**的资源，且要求**停止证据**（`SweepReport.Expired` + `StopEvidence`），不是到点就杀；
- `unknown ≠ dead`：连续 `unknown` 只累加 `ConsecutiveUnknowns` / 升级 `Escalations` 诊断，或走**显式停止**；未止义务与租约保留；
- `FakeDeadDuration = 120s` 专门处理"安静但有活进程"——**把安静当死会误杀长跑任务**，这是不可逆损失；
- `IsTmuxAvailable()` 是真实 `exec.LookPath("tmux")`（每次调用真探测，不恒真），`MonitorConfigFrom` 对退役 `compress` 键**忽略而非报错**、对畸形 duration **具名报错**——这两条与 spec 活约束一致，属真交付。

所以它的本质是：**把"我不知道它是否还活着"变成一条可以放在账上的状态**，而不是"多久算超时"。这条设计选择是对的（tmux 场景下"安静"与"结束"确实不可区分）。

**反面（如实，且是本域最硬的一处落差）**：`Manager` 在库内**没有默认构造**（`exec.NewManager` 零产品调用），`NewRunTool` / `NewReleaseTool` **零注册**（不在 `prepare.go` 的工具面里），`WithExecManager` 的唯一用途是 `ExecLeaseExplainer` 的诊断读出（`run.go:224`）。正式执行链跑的是 `task.Wrap` + `admissionGate`，**不经 `Manager`**。⇒ 04.2 交付的是"资源生命周期组件 + 诊断面"，不是"执行链上的资源租约"。`tool/exec` 那套 TTL 语义在终态树里**是空转的**（G4）。

### 3.3 概念收敛 04.21–04.23 的价值归因

**04.21（Policy 零选择构造 + ApprovalRequest 拆块）**
实测：`Policy` 9 个字段**全部未导出**，只能经 `Open(opts...)` + 8 个 `With*` 构造；`ApprovalRequest` 拆成三匿名块。
价值归因 = **防止半初始化对象**：调用方无法"挑一个 enabled 但没 ledger 的 Policy"，也无法构造缺 evidence 的审批请求。这不是减行数，是把"配置正确性"从约定变成类型约束。

**04.22（五空相位限期接或删）**
实测：`ExternalizePolicy` / `Enricher` / `ViewSelector` 全树 **grep 0 残留**；相位 7→4，窄口 8→5。
价值归因 = **每相位至少一个真消费者**，且删掉的三个确实是"零注册零调用"（其 evidence 以"`Pipeline` 的 enrichers/views/externals 字段从未被遍历"作结构自证）。
**成本（必须记）**：删相位没有连带清理**跨包的相位消费者**——`workspace.DecideExternalize` 的 doc 还挂在"plugin pipeline"上，`Exceeds` 的 doc 还说"四生产者在此决定"。收敛的边界只画在 `plugin` 包内，包外引用未回溯。这属"收敛的边界成本"，且 04.22 的 evidence 未登记（G9）。

**04.23（memory 端口 ≤7）**
实测：`check_concept_budget` 全量 `RESULT: ok`，`productVerified: null`；memory 侧 12→7（删 `Projection`/`FactLog`/`RelationStore`/`EventQuerySource`/`OwnerResolver` 五零消费端口），`kvPort` 5→2，`ScopedOriginalStore` 5→2，`LifecycleStore` 4→3。
价值归因要分层，不能一锅算收益：
- 删五个零消费端口 = **真收益**（无障碍、无风险）；
- `ScopedOriginalStore` 5→2（去掉 bare 读入口）= **安全收益**——不是计数游戏，是把"未授权的按 key 直读"这条路封掉；
- `LifecycleStore` 4→3 且能力改装配入参 = **正确性收益**（fail-closed：能力不齐就不许做破坏性清理）。

**三叶的共同局限（如实）**：验证都是**静态锚**（脚本规则 0 命中）+ 回归锚，脚本自报 `productVerified: null`（本轮复跑同样输出）。静态上限通过 ≠ 语义成立。最刺眼的一条：04.23 用"零消费者就删"的规则清了 memory 的端口，却没有人用同一条规则看**根入口**——`tagent.Build` / `Runtime.{CloseSession,Shutdown,Query,Bridge}` 零消费者、恒返 `errNotImplemented`，仍在公开面上（G10）。**规则没有反射到自己身上**，这是本轮最值得记的方法论缺口。

---

## 四 双维局部评

### 4.1 对象维度（26 叶的实现质量）

按"语义是否被真正实现 + 是否在正式路径上被消费 + 是否有负向锚"三问，分四档（不逐叶打分，只给归因）：

**A. 语义与接线都闭合（5 叶 + 3 叶构造面）**
- `03.3`：自包含 Spec + 缺 Spec 具名拒 + 幂等 + 义务 + owner 按当前绑定解析——这是全 26 叶里**语义最严**的一叶，且 `Assemble` 真消费（`run.go:199` → `restoreChains`）。
- `03.7`：保护随 `Open`→`ActiveReplica.Close` 生命周期，负锚 S0–S6 覆盖注入、重定影、释放、父级链接、文件链接治愈、TOCTOU、遗留暂存件——这是全批里**负锚最厚**的一叶。
- `04.1`：三族工具统一受理 + 技能正文每请求新读，两个 host 负锚都在正式 `Assemble→Invoke` 上跑。
- `04.5`：`policyRuling` 成为唯一裁决点，strict 无 approver 的拒绝路径是**结构性**的（不是测试里补的）。
- `03.1` / `03.2`：原文层的隔离与提交屏障语义完整，`OpenTier` 进正式路径。
- `04.21` / `04.22` / `04.23`：构造面与端口面收敛真实落地（但见 4.2 的静态口径限定）。

**B. 语义正确、交付边界比任务行窄（6 叶）**
- `03.4`：`AttachReplay` 是宿主要显式调的接缝；`RebuildProjectionFromWAL` 零调用 ⇒ "冷启动自动回放"这条隐含承诺没有实现者。任务行写的是"进入实际 Agent 请求"，技术上成立（`requestBoundary` 真消费 `r.replays`），但**"谁来填 replay"这件事没人做**。
- `03.6`：`Materializer` 只在 `examples/wechat-bot`（另一 module）被构造；库内无消费者。
- `04.2`：组件完整（8 子测），但执行链不经过它。
- `04.4`：`SendFace` 语义完整（显式目标 / 三形态拒 / Duplicate 同 AcceptSeq / 三类拒绝账），但 `Run.deliver` 走 mailbox ⇒ 白名单与 running check 在正式路径上不生效。04.4 的负锚是**直接构造 SendFace** 测的，不是穿过 `Assemble→Invoke→deliver`。
- `04.6`：回放语义完整（`BindStore` + `rebuildFromStore`），但根内没有 `GovernanceEventLog` 适配器 ⇒ 治理恢复只在测试 rig 成立。
- `04.9`：评测面诚实（保留 `gain-zero` / `gain-failed`，不美化），但真实资源腿 Skip，且 D2 让"正式链召回"没有输入。

**C. 组件成立、链路不通（3 叶）**
- `03.5` → `04.8`：`NewLifecycle` / `NewRetention` / `NewCuration` **全链零非测试消费者**；`NewReplay` 亦零。这是 26 叶里最大的"孤岛链"：上游（保留/墓碑/回收）与下游（价值评估/整理/固化）都齐，但没有入口把它们接到运行时。另有一处**账目重叠**：`workspace` 用自己的 `holdSpan` 做副本保护，`memory` 用 `ProtectionLease`/`Retention` 做原文保护——**两套保护账并存**，而 03.7 的负锚证的是前者。
- `04.3`：MCP 生命周期 110 pass，但只被 `examples/contract` 引用。

**D. 骨架残留（4 叶）**
- `03.P0.1` / `03.P0.2` / `04.P0.1` / `04.P0.2`：产物仍在树内（`skeleton_assert.go` 编译断言），语义已被 03.x/04.x 覆盖。问题不在"残留"，在**文档没跟着更新**：7 个包 Godoc 仍自称 "phase 1 skeleton"——`memory/reference.go:10`、`memory/search/port.go:10`、`agent/agent.go:10`、`tagent.go:11,33,79,88`、`state/store.go:10`、`query/types.go`、`build.go:76`。读者按文档读代码会以为这些包还没实现（G11）。其中 `tagent.go` 最严重：包注释说"its only public entry ... holds no behaviour"，而真实入口是同一文件之外的 `Assemble`。

### 4.2 纪律维度（凭据 / 消费者 / 回写 / 红线）

**凭据**：26 叶 evidence 齐备（`run_id` / `commands` / `exit_codes` / `artifact_paths`），`check_runtime_plan.py` 的"verification 全覆盖 + 测试锚存在性"两规则 `RESULT: ok`。本轮独立复跑三包全绿（§0.2）。**勾选在测试层不虚**，这是本域做得最实的一维。

**生产消费者（五查第五查）**：本轮 grep 实测出 **11 处零消费者 / 无产品消费者**：
`NewLifecycle`、`NewRetention`、`NewCuration`、`NewReplay`、`RebuildProjectionFromWAL`、`NewSendFace`、`NewRunTool`、`NewReleaseTool`、`workspace.NewBoundedTool`、`tool/mcp`（产品侧）、`tool/remote`（产品侧）。
其中与台账申报的**对齐情况**：
- 已申报：`04.9 D2`（`Run.record` 未接 `StoreEvent`）、`03.8 S3`（生产者接线留 09.1）、`04.2`（`state/resources.go` 租约移交 05.4）、`04.7`（真实 embedder 资源门）。
- **未申报**：`03.5→04.8` 全链零消费者、`04.6` 无根内 `GovernanceEventLog` 适配器、`04.2` 的 `RunTool/ReleaseTool` 未注册、`04.4` 正式投递绕开 `SendFace`、`DecideExternalize` 孤儿、`Build/Runtime` 死面。
⇒ 申报纪律在"自己域内已知的缺口"上成立，在"**叶之间、包之间的断链**"上失效。

**回写**：
- 成立：`03.4` 的 W2（`RequestChain.PartitionID` 被 `AttachReplay`/`requestBoundary` 忽略，只按 `SessionID` 查键）已登记在台账 notes。
- 失效一：04.22 删相位后，03.8 evidence 的"结构化满足 `plugin.ExternalizePolicy` 零新边"**已不成立**却仍留在台账；`workspace` 的注释仍指向已删相位。收敛波（`wS1`/`wS2`）只跑自己的回归锚，不回溯引用它的先行叶。
- 失效二：03.8 的挂账指针指向 09.1，09.1 已 `done` 却未交付该挂账项，且 09.5 的挂账清单（波25）也不含它 ⇒ **挂账在终态树里无主**。

**红线**：本轮只读；除本文件外零写入。仓库内未发现本轮引入的破坏性改动（`git status` 的非空内容为审查前既存）。

---

## 五 缺口与待裁

编号 G1–G12。每条给：证据 / 影响面 / 建议归属 / 需用户裁决的选项。**以下均为"待裁"，本报告不代替裁决。**

### G1 03.8 生产者接线无主，且结构上被两条红线夹死

- **证据**：03.8 evidence "S3 注明: task/tmux/overflow 生产者调用点接线=09.1"；`ORCHESTRATION.md:698` 同句；09.1 已 `status: done`（`scenario-runner-09.1`），其 evidence/notes 无该接线；`workspace.NewBoundedTool` 零非测试构造；`NewResultExternalizer` 产品侧仅 `examples/wechat-bot/file_replica.go:81`（投递副本，非工具结果进模型）。
- **夹死的两条约束**：`NativeAgent` 红线3 "MUST NOT replace, cache or reorder the tools ... the host declares" 禁止根内包装；`workspace → task` 是架构禁边。⇒ 库内自动接线**被设计禁止**。
- **影响面**：一个不自带 wrapper 的宿主，工具产出的大结果**永远不会外置**——03.8 的"首入模前提交"拓扑改善对产品默认路径不生效。
- **建议归属**：把"宿主必须自行用 `BoundedTool` 包装"写成正式契约（`tagent.go` 的 `NativeAgent` doc + 03-history design），并把现挂 09.1 的指针改挂到"文档契约 + 范例"；需要真跑的验证归 09.5（真实大结果场景）。
- **待裁选项**：A. 保持红线，只补文档契约（我倾向此项）；B. 放宽红线3，允许根内包装（需 architecture.json 与 04.1 负锚同步改，风险高）；C. 新增一个 `WithResultBounding` 装配选项，在 `Assemble` 内 **产生新工具切片** 而非"替换宿主工具"（需先裁定这是否仍触红线3）。

### G2 04.9 D2：`Run.record` 未接 `StoreEvent` 写入侧

- **证据**：`run_fact.go:26-56` 的 `record` 只写进程内 `r.facts` 与 `r.mu`，不写 `tier.Segments`；`FileSegmentStore.StoreEvent`（`memory/segment_store.go:210`）**零调用者**（含测试）。
- **影响面**：`supplyRetrieval` 用 `ObserveStore(ctx, tier.Segments)` 建观察表，而正式链的工具产物从不进入 `tier.Segments` ⇒ `Run.Recall` 在正式链上**恒空**。04.7/04.9 的"召回质量"因此没有输入。
- **已登记**：04.9 evidence D2 + 09.5 notes ①。
- **待裁**：写入应复用 `r.facts`（同一真源，`StoreEvent` 只做持久化落点）还是让 `record` 双写（`r.facts` + `tier`）？**前者单一真源、后者两处写有分叉风险**——建议裁定为前者，再在 09.5 落。

### G3 真实资源腿全线欠账（Skip 不得结项）

- **证据**（本轮实测）：`TestStorageCapabilities` file 档 Skip（无 rustviking）；`TestSearchRecallsFromRealStore` 真实 embedding Skip；`TestRecallEvaluation` 真实检索资源与启用门 Skip；`tool/mcp` / `tool/remote` 无真实 MCP/A2A 端点。
- **影响面**：03.2 的 file 档、04.3 的 MCP、04.4 的 A2A、04.7/04.9 的真实检索，在终态树内**零真实路径证据**。
- **归属**：09.5（其 verification 已明写"未启用可 Skip，但正式必验项不得以 Skip 结项"）。本轮不重复挂账，只登记"这四条 Skip 的性质已核"。

### G4 04.2 的 `Manager` / TTL 未进执行链

- **证据**：`exec.NewManager` 零产品调用；`NewRunTool` / `NewReleaseTool` 零调用；`WithExecManager` 仅 `ExecLeaseExplainer`（`run.go:224`）；正式执行走 `task.Wrap` + `admissionGate`。
- **影响面**：`unknown≠dead` / TTL / 停止证据这套语义在**诊断面**可读，在**执行面**不生效。
- **待裁**：A. 明确定位为"宿主可选资源管理器"，只保留诊断面（需在 04.2 evidence 与 doc 写明）；B. 要求 `Assemble` 默认注入 `Manager` 并把 `RunTool`/`ReleaseTool` 注册进工具面（会扩大工具面对模型可见，需评估与 04.1 "不重排宿主工具"的冲突）。

### G5 04.4 正式投递绕开 `state.SendFace`

- **证据**：`Run.deliver` → `r.store.TrackDelivery` + `r.store.AcceptNotice`（mailbox）；`state.NewSendFace` **零调用**；04.4 负锚直接构造 `SendFace`。
- **影响面**：`deliver_to` 白名单、`WithRunningCheck`、`refusal ledger` 在正式投递路径上**不生效**；04.4 的"显式目标"保证只对直接使用 `SendFace` 的宿主成立。
- **待裁**：A. 把 `SendFace` 接进 `deliver`（谁是投递意图的拥有者？mailbox 与 SendFace 的语义重叠需先裁）；B. 明确 mailbox 是唯一投递面、`SendFace` 是宿主侧预留，并在 04.4 evidence 补注"负锚为组件级"。

### G6 04.6 无根内 `GovernanceEventLog` 适配器

- **证据**：`plugin.GovernanceEventLog` + `DenialLedger.BindStore` / `GoalRegistry.BindStore` 均已实现，但全树无实现该接口的适配器（`BindStore` 零非测试调用）。
- **影响面**：治理恢复（拒绝账 / goal 状态）只在测试 rig 成立；重启后真实治理账不重建。
- **待裁**：A. 在 `Assemble` 里用 03.3 的 durable commit 实现 `GovernanceEventLog`（与 03.3 同链，最自然）；B. 降级为"宿主可选"，并在 04.6 evidence 写明"回放语义已交付、根内装配未交付"。

### G7 03.5 → 04.8 全链零消费者 + 两套保护账并存

- **证据**：`NewLifecycle`/`NewRetention`/`NewCuration`/`NewReplay` 零非测试调用；`workspace` 用 `holdSpan`/`ReplicaCleaner` 做副本保护，`memory` 用 `ProtectionLease`/`Retention` 做原文保护。
- **影响面**：保留 / 墓碑 / 回收 / 整理在正式运行时不发生；"保护"有两套互不知情的账，一份在 workspace、一份在 memory。
- **待裁**：A. 承认两套账分层（副本账 vs 原文账），并在 03.5/03.7 文档写明边界（我倾向此项，因两者对象确实不同）；B. 合并为一套（成本高，且 03.7 的 S0–S6 负锚需重写）；C. 给 03.5/04.8 安排真实入口（谁触发整理？预算/周期？）——这条**必须先裁**才有实施意义。

### G8 03.4 `AttachReplay` 只按 `SessionID` 键 + 无自动回放

- **证据**：`run_request.go:229-234` `AttachReplay(chain, budget, projection)` 只用 `chain.SessionID` 建 `r.replays` 键；`RequestChain.PartitionID` 未参与；`RebuildProjectionFromWAL` 零调用。
- **影响面**：同 `SessionID` 跨分区的两个链会互相覆盖投影；"冷启动回放"没有实现者，宿主必须自己调 `RebuildProjectionFromWAL` 再 `AttachReplay`。
- **已登记**：W2（台账 notes）。**待裁**：键改为 `(PartitionID, SessionID)` 的兼容成本，以及是否要在 `Assemble` 里做自动回放（与 G8 的 `RebuildProjectionFromWAL` 零调用一并解决）。

### G9 04.22 收敛后遗症：孤儿 API + 三处 doc 失真 + 03.8 论证失效

- **证据**：`workspace/externalize.go:241`（"plugin pipeline 的 overflow 决策面"）、`:237`（"four producers decide here"）、`:305`（"composition root builds it"）三处断言均不成立；`DecideExternalize` 唯一调用方是测试；03.8 evidence 的"零新边"句已失效。
- **影响面**：读者按 doc 找不到生产者；03.8 的"结构化满足"论证在终态树上不成立。
- **建议**：与 G1 一并处理——要么接上生产者（则 doc 成立），要么删 `DecideExternalize` 并改三处 doc（则 03.8 evidence 需补一条回写）。**不建议只改注释不登记**。

### G10 根入口双轨：`Build`/`Runtime`（死面）vs `Assemble`/`Run`（真实）

- **证据**：`build.go:78-79` `Build()` 恒返 `errNotImplemented`；`tagent.go` 的 `Runtime.{CloseSession,Shutdown,Query}` 与 `Bridge` 全部 stub；全树 `tagent.Build(` **零调用（含测试）**；`tagent` 包注释仍写 "its only public entry ... holds no behaviour ... phase 1 skeleton"；真实入口是 `run.go:154` 的 `Assemble`。
- **影响面**：宿主按包文档走会拿到"永远失败"的构建函数；公开面存在两套互不衔接的入口，是本域最大的**概念预算负债**（04.23 的"零消费者就删"规则若应用于此，会直接判 `Build`/`Runtime` 出局）。
- **待裁（本报告不自行定）**：A. 删 `Build`/`Runtime`/`Deps`，把 `tagent` 包文档改为 `Assemble`/`Run`；B. 实现 `Build` 为 `Assemble` 的薄包装（需先裁 `Deps` 与 `Option` 的取舍）；C. 标记 `Deprecated` 并给迁移范例。**A 与 B 是不同量级的公开面决定，必须由用户裁。**

### G11 P0 叶的文档残留（7 个包仍自称 phase 1 skeleton）

- **证据**：`memory/reference.go:10`、`memory/search/port.go:10`、`agent/agent.go:10`、`tagent.go:11,33,79,88`、`state/store.go:10`、`query/types.go`、`build.go:76`。
- **影响面**：终态可读性（读者误判实现成熟度）；且这些包恰是 03/04 两域的主战场。
- **建议**：随 G10 一并做一次"包注释与实现对齐"（不含行为改动），归属 09.6 文档闭合面。

### G12 台账维度混用风险（非缺陷，防误用）

- **证据**：`ORCHESTRATION.md:178-222` 是**规划期 G3 收敛口径表**（09.1 行仍 `blocked（未收敛）`，指白名单未收敛），与 `task-graph.json` 的 `status: done` 是**不同维度**。
- **影响面**：后续若有人用该表判"09.x 未完成"，或反过来用 task-graph 判"G3 已闭合"，都会得出错误结论。
- **建议**：在该表表头加一行"本表为规划期 G3 快照，任务状态以 `task-graph.json` 为准"，一劳永逸。

---

## 附 本轮复跑原文（可复现的最小集）

```
$ go test -race ./tests/integration/history -count=1 -json | (汇总)
  TestDurableTaskRoundTrip pass=1 skip=0
  TestLargeResultRestart pass=1 skip=0
  TestOriginalReferenceIsolation pass=1 skip=0
  TestProjectionReplayAndBudget pass=1 skip=0
  TestRecoveryBeforeReclamation pass=1 skip=0
  TestReplicaCleanup pass=1 skip=0
  TestReplicaLifecycleProtectsActiveUse pass=1 skip=0
  TestReplicaMaterialization pass=1 skip=0
  TestRestoreRefusesChainWithoutSpec pass=1 skip=0
  TestSearchQueryContract pass=1 skip=0
  TestStorageCapabilities pass=1 skip=0
  FAIL: []
  SKIP: TestStorageCapabilities/file_tier_runs_the_real_rustviking_CLI_and_names_its_absence

$ go test -race ./tests/integration/capabilities -count=1 -json | (汇总)
  TestAddressedDelivery / TestCriticalCallsHoldThroughFormalPath /
  TestExecutionResourceLifecycle / TestFormalLoopEnforcesPolicy /
  TestFormalWrapperAdmission / TestGovernanceBeforeExecution /
  TestGovernanceRecovery / TestMCPProtocolLifecycle / TestMemoryCuration /
  TestNativeToolsAndSkillState / TestRecallConsumer / TestRecallEvaluation /
  TestSearchRecallsFromRealStore  → 13 个 pass=1 skip=0（顶层）
  FAIL: []
  SKIP: TestRecallEvaluation/真实检索资源与启用门:_R24_三态
        TestSearchRecallsFromRealStore/the_real_embedding_endpoint_carries_the_same_recall

$ go test ./tests/integration/host -count=1 -json | (汇总)
  12 个顶层测试 pass=1 skip=0；FAIL: []；SKIP: []

$ python3 scripts/check_concept_budget.py
  code_files 176 / interfaces 74 / packages 18 / structs 545
  productVerified: null
  RESULT: ok

$ python3 scripts/check_runtime_plan.py
  productVerified: null
  RESULT: ok
```

**复跑注意**：`capabilities` 与 `history` 两包内各有一个"真实资源"子测在缺资源时合法 Skip；**Skip 不等于通过**，本报告不把这三处 Skip 计入任何"成立"判定。

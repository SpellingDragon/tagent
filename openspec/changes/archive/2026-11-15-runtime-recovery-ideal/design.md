# Design: runtime-recovery-ideal（重制：三挂点动态拓展为核心）

> 冻结状态：D1–D7 与 §六孙任务 verify 为派发依据，实施不得擅改；偏离上报编排者。
> 用户终裁：「重制计划，一笔做完，工作次序你定」。核心重框定（用户确立）：**三挂点（事件生产/插件管线/消费读链）的充分动态拓展性是框架核心，须作为 golden 机械守护**；压缩-投影-召回拉回三挂点轨道。退位/checkpoint 有界是此轨道上的一个结果，非全部。
> 工作次序编排者定＝**甲：先立框架级 golden 守护判据，再修特性收敛**（先立纯粹判据再收敛）。
> 基线 HEAD f1c51e87（dev）；与 config-surface-normalization 同分支独立 change，互不触碰。

## D1 第一性原理与纯粹判据

**核心教义**（ADR 0006 + mechanism-tour §2）：**事件 x（生产）→ 插件 f(x)（管线）→ 消费 g(x)（读链）三挂点是一切特性拓展的唯一轨道**；转换永远作用派生品（视图/投影/压缩产物），永不作用事实本身；三挂点充分动态拓展，作为 golden 守护。
**链教义**（59feb537 + session-checkpoint-material:9）：链是真相，checkpoint 是派生态载体非第二真相；恢复＝读最近载体＋对尾部重跑同一 g(x)。

理想态判据（可机械证伪）：

- **P0 三挂点动态拓展（核心，golden）**：所有特性经三挂点/插件注册表动态挂载，装配根不认识特性名（无具体类型硬编码）；plugins: 声明真消费；动态拓展性立为 golden 不变量＋门禁（硬编码即红）。
- **P1 单一真相**：链唯一真相，一身份一不可变事实，SHALL NOT 删/改已提交事实；退位搬迁非删除，逻辑链恒完整可按身份重放。
- **P2 单一 g(x)**：全部恢复经同一 reducer（seedFact），无全史读旁路。
- **P3 全部托管**：所有可恢复态是 Stateful 切片（task commit port + governance 收编）。
- **P4 有界恢复**：boot 解码事件数/索引规模/durable load/墙钟不随历史线性增长（真量，非正文计数代理量）。
- **P5 契约一致**：主 spec 与代码同向，legacy 条款清零，golden 同步。
- **P6 无死代码**：死生产者删、未接线脚手架接线或删、废弃 spec 退役。
- **P7 模型自主消费**：中段过程呈现为可召回 event_key 票据（[evt_KEY|type]），模型经 recall_get 自主取全文，框架不喂摘要；老段摘要是模型产出（压缩消费插件）。
- **P8 压缩可配置**：摘要预算可配置（非硬编码 2048），压缩消费是动态插件，摘要请求经租约＋声明采样参数。

**反事实检验**：删压缩消费硬编码改动态注册——无人坏（端口已齐，只差挂载）；删中段摘要改票据——无人坏（recall_get 已能按 key 取全文，只差暴露 key）；删 2048 硬界改配置——无人坏（预算本是策略量）。三处皆「滑出三挂点的硬编码/框架干预」，删之拉回轨道。

## D2 子域表

| # | 子域 | spec 能力 | 一句话边界 | 外部依赖 |
|---|---|---|---|---|
| 01 | three-hooks-golden | three-hooks-extensibility（ADDED）| 特性经三挂点动态挂载立 golden 不变量＋门禁（硬编码即红）| ADR 0006 四规则、architecture.json |
| 02 | plugin-dynamic-mount | plugin-mounting（MODIFIED）| plugins: 声明接通＋代际注册表＋压缩消费动态化 | 01 守护判据 |
| 03 | compression-configurable | compression-pipeline（MODIFIED）| 摘要预算可配置＋模型产出经租约＋压缩消费动态 | 02 |
| 04 | midband-ticket-recall | event-key-visibility（MODIFIED）＋presentation | 中段过程→event_key 票据＋recall_get 兼容＋InjectEventKeys | recall 工具、呈现降级梯 |
| 05 | task-custody | session-checkpoint-material | DurableCommit.load 改读 tasks 切片，消全史扫 | Stateful 接缝 |
| 06 | bounded-open-index | bounded-recovery | 引擎自持索引切片＋尾增量（不从 checkpoint 派生）| 引擎索引切片 |
| 07 | governance-custody | session-checkpoint-material | governance 收编 Stateful 切片并接线 | Stateful 接缝 |
| 08 | fact-retirement | fact-retirement＋storage-lifecycle | 已结事实退位，按身份可读，热 journal 有界 | 05/06/07 存活权威 |
| 09 | acceptance-real-measure | bounded-recovery | 症状锚真量（S1/S2/S3）| 05-08 落地转绿 |
| 10 | contract-sync-dead-code | （多主 spec MODIFIED）| 主 spec 转正/退役＋死码删＋golden 同步 | 代码稳定后 |

## D3 波次表（波次仅汇报分组，解锁＝前置复跑凭据绿）

| 波次 | 子域 | 解锁条件 |
|---|---|---|
| W0 启动门 | 症状锚 S1/S2/S3 红＋golden 守护门红（硬编码）＋契约冻结 | — |
| W1 框架守护 | 01 three-hooks-golden（立判据，硬编码即红）| W0 |
| W2 动态挂载 | 02 plugin-dynamic-mount（plugins 接通＋压缩动态）| W1 判据立 |
| W3 特性并行 | 03+04 合单（压缩可配置＋中段票据，共触 age_segments）∥ 05+06+07 recovery（boot 簇合单）| W2 绿 |
| W4 退位 | 08 fact-retirement（索引裁剪＋冷层 identity-read 合波）| W3 recovery 绿 |
| W5 验收/契约 | 09 acceptance（S1/S2/S3 转绿）∥ 10 contract-sync-dead-code | W4 绿 |
| W6 收尾 | F1–F4 | W5 绿 |

## D4 合并规则（R21/R28 触碰集）

- **01 独立先行（W1）**：触 scripts/gates/＋golden/runtime/architecture.json（守护不变量），与特性代码不相交；先立判据。
- **02 依赖 01（W2）**：触 api/config（plugins 解析）＋assembly/host_document.go（hostAgent plugins 字段）＋assembly/run.go（挂载）＋代际注册表；压缩消费动态化与 plugins 接通同簇合单。
- **03+04 合单（W3）**：共触 memory/replay/age_segments.go（03 去 2048 硬界、04 中段票据）→同文件簇合单串行；03 触 compress_consumer/summary_gist/api/config，04 触 replay_budget/request/InjectEventKeys/recall。
- **05+06+07 合单（W3，boot 簇）**：同触 assembly/run.go boot 路径＋runtime/state/durable.go＋runtime/task/checkpoint.go＋memory/ledger；recovery 三域合单串行。与 03+04 不相交→并行。
- **08 合波原子（W4）**：索引裁剪＋冷层 identity-read 同批（D6.12）；依赖 05/06/07 存活权威。
- **09+10（W5）**：09 验收探针植根 boot 簇（待 05-08 绿）；10 触 openspec/specs/*＋golden（待代码稳定）。
- **共享面单写**（R17）：三挂点 golden 不变量、Stateful 接缝、存活权威、退位 identity-read、票据格式 [evt_KEY|type]、索引切片格式由编排者冻结（§五）。

## D5 三级验收

- **孙任务级**：verify 三要素（正锚点名测试＋可复跑命令、负锚具名错误路径、消费者锚正式入口实测）exit 0；测试名与 -run 逐字符一致。
- **子域级**：各域 specs/*/spec.md Scenario 全绿（openspec validate --strict）。
- **变更级 DoD**：
  - [ ] S1 红转绿（journal 物理有界）＋回滚必红，两次证据。
  - [ ] S2 红转绿（boot 真量有界）＋回滚必红。
  - [ ] S3 红转绿（中段过程模型可自主召回）＋回滚必红。
  - [ ] P0 golden 守护门：硬编码特性即红（压缩动态化后绿）；动态拓展不变量登记 architecture.json。
  - [ ] P1–P8 逐项核验。
  - [ ] 全量门 exit 0。
  - [ ] 主 spec 与代码同向；死代码清零。

## D6 关键设计裁定（含被否方案）

### 核心重框定（新增）

- **D6.15 三挂点动态拓展为 golden（用户确立核心）**：「所有特性经三挂点/插件注册表动态挂载、装配根不认识特性名」立为 golden 不变量＋门禁。现状 R1-R4 守符号形态/持久面/规格文本，**不守挂载动态性**（R2 正则抓不到 NewCompressionConsumer 具体类型硬编码）；architecture.json 无此断言。新增守护：装配根出现具体特性类型构造/无条件 append 即红（扩 R2 或新规则），plugins: 声明须有生产消费方。**先立此判据（W1），特性据此收敛**。否「特性与守护一起修」（守护迁就特性）。
- **D6.16 全插件化＋builtin 默认注册（用户裁「全插件，但需默认启动流程注册 builtin，使用者可自定义拓展」）**：golden 门红集 7 处具名构造（compress/finalRound/ageFold/observePlugin/telemetry/reflection/settle）**全部**改为插件经注册表挂载（非仅 compress）。**默认启动流程注册 builtin 插件**：plugin_catalog.go 列 builtin 清单，boot 经代际注册表注册（装配根不具名构造具体类型，只转发 catalog）；框架开箱即用。**使用者可自定义**：经 plugins: 声明禁用/覆盖/扩展 builtin（hostAgent 加 plugins 字段＋解析＋装配消费，死字段接通）。否「装配根硬编码」（滑出管线挂点）；否「无默认注册」（框架开箱不可用）；否「仅 compress 动态化」（P0 门不全绿）。
- **D6.17 摘要预算可配置（P8）**：2048 硬编码两处（DefaultAgeSummaryTextLimit＋提示词 within 2048 bytes）＋golden 冻结常量→改配置字段（AgentSpec/FoldWindow 加 summary 预算），golden 从「冻结机制常量」改「可配置策略量」。摘要产出已是模型（LLM-first＋机械兜底，保留），但摘要请求须改经租约＋withHostModelParams（现绕过，用裸 native model）。否「2048 硬界」（用户：不划特殊界限、可配置）。
- **D6.18 中段票据化＋自主召回（P7）**：中段过程现状＝foldProcessDetail 铸机械摘要 Content（无 key），ticketText 格式 [ticket KEY:] 与 recall_get 不兼容，合成键 -seg- 不可召回（事实键是 cmpout-）。改：中段过程呈现为 **event_key 票据**（[evt_KEY|type]，parseKey 兼容），暴露原事件键（非合成键），模型经 recall_get 自主取全文；框架不喂摘要。实现 event-key-visibility spec 的 InjectEventKeys（现孤儿）。老段 history_summary 保留模型产出摘要（恢复基底，需稳定身份）。否「中段喂摘要」（框架干预消费挂点）。

### 退位/恢复（沿用，仍有效）

- **D6.1 物理有界＝乙（退位）**：搬迁非删除，守 spec.md:44 红线治膨胀。
- **D6.2 退位＝搬迁非删除**：已结事实搬冷层，GetEvent 透明回冷按身份恒可读。
- **D6.3 存活权威单一**：live＝checkpoint 点名∪尾∪义务租约∪链可达 blob/ref；一个权威两处消费。
- **D6.4 task 托管＝读切片非全扫**：tasks 切片扩 consumed/attempts/rejected，load 改读切片，时序移 checkpoint 后。
- **D6.5 governance 托管＝切片携尾 seq**：seedEventSeq/replay 读切片不扫全流，生产接线。
- **D6.6 验收真量**：journal 字节/索引条目/load 计数/墙钟，非正文计数。
- **D6.7 退位引擎 journal-only**：新 capability（参 commitOrder），排他安静点（持 journalKV.mu＋Run 停写闸）。
- **D6.8 引擎索引自持**：不从 checkpoint 派生（循环依赖），引擎自持持久索引切片＋尾增量。
- **D6.9 boot 入表时序**：任务账户入表先于全部切片恢复（mailbox 读 durable 欠账）。
- **D6.10 能力名归正**：checkpoint-state 并入 session-checkpoint-material；task-skeleton-compression:51 未反向只:74 反向。
- **D6.11 词汇纪律**：冷归档→退位、liveness→存活、存活集→存活键集、索引切面→索引切片、排他闸→排他安静点；不造新概念。
- **D6.12 索引裁剪＋冷层合波**：06+08 固有耦合，合波原子，S2 一次转绿无中间态。
- **D6.13 正确性/性能分层＋水位制退位**：正确性＝identity-read＋全局提交序；存活权威是性能层非正确性门；退位判据水位制（水位前已结搬冷、尾留热）。
- **D6.14 BindStore 退役**：生产 0 调用→删（已执行）。

## D7 风险与回退

- **保序陷阱**：退位重写存活键集须逆帧序去重反写（segment_store_test.go:508/journal_kv_test.go:72 钉）。
- **在途歧义**：originals-before-segments→退位只在排他安静点。
- **静默降级**：活兜底读缺失页静默跳过→改响亮失败。
- **冷层按身份读**：GetEvent 透明回冷；冷层损坏具名拒绝。
- **票据召回兼容**：中段票据须 [evt_KEY|type]（parseKey 兼容）＋原事件键（非合成键），否则 recall_get 答 missing。
- **golden 守护误伤**：新守护规则须带正反样例自测，grandfather 既有合规面一次性登记，不追溯改写。
- **回退**：退位增量能力，回退＝关闭退位（热 journal 只追加），冷层恒可读；特性回退＝恢复硬编码/摘要（症状回红）。

## §五 共享面冻结签名（编排者单写，R17/R22）

- **三挂点 golden 不变量**：装配根无具体特性类型构造/无条件 append；特性经 plugins: 声明＋代际注册表挂载；plugins: 有生产消费方。01 定义，02/03 消费。
- **票据格式**：[evt_KEY|type]（parseKey 兼容，recall_get 可取）；中段票据引用原事件键。04 定义。
- **Stateful 接缝**（runtime/state/checkpoint.go:36）：StateName/SnapshotState/RestoreState 不变。
- **存活权威**：live = checkpoint.Survivors ∪ {ref: ref.seq>Offset} ∪ obligationLeases ∪ chainReachableBlobs(live)。
- **退位 identity-read**：GetEvent(key) 对已搬迁事实 SHALL 仍返回原字节，SHALL NOT missing。
- **验收真量**：journalBytes（开档读入字节）/indexEntries（len(s.index)，非 EventCount）/durableLoadCount/wallClock。
- **摘要预算配置字段**：AgentSpec/FoldWindow 加 summary 预算（可配置），03 定义。

## §六 症状直译锚（R33，第一天红）

| 锚 | 用户视角直译 | 今天 | 修复后 | 回滚 |
|---|---|---|---|---|
| **S1** | 持续写入＋折叠＋投递结清后，journal.log 字节有界于存活键集，不随历史线性增长 | 红 | 绿 | 必红 |
| **S2** | 累积多代＋多任务存储重启，boot 解码/索引/load/墙钟不随历史线性增长（真量）| 红 | 绿 | 必红 |
| **S3** | 中段过程被 fold 后，模型仅见 event_key 票据，经 recall_get 自主取回中段过程全文（框架不喂摘要）| 红（喂摘要/key 不可召回）| 绿（票据可召回）| 必红 |

S1/S2/S3 是验收矩阵第一行，W0 写就且红；P0 golden 守护门 W0 红（硬编码）；工程化目标只能追加。

## §七 消费者枚举（R11）

- 三挂点守护：scripts/gates/（check_assembly_symbols/schema_allowlist/durable_truth/spec_hygiene）＋golden/runtime/architecture.json。
- 插件挂载：api/config（plugins 解析）、assembly/host_document.go（hostAgent）、assembly/run.go（挂载）、代际注册表 runtime/state。
- 压缩：assembly/compress_consumer.go、summary_gist.go、memory/replay/age_segments.go、api/config（预算）。
- 中段呈现/召回：memory/replay/{age_segments,replay_budget}.go、assembly/request.go、tool/recall/{get,key}.go、InjectEventKeys。
- boot/恢复：assembly/run.go、runtime/state/durable.go、runtime/task/checkpoint.go、memory/ledger/*。
- governance：features/evolution/governance/*、assembly/host.go、runtime/task/approval。
- 退位脚手架：memory/ledger/{lifecycle,retention,guard}.go。
- 契约：openspec/specs/*（task-skeleton-compression/event-key-visibility/session-checkpoint-material 等）、golden/runtime/*、docs/wiki/*。
- 远端：examples/wechat-bot（tagent.yaml 部署声明）。

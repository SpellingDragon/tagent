# 04 中段票据自主召回（特性域）

> 下文旧注入方案与立案问题为原阶段记录，不代表全部仍待实现。当前04.R1只核销票据呈现对测试模型/背景结果消费者的真实影响，04.R2完整对账S3与交接。源码按前缀判据、重复注入与预算呈现单一拥有者已交独立no-prefix变更，不以旧4.3成功记录宣称那些缺陷已解决。

## 当前边界与依赖

- 依赖：09.R1的同提交CI原始请求；最小红绿路径先于大范围实现。
- 被依赖：09.R3长矩阵、最终P7验收；no-prefix的呈现迁移读取本域明确的旧行为和缺陷交接。
- 接口面：模型实际收到的请求、召回返回原文与测试模型应答；只修测试模型对已批准票据呈现的识别，不让生产代码迎合裸正文。
- 禁止：不扩大到全面无前缀迁移、不隐藏双票据/预算缺口；若验收与后置启动条件互相等待，先在设计门明确归属，不各自声称完成。真实用户数据禁读不妨碍临时合成根消费者验收。


## Why

一级 [design.md](../../design.md) D1 P7（:22）与 D6.18（:84）确立：中段过程 SHALL 呈现为**可召回的 event_key 票据**（`[evt_KEY|type]`），模型经 recall_get 自主取全文，框架不喂摘要；框架 SHALL NOT 在消费挂点上替模型消化材料。现状六条实证（✅ 读码＋grep，行号为当前 dev 工作树）：

- **中段折的是机械摘要，正文不含任何事件键**：`foldProcessDetail`（memory/replay/age_segments.go:534-558）把中段闭合段内的 `thinking_plan`/`action_command` 折成一条 `segment_fold` 产物，正文由 `foldLineOf`（:680-682）拼 `[MM-DD HH:MM|gen:<g>] <type>: <summary>`，一行无键。
- **被折原键随之退出投影可见面**：`forgetDroppedLocked`（replay_budget.go:293-315）删掉条目不再携带的 `originals`/`seen`/`consumed`；呈现取原文的 `originalOf`（:272-279）随之为空，模型只剩 `entry.Content`。
- **票据格式与 recall 不互通**：`ticketText`（replay_budget.go:317-324）产出 `[ticket KEY: full text retrievable by segment]`；`parseKey`（tool/recall/key.go:21-33）只认 `[evt_KEY|type]`/`evt_KEY`/裸键，同一串在 :24 被按首个 `|` 切成裸键 `ticket KEY: full text retrievable by segment` 去查表。归档证据亦已记录该不互通（openspec/changes/archive/2026-10-25-event-consumption-integrity/changes/02-event-consumers/evidence/02.3-summary.json:348）。
- **中段票据即使指名产物键也必答 missing**：产物条目键是合成键 `-seg-<hex>`（`ageKey` age_segments.go:712-722；`SyntheticKey` projection.go:274-276），落链的事实键却是 `cmpout-<hex>`（api/event/compress_product.go:74-83，:77）→ `recall_get("-seg-…")` 在 `Table.Event`（tool/recall/get.go:131-137）处未持有 → `reason=missing`。
- **被覆盖的原键只在事实元数据里，模型看不见**：`planFold` 的 `Covered: processDetailKeys(segment.Entries)`（age_plan.go:99-111，:108）经 `metaKeyCompressCovered`（assembly/compress_consumer.go:39、:563）落进产物事实元数据；模型侧只有摘要（`presentedProjectionContent` assembly/request.go:1340-1350）。
- **event-key-visibility 的注入要求是孤儿**：主 spec `event-key-visibility` 的 Requirement「事件键在请求呈现面注入」（旧行号引用 :9-13）要求以 `[evt_<KEY>|<type>] ` 前置注入，Go 树 grep `InjectEventKeys` 零命中、`BeforeModel` 零命中；真实呈现接缝是 `projectedSegment`（assembly/request.go:1142-1170，:1162）→ `presentedProjectionContent`（:1340-1350）。

**契约违背（P5）**：主 spec `task-skeleton-compression` 的 Requirement「老带段整段折叠为一条历史综述」与其 Scenario「综述正文由模型产出并服从声明预算」、Requirement「进行中段与近期窗口保持原样」（旧行号引用 :74/:94/:173）已承诺「被折叠的原始事件仍留在事实链，可经 `[evt_key]` 票据召回」，但今天呈现面不含任何票据——这条 I4 无损在消费侧没有兑现路径。

## What

- **中段过程→event_key 票据**：中段产物正文逐条携带被折条目自己的票据（原事件键），紧随其 provenance 锚与类型读法（`[MM-DD HH:MM|gen:<g>] [evt_KEY|type] <type>: <summary>`）；行首仍是锚，既有折行契约与其断言零改。
- **票据格式归正为 `[evt_KEY|type]`**：`ticketText` 收为唯一生成点并改格式（parseKey 兼容），键为空或合成键时返回空串——**不给不可召回的地址发票据**；呈现降级梯与 notice 卡共用同一形态。
- **InjectEventKeys 落到真实接缝**：在 `presentedProjectionContent`（assembly/request.go:1345-1350）为携带事实键的呈现条目前置同一票据，幂等（已以 `[evt_` 开头则跳过），MUST NOT 重建无 Go 实现的 `BeforeModel` 回调。产物条目（合成键）不加票据，其正文自带每行票据。
- **模型自主召回**：模型用所见票据调 `recall_get`（tool/recall/get.go:115-177）取回原正文；召回读的是链上 `Table`/`Originals`（:131、:153），MUST NOT 依赖投影的 per-key 原文——投影不必为召回保留被折原文（`forgetDroppedLocked` 保持原样）。
- **老段 history_summary 保留模型摘要**（恢复基底）：`summarizeProduct`（compress_consumer.go:275-291，:276 仅对 `ProjectionTypeHistorySummary` 生效）不动；中段票据正文 MUST NOT 被模型摘要覆盖。

## Capabilities

- `event-key-visibility`（**MODIFIED ＋ ADDED**，同能力一份 delta）：
  - MODIFIED：注入挂点从无 Go 实现的 `ContextIntervention.BeforeModel` 归正到请求呈现面，并去掉位置匹配形态（票据由条目自身的键与类型构成）。
  - ADDED：中段过程以事件键票据呈现并可自主召回（含呈现降级梯对产物条目的降级规则）。
- **为何不新立能力**：票据是 event_key 可见性的同一事实面（D2 design.md:34 记作「event-key-visibility（MODIFIED）＋presentation」）；presentation 是同一能力在呈现降级梯上的行为，不另立能力名（D6.11 词汇纪律）。

## 边界与依赖

- **依赖**：02 plugin-dynamic-mount（压缩消费经 `plugins:` 动态挂载后，中段判定的生产者仍是同一 `CompressRequest` 族，本域只改产物正文与呈现，不改生产者身份）；`recall.New`/`recall.Reads` 面（tool/recall/get.go:82-108、assembly/tools.go:160-179 `Run.RecallReads`）与生产接线（assembly/host_document.go:770-775）。
- **合单（W3）**：与 03 compression-configurable **共触 memory/replay/age_segments.go**（03 去 2048 硬界、04 中段票据）→ 同文件簇**合单串行，不可并行派发**（一级 design.md:58、tasks.md:40）。03 另触 assembly/{compress_consumer,summary_gist}.go 与 api/config；04 触 replay_budget.go、assembly/request.go、tool/recall/*、新测试。**04 MUST NOT 触 summary_gist.go**（03 的触碰集）：故票据不进 `bandSummaryInstruction`（summary_gist.go:55 的「行首锚」措辞）所读的材料面，锚保持行首这一选择正为避让该文件。
- **接口面**：票据格式 `[evt_KEY|type]` 属 §五 共享面（design.md:116，04 定义、02/03 消费）；新增唯一导出面 `replay.TicketOf(key, typ)`（跨包单写格式，供 assembly 复用），须登记 concept_budget 绑定。
- **禁止事项**：
  - 不触外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）。
  - 不改写冻结归档（openspec/changes/archive/** 只附勘误）；不读 examples/wechat-bot/.wechat-config/data。
  - 不触 config-surface-normalization 未提交变更（同 dev 分支，独立 change）。
  - **不改摘要预算**（`DefaultAgeSummaryTextLimit` 与其配置化归 03）；不放宽任何既有体积/预算锚（`TestSymptomAnchorCompressionStaysBounded` age_segments_test.go:1264 等），撑破即按 R29 上报。
  - 老段 `history_summary` 保留模型产出摘要，MUST NOT 改为票据；中段产物正文 MUST NOT 改为 LLM 生成文本。
  - 不新增恢复旁路、不改 `EventCount`/索引语义、不新增词汇（沿用 票据 / event_key / 召回 / 呈现降级梯 / 原事件键 / 合成键）。
  - 本文档阶段**只规划不实施**。
- **契约漂移交接（归 10-contract-sync-dead-code，不在本域改主 spec）**：`task-skeleton-compression` 的 Requirement「中带段的过程细节折叠为带事件键票据的段折叠产物」（旧行号引用 :51）的段折叠正文条款（「一行读法（类型 + 摘要）」）需扩为「含被折条目的事件键票据」；Requirement「老带段整段折叠为一条历史综述」（旧行号引用 :74）「中带与老带整段折叠为一条历史综述」与现行代码（中带折过程细节为段折叠产物、老带入综述）已不一致，属**既有漂移**，非本域引入。两处均记入 [design.md](design.md) §七。

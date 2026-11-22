> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# Design: 04 中段票据自主召回

> 继承一级决策：D1 P7（design.md:22）、D6.18（:84）、§五 票据格式（:116）、§六 S3（:129）、D2/D3/D4（:34/:49/:58）、D7 票据召回兼容（:109）。本域不改一级 D 决策；偏离或细化逐条登记于此并在 §七 交接。
> **合单**：与 03 共触 memory/replay/age_segments.go，W3 合单串行（design.md:58）。

## 当前重整设计

04.R1只处理真实测试消费者：smokeSettleNoticeOf对带票据通知不匹配，使smokeProvider走delegate应答而长测等不到settle应答；Scenario03仍以裸正文相等匹配背景结果，其日志不全，需原始请求证明根因。保留真实缺失/错调用身份/错来源负例，不直接把所有字符串前缀抹掉。

04.R2维持原S3模型自主召回及原预算目标，核原4.1–4.5全体消费者；下面04-D4是已落地历史方案，不再是新设计方向。其前缀判据、双注入及未计量增量已交独立no-prefix，不在本域偷偷重写生产呈现入口。交接有循环依赖时在实现前具名解决，不能以将来修复作为当前假绿理由。

当前写入白名单/正负/消费者锚见tasks的04.R1–R2；旧W3合单表仅历史定位。测试模型匹配修复须用真实呈现请求复现红，再验证结果恰好按调用身份归属，跨会话和重复投递未放宽。

## 一、原立案现状与接缝（历史证据）

- **中段折叠**：`FoldByAge`（age_segments.go:468-524）对 `BandMid` 段调 `foldProcessDetail`（:534-558），产物 `Key = ageKey("seg", process)`（:551 → :712-722 得 `-seg-<hex>`）、`Content = strings.Join(lines, "; ")`、`lines` 来自 `foldLineOf`（:680-682）。`isProcessDetail`（:563-568）排除合成键，故被折条目键全是已提交事实键。
- **呈现链**：`assembleBudgetLocked`（replay_budget.go:167-190）逐条过 `presentWithin`（:251-268）的梯子（全文→票据→摘要→notice 卡）；请求侧 `projectedSegment`（request.go:1142-1170）逐条转成消息（:1162），内容由 `presentedProjectionContent`（:1340-1350）合成——只有 `history_summary` 加标签，`segment_fold` 原样。
- **票据**：`ticketText`（replay_budget.go:317-324）是全文唯一生成点，被 `noticeCard`（:206-235，:211）与梯子 rung 2（:258）使用。
- **召回**：`parseKey`（key.go:21-33）在 :24 按**首个** `|` 切分；`getTool.Call`（get.go:115-177）经 `Table.Event`（:131）→ `Table.Reference`（:145）→ `Originals.ReadSegmentScoped`（:153）取原文，与投影的 `originals` 映射无关。
- **中段不被模型摘要覆盖**：`summarizeProduct`（compress_consumer.go:275-291）只对 `ProjectionTypeHistorySummary` 生效（:276），中段产物保留机械正文。
- **注入面缺位**：Go 树 `grep InjectEventKeys` / `BeforeModel` 零命中；event-key-visibility 主 spec:9-13 描述的回调在本仓库没有实现形态。

## 二、裁定（含被否方案）

### 04-D1 中段产物正文逐条携被折条目的票据，票据居锚之后

行形态：`[MM-DD HH:MM|gen:<g>] [evt_KEY|type] <type>: <summary>`，`"; "` 连接。

- 保留 `[MM-DD HH:MM|gen:…] ` 行首（age_segments.go:530-533、:684-697 的锚契约）与 `<type>: <summary>` 尾（task-skeleton-compression:51「类型 + 摘要」一行读法），票据追加在两者之间：既有 8 处 `foldAnchorLine`（age_segments_test.go:23）断言零改，主 spec 的正文条款只需扩一条（§七交接）。
- **MUST NOT 触 summary_gist.go**（03 触碰集）：其 `bandSummaryInstruction`（:55）以「行首锚」措辞要求模型保留锚，行首保持锚即不与 03 冲突；若票据居行首，该措辞即失真而必须改 summary_gist.go——跨域越界。
- 否「票据居行首」（对整行拷贝最稳，但改掉「每条折行以 provenance 锚开头」这条契约文档＋8 处断言，并与 03 触碰集相撞）。
- 否「不折，直接把原条目按票据呈现」（`presentWithin` 已有该形态）：中段的意义是把体量压成有界行数，保留原文条目等于取消中段折叠（违 I1 有界，task-skeleton-compression:173）。

### 04-D2 票据格式单一生成点，且不给不可召回的地址发票据

`ticketText(key, typ)` 改产 `[evt_KEY|type]`（追加空格由行拼接提供），并由本域导出 `replay.TicketOf(key, typ)` 供 assembly 复用（格式属 §五 共享面，单写）。

- 键为空或 `SyntheticKey(key)`（projection.go:274-276）为真 → 返回空串；调用点（noticeCard:211、梯子 rung 2:258、折行、注入）见空串即省略票据。
- 否「保留 `[ticket KEY: …]` 并让 parseKey 兼容它」（把不可召回的合成键形态固化成契约；且与 §五 冻结格式冲突）。
- 否「各处自拼格式」（第二真相，正是本次要消掉的失效模式）。

### 04-D3 票内嵌整行时的解析：parseKey 认整行读法

模型很可能把整行贴进 `key` 参数。行内首个 `|` 属于锚（`|gen:`），故 `parseKey`（key.go:24）会把整行解析成残缺裸键 → 答 missing，形同本域未修。

- 规则：先在参数中找**带 `evt_` 前缀且以 `]` 闭合的票据 token**，找到即取其键；找不到则完全按现有三写法解析（`[evt_KEY|type]`／`evt_KEY`／裸键）。
- 工具声明（get.go:91、trace.go:71）的三种写法说明补一句「也可直接贴整行（行内票据会被取出）」，MUST NOT 改变既有三写法的含义。
- 否「票据居行首规避此解析」（见 04-D1 的两条代价）。

### 04-D4 InjectEventKeys 落在请求呈现面，幂等，只给真实事实键发票据

- 挂点：`presentedProjectionContent`（request.go:1345-1350）——一条投影条目成为模型消息的唯一位置（:1162）。注入 MUST NOT 发生在折叠或事实提交处（那会改写事实面/派生载体）。
- 规则：条目键非空且非合成 → `TicketOf(key, type) + " "` 前置；内容已以 `[evt_` 开头 → 跳过（主 spec:13 的幂等要求，覆盖「模型模仿前缀并被读回」）。产物条目（`-seg-`/`-hist-`）不加票据：它们没有可召回的事实键，其材料自行呈现（中段正文自带每行票据；老段是摘要）。
- 否「重建 `ContextIntervention.BeforeModel` 回调」（无 Go 实现的接缝，重建即与真实呈现面并存的两个注入点 = 第二真相）；否「按位置匹配 `args.Request.Messages` 与 `Session.Events`」（主 spec:11 的旧形态，位置匹配在消息被装配重排/降级后不可靠；条目自身携带键与类型，无需匹配）。

### 04-D5 召回不经投影，投影不必保留被折原文

`recall_get` 读链上 `Table`/`Originals`（get.go:131/145/153），故 `forgetDroppedLocked`（replay_budget.go:293-315）照旧丢弃被折条目的 per-key 原文；其既有断言（replay_budget_test.go:330/:402/:462）保持绿。

- 否「把被折原文留在投影供召回」（等于不折，且造第二份原文 = P1 双真相）。

### 04-D6 中段票据正文不被模型摘要覆盖

`summarizeProduct`（compress_consumer.go:276）的类型门不动；中段产物正文 MUST NOT 被 gist 改写。老段 `history_summary` 的模型摘要保留（恢复基底）。

## 三、呈现降级梯（改动后）

| 条目 | 梯级 1（全文） | 梯级 2（票据） | 梯级 3（摘要） | 梯级 4（notice 卡） |
|---|---|---|---|---|
| 携带事实键的条目 | 原文 | `预览\n[evt_KEY\|type]` | `entry.Summary` | 仅 `TypeFeedback`（:264-267） |
| `segment_fold` 产物（合成键） | 自身正文（每行自带票据） | **不发票据**，改按整行边界截断自身正文 | `entry.Summary` | 无 |
| `history_summary`（合成键） | 自身正文（标签在 request.go:1346-1348） | 不发票据，整行截断 | `entry.Summary` | 无 |

- 产物条目的梯级 2 截断 MUST 落在整行边界（`"; "` 之后），**绝不产出半张票据**；截断后至少保留一行完整票据行，否则降级到梯级 3。
- 体积：中段正文每行新增约 `len("[evt_KEY|type] ")`（键 ~10-20 字节、类型 ~15 字节）——`TestSymptomAnchorCompressionStaysBounded`（age_segments_test.go:1264）的 `DefaultAgeSummaryTextLimit + 8KiB` 余量须复跑；撑破即按 R29 上报，**禁放宽**。

## 四、触碰集与文件清单

- 改：`memory/replay/age_segments.go`（折行）、`memory/replay/replay_budget.go`（ticketText 格式＋梯级）、`assembly/request.go`（注入）、`tool/recall/key.go`＋`get.go`/`trace.go` 声明措辞（整行读法）。
- 新增导出面：`memory/replay` 的 `TicketOf(key, typ)`（concept_budget 绑定 `event-key-visibility` concept；exported_interfaces 具名 +1）。
- 测试：`memory/replay/*_test.go`、`assembly/*_test.go`、`tool/recall/*_test.go`、`tests/integration/history/`（S3 锚＋召回消费者锚）、`tests/integration/host/projection_assembly_test.go`（过时断言随行为改，:82/:155）。
- 不触：`assembly/summary_gist.go`（03）、`assembly/compress_consumer.go`（03）、`api/config`（03）、`memory/ledger/*`（05-08）、`openspec/specs/*`（主 spec 同步归 10）。

## 五、风险与回退

- **召回兼容**：票据不合 `[evt_KEY|type]` 或指名词不等于已提交事实键 → `parseKey`/`Table.Event` 答 missing。负锚须覆盖「旧 `[ticket …]` 形态仍不可解析」与「合成键不发票据」两侧。
- **角色/预算扰动**：注入给每条历史消息加前缀字节，`projectionBudget`（request.go:1192-1210）按保留原文计量，故预算判定口径不变，但既有预算/降级断言（request_test.go:584、:1035）与 host 装配（projection_assembly_test.go:42）须复跑。
- **checkpoint 体积**：产物正文进 anchor 载荷（compress_consumer.go:298-299、:310-317），票据字节随行数增长；复跑 `TestSymptomAnchorCompressionJudgmentParityAfterRestart`（checkpoint_test.go:1289）与 `TestAnchoredBootMatchesFullBoot`。
- **注入面漂移**：若后续出现第二条呈现路径，票据只在其一出现即回到「不可召回」；4.5 的级联 grep 须枚举 `presentedProjectionContent` 的全部调用方。
- **回退**：①折行回到无票据摘要行（S3 回红，recall 取不到）；②票回 `[ticket …]`（parseKey 不可解析）；③关闭注入（模型回到无键世界）。三者均不改事实面，回退后与基线零 diff（除测试断言）。

## 六、验收面（S3，冻结不缩水）

症状直译（design.md:129）：中段过程被 fold 后，模型仅见 event_key 票据，经 `recall_get` 自主取回中段过程全文（框架不喂摘要）。

- 锚：`go test ./tests/integration/history/ -run '^TestSymptomAnchorMidbandProcessRecalledByTicket$'`（新增，第一天**红**：今天请求里没有任何 `[evt_` 票据，而被折条目仍在链上可取——红只在可见性，不在可取回性，分母自证 `report.NewFolded > 0`）。
- 红转绿两次证据入 run-notes（F3）；回退 ①/② 任一 → 同一命令必红。
- **本域不缩面**：S3 判据落在正式入口 `Run.Invoke`（age_band_request_test.go:102-116 的 `ageBandRequest` 同路）＋真实召回面 `Run.RecallReads`（assembly/tools.go:160-179，生产接线 host_document.go:770-775），不以组件内手工补接线替代。

## 七、契约漂移交接（归 10-contract-sync-dead-code）

- `task-skeleton-compression` 的 Requirement「中带段的过程细节折叠为带事件键票据的段折叠产物」（旧行号引用 :51）（产物正文「一行读法（类型 + 摘要）」）→ 需扩为「一行读法 ＋ 被折条目的 `[evt_KEY|type]` 票据」；本域 delta 只写 event-key-visibility，此处仅登记。
- `task-skeleton-compression` 的 Requirement「老带段整段折叠为一条历史综述」（旧行号引用 :74）（「中带与老带整段折叠为一条历史综述」）与现行代码（中带折过程细节为段折叠产物、老带入综述，age_segments.go:490-514）**已不一致**，属既有漂移，非本域引入；归 10 一并转正。
- event-key-visibility 主 spec 的 Purpose（:5）与旧回调名（:9）在归档时按本域 RENAMED＋MODIFIED delta 收口（10 复核）。

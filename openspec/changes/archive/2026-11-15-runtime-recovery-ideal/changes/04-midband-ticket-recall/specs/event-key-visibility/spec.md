## RENAMED Requirements

- FROM: `### Requirement: Event key injection in BeforeModel`
- TO: `### Requirement: 事件键在请求呈现面注入`

## MODIFIED Requirements

### Requirement: 事件键在请求呈现面注入

请求装配 SHALL 在**呈现面**为每条携带已提交事实键的投影条目注入事件键票据，使模型手里的材料自带可召回的地址。挂点 SHALL 是 `presentedProjectionContent`（assembly/request.go:1345-1350）——一条投影条目成为一条模型消息的唯一位置（:1162）；MUST NOT 注入在折叠、产物铸造或事实提交处（那是派生载体与事实面，改写即篡改）。票据格式 SHALL 是 `[evt_<KEY>|<type>] `，由 `memory/replay` 的票据生成点单写、assembly 复用，MUST NOT 各处自拼（§五 共享面）。

票据 SHALL 只由条目自身携带的身份构成：键取该条目的事实键，类型取其冻结类型；注入 MUST NOT 依赖按位置匹配 `args.Request.Messages` 与 `Session.Events`（消息经装配、降级、配对还原后位置不可靠，且条目本就携带两者）。

注入 SHALL 幂等：内容已以 `[evt_` 开头时 SHALL 跳过该条目，MUST NOT 追加第二张票据。键为空或为合成键（`-seg-`/`-hist-` 形态，projection.go:274-276）的条目 SHALL NOT 注入票据——合成键不是可召回的事实身份，给它发票据等于承诺一个必然答 missing 的地址；其正文自行呈现（中段正文自带每行票据，老段是摘要）。

#### Scenario: 首次呈现注入票据

- **WHEN** 一条携带已提交事实键的条目被装配成模型消息，其内容不以 `[evt_` 开头
- **THEN** 消息内容 SHALL 以 `[evt_<KEY>|<type>] ` 开头，键与类型分别等于该条目的事实键与冻结类型，其后 SHALL 是原呈现内容一字不改

#### Scenario: 幂等注入不加第二张票据

- **WHEN** 一条条目的呈现内容已以 `[evt_` 开头（模型模仿票据形态并被读回）
- **THEN** 该内容 SHALL 保持原样，MUST NOT 出现第二张票据

#### Scenario: 产物条目不按合成键发票据

- **WHEN** 呈现条目是折叠产物（`segment_fold` 或 `history_summary`），其键为合成键
- **THEN** 该消息 SHALL NOT 前置任何 `[evt_-seg-…]`/`[evt_-hist-…]` 票据
- **AND** MUST NOT 出现「模型拿到票据而 recall_get 答 missing」的地址

## ADDED Requirements

### Requirement: 中段过程以事件键票据呈现并可自主召回

中段（`BandMid`，段龄落在 `KeepRecentTasks` 与老带之间的已闭合段）的过程细节折叠产物 SHALL 逐条携带**被折条目自己**的事件键票据：`foldProcessDetail`（memory/replay/age_segments.go:534-558）的正文每行形态为 `[MM-DD HH:MM|gen:<g>] [evt_<KEY>|<type>] <type>: <summary>`，`"; "` 连接；键 SHALL 是 `Covered` 所列的原事件键（age_plan.go:99-111），MUST NOT 是产物自身的合成键。每行 SHALL 仍以其自身 provenance 锚开头并保留 `<type>: <summary>` 一行读法（task-skeleton-compression:51），既有锚契约与断言因此零改。

票据 SHALL 是 `recall_get` 兼容形态：`parseKey`（tool/recall/key.go:21-33）SHALL 把 `[evt_<KEY>|<type>]`、`evt_<KEY>` 与裸键三种写法解析到同一键，并 SHALL 认「整行读法」——参数中出现带 `evt_` 前缀且以 `]` 闭合的票据时取其键，未出现时完全按既有三写法解析，MUST NOT 猜键（含锚无票据的整行 SHALL 答 missing）。框架 SHALL NOT 把被折原文内联进请求，也 SHALL NOT 用模型摘要替代中段票据：模型自主经 `recall_get` 取全文（召回读链上 `Table`/`Originals`，tool/recall/get.go:131/145/153），投影 MUST NOT 为召回保留第二份原文（`forgetDroppedLocked` replay_budget.go:293-315 照旧丢弃）。中段产物正文 MUST NOT 被模型生成文本覆盖（`summarizeProduct` 的类型门 compress_consumer.go:276 只作用于老段 `history_summary`）；老段摘要仍由模型产出并保留（恢复基底）。

呈现降级梯（replay_budget.go:251-268）SHALL 对产物条目按自身正文降级：全文不下的产物条目 SHALL 按整行边界（`"; "` 之后）截断，至少保留一行完整票据行，否则降级到条目摘要；MUST NOT 产出半张票据，MUST NOT 为产物补发合成键票据。

#### Scenario: 中段过程呈现事件键票据

- **WHEN** 一个已闭合的中段段含 `thinking_plan`/`action_command` 过程细节，折叠触发
- **THEN** 该段的过程细节 SHALL 折为一条 `segment_fold` 产物，其正文每行 SHALL 携带该被折条目自己的 `[evt_<KEY>|<type>]` 票据
- **AND** 票据键集 SHALL 恰等于该产物的 `Covered` 集合，顺序同折前

#### Scenario: 票据为 recall_get 兼容形态

- **WHEN** 模型把呈现里的票据（`[evt_<KEY>|<type>]`、`evt_<KEY>`、裸键之一）交给 `recall_get`
- **THEN** 三种写法 SHALL 解析到同一事件键并读回原正文
- **AND** 模型直接贴整行（锚 + 票据 + 一行读法）时，SHALL 取出行内票据的键并读回原正文，MUST NOT 把锚里的 `gen:` 段误当作键

#### Scenario: 票据引用原事件键而非合成键

- **WHEN** 中段产物落链，其事实键为 `cmpout-<hex>`、投影条目键为 `-seg-<hex>`
- **THEN** 呈现里的票据 SHALL 指名词条目的原事件键，MUST NOT 出现 `-seg-`/`-hist-` 形态的票据键
- **AND** 以该原键调 `recall_get` SHALL 答 `found=true`（MUST NOT 答 missing）

#### Scenario: 模型自主召回，框架不喂摘要

- **WHEN** 中段已折叠，下一次请求装配完成
- **THEN** 请求里该中段材料 SHALL 只呈现为一行一条的票据读法，被折原文 MUST NOT 内联进请求
- **AND** 取全文的唯一路径 SHALL 是模型自主调用 `recall_get`，框架 MUST NOT 代替模型把原文或摘要喂进请求

#### Scenario: 中段票据正文不被模型摘要覆盖

- **WHEN** 压缩消费执行为中段产物与老段历史综述各铸一次产物
- **THEN** 中段产物正文 SHALL 保持机械票据行（MUST NOT 被模型生成文本改写）
- **AND** 老段历史综述正文 SHALL 仍是模型产出的摘要（恢复基底保留）

#### Scenario: 降级梯绝不产出半张票据

- **WHEN** 预算不足以容纳中段产物的整段正文
- **THEN** 截断 SHALL 落在整行边界并至少保留一行完整票据行；一行都放不下时才降级到条目摘要
- **AND** MUST NOT 呈现被截断的半个 `[evt_…|…` token

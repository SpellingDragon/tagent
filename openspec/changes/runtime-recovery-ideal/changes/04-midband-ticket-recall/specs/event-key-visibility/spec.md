> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../../../CARRYOVER.md)逐项接收，不因转历史而取消。

> 当前主规格已有「事件键在请求呈现面注入」，不再重复执行历史BeforeModel重命名。本域与no-prefix对同能力的修改由10.R4按requirement合并，冲突在写主spec前拒绝；下列原呈现实现条款与新原则的差异由04.R0交接排序，不以旧前缀守卫作为新实现方向。

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

### Requirement: 真实呈现消费者认识已批准的票据

验收使用的模型替身及结果匹配 SHALL 读取真实请求中的身份、正文与调用归属，SHALL NOT 因合法的票据呈现而遗漏已到达的结算通知；错误来源、错误调用和真正缺失的结果 SHALL 仍被拒绝。

#### Scenario: 已到请求的结算通知带票据

- **WHEN** 结算通知已进入真实模型请求且正文前有合法事件票据
- **THEN** 测试模型 SHALL 识别该结算而不是走普通委派应答；同场景真实缺少结算时仍不能通过

#### Scenario: 背景结果属于另一次调用

- **WHEN** 请求中有相同正文但调用身份或来源不属于等待对象
- **THEN** 消费者验收 SHALL 不把它计为等待对象的结果，不以去掉票据后文字相等掩盖归属错误

#### Scenario: 呈现所有权重构尚未交付

- **WHEN** 独立no-prefix变更中的呈现单一拥有者或身份格式重构尚未实现
- **THEN** 本域 SHALL 只陈述已验证的票据可见性与消费者匹配，不宣称已解决重复注入或未计量增量；验收依赖冲突在归档前具名解决

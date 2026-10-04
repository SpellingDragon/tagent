# Design

## D0 四方契约（本 change 的不变量）

系统（框架+宿主）保证三件机械的事：**意图可声明**（结构通道）、**结局可见**（回执）、**账本可查**（落盘）；第四件事"此刻该不该对用户说"永远不接管——语义留在有语境的一方（agent）。合理行为不是"永不错过投递"，而是**"没有任何投递能被无感地错过"**。

| 角色 | 合理行为 | 永不去做 |
|---|---|---|
| 框架 | 忠实传递声明的血统；落盘事件级血统 | 推断意图 |
| 宿主 | 按血统机械分发；预期外终态必回执 | 决定内容语义、替 agent 补发 |
| agent | 在意图产生点声明；把回执当事实对账 | 在冥想叙事里承诺投递；无视回执 |

核心交易（明说）：**声明过的，完成即达（机械、不问时刻）；未声明的，等用户在场。** 用"未声明交付必须等待用户出现"换"永无未经声明的推送"。

## D1 被否方案（否决区）

| 方案 | 否决理由 |
|---|---|
| 白名单加 `email-inbound` | 通道≠意图；/task 是通用面（AReaL 遗产），任何走该面的 webhook 会随标签获得推送权。若端点为 poller 专用本可成立——设计必须匹配面的通用性。 |
| 机械补发 pending | 无安全域：能安全机械发的（声明过的）本来就直达；不能安全发的（未声明的）恰是机械发会出事的（内容可能过时/重复/半成品）。 |
| 工具面自由填写 Origin（A1 伪护照） | 模型在冥想轮可自填 user ⇒ 强门虚设，spam 回归。承接只能引用"用户在场时刻"的冻结记录（A2），另案立项。 |
| 投递门内容推断 | 内容特征只许参与回执分级（"预期可能存在"的代理），永不参与投递裁决——两个不等价的代价里取小者。 |
| 调大回执面（一切扣留皆回执） | 冥想每轮叙事产回执 = 告警疲劳，回执贬值。回执发给"预期的破裂"，不发给内容。 |

## D2 P1 注入声明

- `taskRequest` 增加可选 `trigger_source`。校验三点 fail-closed：非空时必须恰为 `"user"`（当前唯一合法入站意图声明；扩域需显式裁决）；端点 auth 未启用（无 token 配置）时**拒绝受理声明**（400 `declaration_requires_auth`）——防匿名伪造；缺省不传 ⇒ 行为与现状逐位相同（`Source="http"`、白名单外消化）。
- 声明落点：事件 `Metadata[trigger_source]`；`Source` 保持 `"http"`。`extractTriggerSource` 现行 L365（`firstLineage=="user"`）路径直接生效，`DeliverableLineage` 白名单与四个分发分支零改动。
- 事实链（`persistBusEvent`）自动落回合级 `trigger_source=user`（声明轮即 user 轮）；通道遥测由 `Source=http` 保留。
- mail-poller（本仓 `examples/wechat-bot/mail-poller/mail_poller.py`）POST 体加 `"trigger_source": "user"`——它知道邮件来自人类通信者，是唯一知道的一方。

## D3 K2 统一回执（终态分级表为验收基准）

| 终态 | 位置 | 动作 |
|---|---|---|
| 已送达 | SendText/DeliverFiles 成功 | 不回执（agent 自证可观察） |
| 发送失败 | main.go SendText 错误分支 | **ERROR 回执** |
| 未知/未声明血统消化 | deliverable 门（现 Infof） | WARN 回执（恒）——集成缺陷信号 |
| 冥想扣留·含交付特征 | case "meditation" | WARN 回执 |
| 冥想扣留·纯叙事 | case "meditation" | INFO 不回执（契约内静默） |
| error 分支 | case "error"（现 Infof） | WARN 回执（恒） |
| 无目标扣留 | resolveDeliveryTarget 无解（现 Warn） | WARN 回执（恒） |

- **交付特征**（仅分级用）：内容含 `[task settled]` 前缀或 `delivery/` 路径片段。明确其代理性质：漏报的代价是"迟到可见"，越界到投递裁决的代价是语义崩坏。
- **回执通道**：宿主调 `InjectMessageWithSource("delivery_receipt", …)` 走持久总线——转生后仍在账上；`delivery_receipt` 非白名单 ⇒ 回执轮自身输出静默（自言自语不外发）、不武装冥想新颖门（meditation.go:194 非用户源不武装）；与用户消息混批时 user 一票否决 ⇒ 该轮可投递（agent 可当场补投，S9 特性）。回执正文含：终态原因、血统、内容截断、目标（chat_id 或 lastActive 兜底）。
- **防自激红线**：lineage 为 `delivery_receipt` 的最终输出被消化时**不再产回执**（分发层显式豁免分支）。

## D4 K3 结算血统一级化（前提经探针修正，见 fail-before.log）

探针实测：`buildBusFact` 的 `source_snapshot` 已全量无损保存事件元数据（含结算血统），fresh 与 prepared-fact 两路径共享同一构造点（claim 期序列化的也是 buildBusFact）——故 K3 不是“补落盘”，是**提升一级可读性 + 修空串陷阱**：

- ① `SourceTask` 分支补拷：`evt.Metadata[trigger_source]`（settle 事件自带，`event_bus.go:250` 由 SettleSignal.Lineage 盖）→ 顶层 `settle_trigger_source`（独立键，快照原样保留）。两键并存使“回合血统 vs 事件血统”免解码直接对账（用户血统结算件被冥想回合消费的形态将可度量）。
- ② 空串陷阱：现码无条件写 `fullEvent.Metadata[trigger_source] = cm.triggerSource`，回合血统为空时一级键落空串——“键存在但为空”被误读为“未盖章”（事故取证失败的现场形态）。改为空则不写，与同函数族 `buildEventAttributes` 的既有 `!= ""` 保护同形。
- 读方：`QueryEvents` 返回的 FullEvent.Metadata 直接可读；不新增 API。

## D5 验证口径（fail-before）

- P1：现注入无声明 ⇒ `extractTriggerSource` 判 `http`（红：期望 user 的断言失败）→ 加声明后判 `user` 且 `Source=http`；无 auth 时声明 400；非法值 400。
- K2：七终态 × 断言回执有/无与级别（构造各血统+特征形态的最终事件过分发层）；回执轮二次输出零回执；`[user, receipt]` 混批一票否决。
- K3：① 一级键 `settle_trigger_source` 免解码可读（现码无该键，必红）；② 回合血统为空时一级 `trigger_source` absent（现码落空串，必红）；快照保留断言防回归。
- 全量：`go test ./... -short -count=1`、`-race`（根+agent+rl 包）、`bash scripts/lint.sh`、`openspec validate --strict`、CI 四 job；换装后由彼方邮件轮真跑 S2（dogfood）。

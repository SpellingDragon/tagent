## Context

**根因假设链已证伪、取证重启（2026-09-15 重大修正）**：原假设「看板快照以 user source 注入事件批、压过冥想/任务世系」**不成立**。实证：看板**不走事件管道**，而是 BeforeModel 请求期动态注入——`context_manager.go:406` RegisterBeforeModel 回调内调 `cm.injectLiveTaskBoard(args)`（:1221-1227：`task.RenderBoard(cm.taskController.List())` 非空则 `task.InjectBoard` **直接改 args.Request.Messages**，注释自供 "The board is ephemeral (request-only, never projected/compressed)"）。旁证：轨迹 3977 条记录中 board 内容 0 命中（Twice 验证，防静默 0 的重查仍为 0）。**推论：board 不经过 extractTriggerSource 的事件批，不可能直接压过 meditation source——04:52 泄漏的 trigger 判定另有成因，根因取证回到两问：① cm.triggerSource 在冥想批的实值；② main.go switch 覆盖面/其他投递通道**。

**已排除面（本轮实证 + 读码，防重复取证）**：

- 看板事件批回流假设（原 1.1 回流管道追踪）：**已证伪**——board 是 request-only 消息层注入，never projected/compressed，不产生事件批记录（轨迹 0 命中双重验证）
- main.go:371-375 消费端**空值兜底嫌疑**（本轮读码新增）：`triggerSource := meta.TriggerSource; if triggerSource == "" { triggerSource = "user" }`——**若冥想批 trigger_source 导出为空，消费端当 user 投递**（泄漏候选成因之一，待取证核答）
- main.go:488 起 Non-final events 按 message role 分发通道（本轮读码新增）：非 final 事件存在独立于 trigger_source switch 的投递路径，覆盖面待盘点（泄漏候选成因之二）
- main.go:400-481 switch 覆盖面已读码核验：meditation/error 扣留（:403-407）、user/task 投递（:408-477，含 chatID 空回退 lastActiveChat）、default 未知源仅记日志（:478-481）——switch 本身完备，风险在入口（空值兜底）与旁路（non-final 通道）

**e2195fc（读侧世系）/8bab6a4（写侧 Origin 盖章）两修复机制仍成立**（世系读写链路本身正确），但它们防的是「世系键缺失」，未防「导出为空/旁路投递」——泄漏真因在此两处之侧。

【D0 原则（用户拍板，不变）】**事件分类与投递路由是框架职责，在事件层闭合，与 LLM 收到什么 role 无关；禁止把"渲染为 user role"当门禁判定依据**——门禁判定只能依据事件层类别（external_input 的 Source / 类别字段），不能依据消息层 role 形态。

**已证伪链（留档防复用）**：

1. ~~RenderBoard 看板快照以 user source 注入事件批~~ → 实为 request-only 消息层注入（context_manager.go:1221-1227），不产生事件批记录
2. ~~extractTriggerSource 优先级链"真实 user 必胜"被看板伪装的 user source 合法压过~~ → board 不入批，无压制路径
3. ~~看板注入 + "user 消息经事件管线回流"管道存在，回流环节待钉死~~ → 回流假设整体证伪；消费端读取机制不变，但**空值兜底 "user"**（main.go:375）与 non-final 旁路（:488+）成为新嫌疑面
4. ~~泄漏特征与"看板伪装"假设完全吻合~~ → 该吻合是伪相关：泄漏发生时后台任务在跑 ⇒ 看板注入生效，但看板不影响事件批——真正的共因另有其物（待 §1 两问取证）

**board 的 role 呈现问题（解耦处理）**：看板以 RoleUser 注入（user 位）仍违反「内部观察不冒充用户」原则，但属**呈现层问题，与投递门禁解耦**——单独立项处理（见 tasks §5），不阻塞本计划门禁修复。

关键事实坐标（已实读，2026-09-15 本轮核验）：

- main.go:371-375 消费端：ParseEventMeta(evt) 读 meta.TriggerSource，**空则兜底 "user"**；switch 仅对 "meditation"/"error" 扣留，user/task 投递（meta_chat_id / lastActiveChat 回退），default 仅记日志
- main.go:488+ 非 final 事件按 message role 分发（独立于 trigger_source 的旁路通道，覆盖面待盘点）
- context_manager.go:406 / 1211-1227：injectLiveTaskBoard（BeforeModel 请求期，request-only、never projected/compressed，直接改 args.Request.Messages）
- context_manager.go:1155-1167（8bab6a4 后）：eventCh 循环内先克隆再盖章再投递，Origin 世系（:1143-1150）使 task settle 回流 turn 经 Metadata 世系被 extractTriggerSource 读到
- event_loop.go:62-64：cm.SetTriggerSource(extractTriggerSource(events)) 在 BuildInvocation 之后、RunFlow 之前——写侧输入依赖批次事件的世系判定
- event_loop.go:215-244（e2195fc）：优先级 1 `Source=="user"` 立即返回；世系分支读 evt.Metadata[trigger_source]
- agent.go:422-428：SessionHook 只投用户消息事件（IsUserMessage），非冥想输出通道

## Goals / Non-Goals

**Goals:**

- 取证两问收口：① cm.triggerSource 在冥想批/泄漏 turn 的实值（轨迹 + 生产日志双证据）；② main.go switch 覆盖面与全部投递通道（含空值兜底、non-final role 分发旁路）的盘点——得出 04:52 泄漏的真实成因
- 据真实成因实施门禁修复，使两类泄漏输入（纯冥想批次 / meditation 世系 task settle）输出均不投递给用户
- 三用例回归（纯冥想不投 / user 正常投 / task 世系不投）+ 既有世系测试零回归 + 生产双证据验证

**Non-Goals:**

- 不改 meditation 触发调度/节奏（min_gap 等配置）
- 不重构 StateDelta/Metadata 双通道架构（世系读侧 e2195fc 与写侧 8bab6a4 的两路设计保留）
- 不处理与冥想无关的投递问题（如 lastActiveChat 持久化）
- 不动看板 role 呈现（user 位注入）——呈现层问题解耦单独立项（见 tasks §5），本计划不因它改变门禁设计

## Decisions

### D0：事件层闭合原则（用户拍板，最高优先级，不变）

事件分类与投递路由是**框架职责**，必须在**事件层**闭合，与 LLM 收到什么 role 无关。禁止把"渲染为 user role"当门禁判定依据。推论：

- 修法落在**事件类别与 trigger_source 导出**，不动消息层 role 形态（看板继续以 RoleUser 注入 LLM——role 是给模型看的形态，不是门禁依据；其"冒充用户"的呈现层异议另立项处理）
- 消费端 main.go switch 分支逻辑保持不变（扣留 meditation/error 的机制本身正确）；**但 main.go:375 的空值兜底 `triggerSource == "" → "user"` 在新证下成为头号嫌疑**——若取证证实冥想批导出为空，修法应落在「空值不兜底为 user」（fail-closed：未知/空 trigger 不投递，或导出侧保证非空）而非改 switch 分支

### D1：根因实证先行（root-cause.md 记录已证与已证伪）

看板回流假设已证伪（本轮修正）。剩余取证面收窄为两问：
(a) **cm.triggerSource 在冥想批的实值**——提取 03:20 / 04:52 泄漏 turn 的轨迹 span 属性 + 生产日志（[Event] Debug 行 StateDelta[trigger_source]、RunFlow 盖章值），核对消费端实际读到的值：是 "meditation"（则泄漏在旁路通道）、是 ""（则空值兜底成 user）、还是其他；
(b) **main.go switch 覆盖面/其他投递通道盘点**——除 main.go:400-481 trigger_source switch 外，盘点所有能把输出送达用户的路径：non-final role 分发（:488+）、interim/typing、DeliverFiles、其他 bot.Send 调用点，确认冥想输出是否可能绕过 switch。

root-cause.md 实证记录改为：已证伪链（看板回流）+ 两问核答结论 + 真实根因链坐标 + "任务清闲时门禁正常"的对照证据 + 待修复面清单。

### D2：框架层修法（据取证结论定向，原"三件套"废止一件）

① **事件层独立类别（保留，语义不变）**：为系统注入建立 internal-observation 类别（事件 Source 增设类别值如 "internal_observation"，或 AgentEvent 增设类别字段），语义约束：内部观察不冒充 user。适用面待 §1.3 盘点结论圈定（哪些注入真正入批）。

② **extractTriggerSource 按类别确定性导出（保留）**：内部观察事件不参与优先级 1"真实 user 必胜"判定、不覆盖冥想/任务世系。

③ ~~回流环节补类别~~ **（废止——回流假设已证伪，看板不产生批内事件，无回流可补）**。替代待取证定向：
- 若泄漏成因 = **空值兜底**（冥想批 trigger_source 导出为 ""）→ 修法：导出侧保证世系非空（extractTriggerSource 对世系已知批次返回非空），且消费端空值策略从 fail-open（兜底 user）改为 fail-closed（空值不投递，仅记日志）——后者需评估对既有 user 正常投递路径的影响（3.3 防过修用例）
- 若泄漏成因 = **non-final 旁路**（冥想输出走 role 分发通道绕过 switch）→ 修法：non-final 通道同样受 trigger_source 门禁约束
- 若泄漏成因 = 其他（取证给出）→ 据实定向

### D3：回归用例落在 tests/ 集成层 + agent 单测双处（不变，前置条件修订）

用例一/二（不投递）仿 tests/async_result_routing_test.go 的 StateDelta 断言风格做集成验证。原"必须在批内构造看板注入"前置随看板假设证伪而**废止**——看板不入事件批，不再是复现前置；取而代之的前置按取证结论设定（如空值兜底成因 → 构造 trigger_source 为空/缺失的冥想批）。用例三（防过修）验证 user 正常投递与 lastActiveChat 回退，**新增子断言：user turn 的 trigger_source 为空时不因 fail-closed 改动而误伤**（若 D2③ 走 fail-closed 方向）。既有 e2195fc 六用例与 8bab6a4 写侧用例作为不可回归基线。

### D4：生产验证以"轨迹+投递日志"双证据收口（不变）

单证据（仅日志）不足以区分"冥想未触发"与"触发但未投递"。轨迹中 turn span 的 tagent.turn.trigger_source 属性 + 投递日志的 [Agent][meditation] 行共同构成闭链。验证场景必须包含泄漏复现前置（按取证结论设定）。

### D5：与 reincarnation-notice 计划顺序协调（不变）

两计划都动 main.go/注入链。建议本计划先行，或同 PR 分 commit；实施前核对 wechat-bot-reincarnation-notice 的 tasks 状态避免改同一处冲突。

## Risks / Trade-offs

- [看板注入形态改变可能破坏 prompt-cache / 模型行为] → 本计划不动看板注入（role 形态、注入位置均不变）；呈现层异议另立项
- [fail-closed（空值不投递）可能误伤正常 user 投递] → 若 D2③ 走此方向，3.3 用例必须含"user turn trigger_source 为空"子断言；先取证确认空值在正常 user 路径的出现频率再定夺
- [泄漏真因可能是多通道叠加（空值 + 旁路并发）] → §1 两问取证须对三次泄漏逐一核答，不满足于单点解释；root-cause.md 记录每次泄漏的具体通道
- [non-final 通道盘点遗漏] → grep 全部 bot.Send / SendTextToUser / SendLongText / DeliverFiles 调用点，逐一归类"是否受 trigger_source 门禁约束"，产出通道清单入 root-cause.md
- [extractTriggerSource 优先级链改动可能影响非冥想场景] → 优先级 1 收窄为"真实 user（非内部观察）必胜"，真实用户事件不受影响；3.3 防过修用例必做；lastActiveChat 回退路径（main.go:408-437 内）保留
- [测试桩无法复现生产时序] → 用例尽量走 RunFlow 真实 eventCh（仿 async_result_routing_test），避免只测纯函数
- [hotswap 热重建壳（executorOnly）绕过新逻辑] → 部署后核对二进制版本串；验证包含一次完整重启而非仅热重建
- [并行计划改同一文件] → D5 顺序协调；若同 PR 分 commit，先本计划 commit 后通报计划 rebase

## Migration Plan

1. §1 取证两问（cm.triggerSource 实值 + 投递通道盘点）→ root-cause.md 实证记录（无行为变更，可随时执行）
2. 据 D2③ 替代方向定向修法 → §2 框架层修复 → §3 回归全绿（三用例 + 按取证结论设定的前置）
3. 部署：restart-tagent.sh + healthz（180s 窗口）确认新二进制版本生效
4. §4 生产观察泄漏场景双证据收口；异常则回滚重启并更新 root-cause.md
5. 全勾后 spec validate → archive，LEDGER.md 登记

## Open Questions

- cm.triggerSource 在 03:20 / 04:52 泄漏 turn 的实值（meditation / 空 / 其他）——§1.1 取证核答，直接决定 D2③ 修法方向
- 除 main.go trigger_source switch 外的投递通道全量清单（non-final role 分发、DeliverFiles、其他 Send 点）——§1.2 盘点
- 空值兜底 fail-open → fail-closed 的迁移影响面（正常 user 路径 trigger_source 是否可能为空）——若取证指向空值成因，需在修复前评估
- 内部观察类别的具体形态（Source 新值 vs AgentEvent 新字段）——实现时按最小侵入选定，语义约束已由 D2① 锁定
- 看板 role 呈现层问题的立项归属（独立 change 还是并入呈现层计划）——tasks §5 登记，不阻塞本计划

# 02 execution — 设计

## Context

执行域把 `event` 身份、`task` 任务层、`agent` 事件调度、`plugin` 相位、子委派与关闭串成一条链。历史生产树已删除，残件不可运行（计划 §1）；旧 `rebuild-formal-runtime` 的 c02/c06/c07 只作字段线索。本设计只写「怎么做」，不改一级 design / 主 spec / 旧 change / 外部计划 / prototype。

权威来源：`运行时_重设_实施计划_304e54c3.md` §3（架构与责任）、§4.1–§4.3（身份/插件/模型与工具协议）、§5（tRPC 复用验收）、§6、§7（02.1–02.9）。

## Goals / Non-Goals

**Goals**

- 一个请求内**恰好一次**受理：注册 wrapper 收到调用即返回一次 Receipt；后台结果作为独立 observation 回流，不冒充第二次同步 tool response。
- 任务 Registry 是 state 转移的**提交载体**（在状态转移前），`terminal + outbox` 一步同提交；三种提交结果分别表达。
- 模型面单一终局归约；不引第二工具循环。
- 插件按相位分工，`Before` 恰一次、`After` 仅呈现 canonical commit 之后。
- 关闭基于**真实义务收束**，非合作 producer 的 `false` 不能伪结束。
- Handoff 通知按新目标/实例与稳定 seq 定身份。

**Non-Goals**

- 不做 memory/大结果/副本（03）、Query 读面（05）、RL 采集导出（06）、宿主 CLI/微信（08）。
- 不实现第二套 ReAct；不重写 provider 协议；不引入 DI/DAG。

## 语义与最小实体（字段/接口语义，不填空 struct 冒充冻结）

签名未定处标 **DESIGN-GATE(G1)**，须在生产调用链验证后冻结（计划 §4 首句）。

- `Event`：`Key`（canonical hex）、`Type`（`event.Type*`）、`Timestamp`、`Meta`（`event_key`/`partition_id`/`event_type`/`event_summary`/`trigger_source`/`settle_trigger_source`/`meta_*`）、**原生载荷** `Message model.Message`。**DESIGN-GATE(G1)**：`Message` 是否直接承载框架 `model.Message`（含 `ContentParts`/`Reasoning`/透传字段）须冻结，避免加一层「影子消息」。
- `Message`：`Role`（native）、`Content`、`ContentParts`（multimodal）、`ToolCalls[]`、`ToolCallID`（原生 CallID，保真）。去重作用域 = `(InstanceID, InvocationID, DecisionID, CallID)`——**CallID 非全局唯一**。
- `task.Record`：`Terminal{TaskID, Kind, SettledAt, ExitCode}` + `Outbox[]Notice` + `ExpectedRevision`/`MutationID`。**三层结果分层、MUST NOT 以单一 enum 合并**：**受理回执** `AdmissionReceipt{TaskID, Accepted|Rejected}`（受理 ≠ 结果）；**存储提交结果** `CommitResult{Committed|NotCommitted|Uncertain}`；**投递接纳结果** `DeliveryAcceptance{Accepted|Rejected|Pending}`（仅目标明确接纳才推进）。**DESIGN-GATE(G1)**：`Commit` 端口签名与幂等键 = `TaskID` + `ExpectedRevision`/`MutationID`（不用 `(TaskID, round)`——同一任务有 accepted/settled 等多 transition；事件内同轮不同 notice 以 `Notice{Kind, Seq}` 身份区分）。解析后的具体导出签名待 G1 冻结，不预写为全强制。
- `Notice`：`{TaskID, ExecutionRound, Category, Seq}`——身份含任务、轮次、通知类别与**稳定 seq**；重复运输 MUST NOT 生成第二份事实。
- `Admission`：`{Tool, Args, AdmissionReceipt}`（受理回执 ≠ 结果）；`Hold{TaskID, Reason}`（等待状态，非终局）；`Denied{Tool, Reason}`（终局拒绝事实，**零执行**）。
- 插件相位枚举（须与 01.4 的 `AgentBinding.Plugins` 一致）：请求丰富 / 风险策略 / 视图选择 / 外置策略 / 提交后观测 / 最终呈现。**DESIGN-GATE(G1)**：框架 `plugin` 回调与各相位的映射点；`Before` 恰一次的执行点。
- `Terminal` 归约：`kind ∈ {completed, failed, cancelled, partial-only, unknown}`；`unknown` MUST 告警，MUST NOT 默认 completed。

## 构造 / 调用 / 关闭时序

1. **注册期**：工具以 wrapper 注册进 task 层；wrapper 拥有「一次受理」协议——成功受理返回一次**受理回执** `AdmissionReceipt{TaskID, Accepted}`，快任务也不另走同步最终结果分支；受理失败产生一次原生工具错误响应。
2. **调用（一步提交）**：任务决定状态转移 → 经**提交载体**（Registry 在状态转移前接入）把 `terminal + outbox` 作为一步提交 → 依**存储提交结果**（`Committed`/`NotCommitted`/`Uncertain`）分派可见性与派发；受理回执与投递接纳结果**各自独立**表达，不用单一 enum 合并。持久档同提交成功后才对外可见与派发；`Uncertain` 须可观察并告警。
3. **模型终局**：单一 channel/iterator 处理；事件流 → 归约（有效终局 / usage 尾包 / partial-only / 错误 / 取消 / ProducerDone）→ 形成 failed/completed completion；不引第二循环。
4. **插件相位**：请求丰富/风险策略等执行前相位恰一次；变换后最终请求经核心复核身份与权限，同时用于执行与记录；拒绝不执行工具但产生具名配对结果；提交后观测只读 canonical commit 之后的事实。
5. **Handoff**：显式转交后，未来通知送**新接收 session 与目标实例（target instance）**并记录转交关联（源实例 source instance → 目标实例）；已开始的通知保留旧目标/身份，不追回、不因重试改投；实例关闭时未交付通知保留并按可恢复路径承接（不以 `removed` 丢弃）。
6. **关闭**：先拒新 → 结算已接受义务 → 轮询真实义务（含非合作 producer 的完成确认）；截止返回未收束原因，MUST NOT 把 `cancel`/队列取空当完成。

**资源/取消/变异/错误**：
- 取消：一次 invocation 取消不连坐同 session 其他 invocation；后台任务脱离父取消但保同代绑定至真实停止（跨域 01.5/03.3）。
- 变异：meta 常量单点定义、注入点职责归一；`meta_*` 透传不得被解读为业务字段。
- 错误：提交三态分别表达；终局未知映射为 `unknown` 并告警；失败以带 `Response.Error` 的失败响应进入事件流。

## Decisions

- **D02-D1 受理是注册 wrapper 的一次协议，不是 NoticePort 的后置装饰**：Registry 在状态转移**前**接提交载体。备选（否）：在通知端口后装饰——会在状态已转移后才发现受理失败，产生「已转移未受理」幽灵。理由：计划 §4.3、用户裁定。
- **D02-D2 `terminal + outbox` 一步同提交、存储提交三态分别表达**：`Committed`/`NotCommitted`/`Uncertain` 不合并；受理回执与投递接纳结果为**另两层**，不并入提交 enum。备选（否）：两段提交、一概宣「双方未生效」或以单 enum 混合三层——掩盖「已提交待派发」/「已存在事实但未交付」。理由：计划 §4.4。
- **D02-D3 单一终局归约、不引第二循环**：直接复用框架 model/tool 协议。备选（否）：LLMAgent 第二工具循环——计划 §4.3 明令不加。
- **D02-D4 native role/multimodal/CallID 保真**：消息不重构角色，保留 assistant 的 tool_calls+content 并存与 ContentParts。备选（否）：把多模态压成纯文本——丢保真（06.2 采集依赖）。理由：计划 §4.3。
- **D02-D5 同 session 不同 invocation 不合批**：同 invocation 的已就绪输入可自然聚合；不同 invocation 不合并模型请求但共享 session 历史。备选（否）：按 session 合批——破坏调用归属。理由：计划 §4.1。
- **D02-D6 插件相位分工、Before 恰一次、After 仅 canonical 之后**：核心最终身份授权。备选（否）：Before/After 承包所有行为——历史「提交后呈现丢事实」缺陷。理由：计划 §4.2。
- **D02-D7 Hold 与 Denied 分离、关闭真实义务收束**：审批等待登记为未执行任务并返回等待状态；非合作 producer `false` 不伪结束。理由：计划 §4.2、§4.1。
- **D02-D8 Handoff 通知改目标/实例 + 稳定 seq**：通知实例所有者区分**源实例/目标实例**，关闭未交付通知保留；备选（否）：通知不携带稳定身份或实例退役即丢弃未交付——重复运输生成第二份事实/未交付丢失。理由：计划 §4.1、§4.4。

## 消费者与反例（先枚举消费者再定验收）

| 产出物 | 真实消费者 | 反例（必须被拒绝/证明） |
|---|---|---|
| 受理回执（admission Receipt） | 模型第二轮 tool response 配对、06.2 采集 | 快任务另走同步最终结果分支；受理失败仍返回成功；与提交/投递结果混为一个 enum |
| `task.Record`/`Commit` 存储提交三态 | state 提交（03.3）、05.1 总图 | 「已提交待派发」被报成「双方未生效」；`uncertain` 被当确定成功 |
| 投递接纳结果 | 投递器、04.4、微信宿主 | 幂等提交返回「已存在事实」即当作已交付 |
| 模型终局归约 | 06.2/logprobs 保留、09 验收 | nil 流/迭代器失败被判为 completed |
| 插件相位 | 04.5 治理、05.3 观测 | Before 跑两次；After 在被拒后仍呈现成功事实 |
| Handoff 通知 | 04.4 显式投递、微信宿主 | 已开始通知被改投新实例；重复运输生成第二份 |
| 关闭收束 | 08 宿主关闭、09 soak | `cancel`/队列取空即宣称关闭完成 |

## 覆盖映射（证据映射：每个 task → 场景落点或继承源块）

| task | Requirement（能力） | delta 场景落点 | 继承源块（不改，证据映射） |
|---|---|---|---|
| 02.1 | `event-metadata-contract` ADDED「事件身份与原生载荷保真」；`unified-task-semantics` MODIFIED「任务统一语义」；`event-type-unification` MODIFIED；`thinking-plan-event-classification` MODIFIED | native role/CallID/去重作用域；提交载体 | 主 spec `event-relation-store`（父子关系，03 落地） |
| 02.2 | `unified-task-semantics` MODIFIED「任务统一语义」 | 受理/限额/取消/幂等/义务明细 | 主 spec `async-task-lifetime`（TTL 回收，04.2 落地） |
| 02.3 | `async-task-execution` MODIFIED「task_settled 为通知类 input 事件」+ ADDED「审批 Hold 与 Denied 分离」 | 受理回执/后台独立 observation/Hold≠Denied | 主 spec `delivery-dispatch-receipts`（投递回执，04.4 落地） |
| 02.4 | `persistent-event-loop` MODIFIED「模型入口错误以失败极性呈现」；`failure-polarity` ADDED「模型与 RL 单终局规则」；`request-budget-accounting` ADDED「usage 尾包归约」 | 单终局分档/usage 尾包/ProducerDone | 主 spec `llm-event-valuation`（只读） |
| 02.5 | `persistent-event-loop` MODIFIED「业务 turn 开始执行时绑定编排版本」；`subagent-turn-execution` MODIFIED「子 agent 调用可作为异步任务执行」 | 借同代绑定/按 invocation 处理/不合批 | — |
| 02.6 | `unified-task-semantics` MODIFIED「演进命名与治理插件分工」 | Before 恰一次/denied-only/After 仅 canonical 后 | 主 spec `tool-lifecycle-management`（只读） |
| 02.7 | `subagent-turn-execution` MODIFIED「子 agent 调用可作为异步任务执行」 | 调用关联/进度/终局 | 主 spec `cross-session-delivery`（只读） |
| 02.8 | `persistent-event-loop` ADDED「Handoff 未来通知的目标、实例与稳定序号」 | 未来改投新目标（目标实例）/已开始保留旧目标（源实例）/关闭未交付保留/稳定 seq | — |
| 02.9 | `persistent-event-loop` MODIFIED「循环停止为终结态」 | 先拒新/义务结算/非合作 false 不伪结束 | — |

> 说明：`event-relation-store`、`async-task-lifetime`、`delivery-dispatch-receipts`、`llm-event-valuation`、`tool-lifecycle-management`、`cross-session-delivery` 属**跨域只读消费或他域落地**，本域不出 delta、不声称其已认证。

## 文件白名单（本域本轮可写，其余不动）

- `openspec/changes/runtime-rebuild/changes/02-execution/**`（**仅 md 规划，无 Go/脚本**）。
- 实施期（本轮**不**写，候选）：`event/*.go`、`task/*.go`、`agent/*.go`、`plugin/*.go`、`tagent/*.go`、`tests/integration/execution/**`。

## 验证表（可复跑命令；本轮不跑）

| task | 命令 |
|---|---|
| 02.1 | `go test ./tests/integration/execution -run '^TestIdentityAndCommitContract$' -count=10 -json` |
| 02.2 | `go test -race ./tests/integration/execution -run '^TestTaskLifecycle$' -count=10 -json` |
| 02.3 | `go test -race ./tests/integration/execution -run '^TestNativeToolAdmission$' -count=10 -json` |
| 02.4 | `go test ./tests/integration/execution -run '^TestModelTerminalProtocol$' -count=10 -json` |
| 02.5 | `go test -race ./tests/integration/execution -run '^TestNativeConversationRoundTrip$' -count=10 -json` |
| 02.6 | `go test -race ./tests/integration/execution -run '^TestPluginPhaseOrdering$' -count=10 -json` |
| 02.7 | `go test -race ./tests/integration/execution -run '^TestDelegationCompletion$' -count=10 -json` |
| 02.8 | `go test -race ./tests/integration/execution -run '^TestHandoffFutureNotifications$' -count=10 -json` |
| 02.9 | `go test -race ./tests/integration/execution -run '^TestCloseDrainsObligations$' -count=10 -json` |

> 口径：点名用例真实 pass 才算过；选择器零匹配/Skip 不得因退出码 0 记通过。02.6 与并发/状态类加 `-race`。

## Risks / Trade-offs

- [受理写成后置装饰] → Registry 在状态转移前接载体 + `TestNativeToolAdmission` 断言「受理失败时状态未转移」。
- [终局多套归约] → 单一归约入口；`unknown` 告警不默认 completed。
- [Hold 被伪装成 Denied] → 分开建模 + 断言等待态可观测且工具零执行。
- [After 呈现先于 canonical commit] → 相位顺序约束 + 「拒绝后 After 不呈现成功事实」反例。
- [关闭伪结束] → 真实义务轮询 + 非合作 producer false 用例；截止返回未收束原因。
- [后台结果冒充同步应答] → 「后台结果为独立 observation、同 CallID 无第二次同步 response」用例。

## Migration

- 旧事件类型常量（`memory.EventType*`）以 MODIFIED 收窄为单一来源；不恢复已退役工具名/类名。
- **MODIFIED（非 REMOVED）**：`async-task-execution`「服务型任务转 alive-detached」——服务型任务的运行中/就绪/终局观测能力**按维度保留**（见该能力 delta）：就绪以独立 `Kind`+稳定 `Seq` 表达、**非 completed**、不按时间推成功；原 REMOVED 主张（Reason 引用不在本计划证据面内的旧变更）撤回，本域对该能力**不设 REMOVED**。
- 旧 `Declarative` 承载若确认退役，按能力 delta 的 REMOVED 登记，不静默丢弃。

## Open Questions / 设计待核（G0–G3）

- **G0**：事件/消息是否直接承载框架原生 `model.Message`（不建影子消息）——仍待核。`event.Type*` 常量集已随 contracts §1.1 骨架裁决冻结（8 Type + 7 MetaKey + meta_ 前缀），此项解除。
- **G1**：`Commit` 端口签名与幂等键（=`TaskID`+`ExpectedRevision`/`MutationID`，非 `(TaskID, round)`）；受理回执/存储提交/投递接纳**三层导出签名**；`Notice` 稳定 `Kind`+`Seq` 生成点；插件相位枚举与框架 `plugin` 回调映射；`AgentBinding.Plugins` 与 01.4 对齐（DESIGN-GATE，未冻结则相关 task design-blocked，不预写全强制签名）。
- **G2**：四条纵向序列（正常受理→后台→回收 / 受理失败 / 并发同 session 双 invocation / 关闭中 producer 不合作）。
- **G3**：逐 task 文件白名单 + 前置 + 与 01 共享 `tagent`/`state` 写面串行 + 与 03 共享 `event`/`memory` 边界的读写冲突登记。
- **跨域签名待核**：`Commit`/三层结果（受理回执/存储提交/投递接纳）与 03.3 持久提交、`Notice` 与 04.4/05.1、插件相位与 01.4——需 01.4/03.3/04.4 对齐后冻结；未对齐前相关 task 保持 design-blocked。

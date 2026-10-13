# 01 state-binding — 设计

## Context

清理后的工作树只剩 `config`/`state`/`tagent` 的少量残件（`state/compile.go`、`state/store.go`、`state/reload/publish.go`、`state/state.go`），引用的多个包已不存在，**不是可运行基线**（计划 §1）。旧 `rebuild-formal-runtime` 残件（c05-state-config 等）只作字段线索，其进度与勾选不继承。本设计只写「怎么做」，不改一级 design / 主 spec / 旧 change / 外部计划 / prototype。

权威来源：`运行时_重设_实施计划_304e54c3.md` §3（架构与责任）、§4.1–4.2（身份/绑定/插件行为合同）、§6（领域划分与 G0–G3）、§7（01.1–01.6 叶任务与锚点）。三级结构与编排文档由主代理负责（`ORCHESTRATION.md`、`specs/orchestration/spec.md` 仅主代理写）。

## Goals / Non-Goals

**Goals**

- `config` 成为**叶子**：只解析纯组织声明（yaml/json），不外泄可变容器，不构造活对象。
- `state` 成为生效绑定的**唯一持有者**：模型/工具/文件版本/插件管线 + agent/session 现场 + mailbox 消费位置；执行器只借用。
- 请求借「同代」绑定：在途请求持开始时的代跑完，新请求读已发布最新代；发布/回收与取用互斥，无「先读后回收」空窗。
- 组织发布唯一入口，候选记基线、同内容不换代、过期候选不覆盖、失败逆序回收。
- 文件直接热读与结构变更前置门为**两条独立路径**，不互相绕门；错误原因链保留。

**Non-Goals**

- 不做 task 生命周期/取消（02）、model 协议终局（02）、memory/大结果（03）、Query 读面（05）、装配入口（08/02.5）。
- 不内置「一 owner 一会话」；不在 config 读文件于 state；不实现评估算法。
- 不新增数据库/SQLite；不引入通用 DI/插件 DAG；不建第二编排表示。

## 语义与最小实体（字段/接口语义，不填空 struct 冒充冻结）

以下字段级描述是**设计义务**，不是已冻结签名；凡签名未定处标 **DESIGN-GATE**，实现前必须在生产调用链上验证后冻结（计划 §4 首句）。

- `config.Declaration`（纯数据）：`Entry`（entry agent 名）、`Agents`（声明态 agent 图）、`Refs`（工具/子 agent/远端引用）、`Baseline`（版本基线）、`Raw map / canonical bytes`。**DESIGN-GATE(G0)**：`Raw` 归一化算法（键序、默认值应用前后）未定，须先冻结「默认值在解析后应用、指纹基于解析后规范化内容」。
- `OrgFingerprint`（字符串）：仅覆盖影响工具声明/子调用目标/模型选择/prompt 来源的字段与完整 `ToolRef`。活对象（模型句柄、工具实例、连接、锁、generation）**不进指纹**。
- `OrgPlan`：`Fingerprint string`、`Agents`（每 agent 的执行声明 + 工具/子调用目标 + 模型引用 + prompt 来源）、`Changes`（声明面 Change / Adds / Updates / Removes）、`NextPrompt`（下一版生效 prompt）。
- `StatePlan`：`Plan OrgPlan`、`Agents map[AgentID]AgentBinding`、`Leases []LeaseRef`。
- `AgentBinding`（活绑定，候选私有构造）：`Model`（原生 `model.Model`）、`Tools`（原生 `tool.Tool`/`ToolSet`）、`Plugins`（**有序**回调管线）、`PromptSource`（文件来源 + 固化版本）、`Set`（工具集连接，Close 归资源租约）。**DESIGN-GATE(G1)**：`Plugins` 的元素类型 = 框架 `plugin` 回调 + tagent 相位标签；相位枚举（请求丰富/风险策略/视图选择/外置策略/提交后观测/最终呈现）须与 02.6 冻结一致。
- `LeaseRef{Kind, Key, Ref}`：候选资源登记回执；`Ref` 即逆序释放依据。
- `Generation`（单调递增 `uint64`）：每次**实际发布**赋号；字节相同的重载不换号、不重建资源。
- `Session`/`Mailbox`：`(AgentID, SessionID)` 键；mailbox 项含 `acceptSeq`（接纳序）、`claimState`（pending/claimed/consumed）、`consumeReceipt`。**DESIGN-GATE(G1)**：`acceptSeq` 的分配点（单一串行化点）与 `claimState` 转换的可见性边界。

## 构造 / 调用 / 关闭时序

1. **构造（候选私有）**：`config.Load`（严格解析 + 默认值 + 规范化）→ `Compile`（产出 `OrgPlan`，纯数据）→ `Prepare`（构造候选 `AgentBinding`，登记 `Leases`）→ 前置检查（治理按 `OrgPlan.Changes`）→ `state.Commit`（一次串行化提交换入 generation + 回滚快照 + 诊断）。
2. **调用（请求边界）**：请求进入 → **短锁**取当前已提交代 → 绑定借出（同代 Model/Tools/Plugins/Prompt 固化）→ 执行；期间发生发布**不影响**本次（持旧代）。
3. **关闭/退役**：先拒新接纳 → 已接受义务排空或显式取消结算（**未交付**义务保留并由可恢复路径承接，不以 `removed` 丢弃）→ 按 `Leases` 逆序释放候选独有资源 → 共享 store/session/TaskManager **不**在回收权限内。发布与顶层 ACK **不**关闭旧资源。

**资源/取消/变异/错误**：
- 资源获取即登记；任何可失败接线前完成登记；失败按构造逆序释放、每件一次。
- 取消：请求取消只终止**本次调用对借出绑定的使用**，MUST NOT 等同于**结束资源借用/释放**（资源借用与释放归 01.4/01.5 与 `Leases` 逆序释放管辖），不连坐同 session 其它 invocation（02 落地），不销毁共享资源。
- 变异：`config` 的输入与返回快照隔离 slice backing array / map / 嵌套 schema / 可变值指针；运行资源对象经内部受限句柄复用，不盲目深拷贝带锁对象。
- 错误：解析失败具名（未知字段/未知引用/环）；结构前置拒绝时不改生效绑定；错误原因链保留（源文件 → 归一行 → 具名原因）。

## Decisions

- **D01-D1 声明与活对象分家**：`config` 只产纯声明；指纹只含声明面。备选（否）：把活句柄纳入指纹——会导致同内容重载误换代、在途请求被拼接。理由：`workflow-config-compilation`「发布指纹与版本代」。
- **D01-D2 state 持生效绑定、执行器借用同代**：`AgentBinding` 归 state，请求按代借出。备选（否）：执行器直取私有 `Config`——即历史缺陷（`state/compile.go` 仅声明、执行侧直取）。理由：计划 §4.2「旧请求继续持有旧代」。
- **D01-D3 纯声明解析用 yaml/json、目标形态显式 gate**：只接受 yaml/json 声明；是否支持 json 与 yaml 混用、注释/锚点语义**DESIGN-GATE(G0)**。备选（否）：支持图 DSL——违反「单一编排表示」。
- **D01-D4 显式 `(agent, session)` 寻址、不猜默认**：session 由应用选择。备选（否）：按「最近会话」推断目标——计划 §4.1 明令禁止。
- **D01-D5 mailbox 分点记账**：接纳/认领/消费完成分别记录。备选（否）：单一游标——无法区分「已接纳未认领」「已认领未完成」。理由：计划 §3 责任表（state 持消费位置）。
- **D01-D6 文件直接热读 vs 结构前置门两条路径**：prompt/skill 在请求边界读取固化，不等 register/评估/前置门；**直接热读 MUST NOT 被「未 Reload」禁止**：文件内容变化在下一请求边界即生效（固化实际版本），不要求先 Reload/register；结构变化走前置检查。备选（否）：文件也过前置门——与用户裁定冲突。
- **D01-D7 同名重建/退役先拒新、未交付义务保留**：删除 owner/session 先拒新接纳再收尾；同名再建不接收旧实例晚到结果；实例所有者区分**源实例/目标实例**；退役时**已接纳未交付**义务保留并由可恢复路径承接（不以 `removed` 丢弃）。理由：计划 §4.1 关闭合同。

## 消费者与反例（先枚举消费者再定验收）

| 产出物 | 真实消费者 | 反例（必须被拒绝/证明） |
|---|---|---|
| `config.Load` 纯声明解析 | 组合根装配、01.5 发布、迁移 | 含活对象字段的声明被拒；非 yaml/json 源被拒 |
| `OrgFingerprint`/`OrgPlan` | 01.5 发布、07.4 结构评估 | 只改 prompt 正文（FILE 面）不应换结构代；改工具参数才换代 |
| `StatePlan`/`AgentBinding`（同代借用） | 02.5 驱动、05.1 总图 | 在途请求中途拼接新代绑定（须拒绝/不可达） |
| `Generation`/基线复核 | 05.1 观测、回滚 | 过期候选覆盖新发布（须丢弃） |
| mailbox 记账 | 02.5、05.1、恢复 | 慢模型阻塞接纳/取消/结果记账（须不阻塞） |
| prompt/skill 固化版本 | 02.5、06.2 采集 | 未 Reload 仅改 registry 内容就改变请求所用 prompt（须不生效） |

## 覆盖映射（证据映射：每个 task → 场景落点或继承源块）

| task | Requirement（能力） | delta 场景落点 | 继承源块（不改，证据映射） |
|---|---|---|---|
| 01.1 | `workflow-config-compilation` ADDED「配置解析为纯组织声明」+ MODIFIED「发布指纹与版本代」 | 严格解析/默认值/纯声明/规范指纹 | — |
| 01.2 | `config-state-twinship` ADDED「session 身份与授权」 | 可信主体/显式寻址/不猜默认来源/源目标实例身份/退役未交付义务保留 | 主 spec `context-scoping`（resume 还原，本域不改，消费方在 03/04） |
| 01.3 | `config-state-twinship` ADDED「session mailbox 记账与执行占用」 | 分点记账/同 session 占用/接纳不阻塞 | 主 spec `persistent-event-loop`「批量 drain mailbox」（02 落地，本域只提供记账面） |
| 01.4 | `config-state-twinship` ADDED「候选活绑定准备与同代借用」+ MODIFIED「生效执行绑定由 state 持有」 | 候选私有构造/失败释放/同代借出 | — |
| 01.5 | `workflow-config-compilation` MODIFIED「唯一原子发布入口」+ `config-hot-reload` MODIFIED「首代可回滚」+ `per-call-subagent-overrides` MODIFIED | 基线复核/同内容不换代/在途旧代/覆盖钉定 | 主 spec `generation-bound-model-references`（模型引用绑定代，只读） |
| 01.6 | `config-hot-reload` MODIFIED「配置热更维度矩阵」+ ADDED「结构变更与文件直接生效分路及错误原因链」 | FILE 直接生效/两路径不互绕/错误原因链 | 主 spec `prompt-loader-fallback`（prompt 回退，只读） |

> 说明：`context-scoping`、`generation-bound-model-references`、`prompt-loader-fallback`、`persistent-event-loop` 属**跨域只读消费或他域落地**，本域不出 delta、不声称其已认证（不声称 601 条全绿）。

## 文件白名单（本域本轮可写，其余不动）

- `openspec/changes/runtime-rebuild/changes/01-state-binding/**`（**仅 md 规划，无 Go/脚本**）。
- 实施期（本轮**不**写，候选）：`config/*.go`、`state/*.go`（含 state 同包 resources 消费点，如 `state/resources.go`）、`prepare.go`（module 根组合入口）、`tests/integration/config/**`、`tests/integration/state/**`。（历史残件旧路径如 `state/reload/publish.go`、`state/resources/*` 保持**历史事实**，不作为候选写域。）

## 验证表（可复跑命令；本轮不跑）

| task | 命令 |
|---|---|
| 01.1 | `go test ./tests/integration/config -run '^TestConfigurationContract$' -count=10 -json` |
| 01.2 | `go test -race ./tests/integration/state -run '^TestSessionAuthority$' -count=10 -json` |
| 01.3 | `go test -race ./tests/integration/state -run '^TestMailboxConsumption$' -count=10 -json` |
| 01.4 | `go test -race ./tests/integration/state -run '^TestStateOwnsPipeline$' -count=10 -json` |
| 01.5 | `go test -race ./tests/integration/state -run '^TestGenerationPublication$' -count=10 -json` |
| 01.6 | `go test -race ./tests/integration/state -run '^TestFileAndStructuralChange$' -count=10 -json` |

> 口径：点名用例真实 pass 才算过；选择器零匹配/子项 Skip/`-run` 未命中不得因退出码 0 记通过（计划 §7 尾句）。01.1 的 config 契约测试置于 `tests/integration/config`；01.2–01.6 置于 `tests/integration/state`。

## Risks / Trade-offs

- [候选深拷贝泄漏变异] → 访问器隔离 + 「修改读取快照不影响已发布绑定」用例（`workflow-config-compilation` 场景）。
- [提交非原子部分生效] → 单一提交边界 + 「候选后半段失败不部分生效」用例。
- [在途请求持代未真实生效] → 复用旧缺陷教训：`Prepare` 只声明而不构造有效绑定即视为未通过；`TestStateOwnsPipeline` 必须驱动真实执行侧借用（不得只测 getter）。
- [文件通道被误当作经过前置门] → 两路径分别登记消费点并各有契约测。
- [同 session 双 invocation 连坐] → mailbox 记账 + 取消只及本 invocation（跨域验证归 02.5/02.7）。

## Migration

- 旧存档/旧声明**只读**：显式版本检查 + 离线迁移 + 具名错误；MUST NOT 在原路径自动改写（09.2 落地迁移驱动，本域只定义解析面契约）。
- 退役的旧接线约束（如以 register 充当文件生效开关、旧 Runner 接线、`Declarative` 旧承载）**不新增也不恢复**；若某旧字段确认退役，在该能力 delta 中以 `## REMOVED Requirements`（含 Reason/Migration）登记，而非静默丢弃。**01 当前无 REMOVED 项**（旧承载已在主 spec 层处理；本域以 MODIFIED 收窄义务）。

## Open Questions / 设计待核（G0–G3）

- **G0**：`config` 接受的确切源格式（yaml/json/两者）、未知字段策略（warn/strict）、默认值应用点、指纹归一化算法。→ 01.1 前冻结。
- **G1**：插件相位枚举与回调类型（与 02.6 一致）；`Commit` 端口签名；`acceptSeq` 分配点；`Leases` 与资源租约接口（与 02.9/09 一致）。→ 01.4/01.3 前冻结。
- **G2**：四条纵向序列（正常发布 / 候选失败 / 并发发布+在途 / 重启恢复）桌面推演；不得存在「之后再补」才能成立的接口。
- **G3**：逐 task 文件白名单 + 前置 + 读写冲突（本域与 02 共享 `state`/`config` 的串行化）+ 资源预算（无外部资源）。
- **跨域签名待核**：`OrgPlan`/`StatePlan`/`LeaseRef` 三对象形态、`generation` 归属、插件绑定归 state 的调用链——需 02.1/02.5/02.6 对齐后冻结；未对齐前相关 task 保持 design-blocked，不填空 struct 冒充冻结。

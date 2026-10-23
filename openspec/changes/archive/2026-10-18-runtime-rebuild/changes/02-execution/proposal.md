# 02 execution — 二级子变更提案

> 一级变更：`runtime-rebuild`（schema `spec-driven`）。本文件是二级域 `02-execution` 的提案。
> 二级嵌套不被 CLI 跟踪是**有意设计**；进度由一级 `tasks.md` 的域 checkbox 人工记账。
> 领域 = 责任与验收单位（计划 §6），不是代码包数量，也不是整域派发单位；派发原子下沉到就绪孙任务（计划 §8）。
> 本轮只产出 OpenSpec 规划产物；**不写产品代码/测试、不改残件、不跑产品 build/test、不安装/提交/归档**。

## Why

执行域承载最易失控的接线：若任务受理被写成「通知端口的后置装饰」、若终局与待通知分两次提交、若模型有多套终局归约、若关闭把「队列取空」当作完成，失败都会以「看起来完成、实际未收束」的形式隐藏。历史失效模式（第二套 ReAct 循环、mock 冒充真实接线、把 kill/超限报成成功）都出在这里。本域把「事件身份 → 任务受理与提交 → 模型终局 → 插件相位 → 委派/Handoff → 关闭」定成一条可复跑、可证伪的链。

## What Changes

- 事件身份与**原生载荷**保真：native role、multimodal ContentParts、原生 CallID 保真；去重作用域含所属实例、调用与决策（CallID 非全局唯一）。
- 任务 Registry 作为 state 转移的**提交载体**（在状态转移前接入），接收一次受理即返回**受理回执**（admission Receipt，不等于结果）；后台结果作为独立 observation 回流，不冒充同 CallID 的第二次同步 tool response。
- 一步提交 `terminal + outbox` 同提交；**存储提交三结果**（`committed` / `not-committed` / `uncertain`）分别表达，不一概宣称「双方未生效」；**受理回执**与**投递接纳结果**为另两层，分层表述、MUST NOT 合并为单 enum；幂等提交返回已存在事实不等于已交付。
- 模型面**单一** channel/iterator 终局归约：有效终局、usage 尾包、partial-only、错误、取消、ProducerDone 分别取证；不引 LLMAgent 第二工具循环。
- 消息按原生角色保真（assistant 含 tool_calls 与 content 并存）；同 session 不同 invocation **不合批**。
- 插件按相位分工：执行前策略恰一次、变换后最终请求经核心复核身份与权限、`Before` 恰一次、`After` 仅呈现 canonical commit 之后的事实；拒绝不执行但产生具名结果；审批 **Hold**（等待状态）与 **Denied**（终局）不混。
- 关闭真实义务收束：先拒新、结算已接受义务；非合作 producer 返回的 `false` 不能伪结束；截止返回未收束原因。
- Handoff：未来通知改投新目标与实例，已开始的通知保留旧目标与身份；通知按任务/轮次/类别/稳定 seq 身份，重复运输不生成第二份事实。

## Capabilities

### New Capabilities

本域不新增产品能力。新增义务以既有能力的 **ADDED Requirement** 登记：`async-task-execution` 增「审批 Hold 与 Denied 分离」（并 MODIFIED「工具调用经 spawn + dense 窗口执行」等五条）；`persistent-event-loop` 增「Handoff 未来通知的目标、实例与稳定序号」；`failure-polarity` 增「模型与 RL 单终局规则、上游 callbacks 优先」；`event-metadata-contract` 增「事件身份与原生载荷保真」；`request-budget-accounting` 增「usage 尾包归约与单终局消费」。

### Modified Capabilities

- `persistent-event-loop`：修改「模型入口错误以失败极性呈现」「业务 turn 开始执行时绑定编排版本」「循环停止为终结态」。
- `unified-task-semantics`：修改「任务统一语义」「演进命名与治理插件分工」。
- `async-task-execution`：修改「task_settled 为通知类 input 事件」「服务型任务转 alive-detached」（服务运行/就绪/终局按维度保留，就绪非 completed、不按时间推成功；**不设 REMOVED**）。
- `failure-polarity`：以 ADDED 增单终局规则（原 Requirement 保留继承）。
- `subagent-turn-execution`：修改「子 agent 调用可作为异步任务执行」（受理一次/后台独立回流/义务结清才 settle）及「Sub-agent 调用执行恰好一个 turn」「Sub-agent 保持并发隔离与调用契约不变」「投递对账屏障的 booking 与投递期望一一配对」（**统一事件调度、同 invocation 可含多 decision**；turn 边界 ≠ 义务完成；**受理 ≠ 运行即成功**；不建私有可写真源；**成功受理保持义务至结清，仅未新增义务的 dedup/被拒撤本次预登记**；不冻结旧类名/方法）。
- `event-metadata-contract`：以 ADDED 增原生载荷保真。
- `event-type-unification`：修改「事件类型常量单一来源」（native role 分类）。
- `thinking-plan-event-classification`：修改「thinking_plan event type classification」（native role 保真）。
- `request-budget-accounting`：以 ADDED 增 usage 尾包与单终局消费。

## Impact

- 实施候选写面（本轮不写）：`agent/*.go`、`task/*.go`、`plugin/*.go`、`event/*.go`（事件常量）、`tagent/*.go`（根装配入口）、`tests/integration/execution/**`。
- 只读消费：state 借出的同代绑定（01.4）、`config` 声明（01.1）、memory 事件存储面（03）。
- 对外符号 `package tagent` 位于 **module 根**，不 nested。
- 组合根、主 spec、旧 change、外部计划、依赖、`scripts/` 均不在本轮写域。

## 边界与依赖

### 依赖（接口前置 vs 实现前置）

- **接口前置**（签名冻结即可开工）：00.3 G1 共享合同（身份/插件相位/任务提交/通知/关闭/Query 签名与版本解释）；01.4 的 `StatePlan`/`AgentBinding` **签名**（02.5 借绑定用，不等待其实现）。
- **实现前置**：02.2 ← 02.1；02.3 ← 02.2；02.4 ← 00.3；02.5 ← 01.3/01.4（实现）、02.3/02.4；02.6 ← 02.5；02.7 ← 02.6；02.8 ← 02.7；02.9 ← 02.8。
- 02.1 仅依赖 00.3。

### 被依赖

- 01.2/01.3/01.4 依赖 02.1 的**签名**（事件身份、提交端口）；03.3 依赖 02.2 的提交语义；04.x/05.x 依赖 02.6 的插件相位与 02.2 的任务账目。

### 接口面（本域最小冻结面，只登记不实现）

- 事件：`event.Type*` 常量（单一来源）、`trigger_source`/`meta_*` 元数据常量、原生 `model.Message`（Role/ContentParts/ToolCalls/CallID）。
- task：`Commit(ctx, Record) (CommitResult, error)`（提交载体）、`Record{ Terminal, Outbox, ExpectedRevision/MutationID }`、**分层结果**：受理回执 `AdmissionReceipt`（独立）、存储提交 `CommitResult{Committed|NotCommitted|Uncertain}`、投递接纳 `DeliveryAcceptance`（独立）；不合并为单 enum。幂等键 = `TaskID`+`ExpectedRevision`/`MutationID`（DESIGN-GATE(G1) 前不预写为全强制导出）。
- 插件相位枚举：请求丰富 / 风险策略 / 视图选择 / 外置策略 / 提交后观测 / 最终呈现。

### 禁止事项

- 不引 LLMAgent 的第二工具迭代循环；不建第二套终局归约。
- 不把任务受理写成 NoticePort 的后置装饰；不让后台结果冒充同 CallID 的第二次同步 tool response。
- 不把 `Hold`（等待）伪装成 `Denied`（终局）；不把 `cancel`/队列取空当作关闭完成。
- 不为形式复用另造事实总线；不镜像 provider/tool 协议；不为保留旧接口发明消费者。
- 不提交/推送/归档；不跑产品 build/test；不触碰 Git index。

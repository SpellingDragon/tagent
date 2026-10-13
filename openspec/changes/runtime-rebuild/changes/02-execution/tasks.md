# 02 execution — 任务清单（三级孙任务）

> 本文件是二级子变更 `02-execution` 的任务清单，层级为三级「孙任务」。全部未勾选（`- [ ]`），本轮**不执行**。
> 编号沿用权威计划 §7 的 `02.1–02.9`，不修补旧编号、不继承旧完成率。每条编号**只出现一次**。
> 每条格式：`- [ ] <编号> <动词开头产出> + 关键约束 —— 前置 + 候选拟写文件（非本轮授权） + 设计闸(G0–G3) + 验证`。
> 验证口径：点名用例真实 pass 才算过；选择器零匹配/Skip/未命中不得因退出码 0 记通过（计划 §7）。并发/状态任务加 `-race`。
> 「前置」含设计/实现依赖，不表示当前授权执行；「候选拟写文件」是未来候选写域，不表示已存在或批准修改。

## 前置与边界

- 实现前置：00.3（G1 共享合同冻结）。
- 接口前置：01.4 的 `StatePlan`/`AgentBinding` **签名**（02.5 借绑定用）；00.3 的插件相位/提交/通知签名。
- 本域与 01 共享 `tagent` 写面、与 03 共享 `event`/`memory` 边界 → G3 登记读写冲突、串行或同一代理。
- 02.1 是本域**接口与实现提供者**：签名由 00.3 冻结，实现（身份与内存提交载体）随 02.1 落地；01.2/01.3/01.4 的**实施及验收**依赖其**真实集成**，不能只凭签名解除实现前置。

## 任务

- [ ] 02.1 实现事件身份与原生载荷、task 提交/消费端口（先内存载体，明确持久适配合同）—— 前置：00.3 —— 候选拟写文件（非本轮授权）：`event/types.go`（`event.Type*` 单一来源常量）、`event/meta.go`（`MetaKey*` 常量与 ParseEventMeta）、`task/commit.go`（`Commit` 端口 + `Record{Terminal,Outbox,ExpectedRevision/MutationID}` + **分层结果**（受理回执 `AdmissionReceipt`／存储提交 `CommitResult{Committed|NotCommitted|Uncertain}`／投递接纳 `DeliveryAcceptance`，不合并单 enum）的内存载体）、`tests/integration/execution/identity_commit_contract_test.go`（`TestIdentityAndCommitContract`）—— 设计闸：G0 = 常量集与原生载荷承载（不建影子消息）；G1 = `Commit` 签名/幂等键 = `TaskID`+`ExpectedRevision`/`MutationID`（非 `(TaskID, round)`；三层结果不合并单 enum）；去重作用域含实例/调用/决策（DESIGN-GATE：未冻结则 design-blocked）；G2 = 序列「原生 role/多模态/CallID 保真 / 同 CallID 不同实例不误去重 / 存储提交三态分别可辨 / 受理回执独立于提交与投递接纳 / 幂等提交≠已交付」；G3 = 白名单 + 与 01 的 `tagent` 写面串行 —— 验证：`go test ./tests/integration/execution -run '^TestIdentityAndCommitContract$' -count=10 -json`

- [ ] 02.2 实现 task 受理、限额、取消、幂等与可解释义务明细 —— 前置：02.1 —— 候选拟写文件（非本轮授权）：`task/registry.go`（Registry 接提交载体、义务明细）、`task/cancel.go`（取消与幂等）、`tests/integration/execution/task_lifecycle_test.go`（`TestTaskLifecycle`）—— 设计闸：G1 = Registry 在**状态转移前**接载体（非 NoticePort 后装饰）；义务明细字段（数量/归属/阻塞/退出原因）；G2 = 序列「受理成功/失败 / 超限拒绝 / 取消只及本任务 / 重复受理幂等」；G3 = 白名单 + `task` 包读写 —— 验证：`go test -race ./tests/integration/execution -run '^TestTaskLifecycle$' -count=10 -json`

- [ ] 02.3 实现注册期原生 wrapper、审批等待/拒绝、一次受理回执（admission Receipt）与后台结果独立回流 —— 前置：02.2 —— 候选拟写文件（非本轮授权）：`task/wrapper.go`（原生化注册 wrapper，一次受理）、`task/hold.go`（Hold 等待 vs Denied 终局）、`tests/integration/execution/native_tool_admission_test.go`（`TestNativeToolAdmission`）—— 设计闸：G1 = 一次受理回执协议 + 后台结果作为独立 observation（不冒充同 CallID 第二次同步 response）；Hold/Denied 分离（DESIGN-GATE：与 04.4/04.5 对齐）；G2 = 序列「快任务在窗口内 settle 仍走一次受理 / 审批Hold登记未执行任务 / 拒绝零执行产生具名配对结果 / 后台结果独立回流」；G3 = 白名单 + 与 03 的 `event` 边界 —— 验证：`go test -race ./tests/integration/execution -run '^TestNativeToolAdmission$' -count=10 -json`

- [ ] 02.4 实现模型单一 channel/iterator 终局处理（usage 尾包、错误/取消、生产者结束）—— 前置：00.3 —— 候选拟写文件（非本轮授权）：`model/terminal.go`（单一终局归约，终局归 model 包，不另造 agent 副本）、`model/usage.go`（usage 尾包）、`tests/integration/execution/model_terminal_protocol_test.go`（`TestModelTerminalProtocol`）—— 设计闸：G1 = 单一归约入口与 `kind` 枚举（unknown 告警不默认 completed）；不引 LLMAgent 第二循环（DESIGN-GATE：上游 callbacks 优先规则）；G2 = 序列「有效终局 / usage 尾包 / partial-only / 错误 / 取消 / ProducerDone 分别取证 / nil 流归 failed」；G3 = 白名单 + `model` 包 —— 验证：`go test ./tests/integration/execution -run '^TestModelTerminalProtocol$' -count=10 -json`

- [ ] 02.5 实现经根包最小公共装配入口驱动 Agent、从 state 借同代绑定并按 invocation 处理事件 —— 前置：01.3/01.4（实现）、02.3/02.4 —— 候选拟写文件（非本轮授权）：`run.go`（module 根 `package tagent` 最小公共装配入口）、`agent/loop.go`（按 invocation 调度、同代借用）、`tests/integration/execution/native_conversation_roundtrip_test.go`（`TestNativeConversationRoundTrip`）—— 设计闸：G1 = 借 `StatePlan` 同代绑定、保留原生消息与多工具配对；同 session 不同 invocation 不合批（DESIGN-GATE：与 01.4 对齐）；G2 = 序列「原生多轮工具调用往返 / 同 session 双 invocation 各自决策不连坐 / task_settled 回流选当前代」；G3 = 白名单 + 与 01 的 `tagent`/`state` 写面串行（不另造测试装配器）—— 验证：`go test -race ./tests/integration/execution -run '^TestNativeConversationRoundTrip$' -count=10 -json`

- [ ] 02.6 实现插件执行前恰一次、最终参数执行、denied-only 可收束、提交后呈现不丢事实 —— 前置：02.5 —— 候选拟写文件（非本轮授权）：`plugin/phases.go`（相位分工与 Before 恰一次）、`plugin/present.go`（提交后呈现）、`tests/integration/execution/plugin_phase_ordering_test.go`（`TestPluginPhaseOrdering`）—— 设计闸：G1 = 相位枚举与 01.4 `AgentBinding.Plugins` 一致；变换后最终请求经核心复核身份/权限、同时用于执行与记录；After 仅在 canonical commit 之后（DESIGN-GATE：与 04.5 对齐）；G2 = 序列「Before 恰一次 / denied 零执行且只产生具名结果 / After 不先于 commit / 观察者失败记缺口不回滚事实」；G3 = 白名单 + `plugin` 包 —— 验证：`go test -race ./tests/integration/execution -run '^TestPluginPhaseOrdering$' -count=10 -json`

- [ ] 02.7 实现子委派的调用关联、进度与终局（复用应用选择的目标 session）—— 前置：02.6 —— 候选拟写文件（非本轮授权）：`agent/delegate.go`（委派调用关联/进度/终局）、`tests/integration/execution/delegation_completion_test.go`（`TestDelegationCompletion`）—— 设计闸：G1 = 委派绑定发起代目标（不改读全局最新表）；子调用环投递对账屏障一一配对；G2 = 序列「A→B→C 同代不同 owner 视图 / 受理回执·dedup 命中·gate 拒绝 三形态 booking 撤销 / 多级任务重入定位所属 owner」；G3 = 白名单 + `agent` 包 —— 验证：`go test -race ./tests/integration/execution -run '^TestDelegationCompletion$' -count=10 -json`

- [ ] 02.8 实现 Handoff 目标实例与通知重试（已开始者固定旧目标【源实例】，未来通知转新目标【目标实例】；实例关闭未交付保留）—— 前置：02.7 —— 候选拟写文件（非本轮授权）：`task/handoff.go`（目标 session/instance 与转交关联、源/目标实例身份）、`task/notice.go`（稳定 seq）、`tests/integration/execution/handoff_future_notifications_test.go`（`TestHandoffFutureNotifications`）—— 设计闸：G1 = 通知身份 `(TaskID, ExecutionRound, Category, Seq)`；实例所有者区分源/目标实例；关闭未交付由可恢复路径承接（不以 removed 丢弃）；重复运输不生成第二份事实（DESIGN-GATE：与 04.4 对齐）；G2 = 序列「已开始通知保留旧目标不改投 / 未来通知转新目标/实例 / 源目标实例身份区分 / 关闭未交付保留 / 重试不改投 / 多 progress 稳定 seq 有序」；G3 = 白名单 + `task` 包 —— 验证：`go test -race ./tests/integration/execution -run '^TestHandoffFutureNotifications$' -count=10 -json`

- [ ] 02.9 实现关闭/排空/真实停止确认（未收束义务与非合作 producer 不得伪称结束）—— 前置：02.8 —— 候选拟写文件（非本轮授权）：`agent/shutdown.go`（先拒新、义务结算、真实停止确认）、`tests/integration/execution/close_drains_obligations_test.go`（`TestCloseDrainsObligations`）—— 设计闸：G1 = 关闭以真实义务收束为准（非 cancel/队列取空）；非合作 producer `false` 不伪结束；截止返回未收束原因（DESIGN-GATE：与 08 宿主关闭/09 soak 对齐）；G2 = 序列「先拒新 / 已接受义务结算 / 非合作 producer 不伪结束 / 截止报告未收束原因 / 重复 Close 幂等」；G3 = 白名单 + `agent` 包 —— 验证：`go test -race ./tests/integration/execution -run '^TestCloseDrainsObligations$' -count=10 -json`

## 执行期回写（本轮为空）

（执行期新增问题按「场景 → 根源 → 影响面」追加；契约变化先回写 `design.md`，不静默塞进补丁。）

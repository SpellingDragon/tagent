# 02-failure-delivery：真失败必须把原始原因交给用户

## Why

症状 3 是三条症状里最伤人的一条：**模型真的失败了，用户什么都没收到。**

真机证据（2026-10-25 20:15:55）：provider 返回真失败（观测面 `terminal=failed`、0 tokens），该回合以失败终端收尾，既没有可交付的 assistant 正文，也没有任何失败告知——微信侧零消息，用户以为机器人没听见。示例层另有一条固定降级文案（`执行出错：本轮模型回合失败，未产出交付内容。`），它不含任何原始原因，用户无法判断是网络、额度还是服务端故障，运维也无法从用户回执反推。

一级 `proposal.md` 的用户裁定是"**原始信息**"，一级 D6 已否"失败告知用柔化系统话术"。本域把这条裁定落成机制：失败且无可交付正文 → 由宿主直接把 provider 的原始错误投给用户，同时留一条可审计的事实与一行带原因的日志。

## What Changes

- **失败分支直投原始错误**：`host.go` 的 `deliverFresh`（回合结束的框架直投通道）在"本次回合终态为 `failed` 且没有任何可交付 assistant 正文"时，改为经同一个 `MessageSender` 直投**原始错误文本**，而不是只写一行 `assemblyDebug` 了事。投递失败仅记 `assemblyWarn`，不改变回合已结束的事实。
- **原始原因必须真实贯通到宿主**：`runtime/agent` 的 `Turn` 增加只读字段 `Failure string`，由 `Drive` 在每一轮从 `Outcome.Cause()` 取值（失败终态时填、其它终态时清空）。**判据只认 `Terminal == model.KindFailed`**：`cancelled`（调用方取消）与"成功但无正文"都 MUST NOT 触发失败告知。
- **失败留痕事实**：每次真的投出失败告知，宿主提交一条**无模型载荷**的事实（`TypeFeedback`，summary 前缀 `failure-notice`，pass-through 元数据带原始原因与 `terminal=failed`，带失败的 invocation 身份）。无载荷是硬要求：它 MUST NOT 进入任何模型请求，也 MUST NOT 在回放投影里变成一条消息（`mappedEntryOf` 对无载荷事实不投影）。事实提交失败 MUST NOT 阻断告知，但 MUST 记警告。
- **日志带原因**：`api/model` 的模型观测行在失败终态且有非空原因时，于同一行追加 `error=<原因>`；成功终态 MUST NOT 出现该字段，失败但原因为空（如 nil stream 告警）MUST NOT 渲染 `error=""`。取数统一走新增的 `Outcome.Cause()`，`rl.AttemptError` 委托同一实现（行为不变）。
- **示例层收敛（越界申报 P-02）**：`examples/wechat-bot/wechat.go` 的 `hostRuntime.Drive` 删除固定降级文案（改为一行 debug 日志），否则框架直投后用户会收到**两条**失败话术，"恰一条"不成立。连带修订 `smoke_errors_test.go` 与 `smoke_harness_test.go` 里等待/断言该文案的两处。
- **连带测试面**：`tests/integration/host/event_integrity_test.go` 等既有失败路径断言若按"零消息"或"罐头文案"取数，必须同轮改为按"原始原因"取数（见 design D8 写集）。

## Capabilities

### New Capabilities

- `failure-notice`：真失败且无可交付正文时的用户告知契约——谁投、投什么、留什么事实、日志带什么、以及"成功/取消/有正文时一律零失败话术"的反向边界。

### Modified Capabilities

无。`persistent-event-loop` 的失败路径场景与 `event-integrity` 的 golden 场景核对归 04 域；本域只提供可验收的行为契约与证据。

## 边界与依赖

- **依赖（接口常数优先，实现依赖次之）**：
  - **接口常数**：`model.Outcome{Kind, Usage, Body, Trace, Late}` 与 `model.Item.Err` 的结构不变（本域只新增只读访问器 `Outcome.Cause()`）；`MessageSender.Send(ctx, target, content) error` 签名不变；`Host.Invoke(ctx, req) (agent.Turn, error)` 签名不变；`event.Event` 结构与既有 `MetaKey*` 常数不变（失败事实只用既有 type + 既有 key + `MetaPrefix` 业务键）。
  - **实现依赖（可改）**：`Host.deliverFresh`（新增失败分支与 turn 参与）、`Host.driveLocked`（把 turn 交给直投通道）、`agent.Turn`（新增 `Failure` 字段）、`Loop.Drive`（每轮填写/清空 `Turn.Failure`）、`model.callObservation.end`（失败行追加原因）、`rl.AttemptError`（委托 `Outcome.Cause()`）。
  - **契约依赖**：`deliverFresh` 仍是"回合结束后唯一的框架直投通道"（`freshAssistantOf` 的 baseline 语义不变）；`mappedEntryOf` 的"无载荷事实不投影"仍是回放不污染的既有保证；`event-consumption-integrity` 已归档的"终态通知驱动跟进轮"不变。
- **被依赖方**：00.3（症状 3 转绿靠本域 2.1+2.3）、F3（回滚即红）、04 域（失败路径主规格与 golden 场景核对）。
- **接口面（本域读写的既有面）**：`Host.Invoke` / `Host.driveLocked` / `Host.deliverFresh` / `freshAssistantOf` / `hostDeliveryMeter` / `MessageSender`；`agent.Turn` / `Loop.Drive` / `model.Outcome` / `model.ReduceChannel` / `model.callObservation` / `rl.AttemptError`；`event.Event` / `Run.CommitToSession` / `Run.SessionFacts`。**唯一新增公开面**是只读访问器 `func (o Outcome) Cause() error` 与 `Turn` 的只读字段 `Failure string`；不改任何既有函数签名、不新增事件类型、不新增持久字段。
- **禁止事项**：① 不改 `cancelled` 的静默（调用方取消不是失败）；② 不在有可交付正文时追加错误话术（正文优先）；③ 不把失败写进 assistant 正文或任何有模型载荷的事实（不得冒充模型输出）；④ 不复用 `MetaKeyNoticeKind` 等通知元数据（避免既有通知消费者把失败事实当通知再驱动一轮）；⑤ 不改 `[send_denied]`、终态通知、受理回执（01 域）；⑥ 不改 golden 与主规格（04 域）；⑦ 不以示例层再写一份兜底文案（P-02）；⑧ 不做原因码映射或错误分类（一级 D6 已否）；⑨ 不以测试内直接调 `deliverFresh` 冒充消费者。

## Impact

- 生产：`host.go`（`deliverFresh`、`driveLocked`、失败事实 helper）、`runtime/agent/loop.go`（`Turn.Failure`、`Drive` 每轮取因）、`api/model/terminal.go`（`Outcome.Cause()`）、`api/model/observe.go`（失败行原因）、`rl/produce.go`（`AttemptError` 委托）、`examples/wechat-bot/wechat.go`（删罐头文案）。
- 测试（连带，必须同轮）：`api/model/observe_test.go`（`TestModelCallObservation` 补失败原因/成功无原因/空原因三子测试）、`rl` 侧 `AttemptError` 既有断言、`examples/wechat-bot/smoke_errors_test.go`（`smokeErrorsModelTurn` 的文案与计数）、`examples/wechat-bot/smoke_harness_test.go`（`smokeLongFaultModel` 等待 `执行出错` 的一处）。
- 新增测试：`tests/integration/host/failure_notice_test.go`（2.1/2.3）、`api/model/terminal_test.go` 的 `Cause` 子测试（2.1）。
- **上报项 P-02（越界申报，需编排者确认）**：本域写集含 `examples/wechat-bot/wechat.go` 与 `smoke_errors_test.go`、`smoke_harness_test.go`——派发单只写"deliverFresh/host.go"。不收敛示例层文案，G3 的"恰一条"必然两条；若被否，`failure-notice` 的「恰一条」条款与 00.3 一并标为不可达。
- **上报项 P-03（代码事实与派发单不符，需编排者知悉）**：派发单称"`Invoke` 返回 `(agent.Turn, error)`——error 携带模型失败原始信息"。实测 `Loop.Drive` 在模型真失败时返回 `Turn{Terminal: KindFailed}, nil`（**error 为 nil**），原始错误只存在于 `Outcome.Trace[i].Err`。因此 2.1 不能靠 `Invoke` 的 error 贯通，必须新增 `Turn.Failure` 字段并把 `runtime/agent/loop.go` 纳入写集；`Invoke` 的 error 仍是**宿主/协议错误**通道，两者 MUST NOT 混用（宿主错误不投"模型失败"话术，见负锚 `host_error_not_a_model_failure`）。
- 本轮仅落 OpenSpec 文档：不改产品代码、测试、CI、主规格，不提交。

## 规划入口

- 静态决策、机制细节、被否方案与写集：[design.md](design.md)
- 三叶账目与三锚：[tasks.md](tasks.md)
- 能力契约：[specs/failure-notice/spec.md](specs/failure-notice/spec.md)

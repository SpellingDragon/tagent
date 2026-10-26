# 01-silent-receipts：系统内部确认的静默语义

## Why

症状 1/2 的直接成因不在模型"多嘴"，而在**系统把内部确认当素材喂给它**：受理回执写着 task id 与"结果会以通知到达"的承诺，投递成功回执写着目标会话 id，progress 通知写着轮次与状态。模型看到这些"事件"，就会把它们复述成面向用户的话——这就是"后台任务摘要后又发多余确认"（2026-10-24 微信实录；2026-10-25 真机 20:12:26 仍复现）。

一级 D1 把事件流向模型的输入分成三族，本域负责第三族：**系统内部确认绝不进模型**。用户世界（用户消息）与业务需注意（终态、具名拒绝）不动。

## What Changes

- **受理回执最小化**：`runtime/agent/loop.go` 的 `admissionText` 由 `accepted <taskID> (running): … the call runs as a background task and its result will arrive as a notification when it settles` 改为最小确认 `ok`。tool 结果仍以 `tool` 角色配对本调用（模型能判定"调用已发生"），但不再携带 task 标识、状态词、结算承诺与轮询提示。
- **投递成功回执最小化**：`host.go` 的 `hostSendTool.Call` 成功路径由 `[send_ok] delivered to <sessionID>` 改为 `ok`（成功无需素材，目标会话 id 也不外泄给模型）。
- **拒绝保留具名原因**：`hostSendTool.denied` 的 `[send_denied] <reason>` 形态**不变**——拒绝是"业务需注意"，模型需要据此应对（改参数、换路径、告知用户）。本域只改成功路径。
- **progress 只落事实**：`Loop.admitNotice` 不再把 progress 通知停进 intake；事实仍由 `commitNotice` 提交（审计与投影用）。并且——**这是本域对派发单的一处扩面，见下**——progress 事实 MUST NOT 再作为输入材料进入任何模型请求：现状 `Loop.progressNotes` 经 `NoticeFeed.NoticesFrom` 把通知事实读回 transcript（`Drive` 每轮头部 `transcript = append(transcript, l.progressNotes(inv)...)`），**只去掉 `in.notify` 并不能让 progress 离开模型视野**。
- **终态通知不动**：`NoticeSettled/NoticeFailed/NoticeCancelled` 仍经 `commitSettled` + `intake.deliver` 投递并驱动 range 的下一决策；静默 MUST NOT 波及终态（否则会把"结算结果"一起静音）。
- **连带测试面**：`admissionText` 的新文本与 `send_ok` 的新文本会让四处既有断言与 smoke 脚本模型的"分支键"失效，必须同轮修订（见 Impact 与 tasks 写集）。

## Capabilities

### New Capabilities

- `quiet-receipts`：三类系统确认（受理、投递成功、progress）的静默语义——什么进模型、什么只落事实、什么必须保留具名拒绝；以及"静默不得波及终态通知"的反向边界。

### Modified Capabilities

无。`persistent-event-loop` 的三条新场景与 progress 呈现语义修订归 04 域（本域不预写主规格）；`event-integrity` 的 golden 场景核对同样归 04。本域只提供可验收的行为契约与证据。

## 边界与依赖

- **依赖（接口常数优先，实现依赖次之）**：
  - **接口常数**：`task.AdmissionReceipt{CallID, TaskID, Accepted, Kind}` 的结构不变；`task.Notice{TaskID, Round, Kind, Seq, Event}` 的结构不变；`model.Message{Role, ToolID, ToolName, Content}` 的配对语义不变；`[send_denied] ` 前缀是既有具名拒绝文本，本域不动。
  - **实现依赖（可改）**：`Loop.admissionText`（文本）、`Loop.admitNotice`（是否停 intake）、`Loop.progressNotes`/`Drive` 的读路径（是否把通知事实读进 transcript）、`hostSendTool.Call` 的成功返回值。
  - **契约依赖**：`event-consumption-integrity` 已归档的"通知按 Seq 有序、重复运输不生成第二份事实"仍成立——本域只改"是否进模型"，不改"事实是否落、是否有序、是否幂等"。
- **被依赖方**：00.1/00.2（症状锚，靠本域 1.1 转绿）、04 域（progress 呈现语义与既有 golden 场景修订，必须在 1.3 落地后逐条核对）、F3（回滚即红）。
- **接口面（本域读写的既有面）**：`Loop.Drive` / `Loop.Backflow` / `Loop.admitNotice` / `Loop.commitNotice` / `Loop.progressNotes` / `NoticeFeed.NoticesFrom` / `intake.notify` / `intake.takeNotices`；`hostSendTool.Call` / `hostSendTool.denied`。不改任何公开函数签名、不改事件结构、不新增事件类型。
- **禁止事项**：① 不改 `NoticeSettled/NoticeFailed/NoticeCancelled` 的投递路径与终态语义；② 不改 `[send_denied]` 的具名原因（拒绝必须可读）；③ 不删除 progress 事实（"静默"不等于"丢事实"）；④ 不改 golden 与主规格（04 域）；⑤ 不改 `examples/wechat-bot/wechat.go`（示例层文案归 02）；⑥ 不新增公共 API、持久格式或依赖；⑦ 不以"测试里手工接线"证明模型静默——必须读脚本 provider 的真实请求。

## Impact

- 生产：`runtime/agent/loop.go`（`admissionText`、`admitNotice`、`progressNotes`/`Drive` 读路径）、`host.go`（`hostSendTool.Call` 成功回执）。
- 测试（连带，必须同轮；全仓实测共 8 文件 11 处受理断言，逐处见 design D6 表）：`runtime/agent/loop_test.go`（`TestBackflowNotificationLanes` 的两个 progress 子测试）、`tests/integration/execution/{receipt_background_separation_test.go:122,194, native_conversation_roundtrip_test.go:122,127,330, plugin_phase_ordering_test.go:109, subagent_call_test.go:124}`、`tests/integration/capabilities/native_tools_test.go:414`、`tests/integration/host/{agent_reference_test.go:101, request_boundary_test.go:305}`、`examples/wechat-bot/smoke_data_test.go:175`（`accepted task-` 断言）、`examples/wechat-bot/smoke_harness_test.go`（脚本模型以 `accepted task`/`[send_ok]` 作为分支键与帧识别）；另 `tests/integration/host/event_integrity_test.go` 三处 `[send_ok]` 断言，其中两条是“不含”的负向断言，MUST 补“恰为 `ok`”的正向断言以免空转变绿。
- 新增测试：`tests/integration/execution/quiet_receipts_test.go`（1.1、1.3）、`tests/integration/host/quiet_receipts_test.go`（1.2）。
- **上报项 P-01（本域对派发单的扩面，需编排者确认）**：派发单原话只要求"1.3 progress 不 park（`admitNotice`）"。实测读路径：progress 事实经 `progressNotes → NoticeFeed.NoticesFrom` 进入每轮 transcript，因此**仅去 park 不会让 progress 离开模型材料**。本域把读路径纳入 1.3 的写集（仍限 `runtime/agent/loop.go` 内，不扩到别的文件）；若被否，则 `quiet-receipts` 的"progress 不进模型"条款无法成立，1.3 不能勾选。
- 本轮仅落 OpenSpec 文档：不改产品代码、测试、CI、主规格，不提交。

## 规划入口

- 静态决策、机制细节与被否方案：[design.md](design.md)
- 三叶账目与三锚：[tasks.md](tasks.md)
- 能力契约：[specs/quiet-receipts/spec.md](specs/quiet-receipts/spec.md)

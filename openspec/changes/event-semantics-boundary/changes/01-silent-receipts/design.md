# Design — 01-silent-receipts（静默回执）

## D0 结论

本域把"系统内部确认"从模型视野里摘出去，**只摘确认，不摘事实**：受理回执降到 `ok`、投递成功降到 `ok`、progress 只落事实。三条合起来才构成"模型说话的唯一合法素材=用户问题+业务结果"（一级 D1 规则④）。反向边界同样硬：终态通知与具名拒绝 MUST 原样保留——**静默不是把所有东西都静音**。

## D1 三族四规则在实现上的落点

| 族 | 事件 | 现状 | 本域后 |
|---|---|---|---|
| 用户世界 | 用户消息 | 进模型 | 不动 |
| 业务需注意 | `NoticeSettled/Failed/Cancelled`、`[send_denied] <reason>`、工具错误 | 进模型 | **不动（硬边界）** |
| 系统内部确认 | 受理回执、`[send_ok]`、progress | 进模型 | 受理→`ok`；成功→`ok`；progress→只落事实 |

规则①初始派发静默 → 1.1；规则②回执是凭据不是消息 → 1.2；规则③progress 只落 fact → 1.3；规则④是前三者的合并结论，不单独实现。

## D2 受理回执：恰为 `ok`

- `Loop.admissionText(receipt task.AdmissionReceipt) string` 的返回值改为常量 `ok`；`dispatch` 里 `paired(call.ID, name, admissionText(out))` 的配对结构**不变**（仍是 `tool` 角色 + 本调用 id），因此模型仍能判定"这次调用发生了、结果稍后到"。
- **不改** `task.AdmissionReceipt` 结构与 `dispatch` 的分支（`task.AdmissionFresh` / `task.HoldReceipt` / default 三条路径的语义不变）。`HoldReceipt` 的 `parked awaiting approval` 属于业务需注意（审批扣留），**保留**。
- **被否方案**：① 回执完全为空串——tool result 协议要求有内容，且空串让模型无法判断调用是否发生；② 保留内容加 `[receipt]` 前缀——仍是可复述素材，治标不治本（一级 D1 已否）；③ 只删"task id"保留状态词——`(running)` 与"notification when it settles"本身就是可复述素材。
- **连带**：四处既有断言（`accepted task-`/`(running)`）与 smoke 脚本模型的分支键会失效，必须同轮改为**按结构取键**（`role`/`tool_call_id`），不得按正文取键（见 D6）。

## D3 投递成功回执：`ok`；拒绝保留具名原因

- `hostSendTool.Call` 成功路径 `return "ok", nil`；失败与拒绝一律走 `denied(...)` → `[send_denied] <reason>`，**一字不改**。
- **被否方案**：① 成功回执保留目标会话 id（`delivered to <session>`）——目标 id 是运营身份，不是模型决策素材，且是最容易被复述成"已发到 xx"的碎片；② 拒绝也降为 `ok`——模型失去改参/换路径的判据，会把失败当成功汇报（比多说一句危险得多）。
- **不受影响**：`hostSendTool.Declaration` 的 `[send_denied]` 说明文字保留（声明面告诉模型拒绝形态，属业务需注意）。
- **`[send_ok]` 的三处断言必须重新指向**：`tests/integration/host/event_integrity_test.go:2162` 是正向断言（改为 `Content == "ok"`）；`:2066` 与 `:2337` 是“结果**不含** `[send_ok]`”的负向断言——改文案后它们会**空转式变绿**，因此 MUST 同时补“该结果恰为 `ok`”的正向断言，否则判据静默消失。

## D4 progress：只落事实，不进 intake，也不进模型材料

现状三条通道同时喂模型：`admitNotice` → `commitNotice`（事实）+ `in.notify`（intake 缓存）→ `Drive` 每轮 `progressNotes` 从 `NoticeFeed.NoticesFrom`（链投影）**和** `takeNotices`（缓存）读进 transcript。

本域后：

1. **事实保留**：`commitNotice` 照旧（`NoticeFactKey`、`TypeFeedback`、kind/task 元数据、Seq 有序、幂等）——审计、投影与"通知消费位置连续"仍成立。
2. **不再停 intake**：`admitNotice` 只提交事实，不 `in.notify`、不产生 `noticeEntry`。
3. **不再进模型材料**：`Drive` 不再把 progress 通知读进 transcript。`progressNotes` 的读路径从 `Drive` 摘除；`NoticeFeed`/`progressNotes` 的代码是否随之删除，归 04 域裁定（本域只摘调用点，不删接口，避免波及既有装配与测试夹具）。
4. **退化情形**：range 地址不完整（`addr.IsComplete()` 为假）时 `commitNotice` 不提交事实——此时 progress 既不落事实也不进模型，属**有意丢弃**（没有 range 就没有可审计的归属）。此路径 MUST NOT 影响终态通知（终态走 `commitSettled` 的地址非空分支 + `deliver`，与 progress 完全独立）。
5. **被否方案**：① 只去 `in.notify`——`progressNotes` 的链投影读路径仍把 progress 送进 transcript，"绝不进模型"不成立（本域最重要的实测发现，见上报项 P-01）；② progress 业务白名单（一级 D6 已否，防过度设计）；③ 删除 progress 事实——"静默"被偷换成"丢事实"，审计与 Seq 有序契约一起破。

**既有测试的修订方向（不得静默删除）**：`runtime/agent/loop_test.go` 的 `TestBackflowNotificationLanes` 两个 progress 子测试现在是"progress 进 range 的下一决策"，必须**反向改写**为"progress 事实已提交、intake 为空、下一请求不含 progress 文本"，而不是删掉了事——删掉等于把验收面从"进"改成"无"却不留证据。

## D5 终态通知不动（反向硬边界）

`Backflow` 的 `NoticeSettled/NoticeFailed/NoticeCancelled` 分支、`commitSettled`、`intake.deliver`、`await` 的配对语义全部不动。任何"顺手把 notice 机制整体收窄"的改动都会把结算结果一起静音，属越权。验收上由 `quiet-receipts` 的「静默不得波及终态」条款与 1.3 的消费者锚共同守住。

## D6 连带面：`ok` 会让"按正文取键"的脚本与断言失效

`admissionText` 与 `send_ok` 的文本是 **8 个测试文件、11 处**产品断言与 smoke 脚本模型的分支键（全仓实测，实施前按本表逐处核）：

| 位置 | 现状取键 | 本域后取键 |
|---|---|---|
| `tests/integration/execution/receipt_background_separation_test.go:122,194` | `Contains(receipt.Content, "accepted task-")` + `"(running)"`；194 行按该正文判“哪条是回执” | 断言 `receipt.Content == "ok"`，并补“不含 task id/状态词”的负向断言；194 行改按 `Role==tool` + `ToolID` 判定 |
| `tests/integration/execution/native_conversation_roundtrip_test.go:122,127` | 同上 | 同上 |
| `tests/integration/execution/native_conversation_roundtrip_test.go:330` | `!Contains("accepted task-") || Contains("released")`（释放后的回执判定） | 改为“回执不是 `ok`，且含 `released`”的具名判定 |
| `tests/integration/execution/plugin_phase_ordering_test.go:109` | `Contains("accepted task-")` + `"(running)"` | `Content == "ok"` |
| `tests/integration/capabilities/native_tools_test.go:414` | 同上 | 同上 |
| `tests/integration/execution/subagent_call_test.go:124` | `Contains(receipt.Content, "accepted task-")` | `Content == "ok"` |
| `tests/integration/host/agent_reference_test.go:101` | 同上 | 同上 |
| `tests/integration/host/request_boundary_test.go:305` | `Role==tool && Contains("accepted task-")` 选消息 | `Role==tool && ToolID==<callID>` 选消息，再断正文 |
| `examples/wechat-bot/smoke_data_test.go:175` | `accepted task-`/`(running)` | 断言回执为 `ok`，并以结构（role/tool_call_id）判“是本调用的回执” |
| `examples/wechat-bot/smoke_harness_test.go`（`smokeProvider.answerFor:338-340`、`smokeFrame`、`smokeEndedWithTool`） | `strings.Contains(end, "accepted task")`、`[send_ok]` 前缀 | 按 `role` + `tool_call_id`（`ToolCallID` 已在 `smokeModelMessage` 里）取键；`[send_` 前缀判断改为“`tool` 角色结果”判断 |

- **原则**：脚本模型与断言都以**结构**（角色、调用 id）取键，不以回执正文取键——否则下一次改文案又会连锁失效。这也是"回执是凭据不是消息"在测试面的落地。
- **不得**为了省事把 smoke 脚本改成"看到任何 tool 结果都答同一句话"——那会把"模型是否被确认诱导"这条判据彻底抹平（G2 症状锚依赖它）。

## D7 写集与串行

| 叶 | 生产写集 | 测试写集 | 只读 | 串行 |
|---|---|---|---|---|
| 1.1 | `runtime/agent/loop.go`（`admissionText`） | `tests/integration/execution/quiet_receipts_test.go`（新）、D6 表前 8 行（7 个产品测试文件 / 10 处断言）+ `examples/wechat-bot/smoke_data_test.go`（1 处）、`smoke_harness_test.go`（分支键） | `task.AdmissionReceipt`、`dispatch` 三分支 | `loop-receipts`；与 1.2/1.3 同域串行（1.1 → 1.2 → 1.3） |
| 1.2 | `host.go`（`hostSendTool.Call` 成功路径） | `tests/integration/host/quiet_receipts_test.go`（新）、`tests/integration/host/event_integrity_test.go` 三处 `[send_ok]` 断言、`smoke_harness_test.go`（`[send_` 帧判断） | `hostSendTool.denied`、`hostSendTool.Declaration` | `root-package-tagent`（与 02 域同编译单元 → 串行）；`wechat-smoke`（与 00 域同文件族 → 00 先落） |
| 1.3 | `runtime/agent/loop.go`（`admitNotice`、`Drive` 的 `progressNotes` 调用点） | `runtime/agent/loop_test.go`（两个 progress 子测试反向改写）、`tests/integration/execution/quiet_receipts_test.go`（扩展） | `commitNotice`、`NoticeFeed`、`intake` | `loop-receipts`；与 1.1 同文件 → 严格串行 |

- **1.1 与 1.2 的真实交集是 `examples/wechat-bot/smoke_harness_test.go`**（不是 host.go/loop.go）：两叶各自要改脚本模型的分支键，故按一级 D4 合单或严格串行。本域建议**串行**：1.1 改受理键、1.2 改 send 键，两次小改比一次大改更好归因。
- **与 02 域相交**：`host.go`（1.2 vs 2.1）与 `runtime/agent/loop.go`（1.3 vs 2.1 的 `Turn.Failure` 贯通）——必须串行，禁止并发。

## D8 风险与回退

| 风险 | 影响 | 缓解 / 回退 |
|---|---|---|
| 只去 `in.notify` 没去掉链投影读路径 | progress 仍进模型，条款假绿 | 1.3 写集含 `Drive` 读路径；正锚直接读 `provider.recorded()` 请求消息断言不含 progress 文本 |
| 把终态通知一起收窄 | 结算结果静音，症状 1 反而更重 | D5 硬边界；1.3 消费者锚必须证明终态仍驱动 |
| `ok` 使脚本模型不再应答、既有 smoke 用例连锁红 | 大面积红、归因困难 | D6 结构取键；每次改文案后先跑 `-run TestLocalSmoke` 全量 |
| 拒绝文本被动 | 模型把失败当成功 | `[send_denied]` 一字不改；1.2 负锚具名断言 |
| 删掉 progress 事实凑"静默" | 审计与 Seq 契约破 | 负锚 `progress_fact_still_committed`；04 逐条核对既有场景 |
| 测试里手工接 `Backflow(NoticeProgress)` 冒充生产消费者 | 假绿 | 消费者锚必须读 `Drive` 的真实请求装配；并如实声明"progress 无生产发出者" |

## D9 本域不做的

- 不写主规格与 golden（04 域）；不改 CI；不改示例层失败文案（02 域）。
- 不做 progress 业务白名单（一级 D6 已否）。
- 不干预 agent 主动 send（用户裁定）；不新增回合内 send 单次纪律。
- 不改 `HoldReceipt`/`[send_denied]`/终态通知的任何文本与语义。

## 相邻能力边界（5.9 检索裁定）

- 与 `delivery-dispatch-receipts` 的边界：本能力只改**模型可见文本形态**（send 成功回执 → `ok`）；内部回执总线（发送失败 ERROR/扣留 WARN 注入 inbox）的触发与血统不受影响。

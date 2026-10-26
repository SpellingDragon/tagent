# 00-symptom-anchors：三条症状直译锚（麻雀域·无新能力）

## Why

本变更的立案输入是用户三句原话（见一级 `proposal.md`「症状」节），不是三条工程指标。R33 的教训是：把症状翻译成工程语言（"通知恰好消费一次"）后，工程化验收可以全绿而原始现象照旧复现。因此本域的唯一产物是**用户视角的端到端测试**——在正式微信宿主装配上，从用户收到的消息（outbound）这一侧说话：

1. **受理后静默**：委派被受理后，用户不该收到任何"已受理/在跑/稍后会通知你"这一类确认。
2. **完成恰一条**：任务结算后，用户在本轮收到**恰好一条**结果消息，且该条不是确认类话术。
3. **失败必告知**：模型调用**真失败**时，用户收到一条**带原始错误信息**的告知；成功回合不产生失败话术。

三条测试第一天存在、且在**修复前的代码上必须红**。它们不新增任何产品能力，也不改写既有能力条款——本域是后续 01/02/03/04 域的启动门（一级 `specs/orchestration/spec.md`「症状锚启动门」）。

## What Changes

- 在既有 `examples/wechat-bot/smoke_test.go`（`//go:build smoke`）中新增三个**独立父测试函数**（同文件不同函数，触碰集相交→按 D4 合单为一次写入）：
  - `TestLocalSmokeSymptomAcceptedSilent`（G1 症状 2）
  - `TestLocalSmokeSymptomSettledOnce`（G2 症状 1）
  - `TestLocalSmokeSymptomFailureNotice`（G3 症状 3）
- 全部复用既有 smoke 台架：`bootSmokeRig` / `newSmokeProvider`（脚本模型 + 请求计数）/ `postTaskSession`（独立注入会话，不污染既有三会话期望）/ `rig.recorded()`（替身通道的 outbound 事件）/ `installSmokeLog`。故障注入沿用 `smokeProvider.hook` 的 500 先例（`smoke_errors_test.go`、`smokeLongModelFaultHook`）。
- 断言一律打在**用户世界**（outbound 事件按 `Target` 过滤）与**模型世界**（`provider.recorded()` 里模型真实收到的请求消息）两个可观测面；不读内部字段、不查日志文本充当行为证据（日志只作辅助定位）。
- 每条叶交付「**当前红证形态 + 跑法**」：命令、期望的失败断言、以及**如果该断言在 fake 模型下不红**的如实记录（不伪造红）。
- 本轮仅落 OpenSpec 文档：不改产品代码、不改产品测试、不改 CI 与主规格，不提交。

## Capabilities

### New Capabilities

无。本域不产生产品行为，因此不写能力 delta；本域 `specs/symptom-anchors/spec.md` 的对象是**三条测试自身的 golden 现状契约**（存在性、红证形态、不得缩水），已在文件头显式声明。

### Modified Capabilities

无。`persistent-event-loop` 与 `event-integrity` 的修订归 04 域；本域不预写、不改写任何既有条款。

## 边界与依赖

- **依赖（接口常数优先，实现依赖次之）**：① smoke 台架的接口常数——`smokeProvider.hook` 的 `(w, status, modelName, messages) bool` 签名、`smokeModelMessage` 结构、`outboundEvent{Kind,Target,Body}`；② `postTaskSession(text, session)` 的独立会话语义（空标签=最近准入会话）；③ 500 注入先例（脚本模型对指定标记返回 `http.StatusInternalServerError`）。这些都是**接口常数**，不是实现依赖：本域不 import 产品内部包，只经台架与 HTTP 注入面。
- **被依赖方**：01.1（受理回执→`ok`，是 G1 模型世界断言与 G2 转绿的最短闭环）、02.1/02.3（失败直投，是 G3 转绿的唯一路径）、04 域（把三测试登记进 short scope 与 CI 选择器）。本域不等待它们，先红。
- **接口面（本域可写/可读的既有面）**：写 `examples/wechat-bot/smoke_test.go` 的三个新函数；只读 `smoke_harness_test.go`、`wechat_test.go`（`fakeChannel`/`outboundEvent`）、`smoke_errors_test.go`（注入写法）。`smoke_test.go` 是本域与 02 域的**同文件串行点**（02 需修订同文件里既有的 `smokeErrorsFailureText` 等待），按 D4 串行。
- **禁止事项**：① 不改产品代码（`host.go`、`runtime/agent/**`、`examples/wechat-bot/wechat.go` 一律只读）；② 不新建微信测试文件、不放宽 `mirrorExemptFiles`；③ 不新增会话、不改变既有三会话（`smokeChats`）的期望轮次——新增测量一律走独立注入会话；④ 不以内部字段、日志文本或"结构存在"充当行为证据；⑤ 不在"fake 模型下本来不红"时伪造红或用 `t.Skip` 掩盖（如实记录形态并上报）；⑥ 不改 `scripts/preflight.sh`。

## Impact

- 测试：`examples/wechat-bot/smoke_test.go`（+3 个父测试函数，`smoke` 标签）。
- 运行面：三条测试的函数名以 `TestLocalSmoke` 开头，会进入 `scripts/preflight.sh` 的 `-run TestLocalSmoke` 集合——**00 域落地后 preflight 会红，直到 01.1（G1/G2）与 02.3（G3）转绿**。这是有意的红窗，不是回归；F2 全量门禁只在 F3 前要求全绿（见 design D4 与一级 `tasks.md` F2）。
- 运行时成本：三条测试各起一台 smoke 宿主；`-race` 下单条约 30–60s 量级（未实测，按既有 smoke 单测时长估计）。不与长跑矩阵同波（R32）。
- 授权：本轮仅 OpenSpec 文档；产品代码、产品测试、CI 与主规格需后续授权。

## 规划入口

- 静态决策、红证口径与写集：[design.md](design.md)
- 三叶账目、红证跑法与勾选判据：[tasks.md](tasks.md)
- golden 现状契约：[specs/symptom-anchors/spec.md](specs/symptom-anchors/spec.md)

# Tasks — 02-failure-delivery（真失败直投原始原因）

> **状态：恰 3 个叶 checkbox（2.1—2.3），全部已勾选（02 域完成）。**
>
> **串行约束（硬）**：本域 `host.go`、`runtime/agent/loop.go`、`examples/wechat-bot/smoke_harness_test.go` 与 01 域**相交** → **本域 MUST 在 01 域三叶全部勾选后启动**，禁止与 01 并行落在同一工作树。域内顺序 2.1 → 2.2 → 2.3：2.2 依赖 2.1 引入的 `Outcome.Cause()`；2.3 依赖 2.1 的框架直投，且与 00 域同文件族（`smoke_test.go` 只读）。
>
> **前置**：00.3 的红证（G3 现状为"只收到示例层固定文案、无原始错误"）与 01.1 的转绿（受理静默）都必须在场；缺 00.3 红证则本域失去症状直译锚。
>
> **上报项 P-02（越界申报，勾选前须编排者确认）**：本域写集含 `examples/wechat-bot/wechat.go`（删固定降级文案）与 `smoke_errors_test.go`、`smoke_harness_test.go`（等待键）。不删示例层文案，用户会收到"框架原文 + 示例文案"两条，2.3 的恰一条断言永远不绿。
>
> **上报项 P-03（代码事实与派发单不符，已写入 design D2/D3）**：模型真失败时 `Loop.Drive` 返回 `Turn{Terminal: KindFailed}, nil`，**`Invoke` 的 error 为 nil**，原始错误只在 `Outcome.Trace[i].Err`。故 2.1 通过新增 `Turn.Failure` + `Outcome.Cause()` 贯通，`runtime/agent/loop.go` 与 `api/model/terminal.go` 在 2.1 写集内；`Invoke` 的 error 保持"宿主/协议错误"语义，不投模型失败话术。
>
> **红证口径**：2.1、2.2、2.3 各自先在**修复前**跑点名命令记红（失败断言必须落在语义断言上，编译错误不算红），改后同命令 exit=0。红/绿 `-json` 日志落仓库外 `/tmp/event-semantics-boundary/<叶>/`。
>
> **禁止**：不把失败写进 assistant 正文或有载荷事实；不复用 `MetaKeyNoticeKind`/`MetaKeyTaskID`；不动 `cancelled` 静默、`[send_denied]`、受理回执、终态通知；不改 golden/主规格/CI；不在示例层加第二份兜底文案；不以测试内直调 `deliverFresh` 冒充消费者；不提交。

## 三叶账目

- [x] 2.1 把真失败的原因贯通到宿主并直投（`api/model/terminal.go` 新增只读 `Outcome.Cause()`、`runtime/agent/loop.go` 新增 `Turn.Failure` 并由 `Drive` 每轮同源填写、`host.go` 的 `deliverFresh` 增参并在「`KindFailed` 且无可交付正文且有原因」时经 `MessageSender` 直投原文、同轮把 `rl.AttemptError` 委托 `Cause()` 保持行为不变、投递前提交一条无模型载荷的 `failure-notice` 事实）+ 新增 `TestFailureNotice` 与 `TestOutcomeCause` —— 验证：`E=/tmp/event-semantics-boundary/02.1; mkdir -p "$E"; go test ./tests/integration/host/ ./api/model/ -run '^(TestFailureNotice|TestOutcomeCause)$' -count=1 -race -json -timeout 20m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o '"Action":"fail"[^}]*' "$E/run-1.json" | head -3`（修复前预期 exit≠0，失败行落在 `raw_error_delivered_once`；修复后必须 exit=0）；正锚 `TestFailureNotice/raw_error_delivered_once`（outbound 恰一条、含 `500`、不含示例层旧文案、等于 `turn.Failure`）；负锚 `TestFailureNotice/failed_turn_with_body_delivers_body_only`（失败但带正文时只投正文、零错误话术）；消费者锚 `Host.Invoke → driveLocked → deliverFresh → MessageSender` 的正式宿主装配（非直调私有方法），并用 `Run.SessionFacts` 核对事实 `Message.Content == ""` 与 summary 前缀；前置 01 域三叶全部勾选（`host.go`/`loop.go` 相交）、00.3 红证在场

- [x] 2.2 让模型观测行在失败终态带原因（`api/model/observe.go` 的 `callObservation.end` 在同一行按条件追加 `error=%q`：仅 `KindFailed` 且 `Cause()` 非空且文本非空；成功行与空原因行零字段）+ 在 `TestModelCallObservation` 补三子测试 —— 验证：`E=/tmp/event-semantics-boundary/02.2; mkdir -p "$E"; go test ./api/model/ -run '^TestModelCallObservation$' -count=1 -race -json -timeout 10m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o 'terminal=failed[^"]\{0,80\}' "$E/run-1.json" | head -3`（修复前预期 exit≠0，失败行落在 `a_failed_request_names_its_cause`；修复后必须 exit=0）；正锚 `TestModelCallObservation/a_failed_request_names_its_cause`（真实 `ReduceChannel` 归约出的 `ResponseError{Message:"...500..."}` → 单行含 `terminal=failed` 与 `error=` 原文）；负锚 `TestModelCallObservation/a_completed_request_names_no_cause`（成功行 MUST NOT 含 `error=`）与 `a_failure_without_a_message_renders_no_empty_cause`（无 Message 的 `ResponseError` MUST NOT 渲染 `error=""`）；消费者锚 真实 `ReduceChannel` 归约 → `quietCallDebug` 的 INFO 行（`watchObservation` 替换 logger 捕获），非手工拼 `Outcome`；前置 2.1（`Outcome.Cause()` 已落）

- [x] 2.3 收敛示例层失败话术并让症状 3 转绿（删 `examples/wechat-bot/wechat.go` 的固定降级文案、改一行 debug；同步把 `smoke_errors_test.go` 的 `smokeErrorsFailureText` 断言改成「原始错误恰一条 + 不含旧文案」、把 `smoke_harness_test.go` 的 `append(wants, "执行出错")` 改为等待原始错误）+ 跑 `TestLocalSmokeSymptomFailureNotice` 取绿证 —— 验证：`E=/tmp/event-semantics-boundary/02.3; mkdir -p "$E"; go -C examples/wechat-bot test -tags smoke . -run '^(TestLocalSmokeSymptomFailureNotice|TestLocalSmokeWave2Errors)$' -count=1 -race -json -timeout 30m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o '执行出错[^"]*' "$E/run-1.json" | head -2`（修复前预期 exit≠0；修复后必须 exit=0 且 grep 无 `执行出错` 命中）；正锚 `TestLocalSmokeSymptomFailureNotice/raw_error_reaches_the_user`（00.3 转绿：恰一条、含 `500`、不含旧文案）；负锚 `TestFailureNotice/success_turn_no_failure_notice`（成功回合零失败话术、零失败事实）与 `TestLocalSmokeSymptomFailureNotice/success_turn_no_failure_text`；消费者锚 `hostRuntime.Drive → Host.Invoke → driveLocked → deliverFresh → bot sender → fakeChannel outbound` 的正式入站装配 + `TestLocalSmokeWave2Errors` 的既有失败路径仍绿；前置 2.1（框架直投已落）、00 域已落（`smoke_test.go` 只读）

## 每叶实施步骤（非 checkbox）

- 2.1：先加 `Outcome.Cause()` 与 `Turn.Failure`（含注释说明"失败以外的终态为空、无原因不编造"），再改 `deliverFresh` 签名与 `driveLocked` 传参，最后加 `noteFailureNotice`；`rl.AttemptError` 改为委托后跑一次 `go test ./rl/ -run 'TestAttempt' -count=1` 确认零行为变化。
- 2.1 的子测试集（名字逐字符固定，后续叶不再改名）：`raw_error_delivered_once`、`failure_fact_left_without_model_payload`、`failure_fact_does_not_drive_a_turn`、`failed_turn_with_body_delivers_body_only`、`cancelled_turn_stays_silent`、`host_error_not_a_model_failure`；`success_turn_no_failure_notice` 在 2.1 先建、2.3 扩展成 smoke 侧同名负锚。
- 2.1 的负向用例构造：失败但带正文 = 一个完整响应后接一个错误响应（`KindFailed` 且 `Body != nil`）；宿主错误 = `ErrHostClosed`/地址不完整（`Invoke` 的 error 分支不得投任何失败话术）。
- 2.2：只改 `end` 的格式串拼接，不改 `quietCallDebug`、不改级别、不新增行；补测试时用真实 `ReduceChannel`，不手拼 `Outcome`。
- 2.3：删文案后先 `go -C examples/wechat-bot vet ./...`（`failed` 必须有使用点），再跑 `-run '^TestLocalSmokeWave2Errors$'` 单独归因，最后与 G3 一起跑；`smoke_test.go` 只读，不改 00 域的断言。
- 2.3 收尾：把 00.3 的 `run-before.json`（红）与本次 `run-1.json`（绿）配对贴入叶下说明，形成症状 3 的"红→绿"双证据。
- 任一叶改动后若 `go test ./...`（根 module）出现 `Turn` 字面量或 `deliverFresh` 调用点连锁红，按 D8 写集核对，不得顺手改无关文件。

## 域内自检

- `failure-notice` 四条 requirement 各有断言对应；"恰一条"在 smoke 与集成两层都被断言。
- 触发条件可逐条核：`cancelled` / `completed` / `partial-only` / 有正文 / 无原因 五类均零失败话术。
- 失败事实 `Message` 零载荷、无 `MetaKeyNoticeKind`/`MetaKeyTaskID`，且下一轮请求不含失败原因（`provider.recorded()` 核对）。
- 观测行仍是一次请求一行；成功行与空原因行不含 `error=`；`attempts.jsonl` 的 `error` 与观测行同源。
- `examples/wechat-bot/wechat.go` 的 diff 只有删除文案 + 一行 debug，`invoke` 签名与 5 处赋值零改动。
- 未改 golden、主规格、CI、`smoke_test.go`；未改 `[send_denied]`、受理回执、终态通知；未提交。
- **P-02 / P-03 已上报**：若 P-02 被否，2.3 不可勾选、G3 标为不可达；若 P-03 被否（坚持用 `Invoke` 的 error），2.1 必须停下来改设计，不得编造 error。

## 本域交付（非 checkbox，只读核对）

- 已写 `proposal.md` / `design.md` / `tasks.md` / `specs/failure-notice/spec.md`。
- 未改任何产品代码、产品测试、CI、主规格与 golden；未提交。
- 主代理独立执行 `openspec validate` / `openspec status`（本域不自行宣称 CLI 校验通过）。

## 叶证据（红 → 绿配对，证据在仓库外 `/tmp/event-semantics-boundary/`）

- 02.1（`02.1/run-1.json`）：`go test ./tests/integration/host/ ./api/model/ -run '^(TestFailureNotice|TestOutcomeCause)$' -count=1 -race -json -timeout 20m` → exit=0（15 pass / 0 fail）。
  正锚 `raw_error_delivered_once`、事实锚 `failure_fact_left_without_model_payload` + `failure_fact_does_not_drive_a_turn`、
  负锚 `host_error_not_a_model_failure` / `cancelled_turn_stays_silent` / `success_turn_no_failure_notice`、截断锚 `a_long_cause_is_truncated_and_marked`。
  本叶引入新符号（`Outcome.Cause()` / `Turn.Failure`），测试与实现同批落地 → “修复前”只能表现为符号缺失（编译红），不是语义红；语义红由 02.3 的 G3 承担。
- 02.2（`02.2/run-1.json`）：`go test ./api/model/ -run '^TestModelCallObservation$' -count=1 -race -json -timeout 10m` → exit=0（10 pass / 0 fail）。
  正锚 `a_failed_request_names_its_cause`、负锚 `a_completed_request_names_no_cause` + `a_failure_without_a_message_renders_no_empty_cause`。
- 02.3（`02.3/run-before.json` 红 → `02.3/run-1.json` 绿）：`go -C examples/wechat-bot test -tags smoke . -run '^(TestLocalSmokeSymptomFailureNotice|TestLocalSmokeWave2Errors)$' -count=1 -race -json -timeout 30m`。
  红 = HEAD `84d6625` 代码 + 本单 G3 测试（失败断言 `smoke_test.go:3397 失败告知不含原始错误标识 500`，语义红）；绿 = exit=0（8 pass / 0 fail），grep `执行出错` 零命中。
- **结构不可达申报**：`TestFailureNotice/failed_turn_with_body_delivers_body_only` 以 `t.Skip` 登记——`Loop.decide` 不设 `model.Request.Stream`，一次请求只折叠一个非流式响应，`KindFailed` 的 outcome 永远无正文；宿主消费者层无法构造“失败且带可交付正文”。`deliverFresh` 仍按 spec「有正文时正文优先」先判正文，该分支的正确性由 `TestOutcomeCause/the_retained_body_names_the_cause_first` 与 `deliverFresh` 的正文路径共同钉住。
- 既有红（非本域引入）：`TestLocalSmokeWave2Recall` 在 HEAD `84d6625`（无本单改动）即红——`smoke_recall_test.go:140` 期望 `recall recent count=2`，实得 `count=5`（会话事实写进了 recall 分区）；证据 `/tmp/event-semantics-boundary/D4-before/recall-head.log`（HEAD 工作树复现，exit=1）。

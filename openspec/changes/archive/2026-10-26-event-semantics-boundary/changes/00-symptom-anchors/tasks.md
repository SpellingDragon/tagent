# Tasks — 00-symptom-anchors（三条症状直译锚）

> **状态：恰 4 个叶 checkbox（00.1—00.4）：00.1—00.3 为三症状红证交付，00.4 为 exec 中间态静默回归锚（2026-10-26 补差新增，已勾选并附凭据）。** 本域前三叶是**红证交付**：勾选判据是"命令可复跑且确定性失败在约定断言上"，不是"跑绿"。转绿归 01.1 / 02.1 / 02.3，F3 以回滚即红复核。
>
> **派发单位**：四叶同写 `examples/wechat-bot/smoke_test.go`（同一文件、不同函数），按一级 D4 **合单为"00 域症状单"一次写入**；叶 checkbox 分列只为逐条对账。
>
> **前置**：无。本域是其余一切域的启动门（一级 `specs/orchestration/spec.md`「症状锚启动门」）。
>
> **红窗（必须提前告知编排者）**：三测试以 `TestLocalSmoke` 开头，会进入 `scripts/preflight.sh` 的 `-run TestLocalSmoke` 集合——本域落地后 preflight 变红，直到 01.1（G1/G2）与 02.3（G3）转绿。这是有意红窗，不是回归。
>
> **写集口径**：生产写集为空（禁改产品代码）；测试写集只有 `examples/wechat-bot/smoke_test.go`。`smoke_harness_test.go` 只读——若实施期必须加台架辅助（如 worker 放行闸），先上报扩写集，不得顺手改。
>
> **证据口径**：`-json` 日志落仓库外 `/tmp/event-semantics-boundary/<叶>/run-<i>.json`；仓内不新增证据文件。失败断言行与退出码摘入本叶说明（追加在叶下方，非 checkbox）。
>
> **上报项 P-02（跨域，先记不修）**：G3 的"恰好一条"在 02.1 落地框架直投后会变成两条（框架原文 + 示例宿主 `hostRuntime.Drive` 的固定降级文案）。02 域必须同步收敛示例层文案；本域只在 G3 注释里写明该约束，不改 `examples/wechat-bot/wechat.go`。

## 四叶账目

- [x] 00.1 写出受理静默症状测试 `TestLocalSmokeSymptomAcceptedSilent`（独立注入会话 `symptom.g1/one`；脚本 provider 的 `hook` 用闸门阻塞结算轮的请求，保持"受理已回、结算未到"窗口）并跑出确定性红 + 断言受理后该会话 outbound 事件数=0 且受理后的下一请求 tool 结果 MUST 恰为 `ok`（无 task id/`(running)`/`background task`/`notification`） —— 验证：`E=/tmp/event-semantics-boundary/00.1; mkdir -p "$E"; go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptomAcceptedSilent$' -count=1 -race -json -timeout 30m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o 'accepted task-[^"]*' "$E/run-1.json" | head -2`（预期 exit≠0，失败行落在受理回执非 `ok` 的断言）；正锚 `TestLocalSmokeSymptomAcceptedSilent/silent_until_settled`（红）；负锚 `TestLocalSmokeSymptomAcceptedSilent/legitimate_answer_still_delivered`（用户正常提问的回答仍必须投递，防"静默吞正常输出"）；消费者锚 `newWechatApp → Boot → bindWechat → bindChannel → registerHandlers` 正式装配下 fakeChannel 记录的 outbound + 脚本 provider 记录的真实请求；前置 无
  > 勾选理由：红证见 `evidence/red.txt`——G1 `exit=1`，模型真实收到的 tool result 仍是 `accepted task-1 (running): …`（smoke_test.go:3268）；用户世界 outbound=0 与负锚为绿（今天即绿，已如实登记）。会话绑定按第三轮 grill 裁定改为 `rig.drive` 生产入站（不再是独立注入会话，未用 postTaskSession；偏离见 `evidence/red.txt` §6）。

- [x] 00.2 写出完成恰一条症状测试 `TestLocalSmokeSymptomSettledOnce`（独立注入会话 `symptom.g2/one`；同链路放闸前先断受理窗口、放闸后数本轮文本）并跑出确定性红 + 断言本轮 outbound 文本数=1、该条不命中确认词表且等于结算正文 —— 验证：`E=/tmp/event-semantics-boundary/00.2; mkdir -p "$E"; go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptomSettledOnce$' -count=1 -race -json -timeout 30m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o 'outbound=[0-9]*[^"]*' "$E/run-1.json" | head -3`（预期 exit≠0，失败行落在"2 条 vs 1 条"或确认词命中）；正锚 `TestLocalSmokeSymptomSettledOnce/settled_exactly_once`（红；脚本"见状态素材即应答"为行为仿真，注释写明否决时的替代出口）；负锚 `TestLocalSmokeSymptomSettledOnce/no_duplicate_settle_text`（结算正文第二条必须判失败）；消费者锚 同 00.1 的正式装配 + 结算跟进轮经 `deliverFresh` 的 sender 投递；前置 00.1（同文件同台架辅助）
  > 勾选理由：红证见 `evidence/red.txt`——G2 `exit=1`，结算前的请求仍含 `accepted task-`、本轮 outbound 2 条 vs 1 条（smoke_test.go:3336/3349）；结算回合自身断言与负锚为绿（实测修正已登记）。同链路走 `rig.drive`（偏离见 `evidence/red.txt` §6）。

- [x] 00.3 写出失败必告知症状测试 `TestLocalSmokeSymptomFailureNotice`（走 `rig.drive` 生产入站；`smokeProvider.hook` 对标记返回 500，仿 `smoke_errors_test.go`）并跑出确定性红 + 断言 outbound 恰好一条失败告知且正文含原始错误标识（`500`/注入文案片段）、MUST NOT 只含柔化系统话术 —— 验证：`E=/tmp/event-semantics-boundary/00.3; mkdir -p "$E"; go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptomFailureNotice$' -count=1 -race -json -timeout 30m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o '执行出错[^"]*' "$E/run-1.json" | head -2`（预期 exit≠0，失败行落在"正文不含原始错误"）；正锚 `TestLocalSmokeSymptomFailureNotice/raw_error_reaches_the_user`（红）；负锚 `TestLocalSmokeSymptomFailureNotice/success_turn_no_failure_text`（成功回合零失败话术，今天必须绿、修复后仍绿）；消费者锚 `hostRuntime.Drive` 生产入站 → `Host.Invoke` → `deliverFresh`/sender → fakeChannel outbound；前置 无（与 00.1/00.2 合单，同文件不同函数）
  > 勾选理由：红证见 `evidence/red.txt`——G3 `exit=1`，500 注入后用户只收到罐头 `执行出错：本轮模型回合失败，未产出交付内容。`，不含原始错误标识（smoke_test.go:3419，非原始错误）；负锚为绿。

- [x] 00.4 写出 exec 中间态静默症状测试 `TestLocalSmokeSymptomExecStateSilent`（`rig.drive` 生产入站发起 `exec_run`，按 tmux monitor 的 stable/suspect/exited 三态各等一条审计事实）并断言三态均不新增模型请求、不新增用户 outbound，同时三态审计事实仍落盘（`Content==""`、无 `MetaKeyNoticeKind`/`MetaKeyTaskID`，允许状态机重复观察） —— 验证：`E=/tmp/event-semantics-boundary/00.4; mkdir -p "$E"; go -C examples/wechat-bot test -tags smoke . -run '^TestLocalSmokeSymptomExecStateSilent$' -count=1 -race -json -timeout 30m > "$E/run-1.json" 2>&1; echo "exit=$?"; grep -o '\[symptom-g4\][^"]*' "$E/run-1.json" | head -4`（预期 exit=0）；正锚 三态各静默（stable/suspect/exited 观察后 provider 请求数与 outbound 均不增）；负锚 `state_still_recorded`（三态审计事实仍落，防"静默=什么都没记"）；消费者锚 `rig.drive` 生产入站 → `Host.Invoke` → `exec_run` → `Host.observeExecState` → `CommitToSession`（真实 boot 路径，非测试直调）；前置 00.1（同台架、同文件）
  > 勾选理由：绿证 `00.4/run-1.json`——`exit=0`，`three_states_leave_no_turn` 与 `state_still_recorded` 两子测试 PASS；正锚三态各 `requests=2, outbound=1` 不增，负锚三态审计事实均落盘（`Content==""`、无通知分类与任务号、exited 带 `exit_code=7`）。测试 `76330ae` + `cfbb034`（重复观察容错），宿主修复 `1632ec8`。

## 每叶实施步骤（非 checkbox）

- 先写测试与台架辅助（只在本文件内），跑一次记基线；红证必须命中 D2 约定的断言，编译器/台架/超时错误不算红。
- 同一命令连跑 2 次，失败点一致才计"确定性红"；日志落仓库外，退出码原样保留（禁止 `| tee` 取管道码、禁止 `|| true`）。
- 若某条子断言在 fake 模型下**不红**：`t.Logf` 记录形态与原因，叶内写明"红证由模型世界承担"，并在汇报中单列——不得改造脚本制造红。
- 四叶一次写入后统一复跑各叶命令，把退出码与失败断言行贴在叶下。

## 域内自检

- 三条父测试均可被 `^Name$` 精确选中，`-run` 与函数名逐字符一致；不存在 `TestLocalSmokeSymptom` 之外的前缀。
- 无 `t.Skip`、无环境变量开关、无 `-short` 条件；红证在任何机器上可复跑。
- 断言只读 outbound 事件与 provider 请求消息两处；未引用 golden、未读产品内部字段。
- 三测试各起独立宿主并 `defer rig.close()`；未改动 `smokeChats` 轮次与既有用例期望。
- 红窗已在上报中声明（preflight 会红到 01.1/02.3 转绿）。

## 本域交付（非 checkbox，只读核对）

- 已写 `proposal.md` / `design.md` / `tasks.md` / `specs/symptom-anchors/spec.md`。
- 未改任何产品代码、产品测试、CI、主规格与 golden；未提交。
- 主代理独立执行 `openspec validate` / `openspec status`（本域不自行宣称 CLI 校验通过）。

# 05 interactive-reentry 孙任务（条件波次）

- [ ] 5.1 task 类型分派：`send{target:{task}}` 按任务类型分派（round 型→02 通道；tmux 型→会话注入 Rearm+SendKeys） —— 验证：`go test ./runtime/task -run '^TestSendReentryTypeDispatch$' -count=1`；负锚 `rejects_tui_session`
  **（裁剪：降档登记 @W5-A，2026-11-01）四道结构性障碍，非实现难度：**
  ① **无 tmux 型 task 实体**：`runtime/task.Spec`/`memoryTask` 无执行器类型字段；tmux 会话在 `tool/exec` 里是带 `Owner.TaskID`（*调用方*任务）的 `ResourceKey`，`send{target:{task}}` 没有可落到 tmux 的任务记录。
  ② **无注入基质**：`tool/exec` 的 `Executor` 端口只有 `Start/Probe/Capture/Stop`；全仓 `grep -rn "IsTUI\|send-keys\|op=peek\|op=send" --include=*.go .` → 命中 0（exit 1）；`grep -c SendKeys tool/exec/*.go runtime/task/*.go` → 全 0。历史 `SendKeys`（`tool/action`+`agent/task`）在 7c4c7121 运行时重建中已随包移除。
  ③ **分派点在白名单外**：类型分派只能落在 `assembly/host_tools.go` 的 `hostSendReentryPort.ReenterTask`（该处 `continuations.Served` 判定即"round 型"门槛）；本单禁改 assembly，故注入侧即便在 `tool/exec` 实现也无生产消费者（测试补接线不算消费者，R14）。
  ④ **Rearm 与 watch 互换代直接冲突**：`tool/exec/state.go:125 watch()` 对 `res.stop != nil` 直接返回（单 tracker 单次），重启观察基线必须改 `track`/`watch`，属本单禁改的 detector/watch 语义。
  → 验证命令 `go test ./runtime/task -run '^TestSendReentryTypeDispatch$'` 在本运行时无被测对象（测试不存在，非红）；负锚 `rejects_tui_session` 同理无 IsTUI 判定可拒。降档不影响 02 主线：`go test ./tests/integration/execution -run '^TestSendReentersAgentTask$' -count=1` 绿（round 型重入通道活着）。
- [x] 5.2 TTL 与幂等：写入型重入重置任务 TTL 锚点（`async-task-lifetime` 既有条款）；同会话并发注入占坑单胜 —— 验证：`go test ./runtime/task -run '^TestInteractiveReentryTTL$' -count=1`；负锚 `peek_does_not_refresh`
  （交付：`Reactivate` 提交后把 TTL 锚点置为本次重入的移动时刻（`runtime/task/reactivate.go`）、`stamps.ttlFrom()` 取"准入/重入"较晚者作为 TTL 起点（`logical.go`），并新增 `WithClock` 时钟缝（对齐 `tool/exec.WithClock` 既有模式）使时间保证可判定而非靠 sleep。红→绿：`/tmp/send-cts-W5A/evidence/5.2-red.txt`（重入后一分钟读 `expired`）→ `5.2-green.txt`（PASS）。**裁剪：注入侧"并发注入占坑单胜"随 5.1 降档**；通道级并发单胜沿用 2.1 `rejects_concurrent_reactivation`（本波复跑 `-race -count=3` 绿）。）
- [x] 5.3 `exec op=send` 共通道确认：op=send 与 send 重入走同一注入路径（无第二条注入路径） —— 验证：`grep -c "SendKeys" tool/exec/*.go runtime/task/*.go` 与代码走查证据入台账；负锚：注入路径唯一性断言测试
  （**事实修正**：本运行时不存在 `exec op=send`——`tool/exec` 只授 `exec_run`/`exec_release`，全仓 `op=send` 命中 0；故"两条入口共用"退化为"只有一条、且无非预期第二条"，已按唯一性断言钉住：`tool/exec/reentry_channel_test.go` `TestExecutionFaceCarriesNoInjectionPath`（冻结 `Executor` 端口方法集 {Start,Probe,Capture,Stop} + 两个工具名 + 两侧声明参数集）。守卫有效性反证：临时给 `exec_run` 加 `send` 参数 → 测试红（`/tmp/send-cts-W5A/evidence/5.3-guard-bites.txt`），回退后绿。走查：`grep -c "SendKeys" tool/exec/*.go runtime/task/*.go` → 全 0；`grep -rn "IsTUI\|send-keys\|op=peek\|op=send" --include=*.go .` → exit 1。**裁剪：`op=send` 共存面**（对象不存在）随 5.1 降档。）

# 02 task-reentry-send 孙任务

验证约定同父变更；重激活测试须开 `-race` 且复跑 ≥10（§七）。

- [x] 2.1 registry 重激活迁移：`runtime/task` 新增 reactivate 迁移（settled→活跃，`MutationID("reactivate:...")`+`ExpectedRevision` 乐观并发），审计事实落链（settled→reactivated） —— 验证：`go test ./runtime/task -run '^TestTaskReactivation$' -count=1`；负锚 `rejects_failed_reactivation`、`rejects_cancelled_reactivation`、`rejects_concurrent_reactivation`（占坑单胜）
- [x] 2.2 send{task} 分派到重入通道：01 的分派口接入 task 重入（喂输入/重激活），回执三态沿用 —— 验证：`go test ./tests/integration/execution -run '^TestSendReentersAgentTask$' -count=1`；消费者锚：经 `assembly.Boot` 真实入口；负锚 `denies_unknown_task`、`denies_failed_task`（编排者复验：三锚绿+竞态 10 连绿，凭据 /tmp/send-cts-W0A2/evidence/ + 编排者复跑记录）
- [x] 2.3 新 Round 通知身份与恰好一次：重激活后再 settle 的 Notice 为新 Round 身份、不与旧合并，发起方后续回合恰好驱动一次 —— 验证：`go test ./tests/integration/host -run '^TestReactivatedSettleFiresOnce$' -count=1`；负锚 `old_round_notice_not_merged`
- [x] 2.4 存活类重入与拒绝面保真：running/suspect 拒绝并引导（等待/relaunch）；陈旧信号不串轮（旧 detector 信号不影响新轮次） —— 验证：`go test ./runtime/task -run '^TestReentryRefusalAndStaleSignals$' -count=1`；负锚 `rejects_inflight_reentry`、`stale_signal_ignored`
- [x] 2.5 subagent 生命周期延长：`tool/subagent` serve 支持被重入续行（Round 型续行点），原"一轮即 settle"契约注释与 spec 同步 —— 验证：`go test ./tool/subagent -run '^TestSubagentServesSecondRound$' -count=1`；负锚 `first_round_settle_unchanged`（未重入时行为零变化）
- [x] 2.6 S2 症状锚转绿：0.1 的 `TestSendSymptomReentersAgentTask` 绿 —— 验证：同 0.1 命令，exit 0 且用例数>0（红→绿双证据：红 /tmp/send-cts-W0A/0.1-red.json、绿编排者复跑）
- [x] 2.7 resume_task 工具面删除（DEL-3）：删除 `resume_task` 声明与挂载点（含任务工具族枚举）；**保留**被 send 复用的底层 Resume/注入通道并在注释声明归属；伴生引用降级检查 —— 验证：`go test ./runtime/task -run '^TestTaskToolFaceWithoutResume$' -count=1`；负锚：工具族枚举不含 resume_task；`grep -c "resume_task" --include=*.go -r .` 零残留

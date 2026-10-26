# Tasks：capability-gaps（W0 五单并发→W1 冒烟转正）

- [x] 1 实现 tmux Executor＋monitor 三态 —— 验证：TestTmuxExecutorRunsCommand（真 tmux echo）＋TestMonitorSuspectAfterQuiet＋负锚 TestExecWithoutTmuxNamesRefusal
- [x] 2 审批闭环：riskCritical 登记请求＋声明式风险面 —— 验证：TestHoldRegistersApprovalRequest＋TestRiskRulesGradeTools＋负锚 TestNoChannelStillRefuses
- [x] 3 worker 轨迹入 Capture —— 验证：TestWorkerCallsEnterTrajectory（全调用入册对账）＋负锚 TestNoCaptureZeroRecords
- [x] 4 skill_load 正文进下一轮请求 —— 验证：TestSkillLoadBodyEntersNextRequest＋负锚 TestUnknownSkillStillRefused
- [x] 5 host 四接线（metrics/exec/风险面/call_subagent） —— 验证：TestMetricsEndpointServesCounters＋TestCallSubagentGrantableByHostDocument＋smoke 前置绿
- [x] 6 冒烟四负锚翻正＋审批去补接线 —— 验证：go test -tags smoke 全绿（exec 正锚/skill 正锚/data 正锚/审批生产 Hold）
- [x] F1-F3 preflight 全绿＋CI 绿

# 06 notice-inbox-contract 孙任务

- [x] 6.1 持久接纳记录 + 旧时序删除（DEL-5）：启动通知入箱即落稳定身份接纳记录（来源+摘要+序号），`.notified`/`.consumed` 改名移至消费确认后；旧"入箱即改名"代码删除（完成判定唯一化，无第二套） —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeDurableAdmission$' -count=1`；负锚 `rename_before_confirm_absent`（改名早于确认即红）
- [x] 6.2 崩溃四窗口故障注入（N1 症状锚）：分别在入箱后/改名后/Drain 后/提交失败时 kill 子进程，恢复后通知仍被消费 —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeSymptomCrashNoLoss$' -count=1 -timeout 300s`；四窗口子测试齐全（缺一即红）
- [x] 6.3 幂等重投：重投同稳定身份不产生重复有效处理（重复恢复恰一次） —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeRedeliveryIdempotent$' -count=1`；负锚 `duplicate_admission_single_effect`
- [x] 6.4 消费确认语义：Drain 返回副本、Invoke 提交成功才清空/改名；失败保留在册 —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeDrainAckSemantics$' -count=1`；负锚 `failed_invoke_keeps_notice`
- [x] 6.5 来源分账（R4，N3 锚）：逐消息来源入持久事实 Metadata（新 `MetaKey*` 键），回合 `trigger_source` 不被污染；运维通知与用户消息同批各自可查 —— 验证：`go test ./tests/integration/host -run '^TestNoticeSymptomSourceQueryable$' -count=1`；负锚 `round_trigger_source_untouched`

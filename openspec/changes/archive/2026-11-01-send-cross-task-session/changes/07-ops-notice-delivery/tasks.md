# 07 ops-notice-delivery 孙任务

- [x] 7.1 固定目标接线 + pending 路径删除（DEL-4）：两条启动通知改投预定目标会址（配置 `ops_session` 声明，缺省宿主运维会址常量）；空地址 pending 路径**删除**（无消费者即删，`InjectInternal` 不完整地址改具名拒绝，`Drain` 空地址分支回卷） —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeOpsFixedTarget$' -count=1`；负锚 `no_pending_for_ops_notice`（空地址入 pending 即红）；`grep -c "in.pending" examples/wechat-bot/wechat.go` 零残留或收缩说明
- [x] 7.2 立即唤醒（N2 锚）：无人输入时通知驱动目标会话跑一轮；多会话竞争由预定目标收到 —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeSymptomOpsWakeupFixedTarget$' -count=1 -timeout 120s`；负锚 `arrival_order_does_not_decide_target`
- [x] 7.3 现场查询接线（R5，N4 锚）：转世通报的 WAL 尾部查询传真实查询面（受范围限制，有限条）；无存储/读失败走降级并清楚报告 —— 验证：`go -C examples/wechat-bot test . -run '^TestNoticeSymptomSceneRealOrDegraded$' -count=1`；负锚 `nil_scene_path_absent`、`degraded_reported_clearly`
- [x] 7.4 通知正文保全：元数据段/降级文案/日志关键字（`[reincarnation]`、`转世通报`、`现场块不可用`）逐项核对保留 —— 验证：`go -C examples/wechat-bot test . -run '^TestReincarnationNoticePreserved$' -count=1`（既有断言迁移后仍绿）

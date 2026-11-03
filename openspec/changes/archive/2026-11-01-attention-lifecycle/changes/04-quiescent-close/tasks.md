# 04 quiescent-close 孙任务

验证约定：`go test -run '^TestName$' -count=1` 逐字符；三锚冻结；症状锚 Q-A–Q-D 红先立（红因各命中语义缺失）。

- [x] 4.1 同源函数 `Unsettled()`：实现"未终态工作∪未消费通知"同源函数（权威账读取+提交序号栅栏+增量索引），关闭/重启/报告三处改读它；诊断面保留观察用 —— 验证：`go test ./runtime/task -run '^TestUnsettledSingleSource$' -count=1`；负锚 `no_second_criterion`（无第二判据实现）
- [x] 4.2 关闸分档+静止相位：`Host.Close` 编排改为"关 intake→quiesce→停消费者"；consumer 排空判据=无未消费∧无未终态∧写入者停；derived 派生工作计入收敛集合 —— 验证：`go test ./tests/integration/host -run '^TestQuiesceDerivedNotSelfLocked$' -count=1`（Q-B）；负锚 `intake_refused_derived_admitted`（intake 拒、derived 收）
- [x] 4.3 三态出口与报告：`Quiesced`/`Cut`/`Broken` 三态返回；禁零 deadline（`Deadline`→`Timeout`）；超时/Cut 报告逐地址权威清单 —— 验证：`go test ./tests/integration/host -run '^TestQuiesceOfflineCutReport$' -count=1`（Q-D）；负锚 `zero_timeout_refused`
- [x] 4.4 拒收落 feedback 事实族：拒收落链（原通知引用+原因），跨重启可见、可重试；销账面补齐（拒收即明确消费结果，非欠账） —— 验证：`go test ./tests/integration/host -run '^TestQuiesceRejectedCutAllowed$' -count=1`（Q-A）；负锚 `rejection_visible_after_restart`
- [x] 4.5 概念拆除（D8 清单）：删 Shutdown 全套、内存义务账判据面、双排空、假 TaskID、2ms 轮询、`answered` 内存位、`- tasks: N open` 提示词行；伴生引用降级回卷；concept_budget 两条红清偿（S4 `UnsettledSource` canonical example 新建、S1 接口收敛到 3 方法） —— 验证：`go build ./...`；`go test ./assembly ./runtime/agent ./runtime/task -count=1` 全绿；`grep -c "shutdownPollInterval\|agentShutdownPollInterval\|settlePass\|settleOwed" --include='*.go' -r .` 零残留；`python3 scripts/gates/check_concept_budget.py` exit 0
- [x] 4.6 契约与文档同步（D10）：spec 条款 MODIFIED、wiki §三重写、state-inventory 补裁决、`polls` 断言改语义断言 —— 验证：`openspec validate --specs --strict` 过；`grep -c "shutdownPollInterval" docs/wiki/ openspec/specs/` 零残留或历史语境；契约测试全绿
- [x] 4.7 症状锚双证据：Q-A–Q-D 红转绿四组凭据（回滚必红、落地必绿）入 run notes —— 验证：四锚 `-count=3` 绿 + 回滚态红证据

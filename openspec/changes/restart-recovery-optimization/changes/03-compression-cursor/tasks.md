# 03-compression-cursor 孙任务

- [x] 3.1 票据事实类型与确定性载荷 —— 验证：go test ./api/event -run '^TestRecoveryTicketDeterministic$' -count=3 PASS
- [x] 3.2 两步入口 + 恢复完成点写唯一一份快照（覆盖压缩时写旧案） —— 验证：go test ./assembly -run '^TestBootFilesRecoveryTicket$' -count=1 PASS（含快照键恰一份断言）
- [x] 3.3 恢复加载 + 完整性校验 + 增量补放 —— 验证：正锚 go test ./runtime/state -run '^TestFullReplayEqualsDumpPlusDelta$' -count=1 PASS（八表逐面比对）
- [x] 3.4 失败退回全量（负锚，含同长度错误 digest、schema-1 拒绝） —— 验证：go test ./runtime/state -run '^TestRecoveryTicketFallbacks$' -count=1 六子用例全 PASS
- [x] 3.5 消费者锚 —— 验证：event-short runner verdict=match（run f9ca2484）；阶段 3 拷问闭环见 r14–r19
- [x] 3.6 并发/幂等/不重投硬化 —— 验证：go test -race ./runtime/state -run 'TestSnapshotBesideConcurrentCommits|TestConsumptionAfterSnapshotNotReOwed|TestRecommitAfterTicketRestoreIsIdempotent' -count=2 PASS

## verify 三锚（随任务行冻结，R29）

正锚：全量≡快照+增量逐位一致（八表逐面）；负锚：四类具名失败 + digest/schema 两类补充；消费者锚：真实 Boot 路径与 event-short runner。

# 03-compression-cursor 孙任务

- [x] 3.1 票据事实类型与确定性 JSON 载荷 —— 验证：`go test ./api/event -run '^TestRecoveryTicketDeterministic$' -count=3` PASS（同状态两次字节相同）
- [ ] 3.2 压缩时写 blob + 票据 + Sync 屏障 —— 验证：`go test ./assembly -run '^TestCompressionFilesTicket$' -count=1 -v` PASS
- [x] 3.3 恢复加载 + 完整性校验 + 增量补放 —— 验证：正锚 `go test ./runtime/state -run '^TestFullReplayEqualsDumpPlusDelta$' -count=1 -v` PASS
- [x] 3.4 四类失败退回全量（负锚） —— 验证：`go test ./runtime/state -run '^TestRecoveryTicketFallbacks$' -count=1 -v` 四子用例全 PASS
- [ ] 3.5 真实 Boot 120 重启数据准确性对账（消费者锚） —— 验证：现有 positive/negative/consumer 全绿 + 新计时产物

## verify 三锚（随任务行冻结，R29）

正锚：全量≡dump+增量逐位一致；负锚：四类具名失败路径；消费者锚：真实 Boot() 路径而非组件测试。

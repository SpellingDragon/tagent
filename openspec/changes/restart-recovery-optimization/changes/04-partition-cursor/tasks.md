# 04-partition-cursor 孙任务

- [ ] 4.1 位图与游标写入（与事件同批原子） —— 验证：`go test ./memory/ledger -run '^TestActivePartitionBitmapWrite$' -count=1 -v` PASS
- [ ] 4.2 Init 位图点读 + 并发游标 —— 验证：`go test ./memory/ledger -run '^TestInitCursorPointReads$' -count=1 -v` PASS（点读次数具名）
- [ ] 4.3 无游标等价回退（负锚） —— 验证：`go test ./memory/ledger -run '^TestInitCursorMissing$' -count=1 -v` PASS，结果与全扫描逐位一致

## verify 三锚（随任务行冻结，R29）

正锚：spec scenarios 具名；负锚：无游标/位图缺失回退等价；消费者锚：真实 Init 路径点读次数可观测。

# 02-timing-evidence 孙任务

- [ ] 2.1 load() 五次 Scan 分段计数（键数/字节/耗时） —— 验证：具名单测 `go test ./runtime/state -run '^TestLoadScanCounts$' -count=1 -v` PASS
- [ ] 2.2 restoreChains fold 分段计数（链数/条数/耗时） —— 验证：`go test ./assembly -run '^TestRestoreFoldCounts$' -count=1 -v` PASS
- [ ] 2.3 观测写入 CI 可上传产物 —— 验证：本地 runner 跑一次 `--scope event-short --out /tmp/x`，产物含 5 段计数
- [ ] 2.4 症状锚 A 基线红证（当前 boot 曲线随代数增长） —— 验证：本地 120 重启报告 boot_ms 首尾比 > 10 入 run notes

## verify 三锚（随任务行冻结，R29）

正锚：2.1/2.2 具名测试名与 -run 正则逐字符一致；负锚：计数缺失或全零即红；消费者锚：runner 产物真实含计数（非组件测试自证）。

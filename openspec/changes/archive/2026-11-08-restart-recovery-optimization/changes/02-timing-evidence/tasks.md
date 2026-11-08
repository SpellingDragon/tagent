# 02-timing-evidence 孙任务

- [x] 2.1 load() 五次 Scan 分段计数（键数/字节/耗时） —— 验证：具名单测 `go test ./runtime/state -run '^TestLoadScanCounts$' -count=1 -v` PASS
- [x] 2.2 restoreChains fold 分段计数（链数/条数/耗时） —— 验证：`go test ./assembly -run '^TestRestoreFoldCounts$' -count=1 -v` PASS
- [x] 2.3 观测写入 CI 可上传产物（部分达成，缺口移交 5.1a）：进程内 boot 的五行计数已随 go test stdout 进入 runner test-log.jsonl（冻结命令实测 10 行：两段 boot 各五行；event-short 场景 TestEventIntegrityReplacement 的 test-log 另有 35 行）；**缺口**——带真实数据的重启计数困在长跑子进程日志（`$TMPDIR/event-consumption-integrity/**/logs/genNN.log`），CI 仅上传 runner --out 树，且 runner 对全零/缺段计数不判红 —— 验证：`TAGENT_LOG_LEVEL=info go test ./assembly -run '^TestDanglingTurnAfterDualFailure$' -count=1 -v 2>&1 | grep -c 'durable load scan'` == 10（两段 boot 各五行）；缺口以 05 域 5.1a 显式承接
- [x] 2.4 症状锚 A 基线红证（当前 boot 曲线随代数增长） —— 验证：本地 120 重启报告 boot_ms 首尾比 > 10 入 run notes

## verify 三锚（随任务行冻结，R29）

正锚：2.1/2.2 具名测试名与 -run 正则逐字符一致；负锚：计数缺失或全零即红；消费者锚：runner 产物真实含计数（非组件测试自证）。

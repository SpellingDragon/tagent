# 05-ci-orchestration 孙任务

- [x] 5.1a 重启计数证据进 CI 产物（自 2.3 移交）：CI 上传树纳入长跑子进程日志目录（`$TMPDIR/event-consumption-integrity/**/logs/`），runner 对选中场景的 durable load scan 计数缺段或全零判红（红条件按 spec=五行缺段或全零；非零预期限定实际承载数据的前缀 spec/consume/fact，attempt/reject 合法为零，避免误红） —— 验证：CI run 产物含带真实数据的 genNN.log，其 spec/consume/fact 三行 keys>0。**本地已按 CI 同参（seed 1/window 32768）复跑取证：43 代真实 genNN.log 共 215 条 scan 行，`durable_scan_attestation` → state=attested，spec=30 / consume=30 / fact=60 键（reject 合法为 0）；修复前同一路径 44 代日志恒 130 B 且 0 条 scan 行（/tmp/hupc-M/07 留档）**，产物见 /tmp/hupc-M2/attestation-partial.json；日志出口由 03.11 新增的 restart-evidence 子测试守（缺失即红）。CI 上传树上的最终对账随 push 由 5.4 取证
- [x] 5.1 分片并发度收敛 + 超时按校准重冻 —— 验证：`python3 -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'` 全绿
- [x] 5.2 具名 liveness 替代 15m 就绪门禁 —— 验证：`go -C examples/wechat-bot test -tags smoke -run '^TestLocalSmoke$' -count=1 .` 绿（任务行原写法在根模块跑必 setup failed：examples/wechat-bot 是独立模块；等价正解按模块目录跑，实测 ok 184.254s / exit 0，日志 /tmp/hupc-M/07/05.2-local-smoke.log）
- [x] 5.3 runner 超时前落证据（负锚） —— 验证：注入超时，产物含 test-log/evidence
- [ ] 5.4 症状锚 B：CI 全 6 分片绿 —— 验证：CI run conclusion==success 且 6 分片 verdict=match

## verify 三锚（随任务行冻结，R29）

正锚：F0 selftests 与本地冒烟；负锚：挂死仍出证据；消费者锚：CI 真实全 6 分片 + 数据准确性对账绿。

# 05-ci-orchestration 孙任务

- [ ] 5.1 分片并发度收敛 + 超时按校准重冻 —— 验证：`python3 -m unittest discover -s scripts/gates -p 'test_check_runtime_*.py'` 全绿
- [ ] 5.2 具名 liveness 替代 15m 就绪门禁 —— 验证：`go test ./examples/wechat-bot -tags smoke -run '^TestLocalSmoke' -count=1` 绿
- [ ] 5.3 runner 超时前落证据（负锚） —— 验证：注入超时，产物含 test-log/evidence
- [ ] 5.4 症状锚 B：CI 全 6 分片绿 —— 验证：CI run conclusion==success 且 6 分片 verdict=match

## verify 三锚（随任务行冻结，R29）

正锚：F0 selftests 与本地冒烟；负锚：挂死仍出证据；消费者锚：CI 真实全 6 分片 + 数据准确性对账绿。

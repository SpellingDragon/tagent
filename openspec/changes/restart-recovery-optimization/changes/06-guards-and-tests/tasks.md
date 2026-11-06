# 06-guards-and-tests 孙任务

- [ ] 6.1 F0 边界规则 + 正反样例自测 —— 验证：`python3 -m unittest discover -s scripts/gates -p 'test_check_durable_truth_registry.py'` 全绿
- [ ] 6.2 R3 登记新引用面 —— 验证：`python3 scripts/gates/check_durable_truth_registry.py` ok
- [ ] 6.3 重写测试：正/负/消费者/症状锚 —— 验证：全模块 go test + 全量门 exit 0
- [ ] 6.4 R35 独立拷问放行 —— 验证：末轮显式 frontier 空 + 扫描范围 + 逐项证据

## verify 三锚（随任务行冻结，R29）

正锚：F0 selftests；负锚：越界持久面即红；消费者锚：全量门真实复跑。

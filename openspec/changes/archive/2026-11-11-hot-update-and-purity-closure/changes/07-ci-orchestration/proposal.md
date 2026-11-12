# 05-ci-orchestration 提案

## Why

六分片并发抢慢盘把恢复成本放大 60 倍；15 分钟就绪窗是误判门禁；超时数值未按校准重冻。

## What Changes

分片并发度与核数绑定或串行；按 03.10 校准重冻超时；去掉 15m 就绪判失败，保留具名 liveness（子进程退出/心跳+日志尾）；判定只看数据准确性对账。

## 边界与依赖

依赖：02/03/04；被依赖方：06；接口面：.github/workflows/ci.yml、examples/wechat-bot/smoke_harness_test.go、scripts/gates/test_check_runtime_workflow.py、scripts/scenarios/run_runtime_scenarios.py；禁止事项：MUST NOT 缩水 1200/120 验收面、MUST NOT 删除数据准确性断言。

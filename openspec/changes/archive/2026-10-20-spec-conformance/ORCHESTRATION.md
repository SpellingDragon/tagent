# ORCHESTRATION：spec-conformance

## 结论层
唯一路径：W1-W2 并发 → W3-W4 并发 → W5 → W6 → F。最大风险＝W1 上下文语义主干（三龄段＋环境块＝规格核心，量最大）；主循环保持现状（裁定）不动架构。

## 分组层·编排四问
**次序**：W1（memory/replay＋run_request＋run_fact）与 W2（runtime/task＋runtime/agent/loop Backflow）触碰集不相交＝并发；W3（api/plugin＋evolution/meditation）与 W4（tool/*＋examples/wechat-bot）并发；W5-W6 后置。本轮做不了：R9 主循环复用（裁定关闭）。
**授权**：每单白名单=本域包＋tests＋docs/api＋doc_budget；golden/scripts/共享登记=编排者单点；新 golden 场景（S13-S15）=W6 申报执行（EXPECTED_SCENARIOS 同步）。
**凭据**：每叶三点锚（正/负/消费者）逐条 exit；波后独立验证子代理（R30）；golden match 为硬门。
**释放**：无资源锁。

## 明细层
| 单 | 域 | 触碰集 |
|---|---|---|
| W1 | context-assembly | memory/replay、run_request.go、run_fact.go、run.go、tests/integration/{history,host} |
| W2 | task-lifecycle | runtime/task、runtime/agent/loop.go（Backflow）、tests/integration/{state,execution} |
| W3 | plugin-phases | api/plugin、evolution/meditation、tests |
| W4 | sample-features | tool/{subagent,recall,knowledge}、examples/wechat-bot、tests/integration/capabilities |
| W5 | observability | api/model、runtime/agent、evolution、memory、runtime/state、telemetry |
| W6 | conformance-golden | golden、scripts/check_runtime_evidence.py、api/plugin、runtime/agent、tests/integration/acceptance |

## 台账层
- 2026-10-20 计划建立；W1/W2 并发派发。

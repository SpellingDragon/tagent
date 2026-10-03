# Proposal: 回滚在途用例的告警轮劫持（测试归因修复）

## Why

`TestRollbackOfHotAddNumericWithInFlightTurn`（`org_candidate_test.go`）在 `GOMAXPROCS=1` 下 5/8 失败（20.04s 预算烧尽）。诊断证明这是**测试编排缺陷**，产品行为逐条合规（告警轮、批合并、同代执行均为 `execution-generations.md` 明文契约）。失败机制：hot-add 成功会发一条 `[system-alert]`（`tagent.go:684`），它与紧随注入的 `"in-flight"` 在 P=1 下极易被 `Pull` 排干合为**一批**、拼成**一个 turn**——`in-flight` 与告警轮同生共死，断言①所等的下一次 serve 永远不来（其唯一供给源 `"after"` 注入排在断言①之后）。

更糟的是**通过形态同样不测真场景**：告警轮抢先单独成批时，`"B parked mid-call"` 的存在性谓词被告警轮的历史 record 立即满足，`Rollback` 发生在 in-flight 轮之前、gate 从未真正拦住任何调用——用例声称钉住的「进行中结构绑定不被回滚迁移」从头到尾没有发生。通过与否由一个纯调度事件决定，两种形态都违反已写下的验收纪律（`config-hot-reload` spec「断言的满足 MUST 只能由被测动作产生」、`execution-generations.md` 对告警轮的验收须知）。

## What Changes

- 仅改该用例的编排与断言锚定，及 delegModel 的 park 观测（测试基建，产品零改动）：`"parked"` 谓词改为 delegModel 的 **park 直接观测**（`parkedNow("SUB-B") >= 1`——armGate 后被消费的任何轮的 B 调用必然 park，谓词必可达且零时序推理；弃排空与基线计数，两者均被实现期证伪，见 design D1/D1'）；断言①改锚 **MAIN 完成事件增量**（`ToolResults` 含 `served:SUB-B`，串行 loop 保证归属被 park 的 turn）——由**完成**而非**进入**满足，保持「回滚若拆掉在途调用即红」的敏感度；断言②维持计数增量（活性语义）。
- `config-hot-reload` 的回滚需求补一个 scenario：回滚在途验收 MUST NOT 被告警轮劫持或替达标。

## Impact

- Affected specs: `config-hot-reload`（MODIFIED：数值与结构共享完整有效配置回滚记录，+1 scenario）
- Affected code: `org_candidate_test.go`（用例编排）+ `delegation_test.go`（delegModel 附加 park 观测，约 15 行）；产品零改动

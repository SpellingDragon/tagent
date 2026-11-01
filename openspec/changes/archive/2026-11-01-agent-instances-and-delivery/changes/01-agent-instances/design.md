# 01 技术要点

## 要点

1. **实例泛化**：`Consumers` 从"入口专属"改"每地址一个"——`Ensure` 在地址首次活动时挂实例（显式：地址来自调用帧/投递目标，不猜测归属），实例生命周期随 run 起停、Close 统一回收。
2. **直驱退役**：`Host.Invoke` 的 `invokeConsumed`/`lockAddressContext` 双分流收敛为单路径（常驻实例）；`driveLocked`/`gateOf` 单槽 gate 删除（清除式，伴生引用回卷）。
3. **委派管线化**：`subagent` serve 由"进程内直跑一轮→completeDelegation"改为"向 sub 实例投递输入（`TypeExternalInput`，来源=delegation）→ sub 实例常驻循环执行 → 结果以事件回流 → 既有 settle/Notice 链不变"。task 受理回执（`ok`）语义不变。
4. **红线守护**：settle 恰好一次（`settle_wake_single_drive_test`）、会话占位（`occupancy.ErrSlotHeld`）、白名单 fail-closed 三组测试每叶必跑；W0 防护用例（`TestInstancesRedlineGuard`）在语义被改动时先红。
5. **探针先行**（1.1）：I1 红锚的红因必须命中"非入口地址走同步直驱"（调用方阻塞/无实例），不得以编译失败冒充。

## 文件清单

- 改：`runtime/agent/consumer.go`（Ensure 泛化）、`assembly/host.go`（分流收敛）、`tool/subagent/tool.go`+`continuation.go`（管线化）、`runtime/agent/loop.go`（仅管线接缝，占位/恰好一次不动）。
- 测：`tests/integration/execution/`（I1 锚）、`runtime/agent/`（实例测试）、防护用例。

## 风险与回退

- 风险：直驱退役影响所有非入口 `Host.Invoke` 调用方（smoke/集成测试大量直驱）——迁移面按 04/收尾清单登记；`invokeConsumed` 的消费水位语义不得因泛化漂移。
- 回退：驱动分流是单域改动，可独立回滚；红线测试在回滚前后均须绿。

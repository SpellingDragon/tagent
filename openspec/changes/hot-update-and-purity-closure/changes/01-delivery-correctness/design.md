# Design: 01-delivery-correctness

## 技术要点

- owed 驻留：pinFact 增钉 owed 声明键（不新建账本，避 0006 形态3），使 owed 活动集不随窗口 trim；boot 从链重建 owed 全集。
- retry 有界：attempt 计数作为追加事实落链，重启读链续计；超界具名拒绝不再 schedule(...,0)。
- 读错具名：settlementFactOf 出错返回具名 error 而非 false；eachDeliveryFact 耐久读错 surfaced 而非 WARN 部分视图。
- 删 legacy 只写字段（审计已在 meta）。

## 文件清单

assembly/host_delivery.go、assembly/host_tools.go、runtime/agent/loop.go、assembly/host_delivery_test.go。

## 风险与回退

- 风险：owed 增钉增大 pin 集；以 1.8 冷读基准钉住不劣化，不引入独立账本。
- 回退：单域回滚；owed 驻留改动不破坏既有 sent 销账语义（sent 不重发保持）。
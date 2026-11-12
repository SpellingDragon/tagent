# Proposal: 01-delivery-correctness

父变更：hot-update-and-purity-closure（合并转移自 restart-recovery-optimization / runtime-simplicity-upgrade / event-driven-consumption）

## Why

owed 超窗漏投已由 restart-recovery E3(e5cb2663) 修复（锚 B 基线绿）；本域重 scopes 到其未修残差：retry attempt 不落链 → 有界重试跨重启变无界（E7 open）；settlementFactOf 读错→false→重发、eachDeliveryFact 耐久读错→静默降级 WARN，把至少一次窗口从崩溃扩到任意读错误。

## What Changes

owed 活动集驻留不被 trim；retry 计数落链跨重启续计有界；交付读错具名上报不降级不重发；删 deliverySettlement.legacy 只写字段。

## 边界与依赖

- 依赖：无（接口常数：结算事实族键=身份+终态，沿用）。
- 被依赖方：无。
- 接口面：host_delivery.go / host_tools.go / runtime/agent/loop.go（owed 与 retry）。
- 禁止事项：不改 read.go/get.go/localfile.go（02 域）；不改 run.go 装配缝（编排者）；不新增独立"已发"写权。
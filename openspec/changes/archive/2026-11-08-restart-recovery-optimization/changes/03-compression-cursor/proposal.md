# 03-compression-cursor 提案

## Why

重启恢复每次从创世第一条重放，成本随累计历史线性增长；需要一个在恢复完成点可装载的内存状态快照。

## What Changes

「重启」是一个事件，恢复就是它的消费逻辑两步：①装载未消费的事件；②装载（票据快照）或重放内存状态。两步完成才开放服务。完成点把内存状态序列化为唯一一份可覆盖的快照 blob，并落一条链上票据事实 recovery.ticket（族 12）；票据带 digest/length/cursor，blob 按 digest 校验，损坏或版本不认识退回全量事件流。

## 边界与依赖

依赖：02 的计时证据；被依赖方：04/05；接口面：api/event/recovery_ticket.go、assembly/run.go、assembly/host.go、assembly/host_delivery.go、runtime/state/recovery.go、golden/runtime/durable_truth.json；禁止事项：MUST NOT 引入链外可写第二真相、MUST NOT 引入 unsafe/mmap、MUST NOT 在恢复完成前开放服务或写票。

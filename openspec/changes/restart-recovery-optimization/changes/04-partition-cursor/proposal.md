# 04-partition-cursor 提案

## Why

startup-active-partition-bitmap 预案早已写明「全量扫描成为瓶颈时可复活」，本次正是该前提成立。

## What Changes

实现 global:active_partitions 位图 + per-partition cursor，Init 从全前缀扫描变为位图 + 游标点读，保留等价回退。

## 边界与依赖

依赖：03 稳定；被依赖方：05；接口面：memory/ledger/segment_store.go、openspec/specs/startup-active-partition-bitmap/spec.md、golden/runtime/durable_truth.json；禁止事项：MUST NOT 改变事实语义与 KV 结果顺序。

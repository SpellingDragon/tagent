# 03-compression-cursor 提案

## Why

重启恢复每次从创世第一条重放，成本随累计历史线性增长；压缩本已产出链上一等事实，可作恢复游标。

## What Changes

压缩时把恢复态序列化为链上票据事实（compress.product 族载荷扩展，JSON 确定性字节），大块状态存外部只读 blob；恢复加载并校验，失败退回全量。

## 边界与依赖

依赖：02 的计时证据；被依赖方：04/05；接口面：api/event/compress_product.go、assembly/compress_consumer.go、memory/replay/age_plan.go、runtime/state/durable.go、golden/runtime/durable_truth.json；禁止事项：MUST NOT 引入链外可写第二真相、MUST NOT 引入 unsafe/mmap。

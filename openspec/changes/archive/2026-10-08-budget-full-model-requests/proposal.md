# O3 请求预算与摘要等待优化

## Why

现有投影估值先于system及任务板装配，工具参数/schema和多模态开销也未完整表达。同步摘要在BeforeModel关键路径内；应完善计量与局部等待边界，而不是让provider再隐式裁剪或直接改异步。

## What Changes
- 共享本次请求声明快照，完整列出输入成本及未知项。
- 固定开销从既有输入预算中扣除，由原ContextCompressor唯一压缩。
- 同步摘要使用总时限及已有工程降级，保留事实链折叠顺序。

## Capabilities
- 新增request-budget-accounting；不更改FP/SRC/FILE边界、provider权限或事件TTL。

## 边界与依赖
- 父docs-objective-review，D14-S1为与O5共享的接口常数；O3.2提供实现，O3.6提供完整接线。
- 被依赖方O5.2/O5.5；可先以S1 fixture开发，不把fixture当完成。
- 独占agent/compress、modelutil；agent/context_manager、execution_gate_model、config、root/tests由编排者单写。
- 禁止：第二压缩器、provider隐藏裁剪、全局声明缓存、异步旧摘要补写、把heuristic说成精确token数。

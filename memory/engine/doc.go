// Package engine 提供记忆引擎的两种实现与其接线方式：进程内混合检索引擎（关键词 ∪
// 向量，向量另存持久 KV 并在启动时异步重建）、store 装饰器 engineBridge（一处包裹覆盖
// 全部写入路径），以及读实时状态而非平行计数器的健康度诊断。
//
// 契约: docs/wiki/memory/memory-architecture.md#engine-overview
package engine

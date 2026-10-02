// Package embedder 承载 Embedder 契约的实现（mock／zhipu HTTP 供应商／traced 装饰器）。
// 分包原则与契约居核心包的 memory.Embedder 一致：本包只依赖核心 memory 包的接口与数据类型，
// 消费方无需认识具体供应商；新增供应商的接线点在组合根。已裁决：嵌入走 tagent 侧 HTTP 供应商，
// 不用 rustviking CLI（其向量索引为进程内易失）。
//
// 契约: docs/wiki/memory/memory-architecture.md#embedder
package embedder

// Package plugin 把记忆写入挂到框架的事件管线上：MemoryPlugin 负责筛选、持久化并在
// 同一同步点投影，SummaryPlugin 负责事件类型与元数据标注，Attribution 与
// EchoCredential 经 ctx 提供回合级归因与精确回显识别。
//
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
package plugin

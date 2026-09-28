// Package event 定义 tagent 的统一事件类型、事件元数据契约与时间线前缀契约：
// 类型注册表是事件类型静态属性的唯一权威源，投影/召回/嵌入/TTL 均由它派生。
//
// 契约: docs/wiki/event/event-architecture.md#overview
package event

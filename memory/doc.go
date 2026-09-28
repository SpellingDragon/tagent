// Package memory 是 tagent 的事实存储层：以 FullEvent 为唯一记录形态、按分区命名空间
// 隔离、以键格式为单点约定，并在此之上提供检索（关键词／语义）、分层与压实、TTL 与
// 物理遗忘。后端抽象（KV 底座、检索引擎、嵌入器）都遵循"契约居核心包、实现居子包"。
// 事件类型常量的单点定义在事件包（event.Type*），本包不重复登记。
//
// 契约: docs/wiki/memory/memory-architecture.md#overview
package memory

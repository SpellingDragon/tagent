// Package agent 是 tagent 的事件驱动引擎核心：50 个文件按职责分五组（事件循环、上下文管理、
// 子 Agent、冥想、可选注入），子域已独立成包。
//
// - 子包各有独立篇：task/ 任务生命周期、compress/ 压缩域、governance/ 治理闸、reliability/ 退化追踪。
// - 文件与分组的逐条职责见文档清单，本注释不复制它。
// 契约: docs/wiki/agent/agent-architecture.md#package-layout
package agent

// Package compress 负责回合上下文的装配与压缩：只决定保留哪些内容与如何折叠，
// 不决定回合的重试与执行面（那在 agent 与 reliability 包）；所有阈值是命名常量，以免与热参源产生第二处真值。
//
// - ContextCompressor／SmartCompressor：两阶段压缩——段级判定丢弃低价值消息，预算仍不足时折叠出综述卡片。
// - SessionProjection：把事实链的事件引用折叠成本会话视图，Append 幂等，重建时按当前引用整表重算。
// - task_segmenter：按任务边界切分消息，使压缩不会把一段工作切成半截。
// - token_counter：字符近似计量与事件类型到角色的映射（映射的唯一权威源在 event 包）。
// - compaction_event／BuildCompactionPayload：把折叠产物作为一等事实落链，载荷携带重建状态，可召回正文即叙事本身。
// - telemetry：可见性票据与处置计数，供宿主与运维判断压缩是否被消费。
// 契约: docs/wiki/agent/compression-and-telemetry.md
package compress

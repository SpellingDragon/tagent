// Package prompt 装配 agent 的系统提示词：从磁盘文件与目录读取、按固定顺序拼接，
// 并在磁盘未命中时回退到内嵌默认值。
//
// 提示词文件是唯一真源，改动经热重载即时生效，无需重启进程。
//
// 契约: docs/wiki/prompt/prompt-architecture.md#file-layout
package prompt

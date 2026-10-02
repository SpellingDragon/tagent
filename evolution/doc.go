// Package evolution 实现 agent 的自我改进通道：改动默认即生效，本包负责留痕（git
// 原生改进 commit）、后验评估（canary 证据 ＋ 确定性指标闸 ＋ LLM 评审双触发）与
// 安全回滚（仅回滚带改进标记的提交，劣化只出建议不自动动手）。
//
// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
package evolution

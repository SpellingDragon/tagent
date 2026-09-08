// Package agent 是 tagent 的事件驱动引擎核心。50 个文件按职责分五组：
//
// # 事件循环组(引擎主干)
//
//   - agent.go: TagentAgent 聚合根与 AgentConfig;event_loop.go: runEventLoop 主循环
//     (Pull 批处理/退避重试/降级 backoff);event_bus.go: EventBus+AgentEvent+ReliableBus
//     磁盘溢出;inject.go: InjectMessageWithSource 渗透入口
//
// # 上下文管理组(LLM 视图)
//
//   - context_manager.go: 粘合层(投影/持久化/settle 反馈/bundle 章盖章);
//     output_overflow.go: outputCh 宽限+溢出票据;helpers.go/lifecycle.go: 辅助与生命周期
//
// # 子 Agent 组
//
//   - tool_agent.go(950L 最大): AgentToolWrapper(本地/A2A 统一封装/重入/交接);
//     a2a.go: 远程协议
//
// # 冥想组
//
//   - meditation.go: 门控触发(novelty+idle);meditation_digest.go: digest 组装
//
// # 可选注入组(经 TagentAgent setter)
//
//   - degradation.go/reliability 注入;governance 经 govGate;evolution 经 root
//
// 子域独立成包:task/(任务生命周期)、compress/(压缩域)、governance/(治理闸)、
// reliability/(退化追踪)——各自有独立 wiki 篇。
package agent

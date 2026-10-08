# Proposal: externalize-meditation-session

## Why

今天的冥想是**入口 agent loop 内的自体维护者**：MeditationManager 以 idle+novelty 双门在同一个 agent 的 mailbox 里自注入，LLM 在自己的 turn 里清理**自己的投影**、巩固**自己的分区**。这带来两个结构性限制：

1. **跨域视角缺失**：多 agent/多 session 并存时（recall、工具 agent、乃至未来的多入口），没有一个角色能看到"整个系统最近发生了什么"并做跨域巩固——每个 agent 只在自己的空闲期回看自己。
2. **判定是拉式的**：巩固候选（容量压力 hint）只在冥想触发时被拼进 digest，存储层的压力信号无法主动唤醒巩固。

本变更换一个不越权的答案：**外部化触发与判定，保留执行权在目标**。一个独立冥想 agent（同构 tagent，非新机制）经授权读共享事实链做跨域策展；巩固判定从拉式升级为事件；跨 session 回写走一条窄投递缝——事件做信号、不做执行权。

## What Changes

1. **冥想 agent 化**（meditation-agent-partition）：冥想可以作为独立配置的 agent 存在（如 recall agent 先例），经 `read_namespaces` 授权读被观察分区；其 novelty 门升级为**跨分区谱系判据**——novelty = 被观察分区内"非自管谱系"的新事件（复用 event 包 lineage 单源派生），从结构上封死"A 的反思成为 B 的新鲜度"的跨 agent 永动回路。
2. **巩固判定事件化**（consolidation-request-events）：既有容量 hint（拉式 digest 附加段）升级为推式——存储层压力达阈发出巩固建议事件唤醒巩固 turn；谱系为自管（不 re-arm novelty）；契约不变：**触发只是建议，执行权在 LLM+工具**。
3. **跨 session 投递缝**（cross-session-delivery）：进程内跨 agent 的窄投递面——投递前目标白名单校验；目标 loop 未运行**具名拒绝**（fail-loud，投递方自决重试/退避，不静默丢、不绑 reliability 开关、不走 one-shot 回退）；投递消息在目标侧以 `meditation` 谱系注入（既有 lineage 白名单：可投递宿主、遥测计自管、不 re-arm 目标 novelty）。

## Capabilities

| Capability | 一句话边界 |
|---|---|
| meditation-agent-partition | 冥想 agent 经授权读跨分区；novelty=非自管谱系新事件；锚点/空闲门沿用 |
| consolidation-request-events | 容量压力→巩固建议事件（自管谱系、防抖、建议式）；拉式 digest 保留为回退 |
| cross-session-delivery | 进程内跨 agent 投递：白名单+具名拒绝+meditation 谱系收口在既有注入入口 |

## 不做什么（边界与依赖）

- **不做跨进程/跨机器投递**：范围=同进程内多 agent（org 形态）；跨进程留给既有 HTTPAPI 面，不另造总线。
- **不转移压缩权**：外部冥想**不写 compaction 事件**（单压缩权是 O3/spec 既钉死的不可变量）；它只写普通事件（经验卡片/综述），经目标自然折叠间接塑造上下文。
- **不做第二消息总线/调度器**：投递=寻址+授权，目标侧收口在既有 `InjectMessageWithSource` 单入口；冥想 agent 是同构 tagent，触发仍是事件驱动。
- **默认关**：全部能力挂 meditation 配置族，关闭态零行为变化（仓规）。

## 依赖

- 复用：`event` 包 lineage 单源（DeliverableLineage/SelfManagedLineage）、`memory.QueryEvents` 只读面、`read_namespaces` 授权模型、MeditationManager 锚点/门控骨架、`InjectMessageWithSource` 注入入口。
- 需核实并可能补齐：`trigger_source` 谱系在 FullEvent.Metadata 的持久化现状（跨分区 novelty 判据的数据源）——列为 W0 探针。
- 继承契约：event-sourced-projection（投影纯回放不可变）、persistent-event-loop（单消费点/StopLoop 终结态）、architecture-guardrails（分层方向/唯一发布权）零改动。

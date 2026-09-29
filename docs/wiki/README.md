# tagent Wiki 索引

模块级架构文档，与代码同仓演进。**每篇经过逐断言代码校对**（断言与结构体/签名/常量逐一对照），并以「已知缺口与演进方向」章主动声明尚未闭合的环。

## 文档地图

| 模块 | 文档 | 一句话 |
|------|------|--------|
| agent 引擎 | [agent/agent-architecture.md](agent/agent-architecture.md) | 事件驱动引擎：EventBus / runEventLoop / ContextManager / 冥想 / 子 Agent 封装 |
| 事件流 | [agent/event-flow.md](agent/event-flow.md) | 一条消息从注入到回复的完整旅程 |
| 记忆存储 | [memory/memory-architecture.md](memory/memory-architecture.md) | FullEvent/EventReference、分层存储（L0-L3）、因果链、墓碑、记忆策展 |
| 事件契约 | [event/event-architecture.md](event/event-architecture.md) | 事件类型系统、元数据契约、时间线前缀（读写单点） |
| 插件 | [plugin/plugin-architecture.md](plugin/plugin-architecture.md) | MemoryPlugin（持久化+因果+同点投影）、SummaryPlugin（元数据标注） |
| 工具 | [tool/tool-architecture.md](tool/tool-architecture.md) | ActionTool（tmux+任务层+跨重启连续）、召回体系、任务工具族、EventKeys 传递 |
| tmux 动作会话存活判定 | [tool/tmux-action.md](tool/tmux-action.md) |
| 持久投递与依赖退化 | [reliability/durable-delivery.md](reliability/durable-delivery.md) |
| 上下文压缩与自身遥测 | [agent/compression-and-telemetry.md](agent/compression-and-telemetry.md) |
| 治理分级处置与批准通道 | [agent/governance-enforcement.md](agent/governance-enforcement.md) |
| 任务层生命周期与回收 | [agent/task-lifecycle.md](agent/task-lifecycle.md) |
| 执行器代际与生命周期收敛 | [agent/execution-generations.md](agent/execution-generations.md) |
| 自我改进 | [evolution/evolution-architecture.md](evolution/evolution-architecture.md) | refine 通道、git 原语安全闸、后验评估窗口与双回滚触发 |
| RL 侧接口 | [rl/rl-architecture.md](rl/rl-architecture.md) | 端点allowlist 与逐跳重定向（SSRF）防线、配置面与升级收紧 |
| 评估套件 | [platform/evaluation-suites.md](platform/evaluation-suites.md) | 票据零幻觉召回、畸形票据显式拒绝、工具面白名单、handoff 契约存在性 |
| Prompt | [prompt/prompt-architecture.md](prompt/prompt-architecture.md) | Loader / bootstrap / 内嵌回退 / 热重载 Source |
| 平台子系统 | [platform/platform-subsystems.md](platform/platform-subsystems.md) | 治理闸 · 自进化(git 原生) · 常驻可靠性 · 配置热重载(候选事务/执行代) · 统一可观测 · 记忆引擎(解耦缝) · MCP 闭环（默认关闭项全部 opt-in） |
| 转世通报（换装后首轮自我告知） | [platform/reincarnation-notice.md](platform/reincarnation-notice.md) |
| 组织级热重载（换代、应用记录与无锁读面） | [platform/org-hot-reload.md](platform/org-hot-reload.md) |
| 运行时资源所有权（租约、代际与封路） | [platform/resource-ownership.md](platform/resource-ownership.md) |
| agent 行为矩阵 | [platform/agent-behavior-matrix.md](platform/agent-behavior-matrix.md) | 启用上述子系统后，agent 在治理/自进化/可靠性/语义召回/可观测/部署各复杂场景下的**实际反应**（逐条溯源到代码） |

## 撰写约定（新增或修订时遵循）

> 注释与文档的分工、以及代码内 `// 契约:` 索引的写法，以 `openspec/specs/code-documentation` 与
> `openspec/specs/architecture-guardrails` 为准；本目录只承载长期事实，不记录迭代过程。


> 注释与文档的分工、以及代码内 `// 契约:` 索引的写法，以 `openspec/specs/code-documentation` 与
> `openspec/specs/architecture-guardrails` 为准；本目录只承载长期事实，不记录迭代过程。


**标准章节骨架**（各篇按此顺序组织，机制章节数量自定）：

1. `## 一、模块定位` — 一句话定位 + 核心职责 + 设计原则
2. `## 二、文件清单` — 文件→职责表（**不列行数**，必然腐化）
3. `## 三、组件关系总览图` — mermaid
4. 中间章节 — 核心数据结构 → 各机制详解 → 与其他模块的关系 → 关键设计决策（"为什么"集中在此）
5. `## 已知缺口与演进方向`（末章，必备）— 以工程事实陈述缺口：**现状与防线 + 候选方向**，不粉饰、不承诺排期

**行文纪律**：

- 图优先 **mermaid**（graph/sequenceDiagram）；比特位域等 mermaid 无对应图型的场景用代码块 + 配套表格
- 代码块引用只标**文件名**，不标行号（行号必然腐化）
- 可验证断言（结构体字段/函数签名/常量值/默认值）修订时须与代码对照；历史机制被替代时**删除死代码留存**，最多保留一行历史注记
- 内部术语首次出现处给白话解释；面向首次读者的表述规范见 README 修订原则（特性先行、少黑话）

## 与其他文档的关系

- **README**：面向首次读者——它能为我做什么（不含内部论证）
- **wiki（本目录）**：面向使用者与外部分析——机制怎么工作、边界在哪、缺口是什么
- **openspec/specs/**：面向实现者——行为契约的规格化表述（SHALL 级）
- **tests/README**：契约守护矩阵——模型↔框架文本接缝的真实 LLM 测试清单

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
| 工具 | [tool/tool-architecture.md](tool/tool-architecture.md) | ActionTool（tmux+任务层）、召回体系、任务工具族、EventKeys 传递 |
| Prompt | [prompt/prompt-architecture.md](prompt/prompt-architecture.md) | Loader / bootstrap / 内嵌回退 / 热重载 Source |

## 撰写约定（新增或修订时遵循）

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

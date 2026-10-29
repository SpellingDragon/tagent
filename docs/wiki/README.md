# docs/wiki：活契约页 + 历史档案

本目录分两类，定位不同、维护规则不同：

## 活契约页（16 篇，随代码走）

代码注释以 `// 契约: docs/wiki/…#锚` 引用这些章节，锚由 `comment_policy`/`doc-refs` 门禁钉住——**它们是现行契约，不是历史文档**。2026-10 起按当前代码做了重写式修订（原文与判据保全、承载点正名为现行实现）：

| 域 | 篇 |
|---|---|
| agent（5） | [agent-architecture](agent/agent-architecture.md) · [event-flow](agent/event-flow.md) · [execution-generations](agent/execution-generations.md) · [governance-enforcement](agent/governance-enforcement.md) · [task-lifecycle](agent/task-lifecycle.md) |
| 存储（3） | [memory-architecture](memory/memory-architecture.md) · [event-architecture](event/event-architecture.md) · [durable-delivery](reliability/durable-delivery.md) |
| 平台/插件（3） | [platform-subsystems](platform/platform-subsystems.md) · [org-hot-reload](platform/org-hot-reload.md) · [plugin-architecture](plugin/plugin-architecture.md) |
| 数据/工具/示例（5） | [rl-architecture](rl/rl-architecture.md) · [tool-architecture](tool/tool-architecture.md) · [tmux-action](tool/tmux-action.md) · [evolution-architecture](evolution/evolution-architecture.md) · [wechat-bot-runtime](examples/wechat-bot-runtime.md) |

维护规则：文档随代码走——代码变了页要跟（重写式修订，非打补丁）；锚标题（slug）不可改（代码引用钉住）；已知缺口在篇末「gaps」节如实登记。

## 历史档案（其余各篇，historical-verbatim）

快照性质：断言对**当时的实现**成立，真源是各自的快照提交；不许「对齐今天」式改写，引用由 `doc-refs` 按快照锚定保护。要了解某机制「当年为什么这样设计」，来这里；要了解「现在是什么」，去活契约页。

## 新读者入口（按序）

1. [docs/GLOSSARY.md](../GLOSSARY.md) —— 术语一行一对照
2. [docs/demos/storage-concept-map.html](../demos/storage-concept-map.html) —— 存储概念地图（12 概念按受众分层 + 一图数据流 + 每概念一句反事实）
3. [docs/demos/storage-time-travel.html](../demos/storage-time-travel.html) —— 回放装载 / 时光回退 / session fork 交互演示
4. [mechanism-tour.md](mechanism-tour.md) —— 十分钟机制导览
5. 按域下钻上方活契约页

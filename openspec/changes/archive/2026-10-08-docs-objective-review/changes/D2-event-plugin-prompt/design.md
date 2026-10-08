# Design: D2 事件插件 prompt 域评审

## 阅读顺序与技术要点

1. `event-architecture.md`：事件类型系统 → 元数据契约（FormatEventKey/ParseEventMeta）→ 时间线前缀读写单点 → EventTypeSpec 注册表。
2. `plugin-architecture.md`（1030 行，最长耐心读）：MemoryPlugin 三职责（持久化+因果+同点投影）→ SummaryPlugin → 事件入库管线。
3. `prompt-architecture.md`：Loader / bootstrap / 内嵌回退 / 热重载 Source → system_prompt.files 装配链。

## 重点问题

- 事件类型系统的规模 vs 实际消费面：类型爆炸还是恰到好处？
- 时间线前缀"读写单点"声明是否真实（散落的渲染路径=前缀缓存失效风险，直接伤判准 R 的"稳定"）。
- MemoryPlugin 三职责聚合是否应拆（单消费者耦合 vs 内聚）。
- prompt 热重载对运行中回合的语义：换脑不换回合的边界在哪。
- 推理友好专项：assembleRequest 渲染的上下文形状是否确定可复现（判准 T 的根基）。

## 代码抽查断言候选（≥2 个）

- FormatEventKey/ParseEventMeta 契约与文档一致（event/ 包）；
- 时间线渲染是否真单点（grep 渲染入口数量）；
- EventTypeSpec 注册表条目数与文档清单一致；
- prompt 热重载 Source 的实现位置与触发面。

## 风险与回退

plugin 篇超长，若会话预算紧张，优先保证 MemoryPlugin 三职责与事件管线的评审深度，SummaryPlugin 可从简（须在报告置信度里注明）。

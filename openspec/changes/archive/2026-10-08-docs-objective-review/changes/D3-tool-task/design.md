# Design: D3 工具任务域评审

## 阅读顺序与技术要点

1. `tool-architecture.md`（主体 1261 行）：ActionTool（tmux）→ 召回体系（recall/knowledge）→ 任务工具族 → EventKeys 传递。
2. `tmux-action.md`：会话存活判定——异步任务可靠性的微观基础。
3. `task-lifecycle.md`：TaskManager / 完成探测 / 看板 / 重入 / task_terminal_ttl 回收。
4. `compression-and-telemetry.md`：压缩触发（compress_threshold 0.8）与前缀字节稳定（缓存友好）承诺、自身遥测。

## 重点问题

- tmux 作为执行底座：外部进程依赖（缺 tmux 即瘫）是否是单点？文档如何交代？
- 任务生命周期状态机：suspect → running 重挂、终态 TTL 回收、resume 窗口——状态数与转换复杂度是否匹配需求（过度设计候选热点）。
- 压缩"前缀字节稳定"与"卡片可变浓缩"并存时，前缀缓存命中率承诺是否成立（判准 R/T 交叉点）。
- recall 工具"参数即路由"（票据/因果链/关键词三合一）：对模型的接口简洁性 vs 语义含混风险。
- 训练友好专项：工具调用的结果回环（超大输出落 workspace_root 再引用）对轨迹完整性的影响。

## 代码抽查断言候选（≥2 个）

- tmux 会话重挂 / TaskID 桥（tool/action/ 下实现）；
- TaskManager 终态 TTL 与 resume_task 重入窗口（agent/task/）；
- SmartCompressor 整理轮锚定/整理间冻结的实现（agent/compress/）；
- recall 子工具路由分发（tool/ 下）。

## 风险与回退

tool-architecture 超长，预算紧张时 MCP/memoryx 两个子主题可从简（须注明置信度）。

# Proposal: D3 工具任务域评审

## Why

工具面是 agent 与世界交互的手脚：tool-architecture.md 1261 行覆盖 ActionTool/召回体系/任务工具族；tmux 异步任务层是 README 场景一（部署盯三天）的支点；压缩与遥测决定长跑上下文的形状。

## What Changes

只读评审 4 篇：`docs/wiki/tool/tool-architecture.md`、`docs/wiki/tool/tmux-action.md`、`docs/wiki/agent/task-lifecycle.md`、`docs/wiki/agent/compression-and-telemetry.md`；≥2 个代码抽查；六维报告落盘 `docs/.dev/20261007-wiki-review-D3-tool-task.md`。

## 边界与依赖

- **依赖**：一级 design.md D0 统一框架；recall 工具的存储语义引用 D1 结论（只读引用其报告，若 D1 未完成则以本域文档自证并在置信度注明）。
- **被依赖方**：W2 汇总；D4 评审 turn 原语时引用本域 task_settled 回收 turn 的结论。
- **接口面**：报告路径与六维锚点契约同 D1。
- **禁止事项**：同 D1。

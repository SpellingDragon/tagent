# 05 interactive-reentry（条件波次）：交互式命令重入归并

父变更：send-cross-task-session（依赖：02 的重入通道——实现依赖）。**条件波次**：用户明示"如果交互式命令重入不好做，先保障好内部 agent tool 重入"——05 未实施时在一级 tasks 显式记录裁剪，不阻 F 收尾。

## Why

用户裁定"main agent 重入 task（包括 agent 和交互式工具）"。现状：tmux 任务已有写入型重入（`exec op=send`/`resume_task` 注入存活会话），但与 send 工具面割裂。本域把交互式命令任务的重入统一到 `send{target:{task}}`。

## What Changes

- `send{target:{task}}` 对 tmux 类任务分派到会话注入通道（Rearm+SendKeys，复用 `task-reentry` 特异出入口条款）；IsTUI 会话拒绝；TTL 锚点重置（`async-task-lifetime` 既有"写入型重入刷新"条款）。
- `exec op=send` 保留（工具管理面），但其注入语义与 send 对内重入共用同一通道（不新开第二条注入路径）。

## 边界与依赖

- **依赖**：02（重入通道/占坑单胜）。
- **被依赖方**：无（条件域）。
- **接口面**：task 类型分派（tmux/round 两型）。
- **禁止事项**：不改 tmux detector/watch 语义；IsTUI 拒绝语义保留；不做 relaunch 归并（管理工具不属通信）。

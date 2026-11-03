# async-task-lifetime Specification（delta：写入型重入归并进 send）

> 本文件为 MODIFIED delta（条件波次）。原条款"`resume_task` 或 `exec op=send` 注入输入"中 `resume_task` 由 `send{target:{task}}` 承载；TTL 锚点刷新条款不变。

## Purpose

交互式命令任务的写入型重入经 `send{target:{task}}` 表达；TTL 刷新语义保留。

## MODIFIED Requirements

### Requirement: 写入型重入经 send 且刷新 TTL

对存活任务会话的写入型重入（`send{target:{task}}` 或 `exec op=send`）SHALL 把任务 TTL 锚点重置为 `now + ttl`；只读 `op=peek` MUST NOT 刷新。IsTUI 会话 SHALL 拒绝注入。两条注入入口 SHALL 共用同一注入路径。

#### Scenario: 注入刷新锚点

- **WHEN** 对存活 tmux 任务经 send 注入输入
- **THEN** TTL 锚点重置，peek 不刷新，TUI 会话被拒

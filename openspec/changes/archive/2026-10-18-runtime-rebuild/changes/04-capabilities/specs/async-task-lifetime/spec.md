# async-task-lifetime Specification

## Purpose

本 delta 使任务 TTL 与 exec/tmux 的**真实停止证据**对齐：TTL 到期强杀必须经 owner 的停止凭证确认，回收判据不因外部进程状态未知而误报。

## ADDED Requirements

### Requirement: TTL 回收与外部停止证据对齐

任务在派生时 SHALL 确定有限 TTL；到期唯一回收器经任务 owner 的 `Cancel()` 终止底层进程/会话。回收 SHALL 以**真实停止证据**（停止凭证/终态）为判据，`unknown` 外部状态 MUST NOT 被记作已完成回收。

#### Scenario: 到期回收并确认底层停止

- **WHEN** 一个常驻服务任务越过生效 TTL
- **THEN** 回收器经 owner `Cancel()` 终止其底层会话
- **AND** 任务一次结算为 failed 并移出看板，附带停止证据

#### Scenario: 停止未知时不伪报完成（失败）

- **WHEN** TTL 到期但底层停止无法确认
- **THEN** 任务 MUST NOT 被标记为干净收束；须保留未收束原因

#### Scenario: 跨重启回放同一寿命锚点（重启）

- **WHEN** 一个显式设了 `ttl` 的 subagent 委派在重启后经声明式回放重建
- **THEN** 其到期锚点与重启前一致

### Requirement: 年龄机制单一来源且与停止路径复用

系统 SHALL 只保留统一 TTL 一条到期强杀路径；到期强杀 MUST 复用既有退役通路（owner `Cancel` → failed 终态 → 退役记账），MUST NOT 新增第二套清理所有者或独立常驻计时器。

#### Scenario: 单一计时入口

- **WHEN** 检查年龄相关配置解析与任务状态机
- **THEN** 仅 TTL 一条到期强杀路径，无并存 stale／job deadline 读取
- **AND** 旧别名不被读取

#### Scenario: 唤醒驱动、静默滞后（并发）

- **WHEN** 一个任务越过 TTL 而进程此后完全静默
- **THEN** 物理回收滞后到下一次唤醒（看板渲染/冥想/冷启动清点），下一次渲染时它已不在 active 集合

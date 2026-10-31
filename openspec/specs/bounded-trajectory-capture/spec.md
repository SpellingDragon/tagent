# bounded-trajectory-capture Specification

## Purpose

本规范定义轨迹捕获的内存边界与折叠清理：常驻快照集合受字节预算约束，超预算时换出落盘并可读回；折叠重写投影时同步清理原文副本；内存事实面按会话热窗口保留。

## Requirements

### Requirement: 轨迹捕获常驻有界

TrajectoryCapture 的常驻快照集合 SHALL 受字节预算约束（`rl.capture_budget_mb`，缺省 64）：超出预算时最旧快照 SHALL 换出落盘，seal/导出 SHALL 能按需读回且逐字段等价。seal 后整集 SHALL 释放。

#### Scenario: 预算下常驻不超界
- **WHEN** 累计快照字节超过预算（如 1MB 预算下 50 个 >50KB 快照）
- **THEN** 常驻集合字节 SHALL ≤ 预算，且全部快照可从换出存储读回

#### Scenario: 导出等价
- **WHEN** 训练导出经公共读面读取含换出快照的会话
- **THEN** 导出样本数与 decision 数一致，字段无损失

### Requirement: 折叠清理投影原文副本

AgeFold 折叠重写投影时 SHALL 同步删除 `originals/seen/consumed` 中被折叠条目的键；`Run.facts` 内存面 SHALL 按会话热窗口（最近 2,048 条 + 未闭合段全部）保留，重启恢复路径不变。

#### Scenario: fold 后无悬挂原文
- **WHEN** 一次折叠覆盖 N 个条目
- **THEN** originals/seen/consumed 中被覆盖键全部消失，链上事实与投影呈现不回归

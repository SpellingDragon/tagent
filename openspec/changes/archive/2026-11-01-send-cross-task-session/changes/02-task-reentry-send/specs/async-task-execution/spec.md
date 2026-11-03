# async-task-execution Specification（delta：resume 边合法源状态收敛）

> 本文件为 MODIFIED delta，与 `task-reentry` delta 同步（design D7）。原条款"合法源状态 {alive-detached, stable, completed, failed} --resume--> running"中的 **failed 移出合法集合**（用户裁定 2026-11-01）。

## Purpose

任务状态机的重入边（resume/reactivate）合法源状态收敛为存活类与 settled；工具族由 `resume_task` 归并进 `send`。

## MODIFIED Requirements

### Requirement: resume 边合法源状态收敛

任务状态机的重入边 SHALL 为：合法源状态 {alive-detached, stable, completed(settled)} → running/活跃（存活类=会话注入续行；settled=重激活并**修改状态**）；**failed / cancelled SHALL 拒绝**并引导（relaunch 或新调用）；running / suspect（轮次在飞）SHALL 拒绝并引导。任务工具族 MUST NOT 再含 `resume_task`（由 `send{target:{task}}` 承载）。

#### Scenario: failed 不在合法集合

- **WHEN** 对 failed 任务发起重入
- **THEN** 明确错误+引导文本返回，状态机不动

#### Scenario: settled 重入修改状态

- **WHEN** 对 settled 任务重入
- **THEN** 状态迁移到活跃（非仅"以新 Run 续行"不改状态），结算复用既有 settle 三档分类与 task_settled 通知路径，通知携原 task id

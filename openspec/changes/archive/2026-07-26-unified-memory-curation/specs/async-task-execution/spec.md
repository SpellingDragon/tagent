## ADDED Requirements

### Requirement: 任务状态机 resume 边

任务状态机 SHALL 新增 resume 边:合法源状态 {alive-detached, stable, completed, failed} --resume(input)--> running(dense)（存活类=会话重入;完成态=round 型执行器的自然续行点）;running/suspect/cancelled SHALL 拒绝并引导。resume 后的结算复用既有 settle 三档分类与 task_settled 通知路径,通知 SHALL 携带原 task id。任务工具族 SHALL 加入 `resume_task`（与 list/get/cancel/relaunch 并列）。

#### Scenario: resume 后的结算走既有通知路径

- **WHEN** resume 的命令在 dense 窗口外完成
- **THEN** SHALL 发布 task_settled 通知（同一 task id）,持久循环按既有规则回收 turn

## MODIFIED Requirements

### Requirement: 恢复与显式重投按当前编排绑定

恢复 SHALL 从同一事实链的任务意图、资源绑定、task_signal、受理receipt、task_round和continuation checkpoint重建，不恢复进程栈或引入另一套执行图。正在等待的逻辑调用 SHALL 按原child invocation和父task producer round重新附着；显式Resume/Relaunch产生新Run时，仍按发起者持有版本或当前effective版本解析目标，MUST NOT 闭包保留旧wrapper/lease或跨owner改投。

#### Scenario: 原调用尚在等待
- **WHEN** 父子进程状态重建且父任务原producer round仍等待子invocation
- **THEN** 只附着原invocation结果，不新建一轮或重复原始goal

#### Scenario: 显式新Run目标已删除
- **WHEN** 所选当前面不含原AgentName
- **THEN** 新Run被明确拒绝，持久任务历史和原producer状态保持

## ADDED Requirements

### Requirement: 恢复先登记后启用 receiver 和 deadline

生产恢复 SHALL 先登记所有任务、完整pending signals、真实settledAt/TTL锚点、子调用私有投影及receiver，再显式ResumePendingDeliveries。屏障解除前，恢复任务 MUST NOT 被deadline/liveness/prune抢先裁决，恢复观察只登记pending而不触发settle receiver。终态任务可绑定原资源的ACK-only detector，MUST NOT 重启业务。

#### Scenario: 过期任务与pending装配窗口
- **WHEN** 历史终态或过期active任务正在逐步恢复其结果及pending receipt
- **THEN** timer和prune不越过装配屏障；解除后按原时间及原身份继续处理

#### Scenario: 终态ACK-only恢复
- **WHEN** 终局已观察而原receipt尚未受理
- **THEN** 恢复原round/seq和证据；只在受理后ACK清理，不重新采样制造新终局

### Requirement: 未资源化意图可安全补完启动

新格式意图记录中的actual ProducerRound=0 SHALL 保持为0，MUST NOT 从intent round推造producer。已受理但从未绑定资源的恢复意图，可经ResumeRestoredStart沿原intent调用Start准备、提交绑定并激活；已存在resource或producer的任务 MUST NOT 走此重执行入口。

#### Scenario: 意图提交后尚未prepare即崩溃
- **WHEN** 恢复得到原intent且无resource、无producer
- **THEN** 补完启动保持原intent round和owner，资源绑定提交在激活前，重复调用被拒绝

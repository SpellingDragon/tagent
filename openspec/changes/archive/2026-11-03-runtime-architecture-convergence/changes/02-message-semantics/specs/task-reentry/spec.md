# task-reentry Delta

## ADDED Requirements

### Requirement: 已提交重入不得被反向否认

re-entry 的输入事实提交前，失败 SHALL 具名拒绝并补偿未发生的尝试；输入事实一旦提交且 send 已返回 ok，后续执行失败 MUST 记录为该 Round 的真实结果，MUST NOT 回滚到旧 settled、追加事后 denied 或以另一回执否认原 ok。同一 send 身份重放 SHALL 重读同一 Round 的事实与结果，不得另开 Round 或复制正文。

#### Scenario: 输入提交后的 terminal 失败
- **WHEN** re-entry 输入已提交并返回 ok，随后 terminal/outbox/Flush/land 任一步失败
- **THEN** 系统 SHALL 保留该 Round 的真实 failed/cancelled/uncertain 状态并按稳定身份恢复，不得回到旧 settled 或追加否认回执

#### Scenario: 恢复后重入
- **WHEN** 进程重启后恢复 settled 任务并再次经 send 注入
- **THEN** 合法性 SHALL 由持久任务状态、地址、Round 与绑定决定，不得因本进程缺少内存 continuation 而拒绝

## ADDED Requirements

### Requirement: 重建任务的 resume nil 安全

TaskManager 对经 RestoreTask 重建（detector/watchDone/firstSettle 未初始化）的任务执行 Resume 时 SHALL NOT panic：重建任务 SHALL 在注册时即持有可用的 watchDone 与 firstSettle 通道；Resume 的 watch 换代判定 SHALL 将 nil detector 视为「必换新 watch」而非依赖 close 旧通道；Spawn 与 Resume 的 select 分支对 nil detector SHALL 只等待 firstSettle（纯同步语义）。修复 MUST 附 fail-before 回归测试（重建任务 + resume 先证 panic 再证修复）。

#### Scenario: 冷启动重建后的任务被 resume

- **WHEN** 进程重启后 RestoreTask 重建历史任务（detector=nil）且模型调用 resume_task 触发 Resume
- **THEN** Resume 正常换装新 watch 并返回 SpawnResult，进程不出现 nil channel close 或 nil 接口调用的 panic

### Requirement: nil 接口模式级审计留痕

对全库「close(通道) / 接口方法调用」中依赖调用方非 nil 口头契约的点 SHALL 完成一次模式级审计，清单（含 file:line 与处置）SHALL 记录于 LEDGER 台账；审计发现的同型裂缝 MUST 逐个修复或显式豁免（豁免须写明理由）。

#### Scenario: 审计发现同型裸调点

- **WHEN** 审计在某调用点发现对可能为 nil 的 detector/channel 的裸操作
- **THEN** 该点获得守卫修复并附回归测试，或以注释与 LEDGER 条目显式豁免（含不可达论证）

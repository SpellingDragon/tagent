# async-task-execution Delta

## ADDED Requirements

### Requirement: 终态事实四端一致
任务终态 MUST 经由唯一转换入口产生：内存状态、Settle 信号 Kind、WAL settle_status、反馈正负性四端 MUST 一致——失败终态的结算信号 MUST 携带失败语义（SettleFailed 或 SettleCompleted+Err），事件映射 MUST 将其记为 failed 并产生负面反馈；未知 Settle Kind MUST 映射为 unknown 并告警，MUST NOT 默认记为 completed。

#### Scenario: 僵死回收不产生成功事实
- **WHEN** zombie / orphan / stale 回收路径终结一个任务
- **THEN** 看板状态、task_settled 事件、WAL settle_status、反馈四端 MUST 同为失败语义，MUST NOT 出现「内存 failed、事件 completed」

### Requirement: 迟到信号不得复活终态
任务进入终态后，后续到达的 detector 信号 MUST 被丢弃并记录（含被丢弃的 Kind），MUST NOT 修改终态、MUST NOT 触发重复结算。

#### Scenario: 终态后迟到 Stable 信号
- **WHEN** 任务已 finalize 为 failed，其旧 detector 随后发出 SettleStable
- **THEN** 该信号 MUST 被丢弃并记 Warn，任务状态与结算事实 MUST 保持不变

## MODIFIED Requirements

### Requirement: 服务型任务转 alive-detached
输出稳定的服务型任务进入 alive-detached 观测语义时，其脱离时间戳（detachedAt）MUST 作为任务生命周期事实持久化（写入 Declarative 并随 task_spawned 链恢复）；恢复后的脱离时间 MUST 沿用原值，MUST NOT 以恢复时间替代。

#### Scenario: detached 任务跨重启
- **WHEN** alive-detached 任务跨重启恢复
- **THEN** 恢复后的 detachedAt MUST 等于死亡前的原值，超龄观测/终止判定沿用真实时长

### Requirement: 任务超龄治理为观测优先、终止显式
超龄 detached 任务默认 MUST 仅标记 stale 观测态（非终态、进程不动、一次性告警通知）；仅当任务生命周期为 job 且宿主显式配置 job deadline 时，超 deadline 才由 owner（detector.Cancel）执行终止并一次结算为 failed。service 型任务 MUST NEVER 因年龄被终止；任务生命周期 MUST 显式声明（TaskSpec.Lifetime，默认按 Kind 推断：command/subagent→job、generic→service）。

#### Scenario: 默认配置下超龄
- **WHEN** job 型任务 detached 超过 task_stale_after 且未配置 job deadline
- **THEN** 任务 MUST 转为 stale 观测态并发出一次性告警，进程与任务 MUST NOT 被强制终结

#### Scenario: 配置 deadline 后超龄
- **WHEN** job 型任务配置了 task_job_deadline 且 detached 超过 deadline
- **THEN** owner MUST 执行 Cancel 确认退出，任务一次结算为 failed，终态事实四端一致

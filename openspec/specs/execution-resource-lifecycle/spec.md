# execution-resource-lifecycle Specification

## Purpose

外部执行资源的五态查询、状态事件与重启接管：调用返回、进程运行、真正退出、资源释放与未知状态可区分，退出码与释放结果可判定，持续输出经既有通知链产生后续事件。

## Requirements

### Requirement: 执行资源查询面区分调用返回、运行、退出与释放

外部执行资源的查询面 SHALL 区分四种可判定状态：工具调用已返回、外部进程仍在运行、进程真正退出（含退出码）、资源已停止释放或状态未知（具名未确认）。调用返回 MUST NOT 被视为进程结束；同一资源键的后续查询 SHALL 能观察到调用返回之后的新输出或仍在持有的资源面（如 `Held`），该观察 MUST 来自正式查询入口，MUST NOT 依赖测试旁路注入。查询面 MUST NOT 新建平行资源仓，SHALL 直接映射既有 `Manager` 的 Acquire/Renew/Observe/Capture/Release/Explain 能力。

#### Scenario: 调用返回不证明进程结束

- **WHEN** 工具调用已返回而对应外部进程仍在运行
- **THEN** 查询面 SHALL 仍可判定该资源处于运行状态
- **AND** MUST NOT 因调用返回而报告资源结束或进程退出

#### Scenario: 同键复用查询可见后续输出

- **WHEN** 调用返回后以同一资源键再次查询该外部进程
- **THEN** 查询面 SHALL 返回调用返回之后产生的新输出或仍持有的资源面
- **AND** 该结果 SHALL 来自正式查询入口，MUST NOT 由测试直接改写内部状态

### Requirement: 进程退出码与释放结果可判定

外部进程真正退出时，查询面 SHALL 记录并呈现其退出码与退出状态（如 `Release` 的 `exit=N`）；释放动作完成后 SHALL 呈现释放确认，或在无法确认时呈现具名的状态未知（如 `retained`/`ErrResourceUnknown`）。查询面 MUST NOT 以调用返回时点替代真实退出时点，MUST NOT 把未确认的释放伪装为成功。

#### Scenario: 真正退出记录退出码

- **WHEN** 外部进程真正退出且调用方完成释放
- **THEN** 查询面 SHALL 呈现该进程的退出码与退出状态
- **AND** MUST NOT 以调用返回时点替代真实退出时点

#### Scenario: 释放确认或状态具名未知

- **WHEN** 释放动作执行后资源已停止，或无法确认其状态
- **THEN** 查询面 SHALL 分别呈现释放确认或具名状态未知（`retained`/`ErrResourceUnknown`）
- **AND** MUST NOT 在未确认时宣称资源已停止

### Requirement: 稳定与部分输出不等于成功或完成

输出稳定（静默）SHALL 仅表示输出进入稳定状态，MUST NOT 表示任务成功；已读取部分输出 MUST NOT 表示任务完成。查询面 MUST NOT 提供把稳定或部分输出报告为成功/完成的判定；若不存在成功报告面，则以“无任何查询面可报成功”为可判形态并在证据中如实登记。成功与完成 SHALL 以进程退出码、业务结果或显式判定为准，失败与未确认 MUST NOT 被伪报为成功。

#### Scenario: 稳定不等于成功

- **WHEN** 外部进程输出进入稳定状态但进程尚未退出或业务结果未达成
- **THEN** 查询面 MUST NOT 报告成功
- **AND** 状态 SHALL 保持为“稳定但未完成”或具名未确认

#### Scenario: 部分输出不等于完成

- **WHEN** 已读取外部进程的部分输出而进程仍在运行
- **THEN** 查询面 MUST NOT 报告任务完成
- **AND** 后续 SHALL 继续跟踪直至真实退出或具名未确认

### Requirement: 返回后持续跟踪并经既有通知链产生后续事件

工具调用返回后，系统 SHALL 继续跟踪外部执行资源，并在状态变化（稳定、退出、释放、可疑）时产生后续事件；这些事件 SHALL 复用既有通知消费链（sink/Backflow）落为可审计事实并供正式查询面读取，MUST NOT 新建第二套执行资源状态仓或第二套通知身份。租约续期 MUST NOT 随工具调用返回而终止，SHALL 持续到资源释放、显式取消或具名未确认。中间态（稳定、可疑、退出）MUST NOT 驱动 follow-up 回合，MUST NOT 进入下一轮 decision 的模型请求。

#### Scenario: 续租不随调用返回终止

- **WHEN** 工具调用已返回而外部进程仍需受管
- **THEN** 续租 SHALL 持续到资源释放、显式取消或具名未确认
- **AND** MUST NOT 在调用返回时停止续租并放任资源失管

#### Scenario: 状态变化产生后续通知

- **WHEN** 调用返回后外部进程状态发生变化（稳定、退出、释放或可疑）
- **THEN** 系统 SHALL 产生后续通知事件
- **AND** 该事件 SHALL 经既有通知消费链落为审计事实，MUST NOT 驱动 follow-up 回合或进入模型请求

### Requirement: 生产生命周期挂钩触达执行资源

生产生命周期入口 SHALL 经与查询面同一 `Manager` 触达外部执行资源：生产 Sweep 与宿主关闭（`Host.Close`）MUST 释放或具名保留仍在受管的 exec 资源，修复关闭后资源泄漏；MUST NOT 只清理测试显式持有的资源而遗漏生产路径。状态变化 SHALL 作为可审计事实供正式查询面读取，MUST NOT 依赖测试旁路注入，MUST NOT 驱动 follow-up 回合或进入模型请求。

#### Scenario: 状态变化只落审计事实

- **WHEN** 外部执行状态变化后原 invocation 进入下一轮 decision
- **THEN** 下一轮模型请求 MUST NOT 携带执行状态材料，该状态 SHALL 只作为审计事实可查
- **AND** MUST NOT 依赖测试旁路注入

#### Scenario: Host.Close 释放执行资源

- **WHEN** 宿主关闭时仍存在受管的外部执行资源
- **THEN** `Host.Close` SHALL 经同一 `Manager` 释放这些资源
- **AND** 关闭后 MUST NOT 残留未释放的受管进程

### Requirement: 重启后接管或具名未确认

进程重启后，系统 SHALL 对外部执行资源执行接管（Adopt）或给出具名未确认状态；MUST NOT 静默丢弃仍在运行的资源，MUST NOT 在未确认时宣称资源已停止。接管结果 SHALL 可经正式查询面读取。

#### Scenario: 重启接管或具名未确认

- **WHEN** 进程重启时存在此前未释放的外部执行资源
- **THEN** 系统 SHALL 接管该资源，或以具名未确认状态呈现
- **AND** MUST NOT 静默丢弃或宣称已停止

#### Scenario: 未确认重启不报已停止

- **WHEN** 重启后无法确认此前外部执行资源的状态
- **THEN** 系统 SHALL 保持具名未确认
- **AND** MUST NOT 报告资源已停止或任务已完成

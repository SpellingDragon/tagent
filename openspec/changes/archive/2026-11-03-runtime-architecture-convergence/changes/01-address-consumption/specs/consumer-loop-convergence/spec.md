# consumer-loop-convergence Delta

## MODIFIED Requirements

### Requirement: 输入入口只提交事件

所有正式输入入口，包括 `Host.Invoke`、`Run.Invoke`、`Run.InjectEvent`、模型可见 send 与 `fireNoticeTurn`，SHALL 提交事实并唤醒目标地址的常驻消费循环，MUST NOT 直接启动 `Loop.Drive`。同步调用方 SHALL 等待其输入所属闭合段的不可变结果；send 回执 SHALL 仅表示目标事实已持久提交，不表示目标已完成回答。

#### Scenario: 用户消息到达
- **WHEN** 用户发送消息经 `Host.Invoke` 提交
- **THEN** 消息 SHALL 落链并唤醒消费循环；消费循环的下一次拉取读到该消息并调用模型

#### Scenario: 冥想注入到达
- **WHEN** 冥想注入经宿主面提交
- **THEN** 注入 SHALL 落链并唤醒目标 Consumer，MUST NOT 启动独立 Drive

#### Scenario: 空闲目标收到 send
- **WHEN** 模型可见 send 向已绑定但空闲的目标地址提交正文
- **THEN** 正文落链后 SHALL 唤醒该地址既有 Consumer，MUST NOT 因目标空闲而启动另一条 Drive

#### Scenario: 低级 Run 入口到达
- **WHEN** 调用方使用 `Run.Invoke` 或 `Run.InjectEvent`
- **THEN** 输入 SHALL 与 Host 输入共用同一地址 Consumer 和答案等待语义，不得保留直驱例外

### Requirement: gate 与 Occupancy 收敛

`addressGate`（token + inflight + hold/release/running）SHALL 删除。冥想让路 SHALL 读消费循环的运行状态（该会话是否有模型请求正在执行）。

#### Scenario: 冥想让路读消费状态
- **WHEN** 冥想 pass 评估时观察的会话有一个模型请求正在执行
- **THEN** 冥想 SHALL 让路（保持 episode，按既有节奏重评）

#### Scenario: 并发两请求同 session
- **WHEN** 两个并发 `Host.Invoke` 到达同一 session 并被同一段合批消费
- **THEN** 两者 SHALL 各自提交自己的事实，并获得该段同一个不可变结果；不得让第二个调用只获得无后续答案出口的受理收据

## ADDED Requirements

### Requirement: 答案归属不受缓存淘汰影响

每个已闭合输出 SHALL 能通过其消费的输入身份在事实保留范围内重建。进程内答案缓存 SHALL 仅作加速；缓存淘汰、过期或进程内晚到等待 MUST NOT 改变等待者取得的答案，也 MUST NOT 使已消费输入转而等待下一段或邻段答案。

#### Scenario: 答案缓存淘汰后的晚到等待
- **WHEN** 输入所属段已经闭合，且其进程内缓存条目已被淘汰或过期
- **THEN** 晚到等待者 SHALL 仍取得该输入所属段的同一不可变结果

#### Scenario: 合批输入不读邻段答案
- **WHEN** 同一地址先后闭合多个段
- **THEN** 每个输入的等待者 SHALL 只取得实际消费该输入的段结果，MUST NOT 读取较早或较晚邻段的答案

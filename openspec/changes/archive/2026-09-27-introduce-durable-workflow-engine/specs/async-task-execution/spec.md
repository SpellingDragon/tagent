## ADDED Requirements

### Requirement: 后台执行保留编排引用至实际停止

纳入任务层的子 agent／工具调用 SHALL 在派生执行启动前获取并持有其编排执行绑定引用，直到实际调用、响应流排空或执行 owner 确认停止后恰好释放一次。顶层 ACK、任务状态被置为终态、流早停或取消请求本身 MUST NOT 单独作为释放依据；spawn 被拒、dedup 命中与取消确认路径同样 MUST NOT 泄漏或提前释放派生引用。

结算与通知 SHALL 经任务所属 tagent 自己的 TaskManager、记录 sink 与 EventBus 完成；来源 metadata（含诊断用 generation 标记）随原管道保真传递，不改变既有任务状态机。A 跟踪一次对 B 的委派，不代表 A 接管 B 内部 C／exec 的任务。B 内部任务结算先回 B.bus，由 B 处理后再产生向 A 的输出；当前回复结束不关闭 B 或其无关任务。

#### Scenario: ACK 后热更不关旧资源

- **WHEN** G1 请求返回任务 ACK 后发布 G2，G1 的后台调用仍在运行
- **THEN** G1 所需绑定与资源保持，实际停止后恰好释放一次；task_settled 触发的新 turn 使用 G2

#### Scenario: 拒绝与去重不泄漏引用

- **WHEN** 派生调用因 spawn gate 拒绝或同名任务 dedup 而未实际启动
- **THEN** 该次尝试获取的执行引用被释放，不产生无主持有；既有任务继续沿用其原绑定

#### Scenario: 拒绝时 detector 已提前启动

- **WHEN** detector 的后台函数已启动，而 Spawn 随后被 gate 拒绝或 dedup 返回既有任务
- **THEN** 该次尝试先取消并等待其真实完成确认再释放派生引用；既有任务的执行、来源 metadata 与租约不受影响

#### Scenario: 后台不受父取消影响但仍保有绑定

- **WHEN** G1 任务已经 ACK 并脱离父取消，父 turn 结束且 G2 已发布
- **THEN** 后台继续使用 G1 的声明与目标，结果经原任务通知链返回；只有真实后台及其流收尾后才释放 G1，通知触发的新顶层 turn 使用当前代

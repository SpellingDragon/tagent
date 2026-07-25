## ADDED Requirements

### Requirement: 任务看板注入不依赖构造/wiring 顺序

任务看板的 BeforeModel 注入 SHALL 在 `taskController` 于 ContextManager **构造完成之后**才被 wiring 的情况下依然生效。看板回调 SHALL **无条件注册**；"是否已 wiring taskController"的判断 SHALL 在每次 BeforeModel 调用的**运行时**进行，SHALL NOT 在回调注册（构造）时一次性判定——否则构造期 `taskController==nil` 会使看板回调被永久跳过。

#### Scenario: taskController 构造后 wiring 仍注入看板

- **WHEN** ContextManager 构造完成后才 wiring `taskController`，随后进入一个存在活跃后台任务的 turn
- **THEN** BeforeModel SHALL 注入当前活跃任务看板
- **AND** SHALL NOT 因构造期 `taskController` 为 nil 而永久跳过看板注入

#### Scenario: 无 taskController 时安全跳过

- **WHEN** 运行时 `taskController` 未 wiring（为 nil）
- **THEN** 看板回调 SHALL 安全跳过（不注入、不 panic）

### Requirement: 退出任务的状态驱动清理与资源回收

TaskManager 的任务表稳态 SHALL 只保留**存活任务**（running/stable/alive_detached/suspect）。已退出任务（completed/failed/cancelled/dead）SHALL 被清理——以**退出状态**为主要触发（不仅依赖 TTL）；短 grace 期 SHALL 足够保证回收 turn 内 `get_task_result` 仍可用。

清理一个退出任务时，TaskManager SHALL 先调用其 detector 的 `Cancel()` 回收底层资源（goroutine/context/tmux 会话），再从任务表移除——SHALL NOT 仅从 map 删除引用而遗留资源。`Cancel()` SHALL 幂等：对已 settle/已 cancel 的任务重复调用 SHALL 安全无副作用。清扫 SHALL 惰性进行（`List`/`Spawn` 调用时），SHALL NOT 依赖后台定时器；资源回收 SHALL NOT 在持有任务表锁期间执行（避免阻塞持锁）。

#### Scenario: 退出任务被清理且资源被回收

- **WHEN** 一个已退出任务超过 grace 期，随后发生一次 `List`/`Spawn`
- **THEN** 该任务 SHALL 被移除
- **AND** 其 detector 的 `Cancel()` SHALL 被调用以释放底层资源（goroutine/context/会话）

#### Scenario: 存活任务不被清理

- **WHEN** 清扫运行时存在 running/stable/alive_detached/suspect 任务
- **THEN** 这些任务 SHALL 保留在任务表中，SHALL NOT 被移除或 `Cancel`

#### Scenario: grace 期内 get_task_result 仍可用

- **WHEN** 一个退出任务 settle 后、在 grace 期之内被 `get_task_result` 查询
- **THEN** SHALL 返回其完整结果（回收 turn 内可用）

#### Scenario: 资源回收幂等

- **WHEN** 对一个已 settle/已 cancel 的任务再次触发资源回收（`Cancel`）
- **THEN** SHALL 安全无副作用（不 panic、不误伤其他资源）

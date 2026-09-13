# task-registry-rebuild Specification

## ADDED Requirements

### Requirement: 重建任务不得因缺失探测器而崩溃

由事实链重建的任务（`RestoreTask`）SHALL 允许携带 nil 探测器（跨重启无资源可回收）。
任务回收路径（`pruneTerminal`）在调用探测器 `Cancel()` 前 MUST 判空；nil 时 MUST 跳过
`Cancel` 并仍回收该任务条目，MUST NOT 因此 panic。

#### Scenario: 重建的终态任务被回收

- **WHEN** 一个由 `RestoreTask` 构造、detector 为 nil 的任务进入终态并超过 terminalTTL
- **THEN** 回收流程正常完成，条目被移除，MUST NOT 发生 nil 解引用 panic

#### Scenario: 探测器读取与 Resume 换装无竞态

- **WHEN** 回收流程读取任务探测器，而 `Resume` 可能并发替换该探测器
- **THEN** 读取 MUST 在任务锁保护下进行

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

### Requirement: 转世孤儿任务必须可回收

跨重启重建后，nil-probe（`Spec.Alive == nil`）且携带 Declarative 的 suspect 任务，
若其 Declarative.TaskID 不被任何存活会话跟踪且年龄超过 reincarnationOrphanGrace，
SHALL 被裁决为 terminal failed（置 settledAt、触发 onSettle 恰一次），从而进入
pruneTerminal 的正常回收轨道。byKey 占用 MUST 随回收解除，MUST NOT 永久阻塞同 key
re-spawn。

#### Scenario: 未被跟踪的孤儿被回收

- **WHEN** 重建后的 suspect nil-probe 任务未被 IsTrackedSession 跟踪且超 grace
- **THEN** 任务被 retire 为 failed，settledAt 置位；terminalTTL 后条目与 byKey 均被回收

#### Scenario: 被跟踪会话不得误杀

- **WHEN** suspect 任务的 Declarative.TaskID 被存活 monitor 跟踪
- **THEN** 任务 MUST NOT 被孤儿裁决；保持 suspect→running 提升路径

#### Scenario: 无 Declarative 的任务不受影响

- **WHEN** 重建的任务无 Declarative（generic 展示卡片）
- **THEN** 任务不受孤儿裁决影响，MUST NOT 被回收

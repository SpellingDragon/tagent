# task-registry-rebuild Specification

## Purpose
定义从事实链（task_spawned/settle 事件）冷启动重建任务 registry 的行为契约：重建保真（身份/世系不丢失）、崩溃安全（nil detector 守卫）与孤儿裁决（跨重启 suspect 的确定性回收）。

## Requirements

### Requirement: 重建任务不得因缺失探测器而崩溃

由事实链重建的任务（`RestoreTask`）SHALL 允许携带 nil 探测器（跨重启无资源可回收）。任务回收路径（`pruneTerminal`）在调用探测器 `Cancel()` 前 MUST 判空；nil 时 MUST 跳过 `Cancel` 并仍回收该任务条目，MUST NOT 因此 panic。

#### Scenario: 重建的终态任务被回收

- **WHEN** 一个由 `RestoreTask` 构造、detector 为 nil 的任务进入终态并超过 terminalTTL
- **THEN** 回收流程正常完成，条目被移除，MUST NOT 发生 nil 解引用 panic

#### Scenario: 探测器读取与 Resume 换装无竞态

- **WHEN** 回收流程读取任务探测器，而 `Resume` 可能并发替换该探测器
- **THEN** 读取 MUST 在任务锁保护下进行

### Requirement: 转世孤儿任务必须可回收

跨重启重建后，nil-probe（`Spec.Alive == nil`）且携带 Declarative 的 suspect 任务，若其 Declarative.TaskID 不被任何存活会话跟踪且年龄超过 reincarnationOrphanGrace，SHALL 被裁决为 terminal failed（置 settledAt、触发 onSettle 恰一次），从而进入 pruneTerminal 的正常回收轨道。byKey 占用 MUST 随回收解除，MUST NOT 永久阻塞同 key re-spawn。

#### Scenario: 未被跟踪的孤儿被回收

- **WHEN** 重建后的 suspect nil-probe 任务未被 IsTrackedSession 跟踪且超 grace
- **THEN** 任务被 retire 为 failed，settledAt 置位；terminalTTL 后条目与 byKey 均被回收

#### Scenario: 被跟踪会话不得误杀

- **WHEN** suspect 任务的 Declarative.TaskID 被存活 monitor 跟踪
- **THEN** 任务 MUST NOT 被孤儿裁决；保持 suspect→running 提升路径

#### Scenario: 无 Declarative 的任务不受影响

- **WHEN** 重建的任务无 Declarative（generic 展示卡片）
- **THEN** 任务不受孤儿裁决影响，MUST NOT 被回收

### Requirement: 任务身份与世系跨重启保真

任务的身份、来源与路由字段（ID / Kind / Key / Desc / Origin）MUST 作为持久化事实跨重启保真：写入 task_spawned 时 MUST 深拷贝保存运行态 Spec.Origin（trigger_source 与来源事件键）；恢复时这些字段 MUST 以 WAL 持久层为准，恢复闭包工厂返回的 spec 仅提供执行能力（runner / probe / resume），MUST NOT 整体覆盖持久化身份。

#### Scenario: 冥想派生任务跨重启恢复

- **WHEN** trigger_source=meditation 的任务跨重启恢复并发生后台结算
- **THEN** 结算事件的世系 MUST 保持 meditation 来源（MUST NOT 退化为通用 task 来源），宿主投递门禁 MUST 维持扣留

#### Scenario: 历史记录缺 origin

- **WHEN** 恢复的 task_spawned 记录不含 origin 字段（旧版本写入）
- **THEN** 恢复后 Origin MUST 为 unknown，宿主侧 unknown 与内部来源 MUST 同等扣留（未知不得升级为可投递来源）

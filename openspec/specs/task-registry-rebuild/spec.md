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

### Requirement: 恢复与显式重投按当前编排绑定

冷启动恢复 SHALL 沿用原 task_spawned、settle 和续命记录重建任务登记，不新增编排持久格式。正常 Spawn 的存量任务和 WAL 恢复任务 SHALL 共用新的 Run 选版规则：有发起调用时继承其执行绑定，无发起者时获取当前 effective。向仍存活执行送输入保持原绑定；创建新 Run 的 Resume／Relaunch 不能直接复用记录闭包捕获的旧 wrapper。

解析目标 SHALL 针对所选版本中的原任务所属 owner 工具集合，而非祖先直接工具集合或全局任意同名 agent；该版该 owner 无目标即明确拒绝，不伪造 Spawn 成功、不静默改投、保留任务链上下文。任务所属 owner 从该任务所在 tagent 自己的 manager／恢复接线取得，不从顶层单例推断；父层委派任务与子层内部任务分别恢复各自记录。复用既有持久身份材料，不为主子角色新增持久字段。闭包 MUST NOT 直接或通过 spawner/context 间接保留旧 wrapper、binding、lease 或私有 CM。TaskManager 只传递调用上下文及原任务材料，不成为编排版本选择者。

#### Scenario: 多级任务重入定位所属 owner

- **GIVEN** G1 中 A 可调用 B，B 可调用 C，A 的直接工具不含 C
- **WHEN** B 所属 C 任务被持 G1 的调用重入，而 G2 已删除 C
- **THEN** 从 G1 的 B 工具面解析并真实调用 C，不因 A 的工具表无 C 而误拒；无发起者的当前重入则按 G2 拒绝且不改变任务链

#### Scenario: 重启后重投用当前配置

- **WHEN** 带声明式投影的 subagent 任务在重启后由独立管理操作 Relaunch，期间配置已更新
- **THEN** 新 Run 使用当前有效声明与目标，原任务链上下文照常还原

#### Scenario: 恢复任务目标已移除

- **WHEN** 所选当前代不含恢复任务 Declarative.AgentName
- **THEN** Relaunch／新 Run 型 Resume 明确报错且不创建执行，不改变既有任务终态协议

#### Scenario: 正常 Spawn 的旧任务也必须重新选版

- **WHEN** G1 对 B 的任务已结束且仍可重入，G2 删除 B 后由 G2 发起任务 action
- **THEN** 实际 Relaunch 和新 Run 型 Resume 均拒绝 B，不通过旧闭包复活 G1 目标；任务链材料保持

#### Scenario: 在途旧代发起者的重入继承

- **WHEN** G1 调用仍持有效租约，G2 已删除 B，而该 G1 调用发起一次 B 的新 Run 型重入
- **THEN** 按 G1 绑定执行 B 并派生租约，不误用 G2 全局表拒绝；实际执行停止后释放派生引用

#### Scenario: 输入送给仍存活的执行

- **WHEN** 任务对应执行仍活着，Resume 仅向该执行提供新输入而不创建 Run
- **THEN** 输入由原执行绑定处理，不发生新的编排版本选择


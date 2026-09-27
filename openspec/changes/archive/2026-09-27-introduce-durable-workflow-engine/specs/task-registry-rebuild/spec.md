## ADDED Requirements

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

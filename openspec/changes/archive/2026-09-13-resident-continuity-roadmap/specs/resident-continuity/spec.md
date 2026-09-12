## ADDED Requirements

### Requirement: 状态与执行器分离不变量

常驻 Agent 的持久状态（投影、任务板、异步/常驻会话态）SHALL 由长生命周期持有者（contextManager/TagentAgent）持有并置于**可换执行器（fwAgent+runner+其工具）之外**；执行器 SHALL 对持久状态无状态（每 turn 操作外部传入的状态、不私藏）。新增状态 SHALL NOT 沉入执行器内部（如工具内私有 TaskManager）——需持久的状态一律外置 + 事件溯源。

#### Scenario: 换执行器不丢状态
- **WHEN** R4 结构热更换入新执行器（新 fwAgent+runner）
- **THEN** 投影/任务板/常驻会话态 SHALL 原封不动（持有者在执行器之外），下一 turn 新执行器在原状态上继续

#### Scenario: 新增持久状态须外置
- **WHEN** 未来变更引入新的需持久状态（如新的看板/注册表）
- **THEN** 其持有位置 SHALL 在执行器之外且经事件溯源持久，SHALL NOT 作为工具/执行器内部内存态

### Requirement: 状态层事件溯源模式

每一持久状态层（R1 投影、R2 任务板、R3 常驻会话态）SHALL 为事实链的旁路产物：写入经事实链（StoreEvent 或其一等记录）、运行期增量维护（旁路同点更新）、重启经**事件回放重建进空态**（R1 为「snapshot+尾部回放」特例——其状态层有折叠压缩；无压缩语义的状态层（如 R2 任务板）为纯全量回放，勿预设须有 snapshot 事件）。R1 确立的一般模式（旁路产物+回放重建+外置）SHALL 作为 R2/R3 范式。

#### Scenario: 重建是同一 fold 的另一入口
- **WHEN** 任一状态层重启重建
- **THEN** 重建结果 SHALL 与运行期增量维护的状态等价（同一「状态=事实链 fold」不变量的增量式与全量式两入口）

### Requirement: 执行器可换语义

执行器（fwAgent+runner）SHALL 是「配置→turn 行为」的可重导出函数：配置变更时 SHALL 按 diff 二分处理——行为变更走细粒度热同步（model/MCP/prompt 内容/阈值，零执行器重建）、结构变更（tools/subagents/prompt wiring）重建执行器并**原子换入**（build-validate-then-swap：新执行器构建校验成功才换、失败旧的原样保留可回滚）。换入 SHALL 在 turn 边界原子完成：in-flight turn 用旧执行器跑完、后续 turn 用新。热更 SHALL NOT 依赖进程重启（重启是有损的：丢在途调用、事件循环、异步跟踪）。

#### Scenario: 结构热更非重启
- **WHEN** 配置的结构项（如工具集）变更
- **THEN** SHALL 重建执行器并 turn 边界原子换入（进程/事件循环/持久状态不中断），SHALL NOT 要求重启

#### Scenario: 换入失败可逆
- **WHEN** 新配置构建/校验失败
- **THEN** SHALL 不换（旧执行器原样服务），并支持按上一份配置回滚

#### Scenario: 中途换工具集安全
- **WHEN** 对话中途热换工具集
- **THEN** 历史 tool_call/tool_result 作为不可变事实照常渲染，新工具集仅对后续 turn 生效（in-flight tool 调用用旧语义跑完）

### Requirement: 路线图阶段治理

R1→R2/R3→R4 的依赖序 SHALL 遵守（R4 依赖状态外置完成）；每阶段 SHALL 派生独立 openspec 子变更执行，且准出门禁 SHALL 含：实现前 fresh-eyes 复验、fail-before 回归、build/vet/全量 -short + 相关包 -race。预留确认项（design.md 各 D 所列）SHALL 在对应阶段执行时核对形成决议或显式降级标注。

#### Scenario: 阶段准出门禁
- **WHEN** 任一阶段子变更进入实现
- **THEN** SHALL 已过 fresh-eyes 复验（对照真实代码）且回归含 fail-before 用例；未过不得编码

#### Scenario: 依赖序守护
- **WHEN** R4（执行器热换）子变更立项
- **THEN** R2（任务板外提+溯源）与 R3（常驻会话态外置）SHALL 已完成或其子变更显式声明为何不阻塞

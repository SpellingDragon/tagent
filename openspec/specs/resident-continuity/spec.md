# resident-continuity Specification

## Purpose

常驻 Agent 架构不变量（守护契约，后续涉及状态层/执行器的变更须对照）：状态与执行器分离、状态层事件溯源模式（旁路产物+回放重建+外置）、执行器可换语义（drain-free turn 级）、R1→R2/R3→R4 依赖序。R1=对话上下文（event-sourced-projection）；R2=任务板（task-registry-and-board）；R3=常驻会话（resident-session-continuity）；R4=热更（swappable-executor）。
## Requirements
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

### Requirement: 请求开始执行时固定编排执行绑定

顶层请求 SHALL 在开始实际执行、首次读取组织绑定前获取当前有效执行绑定版本并持有引用；常驻模式以冻结批次对应的业务 turn 为边界，一次性模式以顶层 Run 为边界。正在排队的输入不提前绑定代，不改接收持久格式。该请求的重试、工具声明、子调用目标与嵌套执行 SHALL 使用同一版，不逐层重新读取 active。

编排热更 SHALL 复用已有 agent runtime 的 store/session/projection/TaskManager，不以换代替换这些常驻状态。版本句柄与执行引用适配 MUST NOT 演化为新的内部接收/任务/恢复状态机。

#### Scenario: 请求中途换编排不跨代

- **WHEN** R1 已在 G1 开始执行，发布 G2 将 A→B 改为 A→C
- **THEN** R1 的模型工具声明与实际委派仍调用 G1 的 B，重试也不改路由；新开始的 R2 使用 G2 的 C；不调用工具时 B 与 C 都不执行

#### Scenario: 排队输入使用执行时版本

- **WHEN** 输入在 G1 时入队，但直到 G2 发布后才开始执行
- **THEN** 使用 G2，接收队列不需要保存 G1 或任何编排现场

### Requirement: 发布回滚与常驻身份保持

候选发布／回滚 SHALL 原子选择整份执行绑定，失败不推进 effective。回滚将上一份已验证配置重新构造为新版本，只影响之后开始的请求。同名 agent 存储路径/后端变更仍需明确拒绝并提示重启；独立 runtime 热参不属于组织快照冻结承诺。agent 新增先完整构造再发布；移除先停止新路由，保留在途调用、任务和未决输入的 owner 至收敛。

#### Scenario: 回滚不迁移进行中请求

- **WHEN** R1 使用 G2 运行期间重新发布 G1 配置为 G3
- **THEN** R1 仍使用 G2，后续请求用 G3，store/session/任务实例不被重建

#### Scenario: 移除 agent 不关闭其在途任务

- **WHEN** 新代不再引用某 agent，而旧代仍有该 agent 的实际执行或未决输入
- **THEN** 新请求不能路由给它，原 owner 保持到相关工作收敛，不因拓扑移除强关

### Requirement: 编排热更不要求跨进程现场恢复

进程重启 SHALL 继续按既有输入/任务协议恢复，不持久化或恢复编排执行现场。按旧协议确需重新开始的顶层调用使用当前有效编排；已完成或核对不确定的输入受原执行资格门约束，不因版本变化获得重跑资格。

#### Scenario: 热更与完成恢复正交

- **WHEN** 新进程加载新编排，但旧输入有 completion/receipt 或处于核对不确定态
- **THEN** 只补收尾或保持阻塞，不重新执行该输入

### Requirement: 入口身份变更在资源获取前拒绝

热更 SHALL 比较新 entry 与启动入口身份；不同则在任何候选资源构建前拒绝并提示重启，MUST NOT 用旧入口缺失后的零配置发布。保留或删除旧定义均不改变该规则。

#### Scenario: 入口改名并删除旧定义

- **WHEN** entry 从 old 改为 new，old 定义被删除且 new 定义合法
- **THEN** 发布被明确拒绝，新候选资源获取次数为零，旧入口继续按原声明与模型服务，序号／存储身份不变

### Requirement: owner 收敛后退役与组织关闭

热移除 SHALL 保留所选旧版本的合法可调用依赖、实际执行、后台任务、已接受输入及恢复核对仍需要的 owner；这些义务消失后 MUST 关闭其独占组件、释放租约并撤销 resident／诊断登记。义务判据与新调用／关闭准入 SHALL 在同一 owner 上协调，不从 shell 和 resident 两份对象拼凑。最终释放不依赖下一次业务输入或配置编辑。不得以回滚配置或曾出现过的名字永久保留运行实例；已有纯存储身份基准不因实例退役而自动丢弃。组织 Close SHALL 覆盖全部组织 owner 和未完成候选，关闭失败诚实报告并保有未确认资源。

#### Scenario: 真实子调用结束且没有后续活动

- **WHEN** 热删除 B 时 B 的调用仍使用旧绑定，之后该调用和全部相关义务完成且没有下一次业务输入
- **THEN** B 的实际 owner 在完成通知驱动下最终退役，store lease／维护组件／登记收敛；不能因私有调用登记在另一个 shell 上提前关闭，也不能因缺下一次 turn 永久滞留

#### Scenario: 不同名字反复增删后资源收敛

- **WHEN** 连续发布不断使用新名字的 agent，并移除旧名字，相关工作均已实际结束
- **THEN** 实例、租约、维护 goroutine 和诊断条目收敛到当前路由及真实未决义务所需规模，不随历史名字总数增长

#### Scenario: 同名关闭中重入不产生第二 writer

- **WHEN** 某名字的 owner 已进入关闭或 poisoned 状态，此时候选尝试重新加入该名字
- **THEN** 候选明确拒绝；关闭尚未开始且仍存活时复用原 owner，确认完整关闭后才按原恢复协议重新取得唯一 writer

#### Scenario: 组织关闭覆盖热新增 owner

- **WHEN** 组织包含冷启动及多次热新增的 owner，并执行最终 Close
- **THEN** 新发布及新入口停止，所有相关 owner 按依赖序收尾；共享资源只由其唯一关闭责任方处理一次，未收敛和错误到达宿主


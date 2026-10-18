# config-state-twinship delta（01-state-binding）

> 复用登记（非规范，不改本条语义）：state 借出的模型/工具为框架原生 `model.Model`/`tool.Tool`/`tool.ToolSet`；插件管线为框架 `plugin` 回调的有序链；不建同形影子结构、不建第二份可写事件历史。
> 本 delta 覆盖「生效执行绑定由 state 持有」「config→plan→govern→state 管道」，并新增「session 身份与授权」「session mailbox 记账与执行占用」「候选活绑定准备与同代借用」；本能力其余义务由主 spec `openspec/specs/config-state-twinship/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 生效 system prompt 由 state 持有

AgentState SHALL 保存 agent 当前生效的 system prompt（由 resolve 在 Apply 时写入，nil resolve 取声明原文），模型请求读取该保存值；JSON 快照保存并恢复该值。

生效 system prompt 是 state 持有的**生效执行绑定**的一员。state SHALL 持有其**结构绑定与文件来源**（哪些文件、以何顺序组装）并按请求借出；**文件内容 SHALL 在请求边界按来源读取，并固化该次请求实际使用的版本**，MUST NOT 把「结构代」等同于「文件内容版本」。执行器 MUST NOT 自持第二份生效绑定作为真源。

模型、工具与插件管线 SHALL 同代借出；**文件内容 MUST NOT 被无条件限定为与模型/工具/插件同一发布代**——文件可热读更新而不换代，**结构 generation 可保持不变**。历史快照重放 MUST NOT 阻止新请求读到更新后的文件内容。

#### Scenario: 声明变更经 Reload 落入 state
- **WHEN** 配置含 skill/prompt 且 ReloadConfig 提交
- **THEN** owner.SystemPrompt() 等于组装结果，模型轨迹中的 SystemPrompt 与之相同；未 Reload 时仅改 registry 内容不改变请求所用 prompt

#### Scenario: 快照恢复带出生效值
- **WHEN** 保存快照后另一进程 LoadState
- **THEN** 恢复的 SystemPrompt 为快照时的生效值，无需 registry 即可读取

#### Scenario: 文件内容请求边界固化且不换代

- **WHEN** 一次请求在请求边界按来源读取到某文件内容
- **THEN** 该请求 SHALL 固化并使用本次读到的实际内容版本；随后的新请求 SHALL 读到更新后的内容，**结构 generation 可不变**

#### Scenario: 历史快照重放不阻文件更新

- **WHEN** 重放一份历史快照后又发起新请求
- **THEN** 新请求 MUST 按来源读到当前文件内容，MUST NOT 被历史快照的内容版本束缚

#### Scenario: 在途请求模型/工具/插件同代而文件可更新

- **WHEN** 一个请求已取得某代模型/工具/插件管线
- **THEN** 三者 SHALL 同属一代、MUST NOT 跨代拼接；而文件内容 MUST NOT 被强制钉在该发布代

### Requirement: config→plan→govern→state 管道

配置变更 SHALL 先构建 StatePlan（声明面 Change、Adds/Updates/Removes、下一版生效 prompt），再依序经 Eval→Benchmark→ApplyGate，全过才提交 state；任一段拒绝则 state 保持旧态。

**发布前置契约**（由 00.3 先行审定）SHALL 声明哪些治理检查为**必需**：**必需检查缺失时 MUST 拒绝发布**，MUST NOT 视作恒真通过；**不适用或被显式禁用的检查 SHALL 显式记录豁免理由与范围**，MUST NOT 计入「证据通过」。本管道只判定**能否发布**：发布机制的确定性验证（01.5）可注入**真实政策的确定性替身**做机制测试，MUST NOT 把替身结果当作**正式质量结论**；**正式质量评估归 07.4**（结构评估门），MUST NOT 因此新增 07.4→01.5 的回环。

StatePlan SHALL 携带每 agent 的候选活绑定与候选资源登记（`Leases`）；任一段拒绝或候选构造失败时，state SHALL 保持**旧态不变**并按构造逆序释放**候选独有**资源；共享面（store/session/TaskManager）与**既有在线生效资源** SHALL NOT 被回收。

#### Scenario: 缺必需检查即拒绝发布

- **WHEN** 以空 Governance（未声明任何必需检查）提交合法变更
- **THEN** MUST 依发布前置契约**拒绝发布**（缺必需证据），state 保持旧态，MUST NOT 报「三段恒真通过」

#### Scenario: 不适用或禁用检查显式记录

- **WHEN** 某治理检查对本次变更为不适用或被显式禁用
- **THEN** 该检查 SHALL 显式记录豁免理由与范围，MUST NOT 计入发布判定的「证据通过」

#### Scenario: 任一段拒绝

- **WHEN** Eval/Benchmark/ApplyGate 任一返回错误
- **THEN** 定义、生效 prompt 与现场不变；**候选独有资源按构造逆序释放**，既有在线资源与共享面不受影响，错误上抛

#### Scenario: 段序与计划内容
- **WHEN** 注入记录顺序的治理段
- **THEN** 调用顺序为 Eval→Benchmark→ApplyGate，三段收到同一计划的 Adds/Updates/Removes/Prompts

#### Scenario: 拒绝后候选独有资源逆序释放

- **WHEN** 候选已构造部分活绑定后被治理段拒绝
- **THEN** 该候选独有资源按构造逆序释放且每件一次，共享 store/session/TaskManager 不受影响

## ADDED Requirements

### Requirement: session 身份与授权

state SHALL 以应用显式选择的 `(AgentID, SessionID)` 建立并持有 session 现场；AgentID 为逻辑身份、InstanceID 区分同名重建。系统 MUST NOT 以「最近会话」等方式推断投递目标。投递与读取 SHALL 先按可信主体授权过滤，未授权读取/投递 SHALL 被具名拒绝。

**实例身份与关闭**：通知/投递的实例所有者 SHALL 区分**源实例（source instance，任务发起所属实例）**与**目标实例（target instance，Handoff 后的接收实例）**；实例退役/关闭时，其**已接纳未交付**的义务 SHALL 保留并由可恢复路径承接，MUST NOT 因实例被 `removed` 而直接丢弃。同一逻辑身份重建的新实例 MUST NOT 接收旧实例晚到的结果。

#### Scenario: 显式寻址投递

- **WHEN** 应用以明确 `(AgentID, SessionID)` 投递输入
- **THEN** 输入 SHALL 进入该 session 的 mailbox，MUST NOT 被路由到按最近活动推断的其他 session

#### Scenario: 未授权读取被拒

- **WHEN** 一个主体请求读取未授权 session 的事件
- **THEN** SHALL 返回具名授权错误，不返回任何该 session 内容

#### Scenario: session 不可用时降级

- **WHEN** 投递目标的 session 尚未建立
- **THEN** 系统 SHALL 按显式契约建立或具名拒绝，MUST NOT 静默落到默认 session

#### Scenario: 退役实例未交付义务保留

- **WHEN** 一个实例退役而仍有已接纳未交付的义务
- **THEN** 该义务 SHALL 保留并由可恢复路径承接，MUST NOT 因 `removed` 而直接丢弃

#### Scenario: 同名重建不接收旧实例结果

- **WHEN** 一个实例退役后以同名重建
- **THEN** 新实例 MUST NOT 接收旧实例晚到的结果

### Requirement: session mailbox 记账与执行占用

state SHALL 分别记账 mailbox 的**接纳**（acceptSeq 单调序）、**认领**（pending/claimed）与**消费完成**（consumeReceipt）三个时点；同 session 的执行占用 SHALL 受管（同一 session 最多一个有效模型决策）。接纳、取消与结果记账 MUST NOT 等待慢模型。

#### Scenario: 三时点分别可辨

- **WHEN** 一条输入被接纳、随后被认领并最终消费完成
- **THEN** 接纳序、认领态与消费完成回执 SHALL 分别可读且互不覆盖

#### Scenario: 慢模型不阻塞接纳

- **WHEN** 一次模型决策仍在进行而新输入到达
- **THEN** 新输入 SHALL 立即被接纳（重新分配 acceptSeq），MUST NOT 阻塞等待当前决策结束

#### Scenario: 同一 session 执行占用

- **WHEN** 同一 session 已有一个有效模型决策在执行
- **THEN** 新到输入 SHALL 排队至下一轮消费，MUST NOT 与该决策并发发起第二次模型决策

### Requirement: 候选活绑定准备与同代借用

组合根 SHALL 在候选私有的准备期构造原生模型/工具/插件**活绑定**（`AgentBinding`）并登记候选资源（`Leases`）；构造期间 MUST NOT 修改任何在线生效绑定或常驻状态。发布成功后 state SHALL 持有生效绑定；一次请求 SHALL 取得**同代**模型、工具及**有序**插件管线，并在其生命周期内继续持有该代；旧请求 MUST NOT 在运行中被换入新代。

#### Scenario: 候选私有构造不触在线状态

- **WHEN** 候选正在准备活绑定
- **THEN** 在线生效绑定、共享投影与 TaskManager 侦测 SHALL 保持不变，且请求仍以短锁取得旧代

#### Scenario: 请求借出同代有序插件管线

- **WHEN** 一个请求在请求边界取得生效绑定
- **THEN** 其模型、工具与插件管线 SHALL 同属一代；插件顺序 SHALL 与发布时一致

#### Scenario: 在途请求持旧代跑完

- **WHEN** 一个请求已取得某代绑定，随后发生新代发布
- **THEN** 该请求 SHALL 持其开始时取得的旧代跑完，MUST NOT 中途拼接新代

#### Scenario: 候选失败释放候选独有资源

- **WHEN** 候选活绑定构造中途失败
- **THEN** 已获取的独占资源 SHALL 按实际获取顺序逆序释放且每件一次，在线绑定与共享资源不变

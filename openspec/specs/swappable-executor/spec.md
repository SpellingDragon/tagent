# swappable-executor Specification

## Purpose

执行器（fwAgent+runner+tools 装配）=配置可重导出的无状态函数，经 cm 内可换执行器缝（SwapExecutor）非重启原子换入：懒检查先序（memory canonical diff 拒绝→fingerprint 检测）、build-validate-then-swap fail-closed、drain-free turn 级、副作用 ownership 表、代际日志与 ring 2 回滚。org 级基础设施（loop/bus/cm/projection/TaskManager/monitor）常驻不换。


## Requirements

### Requirement: 执行器可热换（cm.runner 级换代，非重启）

执行器（fwAgent+runner+其 tools 装配）SHALL 是「配置→turn 行为」的可重导出函数，经 **cm 内可换执行器缝**原子换入：`SwapExecutor(newRunner)` 在 RWMutex 下原子替换；`RunFlow` 每 turn 取当前 runner 引用（**drain-free turn 级**：in-flight turn 用旧 runner 跑完）。**TagentAgent/事件循环/bus/cm/projection/TaskManager/monitor/org 级基础设施 SHALL 常驻不换**（宿主入口 StartLoop/InjectMessage/outputCh 零变化）。结构热更 SHALL NOT 依赖进程重启。

#### Scenario: 结构热更非重启生效

- **GIVEN** 运行中 agent，配置的 tools/subagents/prompt wiring 变更（fingerprint 变化）
- **WHEN** Reload 触发并成功（SwapExecutor）
- **THEN** **下一个 turn** 起用新 runner（新工具集生效），进行中 turn 用旧 runner 跑完
- **AND** 进程/事件循环/投影（R1）/任务板（org 级 TaskManager）/常驻会话 SHALL 原封不动

#### Scenario: 行为变更零重建

- **GIVEN** 仅 model provider/MCP servers/prompt 内容/阈值等行为面变更（fingerprint 不变或可热应用子集）
- **WHEN** 懒检查触发
- **THEN** SHALL 走既有细粒度热同步（SwappableModel/MCP registry/prompt.Source/ApplyOrgParams），SHALL NOT 重建执行器代

### Requirement: fingerprint 检测与 memory 拒绝（懒检查先序）

配置变更检测 SHALL 以懒检查（既有 orgReloader 模式）为先序：mtime 变→**先独立 canonical diff（JSON）agents.*.Memory 段**→命中 SHALL 记 ERROR+「须重启」通知并 return（memory 被 fingerprint 白名单排除，若不先检则静默不生效）；未命中→`computeOrgFingerprint` 白名单比对→变化→结构热更路径；不变→仅 ApplyOrgParams。

#### Scenario: tools 增删热生效

- **GIVEN** 配置新增一个工具引用
- **WHEN** fingerprint 变化触发 Reload 成功
- **THEN** 后续 turn 的 LLM 工具声明集含新工具，历史事件照常渲染（不可变事实，R1 红利）

#### Scenario: memory 变更拒绝热更（检测可达）

- **GIVEN** 配置仅变更 agents.*.memory.*（fingerprint 不变）
- **WHEN** 懒检查触发（mtime 变）
- **THEN** memory 先序检测命中→SHALL NOT 换 runner，SHALL 记 ERROR 并通知「该配置须重启生效」（fail-closed；不依赖 fingerprint 变化路径）

### Requirement: build-validate-then-swap 可逆（fail-closed，副作用 ownership）

Reload SHALL 先完整构建并校验新执行器装配（LoadConfig 校验 + fwAgent/runner/tools 构造）：**失败 SHALL fail-closed**——旧 runner 原样服务、记 ERROR 与通知，SHALL NOT 半换。换代 SHALL 按副作用 ownership 表执行：进程级共享物（memStore/engine/govLedger/goals/evoGit/MCP registry/hintTracker）复用不重建、**SHALL NOT 重复 RegisterCloser/AddChannel**；仅重建代级装配（fwAgent/runner/tools/prompt）。每次成功换入 SHALL 记代际日志（代次、fingerprint、时间戳、变更面），保留上一代配置摘要（ring 2）支持 `Rollback()`。

#### Scenario: 新配置非法不换

- **GIVEN** 新配置含未知工具引用（LoadConfig 校验失败）
- **WHEN** Reload
- **THEN** 旧 runner 原样服务（零中断），记 ERROR+通知，当前 turn 与后续 turn 不受影响

#### Scenario: 回滚到上一代

- **GIVEN** 已成功换 runner 一次（gen 2），发现行为回退
- **WHEN** Rollback()
- **THEN** SHALL 按上一代（gen 1）配置 Reload 回 gen 1 等价装配（记 gen 3 日志标注 rollback-of-1）

### Requirement: 触发时机与观测

热更检测 SHALL 懒触发（既有 orgReloader 每次LLM 调用前检查 mtime/fingerprint 的模式扩展），SHALL NOT 常驻 watcher。热换结果（成功换代/拒绝/回滚）SHALL 可观测（日志+代际事件入 evolution 事件流），行为面热同步（A 面）与结构换代（B 面）SHALL 收敛为统一触发与统一日志前缀。

#### Scenario: 懒检测触发换代

- **GIVEN** 运行中 agent，配置文件被编辑
- **WHEN** 下一次 LLM 调用前的懒检查发现 fingerprint 变化
- **THEN** SHALL 执行 Reload（本 turn 仍用旧 runner——检测在 BeforeModel，换代对下一 turn 生效；或实现为检测后立即换 runner 但当前 turn 已持有的引用不变，二者语义等价取实现简者）

#### Scenario: 中途换工具集安全（历史引用容忍）

- **GIVEN** 对话历史含工具 X 的调用记录，热更后声明集移除 X
- **WHEN** 后续 turn 渲染历史
- **THEN** 历史 tool_call/tool_result SHALL 照常作为不可变事实渲染；SHALL 经 CONFIRM 实验确认主流 provider 容忍（不容忍则文档化约束：移除工具需其历史已被折叠）

### Requirement: 退役执行器与模型延迟回收

SwapExecutor 换代时，跌出 ring-2 回滚窗口的旧 runner SHALL 在无 in-flight turn 引用后被 Close（若实现 Closer 语义）；SwappableModel.Swap 换下的旧 model SHALL 以同型 in-flight 计数延迟 Close。Close SHALL 幂等。回收 MUST NOT 影响回滚能力（ring 内代际不 Close）。

#### Scenario: 连续三次热更后的资源回收

- **WHEN** 执行器连续换代三代（第一代已跌出 ring-2）且期间无未完成 turn
- **THEN** 第一代 runner 的 Close 被调用且仅一次，ring-2 内两代不被关闭

### Requirement: Rollback 手动触发面

SwapExecutor 的 Rollback 能力 SHALL 具备生产可达的手动触发面（进程信号或宿主管理命令），触发后按 ring-2 上一代配置重建并换回，行为与自动回滚一致且留痕日志。

#### Scenario: 运维发信号回滚上一代

- **WHEN** 常驻进程收到约定的回滚信号
- **THEN** 按 ring-2 快照重建执行器并 Swap 回，日志记录代际与指纹，后续 turn 使用上一代配置

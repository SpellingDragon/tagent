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

配置变更 SHALL 懒检查 mtime；解析后先将 agents.*.Memory canonical 指纹与当前 effective 比较，变化即拒绝热迁移、保留当前代并明确须重启，SHALL NOT 因拒绝而推进 effective 指纹。其余结构变化先 build-validate-then-swap，数值变化按成功执行代逐 agent 应用，不与结构分支互斥。

#### Scenario: tools 增删热生效
- **WHEN** 工具引用变化且构建验证通过
- **THEN** 下一 turn 使用新声明集，历史 tool_call/result 不被改写，同批数值配置也生效

#### Scenario: memory 变更拒绝热更（检测可达）
- **WHEN** 仅 memory 变化或同一未生效 memory 再次随其他字段编辑
- **THEN** 每次都以 effective 为比较基准拒绝热迁移，旧资源不变且给出通知

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

组织懒检查 SHALL 在顶层业务 turn 开始、重试循环外触发单飞构建请求，不在调用线程执行长解析、恢复或资源关闭，不保持在每次 BeforeModel 内。该 turn SHALL 获取当时已发布 effective；候选成功发布之后开始的 turn 才保证使用新代。构建调度 SHALL 合并重复请求，不无限积累候选。

ops/test 的显式 CheckOrgReload 与 Rollback SHALL 保持同步管理语义，通过同一协调器等待本次请求的提交／拒绝结果，不阻塞其他业务请求取得旧 effective，不形成第二生效路径。结果 SHALL 在原日志／诊断面呈现 desired、effective generation、拒绝原因与未收敛引用。

#### Scenario: 懒检测不等待候选构建

- **GIVEN** 配置文件被编辑，候选构建停在可控屏障
- **WHEN** 一个业务 turn 的起点懒检查发现变化
- **THEN** 该 turn 及随后在发布前开始的请求立即使用旧 effective，不等待构建；解除屏障并成功发布后开始的 turn 使用新代，失败则仍旧代且错误可见

#### Scenario: 手动检查同步等待但不封住业务获取

- **WHEN** 运维同步调用 CheckOrgReload，候选尚未完成
- **THEN** 管理调用等待结果，业务请求仍能取得旧 effective；管理调用成功返回后开始的 turn 使用新代

#### Scenario: 进行中不再重复检查

- **WHEN** 一个业务 turn 的多次 LLM 迭代期间配置再次变更
- **THEN** 本 turn 不中途切换绑定；后续 turn 的起点检查可调度变更，成功发布之后开始的 turn 使用新版，不强制发现变更的 turn 等待构建

### Requirement: 退役执行器与模型延迟回收

ring-2 SHALL 保留配置快照以支持重建回滚，不以存活 runner 作为回滚真源。旧 runner 在无 in-flight turn 后 SHALL 由 owner 幂等 Close。SwappableModel 的 in-flight SHALL 覆盖返回流关闭/取消之前的完整生命周期，而非仅 GenerateContent 函数调用；error/nil-stream SHALL 释放租约。当前在用或被重新选中的实例 SHALL NOT 被退役清扫，借用的模型 SHALL NOT 被非 owner 关闭。

#### Scenario: 连续三次热更后的资源回收
- **WHEN** 执行器连续换代且旧 turn 均完成
- **THEN** 非在用旧 runner 恰关闭一次，配置快照仍可用于回滚，不要求 ring 内旧 runner 存活

#### Scenario: 流未结束时换模型
- **WHEN** GenerateContent 已返回 channel，但旧流仍在发送，随后 Swap
- **THEN** 旧 model 不被关闭，所有响应继续可读，流结束/取消后才释放并回收

#### Scenario: 重新选择仍在用实例
- **WHEN** A→B→A 且 A 尚有在飞流
- **THEN** A 不被旧退役记录错误关闭，最终各 owner 只关闭一次

### Requirement: Rollback 手动触发面

执行器换代能力（单 owner 走 `PublishExecutor`；组织走 `StageExecutor → ActivateExecutor`；二者共用同一条线性化，只换 runner 而不换执行面的独立入口已废除）的 Rollback SHALL 具备生产可达的手动触发面（进程信号或宿主管理命令），触发后经唯一版本协调器将上一份完整有效配置重新构造并发布为新 generation（记 rollback 来源日志），行为与普通热更共用构建、校验和发布路径，仅影响之后开始的调用。

#### Scenario: 运维发信号回滚上一代

- **WHEN** 常驻进程收到约定的回滚信号
- **THEN** 按上一份有效配置重建执行绑定并发布新序号，日志记录代际与指纹，后续请求使用上一代配置

### Requirement: 逐 agent 执行代与有效配置一致

热更 SHALL 按 agent 身份绑定其常驻 store/session/projection/task，而不是给所有子树复用 entry store。新拓扑准备失败 SHALL 保持上一代；成功时数值与结构 SHALL 同批作用于新代真实对象，日志/回执 SHALL 回读 effective 并列 desired、generation、held/rejected。删除配置字段 SHALL 回归默认值。memory 拒绝 SHALL 不推进 effective 指纹；回滚 SHALL 同时恢复上一成功配置的结构与数值。

#### Scenario: 多 agent 混合变更
- **WHEN** entry 与两个子 agent 同次变更工具、模型和预算
- **THEN** 各自真实请求使用对应参数，各写入原命名空间，投影与任务板不因验证壳丢失

#### Scenario: 内存配置反复被拒绝
- **WHEN** 未生效 memory 配置保留在文件中并再次编辑其他字段
- **THEN** 仍拒绝热迁移，effective memory 指纹不变化，不以第二次检查绕过拒绝

#### Scenario: 首次结构换代后回滚
- **WHEN** 首次换代成功后主动 Rollback
- **THEN** 按启动代配置重建且恢复数值参数，行为与启动代等价，现有在飞 turn 不受影响

### Requirement: 整份编排执行绑定发布

执行器换代 SHALL 以整份编排执行绑定为单位发布：候选包含全部受影响 agent 的执行配置、工具声明与子调用目标，全部构造校验成功后一次提交；MUST NOT 以逐 agent 部分发布造成新旧绑定混用。发布后旧绑定保持可用直到其引用的实际调用、响应流与派生后台执行停止，随后按既有退役路径回收；发布与顶层 ACK 本身 MUST NOT 关闭旧资源。

#### Scenario: 发布不提前关闭旧资源

- **WHEN** G2 发布时 G1 仍有响应流未排空或后台任务未结束
- **THEN** G1 所需绑定与资源保持，实际停止后恰好释放一次

### Requirement: 单一应用记录与请求获取一致

有效配置、执行 binding、本地 owner 视图、热参、revision/generation 和成功回执 SHALL 从同一次提交记录读取。新工作取得该记录和使用引用 MUST 与发布／关闭互斥，不留先读后加引用的回收空窗。构造、I/O、恢复激活的重活与资源 Close MUST 不进入业务获取使用的短临界区；多个 setter、Add 和 runner 换入仅相邻执行不能视为原子发布。

#### Scenario: 混合候选的提交屏障

- **WHEN** 同时修改工具和子 agent 热参，候选停在真正提交之前
- **THEN** 新普通请求与消费边界仍读取旧已提交源，新 owner 不可路由；解除屏障后，新请求及诊断取得同一完整新记录，失败不提前改变热参

#### Scenario: 关闭与后到候选竞争

- **WHEN** 构建中开始组织 Close，候选随后准备完成
- **THEN** 候选被丢弃而不发布，新 Run／输入被拒绝，已有调用继续按停止合同收尾，候选独有资源恰一次退出


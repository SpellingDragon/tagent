## MODIFIED Requirements

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

### Requirement: fingerprint 检测与 memory 拒绝（懒检查先序）

配置变更 SHALL 懒检查 mtime；解析后先将 agents.*.Memory canonical 指纹与当前 effective 比较，变化即拒绝热迁移、保留当前代并明确须重启，SHALL NOT 因拒绝而推进 effective 指纹。其余结构变化先 build-validate-then-swap，数值变化按成功执行代逐 agent 应用，不与结构分支互斥。

#### Scenario: tools 增删热生效
- **WHEN** 工具引用变化且构建验证通过
- **THEN** 下一 turn 使用新声明集，历史 tool_call/result 不被改写，同批数值配置也生效

#### Scenario: memory 变更拒绝热更（检测可达）
- **WHEN** 仅 memory 变化或同一未生效 memory 再次随其他字段编辑
- **THEN** 每次都以 effective 为比较基准拒绝热迁移，旧资源不变且给出通知

## ADDED Requirements

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

## ADDED Requirements

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

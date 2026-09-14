## ADDED Requirements

### Requirement: tmux 可用性真实探测

IsTmuxAvailable SHALL 反映系统真实状态（经 PATH 探测 tmux 二进制），MUST NOT 恒返回 true；依赖该判定的降级分支（如受限模式提示）SHALL 因真实探测而获得实际语义。构造器 NewTmuxExecutor 的「永不返回 nil」现状与探测函数的混淆 SHALL 消除（探测独立于构造）。

#### Scenario: 系统无 tmux 时的降级

- **WHEN** 运行环境 PATH 中无 tmux 二进制
- **THEN** IsTmuxAvailable 返回 false，ActionTool 走可用性降级路径并向模型如实说明，而非在首次执行时才失败

## Why

config 与 state 目前只有单向引用：声明里的 SystemPrompt 只是配置文本，请求时由 registry 现拼，state 没有保存 agent 实际生效的 system prompt，其他派生运行态同样缺位。用户要求 config 与 state 一体双生：config change → state change → govern(eval/benchmark/apply-gate)，治理机制恒真 mock，但管道与计划必须真实存在。

## What Changes

- AgentState 新增生效 system prompt 运行态：Apply 时经 resolve（默认取声明原文，ReloadConfig 注入 registry 组装）写入；模型请求读取 state 保存值，不再每次现拼；JSON 快照保存并恢复该值。
- 引入 StatePlan：一次配置变更引发的 state 变更计划（新增/更新/移除定义、受影响 agent 的下一版生效 prompt、声明面 Change）。
- 引入 Governance 三段管道 Eval→Benchmark→ApplyGate（func(StatePlan) error，nil 段恒真，默认全恒真）；任一段拒绝则整体不提交，state 保持旧态。原 Apply 的单回调 govern(Change) error 废弃。
- **BREAKING（prototype 内部）**：`State.Apply(cfg, resolve, gov)` 与 `ReloadConfig(path, state, registry, gov)` 签名变更，既有集成测试同步迁移。
- README 增补一体双生管道说明。

## Capabilities

### New Capabilities
- `config-state-twinship`: config→plan→govern→state 管道契约与生效 system prompt 运行态。

### Modified Capabilities
无生产主规格改动。

## Impact

仅 prototype 的 contracts.go/state.go/config.go、两份集成测试与 README。不引入真实评估/基准实现（恒真 mock 占位）；不做生产审批、灰度或回滚系统。

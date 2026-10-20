# action-tool-config Specification

## Purpose

本规范定义 action-tool-config 能力。ActionProperties SHALL accept a `monitor` configuration map that overrides DefaultMonitorConfig values.

> **状态核验（2026-09-09，部分有效）**：`monitor` 配置部分**有效**——`properties.monitor`（dense_interval/dense_duration/backoff_factor/max_interval）经 actionFactory 解析并注入 TmuxMonitor（yaml 示例见根 README 与 agent-architecture §6.4）。本规范的 `compress` 子配置部分（`max_tool_result_chars`/`max_exec_state_chars`/`chunk_size`/`chunk_summary_len`，tool 级压缩超参覆盖）**已退役**——压缩超参现由 agent 级 `compress` 块（`config.go` CompressConfig：summary_model/card_max_chars/compact_keys_listed/recent_full_count/summary_max_tokens）统一承载，不再提供 tool 级覆盖（单压缩管线在 entry agent，tool 级覆盖无消费方）。compress 相关 SHALL 条款不约束当前实现。

## Requirements

### Requirement: Monitor configuration is exposed via ActionProperties

ActionProperties SHALL accept a `monitor` configuration map that overrides DefaultMonitorConfig values. Supported fields: `interval` (duration string), `stable_duration`, `interactive_stable_duration`, `fake_dead_duration`. These values SHALL be parsed by `MonitorConfigFrom` into a `MonitorConfig`; a missing key SHALL keep its DefaultMonitorConfig value, and an unknown key SHALL be ignored rather than refused. `monitor` 超参 SHALL 在 exec/tmux 执行路径上按**当次请求借用代**的绑定消费（节奏随代生效，不挂在 agent 私有 `Config`）；无 tmux 环境下的**降级路径** SHALL 真实可观测（`monitor` 配置存在不得使降级失败或被静默吞掉），配置传递链 MUST NOT 依赖恒真探测。

#### Scenario: Custom monitor interval via YAML

- **WHEN** ActionProperties YAML specifies `monitor.interval: "10s"`
- **THEN** TmuxMonitor SHALL poll every 10 seconds instead of the default 30 seconds

#### Scenario: Default config when monitor not specified

- **WHEN** ActionProperties YAML does not include a `monitor` section
- **THEN** DefaultMonitorConfig values SHALL be used (interval=30s, stable_duration=60s, etc.)

#### Scenario: 无 tmux 降级路径可观测

- **WHEN** 运行环境无 tmux 且 ActionProperties 配置了 `monitor`
- **THEN** 系统 SHALL 走可用性降级路径并如实呈现（不因存在 `monitor` 配置而失败或静默吞配置）
- **AND** 该降级判定 MUST NOT 依赖恒真的可用性探测

### Requirement: tool 级 compress 配置不被消费

ActionProperties SHALL accept a `compress` configuration map with fields: `max_tool_result_chars`, `max_exec_state_chars`, `chunk_size`, `chunk_summary_len`, but MUST NOT consume it: 该键 MUST NOT 改变任何压缩行为，压缩超参只由 agent 级配置承载；不因该键存在而报错，也不得据此建立第二套 tool 级压缩路径。

#### Scenario: tool 级 compress 键不被消费

- **WHEN** ActionProperties 提供 `compress.max_tool_result_chars` 等键
- **THEN** 该键 MUST NOT 改变任何压缩行为（压缩超参只由 agent 级配置承载）
- **AND** 不因该键存在而报错、也不得据此恢复第二套 tool 级压缩路径

### Requirement: tmux 可用性真实探测

IsTmuxAvailable SHALL 反映系统真实状态（经 PATH 探测 tmux 二进制），MUST NOT 恒返回 true；依赖该判定的降级分支（如受限模式提示）SHALL 因真实探测而获得实际语义。`IsTmuxAvailable` 的真实性以**真实 PATH 探测**为准，MUST NOT 以构造成功冒充可用；无 tmux 时降级路径 SHALL 在 04.2 的资源/停止凭证断言中可观测。

#### Scenario: 系统无 tmux 时的降级

- **WHEN** 运行环境 PATH 中无 tmux 二进制
- **THEN** IsTmuxAvailable 返回 false，ActionTool 走可用性降级路径并向模型如实说明，而非在首次执行时才失败

# action-tool-config Specification

## Purpose

本规范定义 action-tool-config 能力。ActionProperties SHALL accept a `monitor` configuration map that overrides DefaultMonitorConfig values.

> **状态核验（2026-09-09，部分有效）**：`monitor` 配置部分**有效**——`properties.monitor`（dense_interval/dense_duration/backoff_factor/max_interval）经 actionFactory 解析并注入 TmuxMonitor（yaml 示例见根 README 与 agent-architecture §6.4）。本规范的 `compress` 子配置部分（`max_tool_result_chars`/`max_exec_state_chars`/`chunk_size`/`chunk_summary_len`，tool 级压缩超参覆盖）**已退役**——压缩超参现由 agent 级 `compress` 块（`config.go` CompressConfig：summary_model/card_max_chars/compact_keys_listed/recent_full_count/summary_max_tokens）统一承载，不再提供 tool 级覆盖（单压缩管线在 entry agent，tool 级覆盖无消费方）。compress 相关 SHALL 条款不约束当前实现。

## Requirements

### Requirement: TmuxMonitor configuration is exposed via ActionProperties

ActionProperties SHALL accept a `monitor` configuration map that overrides DefaultMonitorConfig values. Supported fields: `interval` (duration string), `stable_duration`, `interactive_stable_duration`, `fake_dead_duration`. When provided, these values SHALL be passed to NewTmuxMonitor via WithMonitorConfig. When not provided, DefaultMonitorConfig values SHALL be used.

#### Scenario: Custom monitor interval via YAML

- **WHEN** ActionProperties YAML specifies `monitor.interval: "10s"`
- **THEN** TmuxMonitor SHALL poll every 10 seconds instead of the default 30 seconds

#### Scenario: Default config when monitor not specified

- **WHEN** ActionProperties YAML does not include a `monitor` section
- **THEN** DefaultMonitorConfig values SHALL be used (interval=30s, stable_duration=60s, etc.)

### Requirement: ActionProperties supports compress configuration

ActionProperties SHALL accept a `compress` configuration map with fields: `max_tool_result_chars`, `max_exec_state_chars`, `chunk_size`, `chunk_summary_len`. These values SHALL be passed to the agent's SmartCompressor when the action tool is associated with an agent. When not provided, SmartCompressor defaults SHALL be used.

#### Scenario: Tool-level compress override

- **WHEN** ActionProperties YAML specifies `compress.max_tool_result_chars: 1000`
- **THEN** the agent's SmartCompressor SHALL use 1000 as maxToolResultChars for this tool's results

### Requirement: tmux 可用性真实探测

IsTmuxAvailable SHALL 反映系统真实状态（经 PATH 探测 tmux 二进制），MUST NOT 恒返回 true；依赖该判定的降级分支（如受限模式提示）SHALL 因真实探测而获得实际语义。构造器 NewTmuxExecutor 的「永不返回 nil」现状与探测函数的混淆 SHALL 消除（探测独立于构造）。

#### Scenario: 系统无 tmux 时的降级

- **WHEN** 运行环境 PATH 中无 tmux 二进制
- **THEN** IsTmuxAvailable 返回 false，ActionTool 走可用性降级路径并向模型如实说明，而非在首次执行时才失败

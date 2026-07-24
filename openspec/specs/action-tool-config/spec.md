# action-tool-config Specification

## Purpose

本规范定义 action-tool-config 能力。ActionProperties SHALL accept a `monitor` configuration map that overrides DefaultMonitorConfig values.

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

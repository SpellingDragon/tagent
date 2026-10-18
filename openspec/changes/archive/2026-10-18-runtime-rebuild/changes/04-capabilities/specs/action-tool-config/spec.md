# action-tool-config（04-capabilities 子变更 delta）

> 本文件为二级子变更 `04-capabilities` 对既有主 spec `openspec/specs/action-tool-config/spec.md` 的 delta（主 spec **只读**）。
> 覆盖映射：`action-tool-config` → `04.2`（处置 = **M**，见 `../../../../coverage.md`）。
> `MODIFIED` 段为**完整源 Requirement 全量拷贝 + 本波新增约束**；未列出者视为继承主 spec 现行有效约束。**保留原 source 能力名**写 delta——已修改的机制按原能力名 MODIFIED，不折入他 cap 名称。

## Purpose

定义 action 工具（exec/tmux）的配置面在重建运行时下的语义：`monitor` 超参（TmuxMonitor 节奏）在 exec/tmux 执行路径上**按 state 绑定代**消费、无 tmux 时降级路径可观测；`IsTmuxAvailable` 真实探测独立于构造。`compress` 子配置段退役**显式化**，不恢复 tool 级压缩覆盖。

## MODIFIED Requirements

### Requirement: TmuxMonitor configuration is exposed via ActionProperties

ActionProperties SHALL accept a `monitor` configuration map that overrides DefaultMonitorConfig values. Supported fields: `interval` (duration string), `stable_duration`, `interactive_stable_duration`, `fake_dead_duration`. When provided, these values SHALL be passed to NewTmuxMonitor via WithMonitorConfig. When not provided, DefaultMonitorConfig values SHALL be used. **本波新增**：`monitor` 超参 SHALL 在重建的 exec/tmux 执行路径上按**当次请求借用代**的绑定消费（节奏随代生效，不挂在 agent 私有 `Config`）；无 tmux 环境下的**降级路径** SHALL 真实可观测（`monitor` 配置存在不得使降级失败或被静默吞掉），配置传递链 MUST NOT 依赖恒真探测。

#### Scenario: Custom monitor interval via YAML

- **WHEN** ActionProperties YAML specifies `monitor.interval: "10s"`
- **THEN** TmuxMonitor SHALL poll every 10 seconds instead of the default 30 seconds

#### Scenario: Default config when monitor not specified

- **WHEN** ActionProperties YAML does not include a `monitor` section
- **THEN** DefaultMonitorConfig values SHALL be used (interval=30s, stable_duration=60s, etc.)

#### Scenario: 无 tmux 降级路径可观测（本波新增）

- **WHEN** 运行环境无 tmux 且 ActionProperties 配置了 `monitor`
- **THEN** 系统 SHALL 走可用性降级路径并如实呈现（不因存在 `monitor` 配置而失败或静默吞配置）
- **AND** 该降级判定 MUST NOT 依赖恒真的可用性探测

### Requirement: ActionProperties supports compress configuration

ActionProperties SHALL accept a `compress` configuration map with fields: `max_tool_result_chars`, `max_exec_state_chars`, `chunk_size`, `chunk_summary_len`. These values SHALL be passed to the agent's SmartCompressor when the action tool is associated with an agent. When not provided, SmartCompressor defaults SHALL be used. **本波修改**：该 tool 级 `compress` 子配置段**退役**——单压缩管线在 agent 级，`ActionProperties` 的 `compress` 键 MUST NOT 被消费、MUST NOT 改变任何压缩行为；重建 MUST NOT 为兼容恢复 tool 级压缩超参。

#### Scenario: tool 级 compress 键不被消费（本波新增）

- **WHEN** ActionProperties 提供 `compress.max_tool_result_chars` 等键
- **THEN** 该键 MUST NOT 改变任何压缩行为（压缩超参只由 agent 级配置承载）
- **AND** 不因该键存在而报错、也不得据此恢复第二套 tool 级压缩路径

### Requirement: tmux 可用性真实探测

IsTmuxAvailable SHALL 反映系统真实状态（经 PATH 探测 tmux 二进制），MUST NOT 恒返回 true；依赖该判定的降级分支（如受限模式提示）SHALL 因真实探测而获得实际语义。构造器 NewTmuxExecutor 的「永不返回 nil」现状与探测函数的混淆 SHALL 消除（探测独立于构造）。**本波新增**：重建执行路径下该探测 SHALL 保持独立于构造，`IsTmuxAvailable` 的真实性以**真实 PATH 探测**为准，MUST NOT 以构造成功冒充可用；无 tmux 时降级路径 SHALL 在 04.2 的资源/停止凭证断言中可观测。

#### Scenario: 系统无 tmux 时的降级

- **WHEN** 运行环境 PATH 中无 tmux 二进制
- **THEN** IsTmuxAvailable 返回 false，ActionTool 走可用性降级路径并向模型如实说明，而非在首次执行时才失败

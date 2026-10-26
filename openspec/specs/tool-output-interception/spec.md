# tool-output-interception Specification

## Purpose
Prevent oversized tool outputs from consuming the agent's context window by wrapping all tools with an output size interception wrapper.

## Requirements

### Requirement: 工具输出大小拦截包装器

tagent SHALL 在 `NewTagentAgent` 中为所有工具添加输出大小拦截包装器（`OutputLimitTool`）。包装器 SHALL 拦截工具 `Call()` 方法的返回值，当序列化后的输出字符数超过阈值时，截断输出并附加错误提示信息。该包装器 SHALL 作为 **tagent 面类型经注入端口接入**（实现归 `workspace/externalize.go`，见 03.8），MUST NOT 新建**未登记的根 `tool` 包**（architecture.json 无该包，MUST NOT 放宽 policy 换绿）。

超大结果 SHALL 接入**统一外置路径**：通用工具、任务结果、tmux 输出与 overflow 输出 SHALL 走同一外置逻辑，MUST NOT 各自另造外置/截断路径。首次入模前 SHALL 先保存持久原文，再向模型呈现有界预览与分段入口（凭票据可经分段读取取回全文）；MUST NOT 直接截断而丢弃原文。清副本后原文仍有效时 SHALL 可重物化。

#### Scenario: 输出未超限正常返回

- **WHEN** 工具执行返回结果，序列化后字符数为 500，阈值为 8000
- **THEN** 原始结果正常返回，不截断

#### Scenario: 输出超限截断并附加错误

- **WHEN** 工具执行返回结果，序列化后字符数为 15000，阈值为 8000
- **THEN** 结果被截断为前 8000 字符
- **AND** 附加 `[ERROR: Tool output exceeded 8000 characters, truncated. Total: 15000 characters. Consider optimizing your command or using more specific queries.]`

#### Scenario: agent-kind 工具也被拦截

- **WHEN** agent-kind 工具（如 knowledge、recall）通过 AgentToolWrapper 返回超长结果
- **THEN** 包装器同样拦截并截断输出

#### Scenario: nil 结果不拦截

- **WHEN** 工具执行返回 nil
- **THEN** 原始 nil 结果正常返回，不触发拦截

#### Scenario: 统一外置路径先存原文

- **WHEN** 通用工具/任务结果/tmux/overflow 产出超大结果
- **THEN** SHALL 走同一外置路径：先保存持久原文，再给有界预览与分段入口，MUST NOT 直接丢弃原文

### Requirement: 拦截阈值与 MaxTokens 对齐

拦截阈值 SHALL 为 `MaxTokens / 2 * 4`（字符数），其中 MaxTokens 来自 TagentConfig.MaxTokens，1 token 估算为 4 字符。当 TagentConfig.MaxTokens 为 0 时，SHALL 使用默认值 8000（对应 16000 字符阈值）。

#### Scenario: 标准配置计算阈值

- **WHEN** TagentConfig.MaxTokens = 16000
- **THEN** 拦截阈值 = 16000 / 2 * 4 = 32000 字符

#### Scenario: MaxTokens 为 0 使用默认值

- **WHEN** TagentConfig.MaxTokens = 0
- **THEN** 使用默认 MaxTokens = 8000
- **AND** 拦截阈值 = 8000 / 2 * 4 = 16000 字符

### Requirement: OutputLimitTool 实现 trpctool.Tool 接口

`OutputLimitTool` SHALL 实现 `trpctool.Tool` 接口，包装内部工具的 `Definition()` 和 `Call()` 方法。`Definition()` SHALL 原样返回内部工具的定义。`Call()` SHALL 执行内部工具后拦截返回值。

#### Scenario: Definition 透传

- **WHEN** 调用 OutputLimitTool.Definition()
- **THEN** 返回内部工具的 Definition，不做修改

#### Scenario: Call 拦截返回值

- **WHEN** 调用 OutputLimitTool.Call(ctx, args)
- **THEN** 先调用内部工具的 Call(ctx, args)
- **AND** 对返回值进行序列化（json.Marshal）
- **AND** 检查字符数是否超过阈值
- **AND** 超限时截断并附加错误信息

### Requirement: 工作区副本物化与引用稳定

workspace 对同一持久原文的**每次物化**（materialization）SHALL 给出**稳定引用**：同一持久原文可被多次读取/外置，其物化引用 SHALL 稳定可定位，重复物化 MUST NOT 覆盖或丢弃既有引用。**跨 owner / 跨 tool / 跨 restart**（进程重启）的物化目标 MUST NOT 互相覆盖。

系统 SHALL **显式允许安全复用**：同一原文在需要时可复用已有副本路径，复用 MUST NOT 破坏上述稳定性。系统 MUST NOT 强制「同一原文全局唯一副本」，也 MUST NOT 以该要求把物化串行化到一把全局锁；**多个合法副本并存 SHALL 被允许**——不同 owner/tool 各自物化同一原文互不冲突。

原文 **blob 内容去重 SHALL NOT 等同于引用去重**：允许 blob 内容去重，但 `owner/format/保留策略` 不同的引用 MUST NOT 互覆盖（见 `event-segment-store` 与 `recall-protocol`「原文引用与分段读取保真」）。

副本缺失时 SHALL 按持久原文**重物化**；副本写入成功后才 SHALL 声明该路径存在。**原文有效性与副本寿命 SHALL 分开**：清理副本 MUST NOT 使原文失效；原文失效后 SHALL NOT 假称副本可重建，须明确呈现可重建性。

#### Scenario: 物化引用稳定且不互相覆盖

- **WHEN** 同一持久原文被多次按需读取/外置
- **THEN** 每次物化 SHALL 得到稳定可定位的引用，重复物化 MUST NOT 覆盖或丢弃既有引用
- **AND** 跨 owner/tool/restart 的物化目标 MUST NOT 互相覆盖

#### Scenario: 显式允许安全复用

- **WHEN** 同一持久原文在需要时再次物化，且已有可复用副本
- **THEN** 系统 SHALL 允许安全复用该副本，MUST NOT 因「不得存在多副本」而拒绝或串行化

#### Scenario: 多副本并存合法

- **WHEN** 不同 owner/tool 各自物化同一原文
- **THEN** 多副本并存 SHALL 被允许，MUST NOT 被强制归并为全局唯一副本、MUST NOT 经全局锁串行

#### Scenario: blob 去重不等于引用去重

- **WHEN** 两个引用共享同一 blob 内容但 owner/format 不同
- **THEN** blob 内容 MAY 被去重，但两条引用 MUST NOT 互覆盖

#### Scenario: 副本缺失按原文重物化

- **WHEN** 副本已被清理而原文仍有效
- **THEN** SHALL 按持久原文重物化副本，且副本写成功后才声明路径存在

#### Scenario: 原文失效不假称可重建

- **WHEN** 持久原文已失效
- **THEN** 系统 SHALL NOT 假称副本可重建，须明确呈现可重建性为否

#### Scenario: 授权分段读

- **WHEN** 读取副本的某段内容
- **THEN** SHALL 受授权约束，越权分段读取 SHALL 被拒绝

### Requirement: 临时副本清理与活跃读写保护

临时副本清理 SHALL 在启动与周期执行，并 SHALL 保护**活跃读写中**的副本：正在被读写的副本 MUST NOT 被清理。清理 MUST NOT 删除用户文件，MUST NOT 通过符号链接越界删除副本目录之外的路径。清理 MUST 以副本自身的归属标识限定范围（只清理本 workspace 本原文的临时副本）。

#### Scenario: 启动清理

- **WHEN** workspace 启动时存在上次遗留的临时副本
- **THEN** SHALL 清理这些副本，且不触碰用户文件

#### Scenario: 周期清理

- **WHEN** 清理周期到达
- **THEN** SHALL 清理不再被引用的临时副本

#### Scenario: 活跃读写副本不删

- **WHEN** 某副本正被读写
- **THEN** 清理 MUST NOT 删除该副本

#### Scenario: 不删除用户文件

- **WHEN** 清理运行
- **THEN** MUST NOT 删除非本 workspace 临时副本的用户文件

#### Scenario: 不跨链接越界

- **WHEN** 副本路径存在指向目录外的符号链接
- **THEN** 清理 MUST NOT 沿符号链接越界删除副本目录之外的路径

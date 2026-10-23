# mcp-server-registry Specification

## Purpose

本 delta 使 MCP 注册表的连接/关闭与 state 生命周期对齐：连接活绑定随代持有，关停统一释放恰一次，且注册表声明不随内容漂移。

## MODIFIED Requirements

### Requirement: 运行时注册表

系统 SHALL 提供并发安全的 `MCPRegistry`:`Get(name)`、`List()`、`Names()`(输出按名排序)、`Add(name, toolset)`、`Remove(name)`(Remove 时关闭对应 toolset)。注册表操作 MUST NOT 改变任何 agent 的工具声明集合。注册表连接活绑定 SHALL 随 state 代持有；`Remove`/关停 SHALL 释放连接恰一次，且旧代在途调用引用的连接 MUST NOT 被提前关闭。

#### Scenario: 运行时新增 server 即时可见

- **WHEN** 经 Go API `Add` 注册新 toolset 后调用 `List()`
- **THEN** 返回包含新 server,且无需重建任何 agent

#### Scenario: 移除 server 关闭连接

- **WHEN** 调用 `Remove(name)`
- **THEN** 对应 toolset 的 `Close()` 被调用且 `Get(name)` 返回不存在

#### Scenario: 旧代在途调用不因 Remove 提前关闭（并发）

- **WHEN** 一个旧代在途调用仍引用某 toolset 而注册表收到 `Remove`
- **THEN** 该系统调用的连接 SHALL 保持到该调用真实结束
- **AND** 连接关闭恰一次，不重复

## ADDED Requirements

### Requirement: 注册表声明不随内容漂移

MCP 注册表内容（server 集合）增删 MUST NOT 改变任何持有 `mcp_call` / `mcp_discover` 的 agent 的**工具声明集合**（前缀缓存不变量）；可达工具面的变化只体现在运行时结果。

#### Scenario: 增删 server 后声明不变

- **WHEN** 注册表 Add/Remove 一个 server
- **THEN** `mcp_call` 的 `Declaration()` 输出不变
- **AND** 相关 agent 的 `Request.Tools` 前缀字节不变

#### Scenario: 关停统一释放幂等（重启）

- **WHEN** entry agent `Close()` 被调用
- **THEN** 注册表内全部 toolset 的 `Close()` 各被调用一次
- **AND** 重复 Close 不重复释放

# mcp-discovery Specification

## Purpose

本 delta 在保留"实时注册表发现"语义前提下，要求发现读取与 state 绑定代际一致：发现的指引在当次请求所用绑定下真实可执行。

## ADDED Requirements

### Requirement: 发现与绑定代际一致

`mcp_discover` SHALL 在每次调用时从**当次请求借用代**的注入 `MCPRegistry` 读取 server 集合；发现的调用指引 `mcp_call(server=..., tool=..., args=...)` SHALL 在该代下真实可执行；发现结果 SHALL 携带目标工具 InputSchema。

#### Scenario: 发现指引当次可执行

- **WHEN** `mcp_discover` 命中某工具
- **THEN** 结果 content 含 `mcp_call(server=` 指引与 InputSchema JSON
- **AND** 随后按该指引调用 `mcp_call` SHALL 命中同一 toolset（除非期间发生 Remove）

#### Scenario: 空注册表返回空（失败）

- **WHEN** 无任何 server 注册
- **THEN** 返回 `tools=[], count=0`，无错误

#### Scenario: 单 server 故障不中断（失败）

- **WHEN** 两个 server 中一个连接失败
- **THEN** 另一个 server 的匹配工具仍正常返回

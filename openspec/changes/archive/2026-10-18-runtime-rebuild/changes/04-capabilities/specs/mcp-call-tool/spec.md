# mcp-call-tool Specification

## Purpose

本 delta 保持 `mcp_call` 恒定声明网关语义，并要求其失败自纠材料与重建运行时的**原生配对结果**一致（拒绝/失败不静默）。

## ADDED Requirements

### Requirement: 恒定声明网关与原生配对结果

`mcp_call` SHALL 为内置 plain tool，声明固定为 `server`/`tool`/`args` 三参且 MUST NOT 随注册表内容变化；调用失败 SHALL 返回**原生配对的具名结果**（nil error 的自纠材料），携带可用清单或 InputSchema，供模型下一轮修正。

#### Scenario: 声明与注册表内容解耦

- **WHEN** 注册表中 server 集合发生增删
- **THEN** `mcp_call` 的 `Declaration()` 输出不变
- **AND** 模型第二轮仍看到同名同 schema 的 `mcp_call`

#### Scenario: 失败回显自纠材料（失败）

- **WHEN** `mcp_call(server="not-exist", ...)`
- **THEN** 返回结果含当前已注册全部 server 名（或该 server 工具名清单 / InputSchema）

#### Scenario: 空注册表明确错误（失败）

- **WHEN** 无任何 server 注册时调用 `mcp_call`
- **THEN** 返回提示无可用 server 的结果，进程无异常

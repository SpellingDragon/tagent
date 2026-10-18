# unified-tool-registry Specification

## Purpose

本 delta 重述统一工具注册中心在重建运行时下的语义：工具**原生注册**并进入模型实际 `Request.Tools`；活绑定由 state 持有、请求边界**借出同代**；catalog 为**运行时局部装配**（工厂装配，非进程级全局单例、不引入 DI 容器），原生工具列表按**可见 scope** 固定为**请求快照**；注册表不承载第二工具迭代循环。

## MODIFIED Requirements

### Requirement: ToolRegistry 统一工具注册中心

tagent SHALL 在**运行时装配期**（组合根 `tagent.New()`）以**工厂装配**构造一个**运行时局部**的工具 catalog，注册所有内置工具；catalog MUST NOT 被规定为进程级全局单例，MUST NOT 依赖 `GetRegistry()` 式全局访问点，也 MUST NOT 以反射/DI 容器承载装配。`RegisterBuiltinTools()` SHALL 在组合根构造（`tagent.New()`）中被调用，注册所有内置工具。注册只声明**原生工具**（`CallableTool`/`ToolSet`/`FunctionTool` 适配）；注册表 MUST NOT 承载第二工具迭代循环（不得引入或复用 `LLMAgent` 式再循环）。工具的**生效实例**由 state 在请求边界按代借出，catalog 本身不持有跨代可变执行状态。

原生工具列表 SHALL 按**可见 scope** 在当前请求装配时固定为**请求快照**（进入 `Request.Tools`）：请求进行中的运行时增删 MUST NOT 改变该在途快照，但**合法配置换代**（组织发布新代）SHALL 按新代产生**新的**请求快照——「运行时增删不影响在途快照」MUST NOT 被泛化为「快照永不更新」。

#### Scenario: RegisterBuiltinTools 注册所有内置 plain tool

- **WHEN** `RegisterBuiltinTools()` 被调用
- **THEN** 该运行时 catalog 中注册了以下 plain tool factory：`exec`、`skill_search`、`skill_load`、`mcp_discover`、`web_search`、`duckduckgo_search`、`memory_query`、`recall_query`、`recall_get`、`recall_recent`、`recall_trace`
- **AND** 每个 factory 可通过该运行时 catalog 的查询面（如 `GetPlainToolFactory(id)`）查到

#### Scenario: 运行时局部 catalog（不强制全局单例）

- **WHEN** 同一进程内装配两个相互独立的运行时实例
- **THEN** 每个实例经自身装配入口持有独立 catalog，互不共享进程级可变注册表
- **AND** MUST NOT 依赖 `GetRegistry()` 全局单例，MUST NOT 为跨实例共享而引入 DI 容器或新的注册框架

#### Scenario: 同一 catalog 内重复 tool id 为具名装配错误

- **WHEN** 构造某运行时 catalog 时对同一 tool id 注册两次
- **THEN** 装配期返回**具名错误**（含重复 id）并使本次构造失败
- **AND** MUST NOT 以进程级 panic 终止——错误限定在该次装配，不污染其它运行时实例

#### Scenario: 原生工具列表按可见 scope 固定为请求快照

- **WHEN** 一次请求按 config 声明的可见 scope 组装
- **THEN** 该请求的 `Request.Tools` 为该 scope 下的固定快照，请求进行中不被运行时增删改变
- **AND** 合法配置**换代**时新请求 SHALL 依据新代产生**新的**快照（不沿用旧代声明）

#### Scenario: 无第二迭代循环

- **WHEN** 检查 catalog 与执行路径
- **THEN** 一次工具调用只经**一次**原生调用归约，不存在第二次 `LLMAgent` 循环
- **AND** `CallReceipt` 仅表示**受理**（任务已被接收），MUST NOT 被当作任务**终局**归约；任务终局语义由 02 域承载，本域不另定归约口径
- **AND** 后台结果进入**下一次**实际模型视图，不冒充同 CallID 的第二次同步 response

## ADDED Requirements

### Requirement: 原生工具经 state 绑定注册并真实执行

系统 SHALL 使注册的原生工具进入实际模型请求的 `Request.Tools` 并**被模型第二轮真实调用执行**；工具活绑定（file/skill/FunctionTool/MCP ToolSet 等）SHALL 由 state 持有、请求边界借出同代，旧请求继续持有旧代绑定。工具注册 MUST NOT 使同一 native 接口被镜像为同形影子结构。

#### Scenario: 原生工具进入第二轮真实执行

- **WHEN** entry agent 的一次请求完成首个工具调用
- **THEN** 该工具的声明 SHALL 出现在 `Request.Tools`
- **AND** 模型第二轮 SHALL 收到该工具**真实正文结果**（非固定 final 替身）

#### Scenario: 同代绑定借出、旧请求不拼接新代

- **WHEN** 一次请求跨越一次组织发布（新代生成）
- **THEN** 在途请求继续持有其开始时取得的同代工具绑定
- **AND** MUST NOT 在请求中途拼接新代工具实例

#### Scenario: 失败——未注册的 native 工具被拒

- **WHEN** config 引用未注册的 `id`
- **THEN** `ValidateToolAccess` 返回具名 error，构造失败，不静默降级为空工具

# tool-lifecycle-management Specification

## Purpose

本 delta 重述内置工具三阶段生命周期在 state 绑定代际下的语义：注册 → 校验 → 按 config 组装，且生效实例随代借出、旧代资源在途保留。

## MODIFIED Requirements

### Requirement: 内置工具通过 RegisterBuiltinTools 统一注册

tagent SHALL 在组合根构造（`tagent.New()`）中调用 `RegisterBuiltinTools()`，将所有内置工具（exec + knowledge/recall sub-tools）注册到 ToolRegistry。注册通过 `RegisterPlainTool(id, factory)` 完成。knowledge/recall 不再通过 `RegisterToolAgent` 注册工厂 — 它们变为 config-driven agent。生效实例由 state 在请求边界按代借出；旧代工具资源 SHALL 保持至实际调用、响应流与派生后台执行停止后才退役。

（本要求同步修正名单失真：`speak`/`draw` stub 已移除；原文中的 `read`/`write` 为从未存在的幽灵名，一并移除。）

#### Scenario: tagent.New 调用 RegisterBuiltinTools

- **WHEN** 调用 `tagent.New(cfg, opts...)`
- **THEN** `RegisterBuiltinTools()` 被调用
- **AND** ToolRegistry 中注册了 exec + 10 个 sub-tool factory
- **AND** ToolRegistry 中不包含 "knowledge" 和 "recall" 的 tool agent factory

#### Scenario: knowledge 不再通过 RegisterToolAgent 注册

- **WHEN** `RegisterBuiltinTools()` 执行完毕
- **THEN** `GetToolAgentFactory("knowledge")` 返回 false
- **AND** `GetToolAgentFactory("recall")` 返回 false
- **AND** knowledge/recall 的 sub-tools 通过 `GetPlainToolFactory("skill_search")` 等可查询

#### Scenario: 旧代工具在途不提前退役

- **WHEN** 组织发布新代且旧代一次调用仍在途
- **THEN** 旧代工具绑定与其资源 SHALL 保持到该调用及派生后台执行真实停止
- **AND** 发布新代本身 MUST NOT 单独触发旧工具资源关闭

## ADDED Requirements

### Requirement: 组装期依赖注入按代一致

`buildAgent` SHALL 从请求所借的**同代**绑定组装工具，注入的运行时依赖（MemoryStore/SkillRepo 等）SHALL 与该代一致；跨代混用依赖 MUST NOT 发生。

#### Scenario: 组成一致

- **WHEN** 一次请求在某代内组装多个工具
- **THEN** 所有工具所注入的依赖来自同代绑定
- **AND** 请求中途发生换代时，新工具实例不进入该在途请求

#### Scenario: 失败——注入缺失具名返回

- **WHEN** 某 native 工具所需依赖在该代缺失
- **THEN** 组装返回具名 error（含工具 id 与缺失依赖），构造失败而非静默置空

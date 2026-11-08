# plan-agent Specification

## Purpose

> **superseded（2026-11-04 逐模块核对）**：本规范描述的「plan agent 以自定义 `Run` 方法包裹 `TagentAgent`、按 `action` 字段分流（progress 旁路 / create / update / archive）」已退役。现 plan 是 `examples/wechat-bot/tagent.yaml:94` 声明的普通 agent（`{ kind: agent, agent: plan, description_file: plan_tool_desc.md }`），工具面是 `tool/spec` 的七个 op（init/new/status/validate/archive/instructions/list，见 `tool/spec/skeleton_assert.go:19-25`），提示词在 `resources/prompts/plan_agent.md`。`TagentAgent`、`AgentToolWrapper`、`EventBus`、`tool_call(plan, {action:...})`、`resume_task` 与 `action == "progress"` 全仓 0 命中。

本文件此后只作 superseded 记录，不再承载现行规范命令；阅读现行要求时不得以本文件为依据，也不得把已退役的机制形态重新写入现行规范。

## Requirements

### Requirement: 历史条款已退役

原 Requirements 中的规范化关键词（SHALLs/MUSTs）及其描述的机制已随实现退役而撤销；有效义务由对应现行能力主规格与公共行为锚承接。本条只登记历史事实，不设现行命令。

#### Scenario: 查现行要求

- **WHEN** 需要查询该域的现行要求
- **THEN** 读对应现行能力的主规格及其公共行为锚，而不是本历史文件

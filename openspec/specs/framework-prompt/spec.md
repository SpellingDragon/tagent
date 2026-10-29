# framework-prompt Specification

## Purpose

本规范定义 framework-prompt 能力。ContextManager SHALL prepend a framework runtime description before the user-configured system prompt (from AGENTS.md/SOUL.md/etc).

## Requirements

### Requirement: tagent framework prompt injected before user system prompt

ContextManager SHALL prepend a framework runtime description before the user-configured system prompt (from AGENTS.md/SOUL.md/etc). The framework prompt describes: (1) async tool execution semantics and `[action_tool_result]` events, (2) `[evt_KEY|type]` event identifiers and recall tool usage, (3) context compression and `[context_compress]` summaries. This ensures the LLM understands tagent's event-driven mechanisms without requiring users to manually document them.

#### Scenario: Framework prompt prepended

- **WHEN** a TagentAgent is created with a user system prompt
- **THEN** the effective system prompt SHALL be: framework prompt + "\n\n" + user system prompt
- **AND** the framework prompt SHALL describe async tool, event identifiers, and context compression

#### Scenario: Framework prompt applies to all agents

- **WHEN** both the entry agent and sub-agents (action, knowledge, recall) are created
- **THEN** each agent's ContextManager SHALL prepend the framework prompt to its respective system prompt

### Requirement: 环境块作为动态能力清单追加在请求末尾

环境块 SHALL 由既有 `request->environment` 端口在请求边界现算（含 agent/model/tools/skills/mcp/prompt_version 与开放任务行），SHALL 与运行时加载的技能正文合并为一条动态 system 段（`## Environment` 收尾），并作为请求**最后一条消息**追加（transcript 与收尾指令之后）：稳定内容（声明的 prompt 与声明的技能正文，各合并为一条 system 消息）在前，逐轮可变的内容收尾，服务端前缀缓存只截断末尾最小一段。模型家族的特殊 token 环绕 SHALL 由各 adapter 按惯例完成，框架 MUST NOT 自造标记、MUST NOT 以 user 消息冒充环境信息；adapter 的缓存重排（把 system 消息统一抽到最前）SHALL 关闭，否则末条动态段被抽走、前缀排序失效。工具面 SHALL 走原生 `Request.Tools` 字段（工具定义不进任何消息正文），其定义顺序 SHALL 经 `ToolOrder` 表达：随绑定发布的工具在前（名序），运行期后加载的工具在后（注册序）。环境块开关关闭时该段只含后加载技能正文或为空（行为=基线）。

#### Scenario: 末尾追加

- **WHEN** 环境块开关打开且请求装配完成
- **THEN** `Request.Messages` 最后一条 SHALL 为带标记的环境块

#### Scenario: 清单完整

- **WHEN** 环境块生成
- **THEN** 其内容 SHALL 含 tools/skills/mcp 清单与 prompt_version 行

#### Scenario: 前缀不受染

- **WHEN** 检查任何前部消息
- **THEN** 其中 SHALL NOT 出现环境块标记

#### Scenario: 开关关闭

- **WHEN** EnvironmentBlock 为假
- **THEN** SHALL NOT 追加末尾环境消息

# framework-prompt（MODIFIED delta）

## MODIFIED Requirements

### Requirement: 环境块作为动态能力清单追加在请求末尾

环境块 SHALL 由既有 `request->environment` 端口在请求边界现算（含 agent/model/tools/skills/mcp/prompt_version 与开放任务行），SHALL 以 `<environment>…</environment>` 特殊标记环绕，SHALL 作为请求 `Messages` 的最后一条追加（`Role=user`，transcript 之后）；system prompt 文件本体 SHALL 保持前缀装配。环境块开关关闭时 SHALL 不追加末尾消息（行为=基线）。

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

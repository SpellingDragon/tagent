# 技术要点与文件清单

- 触碰集：`assembly/request.go`（environmentBlock 文本：`<environment>` 环绕+`prompt_version:` 行；assemble 段表移除 env 支）、`runtime/agent/request_assembly.go`（BoundaryView 扩环境文本字段+注释）、`runtime/agent/loop.go`（请求拼装：前缀+transcript+环境消息，Role=既有 RoleUser）、`tests/integration/execution/unified_intake_test.go`（锚）、delta/wiki。
- 风险与回退：环境块消失可回退=段表恢复；拼接顺序锚钉「最后一条」。
- 关键不变量：`Request.Messages` 最后一条=环境块（仅当 EnvironmentBlock 开关开）；工具原生 `Request.Tools` 不动（特殊 token 通道由 provider 承担）。

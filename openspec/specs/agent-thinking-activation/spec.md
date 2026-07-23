# agent-thinking-activation Specification

## Purpose

定义"配置了思考模式的 agent 在运行时必须真正产生推理"的行为契约，并明确不同模型的思考触发字段差异：`hy3`（Hunyuan，经 tencent OpenAI 兼容端点）只认 `reasoning_effort`，而 GLM/Claude/Gemini/DeepSeek honor `thinking_enabled`。目标是保证 agent 运行行为符合其思考配置预期，避免"配了 thinking_enabled 却裸跑无推理"的偏差。

## Requirements

### Requirement: hy3 模型经 reasoning_effort 触发思考

使用 `hy3`（Hunyuan，经 tencent OpenAI 兼容端点）模型且需要思考模式的 agent SHALL 通过设置 `reasoning_effort` 来触发思考。`thinking_enabled` 与 `thinking_tokens` 对 hy3 是 no-op，SHALL NOT 被单独依赖来开启 hy3 的思考。

#### Scenario: thinking_enabled 单独不触发 hy3 思考

- **WHEN** 一个 hy3 agent 仅配置 `thinking_enabled: true`（无 `reasoning_effort`）
- **THEN** hy3 端点 SHALL NOT 返回 reasoning_content（思考未生效）
- **AND** 该配置 SHALL 被视为"思考未实际开启"

#### Scenario: reasoning_effort 触发 hy3 思考

- **WHEN** 一个 hy3 agent 配置 `reasoning_effort: "high"`
- **THEN** hy3 端点 SHALL 返回非空 reasoning_content（思考生效）

### Requirement: hy3 agent 运行行为符合思考配置预期

wechat-bot 中所有使用 hy3 模型且意图开启思考的 agent（`tagent`、`knowledge`、`action`）SHALL 配置 `reasoning_effort`，使其运行时真正产生推理，符合"已开启思考"的配置预期。

#### Scenario: 三个 hy3 agent 均实际思考

- **WHEN** wechat-bot 加载后，`tagent`/`knowledge`/`action` 处理需推理的请求
- **THEN** 每个 agent 的 hy3 调用 SHALL 携带 `reasoning_effort` 并产生 reasoning_content
- **AND** SHALL NOT 出现"配置了思考但运行时无推理"的偏差

### Requirement: 思考修复不回归其它模型 agent

对 hy3 思考的修复 SHALL 限定在 hy3 agent 的显式配置层面，SHALL NOT 引入影响其它模型 agent（如 glm-5.2 的 `plan`、deepseek 的 `recall`/`summary`）的全局框架默认——这些 agent 已通过 `thinking_enabled` 正常思考，不得因本次修复而改变行为。

#### Scenario: glm/deepseek agent 行为不变

- **WHEN** 本次修复应用后
- **THEN** 使用 glm/deepseek 的 agent 的思考配置与运行行为 SHALL 保持不变
- **AND** SHALL NOT 因引入全局 `reasoning_effort` 默认而改变其请求或成本

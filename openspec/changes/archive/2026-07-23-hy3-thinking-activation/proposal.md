## Why

wechat-bot 里三个 agent（`tagent` 主 agent、`knowledge`、`action`）都用 `hy3` 模型并配了 `thinking_enabled: true`，本意是开启思考模式。但真实 API 测试（`TestHy3ThinkingMode_RealAPI`）证明 **hy3（tencent 端点）只认 `reasoning_effort`，完全忽略 `thinking_enabled` 与 `thinking_tokens`**：

| 配置 | reasoning_len |
|------|--------------|
| `thinking_enabled` 单独 | **0**（未思考） |
| `thinking_tokens: 2048` | **0**（未思考） |
| `reasoning_effort: high` | **1145**（思考生效） |

因此这三个 hy3 agent **思考从未真正生效**，一直在"无推理裸模型"状态下运行——与"已开启思考"的配置预期不符。生产轨迹里的 reasoning_content 均来自 honor `thinking_enabled` 的其它模型（plan=glm-5.2、recall/summary=deepseek），不是 hy3。

## What Changes

- `examples/wechat-bot/tagent.yaml`：为三个 hy3 agent（`tagent`、`knowledge`、`action`）补 `reasoning_effort: "high"`（hy3 真正的思考触发器）。保留 `thinking_enabled: true`（对其它模型正确、对 hy3 无害）。
- 更新 thinking 配置区注释：明确 `reasoning_effort` 是 hy3/hunyuan 的思考开关，`thinking_enabled` 对 hy3 无效。
- 纳入真实 API 矩阵测试 `tests/hy3_thinking_test.go` 作为思考触发条件的回归。
- **不改框架默认**（避免回归已正常思考的 glm/deepseek agent）；**不碰 echo 问题**（按用户指示，思考生效后模型能力足够即可规避）。

## Capabilities

### New Capabilities

- `agent-thinking-activation`: 定义"配置了思考的 agent 在运行时必须真正产生推理"的行为契约，并明确 hy3/hunyuan 模型经 `reasoning_effort` 触发思考。

### Modified Capabilities

（无）

## Impact

- 配置：`examples/wechat-bot/tagent.yaml`（三个 hy3 agent 补 `reasoning_effort`；注释更新）。
- 测试：`tests/hy3_thinking_test.go`（真实 tencent 端点验证 hy3 思考触发矩阵）。
- 无框架 Go 代码改动；无破坏性变更（glm/deepseek agent 不受影响）。

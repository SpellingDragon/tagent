## Context

wechat-bot 三个 agent（`tagent`、`knowledge`、`action`）用 `hy3` 模型 + `thinking_enabled: true`。真实 API 测试（`TestHy3ThinkingMode_RealAPI`，tencent 端点）证明 hy3 的思考触发矩阵：

| 配置 | reasoning_len |
|------|--------------|
| `thinking_enabled` 单独 | 0 |
| `thinking_tokens: 2048` | 0 |
| `reasoning_effort: high` | 1145 |
| effort + tokens | 1386 |

即 **hy3 只认 `reasoning_effort`**。trpc-agent-go v1.8.1 里 `thinking_enabled` 对 VariantOpenAI/Hunyuan 会被序列化为 `{"type":"enabled"}`，但 hy3 端点不认该字段；`reasoning_effort` 走标准 OpenAI `reasoning_effort`，hy3 认。tagent 已支持 `reasoning_effort` 配置字段（`config.go` → `context_manager.go` genConfig 映射）。

`tagent.yaml` 注释（L99）误标 `reasoning_effort` 为"OpenAI o-series 专用"，误导了配置。

## Goals / Non-Goals

- **Goal**: 三个 hy3 agent 运行时真正产生推理，符合"已开启思考"的配置预期。
- **Goal**: 不回归已通过 `thinking_enabled` 正常思考的 glm/deepseek agent（`plan`/`recall`/`summary`）。
- **Non-Goal**: 不改 tagent 框架默认（不引入 thinking_enabled→reasoning_effort 的全局自动映射）。
- **Non-Goal**: 不碰 agent_output echo 回声环（用户明确：思考生效后模型能力足够即可规避）。

## Decisions

### 决策 1: 配置层显式修复（为三个 hy3 agent 补 reasoning_effort: high）

在 `tagent.yaml` 为 `tagent`/`knowledge`/`action` 各加 `reasoning_effort: "high"`，保留 `thinking_enabled: true`。理由：安全、即时、可用现成测试验证；不触碰框架，零回归风险。

### 决策 2: 拒绝全局框架默认（thinking_enabled → reasoning_effort）

不在 tagent 里"当 thinking_enabled 且 reasoning_effort 未设时自动补 reasoning_effort"。理由：这会把 `reasoning_effort` 附加到 glm-5.2/deepseek 的 agent 上，而这些 agent 已通过 `thinking_enabled` 正常思考，附加未经验证的字段有回归（行为/成本/报错）风险。泛化留作 future（需逐 provider 验证 reasoning_effort 兼容性，或按 provider 注册表声明思考触发字段）。

### 决策 3: 保留 thinking_enabled: true

对 hy3 无害（被忽略），对可移植性有益（换用 GLM/Claude/Gemini 等 honor thinking_enabled 的模型时仍正确）。

### 决策 4: 取值 "high"

这些是意图深度推理的主力 agent；`reasoning_effort` 支持 low/medium/high，选 high。若后续关注成本/延迟可下调 medium（一处配置）。

## Risks / Trade-offs

- [reasoning_effort=high 增加 token/延迟] → 这些 agent 本就意图思考，推理成本在预期内；可按需下调 medium。
- [未来新增 hy3 agent 再次漏配 reasoning_effort] → 通过更新 `tagent.yaml` thinking 注释显式提示"hy3 需 reasoning_effort"；彻底根治留待决策 2 的 future 泛化。
- [reasoning_effort 是否需与 thinking_enabled 并存] → 测试中两者并存即生效；保守保留两者，不额外冒险。

## Migration Plan

纯配置改动（`tagent.yaml`）+ 注释更新，无数据迁移。重启 wechat-bot 加载新配置生效。回滚 = 移除三处 `reasoning_effort`。

## Open Questions

无。触发条件已由真实 API 矩阵测试坐实。

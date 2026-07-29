## Why

wechat-bot 当前只把 agent 的**文本输出**投递给用户（`SendTextToUser` / `SendLongText`）。当 agent 在本地生成文件（图片、报告、数据文件等）时，没有工程化的方式把文件读出来发给用户，只能把文件全文/内容塞进文本消息。这带来两个问题：

1. **无效 token 开销**：agent 必须把文件内容作为文本输出，LLM 输出 token 被文件内容大量占用，且长内容还会触发 `SendLongText` 的分片/转文件逻辑，进一步浪费。
2. **体验差**：用户收到的是一大段文本而非可下载/可预览的文件。

解决思路：让 agent 只输出**本地文件路径**，由 wechat-bot 在投递层用模式匹配解析出路径，调用 wechat-robot-go 的 `Send*FromPath` 把真实文件发给用户。这样文件内容不再经过 LLM 文本通道，token 开销降到仅路径字符串。

## What Changes

- 在 wechat-bot 投递层新增**本地文件路径解析**：从 agent 最终回复文本中识别绝对路径与相对路径（相对路径按配置的工作区目录解析为绝对路径）。
- 新增**按扩展名自动选择发送接口**：`image/voice/video` 分别调用 `SendImageFromPath` / `SendVoiceFromPath` / `SendVideoFromPath`，其余类型调用 `SendFileFromPath`。
- 投递策略为**发文件 + 保留文本**：检测到路径时，文件通过微信发送，原始文本（含路径说明）照常发送给用户，不做文本改写。
- 在 `tagent.yaml` 的 `app.wechat` 段新增 `workspace_dir` 配置，用于解析相对路径。
- 调整 prompts（如 `TOOLS.md` / `AGENTS.md`），引导 agent 在生成文件后输出路径而非文件内容。
- 新增**基于 mock 的单元测试**：由于涉及下游微信（wechat-robot-go）与上游 LLM 资源，路径解析与文件投递逻辑须通过 mock（mock Bot 发送接口、临时文件）验证，不依赖真实微信登录或真实 LLM。

## Capabilities

### New Capabilities
- `example-local-file-delivery`: wechat-bot 投递层从 agent 回复中解析本地文件路径，并按扩展名自动选择 wechat-robot-go 发送接口将文件投递给用户的能力（含路径匹配、工作区解析、类型识别、mock 测试）。

### Modified Capabilities
<!-- 无既有 spec 级行为变更；example-file-memory-wiring 仅涉及 memory backend，不受影响。 -->

## Impact

- **代码**：`examples/wechat-bot/main.go` 的 output 消费者 goroutine（新增文件投递分支）；可能新增 `examples/wechat-bot/file_delivery.go` 封装解析与发送逻辑。
- **依赖**：消费 wechat-robot-go 既有 `SendImageFromPath` / `SendVoiceFromPath` / `SendVideoFromPath` / `SendFileFromPath`（已存在，无需改动 SDK）。
- **配置**：`examples/wechat-bot/tagent.yaml` 的 `app.wechat` 段新增 `workspace_dir`。
- **Prompts**：`resources/prompts/TOOLS.md`、`AGENTS.md` 增加"生成文件后输出路径"的指引。
- **测试**：新增 `examples/wechat-bot/*_test.go`，用 mock Bot 发送接口 + 临时文件验证解析与投递，覆盖绝对路径、相对路径、多路径、无路径、各类扩展名、不存在文件等场景。
- **系统**：不涉及 tagent 框架核心（持久事件循环、ReAct、压缩、子 agent）变更；仅影响 example 适配层。

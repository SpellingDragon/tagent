## Context

wechat-bot 是 tagent 框架与微信之间的适配层（`examples/wechat-bot`）。当前 output 消费者 goroutine（`main.go:255-407`）在收到 agent 最终回复后，只调用 `bot.SendTextToUser` / `wechat.SendLongText` 把**文本**发给用户。当 agent 在本地生成文件（图片、报告、数据等）时，没有工程化投递手段，只能把文件内容塞进文本，造成大量无效 LLM 输出 token，且长内容还会触发 `SendLongText` 的分片/转文件逻辑。

wechat-robot-go 已经具备完整的本地文件发送能力（`SendImageFromPath` / `SendVoiceFromPath` / `SendFileFromPath` / `SendVideoFromPath`，见 `wechat/bot.go:231-276`），它们读取本地路径 → 上传 CDN → 发送，并自动使用持久化的 `context_token` 挂接会话线程。缺的只是 wechat-bot 投递层的"路径识别 + 类型选择 + 调用"逻辑。

约束与现状：
- 整个进程只有一个 tagent 持久会话，多微信用户靠 `chat_id` 元数据 + `context_token` 区分（`main.go:152-159, 437`）。文件发送必须复用同一 `chat_id` 与 `context_token`，否则挂不到正确会话。
- 下游微信（需登录/真实 CDN）与上游 LLM（需真实推理）都是重资源，投递逻辑必须可脱离二者单独测试。
- 已确认的设计取向：**发文件 + 保留文本**、**按扩展名自动识别类型**、**匹配绝对路径 + 相对路径（按工作区解析）**。

## Goals / Non-Goals

**Goals:**
- 在 wechat-bot 投递层新增纯函数式路径解析：从 agent 回复文本中提取本地文件路径（绝对 + 相对），相对路径按 `workspace_dir` 解析。
- 按扩展名自动选择 wechat-robot-go 发送接口（image/voice/video/file）。
- 投递策略为"发文件 + 保留文本"：文件通过微信发送，原始文本照常发送，不做文本改写。
- 通过接口抽象 + mock，使路径解析与文件投递可脱离真实微信/LLM 进行单元测试。

**Non-Goals:**
- 不改动 tagent 框架核心（持久事件循环、ReAct、压缩、子 agent）。
- 不改动 wechat-robot-go SDK（仅消费既有 `Send*FromPath`）。
- 不在本 change 内做"剥离路径文本"或"仅发文件"模式（用户已选保留文本；可作为后续扩展）。
- 不实现群聊消息、富文本卡片（SDK 本身不支持，见探索结论）。

## Decisions

### D1：接口抽象 `FileSender`，投递逻辑依赖接口而非 `*wechat.Bot`
定义窄接口：
```go
type FileSender interface {
    SendTextToUser(ctx context.Context, toUserID, text string) error
    SendImageFromPath(ctx context.Context, toUserID, path string) error
    SendVoiceFromPath(ctx context.Context, toUserID, path string, duration int) error
    SendVideoFromPath(ctx context.Context, toUserID, path string) error
    SendFileFromPath(ctx context.Context, toUserID, path string) error
}
```
`*wechat.Bot` 天然满足该接口（方法签名一致，见 `bot.go:231-276, 344`）。投递函数 `DeliverFiles(sender FileSender, ctx, chatID, content, workspaceDir)` 依赖接口。
- **理由**：mock 实现该接口即可在测试中记录调用，无需真实微信登录/CDN。
- **替代方案**：直接调用 `*wechat.Bot` 具体方法 → 测试必须起真实 Bot，被否决。

### D2：路径识别要求"存在 + 有扩展名"，以杜绝误判
`ExtractFilePaths(text, workspaceDir) []string` 的匹配规则：
1. 用正则匹配候选 token：绝对路径（`/...` 起头）或相对路径（含 `/` 或 `.` 且非 URL）。
2. 排除 URL（`http://` / `https://` / `ftp://` 等前缀）。
3. 必须以文件扩展名结尾（如 `.png`、`.csv`、`.pdf`，正则 `\.[A-Za-z0-9]{1,10}$`）。
4. **必须 `os.Stat` 存在且为普通文件**才纳入；相对路径先按 `workspaceDir` 解析（若 `workspaceDir` 为空则跳过相对匹配）。
5. 去重、保序。
- **理由**：agent 文本常含 `/api/v1/users`、`C:\...`、代码片段等"像路径但不是文件"的内容；仅靠正则会误发或尝试发送不存在文件。要求"存在 + 扩展名"是最强护栏，且天然契合"本地生成的文件"场景。
- **替代方案**：纯正则匹配 → 误判率高，被否决。

### D3：按扩展名选择发送接口
`selectSendFunc(ext)` 映射：
- image：`png|jpe?g|gif|webp|bmp` → `SendImageFromPath`（图片可内联预览）
- voice：`mp3|wav|amr|m4a` → `SendVoiceFromPath`（duration 传 `0`，由 SDK/微信侧处理）
- video：`mp4|mov|avi|webm|mkv` → `SendVideoFromPath`
- 其余 → `SendFileFromPath`
- **理由**：图片内联渲染体验更好；voice 需 `duration` 参数，统一传 `0` 简化调用（SDK 不强制）。

### D4：投递顺序与错误隔离
文本发送（含 >2000 字 `SendLongText` 长文本分片）由消费者 goroutine 负责；`DeliverFiles` 仅负责文件投递，二者在 `main.go` 最终回复分支中顺序执行（先文本后文件，但顺序非契约约束）。
`DeliverFiles` 行为：
1. 对每个解析出的路径，按类型调用对应 `Send*FromPath(chatID, path)`。
2. **任一文件发送失败仅记日志、继续下一个**，绝不中断其余文件投递。
- **理由**：文本是用户主要信息，由消费者保留发送；文件是增强。`DeliverFiles` 与文本发送解耦（因 `SendLongText` 为包级函数无法纳入 `FileSender` 接口），单文件失败不应阻断其余文件。

### D5：`workspace_dir` 配置
在 `tagent.yaml` 的 `app.wechat` 段新增 `workspace_dir`（绝对路径）。`loadWechatConfig`（`main.go:501-522`）读取并传入 `DeliverFiles`。为空时仅匹配绝对路径。

### D6：prompt 引导（软性）
更新 `resources/prompts/TOOLS.md` 与 `AGENTS.md`，引导 agent 在生成文件后**输出本地文件路径**而非文件内容。这是 token 节省的关键，但属软指引，不进入硬性 SHALL 行为测试。

## Risks / Trade-offs

- **[误发敏感文件]** → agent 若输出敏感绝对路径（如 `/etc/passwd`），会被发送。缓解：默认允许 agent 产出的任意存在文件；生产可加 `allowed_dirs` 白名单（仅允许 `workspace_dir` 子树内文件）。本 change 先记录为开放问题，不强制白名单。
- **[agent 仍输出全文]** → 若 agent 不遵守 prompt 仍 dump 内容，token 浪费依旧。缓解：D6 引导 + 后续可加"剥离/仅发文件"模式。
- **[路径误判]** → 靠 D2 的"存在 + 扩展名"护栏大幅降低；极端情况下代码块内恰好存在同名文件仍可能误发，概率低。
- **[多/大文件]** → 多个文件会产生多条微信消息，可能触及频率/大小限制。缓解：记录日志，后续可加数量上限；本 change 不限制。
- **[context_token 过期]** → `Send*FromPath` 依赖持久化 token，过期返回 `ErrNoContextToken`。缓解：D4 错误隔离，文本仍可达；用户重新发消息即可刷新 token。
- **[voice duration=0]** → 微信语音可能需时长；传 0 依赖 SDK 容错。缓解：如 SDK 要求，后续从文件头解析时长。

## Migration Plan

1. 新增 `examples/wechat-bot/file_delivery.go`：`FileSender` 接口、`ExtractFilePaths`、`selectSendFunc`、`DeliverFiles`。
2. 在 `main.go` 消费者 goroutine 的最终回复分支（`main.go:291-360`）调用 `DeliverFiles` 替代/补充纯文本发送；`bot` 作为 `FileSender` 传入。
3. `loadWechatConfig` 增加 `WorkspaceDir` 字段；`tagent.yaml` 的 `app.wechat` 增加 `workspace_dir`。
4. 更新 `resources/prompts/TOOLS.md`、`AGENTS.md` 增加路径输出指引。
5. 新增 `examples/wechat-bot/file_delivery_test.go`：mock `FileSender` + 临时文件，覆盖各场景。
6. **回滚**：上述均为 additive，删除 `file_delivery.go` 与配置项、回退 prompt 即可恢复原纯文本行为，无破坏性。

## Open Questions

- ~~是否需要在生产环境强制 `allowed_dirs` 白名单以防止泄露敏感文件？~~ **已决（D7）**：不引入可配置白名单，改为硬性排除可执行文件（权限位 + 扩展名拒绝列表），覆盖最危险的二进制/脚本外泄场景。非可执行敏感文件（如 `/etc/passwd`）仍属残留风险，留待后续按需处理。
- ~~投递顺序：文本优先还是文件优先？~~ **已决**：顺序不做要求，由实现决定（D4 当前实现为先文本后文件，但非契约约束）。
- ~~是否后续提供"剥离路径 / 仅发文件"模式作为可配置选项？~~ **已决**：不需要可配置模式，固定为"发文件 + 保留文本"。

## Decisions（补充）

### D7：硬性排除可执行文件，不做可配置白名单
`ExtractFilePaths` 在 `os.Stat` 校验后，额外跳过具有任意可执行权限位（`mode & 0111 != 0`）或扩展名属于拒绝列表（`exe|dll|bat|cmd|sh|bin|out|app|msi|com|run`）的文件。
- **理由**：用户要求"限制不传递可执行文件"。相比可配置 `allowed_dirs` 白名单，硬性排除可执行文件以最小复杂度堵住最危险的外泄路径（恶意/意外生成的二进制与脚本），且无需新增配置项（契合"不需要可配置"）。
- **替代方案**：可配置白名单 → 用户明确不需要可配置，被否决。

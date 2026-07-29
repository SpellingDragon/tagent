## 1. 接口与配置骨架

- [x] 1.1 在 `examples/wechat-bot` 新增 `file_delivery.go`，定义窄接口 `FileSender`（含 `SendTextToUser`、`SendImageFromPath`、`SendVoiceFromPath`、`SendVideoFromPath`、`SendFileFromPath`），并确认 `*wechat.Bot` 满足该接口（编译期断言 `var _ FileSender = (*wechat.Bot)(nil)`）。
- [x] 1.2 在 `loadWechatConfig`（`main.go:501-522`）增加 `WorkspaceDir` 字段，从 `tagent.yaml` 的 `app.wechat.workspace_dir` 读取；`tagent.yaml` 的 `app.wechat` 段新增 `workspace_dir` 配置项（默认空）。

## 2. 核心投递逻辑

- [x] 2.1 实现 `ExtractFilePaths(text, workspaceDir string) []string`：正则匹配绝对/相对路径候选，排除 URL 与无扩展名项，按 `workspaceDir` 解析相对路径，`os.Stat` 校验存在且为普通文件，去重保序。
- [x] 2.2 实现 `selectSendFunc(ext string)`：按扩展名映射 image/voice/video/file 四类发送接口（含 voice 的 `duration` 传 `0`）。
- [x] 2.3 实现 `DeliverFiles(sender FileSender, ctx, chatID, content, workspaceDir string) error`：对每个解析出的路径按类型调用 `Send*FromPath`；单文件失败仅记日志并继续，不阻断其余文件。文本发送（含 >2000 字 `SendLongText` 长文本处理）由消费者保留，不在本函数内。

## 3. 接入消费者 goroutine

- [x] 3.1 在 `main.go` 最终回复分支（`main.go:291-360`）将 `bot` 作为 `FileSender` 传入，调用 `DeliverFiles` 替代/补充原有纯文本发送；保留 `>2000` 字 `SendLongText` 与 `StopTyping` 逻辑。
- [x] 3.2 确认 `chat_id` 与持久化 `context_token` 正确传递，文件挂接到正确微信会话线程。

## 4. Prompt 引导

- [x] 4.1 更新 `resources/prompts/TOOLS.md` 与 `AGENTS.md`，增加"生成文件后输出本地路径而非文件内容"的指引，降低 LLM 输出 token 开销。

## 5. Mock 单元测试

- [x] 5.1 在 `examples/wechat-bot/file_delivery_test.go` 实现 mock `FileSender`，记录各方法调用（方法名、toUserID、path）。
- [x] 5.2 为 `ExtractFilePaths` 编写单测：绝对路径、相对路径（workspace 解析）、URL 排除、不存在路径排除、无扩展名排除、去重保序、workspace 为空仅绝对路径、可执行文件排除（使用 `t.TempDir()` 创建临时文件）。
- [x] 5.3 为 `selectSendFn` 编写单测：png/jpg/gif/webp/bmp→image，mp3/wav/amr/m4a→voice，mp4/mov/avi/webm/mkv→video，csv/未知→file（含大小写不敏感与 voice duration=0 校验）。
- [x] 5.4 为 `DeliverFiles` 编写集成式 mock 测试：含图片路径→`SendImageFromPath`；无路径→不调用任何 `Send*FromPath`；多路径分别按类型（`SendImageFromPath`/`SendFileFromPath`）；某文件发送失败→其余文件仍发送、错误被记录。文本发送由消费者负责，本测试聚焦文件投递。全程不触发真实网络。

## 6. 验证

- [x] 6.1 运行 `go build ./...` 与 `go test ./examples/wechat-bot/...` 确认编译与测试通过（含 `-race`）。
- [x] 6.2 运行 `openspec validate --all` 确认 change 与 specs 结构合规（`auto-send-local-files` 校验通过；其余 4 个失败为既有无关变更）。

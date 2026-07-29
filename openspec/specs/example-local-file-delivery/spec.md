# example-local-file-delivery Specification

## Purpose
TBD - created by archiving change auto-send-local-files. Update Purpose after archive.
## Requirements
### Requirement: 从 agent 回复中解析本地文件路径
wechat-bot 投递层 SHALL 提供纯函数 `ExtractFilePaths(text string, workspaceDir string) []string`，从 agent 最终回复文本中识别本地文件路径。识别规则：
1. 匹配绝对路径（以 `/` 起头）或相对路径（含 `/` 或 `.` 且非 URL）。
2. 排除以 `http://` / `https://` / `ftp://` 等协议前缀开头的 URL。
3. 候选必须以文件扩展名结尾（正则 `\.[A-Za-z0-9]{1,10}$`）。
4. 必须通过 `os.Stat` 确认该路径存在且为普通文件才纳入；相对路径先按 `workspaceDir` 解析为绝对路径（若 `workspaceDir` 为空则跳过相对路径匹配）。
5. 结果去重并保持首次出现顺序。
6. **排除可执行文件**：候选文件若具有任意可执行权限位（`mode & 0111 != 0`）或扩展名属于拒绝列表（`exe|dll|bat|cmd|sh|bin|out|app|msi|com|run`），则不予纳入。此规则为硬性限制，不可配置。

#### Scenario: 提取存在的绝对路径
- **WHEN** 回复文本包含 `/tmp/report.pdf` 且该文件存在
- **THEN** 返回的切片包含 `/tmp/report.pdf`

#### Scenario: 相对路径按工作区解析
- **WHEN** `workspaceDir` 为 `/home/user/work`，文本包含 `output/fig.png` 且 `/home/user/work/output/fig.png` 存在
- **THEN** 返回的切片包含解析后的绝对路径 `/home/user/work/output/fig.png`

#### Scenario: 排除 URL
- **WHEN** 文本包含 `https://example.com/file.csv`
- **THEN** 该 URL 不出现在返回切片中

#### Scenario: 排除不存在的路径
- **WHEN** 文本包含 `/tmp/does-not-exist.txt` 且该文件不存在
- **THEN** 该路径不出现在返回切片中

#### Scenario: 排除无扩展名的路径
- **WHEN** 文本包含 `/usr/bin/python` 且该路径存在但无文件扩展名
- **THEN** 该路径不出现在返回切片中

#### Scenario: 去重并保持顺序
- **WHEN** 文本中同一绝对路径出现两次，且中间穿插另一个不同路径
- **THEN** 返回切片中该路径仅出现一次，且顺序与首次出现一致

#### Scenario: 工作区为空时仅匹配绝对路径
- **WHEN** `workspaceDir` 为空，文本包含相对路径 `a/b.txt` 且该文件存在
- **THEN** 该相对路径不出现在返回切片中

#### Scenario: 排除可执行文件
- **WHEN** 文本包含存在的 `/tmp/script.sh` 或具有可执行权限位的 `/tmp/tool`
- **THEN** 这些可执行文件不出现在返回切片中

### Requirement: 按扩展名选择微信发送接口
wechat-bot SHALL 根据文件扩展名选择 wechat-robot-go 的发送接口：图片扩展名（`png|jpe?g|gif|webp|bmp`）使用 `SendImageFromPath`，语音扩展名（`mp3|wav|amr|m4a`）使用 `SendVoiceFromPath`，视频扩展名（`mp4|mov|avi|webm|mkv`）使用 `SendVideoFromPath`，其余扩展名使用 `SendFileFromPath`。

#### Scenario: 图片扩展名选择图片接口
- **WHEN** 解析出的路径以 `.png` 结尾
- **THEN** 投递时调用 `SendImageFromPath`

#### Scenario: 语音扩展名选择语音接口
- **WHEN** 解析出的路径以 `.mp3` 结尾
- **THEN** 投递时调用 `SendVoiceFromPath`

#### Scenario: 视频扩展名选择视频接口
- **WHEN** 解析出的路径以 `.mp4` 结尾
- **THEN** 投递时调用 `SendVideoFromPath`

#### Scenario: 未知扩展名回退为文件接口
- **WHEN** 解析出的路径以 `.csv` 结尾
- **THEN** 投递时调用 `SendFileFromPath`

### Requirement: 投递文件并保留原始文本
wechat-bot SHALL 在收到 agent 最终回复后，将原始文本发送给用户（保留文本，不做路径剥离；长文本沿用既有 `SendLongText` 分片/转文件逻辑），并对文本中解析出的每个本地文件路径按其类型调用对应的 `Send*FromPath(chatID, path)`。文本与文件均使用与 `chatID` 对应的持久化 `context_token` 以挂接正确会话线程。

#### Scenario: 含图片路径时同时发文本与图片
- **WHEN** 最终回复文本包含存在的 `/tmp/fig.png`
- **THEN** 原始文本被发送给用户，且调用 `SendImageFromPath` 发送该图片

#### Scenario: 无路径时仅发文本
- **WHEN** 最终回复文本不含任何可解析的本地文件路径
- **THEN** 仅发送文本，不调用任何 `Send*FromPath`

#### Scenario: 多路径分别按类型发送
- **WHEN** 文本包含存在的 `/tmp/a.png` 与 `/tmp/b.csv`
- **THEN** 分别调用 `SendImageFromPath` 与 `SendFileFromPath`

### Requirement: 文件发送失败不阻断文本与其他文件
wechat-bot SHALL 在 `DeliverFiles` 中对每个文件的发送错误仅记录日志并继续，单个文件发送失败不得中断原始文本投递，也不得阻止其余文件的发送尝试。

#### Scenario: 某文件发送失败仍发送文本
- **WHEN** 文本包含存在的 `/tmp/ok.png` 与 `/tmp/bad.csv`，且 `SendFileFromPath` 返回错误
- **THEN** `SendTextToUser` 仍被调用，且 `SendImageFromPath` 仍被调用，错误仅被记录

### Requirement: workspace_dir 配置驱动相对路径解析
wechat-bot SHALL 从 `tagent.yaml` 的 `app.wechat` 段读取 `workspace_dir` 配置，并将其作为相对路径解析基准传入路径解析逻辑；当该配置为空时，仅匹配绝对路径。

#### Scenario: 配置工作区后相对路径可解析
- **WHEN** `app.wechat.workspace_dir` 设为 `/home/user/work` 且 agent 输出相对路径 `out/x.txt`
- **THEN** 该相对路径被解析为绝对路径并参与文件投递

#### Scenario: 未配置工作区时相对路径被忽略
- **WHEN** `app.wechat.workspace_dir` 未设置且 agent 输出相对路径 `out/x.txt`
- **THEN** 该相对路径不参与文件投递

### Requirement: 通过接口抽象与 mock 实现可测试性
wechat-bot SHALL 定义窄接口 `FileSender`（包含 `SendTextToUser`、`SendImageFromPath`、`SendVoiceFromPath`、`SendVideoFromPath`、`SendFileFromPath`），投递逻辑依赖该接口而非具体 `*wechat.Bot`；`*wechat.Bot` 须满足该接口。路径解析与文件投递逻辑 SHALL 可通过 mock `FileSender` 与临时文件进行单元测试，不依赖真实微信登录、真实 CDN 或真实 LLM。

#### Scenario: *wechat.Bot 满足 FileSender 接口
- **WHEN** 编译时检查 `*wechat.Bot` 是否实现 `FileSender`
- **THEN** 类型检查通过（无需运行真实微信）

#### Scenario: mock 验证图片投递
- **WHEN** 向 `DeliverFiles` 传入 mock `FileSender` 与含存在图片路径的内容
- **THEN** mock 记录到一次 `SendImageFromPath`（文本发送由消费者负责，不在 `DeliverFiles` 内），且未触发任何真实网络请求

#### Scenario: 临时文件验证多类型投递
- **WHEN** 测试创建临时目录并写入 `.png` 与 `.csv` 临时文件，内容引用其路径
- **THEN** `DeliverFiles` 分别调用 `SendImageFromPath` 与 `SendFileFromPath`，且路径解析不依赖真实文件系统外的资源

### Requirement: 引导 agent 输出文件路径而非文件内容
wechat-bot 的 prompt 资源（`resources/prompts/TOOLS.md` 与 `AGENTS.md`）SHALL 包含指引，要求 agent 在本地生成文件后输出该文件的本地路径，而非将文件内容作为文本输出，以降低 LLM 输出 token 开销。

#### Scenario: prompt 资源包含路径输出指引
- **WHEN** 检查 `resources/prompts/TOOLS.md` 与 `AGENTS.md` 内容
- **THEN** 其中包含关于"生成文件后输出本地路径"的明确指引



FUNCTIONS

func ComposeMediaInject(outcome IntakeOutcome, userText, workspaceDir, chatID string, now time.Time) string
    ComposeMediaInject 拼装媒体消息的注入文本：附件路径列表 + 语音转写 + 用户附言。 转写超过阈值时同样转存文件（走
    SaveLongText）。 无可注入内容（全部被拒且无转写无附言）时返回空串。

func DedupKey(msg *wechat.Message) string
    DedupKey builds the layered dedup key for an inbound message (see SeenStore
    doc for the layering rationale).

func DeliverFiles(sender FileSender, ctx context.Context, chatID, content, workspaceDir string) error
    DeliverFiles 将文本中解析出的本地文件按类型发送给用户。

    文本发送（含 >2000 字长文本分片）由消费者负责，本函数仅负责文件投递， 以保证既有 SendLongText
    逻辑不被破坏（SendLongText 为包级函数，无法纳入 FileSender 接口）。 任一文件发送失败仅记录日志并继续，不阻断其余文件。

func ExtractFilePaths(text, workspaceDir string) []string
    ExtractFilePaths 从 agent 回复文本中解析本地文件路径：六条识别规则（本地路径形态、

TYPES

type FileSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
	SendImageFromPath(ctx context.Context, toUserID, imagePath string) error
	SendVoiceFromPath(ctx context.Context, toUserID, voicePath string, duration int) error
	SendVideoFromPath(ctx context.Context, toUserID, videoPath string) error
	SendFileFromPath(ctx context.Context, toUserID, filePath string) error
}
    FileSender 是 wechat-bot 投递层依赖的窄接口。 *wechat.Bot 天然满足该接口（方法签名一致），测试时可用 mock
    替代， 从而脱离真实微信登录 / CDN / LLM 进行单元测试。

type InboundKind int
    InboundKind 是入站消息的处理分类。

const (
	// InboundIgnore 文本为空且非媒体，不进入会话。
	InboundIgnore InboundKind = iota
	// InboundShortText 其余文本：未超过长文本阈值，或工作区未配置时的降级归类。
	InboundShortText
	// InboundLongText 文本 rune 数超过 longTextThreshold，且 workspaceConfigured 为真。
	InboundLongText
	// InboundMedia 消息含图片/语音/文件/视频之一。
	InboundMedia
)
func ClassifyInbound(msg *wechat.Message, workspaceConfigured bool) InboundKind
    ClassifyInbound 对入站消息分类。纯函数，便于单测。 workspaceConfigured 为 false 时长文本降级为
    ShortText（保持现状行为）。

type IntakeOutcome struct {
	Saved      []SavedFile
	Transcript string
	Rejects    []string
}
    IntakeOutcome 是一条媒体消息的接收结果。

func IntakeMedia(ctx context.Context, dl MediaDownloader, cdnBaseURL, workspaceDir, chatID string, msg *wechat.Message, now time.Time) IntakeOutcome
    IntakeMedia 处理一条含媒体 item 的消息：逐 item 预判大小（体验优化，超限免下载） → O_EXCL 创建目标文件 → SDK
    流式下载直落盘（MaxSize 传输中强制）。 单 item 失败/被拒不阻断其余 item；原因归入 Rejects（面向用户，不注入 agent）；
    任何失败路径删除不完整残留文件。

type MediaDownloader interface {
	DownloadImageFromItemTo(ctx context.Context, cdnBaseURL string, img *wechat.ImageItem, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVoiceTo(ctx context.Context, voice *wechat.VoiceItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadFileFromItemTo(ctx context.Context, file *wechat.FileItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVideoFromItemTo(ctx context.Context, video *wechat.VideoItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
}
    MediaDownloader 是入站接收层依赖的窄接口（流式：SDK 边下边解密写入 Writer， MaxSize 由 SDK
    在传输中强制）。*wechat.Bot 天然满足该接口，测试时可用 mock 替代（与 FileSender 同一模式）。

type SavedFile struct {
	Path     string
	OrigName string
	Size     int
}
    SavedFile 描述一个已落盘的入站文件。

func SaveLongText(workspaceDir, chatID, text, nameHint string, now time.Time) (SavedFile, string, error)
    SaveLongText 将超长文本落盘为 .txt，返回保存结果与注入文本 。

type SeenStore struct {
	// Has unexported fields.
}
    SeenStore provides message-level idempotency across restarts.

    - Dedup key layering: ClientID when non-empty, otherwise uid plus a text
    hash prefix. - seen.json sits next to the bot config, loads at start,
    prunes by TTL and capacity, writes atomically; a corrupted file

func NewSeenStore(dir string, logger *slog.Logger) *SeenStore
    NewSeenStore loads (or initializes) the seen set from dir/seen.json.

func (s *SeenStore) CheckAndMark(key string) bool
    CheckAndMark returns true if the key was new (message should be processed),
    false if it was already seen (duplicate — drop). Marking is persisted
    immediately (atomic write) so a crash right after cannot replay the message.

type WechatAppConfig struct {
	ConfigDir       string `json:"config_dir"`
	TokenFile       string `json:"token_file"`
	ContextTokenDir string `json:"context_token_dir"`
	WorkspaceDir    string `json:"workspace_dir"`
	// Approvers：可经消息通道批准 critical 操作的用户 ID 白名单。
	// 空 = 消息通道批准关闭（安全默认——digest 随请求明文送达，任意可达者可批准），
	// 批准仅经 CLI（wechat-bot approve <digest>）。
	Approvers []string `json:"approvers,omitempty"`
}
    WechatAppConfig holds WeChat-specific configuration.

func (c *WechatAppConfig) EnsureDirs() error
    EnsureDirs creates necessary WeChat directories.

func (c WechatAppConfig) IsApprover(userID string) bool
    IsApprover reports whether the user id may approve via the message channel
    (8.2: empty whitelist denies everyone — approval goes via CLI).

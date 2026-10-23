main.go 只做 wiring：读应用配置 → Boot 装配 → 绑定微信通道 → 运行。 微信协议语义（收发/文件/路由/去重/审批/通知）在
wechat*.go； 声明、模型、工具面、storage、meditation 与注入面全部由 tagent.Boot 装配。

wechat.go 是微信宿主的适配文件：SDK 生命周期、收发、路由、去重、send 端口、审批与启动通知
都是微信协议语义的一部分。装配（声明→agents→models→工具面→storage→meditation→注入面） 一律走
tagent.Boot；应用侧只填 Boot 留出的两个端口：出站消息面与审批面。

wechat_approval.go 是微信审批面：审批请求经微信下发、审批裁决按白名单解析，
以及把治理策略适配到宿主每一次调用的准入席位。审批的传输是微信语义，裁决语义 仍由核心的治理策略决定，应用侧只做接线。

wechat_files.go 是微信文件面：入站媒体的分类、落盘与注入文本，出站回复命名的
本地文件投递，以及两条路径共用的命名与大小规则。全部是微信协议语义的一部分， 与宿主装配无关。

wechat_notice.go 是两条一次性启动通知：保险链换装的转世通报与上一世重启失败的系统告警。
两者都把一条内部消息塞进宿主的收件箱，让第一回合读到；传输是微信宿主的启动语义， 与装配无关。

VARIABLES

var ErrApprovalAmbiguous = errors.New("wechat-bot: the reply names more than one pending approval")
    ErrApprovalAmbiguous reports a ruling whose digest prefix names more than
    one pending request: an ambiguous ruling is refused instead of applied to a
    guess.

var ErrApprovalNotApprover = errors.New("wechat-bot: the sender is not in the approval whitelist")
    ErrApprovalNotApprover reports a message-channel ruling from a user outside
    the approver whitelist: an empty whitelist refuses every message ruling.

var ErrNoChatAddress = errors.New("wechat-bot: the inbound message names no chat identity")
    ErrNoChatAddress reports an inbound message that names no chat identity.
    The refusal is the whole answer: a message without an address is never
    routed to a session that belongs to somebody else.

var ErrSessionPrefixMissing = errors.New("wechat-bot: the session router carries no session prefix")
    ErrSessionPrefixMissing reports a router constructed without the explicit
    session prefix its address spelling starts with.

var ErrTargetRebind = errors.New("wechat-bot: the address is already bound to another chat identity")
    ErrTargetRebind reports a second chat identity claimed for an address that
    already delivered to another chat. The bound target is the delivery identity
    of that session and is never rewritten by a later claim.

FUNCTIONS

func ComposeMediaInject(outcome IntakeOutcome, userText, workspaceDir, chatID string, now time.Time) string
    ComposeMediaInject 拼装媒体消息的注入文本：附件路径列表 + 语音转写 + 用户附言。 转写超过阈值时同样转存文件（走
    SaveLongText）。无可注入内容时返回空串。

func DedupKey(msg *wechat.Message) string
    DedupKey builds the layered dedup key for an inbound message.

TYPES

type FileSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
	SendImageFromPath(ctx context.Context, toUserID, imagePath string) error
	SendVoiceFromPath(ctx context.Context, toUserID, voicePath string, duration int) error
	SendVideoFromPath(ctx context.Context, toUserID, videoPath string) error
	SendFileFromPath(ctx context.Context, toUserID, filePath string) error
}
    FileSender 是投递层依赖的窄接口；*wechat.Bot 天然满足，测试以替身顶上。

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
    ClassifyInbound 对入站消息分类。纯函数。workspaceConfigured 为 false 时长文本降级为
    ShortText（保持现状行为）。

type IntakeOutcome struct {
	Saved      []SavedFile
	Transcript string
	Rejects    []string
}
    IntakeOutcome 是一条媒体消息的接收结果。

func IntakeMedia(ctx context.Context, dl MediaDownloader, cdnBaseURL, workspaceDir, chatID string, msg *wechat.Message, now time.Time) IntakeOutcome
    IntakeMedia 处理一条含媒体 item 的消息：逐 item 预判大小（超限免下载）→ O_EXCL 创建目标文件 → SDK
    流式下载直落盘（MaxSize 传输中强制）。单 item 失败/被拒不阻断其余 item； 原因归入 Rejects（面向用户，不注入
    agent）；任何失败路径删除不完整残留文件。

type MediaDownloader interface {
	DownloadImageFromItemTo(ctx context.Context, cdnBaseURL string, img *wechat.ImageItem, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVoiceTo(ctx context.Context, voice *wechat.VoiceItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadFileFromItemTo(ctx context.Context, file *wechat.FileItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
	DownloadVideoFromItemTo(ctx context.Context, video *wechat.VideoItem, cdnBaseURL string, w io.Writer, opts wechat.DownloadOptions) (int64, error)
}
    MediaDownloader 是入站接收层的窄接口：SDK 边下边解密写入 Writer，MaxSize 由 SDK 在传输中强制。

type SavedFile struct {
	Path     string
	OrigName string
	Size     int
}
    SavedFile 描述一个已落盘的入站文件。

func SaveLongText(workspaceDir, chatID, text, nameHint string, now time.Time) (SavedFile, string, error)
    SaveLongText 将超长文本落盘为 .txt，返回保存结果与注入文本。

type SeenStore struct {
	// Has unexported fields.
}
    SeenStore provides message-level idempotency across restarts.

      - Dedup key layering: ClientID when non-empty, otherwise uid plus a text
        hash prefix.
      - seen.json sits next to the bot config, loads at start, prunes by TTL and
        capacity, writes atomically; a corrupted file degrades to an empty set
        so dedup never blocks the pipeline.

func NewSeenStore(dir string, logger *slog.Logger) *SeenStore
    NewSeenStore loads (or initializes) the seen set from dir/seen.json.

func (s *SeenStore) CheckAndMark(key string) bool
    CheckAndMark returns true if the key was new (message should be processed),
    false if it was already seen (duplicate — drop). Marking is persisted
    immediately (atomic write) so a crash right after cannot replay the message.

type SessionRouter struct {
	// Has unexported fields.
}
    SessionRouter maps one explicit chat identity to the session address the
    runtime is driven at, and holds the delivery identity of each address it
    admitted.

    The mapping is injective in its spelling: one chat identity has exactly
    one session identity and two chat identities never share one, so a restart
    re-derives the same addresses instead of inventing a fresh history.

func NewSessionRouter(agentID event.AgentID, prefix string) (*SessionRouter, error)
    NewSessionRouter returns the router of one declared agent beside the
    explicit session prefix its addresses are spelled with. A missing agent
    or prefix is refused at construction, so no address is ever built from a
    default.

func (r *SessionRouter) AddressFor(chatID string) (event.Address, error)
    AddressFor returns the explicit address of one chat identity: the declared
    agent beside the session identity that identity spells. The chat identity is
    percent-escaped, so the spelling is injective over every byte a channel may
    carry and two chats never collapse onto one session.

func (r *SessionRouter) Bind(addr event.Address, chatID string) error
    Bind records the chat identity a drive was admitted for as the delivery
    identity of its address: a later explicit send from that address reaches the
    chat its own drive came from. A rebound address keeps its first identity and
    reports the conflict instead of silently retargeting the session.

func (r *SessionRouter) BoundAddresses(agentID event.AgentID) []event.Address
    BoundAddresses reports the addresses of the named agent that a drive bound
    to a chat identity, in session order.

func (r *SessionRouter) NextInvocation(addr event.Address) event.InvocationID
    NextInvocation mints the responsibility range identity of one drive of the
    address. The identity is unique per drive and derived from the address,
    so a range never collides with the range of another session.

func (r *SessionRouter) Target(addr event.Address) (string, bool)
    Target reports the chat identity bound to exactly this address. An address
    no drive ever bound answers not-ok: the delivery face then refuses instead
    of resolving a most-recent chat of another session.

type WechatAppConfig struct {
	ConfigDir       string   `yaml:"config_dir" json:"config_dir"`
	TokenFile       string   `yaml:"token_file" json:"token_file"`
	ContextTokenDir string   `yaml:"context_token_dir" json:"context_token_dir"`
	WorkspaceDir    string   `yaml:"workspace_dir" json:"workspace_dir"`
	Approvers       []string `yaml:"approvers" json:"approvers,omitempty"`
}
    WechatAppConfig is the WeChat-specific application section of the document.

func (c *WechatAppConfig) EnsureDirs() error
    EnsureDirs creates the directories the channel and the workspace need.

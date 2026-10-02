package rl // import "github.com/SpellingDragon/tagent/rl"

Package rl provides reinforcement learning utilities for tagent agents.

This package contains components for: - Recording agent trajectories for offline
training - Swapping model instances at runtime - HTTP API for external RL
systems (AReaL)

The AgentLoop interface decouples rl/ from agent/, allowing HTTPAPI to interact
with TagentAgent without importing the agent package.

FUNCTIONS

func AuthTokenFromEnv() string
    AuthTokenFromEnv reads TAGENT_RL_AUTH_TOKEN — the host-side convenience
    for wiring SetAuthToken (the rl package has no config section of its own;
    the listen address and token provisioning belong to the host app).

func EndpointRedirectPolicy(allowedHosts []string) func(*http.Request, []*http.Request) error
    EndpointRedirectPolicy 构造 http.Client 的 CheckRedirect，对 30x
    链按跳校验目标 host 是否在 allowlist 内。匹配粒度、空 allowlist 的部署语义、跳数上界（见
    maxRedirectHops）与主机名归一，均以文档为唯一真源。判定留在 rl 包内、不引入 provider SDK
    依赖，宿主经传输层注入口装上守卫。

func NewEndpointGuardedClient(allowedHosts []string) *http.Client
    NewEndpointGuardedClient 返回可直接用作 openai 式 SDK 传输层的 http.Client：默认代理 传输 ＋
    按跳的allowlist 守卫。allowlist 为空时得到"拒绝所有重定向"的客户端。

func NewHTTPServer(addr string, handler http.Handler) *http.Server
    NewHTTPServer (5.5): server construction with explicit timeouts — the :80
    regression taught that zero-value http.Server silently binds :80 and runs
    without deadlines; hosts must get a hardened constructor.

func ValidateListenAddr(addr, token string) error
    ValidateListenAddr is the loopback fail-closed guard : WITHOUT a token,
    the API must only listen on loopback — any reachable caller could
    otherwise InjectMessage (steer the agent) or redirect the LLM endpoint
    via llm_base_url (full prompt exfiltration). Hosts MUST call this before
    ListenAndServe; a non-loopback address without a token returns an error
    listing the three ways out. With a token set, any address is allowed.

TYPES

type AgentLoop interface {
	InjectMessage(msg model.Message)
	InjectMessageWithSource(source string, msg model.Message)
	StartLoop(userID, sessionID string) (<-chan *event.Event, error)
	StopLoop()
	IsLoopActive() bool
}
    AgentLoop is the interface that decouples rl/ from agent/. It defines the
    minimal contract needed by HTTPAPI to interact with a TagentAgent instance.

type HTTPAPI struct {
	// Has unexported fields.
}
    HTTPAPI 经 HTTP 暴露常驻事件循环，使外部调用方（如 AReaL 的 Python 适配器）可提交任务。 它是可选组件，且构成一张能操纵
    agent 的攻击面 —— 鉴权、loopback 守卫、单点上限与端点 策略四道防线都是结构性的，详见文档。

func NewHTTPAPI(agent AgentLoop) *HTTPAPI
    NewHTTPAPI creates a new HTTPAPI for the given agent.

func (h *HTTPAPI) ServeHTTP(w http.ResponseWriter, r *http.Request)
    ServeHTTP routes requests to the appropriate handler.

func (h *HTTPAPI) SetAuthToken(token string)
    SetAuthToken enables bearer-token authentication for every endpoint .
    With a token set, requests without a matching `Authorization: Bearer` header
    get 401 before any routing or side effect. Pair with ValidateListenAddr for
    the loopback fail-closed guard.

func (h *HTTPAPI) SetDiagnosticsFn(fn func() any)
    SetDiagnosticsFn 注入诊断快照构造器（R2：诊断快照获得消费 面——GET /diagnostics 输出 JSON）。fn 为 nil
    时不注册端点（404）。

func (h *HTTPAPI) SetEndpointPolicy(enabled bool, allowedHosts []string)
    SetEndpointPolicy (5.3): dynamic endpoint redirect defaults to disabled;
    enabling requires a host allowlist (exact host match, any port).

func (h *HTTPAPI) SetFeedbackStore(store memory.MemoryStore)
    SetFeedbackStore enables POST /feedback: the store receives feedback events
    bound to produced events by hex event_key.

func (h *HTTPAPI) SetLimits(l HTTPAPILimits) error
    SetLimits installs request bounds (5.1): zero fields keep the defaults,
    negative fields are rejected — a limit of "reject everything" is a
    misconfiguration, not a feature.

func (h *HTTPAPI) SetModelUpdateFn(fn ModelUpdateFn)
    SetModelUpdateFn sets the callback for runtime LLM endpoint updates. When
    POST /task includes "llm_base_url", the callback is invoked with that URL,
    allowing the application to redirect LLM requests to AReaL's proxy (which
    captures logprobs for RL training).

func (h *HTTPAPI) SetModelUpdateFnE(fn func(baseURL string) error)
    SetModelUpdateFnE installs the error-returning endpoint callback (5.3): the
    update and the message acceptance share the endpoint mutex, and a rebuild
    failure rejects the request with 502 — the old URL keeps serving.

type HTTPAPILimits struct {
	// MaxBodyBytes request body cap (default 1 MiB)
	MaxBodyBytes int64
	// MaxMessages /task messages array cap (default 32)
	MaxMessages int
	// MaxContentBytes per-message content cap (default 256 KiB)
	MaxContentBytes int
	// MaxFeedbackQueue feedback long-poll queue cap (default 1024)
	MaxFeedbackQueue int
}
    HTTPAPILimits (5.1): single-point request validation bounds. Zero fields use
    the documented defaults; negative values are rejected by SetLimits.

func DefaultHTTPAPILimits() HTTPAPILimits
    DefaultHTTPAPILimits returns the documented defaults.

type LLMCallRecord struct {
	Request  LLMRequestRecord  `json:"request"`
	Response LLMResponseRecord `json:"response"`
	// TraceID 关联产生这次调用的 turn span（取自 ctx，未启用导出时为空）；omitempty ⇒
	// 旧 RL 消费者向后兼容，RL 训练投影与 OTel 运维投影共用同一锚点互链。
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
}
    LLMCallRecord 记录单次 LLM 调用的 request 和 response。

type LLMRequestRecord struct {
	Messages         []model.Message        `json:"messages"`
	Model            string                 `json:"model"`
	GenerationConfig model.GenerationConfig `json:"generation_config,omitempty"`
}
    LLMRequestRecord 记录 LLM 请求。

type LLMResponseRecord struct {
	Choices      []model.Choice `json:"choices,omitempty"`
	Usage        *model.Usage   `json:"usage,omitempty"`
	FinishReason string         `json:"finish_reason,omitempty"`
	Error        string         `json:"error,omitempty"`
}
    LLMResponseRecord 记录 LLM 响应。

type ModelUpdateFn func(baseURL string)
    ModelUpdateFn is called when POST /task includes llm_base_url. The
    application layer (main.go) sets this callback to create a new model with
    the given base URL and swap it into the active SwappableModel. This allows
    AReaL's dynamically-allocated proxy URL to be used without changing the
    event mechanism.

type SwappableModel struct {
	// Has unexported fields.
}
    SwappableModel 是可在运行期替换内层实例的 model.Model 包装器：换模型不重建 LLMAgent /
    Runner，也不改事件机制（常驻循环、消息注入、输出通道都不动），只换最 底下的模型实例；所有 GenerateContent /
    GenerateContentIter / Info 都委托当前内层。

func NewSwappableModel(m model.Model) *SwappableModel
    NewSwappableModel creates a SwappableModel wrapping the given model.

func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error)
    GenerateContent delegates to the current inner model. The in-flight lease
    now covers the FULL returned-stream lifecycle : responses are forwarded to
    the caller until the upstream channel is closed (or context cancellation
    closes it) — only then is the lease released and the model eligible for
    retirement Close. Error/nil streams release immediately. A model that leaks
    its channel keeps the lease (conservative: never close a possibly-live
    resource).

func (m *SwappableModel) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error)
    GenerateContentIter 保真内层真实的 IterModel 能力，而非把它藏起来：只实现 GenerateContent
    的装饰器会把具备迭代能力的底层模型**静默降级**成"通道＋协程"路径。

      - 惰性：构造返回的 Seq 不算调用——在调用方真正开始迭代前不加租约、不碰内层、不起协程。
      - 内层是 IterModel 时直接委托其迭代入口（真快路径，不做通道桥接）；否则才桥接。
      - 租约覆盖整个迭代（与 GenerateContent 同构），换出的模型不会在流中被关；提前停止或 ctx
        取消时排空上游，生产方不被卡住、租约最终必被释放。
      - 迭代路径不得把错误咽成"空迭代器的成功"：通道形态会把该错误返回给调用方，桥接侧 至少必须记录，否则同一模型走两条路径会有一条静默失败。

func (m *SwappableModel) Info() model.Info
    Info delegates to the current inner model.

func (m *SwappableModel) Swap(inner model.Model)
    Swap replaces the inner model atomically. In-flight GenerateContent calls —
    INCLUDING their still-open response streams — continue with the old model;
    subsequent calls use the new model. The old model is retired: once no
    in-flight lease (call + full stream) references it AND it is not the current
    inner, it gets an io.Closer Close exactly once.

type TrajectoryMetadata struct {
	DurationMs    int64  `json:"duration_ms"`
	ModelEndpoint string `json:"model_endpoint"`
}
    TrajectoryMetadata 记录调用元数据。

type TrajectoryRecord struct {
	Timestamp  string             `json:"timestamp"`
	SessionID  string             `json:"session_id"`
	UserID     string             `json:"user_id"`
	BatchIndex int                `json:"batch_index"`
	LLMCall    LLMCallRecord      `json:"llm_call"`
	Metadata   TrajectoryMetadata `json:"metadata"`
}
    TrajectoryRecord 是 JSONL 文件中每行的 JSON 结构。

type TrajectoryRecorder struct {
	// Has unexported fields.
}
    TrajectoryRecorder 包装 model.Model，把每次 LLM 调用异步落成 JSONL 记录。

func NewTrajectoryRecorder(inner model.Model, trajectoryDir, modelEndpoint string) (*TrajectoryRecorder, error)
    NewTrajectoryRecorder creates a TrajectoryRecorder wrapping the given model.
    The trajectoryDir will be created if it does not exist.

func (tr *TrajectoryRecorder) Close() error
    Close flushes pending records and shuts down the writer goroutine. Close
    sends a flush sentinel before closing the channel, ensuring the writeLoop
    syncs all pending data to disk before exiting.

func (tr *TrajectoryRecorder) Flush()
    Flush forces all buffered records to be written to disk. This is idempotent
    — calling multiple times has no side effects. Flush sends a sentinel nil
    record through the channel; the writeLoop processes all pending records
    before the sentinel, then syncs the file.

    Thread-safety: Flush holds closeMu for the entire operation, consistent with
    the record() method, preventing races with Close().

func (tr *TrajectoryRecorder) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error)
    GenerateContent implements model.Model.

func (tr *TrajectoryRecorder) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error)
    GenerateContentIter 暴露迭代入口，使偏好 model.IterModel 的流程不被本装饰器降级。它复用
    recordGenerateContent（分配批次号、持 gcWg 租约、流结束时落记录），但只在调用方真正开始
    迭代时才触发：构造迭代器既不占批次号也不调用模型。录制器本要观察每条响应，因此保留的是 迭代的契约与能力面，而非把它们藏在
    GenerateContent 之后。

func (tr *TrajectoryRecorder) Info() model.Info
    Info implements model.Model.

func (tr *TrajectoryRecorder) SetModelEndpoint(endpoint string)
    SetModelEndpoint updates the model endpoint recorded in metadata. Useful
    when SwappableModel swaps to a new endpoint.

func (tr *TrajectoryRecorder) SetSessionInfo(userID, sessionID string)
    SetSessionInfo updates the current session context for trajectory recording.
    This should be called when a new session starts (e.g., from StartLoop).

type TrajectoryRecorderModelWrapper struct {
	// Has unexported fields.
}
    TrajectoryRecorderModelWrapper 包装另一个 model.Model 实例，但共用同一 TrajectoryRecorder
    的 record 通道与写协程：用于包住子 agent 的模型，使其调用同样进入 RL 训练数据。

func NewTrajectoryRecorderModelWrapper(inner model.Model, tr *TrajectoryRecorder) *TrajectoryRecorderModelWrapper
    NewTrajectoryRecorderModelWrapper 构造共享同一录制器的模型包装器。

func (w *TrajectoryRecorderModelWrapper) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error)
    GenerateContent 委托内层模型，并经共享的录制路径落一条记录。

func (w *TrajectoryRecorderModelWrapper) GenerateContentIter(ctx context.Context, request *model.Request) (model.Seq[*model.Response], error)
    GenerateContentIter 与 TrajectoryRecorder.GenerateContentIter 同构：暴露惰性的迭代入口，
    使被子 agent 包装的内层 IterModel 能力不对流程隐藏。

func (w *TrajectoryRecorderModelWrapper) Info() model.Info
    Info 委托内层模型。

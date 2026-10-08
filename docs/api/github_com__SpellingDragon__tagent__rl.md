package rl // import "github.com/SpellingDragon/tagent/rl"

Package rl provides reinforcement learning utilities for tagent agents.

- Components: trajectory recording for offline training, runtime model
swapping, and the HTTP API for external RL systems such as AReaL.
- The AgentLoop interface decouples rl from agent so the HTTP API

CONSTANTS

const (
	// CaptureSchemaVersion is written into every v2 record; the offline
	// converter refuses anything that is not exactly this value.
	CaptureSchemaVersion = 2
	// CaptureScopeSDKRequest names the only boundary this layer observes: the
	// model.Model SDK call. It is deliberately not called "wire".
	CaptureScopeSDKRequest = "sdk_request"
)
    CaptureSchemaVersion and CaptureScopeSDKRequest pin the version and the
    observation boundary of the v2 record.

const (
	// DefaultCaptureMaxRecordBytes bounds ONE serialised record.
	DefaultCaptureMaxRecordBytes int64 = 8 << 20
	// DefaultCaptureMaxPendingBytes bounds the capture-owned total: in-flight
	// request copies + accumulated response fragments + queued records + the
	// serialisation buffer, across all concurrent calls.
	DefaultCaptureMaxPendingBytes int64 = 64 << 20
	// DefaultCaptureMaxRunBytes bounds how much one capture run writes to disk.
	DefaultCaptureMaxRunBytes int64 = 512 << 20
	// MaxCaptureOpenFiles is the hard ceiling on simultaneously open capture
	// files; evicting a handle never deletes or rotates the file.
	MaxCaptureOpenFiles = 16
)
    DefaultCaptureMaxRecordBytes and its siblings are construction-time resource
    bounds: read once at construction, never hot-reloaded.

const (
	BindingBound     = "bound"
	BindingUnbound   = "unbound"
	BindingAmbiguous = "ambiguous"
)
    BindingBound, BindingUnbound and BindingAmbiguous are the whole binding
    vocabulary of a captured call; there is no fourth "guessed" value.

const (
	// TerminalDone: a response with Done and a finish reason was received.
	TerminalDone = "done"
	// TerminalError: the stream ended on a Response.Error object.
	TerminalError = "response_error"
	// TerminalCancelled: the call ctx was cancelled before a terminal response.
	TerminalCancelled = "cancelled"
	// TerminalClosedWithoutTerminal: the stream closed with only partials; the
	// deltas are NOT concatenated into a fabricated answer.
	TerminalClosedWithoutTerminal = "closed_without_terminal"
	// TerminalCallError: GenerateContent itself returned an error.
	TerminalCallError = "call_error"
	// TerminalNilChannel: the legal-but-anomalous (nil channel, nil error).
	TerminalNilChannel = "nil_channel"
)
    TerminalDone and its siblings are the states a capture can honestly name for
    a response stream.

const (
	CaptureStatusComplete = "complete"
	CaptureStatusPartial  = "partial"
	CaptureStatusUnknown  = "unknown"
)
    CaptureStatusComplete is reserved for a quiesced, synchronised, loss-free
    run; an absent or failed manifest is "unknown".

const (
	MissingOwnerCaptureNamespace = "owner.capture_namespace"
	MissingOwnerRootSessionID    = "owner.root_session_id"
	MissingOwnerAgentName        = "owner.agent_name"
	MissingOwnerInvocationID     = "owner.invocation_id"
	MissingOwnerPurpose          = "owner.purpose"
	MissingBindingNoInvocation   = "binding.no_invocation_evidence"
	MissingBindingNoResponseID   = "binding.no_response_id"
	MissingBindingCapacity       = "binding.unbound_capacity"
	MissingBindingConflict       = "binding.conflicting_response_id"
	MissingBindingReleased       = "binding.scope_released"
	MissingRequestDigest         = "request.digest_failed"
	MissingRequestSnapshot       = "request.snapshot_dropped"
	MissingResponseTerminal      = "response.no_terminal"
	MissingRecordTruncated       = "record.truncated_max_bytes"
	// MissingRequestStubbed marks a record whose request side was emptied after
	// even the response-stubbed serialization exceeded the per-record cap.
	MissingRequestStubbed   = "record.request_stubbed_after_oversize"
	MissingPendingExhausted = "record.pending_bytes_exhausted"
)
    MissingOwnerCaptureNamespace and its siblings are the record's way of saying
    "this is absent, and here is why" instead of leaving a consumer to infer it.

const (
	ResponseKeyPrefix = "resp:"
	ToolCallKeyPrefix = "toolcall:"
)
    ResponseKeyPrefix and ToolCallKeyPrefix namespace the association table:
    a consumer builds the same key from an identity it already holds, so a match
    is exact rather than a similarity.

VARIABLES

var (
	// ErrExportAuthorization means no partition allowlist was supplied: an
	// un-parameterized export must not degrade into reading everybody's memory.
	ErrExportAuthorization = errors.New("rl: training export requires an explicit non-empty partition allowlist")
	// ErrExportPageLimitExceeded means the requested page size is outside 1..1000.
	ErrExportPageLimitExceeded = fmt.Errorf("rl: training export page limit must be within 1..%d", exportMaxPageLimit)
	// ErrExportNilStore rejects a missing source instead of exporting "nothing" as
	// if that were a complete snapshot.
	ErrExportNilStore = errors.New("rl: training export store is nil")
	// ErrExportNilWriter rejects a missing sink for the same reason.
	ErrExportNilWriter = errors.New("rl: training export writer is nil")
)
    ErrExportAuthorization and its siblings are the sentinel refusals. A caller
    must be able to tell "you did not authorize this" apart from "the read
    failed", so these are typed and never wrapped away.

var (
	// ErrCaptureRequiresDump reports the precondition of the v2 layer: it only
	// exists on top of a recorder that already writes trajectories.
	ErrCaptureRequiresDump = errors.New("rl: trajectory_capture.enabled requires trajectory_dump=true")
	// ErrCaptureNegativeLimit rejects a nonsensical bound instead of silently
	// turning it into "unlimited".
	ErrCaptureNegativeLimit = errors.New("rl: trajectory_capture limit must not be negative")
	// ErrCaptureNotEnabled is returned when a consumer asks a recorder that has
	// no capture layer for a manifest.
	ErrCaptureNotEnabled = errors.New("rl: trajectory_capture is not enabled")
	// ErrCaptureClosed reports that the run is finished, so no new manifest can
	// be produced; the sealed one is replayed instead.
	ErrCaptureClosed = errors.New("rl: capture run is closed")
)
    ErrCaptureRequiresDump and its siblings surface at construction or flush
    time.

FUNCTIONS

func AuthTokenFromEnv() string
    AuthTokenFromEnv reads TAGENT_RL_AUTH_TOKEN — the host-side convenience
    for wiring SetAuthToken (the rl package has no config section of its own;
    the listen address and token provisioning belong to the host app).

func CallIDForResponse(ctx context.Context, responseID string) (string, bool)
    CallIDForResponse resolves a response identity to the call_id captured for
    it. No entry means false: the caller must report unbound, never take the
    nearest call. This is the read seam plugin/agent use without rl depending on
    them.

func CallIDForToolCall(ctx context.Context, toolCallID string) (string, bool)
    CallIDForToolCall is the same exact-key lookup for a tool-call identity.

func EndpointRedirectPolicy(allowedHosts []string) func(*http.Request, []*http.Request) error
    EndpointRedirectPolicy 构造 http.Client 的 CheckRedirect，对 30x
    链按跳校验目标 host 是否在 allowlist 内。匹配粒度、空 allowlist 的部署语义、跳数上界（见
    maxRedirectHops）与主机名归一，均以文档为唯一真源。判定留在 rl 包内、不引入 provider SDK
    依赖，宿主经传输层注入口装上守卫。

func MarshalCaptureManifest(manifests []CaptureManifest) ([]byte, error)
    MarshalCaptureManifest renders the seal document the offline converter
    reads: {"runs": {run_id: {flat counters...}}, "complete":...,
    "synchronized":...}. The counters stay flat because a nested object would be
    read as zero and turn an unsealed run into a claimed-complete one.

func NewEndpointGuardedClient(allowedHosts []string) *http.Client
    NewEndpointGuardedClient 返回可直接用作 openai 式 SDK 传输层的 http.Client：默认代理 传输 ＋
    按跳的allowlist 守卫。allowlist 为空时得到"拒绝所有重定向"的客户端。

func NewHTTPServer(addr string, handler http.Handler) *http.Server
    NewHTTPServer (5.5): server construction with explicit timeouts — the :80
    regression taught that zero-value http.Server silently binds :80 and runs
    without deadlines; hosts must get a hardened constructor.

func ResponseKey(responseID string) string
    ResponseKey is the association key of one SDK response id.

func ToolCallKey(toolCallID string) string
    ToolCallKey is the association key of one SDK tool-call id.

func ValidateListenAddr(addr, token string) error
    ValidateListenAddr is the loopback fail-closed guard : WITHOUT a token,
    the API must only listen on loopback — any reachable caller could
    otherwise InjectMessage (steer the agent) or redirect the LLM endpoint
    via llm_base_url (full prompt exfiltration). Hosts MUST call this before
    ListenAndServe; a non-loopback address without a token returns an error
    listing the three ways out. With a token set, any address is allowed.

func WithCaptureScope(ctx context.Context, s *CaptureScope) context.Context
    WithCaptureScope installs the association object on ctx for one attempt.

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

type CaptureConfig struct {
	// Enabled defaults to false: nothing is captured and nothing is written
	// beyond the v1 recorder.
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// MaxRecordBytes bounds one serialised record. 0 ⇒ default.
	MaxRecordBytes int64 `json:"max_record_bytes,omitempty" yaml:"max_record_bytes,omitempty"`
	// MaxPendingBytes bounds the capture-owned total across concurrent calls.
	MaxPendingBytes int64 `json:"max_pending_bytes,omitempty" yaml:"max_pending_bytes,omitempty"`
	// MaxRunBytes bounds how much this run writes to disk; hitting it stops new
	// data but never deletes or rotates history (manifest becomes partial).
	MaxRunBytes int64 `json:"max_run_bytes,omitempty" yaml:"max_run_bytes,omitempty"`
	// MaxOpenFiles bounds simultaneously open capture files. Values above
	// MaxCaptureOpenFiles are clamped, not honoured. 0 ⇒ default.
	MaxOpenFiles int `json:"max_open_files,omitempty" yaml:"max_open_files,omitempty"`
	// QueueSize is the capture queue depth. 0 ⇒ the v1 depth (256).
	QueueSize int `json:"queue_size,omitempty" yaml:"queue_size,omitempty"`
}
    CaptureConfig configures the v2 layer. Every field is construction-time.

func (c CaptureConfig) Validate(trajectoryDump bool) error
    Validate checks the config the way the composition root must: enabled
    capture without trajectory_dump is a contradiction, and a negative bound is
    never interpreted as "no limit".

type CaptureFileDigest struct {
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	RunBytes  int64  `json:"run_bytes"`
	Records   int64  `json:"records"`
	SHA256    string `json:"sha256"`
	CutoffSeq int64  `json:"cutoff_seq"`
}
    CaptureFileDigest describes one capture file as this run wrote it. SHA256
    covers the bytes this run appended, so a consumer can re-verify the tail of
    the file against the manifest.

type CaptureFragment struct {
	Seq      int             `json:"seq"`
	Response *model.Response `json:"response"`
}
    CaptureFragment is ONE received *model.Response, deep-copied, in receive
    order. Fragments are never merged or re-concatenated.

type CaptureLLMCall struct {
	Request            CaptureRequest        `json:"request"`
	Response           LLMResponseRecord     `json:"response"`
	ResponseFragments  []CaptureFragment     `json:"response_fragments,omitempty"`
	TerminalStatus     CaptureTerminalStatus `json:"terminal_status"`
	ResponseIncomplete bool                  `json:"response_incomplete,omitempty"`
	// TraceID/SpanID remain optional observation labels; an absent trace never
	// affects call_id or binding.
	TraceID string `json:"trace_id,omitempty"`
	SpanID  string `json:"span_id,omitempty"`
}
    CaptureLLMCall is the v2 llm_call block. Response keeps the v1 shape so an
    old consumer can still read the terminal message, usage and finish reason.

type CaptureManifest struct {
	CaptureStats

	RunID        string              `json:"run_id"`
	CutoffSeq    int64               `json:"cutoff_seq"`
	Status       string              `json:"status"`
	Synchronized bool                `json:"synchronized"`
	Sealed       bool                `json:"sealed"`
	Files        []CaptureFileDigest `json:"files"`
	// Complete is the manifest-level verdict: quiesced AND synchronised AND not
	// stopped by the run budget AND produced from a real writer confirmation.
	Complete bool `json:"complete"`
}
    CaptureManifest is what FlushAndWait returns after the writer confirmed the
    fsync. It embeds the ledger so every field the offline reader needs is flat.

type CaptureOwner struct {
	CaptureNamespace   string   `json:"capture_namespace"`
	RootSessionID      string   `json:"root_session_id"`
	AgentName          string   `json:"agent_name"`
	PartitionID        string   `json:"partition_id"`
	SessionID          string   `json:"session_id"`
	UserID             string   `json:"user_id"`
	InvocationID       string   `json:"invocation_id"`
	ParentInvocationID string   `json:"parent_invocation_id"`
	TaskID             string   `json:"task_id"`
	AttemptID          string   `json:"attempt_id,omitempty"`
	GenerationID       string   `json:"generation_id,omitempty"`
	BundleID           string   `json:"bundle_id,omitempty"`
	TriggerSource      string   `json:"trigger_source,omitempty"`
	Purpose            string   `json:"purpose"`
	InputEventKeys     []string `json:"input_event_keys"`
}
    CaptureOwner is the per-call attribution block. Values that could not be
    obtained stay empty and are named in missing_reasons; nothing here invents a
    turn or session system.

type CaptureRecord struct {
	// Timestamp and the rest of the v1 fields keep their v1 name and meaning.
	Timestamp  string             `json:"timestamp"`
	SessionID  string             `json:"session_id"`
	UserID     string             `json:"user_id"`
	BatchIndex int                `json:"batch_index"`
	LLMCall    CaptureLLMCall     `json:"llm_call"`
	Metadata   TrajectoryMetadata `json:"metadata"`

	// SchemaVersion starts the v2-only block of the record.
	SchemaVersion      int          `json:"schema_version"`
	RunID              string       `json:"run_id"`
	CallID             string       `json:"call_id"`
	CaptureScope       string       `json:"capture_scope"`
	Owner              CaptureOwner `json:"owner"`
	BindingStatus      string       `json:"binding_status"`
	MissingReasons     []string     `json:"missing_reasons"`
	RequestDigest      string       `json:"request_digest"`
	ResponseIncomplete bool         `json:"response_incomplete,omitempty"`
	// ResponseID is the SDK's own id for the terminal response, when there was
	// one. The offline reader takes it as the event key tying this call to a
	// committed event; it is never synthesised or guessed.
	ResponseID string `json:"response_id,omitempty"`
}
    CaptureRecord is one JSONL line of the v2 format.

type CaptureRequest struct {
	Messages         []model.Message          `json:"messages"`
	Model            string                   `json:"model"`
	GenerationConfig model.GenerationConfig   `json:"generation_config,omitempty"`
	Tools            []CaptureToolDeclaration `json:"tools,omitempty"`
}
    CaptureRequest is the frozen SDK request. It keeps the v1 request field
    names and adds the independent declaration list.

type CaptureScope struct {
	// InvocationID is the call this scope belongs to.
	InvocationID string
	// Owner carries the attribution the installer already resolved (agent,
	// session, task, input keys ...). Capture never re-derives it.
	Owner OwnerAttrs

	// Has unexported fields.
}
    CaptureScope is the bounded, per-runner-attempt association object.
    It exists only while capture is enabled, holds at most captureScopeCapacity
    entries, and is released by its owner when the real producer stopped and the
    event callbacks drained — never at the first assistant message or channel
    close.

func CaptureScopeFrom(ctx context.Context) (*CaptureScope, bool)
    CaptureScopeFrom returns the scope installed on ctx, if any.

func NewCaptureScope(invocationID string) *CaptureScope
    NewCaptureScope returns an empty bounded scope for one invocation.

func (s *CaptureScope) Link(key, callID string) (string, LinkResult)
    Link records that key (an exact "resp:<id>" or "toolcall:<id>" identity) is
    owned by callID. It never overwrites a live mapping: a second claim on the
    same key reports LinkConflict so the consumer sees ambiguous, not a guess.

func (s *CaptureScope) Lookup(key string) (string, bool)
    Lookup resolves an exact identity key to a call_id.

func (s *CaptureScope) Release()
    Release drops the scope's memory once the owning attempt truly ended.

func (s *CaptureScope) Stats() CaptureScopeStats
    Stats reports scope occupancy without exposing its maps.

type CaptureScopeStats struct {
	Entries  int
	Overflow int64
	Released bool
}
    CaptureScopeStats is the scope's own occupancy view.

type CaptureStats struct {
	Started          int64 `json:"started"`
	Enqueued         int64 `json:"enqueued"`
	Written          int64 `json:"written"`
	DroppedFull      int64 `json:"dropped_full"`
	DroppedClosed    int64 `json:"dropped_closed"`
	DroppedDiskLimit int64 `json:"dropped_disk_limit"`
	Oversized        int64 `json:"oversized"`
	PendingExhausted int64 `json:"pending_exhausted"`
	OversizedDropped int64 `json:"oversized_dropped"`
	SerializeFailed  int64 `json:"serialize_failed"`
	WriteFailed      int64 `json:"write_failed"`
	SyncFailed       int64 `json:"sync_failed"`

	UnboundNoEvidence   int64 `json:"unbound_no_evidence"`
	UnboundNoResponseID int64 `json:"unbound_no_response_id"`
	UnboundCapacity     int64 `json:"unbound_capacity"`
	UnboundReleased     int64 `json:"unbound_released"`
	Ambiguous           int64 `json:"ambiguous"`

	Inflight        int64 `json:"inflight"`
	Pending         int64 `json:"pending"`
	PendingBytes    int64 `json:"pending_bytes"`
	MaxPendingBytes int64 `json:"max_pending_bytes_observed"`
	RunBytes        int64 `json:"run_bytes"`
	OpenFiles       int64 `json:"open_files"`
}
    CaptureStats is the loss ledger. It is read from atomic counters,
    never from the data queue, so it stays available when the queue is jammed
    (which is exactly when a consumer needs it). The JSON is flat because the
    offline reader looks up these keys directly on the manifest run entry;
    a nested "counts" object would read as zero and mark an unsealed run sealed.

func (s CaptureStats) Complete() bool
    Complete is the loss/queue view used by callers that only have Stats.

func (s CaptureStats) Quiesced() bool
    Quiesced reports a loss-free, fully drained ledger. CaptureManifest.Complete
    adds the synchronisation and disk-limit conditions on top of it.

type CaptureTerminalStatus struct {
	Kind         string `json:"kind"`
	FinishReason string `json:"finish_reason,omitempty"`
	ResponseID   string `json:"response_id,omitempty"`
	Fragments    int    `json:"fragments"`
	Truncated    bool   `json:"truncated,omitempty"`
	Error        string `json:"error,omitempty"`
}
    CaptureTerminalStatus is the reduced description of how the stream ended.

type CaptureToolDeclaration struct {
	RegistryKey  string       `json:"registry_key"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	InputSchema  *tool.Schema `json:"inputSchema,omitempty"`
	OutputSchema *tool.Schema `json:"outputSchema,omitempty"`
}
    CaptureToolDeclaration is one frozen declaration from the S1 snapshot. The
    schema keys keep the framework spelling (inputSchema/outputSchema) because
    that is what the offline converter reads.

type ExportManifest struct {
	// GeneratedAt is this export's Unix-millisecond wall clock. Cutoff is the
	// upper time bound honoured (opts.Until, or GeneratedAt when unset).
	GeneratedAt int64 `json:"generated_at"`
	Cutoff      int64 `json:"cutoff"`
	// Partitions echoes the effective (deduplicated, sorted) allowlist.
	Partitions []int `json:"partitions"`
	PageLimit  int   `json:"page_limit"`

	EventsWritten int `json:"events_written"`
	// Forbidden counts bodies that surfaced inside an authorized paging round but
	// failed the pre-write authorization re-check, and were therefore not written.
	Forbidden int `json:"forbidden"`
	// Filtered counts events excluded by an explicit CallIDs selection.
	Filtered int `json:"filtered"`
	// ParentKeyMissing counts written lines whose parent_key resolved to "".
	ParentKeyMissing int `json:"parent_key_missing"`

	// FeedbackTotal counts written feedback events; the remaining columns
	// partition exactly those.
	FeedbackTotal        int `json:"feedback_total"`
	JoinBound            int `json:"join_bound"`
	JoinMissing          int `json:"join_missing"`
	JoinExpiredOrMissing int `json:"join_expired_or_missing"`
	JoinForbiddenParent  int `json:"join_forbidden_parent"`
	JoinAmbiguous        int `json:"join_ambiguous"`

	// ReadErrors counts failed paging reads and failed event reads; Pages counts
	// paging rounds actually issued.
	ReadErrors int `json:"read_errors"`
	Pages      int `json:"pages"`

	// SourceSHA256 is the digest of exactly the bytes written, tying the snapshot
	// file to this manifest.
	SourceSHA256 string `json:"source_sha256"`
	// Complete is true only when no paging round, event read or write failed.
	Complete bool `json:"complete"`
	// Errs is how many distinct failures were folded into the returned error.
	Errs int `json:"errs"`
}
    ExportManifest states what the snapshot contains and how completely it was
    read. Every counter is a flat top-level field on purpose: the offline reader
    treats a nested object as zero, and a zero read as "nothing went wrong"
    would turn a partial export into a claimed-complete one — the same rule the
    capture seal follows in MarshalCaptureManifest.

func ExportTrainingFacts(ctx context.Context, store memory.MemoryStore, opts ExportOptions, w io.Writer) (ExportManifest, error)
    ExportTrainingFacts streams the authorized fact snapshot into w and returns
    the manifest describing it. A partial read returns the manifest AND a
    non-nil error with Complete=false: an incomplete snapshot is never presented
    as a finished one.

type ExportOptions struct {
	// PartitionIDs is the allowlist. It must be non-empty; duplicates collapse and
	// each partition is paged on its own, so one authorized partition can never
	// widen the read into its neighbours.
	PartitionIDs []int
	// EventTypes narrows the query. Empty means "every type in the authorized
	// partitions" — leaving it empty is the safe default, because a type filter
	// that omits feedback silently removes the join the consumer asked for.
	EventTypes []string
	// Since/Until bound Timestamp in Unix MILLISECONDS — the same unit as
	// memory.FullEvent.Timestamp and QueryOptions.StartTime/EndTime. Zero is open.
	Since int64
	Until int64
	// PageLimit is events per paging round: 0 uses exportDefaultPageLimit, and a
	// value outside 1..exportMaxPageLimit is refused rather than silently clamped.
	PageLimit int
	// CallIDs, when non-empty, keeps only lines whose metadata call_id is in the
	// set. There is no metadata index, so this is a read-then-filter pass over the
	// authorized partitions and excluded events are counted in Filtered — never
	// reported as absent.
	CallIDs []string
}
    ExportOptions is the caller's authorization plus scoping for one export.

type ExportedFact struct {
	memory.FullEvent
	// ParentKey is the canonical hex event key of this event's predecessor, or ""
	// when the store holds no evidence of one. Absence is counted
	// (ParentKeyMissing), never invented.
	ParentKey string `json:"parent_key"`
}
    ExportedFact is one JSONL line: the complete FullEvent copy (embedded,
    so the field set is the storage contract's rather than a projection of it)
    plus the causal parent pointer, which FullEvent itself does not carry.

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

type LinkResult int
    LinkResult explains why a link was or was not stored.

const (
	// LinkStored: newly stored, or the same call_id was already there.
	LinkStored LinkResult = iota
	// LinkConflict: an active mapping for this key points at a different call.
	LinkConflict
	// LinkCapacity: the scope is full; no active mapping was evicted.
	LinkCapacity
	// LinkReleased: the scope was released before this arrival.
	LinkReleased
)
type ModelUpdateFn func(baseURL string)
    ModelUpdateFn is called when POST /task includes llm_base_url. The
    application layer (main.go) sets this callback to create a new model with
    the given base URL and swap it into the active SwappableModel. This allows
    AReaL's dynamically-allocated proxy URL to be used without changing the
    event mechanism.

type OwnerAttrs map[string]string
    OwnerAttrs is the flat attribution surface a capture record can read for one
    call. Key spelling follows the event metadata keys the agent already stamps
    (agent_name / rollout_id / bundle_id / trigger_source / partition_id ...),
    plus the invocation/task identity used by the runner.

type OwnerResolver func(ctx context.Context) OwnerAttrs
    OwnerResolver maps the model-call ctx onto OwnerAttrs. It is injected by the
    composition root (which may read plugin.Attribution) so that rl stays a leaf
    and never imports plugin/agent/root.

type RecorderOption func(*recorderOptions)
    RecorderOption configures optional layers on a TrajectoryRecorder at
    construction time. Nothing here is hot-reloadable.

func WithCapture(cfg CaptureConfig) RecorderOption
    WithCapture installs the v2 capture layer. cfg.Enabled=false leaves the
    recorder byte-for-byte on the v1 path. The trajectory_dump precondition
    is structural here (this recorder only exists when dumping is on) and
    is checked by config.Validate/Validate at the composition root via
    CaptureConfig.Validate.

func WithCaptureOwnerResolver(r OwnerResolver) RecorderOption
    WithCaptureOwnerResolver injects the ctx→attribution read seam.
    The composition root wires it to its own attribution carrier (e.g.
    plugin.AttributionFrom) so rl never imports plugin.

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
    GenerateContentIter 保真内层真实的 IterModel 能力，而非把它藏起来。

    - 构造返回的 Seq 不算调用：调用方真正开始迭代之前不加租约、不碰内层、不起协程。 - 内层是
    IterModel 时直接委托其迭代入口，否则才做通道桥接；租约覆盖整个迭代，换出的模型不会在流中被关。

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
    The trajectoryDir will be created if it does not exist. It is the v1 entry
    point: no optional layer is installed, so the behaviour is what it was
    before the capture layer existed.

func NewTrajectoryRecorderWithOptions(inner model.Model, trajectoryDir, modelEndpoint string, opts ...RecorderOption) (*TrajectoryRecorder, error)
    NewTrajectoryRecorderWithOptions builds a recorder and installs optional
    layers. Without WithCapture (or with Enabled=false) the result is the plain
    v1 recorder: same queue, same writer, same bytes on disk. The capture layer
    is constructed and started before the first call, never lazily, so a config
    that cannot be honoured fails at construction instead of degrading silently
    later.

func (tr *TrajectoryRecorder) CaptureEnabled() bool
    CaptureEnabled reports whether the v2 layer is active.

func (tr *TrajectoryRecorder) CaptureRunID() string
    CaptureRunID is the identity every v2 record and manifest shares.

func (tr *TrajectoryRecorder) CaptureStats() CaptureStats
    CaptureStats reads the ledger without touching the data queue.

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

func (tr *TrajectoryRecorder) FlushAndWait(ctx context.Context) (CaptureManifest, error)
    FlushAndWait returns the writer-confirmed manifest for this run's capture.

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

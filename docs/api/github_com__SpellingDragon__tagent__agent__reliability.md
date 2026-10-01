package reliability // import "github.com/SpellingDragon/tagent/agent/reliability"

Package reliability 承载 tagent 的常驻可靠性子系统（T-G）：依赖失效的优雅退化、 可靠投递、冥想锚点持久化。核心理念（报告
D3）：at-least-once 而非 exactly-once； 每个外部依赖失效有明确定义的「检测→降级→恢复」三段式路径，无静默丢失、无 panic、
无死循环；失败是一等资产（退化状态可查询、可观测、入 governance 事件）。

CONSTANTS

const (

	// PreparedVersionCurrent 是冻结 prepared_fact 时盖上的预备格式版本。带着其它版本（或缺失版本）
	// 材料落盘的槽位属不兼容的过渡材料：绝不解析、绝不静默消费——readEnvelope 拒绝它，由调用方隔离该信封。
	PreparedVersionCurrent = 1

	// InboxStatePending / Claimed / Receipted are the three durable states.
	InboxStatePending = "pending"
	// InboxStateClaimed 信封已被某次 Pull 领取：持有者独占处理权，其它领取者看不到它。
	InboxStateClaimed = "claimed"
	// InboxStateReceipted 表示已写下回执但尚未 ack：此类信封必须在重复领取扫描中
	// 原样存活（既不删除也不重投），删除与容量/租约的释放只属于 Ack。
	InboxStateReceipted = "receipted"
)

VARIABLES

var (
	// ErrInboxFull: pending ≥ maxPending — explicit rejection, never a silent
	// fallback to volatile.
	ErrInboxFull = errors.New("reliability: durable inbox full")
	// ErrQuarantineUndispositioned 表示：上一次运行隔离了不可读或版本未知的项，而运维尚未处置。
	// 重开必须拒绝，而不是每次启动都静默忽略它们。
	//
	// 前代格式（.spill / inbox-v1）数据不阻止启动：运行时只加载当前格式，并把前代项当作惰性数据
	// 处理（绝不猜测解析、绝不消费），等待一次显式的受管 ResetTransitional。仍会阻止重开的，
	// 只有当前格式自身的损坏（隔离）。
	ErrQuarantineUndispositioned = errors.New("reliability: inbox quarantine holds undispositioned items; review and disposition them before reopening (never silently ignored)")
	// ErrCompletionConflict: RecordCompletion carries a payload that differs
	// from the already-durable completion. The frozen completion is
	// authoritative; the caller must not overwrite it (D3 step 8).
	ErrCompletionConflict = errors.New("reliability: envelope completion already durable with a different payload")
	// ErrReceiptKeyConflict: PrepareFacts was asked to reserve a different
	// receipt_key than the one already frozen on the envelope.
	ErrReceiptKeyConflict = errors.New("reliability: envelope receipt_key already reserved with a different value")
	// ErrPrepareConflict: PrepareFacts hit a DETERMINISTIC format/identity conflict
	// — a slot already frozen with a different fact, a facts/slots mismatch, or an
	// envelope not in the claimed state. Unlike a transient I/O failure, retrying
	// cannot resolve it, so the submit gate isolates the envelope and stops
	// auto-consumption. ErrReceiptKeyConflict is a
	// specific instance of this class.
	ErrPrepareConflict = errors.New("reliability: prepare hit a deterministic conflict")
	// ErrReceiveUncertain: an envelope's rename landed on disk but its directory
	// sync failed. The original and its allocated sequence are
	// retained — no later input may reuse that sequence to overwrite it — but the
	// receive MUST NOT be reported as durable-accepted; reconciliation on reopen
	// owns the item. It is distinct from a definite not-published failure.
	ErrReceiveUncertain = errors.New("reliability: receive published but durability unconfirmed")
	// ErrInboxClosed is returned by Enqueue once Close has been called. The
	// liveness decision is made under the mutation lock so an Enqueue whose
	// pre-lock fast-check raced a completed Close cannot register afterwards.
	ErrInboxClosed = errors.New("reliability: inbox closed")
)

TYPES

type AnchorStore struct {
	// Has unexported fields.
}
    AnchorStore 持久化冥想锚点（单 JSON 文件，tmp+rename 原子写）。并发安全。

func NewAnchorStore(path string) (*AnchorStore, error)
    NewAnchorStore 构建锚点存储。path 为空返回 error（持久化必须有路径）。

func (s *AnchorStore) Load() (MeditationAnchors, error)
    Load 读取持久化锚点。文件不存在返回零值 + nil（首次启动，无历史锚点，冥想门控从头开始）。 解析失败返回
    error（调用方保守用零值，不因坏文件阻断启动）。

func (s *AnchorStore) Path() string
    Path 返回锚点文件路径（诊断）。

func (s *AnchorStore) Save(a MeditationAnchors) error
    Save 原子持久化锚点（tmp + rename，防半写被 Load 读到）。

type DegradationManager struct {
	// Has unexported fields.
}
    DegradationManager 管理五依赖的退化状态机。并发安全（mu 保护）。 onChange 在每次状态迁移时回调（默认实现由调用方注入：写
    governance 事件 + 日志）。

func NewDegradationManager(onChange func(dep Dependency, from, to DepState)) *DegradationManager
    NewDegradationManager 构建退化管理器。onChange 可为 nil（仅内部状态）。

func (d *DegradationManager) Configure(dep Dependency, cfg DepConfig)
    Configure 为某依赖设置退化参数（未配置的用默认）。

func (d *DegradationManager) IsDegraded(dep Dependency) bool
    IsDegraded 报告依赖是否处于非正常态（业务侧据此选择降级行为）。

func (d *DegradationManager) ReportFailure(dep Dependency, _ error)
    ReportFailure 上报一次依赖失败。达阈值 → 进入 degraded；recovering 中失败 → 退回 degraded。

func (d *DegradationManager) ReportSuccess(dep Dependency)
    ReportSuccess 上报一次依赖成功。normal 重置失败计数；degraded（探测窗口到）→ recovering； recovering
    连续成功达阈值 → normal。

func (d *DegradationManager) ShouldProbe(dep Dependency) bool
    ShouldProbe 报告 degraded 依赖是否到了探测时机（退避窗口已过）——供**主动探测型**依赖 判断（半开熔断：degraded
    期间只在探测窗放行真实调用）。当前五依赖均为**上报型**（真实操作 即探针：memory/disk/rustviking
    经存储栈、model 经 RunFlow、mcp 经 mcp_call 直连后上报）， 故 ShouldProbe/backoff
    是预留能力（生产路径未接半开熔断，S-2）；接入主动探测型依赖时启用。

func (d *DegradationManager) Snapshot() map[Dependency]DepState
    Snapshot 返回全部依赖状态快照（诊断/可观测）。

func (d *DegradationManager) State(dep Dependency) DepState
    State 返回依赖当前状态（未监控的依赖视为 normal）。

type DepConfig struct {
	FailThreshold    int
	RecoverSuccesses int
	ProbeBackoff     time.Duration
	BackoffMax       time.Duration
}
    DepConfig 是单依赖的退化参数。

type DepState string
    DepState 是依赖健康状态（normal → degraded → recovering → normal）。

const (
	// StateNormal 表示依赖可用：连续失败达到阈值才转入 degraded，并在此刻设起探测退避；
	// 期间的成功只把失败计数清零（避免偶发抖动误判）。
	StateNormal DepState = "normal"
	// StateDegraded 表示依赖不可用，消费方须按各自策略绕行。此态下失败只加倍退避
	// （封顶 BackoffMax）；一次成功即转入 recovering，探测窗口是否到由调用方把门。
	StateDegraded DepState = "degraded"
	// StateRecovering 是"正在确认恢复"的中间态：连续成功累计到 RecoverSuccesses 才回到
	// normal；期间任何一次失败立即退回 degraded，并把退避加倍——恢复必须是可证伪的。
	StateRecovering DepState = "recovering"
)
type Dependency string
    Dependency 是受监控的外部依赖。

const (
	// DepMemory 是存储栈这一受监控依赖：写路径失败由最外层的错误追踪存储上报。
	DepMemory Dependency = "memory"
	// DepRustViking 是外部向量/存储后端依赖，与 DepMemory 分开计状态，
	// 以便后端抖动只降级到它实际影响的能力，而不连带拖垮整条存储栈。
	DepRustViking Dependency = "rustviking"
	// DepMCP 是 MCP 工具来源依赖：处于 degraded 时对 mcp_call 熔断，按探测窗口放行。
	DepMCP Dependency = "mcp"
	// DepModel 是模型调用依赖：degraded 时回合之间按退避暂停，而不是把失败当作正常答复往下走。
	DepModel Dependency = "model"
	// DepDisk 是磁盘写入依赖：写入失败会影响需要落盘的委派路径，因此单独计状态。
	DepDisk Dependency = "disk"
)
type Envelope struct {
	Version   int    `json:"version"`
	RequestID string `json:"request_id"`
	Source    string `json:"source"`
	State     string `json:"state"`
	// Attempts counts re-claims of this envelope (requeue + claim each count
	// one). It is a PERSISTENT AUDIT field only: the behavioural consumer (a
	// max-attempts quarantine gate) is NOT wired, and the diagnostics
	// aggregation surface over it is deferred
	// . Kept for retry forensics, not read to drive behaviour.
	Attempts int `json:"attempts,omitempty"`
	// ReceiptKey is the processing-receipt EventKey (hex) reserved on FIRST
	// prepare. It represents a RESERVED identity only, not completion .
	ReceiptKey string `json:"receipt_key,omitempty"`
	// Messages are the fixed-slot inbound inputs (one per producer message).
	Messages []MessageSlot `json:"messages"`
	// Completion is the frozen post-turn receipt payload + per-slot
	// processed/skipped disposition. Absent = no terminal state yet .
	Completion json.RawMessage `json:"completion,omitempty"`
}
    Envelope is one durable acceptance unit (inbox-v2): a batch of messages from
    one producer call, accepted (202) only after this whole file is durable.

type Inbox struct {
	// Has unexported fields.
}
    Inbox is the durable input mailbox (concurrency-safe).

func NewInbox(dir string, maxPending int) (*Inbox, error)
    NewInbox opens (or creates) an inbox-v2 at dir. On reopen: claimed items
    WITHOUT a receipt go back to pending (the crash may have happened anywhere
    between claim and receipt — replay is the safe default); receipted items
    stay receipted so the consumer Ack-skips them without re-executing.

func (in *Inbox) Ack(path string) error
    Ack removes a confirmed envelope. Idempotent: a repeated Ack (or a lost
    remove that a later replay retries) is success, never a re-execution.

    /: removal is only "done" once BOTH the unlink and its directory-sync are
    durable, and the independent cleanup account is REGISTERED BEFORE the
    unlink happens — an unconfirmed removal can never be lost between "started"
    and "owed". If the unlink lands but the dir-sync fails, the account stays
    (unlink 后同步失败账目保留): capacity, the request-id index and the retention lease
    are NOT released, and Ack returns an uncertain error. A later Ack of the
    now-missing file, or a per-turn DrainCleanups, completes the outstanding
    barrier and then releases capacity/index/retention EXACTLY ONCE. A remove
    that definitively fails (file still present, cleanup never started) cancels
    the pre-registered account — no phantom owed entry for an untouched file.
    An Ack of a missing file with NO owed account was fully acked earlier and
    must not decrement again (no double release).

func (in *Inbox) ClaimNext() (*Envelope, string, error)
    ClaimNext returns the OLDEST pending envelope (lexicographic seq = enqueue
    order), atomically marking it claimed. The file is NOT deleted: a crash
    after claim replays it. Returns (nil, "", nil) when the inbox is drained.

func (in *Inbox) Close() error
    Close refuses further use; on-disk items are intentionally left intact
    (shutdown keeps unconfirmed inputs durable for the next process).

func (in *Inbox) Dir() string
    Dir returns the envelope directory (diagnostics/logging).

func (in *Inbox) DrainCleanups() []UnackedMaterial
    DrainCleanups 为"unlink 已落地、但目录同步仍未成功"的信封补齐所欠的 ack-清理屏障：
    每个本次同步成功的账目恰好释放一次容量，并交回受保护材料供调用方释放保留租约；仍无法 同步的账目继续欠着，留待下一次排空。每轮调用都安全。

func (in *Inbox) Enqueue(env *Envelope) (int64, error)
    Enqueue durably appends one envelope (whole-batch acceptance). It stamps
    version=2, assigns fixed slot indices, and fsyncs the file + syncs its
    directory entry BEFORE returning — only then may the caller report a durable
    receipt. Each slot MUST carry a non-empty source_event (lossless input);
    an empty payload is refused, never accepted with silently-dropped fields.

func (in *Inbox) MaterialOfPath(path string) (UnackedMaterial, bool, error)
    MaterialOfPath reads one envelope from disk and returns its material,
    used by the ack path to release exactly what the lease protected for that
    envelope. os.IsNotExist (already acked/absent) → (zero, false, nil).

func (in *Inbox) Outstanding() ([]OutstandingEnvelope, error)
    Outstanding inventories every un-acked envelope (any state: pending/claimed/
    receipted) with its full original. Read-only under the mutation lock;
    it deletes, claims, rewrites or requeues NOTHING — reconcile dispositions
    are the caller's protocol-level decisions (Ack/Quarantine/retain).

func (in *Inbox) Pending() int64
    Pending returns the unconfirmed (pending+claimed+receipted) count.

func (in *Inbox) PrepareFacts(path, receiptKey string, facts []json.RawMessage) error
    PrepareFacts durably freezes each slot's prepared_fact and a reserved
    receipt_key onto a CLAIMED envelope in ONE atomic rewrite. The caller MUST
    complete this before writing the first fact (D2「写前准备耐久重写」): a failure
    here means NO fact is written and the claim replays. It is idempotent on
    a re-prepare with the same receipt_key and identical facts; a different
    receipt_key or a different already-frozen fact is a conflict (the envelope
    stays untouched so the caller can quarantine it). A nil element in facts
    leaves that slot's existing prepared_fact unchanged (partial prepare across
    a retry is allowed).

func (in *Inbox) QuarantineEnvelope(path, reason string) (UnackedMaterial, bool)
    QuarantineEnvelope isolates one specific envelope (by path) into the
    quarantine dir under the mutation lock and frees its unacked capacity.
    The bytes are kept on disk for operator inspection (never destroyed).
    The submit gate uses it to isolate a deterministic-conflict input rather
    than silently retry or drop it.

    It returns the isolated envelope's retention material together with the
    moved flag: quarantine is a terminal disposition like Ack, so the caller
    MUST release the returned material's holders whenever moved is true. Reading
    the material from the SAME pre-move read (rather than a separate pre-read)
    is what makes "isolated ⇔ releasable" atomic — a second read that could
    disagree with this one (transient I/O error between the two) would strand
    the lease forever. An already-gone/unreadable envelope quarantines nothing,
    protects nothing, and returns (zero, false).

func (in *Inbox) RecordCompletion(path string, completion json.RawMessage) error
    RecordCompletion durably freezes the post-turn completion payload (per-slot
    processed/skipped disposition + receipt content) onto the envelope BEFORE
    the fact-chain receipt is submitted (D3 step 7). Idempotent on an identical
    payload; a different payload on an already-completed envelope is a conflict
    — the frozen completion is authoritative.

func (in *Inbox) RecordReceipt(path string, cred ReceiptCredential) error
    RecordReceipt durably marks a claimed envelope as processed (claimed
    → receipted) ONLY on valid evidence. Three gates, none of which is a
    description string or a request id: ① a LEGAL completion is durably frozen —
    present, valid JSON, and schema-

        consistent enough for this schema-agnostic leaf to trust (the agent layer
        fully decodes/validates before issuing any credential, D2);

    ② the two-phase reservation was actually established — the envelope carries
    a

        non-empty reserved receipt key from PrepareFacts;

    ③ the caller presents a ReceiptCredential whose key matches that reservation

        — the verified receipt identity, minted only after the fact-chain receipt
        commit was confirmed. A refused receipt never advances the state: the claim
        stays and replays rather than letting a bare transition stand in for
        processing evidence. Crash before a successful call → the claim replays.

func (in *Inbox) ReleaseClaim(path string) error
    ReleaseClaim 把已领取的信封退回 pending，使后续 Pull 能按严格序号重新领取它。提交门用它 在瞬时 I/O 失败时退避——不
    ack、不丢弃、不消费输入，从而保住顺序与有界背压（最旧的卡住 信封先于任何较晚到达者被重领）。文件已消失、或已不处于 claimed 态时是
    no-op。

func (in *Inbox) ResetTransitional(confirm bool) (int, error)
    ResetTransitional 是对前代格式数据的**一次性受管重置**，也是本可靠性叶子内唯一具破坏性的路径。
    安全约束（不得被推导成"任意删除"的能力）：
      - 必须显式 confirm：重置是运维动作，绝不自动发生；
      - 只删除打开时枚举到的前代格式文件（散落的 *.spill 与 inbox-v1/*.json）——它们与 在用的 inbox-v2
        树互不相交，且不被任何当前事实引用，因此清掉它们不留悬空引用 （恢复单元的一致性规则约束的是当前数据）；
      - 绝不触碰 inbox-v2 及其隔离区（当前格式损坏必须暴露，而不是被抹掉），也不触碰本 叶子目录树之外的任何路径；
      - 已关闭、或仍有信封未 ack（pending>0）时拒绝执行：受管重置需要独占写入权，而不是 在一个进行中的回合里插队。

    当前格式损坏与一般 I/O 失败都不属于"过渡数据"，这里绝不清理它们。返回被删除的 前代格式文件数。

func (in *Inbox) TransitionalData() (spill, v1 []string)
    TransitionalData reports the previous-format items detected at open. They
    are inert — never read or consumed — and remain on disk until an explicit,
    operator-confirmed ResetTransitional. The slices are copies (safe to
    retain).

func (in *Inbox) UnackedMaterial() ([]UnackedMaterial, error)
    UnackedMaterial enumerates the protected originals of every
    un-acked envelope (a *.json file still present in the inbox ==
    pending/claimed/receipted; an acked envelope's file is already removed).
    This is the "rebuild the lease from existing unacked material" source:
    it reads the on-disk envelopes directly and introduces NO second persistence
    table. Read-only. A dir-read failure is returned so the caller can fail
    conservative (do not open forgetting on an incomplete view).

type MeditationAnchors struct {
	LastUserInput  int64 `json:"last_user_input"`
	LastTurnEnd    int64 `json:"last_turn_end"`
	LastMeditation int64 `json:"last_meditation"`
}
    MeditationAnchors 是冥想门控锚点的持久化快照（Unix ms）。

    锚点跨重启持久；缺失即视为 0。

type MemorySink struct {
	Mgr *DegradationManager
}
    MemorySink 把 DegradationManager 适配为 memory.DegradationSink（string 依赖名 →
    typed Dependency）。

func (s MemorySink) ReportFailure(dep string, err error)
    ReportFailure 实现 memory.DegradationSink：string 依赖名转 typed Dependency 上报失败。

func (s MemorySink) ReportSuccess(dep string)
    ReportSuccess 实现 memory.DegradationSink：string 依赖名转 typed Dependency 上报成功。

type MessageSlot struct {
	// Slot is the immutable 0-based position within the envelope.
	Slot int `json:"slot"`
	// SourceEvent is the lossless JSON snapshot of the original AgentEvent
	// (ID/Type/Source/Timestamp/full Message/business Metadata), written at
	// acceptance. The reliability leaf holds it as opaque bytes: only the
	// agent layer knows its schema (no agent/memory import here, D2).
	SourceEvent json.RawMessage `json:"source_event"`
	// PreparedFact is the canonical FullEvent JSON frozen on FIRST handling,
	// before any fact is written. Absent (nil) = not yet prepared. A replay
	// MUST reuse it verbatim — never restamp time/attribution/summary .
	PreparedFact json.RawMessage `json:"prepared_fact,omitempty"`
	// PreparedVersion 记录 prepared_fact 冻结时所用的预备格式版本（见 PreparedVersionCurrent）；
	// 0 表示尚无材料。readEnvelope 会拒绝"有材料但版本非当前"的槽位，因此不兼容的过渡材料
	// 绝不会经前代格式解析器被回放。
	PreparedVersion int `json:"prepared_version,omitempty"`
}
    MessageSlot is one inbound input at a FIXED slot index inside an envelope.
    The slot number is assigned at acceptance and NEVER renumbered — a failed
    slot does not compact the others (F4「序号不可压紧」).

type OutstandingEnvelope struct {
	Path string
	Env  *Envelope
	// ReadErr is the raw per-file failure (nil when decodable). A missing file
	// (os.IsNotExist) is simply skipped; any other error means the original is
	// present but unreadable NOW (open already quarantined corrupt items, so
	// this is new I/O trouble) — the reconcile must block on it conservatively,
	// never guess a confirmation or a deletion.
	ReadErr error
}
    OutstandingEnvelope is one still-present inbox envelope from the cold-start
    inventory: the full original plus its path, for the agent layer to reconcile
    DIRECTLY against each envelope's own fixed receipt key — the confirmation
    list is never harvested from the projection scan (spec L162: an outstanding
    receipt whose key predates the snapshot/tail window would be missed and
    re-executed forever).

type ReceiptCredential struct {
	// ReceiptKey is the hex reserved receipt key whose fact-chain receipt the
	// caller verified present. The empty key is never a credential.
	ReceiptKey string
}
    ReceiptCredential is the verified receipt credential RecordReceipt
    requires (, design 决策 L126「RecordReceipt 必须要求合法 completion 与已核验回执 凭据」).
    It carries ONLY the reserved receipt-key identity under which the caller has
    verified the fact-chain receipt — it is never a free-form description string
    or a request id, the two weak confirmation shapes deletes. The leaf checks
    it against the envelope's frozen reservation; the schema-level legality of
    the completion (decode + validate) is verified by the agent layer before a
    credential is ever issued (D2 keeps this leaf schema-agnostic).

type UnackedMaterial struct {
	// ReceiptKey is the processing-receipt EventKey in hex, reserved on prepare
	// ("" if the envelope never got a receipt key). Returned verbatim: the
	// reliability leaf holds it as an opaque identity and does not parse it.
	ReceiptKey string
	// FactKeys are the canonical fact EventKeys frozen in the slots' prepared_fact
	// (int64, parsed straight from the opaque JSON via its event_key field). A slot
	// with no prepared_fact contributes nothing (never committed → nothing to protect).
	FactKeys []int64
}
    UnackedMaterial describes the durable originals that an outstanding
    (not-yet- acked) envelope still depends on. The recovery owner rebuilds
    the store's retention lease from these so the originals survive
    TTL/capacity/compaction until the envelope is acked (dir-synced) and
    released.

func MaterialOf(env *Envelope) UnackedMaterial
    MaterialOf extracts the protected originals of one envelope: the fact
    EventKeys frozen in its slots' prepared_fact plus the reserved receipt key
    (hex, returned verbatim — the reliability leaf does not parse it). Used by
    both the startup lease rebuild and the per-ack release so protect/release
    derive the SAME key set. A nil env yields the zero material.


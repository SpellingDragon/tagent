package compress // import "github.com/SpellingDragon/tagent/agent/compress"

Package compress 负责回合上下文的装配与压缩，是"预算怎么用"这一侧的全部机制：

  - ContextCompressor／SmartCompressor：两阶段压缩——先按段级判定丢弃低价值消息， 仍不足时再折叠出综述卡片；
  - SessionProjection：把事实链的事件引用折叠成本会话视图，Append 幂等，重建时按 当前引用整表重算；
  - task_segmenter：按任务边界切分消息，使压缩不会把一段工作切成半截；
  - token_counter：字符近似计量与事件类型到角色的映射（映射的唯一权威源在 event 包）；
  - compaction_event／BuildCompactionPayload：把折叠产物作为一等事实落链，载荷携带 重建状态，可召回正文即叙事本身；
  - telemetry：可见性票据与处置计数，供宿主与运维判断压缩是否被消费。

本包只决定"保留哪些内容、如何折叠"，不决定回合的重试与执行面（那在 agent 与 reliability
包）。所有阈值都是命名常量而非配置项，以免与热参源产生第二处真值。

CONSTANTS

const (
	CompactionMetaKey        = "compaction"
	CompactionGenV1          = "v1"
	CompactionPayloadMetaKey = "compaction_payload"
)
    CompactionMetaKey 约定 compaction 事件的元数据键。代际标记把 supersede 与快照选取
    限定在本系统产出的 compaction 事件上——无标记的既有固化数据（同类型、无标记、TTL 永久） 既不删除也不被选中。载荷 JSON
    承载重建状态；可召回正文即叙事本身。

const (
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8

	// DefaultSummaryMaxTokens is the floor for the output-token budget reserved
	// on each summary LLM call. A reasoning model spends part of max_tokens on
	// its thinking chain; too small a budget leaves Content empty (mass
	// degradation). The per-call budget scales up with the summary size
	// (targetChars*2) but never below this floor.
	DefaultSummaryMaxTokens = 8192

	// DefaultCompactKeysListed caps the keys listed in the rolling
	// compaction summary; older events stay retrievable via recall.
	DefaultCompactKeysListed = 32
	// DefaultRefsPerTurn is the assumed refs per complete task turn
	// (external_input + thinking_plan + action_command + agent_output) used
	// to derive the recentFullCount default: keepRecent × DefaultRefsPerTurn,
	// so the most recent keepRecent turns resolve with full content as a
	// whole. An explicit WithRecentFullCount /
	// recent_full_count setting overrides the derived value.
	DefaultRefsPerTurn = 4
	// DefaultCardMaxChars caps the index-card section of the rolling summary;
	// beyond it old card lines are LLM-condensed (or sink, without a model).
	DefaultCardMaxChars = 6000
)
    DefaultMaxTokens Compression defaults (single source; the agent package
    re-exports aliases).

const (
	// TelemActive: not yet consumed (no reclaim turn produced after it).
	// It must never be demoted or L3-archived — at-least-once reaches the
	// model view intact (不可丢级的通道层落点).
	TelemActive int8 = iota
	// TelemInternal: consumed but the reclaim output is NOT deliverable to
	// the host (internal lineage: meditation-spawned, retired-settle,
	// unknown/absent lineage). Kept verbatim for up to keepRecent turns as
	// a short-term reminder, then demoted.
	TelemInternal
	// TelemDemote: consumed and externalized (or an internal notice that
	// aged past its reminder window) — demotes to a ticket card at the next
	// compaction act. Details stay recallable via the fact chain.
	TelemDemote
)
    TelemActive Telemetry disposition values for settle-notice refs (keyed by
    EventKey).


FUNCTIONS

func EventTypeToRole(eventType string) model.Role
    EventTypeToRole maps an event type to its pairing-free timeline role :

        external_input → user
        agent_output → assistant
        action_command → user (tool results are input events, never role=tool)
        thinking_plan  → assistant
        (default) → user (safe degradation)

    角色映射的唯一权威源是 event 包的注册表，本函数只委托。

func IsSkeletonMessage(msg *model.Message) bool
    IsSkeletonMessage reports whether a message is a task-skeleton node — a pure
    event-type function, never reading message content. Skeleton: external_input
    / agent_output. Droppable intermediates: action_command / thinking_plan.
    Any other type (e.g. the rolling context_compress summary) is conservatively
    treated as skeleton so it is never dropped in-segment.

func MessageEventType(msg *model.Message) string
    MessageEventType classifies a message by event type (task-skeleton D1).
    Primary: the [evt_KEY|type] prefix stamped by resolveRef. Fallback for
    unprefixed inputs (e.g. tests feeding bare messages): the role heuristic of
    ExtractEventType — assistant without tool_calls counts as agent_output.

func SplitSystemMessage(messages []model.Message) (*model.Message, []model.Message)
    SplitSystemMessage splitSystemMessage separates the system message from the
    rest.

func TelemetryDispositions(ctx context.Context, store memory.MemoryStore, refs []memory.EventReference, keepRecent int) map[int64]int8
    TelemetryDispositions folds the projection refs into a per-settle-key
    disposition map. store may be nil (pure structural mode: externalization
    undecidable → treated internal, conservative per the unknown-withhold
    philosophy). keepRecent bounds the internal reminder window.


TYPES

type CompactionPayload struct {
	// SummaryRef is the projection's new negative-key summary reference —
	// full identity + composed EventSummary, byte-exact.
	SummaryRef memory.EventReference `json:"summary_ref"`
	// Retained is the ordered retained list (synthetic and positive entries
	// interleaved in projection order; the summary ref itself excluded).
	Retained []RetainedEntry `json:"retained"`
	// FullBoundary seeds the rebuilt compressor's full-render boundary.
	FullBoundary int64 `json:"full_boundary"`
}
    CompactionPayload is the wire form of one compression fold, persisted as a
    context_compress_summary fact-chain event. It captures everything about the
    fold that is NOT recomputable: the composed summary text, the summary ref
    identity, the ordered (interleaved) retained list, and fullBoundary.

func BuildCompactionPayload(retained []memory.EventReference, fullBoundary int64) (p *CompactionPayload, ok bool)
    BuildCompactionPayload derives the persistable fold record from a fresh
    fold's projection refs. ok=false when retained does not start with a
    negative-key context_compress summary ref — i.e. no REAL fold happened this
    round. Pure: no I/O, no LLM.

func UnmarshalPayload(raw string) (*CompactionPayload, error)
    UnmarshalPayload parses Metadata[CompactionPayloadMetaKey].

func (p *CompactionPayload) MarshalPayload() (string, error)
    MarshalPayload serializes the payload for
    Metadata[CompactionPayloadMetaKey].

func (p *CompactionPayload) RestoreRefs() (ordered []memory.EventReference, posKeys []int64, posIdx []int)
    RestoreRefs reconstructs the projection refs from the payload in STORED
    ORDER: [SummaryRef] + retained entries. Synthetic entries contribute their
    verbatim ref; positive entries return their keys separately — the caller
    resolves them via GetEvent (immutable store = byte-exact ref) and fills
    ordered[posIdx[i]]. Missing/tombstoned keys degrade to a skip (the caller
    logs WARN and drops the slot — never blocks the rebuild).

type CompressOptions struct {
	// KeepRecentTasks overrides SmartCompressor.KeepRecentTasks for this call
	// (<= 0 → configured value). Per-call instead of mutating the shared
	// field: the stash-rewrite-restore dance was a data race under concurrent
	// compress — correctness is now structural, not single-goroutine-discipline.
	KeepRecentTasks int
	// MaxTokens / TriggerBudget carry the caller's live numeric generation
	// .
	// When they travel with the call, the shared hot fields are never consulted,
	// so a concurrent hot rotation cannot tear this pass (outer trigger line and
	// inner aging target from one source read). <=0 → configured fallback.
	MaxTokens     int
	TriggerBudget int
}
    CompressOptions carries per-call overrides. Zero-value fields fall back to
    the compressor's configured defaults.

type CompressResult struct {
	// Messages is the resolved (and possibly compressed) message list from the projection.
	// Does NOT include system prompt or current-turn messages — those are
	// prepended/appended by the BeforeModel callback.
	Messages []model.Message
	// RetainedRefs is the updated list of EventReferences to replace
	// the projection with after compression.
	RetainedRefs []memory.EventReference
	// Notices contains error/degradation notices injected during compression.
	Notices []model.Message
	// Compressed reports whether this round performed a real compaction
	// (budget-exceeded path). False for under-budget early returns and
	// degraded paths — only true results carry a persisted snapshot (D2,
	// tagent-compress-event-sourcing).
	Compressed bool
}
    CompressResult is the output of ContextCompressor.Compress.

type ContextCompressor struct {
	// Has unexported fields.
}
    ContextCompressor is the projection-only compression engine.

    It reads EventReferences from the SessionProjection, resolves them to
    messages via MemoryStore, checks token budget, and applies value-driven
    L0-L3 compression when over budget. Returns: - Resolved/compressed messages
    (the historical timeline) - Retained refs to update the projection - Error
    notices for engineering awareness

    Design principle: Projection is the SINGLE source of truth for the
    historical timeline. ContextCompressor does NOT reconcile against framework
    ContentRequestProcessor output — there is no content-based deduplication.
    The BeforeModel callback handles merging the compressed history with
    current-turn messages.

func NewContextCompressor(
	sc *SmartCompressor,
	memStore memory.MemoryStore,
	tokenCounter TokenCounter,
	maxTokens int,
	thresholdPct float64,
	keepRecent int,
	opts ...ContextCompressorOption,
) *ContextCompressor
    NewContextCompressor creates a ContextCompressor from a SmartCompressor. The
    SmartCompressor provides the L0-L3 compression strategy; ContextCompressor
    adds the ref-resolution and projection-management layer on top.

func (cc *ContextCompressor) BudgetLine() int
    BudgetLine exposes the effective compression trigger line (maxTokens ×
    currentThreshold) for introspection and tests — the number the "under budget
    (x <= y)" log prints. Resolved per boundary from the installed hot source;
    construction atomics when no source.

func (cc *ContextCompressor) BudgetUnrepresentable() int64
    BudgetUnrepresentable returns the cumulative count of budget-unrepresentable
    card states (diagnostics surface; 6.3 "无法表达状态传给诊断").

func (cc *ContextCompressor) Compress(
	ctx context.Context,
	refs []memory.EventReference,
) CompressResult
    Compress resolves all projection refs into messages, checks token budget,
    and compresses if over threshold.

    Input: - ctx: context for LLM calls (used by SmartCompressor) - refs:
    EventReferences from SessionProjection (the historical timeline)

    Output: - Messages: resolved (and possibly compressed) message
    list - RetainedRefs: updated refs (replaces projection) - Notices:
    error/degradation notices

    The returned Messages do NOT include a system prompt — the caller
    (BeforeModel callback) prepends system prompt and appends current-turn
    messages after calling Compress.

func (cc *ContextCompressor) CondensedTicketsLost() int64
    CondensedTicketsLost returns the cumulative count of recall tickets folded
    into prose by card condensation.

func (cc *ContextCompressor) FullBoundary() int64
    FullBoundary returns the current full-render window anchor . Zero means
    everything renders full (never compacted, or fewer retained refs than the
    window).

func (cc *ContextCompressor) KeepRecentValue() int
    KeepRecentValue returns the live keepRecent (introspection, 4.6) — hot
    source when installed, construction atomics otherwise.

func (cc *ContextCompressor) MarkMeditationKey(key int64)
    MarkMeditationKey 把该 EventKey 登记为冥想产出的锚点，供冥想身份的判定与重建时复用； 键集按需分配，压缩器为 nil 或
    key 为 0（无身份）时不记录。

func (cc *ContextCompressor) MeditationKeysSnapshot() []int64
    MeditationKeysSnapshot returns a sorted copy of the meditation protection
    keys (lock-held snapshot; safe for the caller to hold/traverse).
    Observability for the projection-rebuild reseed path and its tests ; the
    old snapshot.go consumer is gone but the read surface stays — ★ rendering
    correctness depends on these keys surviving restarts.

func (cc *ContextCompressor) SetFullBoundary(key int64)
    SetFullBoundary overrides the full-render window anchor. Intended for
    cross-instance boundary inheritance (e.g. simulations / future restart
    restoration): the anchor is process-local state, so a fresh compressor
    starts at zero unless explicitly seeded.

func (cc *ContextCompressor) SetHotSource(src func() HotNumbers)
    SetHotSource installs (or replaces) the pull source after construction — the
    owner's hot view is only reachable once the agent wiring exists (resident CM
    is built before its TagentAgent fields finish wiring, ).

func (cc *ContextCompressor) Threshold() float64
    Threshold reports the live compression threshold (the authoritative consumer
    value) for introspection/ops callers. OrgThreshold reads through here so
    ContextManager needs no separate non-atomic mirror (D4/M-3 single source;
    also removes a background-write vs read data race on the mirror). Resolved
    from the hot source when installed.

type ContextCompressorOption func(*ContextCompressor)
    ContextCompressorOption configures optional ContextCompressor constraints.

func WithCardMaxChars(n int) ContextCompressorOption
    WithCardMaxChars caps the index-card section length in the rolling summary
    (default DefaultCardMaxChars).

func WithCompactKeysListed(n int) ContextCompressorOption
    WithCompactKeysListed caps the number of keys listed in the rolling
    compaction summary (default DefaultCompactKeysListed).

func WithHotSource(src func() HotNumbers) ContextCompressorOption
    WithHotSource installs the pull source read at EVERY consumption boundary
    (BudgetLine/Threshold/KeepRecentValue/Compress). While installed the source
    wins per field; unset/invalid fields fall back to the construction atomics.

func WithRecentFullCount(n int) ContextCompressorOption
    WithRecentFullCount sets the full-window size anchored at compaction rounds,
    overriding the derived default (keepRecent × DefaultRefsPerTurn, see D6).

type DefaultTokenCounter struct {
	CharsPerToken float64
}
    DefaultTokenCounter estimates tokens using a character-based heuristic.

func NewDefaultTokenCounter() *DefaultTokenCounter
    NewDefaultTokenCounter 构造默认计量器，字符/token 比值取 2.0（中英混排的保守近似）。

func (c *DefaultTokenCounter) Estimate(messages []model.Message) int
    Estimate 按"字符数/比值 ＋ 每条固定开销"累加估算 token 用量。空消息集返回 0，**不计**任何
    每条开销——否则空集合会被估出正成本，压缩判定会误以为还有内容要处理。

type HotNumbers struct {
	ThresholdPct float64
	MaxTokens    int
	KeepRecent   int
}
    HotNumbers is the full numeric hot bundle consumed at compression
    boundaries. It must be taken with a single read (why a
    per-field read is unsafe is specified in the document below);
    zero or invalid fields fall back to the construction values,
    so an owner without any record yet still computes a sane budget. 契约:
    docs/wiki/agent/compression-and-telemetry.md#hot-bundle-atomicity

type RetainedEntry struct {
	Key int64 `json:"key"`
	// Ref is set only for negative-key synthetic entries (full identity +
	// EventSummary, byte-exact).
	Ref *memory.EventReference `json:"ref,omitempty"`
}
    RetainedEntry is one slot of the fold's ordered retained list. Positive keys
    carry only the key (the store is the source of truth: GetEvent rebuilds
    the ref byte-exact). Negative-key synthetic refs (tool_chain) carry the
    FULL ref verbatim — the store holds no event behind them and they are not
    recomputable (fresh-eyes: dropping them loses the `- 工具链:` render lines).

type SessionProjection struct {
	// Has unexported fields.
}
    SessionProjection is the bounded, lightweight projection of the event
    flow. It mirrors the prototype's `inputs []string`: onEvent appends,
    ContextManager reads, Compactor clears. Full event data lives in
    MemoryStore.

func NewSessionProjection() *SessionProjection
    NewSessionProjection 构造一个空投影：引用表与去重键集都是空的。

func (p *SessionProjection) Append(ref memory.EventReference)
    Append 追加一条事件引用，并对带键（EventKey>0）的事件幂等：同一键第二次追加会被跳过并
    记一条告警（重复投影会让同一条事实出现在上下文两次）。无键（EventKey==0）的引用不参与 去重，总是追加。

func (p *SessionProjection) GetAll() []memory.EventReference
    GetAll 返回引用表的**拷贝**：调用方（装配上下文的一侧）改写结果不会影响投影内部状态。

func (p *SessionProjection) Len() int
    Len 返回当前投影的引用条数（读锁下），供"是否已折叠/是否空投影"这类判定使用。

func (p *SessionProjection) Replace(refs []memory.EventReference)
    Replace 整表替换投影内容，并按新引用**重算**去重键集：因此被折叠掉的键会随之离开键集， 键集大小只随重建有界，不会单调增长。

func (p *SessionProjection) UpdateSummary(idx int, summary string)
    UpdateSummary updates the EventSummary of the ref at the given index.
    Silently returns if idx is out of bounds.

type SmartCompressor struct {
	KeepRecentTasks int

	// Has unexported fields.
}
    SmartCompressor performs deterministic context compression.

    Pipeline (skeleton model):
     1. Segment messages into task turns bounded by agent_output.
     2. Deterministic level per segment age (pure function).
     3. Per-segment drop: L0 (keep) / L1 (drop tool) / L2 (skeleton only) /

    L3 (multi-segment compaction — whole segment leaves the timeline).
     4. Assemble chronologically; kept messages keep their event key prefixes.

    This is a "view transformation" — it modifies the messages sent to the LLM,
    but does NOT modify the Session or Projection.

func NewSmartCompressor(opts ...SmartCompressorOption) *SmartCompressor
    NewSmartCompressor creates a new SmartCompressor.

func (sc *SmartCompressor) Compress(
	ctx context.Context,
	messages []model.Message,
) []model.Message
    Compress implements budget-aware compression via the skeleton pipeline :
    agent_output-bounded segments, age-driven deterministic levels,
    tool>assistant drop order, L3 multi-segment compaction. Pure engineering —
    no per-segment LLM summarization.

func (sc *SmartCompressor) CompressWithOptions(
	ctx context.Context,
	messages []model.Message,
	opts CompressOptions,
) []model.Message
    CompressWithOptions is Compress with per-call overrides (see
    CompressOptions).

type SmartCompressorOption func(*SmartCompressor)
    SmartCompressorOption configures SmartCompressor.

func WithKeepRecentTasks(n int) SmartCompressorOption
    WithKeepRecentTasks sets how many recent tasks to keep.

func WithMaxTokens(n int) SmartCompressorOption
    WithMaxTokens sets the token budget used for batch size calculation.

func WithSummaryEffort(effort string) SmartCompressorOption
    WithSummaryEffort WithSummaryMaxTokens sets the output-token budget floor
    for summary calls (0 → DefaultSummaryMaxTokens). Reasoning models spend
    part of max_tokens on their thinking chain; reserving enough output tokens
    keeps Content from coming back empty (which would degrade every segment).
    WithSummaryEffort sets the reasoning_effort for summary LLM calls (optional;
    nil = leave unset). (tagent-unify-model-call-config.)

func WithSummaryMaxTokens(n int) SmartCompressorOption
    WithSummaryMaxTokens 设置综述的 token 上限。仅对正数生效：0 或负数保持默认值，避免调用方
    用零值表达"不限制"时把综述压成空。

func WithSummaryModel(m model.Model) SmartCompressorOption
    WithSummaryModel sets the LLM model for index-card condensation.

func WithTokenCounter(tc TokenCounter) SmartCompressorOption
    WithTokenCounter 替换计量器：预算判定用哪套计数法由宿主决定，压缩策略本身不关心。

func WithTriggerBudget(n int) SmartCompressorOption
    WithTriggerBudget sets the post-compression token target. When > 0,
    aging escalation and L3 archival target this budget instead of maxTokens,
    aligning the compression target with the trigger line (unified-threshold
    mode). This eliminates the dead zone where a session baseline sits between
    the trigger line (threshold*maxTokens) and the budget line (maxTokens):
    the compressor fires at the trigger line but refuses to age anything because
    the budget appears unspent, causing an every-turn no-op compression loop.

type TaskSegment struct {
	Messages   []model.Message
	IsComplete bool
}
    TaskSegment is a group of messages delimited by task boundaries. Under
    the skeleton model a segment is one complete task turn `[external_input,
    (thinking_plan|action_command)*, agent_output]`, closed by the final reply.
    A trailing segment without an agent_output is in progress (IsComplete=false)
    and never compressed.

func SegmentMessages(messages []model.Message) []*TaskSegment
    SegmentMessages splits messages into task-turn segments bounded by
    agent_output (task-skeleton D1): an agent_output closes the current segment;
    consecutive external_input (user re-sends, agent silent) stay in the
    same in-progress segment; a trailing run without agent_output is left
    IsComplete=false.

type TokenCounter interface {
	Estimate(messages []model.Message) int
}
    TokenCounter estimates token count for message lists.


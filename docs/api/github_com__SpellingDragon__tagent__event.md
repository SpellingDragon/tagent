package event // import "github.com/SpellingDragon/tagent/event"

Package event 定义 tagent 的统一事件类型、事件元数据契约与时间线前缀契约：
类型注册表是事件类型静态属性的唯一权威源，投影/召回/嵌入/TTL 均由它派生。

CONSTANTS

const (
	MetaKeyEventKey      = "event_key"
	MetaKeyPartitionID   = "partition_id"
	MetaKeyEventType     = "event_type"
	MetaKeyEventSummary  = "event_summary"
	MetaKeyTriggerSource = "trigger_source"

	// MetaKeyTaskInlineRecord 标记回合内已作为 tool result 返回的账目记录，仅经
	// IsNonProjectionRecord 消费。
	MetaKeyTaskInlineRecord = "task_inline_record"

	// MetaKeySettleTriggerSource 结算血统章：持久化 SourceTask 事件时把事件自带的
	// 派生血统（由 SettleSignal.Lineage 盖入）提升为事实链一级键，与消费回合的
	// MetaKeyTriggerSource 并存可对账；source_snapshot 的无损快照不因提升而移除。
	MetaKeySettleTriggerSource = "settle_trigger_source"

	// MetaKeyAgentName 归因章键：写入 FullEvent.Metadata，使产出事件可回溯到生效的 agent、bundle 与回合。
	MetaKeyAgentName = "agent_name"
	MetaKeyBundleID  = "bundle_id"
	MetaKeyRolloutID = "rollout_id"

	// MetaKeyTraceID trace 关联键：使事件溯源、轨迹与遥测三个投影共用同一锚点双向互链。
	MetaKeyTraceID = "trace_id"
	MetaKeySpanID  = "span_id"

	// MetaKeySubtype 是治理子类型的唯一权威键名：写入方与取证方共用本常量，
	// 避免跨包字面量漂移（漂移会使取证侧拒绝计数归零，废掉快道回滚防线）。
	MetaKeySubtype = "subtype"

	SubtypeDenial   = "denial"
	SubtypeGoal     = "goal"
	SubtypeApproval = "approval"
	SubtypeDegraded = "degraded"
	SubtypeAudit    = "audit"

	// MetaPrefix 标记透传业务元数据键。
	MetaPrefix = "meta_"

	// MetaKeyInboxRequestID 输入身份键：durable 输入的身份以事实链为准（inbox 文件不是持久溯源源）；
	// 运行时 claim 状态由类型化字段承载，不进入这些业务元数据。
	MetaKeyInboxRequestID = "inbox_request_id"
	MetaKeyInboxSlot      = "inbox_slot"
	MetaKeySourceEventID  = "source_event_id"

	MetaKeySourceSnapshot = "source_snapshot"
)
    MetaKeyEventKey StateDelta 与 FullEvent.Metadata 的键常量：每个键在此定义一次，注入点引用常量， 消费方经
    ParseEventMeta 解析。谁写谁读见文档的键归属表。

const (
	TypeExternalInput = "external_input"

	TypeAgentOutput = "agent_output"

	TypeActionCommand = "action_command"

	TypeThinkingPlan = "thinking_plan"

	TypeThinkingRecall = "thinking_recall"

	TypeThinkingKnowledge = "thinking_knowledge"

	TypeContextCompressSummary = "context_compress_summary"

	TypeContextCompress = "context_compress"

	TypeToolChain = "tool_chain"

	TypeSettleFold = "settle_fold"

	TypeTaskSpawned = "task_spawned"

	TypeResidentSession = "resident_session"

	TypeConsolidation = "consolidation"

	TypeGovernance = "governance"

	TypeFeedback = "feedback"

	TypeCognitiveAssetChanged = "cognitive_asset_changed"
)
    TypeExternalInput 事件类型常量。除 agent_output 与 action_command 外的一切角色都归为
    external_input；超出上下文的内容由多轮压缩处理，不做截断。

const (
	// TypeWFReceived 记录一个输入批次成员已被持久接收。
	TypeWFReceived = "wf.received"
	// TypeWFIntent 在活动体执行前声明其身份三元组 {lineage,node,attempt}。
	TypeWFIntent = "wf.intent"
	// TypeWFResult 在活动推进前持久化其结果。
	TypeWFResult = "wf.result"
	// TypeWFSignal 承载外部持久信号（settle、send/resume、cancel、TTL 续期与到期）。
	TypeWFSignal = "wf.signal"
	// TypeWFTransition 是检查点转移事实，携带 {lineage,step,node}、前驱检查点引用、活动句柄与输出摘要。
	TypeWFTransition = "wf.transition"
	// TypeWFFinalized 是 workflow 终态事实：框架的 Done 本身不构成终态，只有此事实构成。
	TypeWFFinalized = "wf.finalized"
	// TypeWFSafeRemoved 是受保护材料已完成移除的证据事实；保留租约只在**已完成**的移除上释放，而非已发起。
	TypeWFSafeRemoved = "wf.safe_removed"
)
    TypeWFReceived 等 wf.* 是 workflow 运行时事实链的记录类型（逐项含义见各常量自己的
    doc）。引擎已撤回，此处注册的唯一作用是让历史 wf.* 记录继续被投影、召回与嵌入 排除，因此注册时 **TTLDays 必须保持 0** ——
    任何正值都会静默缩短既有记录的保留期。 事实链语义与由它折叠出的各视图以文档为唯一真源。

const (
	MetaKeyWFLineage = "wf_lineage"
	MetaKeyWFNode    = "wf_node"
	MetaKeyWFAttempt = "wf_attempt"
	MetaKeyWFStep    = "wf_step"
	MetaKeyWFKind    = "wf_kind"
	MetaKeyWFWriter  = "wf_writer"
	MetaKeyWFExtra   = "wf_extra"
)
    MetaKeyWFLineage wf_* 是 workflow 运行时溯源在事实链上的唯一权威身份键，写入 FullEvent.Metadata；
    消费方只经这些常量解析，不得使用字面量。

const LineageMeditation = "meditation"
    LineageMeditation is the self-initiated reflection turn trigger source.

    - It is host-visible, so its output may be delivered, yet remains
    self-managed traffic for the telemetry audit: the two-layer meaning every
    consumer of this package must keep.

const TypeInboxReceipt = "inbox_receipt"
    TypeInboxReceipt 标记「一个输入信封已被确认消费」的记账事实：其真源是事实链而非 inbox 文件。它的 TTL 就是
    request-id 的 30 天去重窗口，过期后同一 request-id 重投 不保证幂等。注册为非投影、非嵌入、非召回。

FUNCTIONS

func DefaultTypeTTL() map[string]int
    DefaultTypeTTL 返回全部显式声明 TTLDays（非 0，含 -1 豁免）的类型→天数映射（新 map）。

func DeliverableLineage(ts string) bool
    DeliverableLineage reports whether a trigger_source lineage is host-facing,
    i.e. a reclaim carrying it was or can be delivered to the host, so its
    verbatim notice may age out.

    - Anything outside this whitelist is internal and fail-closed
    withheld: unknown values are never delivered. - Adding an
    externally-visible lineage means adding it here and nowhere else;

func EncodeSourceSnapshot(source string, metadata map[string]any) (string, error)
    EncodeSourceSnapshot 渲染持久化用的快照 JSON；来源与元数据皆空时返回空串。

func EstimateTokens(text string) int
    EstimateTokens 以约 3 字符一 token 估算文本 token 数。

func EventTypeRole(name string) model.Role
    EventTypeRole 返回类型的渲染角色。

func ExtractEventType(msg model.Message) string
    ExtractEventType 按消息角色判定事件类型。RoleSystem 会出现在事件流中 （如常驻监控注入的状态通知）并归为
    external_input；系统提示词本身不属于事件流。

func FormatEventDescription(index int, msg model.Message) string
    FormatEventDescription 为压缩生成保留全部信息的结构化多行描述。

func FormatEventKey(key int64) string
    FormatEventKey 以规范小写十六进制渲染 EventKey（负 key 保留前导 -）。

func FormatEventPrefix(key int64, eventType string) string
    FormatEventPrefix 渲染时间线行的规范前缀 `[evt_<KEY>|<type>] `，KEY 用十六进制。 写入端与本文件的读取端
    ParseEventKeyAndType 同处一包，渲染与解析不会各自演化。

func GenerateEventSummary(msg model.Message, eventType string, opts EventSummaryOptions) string
    GenerateEventSummary 生成事件的 event_summary 元数据视图：多数类型是原文逐字视图， action_command
    是一行机械工具调用行。内容级摘要属压缩与策展管线，不在此处。

func HasEventPrefix(content string) bool
    HasEventPrefix reports whether content starts with a timeline prefix.

func IsEmbeddableType(name string) bool
    IsEmbeddableType 报告类型是否纳入向量索引。

func IsLowValueType(name string) bool
    IsLowValueType 报告类型内容是否可在深层压缩中丢弃。

func IsNonProjectionEventType(name string) bool
    IsNonProjectionEventType 报告类型是否事实链内部记录（永不进投影）。

func IsNonProjectionRecord(eventType string, metadata map[string]string) bool
    IsNonProjectionRecord 是「可否进投影」的唯一判定源：类型维度取注册表，外加携带 task_inline_record
    标记的事件（终态 settle 已在回合内返回，再进投影即双呈现）。 所有 append 路径必须共用本谓词。

func IsRecallableType(name string) bool
    IsRecallableType 报告类型的原文票据能否被取回。

func IsSkeletonEventType(name string) bool
    IsSkeletonEventType 报告类型是否为压缩骨架节点（未知类型保守为 true）。

func IsSpecialEventType(eventType string) bool
    IsSpecialEventType 报告该类型是否原文优先。集合由事件类型注册表定义。

func IsSyntheticEventType(name string) bool
    IsSyntheticEventType 报告类型是否为使用负 EventKey 的合成投影引用。

func LowValueTypes() map[string]bool
    LowValueTypes 返回全部低价值类型名（新 map，调用方可自行持有）。

func ParseEventKey(s string) (int64, error)
    ParseEventKey 解析规范十六进制字符串，并容忍模型回显票据的常见形态：0x 前缀、 evt_ 前缀、完整 [evt_HEX|type]、尾随
    |type 或 ]。

func ParseEventKeyAndType(content string) (key int64, eventType string, remainder string)
    ParseEventKeyAndType extracts EventKey and EventType from a message content
    with "[evt_<KEY>|<type>] <remainder>" prefix. Returns (0, "unknown",
    content) if no valid prefix is found.

func RegisterEventType(spec EventTypeSpec)
    RegisterEventType 注册或覆盖一个类型 spec（同名覆盖，供子系统显式重声明）。 空 Name 被忽略。全链路属性由这一处声明派生。

func RegisteredEventTypes() []string
    RegisteredEventTypes 返回全部已注册类型名，供诊断与守卫断言使用。

func SelfManagedLineage(ts string) bool
    SelfManagedLineage reports the telemetry-audit sense: traffic the agent
    initiated for itself rather than a user-awaited interaction. It is derived
    from the same whitelist — withheld lineages are self-managed by definition,
    and meditation keeps its second layer (host-visible yet self-initiated).

func StripEventKeyPrefix(content string) string
    StripEventKeyPrefix removes a leading [evt_KEY|type] prefix from content.
    Returns the original content if no prefix is found.

func WFExcludedTypes() []string
    WFExcludedTypes 返回全部 wf.* 类型名，供诊断与守卫断言使用；投影/召回/嵌入的排除 判定不依赖此列表，而统一走
    IsNonProjectionRecord 与注册表。

TYPES

type EventMeta struct {
	EventKey      int64
	PartitionID   int
	EventType     string
	EventSummary  string
	TriggerSource string
	Meta          map[string]string
}
    EventMeta 是投递事件元数据的解析结果。

func ParseEventMeta(evt *frameworkevent.Event) EventMeta
    ParseEventMeta 提取元数据契约；缺失字段留零值，Meta 恒非 nil。

type EventSummaryOptions struct {
	StructuredFormat bool
}
    EventSummaryOptions 配置 event_summary 视图的呈现形态。
    内容截断被严格禁止：超量内容交由多轮压缩处理，任何非设计的信息折损都会污染压缩质量。

func DefaultOptionsForCompression() EventSummaryOptions
    DefaultOptionsForCompression 面向压缩：多行以保信息完整，不截断。

func DefaultOptionsForLLMContext() EventSummaryOptions
    DefaultOptionsForLLMContext 面向 LLM 上下文：单行以省 token，不截断。

type EventTypeSpec struct {
	Name string

	Role model.Role

	Special bool

	ToolLineSummary bool

	Skeleton bool

	LowValue bool

	TTLDays int

	Synthetic bool

	Embeddable bool

	Recallable bool

	NonProjection bool
}
    EventTypeSpec 声明一个事件类型的全链路静态属性：角色、是否原文优先、摘要形态、 是否压缩骨架、是否低价值、类型级
    TTL、是否合成投影引用、是否可嵌入、是否可召回、 是否永不进投影。未注册类型回退 defaultSpec，与引入注册表前对未知类型的处理一致。

func LookupEventType(name string) (EventTypeSpec, bool)
    LookupEventType 返回类型 spec 及是否已注册。

type SourceSnapshot struct {
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
    SourceSnapshot 冻结 durable 输入的原始来源与完整业务 Metadata，以 JSON 存于
    MetaKeySourceSnapshot。

func DecodeSourceSnapshot(raw string) (SourceSnapshot, error)
    DecodeSourceSnapshot 解析快照 JSON；空串得到零值。

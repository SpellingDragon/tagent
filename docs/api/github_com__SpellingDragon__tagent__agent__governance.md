package governance // import "github.com/SpellingDragon/tagent/agent/governance"

Package governance 承载 tagent 的有界自治与审计：治理是闸不是墙，OS 降权仍是最后防线。

- 风险分级 + 预算 + goal 登记 + critical 异步人工批准构成闸面；所有分级与裁决为纯函数（无 IO 无随机），规则表数据驱动，拒绝必记账。

- 契约 C5：`RiskClassifier.Classify(RiskContext) → (level, ruleID, reason)`，消费方是
GovernanceGate。

CONSTANTS

const (
	SubtypeDenial   = event.SubtypeDenial
	SubtypeGoal     = event.SubtypeGoal
	SubtypeApproval = event.SubtypeApproval
	SubtypeDegraded = event.SubtypeDegraded
	SubtypeAudit    = event.SubtypeAudit
)
    SubtypeDenial governance 事件的 subtype 值——权威源在
    event 包（C3/C4：evolution.StoreEvidenceSource 也引用
    event.Subtype*，消除跨包字面量复制的静默漂移）。此处别名保持 governance 内部引用不变。

VARIABLES

var ErrBudgetExhausted = budgetExhaustedError{}
    ErrBudgetExhausted 表示窗口内该风险级别预算耗尽。

FUNCTIONS

func ArgsDigest(argsJSON string) string
    ArgsDigest 计算参数摘要（sha256），批准绑定此摘要防「批准后换参数」。

func ParseApprovalReply(text string) (digest string, approve bool, ok bool)
    ParseApprovalReply 解析消息通道的人工回复（3.3 微信注入侧共用纯函数）： "approve <digest>" / "reject
    <digest>"（大小写不敏感，digest ≥8 hex）。 非审批回复返回 ok=false（调用方按普通消息处理）。

func RespondFile(approvalsDir, digest string, approve bool, by string) (string, error)
    RespondFile 是 CLI 与消息通道共用的**人工回应纯函数**（3.2/3.3）：在 approvals
    目录中定位 digest 前缀匹配的 pending 请求，写入 approved/denied 状态（DecidedBy
    留痕）。幂等：已回应（approved/denied）的请求不重复改写，返回说明。 digest 支持短前缀（≥8 字符，与 approval_list
    展示的短 digest 一致）。

func TriggerSourceFrom(ctx context.Context) string
    TriggerSourceFrom 从 ctx 读触发源（未盖章则空——goal 检查据此不误触发）。

func WithTriggerSource(ctx context.Context, source string) context.Context
    WithTriggerSource 把触发源（user/meditation/task/tmux/subagent/inject）存入 ctx。
    event loop 每回合盖章，GovernanceTool 读取用于 goal-required 判定（meditation/task 须挂
    goal）。

TYPES

type ApprovalChannel interface {
	Deliver(req *ApprovalRequest) error
}
    ApprovalChannel 是审批请求的送达通道（微信注入/邮件/IM 等由装配层实现）。 Deliver
    失败仅记日志——审批门不依赖任何通道在线。

type ApprovalManager struct {
	// Has unexported fields.
}
    ApprovalManager 管理异步批准请求（文件通道）。并发安全（内存索引 + 文件持久）。

func NewApprovalManager(dir string, ttl time.Duration) *ApprovalManager
    NewApprovalManager 构建批准管理器。dir 非空时持久化到 <dir>/approvals/ 并重建索引。

func (a *ApprovalManager) AddChannel(ch ApprovalChannel)
    AddChannel 注册送达通道（装配期）。Request 成功后逐个 Deliver（尽力）。

func (a *ApprovalManager) Check(toolName, argsDigest string) *ApprovalRequest
    Check 查找匹配 (toolName, argsDigest) 的、已批准且未过期的请求（批准放行判据）。 精确匹配 digest →
    防「批准后换参数」。无匹配返回 nil（调用方据此挂起/拒绝）。 W2：索引未命中时**节流重扫** approvals 目录——外部审批者（人工
    digest 文件 / 微信 通道回写）在运行中落盘批准文件后须可见。否则 Check 只读构造时索引 → 运行中外部批准永不 可见 →
    critical 恒 Hold、重试持续堆积 pending（治理审批闭环断路）。

func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) error
    Decide 记录批准决策（外部审批者经 CLI/文件调用）。写回文件 + 更新索引。

func (a *ApprovalManager) Pending() []*ApprovalRequest
    Pending 返回全部未过期 pending 请求（供审批通道展示），按创建时间升序。

func (a *ApprovalManager) Request(toolName, argsJSON, argsPreview, level, ruleID, reason, goalID string) (*ApprovalRequest, error)
    Request 登记一个 pending 批准请求（写文件 + 内存索引）。返回请求（含 ID/ExpiresMs）。

type ApprovalRequest struct {
	ID          string         `json:"id"`
	ToolName    string         `json:"tool"`
	ArgsDigest  string         `json:"args_digest"`
	ArgsPreview string         `json:"args_preview"`
	RiskLevel   string         `json:"risk"`
	RuleID      string         `json:"rule_id"`
	Reason      string         `json:"reason"`
	GoalID      string         `json:"goal_id,omitempty"`
	CreatedMs   int64          `json:"created_ms"`
	ExpiresMs   int64          `json:"expires_ms"`
	Status      ApprovalStatus `json:"status"`
	DecidedBy   string         `json:"decided_by,omitempty"`
}
    ApprovalRequest 是一次批准请求（一请求一文件，可被外部审批者读写）。

type ApprovalStatus string
    ApprovalStatus 是批准状态。

const (
	// ApprovalPending 等待外部批准者裁决；此时被治理的操作不会执行。
	ApprovalPending ApprovalStatus = "pending"
	// ApprovalApproved 已批准：允许继续执行该次操作。
	ApprovalApproved ApprovalStatus = "approved"
	// ApprovalDenied 已拒绝：操作终止并作为拒绝结果返回。
	ApprovalDenied ApprovalStatus = "denied"
	// ApprovalExpired 超过有效期未被裁决：按未获批准处理，不得事后凭旧请求继续执行。
	ApprovalExpired ApprovalStatus = "expired"
)
type BudgetConfig struct {
	Window        time.Duration
	BucketCount   int
	MaxHighRisk   int
	MaxMediumRisk int
}
    BudgetConfig 配置预算窗口与各级上限。

type BudgetManager struct {
	// Has unexported fields.
}
    BudgetManager 是滑动窗口预算闸（并发安全，持久化 epoch 防重启刷预算）。

func NewBudgetManager(cfg BudgetConfig, dir string) *BudgetManager
    NewBudgetManager 构建预算闸。dir 非空时持久化到 <dir>/budget.json 并恢复 epoch。
    dir 为空即纯内存、绝不落盘。调用方要把 dir 原样交下来由本函数判断，不能先自行拼接目录片段： 空串参与拼接会得到相对路径（如
    budget/name），预算被意外写进进程当前目录，违反「空 dir＝纯内存」。

func (b *BudgetManager) Admit(level RiskLevel) error
    Admit 判定并（若放行）计入一次该级别操作。critical/low 直接放行（不占预算）； high/medium 超限返回
    ErrBudgetExhausted。

func (b *BudgetManager) Usage(level RiskLevel) int64
    Usage 返回当前窗口各级别已用预算（诊断/可观测）。

type Decision struct {
	Disposition Disposition
	Level       RiskLevel
	RuleID      string
	Reason      string
	ApprovalID  string
	Denied      bool
	DenyReason  string
}
    Decision 是一次治理裁决。

type DenialLedger struct {
	// Has unexported fields.
}
    DenialLedger 是治理账本：内存索引 + governance 事件（可选持久化到 MemoryStore）。

func NewDenialLedger(store memory.MemoryStore, partitionID int) *DenialLedger
    NewDenialLedger 构建账本。store 非 nil 时记录同步写 governance 事件（可 recall 审计）。

func (l *DenialLedger) BindStore(store memory.MemoryStore, partitionID int)
    BindStore 延迟绑定持久化 store：所有 agent gate 共享同一 DenialLedger 实例，但 entry memStore
    在子 agent 之后才就绪（entry 依赖子 agent，buildAgent 递归先构造子 agent）， 故 Ledger 先以 nil
    store 创建（纯内存），entry buildAgent 时经本方法绑定持久 store + rebuild。 绑定后所有 gate（含子
    agent 主风险面 exec/save_file/mcp_call）的治理记录写同一 entry governance 分区（durable，重启可
    recall）——修复 W3 子 agent gate 兜底内存账本致审计重启即失。

func (l *DenialLedger) Count() int
    Count 返回账本记录总数。

func (l *DenialLedger) Query(limit int) []DenialRecord
    Query 返回最近 limit 条记录（新→旧）。limit<=0 返回全部。

func (l *DenialLedger) Record(rec DenialRecord)
    Record 记一条治理记录（内存索引 + governance 事件）。写事件失败不阻断（记账尽力）。

type DenialRecord struct {
	Subtype    string    `json:"subtype"`
	ToolName   string    `json:"tool"`
	Level      RiskLevel `json:"level"`
	RuleID     string    `json:"rule_id"`
	Reason     string    `json:"reason"`
	ArgsDigest string    `json:"args_digest,omitempty"`
	GoalID     string    `json:"goal_id,omitempty"`
	// AgentName 标注记录来源 agent：W3 后所有 agent 共享同一 entry Ledger，无此字段则
	// 多 agent 治理事件无法区分来源。omitempty 保持单 entry 场景（历史事件无 agent）向后兼容。
	AgentName string `json:"agent,omitempty"`
	Timestamp int64  `json:"ts"`
}
    DenialRecord 是一条治理记录（拒绝/审计）。

type Disposition int
    Disposition 是三档处置（由风险级别 + 策略派生）。

const (
	// DispositionAllow 放行内层工具，无额外留痕要求。
	DispositionAllow Disposition = iota
	// DispositionRecord 放行，但要求把这次执行记入审计账本。
	DispositionRecord
	// DispositionHold 拦下：交给批准流程，内层工具不执行；放行后才委托内层。
	DispositionHold
)
func DispositionFor(level RiskLevel) Disposition
    DispositionFor Disposition 由风险级别派生处置（默认策略；可被 Policy 覆盖）。 critical →
    挂起批准；high/medium → 记账放行；low → 直接放行。

func (d Disposition) String() string
    String 返回处置名。

type Enforcement string
    Enforcement 是 goal/预算违规的处置模式。

const (
	// EnforcementWarn 只告警：违规被记账并暴露，但不阻断执行。缺省即此值。
	EnforcementWarn Enforcement = "warn"
	// EnforcementStrict 严格执行：违规判为拒绝。是否真的 denied 只由此档决定。
	EnforcementStrict Enforcement = "strict"
)
type GateConfig struct {
	Enabled         bool
	Enforcement     Enforcement
	GoalRequiredFor []string
}
    GateConfig 配置治理门。

type GateDeps struct {
	Classifier *RiskClassifier
	Budget     *BudgetManager
	Approval   *ApprovalManager
	Ledger     *DenialLedger
	Goals      *GoalRegistry
	Config     GateConfig
	// AgentName 是本 gate 服务的 agent 名：治理记录写事件时标注来源 agent。W3 后所有
	// agent 共享同一 entry Ledger，无此字段则多 agent 治理事件无法区分来源。
	AgentName string
}
    GateDeps 是构建 GovernanceGate 的依赖集（nil 组件按各自降级语义处理）。

type Goal struct {
	ID        string     `json:"id"`
	Statement string     `json:"statement"`
	CreatedBy string     `json:"created_by"`
	Status    GoalStatus `json:"status"`
	CreatedMs int64      `json:"created_ms"`
	ExpiresMs int64      `json:"expires_ms,omitempty"`
}
    Goal 是一条自治目标声明。

type GoalRegistry struct {
	// Has unexported fields.
}
    GoalRegistry 管理 goal 声明（有界自治：high+ 操作须挂 goal）。并发安全。
    store/partitionID：BindStore 延迟绑定后 Declare/Resolve 双写 governance
    事件，重启经事件回放重建。

func NewGoalRegistry() *GoalRegistry
    NewGoalRegistry 构建 goal 注册表。

func (g *GoalRegistry) Active() []*Goal
    Active 返回全部 active goal（诊断/工具展示）。

func (g *GoalRegistry) BindStore(store memory.MemoryStore, partitionID int)
    BindStore 绑定持久化存储并回放重建（重启不丢 goal 声明）。nil 安全； 构造期单线程调用（对齐
    DenialLedger.BindStore 纪律）。

func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string
    Declare 登记一个 goal，返回其 ID。BindStore 后同步写 governance 事件（5.2）。 8.7：事件
    Timestamp/EventKey 在锁内分配——并发 Declare/Resolve 时 事件的 (Timestamp, EventKey)
    全序与内存操作序一致，rebuild 不会让已关闭 goal 复活。

func (g *GoalRegistry) HasActive() bool
    HasActive 报告是否存在未过期的 active goal（GovernanceGate 的 goal 检查判据）。

func (g *GoalRegistry) List() []*Goal
    List 返回全部 goal 的快照副本（5.1 goal_list 工具消费；按 ID 序不保证， 调用方按需排序）。返回副本防外部改动内部状态。

func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool
    Resolve 更新 goal 状态。BindStore 后同步写 governance 事件（5.2，锁内时序见 Declare）。

type GoalStatus string
    GoalStatus 是 goal 生命周期状态。

const (
	// GoalActive 目标在途，仍占用治理预算并参与门禁判定。
	GoalActive GoalStatus = "active"
	// GoalAchieved 已达成：不计入在途目标。
	GoalAchieved GoalStatus = "achieved"
	// GoalAbandoned 被主动放弃（例如被更优路径取代或人工终止）。
	GoalAbandoned GoalStatus = "abandoned"
	// GoalExpired 超出有效期未达成；与放弃区分，用于统计与预算归因。
	GoalExpired GoalStatus = "expired"
)
type GovernanceGate struct {
	// Has unexported fields.
}
    GovernanceGate 是治理决策管线（并发安全：各组件自身并发安全，Gate 无额外可变状态）。

func NewGovernanceGate(deps GateDeps) *GovernanceGate
    NewGovernanceGate 构建治理门。缺失组件用安全默认（classifier 默认规则，其余 nil 跳过）。

func (g *GovernanceGate) Approval() *ApprovalManager
    Approval 暴露审批管理器：供消息/CLI 审批通道调 Decide（批准/拒绝 pending）与
    Pending（展示待批）。审批送达形态（用户裁决）= **文件为主 + 预留微信接口**：外部审批者直接 写
    <dir>/approvals/<id>.json（人工 digest 落盘）经 Check 节流重扫可见；微信交互通道则调本 访问器
    Decide(id, status, by) 回写。此前 Gate 不暴露 Approval → Decide 全仓无调用方 → critical 恒
    Hold（审批闭环断路）。

func (g *GovernanceGate) Classifier() *RiskClassifier
    Classifier/Goals/Config 暴露跨 agent 共享组件（W3）：buildAgent 为每个 agent 构造独立
    GovernanceGate（per-agent BudgetManager，用户裁决子 agent 独立预算）时复用这些共享件——
    Classifier 纯函数无状态、Goals 全局注册表、Config 同策略。均 nil-safe。

func (g *GovernanceGate) Config() GateConfig
    Config 交出当前治理门配置；门为 nil 时返回零值配置（即未启用），而不是 panic。

func (g *GovernanceGate) Enabled() bool
    Enabled 报告治理是否开启。

func (g *GovernanceGate) Evaluate(ctx RiskContext) Decision
    Evaluate 对一个工具调用做治理裁决（管线：classify → 批准 → goal → 预算 → 记账）。

func (g *GovernanceGate) Goals() *GoalRegistry
    Goals 交出目标登记表；门为 nil 时返回 nil，调用方据此跳过目标判定。

func (g *GovernanceGate) Ledger() *DenialLedger
    Ledger 暴露治理账本（诊断/审计查询入口）。

type GovernanceTool struct {
	// Has unexported fields.
}
    GovernanceTool 是治理闸装饰器（实现 trpctool.Tool + trpctool.CallableTool）。

func NewGovernanceTool(inner trpctool.Tool, gate *GovernanceGate) *GovernanceTool
    NewGovernanceTool 包裹内层工具。gate 为 nil 或关闭时 Call 直接透传（零开销）。

func (t *GovernanceTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call 先经治理闸裁决，放行才委托内层执行。

func (t *GovernanceTool) Declaration() *trpctool.Declaration
    Declaration 透传内层声明（治理不改工具声明 → prefix-cache 稳定）。

func (t *GovernanceTool) Inner() trpctool.Tool
    Inner 返回被包裹的内层工具（供下游按具体类型断言，如需要）。

type RiskClassifier struct {
	// Has unexported fields.
}
    RiskClassifier 是纯函数风险分级器（契约 C5）。规则按序匹配，首中即返回； 全不中返回默认级别（保守
    medium）。无状态、并发安全（规则只读）。

func NewRiskClassifier(rules []Rule, defaultLevel RiskLevel) *RiskClassifier
    NewRiskClassifier 构建分级器。rules 为空则用 DefaultRules()；defaultLevel<=0 取
    RiskMedium。

func (c *RiskClassifier) Classify(ctx RiskContext) (RiskLevel, string, string)
    Classify 分级（契约 C5）：返回 (级别, 规则ID, 理由)。纯函数——同输入同输出。

type RiskContext struct {
	ToolName      string
	ArgsJSON      string
	TriggerSource string
}
    RiskContext 是分级输入（纯数据，无 IO）——GovernanceTool 装饰器从工具调用构造。

type RiskLevel int
    RiskLevel 是四级风险（低→危急）。

const (
	// RiskLow 低风险：默认放行，不额外要求批准。
	RiskLow RiskLevel = iota
	// RiskMedium 中风险：由规则决定是否只记账放行。
	RiskMedium
	// RiskHigh 高风险：需批准后方可执行。
	RiskHigh
	// RiskCritical 最高风险：恒走异步批准流程，绝不因放行策略而跳过。
	RiskCritical
)
func (l RiskLevel) String() string
    String 返回风险级别名（记账/日志/事件用）。

type Rule struct {
	ID     string
	Level  RiskLevel
	Reason string
	Match  func(ctx RiskContext) bool
}
    Rule 是一条数据驱动的风险规则（纯函数匹配）。

func DefaultRules() []Rule
    DefaultRules 返回 tagent 工具集的默认风险规则表（数据驱动，按序匹配，危急优先）。
    设计：exec（shell）是主风险面，按命令内容分级；文件写/删中危；只读工具低危。

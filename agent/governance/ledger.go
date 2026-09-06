package governance

import (
	"fmt"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
)

// ==================== DenialLedger + GoalRegistry（T-G · 审计与有界自治）====================
//
// DenialLedger（报告 D3 §4.4.2）：拒绝必记原因，可审计可分析。账本 = 事件流子集——
// 单 `governance` 事件类型（Metadata.subtype 区分）走正常 StoreEvent 路径 + 内存索引
// （启动从 QueryEvents 重建）。选单类型而非五类型：每加类型有注册成本，审计查询天然单类型过滤。
//
// GoalRegistry（报告 D3 §4.6.1）：自治须挂登记 goal。默认 enforcement=warn（记账放行 +
// 提示），strict 才拒绝——强制 goal 声明增加模型负担，先 warn 收集数据再升 strict。

// governance 事件的 subtype 值——权威源在 event 包（C3/C4：evolution.StoreEvidenceSource
// 也引用 event.Subtype*，消除跨包字面量复制的静默漂移）。此处别名保持 governance 内部引用不变。
const (
	SubtypeDenial   = event.SubtypeDenial
	SubtypeGoal     = event.SubtypeGoal
	SubtypeApproval = event.SubtypeApproval
	SubtypeDegraded = event.SubtypeDegraded
	SubtypeAudit    = event.SubtypeAudit
)

// DenialRecord 是一条治理记录（拒绝/审计）。
type DenialRecord struct {
	Subtype    string    `json:"subtype"`
	ToolName   string    `json:"tool"`
	Level      RiskLevel `json:"level"`
	RuleID     string    `json:"rule_id"`
	Reason     string    `json:"reason"`
	ArgsDigest string    `json:"args_digest,omitempty"`
	GoalID     string    `json:"goal_id,omitempty"`
	// AgentName 标注记录来源 agent（§8.1）：W3 后所有 agent 共享同一 entry Ledger，无此字段则
	// 多 agent 治理事件无法区分来源。omitempty 保持单 entry 场景（历史事件无 agent）向后兼容。
	AgentName string `json:"agent,omitempty"`
	Timestamp int64  `json:"ts"`
}

// DenialLedger 是治理账本：内存索引 + governance 事件（可选持久化到 MemoryStore）。
type DenialLedger struct {
	store       memory.MemoryStore // 可选：nil = 纯内存（测试/无持久化）
	partitionID int

	mu      sync.RWMutex
	records []DenialRecord
}

// NewDenialLedger 构建账本。store 非 nil 时记录同步写 governance 事件（可 recall 审计）。
func NewDenialLedger(store memory.MemoryStore, partitionID int) *DenialLedger {
	l := &DenialLedger{store: store, partitionID: partitionID}
	if store != nil {
		l.rebuildFromStore()
	}
	return l
}

// BindStore 延迟绑定持久化 store（N2，§8.9）：所有 agent gate 共享同一 DenialLedger 实例，但
// entry memStore 在子 agent 之后才就绪（entry 依赖子 agent，buildAgent 递归先构造子 agent），
// 故 Ledger 先以 nil store 创建（纯内存），entry buildAgent 时经本方法绑定持久 store + rebuild。
// 绑定后所有 gate（含子 agent 主风险面 exec/save_file/mcp_call）的治理记录写同一 entry
// governance 分区（durable，重启可 recall）——修复 W3 子 agent gate 兜底内存账本致审计重启即失。
func (l *DenialLedger) BindStore(store memory.MemoryStore, partitionID int) {
	if l == nil || store == nil {
		return
	}
	l.mu.Lock()
	l.store = store
	l.partitionID = partitionID
	l.mu.Unlock()
	l.rebuildFromStore() // 加载已持久化治理事件（重启恢复审计）
}

// Record 记一条治理记录（内存索引 + governance 事件）。写事件失败不阻断（记账尽力）。
func (l *DenialLedger) Record(rec DenialRecord) {
	if rec.Timestamp == 0 {
		rec.Timestamp = time.Now().UnixMilli()
	}
	l.mu.Lock()
	l.records = append(l.records, rec)
	// ①（§9.1）锁内快照 store/partitionID：BindStore 并发写这两字段（同锁保护），若在锁外读
	// l.store 则与 BindStore 竞争（-race data race）。快照后锁外写事件，锁纪律一致。
	store, pid := l.store, l.partitionID
	l.mu.Unlock()

	if store != nil {
		l.writeGovernanceEvent(rec, store, pid)
	}
}

// Query 返回最近 limit 条记录（新→旧）。limit<=0 返回全部。
func (l *DenialLedger) Query(limit int) []DenialRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	n := len(l.records)
	start := 0
	if limit > 0 && n > limit {
		start = n - limit
	}
	out := make([]DenialRecord, 0, n-start)
	for i := n - 1; i >= start; i-- {
		out = append(out, l.records[i])
	}
	return out
}

// Count 返回账本记录总数。
func (l *DenialLedger) Count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.records)
}

func (l *DenialLedger) writeGovernanceEvent(rec DenialRecord, store memory.MemoryStore, partitionID int) {
	content := fmt.Sprintf("[governance:%s] tool=%s level=%s rule=%s reason=%s",
		rec.Subtype, rec.ToolName, rec.Level, rec.RuleID, rec.Reason)
	metadata := map[string]string{
		event.MetaKeySubtype: rec.Subtype,
		"tool":               rec.ToolName,
		"level":              rec.Level.String(),
		"rule_id":            rec.RuleID,
		"reason":             rec.Reason,
		"args_digest":        rec.ArgsDigest,
		"goal_id":            rec.GoalID,
	}
	if rec.AgentName != "" {
		// §8.1：来源 agent（omitempty 语义——单 entry 场景不写噪声空键，历史事件也无此键）。
		metadata["agent"] = rec.AgentName
	}
	evt := memory.FullEvent{
		EventKey:     memory.NewSnowflakeEventKey(partitionID, 0),
		PartitionID:  partitionID,
		EventType:    event.TypeGovernance,
		EventSummary: content,
		Content:      content,
		Timestamp:    rec.Timestamp,
		Metadata:     metadata,
	}
	// 尽力写入（治理账本失败不阻断主链路）。
	_ = store.StoreEvent(evt.EventKey, evt)
}

func (l *DenialLedger) rebuildFromStore() {
	refs, err := l.store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{l.partitionID},
		EventTypes:   []string{event.TypeGovernance},
		Limit:        10000,
		OrderBy:      "timestamp_asc",
	})
	if err != nil {
		return
	}
	events, _ := l.store.GetEvents(keysOf(refs))
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range events {
		l.records = append(l.records, DenialRecord{
			Subtype: e.Metadata[event.MetaKeySubtype], ToolName: e.Metadata["tool"],
			Level: parseRiskLevel(e.Metadata["level"]), RuleID: e.Metadata["rule_id"],
			Reason: e.Metadata["reason"], ArgsDigest: e.Metadata["args_digest"],
			GoalID: e.Metadata["goal_id"], AgentName: e.Metadata["agent"], Timestamp: e.Timestamp,
		})
	}
}

func keysOf(refs []memory.EventReference) []int64 {
	out := make([]int64, len(refs))
	for i, r := range refs {
		out[i] = r.EventKey
	}
	return out
}

func parseRiskLevel(s string) RiskLevel {
	switch s {
	case "low":
		return RiskLow
	case "medium":
		return RiskMedium
	case "high":
		return RiskHigh
	case "critical":
		return RiskCritical
	default:
		return RiskMedium
	}
}

// ==================== GoalRegistry ====================

// GoalStatus 是 goal 生命周期状态。
type GoalStatus string

const (
	GoalActive    GoalStatus = "active"
	GoalAchieved  GoalStatus = "achieved"
	GoalAbandoned GoalStatus = "abandoned"
	GoalExpired   GoalStatus = "expired"
)

// Goal 是一条自治目标声明。
type Goal struct {
	ID        string     `json:"id"`
	Statement string     `json:"statement"`
	CreatedBy string     `json:"created_by"` // "user"|"agent"
	Status    GoalStatus `json:"status"`
	CreatedMs int64      `json:"created_ms"`
	ExpiresMs int64      `json:"expires_ms,omitempty"` // 0 = 不过期
}

// GoalRegistry 管理 goal 声明（有界自治：high+ 操作须挂 goal）。并发安全。
// store/partitionID（5.2 design-report-closeout）：BindStore 延迟绑定后
// Declare/Resolve 双写 governance 事件，重启经事件回放重建。
type GoalRegistry struct {
	mu    sync.RWMutex
	goals map[string]*Goal
	seq   int

	store       memory.MemoryStore
	partitionID int
}

// NewGoalRegistry 构建 goal 注册表。
func NewGoalRegistry() *GoalRegistry {
	return &GoalRegistry{goals: make(map[string]*Goal)}
}

// Declare 登记一个 goal，返回其 ID。BindStore 后同步写 governance 事件（5.2）。
// 8.7（review §8）：事件 Timestamp/EventKey 在锁内分配——并发 Declare/Resolve 时
// 事件的 (Timestamp, EventKey) 全序与内存操作序一致，rebuild 不会让已关闭 goal 复活。
func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string {
	g.mu.Lock()
	g.seq++
	id := fmt.Sprintf("g-%d", g.seq)
	now := time.Now().UnixMilli()
	goal := &Goal{
		ID: id, Statement: statement, CreatedBy: createdBy,
		Status: GoalActive, CreatedMs: now, ExpiresMs: expiresMs,
	}
	g.goals[id] = goal
	evtKey := g.eventKeyLocked()
	pid := g.partitionID
	g.mu.Unlock()
	g.writeGoalEvent(goalEventPayload{
		GoalID: id, Op: "declared", Statement: statement,
		CreatedBy: createdBy, ExpiresMs: expiresMs,
	}, evtKey, now, pid)
	return id
}

// eventKeyLocked 在锁内为 governance 事件分配 Snowflake key（调用方持有 g.mu）。
func (g *GoalRegistry) eventKeyLocked() int64 {
	return memory.NewSnowflakeEventKey(g.partitionID, 0)
}

// Resolve 更新 goal 状态。BindStore 后同步写 governance 事件（5.2，锁内时序见 Declare）。
func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool {
	g.mu.Lock()
	goal, ok := g.goals[id]
	if ok {
		goal.Status = status
	}
	evtKey := g.eventKeyLocked()
	now := time.Now().UnixMilli()
	pid := g.partitionID
	g.mu.Unlock()
	if ok {
		g.writeGoalEvent(goalEventPayload{GoalID: id, Op: "resolved", Status: string(status)}, evtKey, now, pid)
	}
	return ok
}

// List 返回全部 goal 的快照副本（5.1 goal_list 工具消费；按 ID 序不保证，
// 调用方按需排序）。返回副本防外部改动内部状态。
func (g *GoalRegistry) List() []*Goal {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Goal, 0, len(g.goals))
	for _, goal := range g.goals {
		cp := *goal
		out = append(out, &cp)
	}
	return out
}

// HasActive 报告是否存在未过期的 active goal（GovernanceGate 的 goal 检查判据）。
func (g *GoalRegistry) HasActive() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	for _, goal := range g.goals {
		if goal.Status == GoalActive && (goal.ExpiresMs == 0 || goal.ExpiresMs > now) {
			return true
		}
	}
	return false
}

// Active 返回全部 active goal（诊断/工具展示）。
func (g *GoalRegistry) Active() []*Goal {
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	out := make([]*Goal, 0)
	for _, goal := range g.goals {
		if goal.Status == GoalActive && (goal.ExpiresMs == 0 || goal.ExpiresMs > now) {
			out = append(out, goal)
		}
	}
	return out
}

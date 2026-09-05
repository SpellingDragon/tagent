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

// governance 事件的 subtype 值（Metadata["subtype"]）。
const (
	SubtypeDenial   = "denial"
	SubtypeGoal     = "goal"
	SubtypeApproval = "approval"
	SubtypeDegraded = "degraded"
	SubtypeAudit    = "audit"
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
	Timestamp  int64     `json:"ts"`
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

// Record 记一条治理记录（内存索引 + governance 事件）。写事件失败不阻断（记账尽力）。
func (l *DenialLedger) Record(rec DenialRecord) {
	if rec.Timestamp == 0 {
		rec.Timestamp = time.Now().UnixMilli()
	}
	l.mu.Lock()
	l.records = append(l.records, rec)
	l.mu.Unlock()

	if l.store != nil {
		l.writeGovernanceEvent(rec)
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

func (l *DenialLedger) writeGovernanceEvent(rec DenialRecord) {
	content := fmt.Sprintf("[governance:%s] tool=%s level=%s rule=%s reason=%s",
		rec.Subtype, rec.ToolName, rec.Level, rec.RuleID, rec.Reason)
	evt := memory.FullEvent{
		EventKey:     memory.NewSnowflakeEventKey(l.partitionID, 0),
		PartitionID:  l.partitionID,
		EventType:    event.TypeGovernance,
		EventSummary: content,
		Content:      content,
		Timestamp:    rec.Timestamp,
		Metadata: map[string]string{
			"subtype":     rec.Subtype,
			"tool":        rec.ToolName,
			"level":       rec.Level.String(),
			"rule_id":     rec.RuleID,
			"reason":      rec.Reason,
			"args_digest": rec.ArgsDigest,
			"goal_id":     rec.GoalID,
		},
	}
	// 尽力写入（治理账本失败不阻断主链路）。
	_ = l.store.StoreEvent(evt.EventKey, evt)
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
			Subtype: e.Metadata["subtype"], ToolName: e.Metadata["tool"],
			Level: parseRiskLevel(e.Metadata["level"]), RuleID: e.Metadata["rule_id"],
			Reason: e.Metadata["reason"], ArgsDigest: e.Metadata["args_digest"],
			GoalID: e.Metadata["goal_id"], Timestamp: e.Timestamp,
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
type GoalRegistry struct {
	mu    sync.RWMutex
	goals map[string]*Goal
	seq   int
}

// NewGoalRegistry 构建 goal 注册表。
func NewGoalRegistry() *GoalRegistry {
	return &GoalRegistry{goals: make(map[string]*Goal)}
}

// Declare 登记一个 goal，返回其 ID。
func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq++
	id := fmt.Sprintf("g-%d", g.seq)
	g.goals[id] = &Goal{
		ID: id, Statement: statement, CreatedBy: createdBy,
		Status: GoalActive, CreatedMs: time.Now().UnixMilli(), ExpiresMs: expiresMs,
	}
	return id
}

// Resolve 更新 goal 状态。
func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if goal, ok := g.goals[id]; ok {
		goal.Status = status
		return true
	}
	return false
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

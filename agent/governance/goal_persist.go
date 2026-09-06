package governance

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"

	event "github.com/SpellingDragon/tagent/event"
)

// ==================== GoalRegistry 事件持久化（5.2 design-report-closeout） ====================
//
// goal 声明是治理审计与 goal 门的一部分，重启不得丢失。模式对齐 DenialLedger：
// BindStore 延迟绑定（构造期 gate 先建、store 后就绪）+ 构造期单线程 rebuild +
// Declare/Resolve 双写（内存态 + governance 事件）。事件 subtype=goal，操作经
// Metadata["goal_op"]=declared/resolved 区分（不扩 event 包 subtype 枚举）。

// goalEventPayload 是 goal governance 事件 Content 的结构化 JSON。
type goalEventPayload struct {
	GoalID    string `json:"goal_id"`
	Op        string `json:"op"`                  // declared / resolved
	Statement string `json:"statement,omitempty"` // declared 时携带
	CreatedBy string `json:"created_by,omitempty"`
	Status    string `json:"status,omitempty"` // resolved 时携带
	ExpiresMs int64  `json:"expires_ms,omitempty"`
}

// BindStore 绑定持久化存储并回放重建（重启不丢 goal 声明）。nil 安全；
// 构造期单线程调用（对齐 DenialLedger.BindStore 纪律）。
func (g *GoalRegistry) BindStore(store memory.MemoryStore, partitionID int) {
	if g == nil || store == nil {
		return
	}
	g.mu.Lock()
	g.store = store
	g.partitionID = partitionID
	g.mu.Unlock()
	g.rebuildFromStore()
}

// writeGoalEvent 把 goal 操作写为 governance 事件（尽力：失败仅记日志，
// 内存态已生效——goal 门可用性优先，审计持久化尽力）。
func (g *GoalRegistry) writeGoalEvent(p goalEventPayload, evtKey int64, ts int64, pid int) {
	g.mu.RLock()
	store := g.store
	g.mu.RUnlock()
	if store == nil {
		return
	}
	content, err := json.Marshal(p)
	if err != nil {
		log.Warnf("[governance] goal event marshal failed: %v", err)
		return
	}
	summary := fmt.Sprintf("[governance:goal] %s %s", p.Op, p.GoalID)
	evt := memory.FullEvent{
		EventKey:     evtKey,
		PartitionID:  pid,
		EventType:    event.TypeGovernance,
		EventSummary: summary,
		Content:      string(content),
		Timestamp:    ts,
		Metadata: map[string]string{
			event.MetaKeySubtype: event.SubtypeGoal,
			"goal_op":            p.Op,
			"goal_id":            p.GoalID,
		},
	}
	if err := store.StoreEvent(evtKey, evt); err != nil {
		log.Warnf("[governance] goal event store failed (op=%s id=%s): %v", p.Op, p.GoalID, err)
	}
}

// rebuildFromStore 回放 governance goal 事件重建内存态（构造期单线程）。
// 按 Timestamp 升序回放 declared→resolved，seq 对齐最大已见编号防 ID 冲突。
func (g *GoalRegistry) rebuildFromStore() {
	g.mu.RLock()
	store, pid := g.store, g.partitionID
	g.mu.RUnlock()
	if store == nil {
		return
	}
	refs, err := store.QueryEvents(memory.QueryOptions{
		PartitionIDs: []int{pid},
		EventTypes:   []string{event.TypeGovernance},
		Limit:        10000,
		OrderBy:      "timestamp_asc",
	})
	if err != nil {
		log.Warnf("[governance] goal rebuild query failed: %v", err)
		return
	}
	// §8.11⑦：refs 级预筛（EventSummary 前缀）——Limit 截断作用于全部 governance 事件
	//（denial/degraded 占大头），不预筛会把 goal 事件挤出窗口。预筛后 GetEvents 只取 goal。
	keys := make([]int64, 0, len(refs))
	for _, r := range refs {
		if strings.Contains(r.EventSummary, "[governance:goal]") {
			keys = append(keys, r.EventKey)
		}
	}
	if len(keys) == 0 {
		return
	}
	events, err := store.GetEvents(keys)
	if err != nil {
		log.Warnf("[governance] goal rebuild fetch failed: %v", err)
		return
	}
	// 8.8（review §8）：不重排——QueryEvents 契约即 (Timestamp, EventKey) 全序
	//（声明式查询语义#3）；先前 sort.Slice 不稳定反而抹掉同毫秒 EventKey 兜底序。

	g.mu.Lock()
	defer g.mu.Unlock()
	maxSeq := g.seq
	for _, e := range events {
		if e.Metadata[event.MetaKeySubtype] != event.SubtypeGoal {
			continue
		}
		var p goalEventPayload
		if err := json.Unmarshal([]byte(e.Content), &p); err != nil || p.GoalID == "" {
			continue
		}
		switch p.Op {
		case "declared":
			g.goals[p.GoalID] = &Goal{
				ID: p.GoalID, Statement: p.Statement, CreatedBy: p.CreatedBy,
				Status: GoalActive, CreatedMs: e.Timestamp, ExpiresMs: p.ExpiresMs,
			}
		case "resolved":
			if goal, ok := g.goals[p.GoalID]; ok {
				goal.Status = GoalStatus(p.Status)
			}
		}
		// seq 对齐：防重建后新 Declare 生成重复 ID（g-N 形态解析 N）。
		var n int
		if _, err := fmt.Sscanf(p.GoalID, "g-%d", &n); err == nil && n > maxSeq {
			maxSeq = n
		}
	}
	g.seq = maxSeq
	if len(g.goals) > 0 {
		log.Infof("[governance] goal registry rebuilt from %d governance events", len(events))
	}
}

package memory

import (
	"encoding/json"
	"fmt"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// ==================== 回执-反馈绑定（D1 design-report-closeout） ====================
//
// FeedbackBinder 把「评价」（用户反馈/任务成败/API 评分）绑定到具体产出事件：
// feedback 事件（TypeFeedback，注册表单点声明）落在与产出同分区，经 RelationStore
// 因果边（SetParent(feedback → parent)）建立回溯链——零新索引，复用既有因果链。
// guardrail 的 negative_feedback_rate 判据沿因果边 join 到 parent 的 bundle_id 章。

// FeedbackPayload 是 feedback 事件 Content 的结构化 JSON（verdict/rating/note/source）。
type FeedbackPayload struct {
	Verdict   string  `json:"verdict"`            // positive / negative / neutral
	Rating    float64 `json:"rating,omitempty"`   // 可选数值评分
	Note      string  `json:"note,omitempty"`     // 可选说明
	Source    string  `json:"source"`             // user / task_settle / api
	ParentKey string  `json:"parent_key"`         // hex 形态的产出事件 key（人可读回溯）
	Timestamp int64   `json:"timestamp"`          // Unix 毫秒
}

// BindFeedback 写入一条绑定到 parentKey 的 feedback 事件并建立因果边。
// parent 必须已存在（否则显式错误——反馈不允许指向幻觉产出）；分区继承 parent。
// SetParent 经 RelationStoreProvider 可选面（store 未暴露关系存储时跳过因果边
// 并在返回错误中说明，事件本身仍写入——反馈优先落库，关系可后补）。
func BindFeedback(store MemoryStore, parentKey int64, payload FeedbackPayload) (int64, error) {
	if store == nil {
		return 0, fmt.Errorf("feedback: nil store")
	}
	parent, err := store.GetEvent(parentKey)
	if err != nil || parent == nil {
		return 0, fmt.Errorf("feedback: parent event %d not found: %v", parentKey, err)
	}
	payload.ParentKey = tagentevent.FormatEventKey(parentKey)
	if payload.Timestamp == 0 {
		payload.Timestamp = time.Now().UnixMilli()
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("feedback: marshal payload: %w", err)
	}

	pid := parent.PartitionID
	key := NewSnowflakeEventKey(pid, 0)
	evt := FullEvent{
		EventKey:     key,
		PartitionID:  pid,
		EventType:    tagentevent.TypeFeedback,
		EventSummary: fmt.Sprintf("feedback[%s]: %s → %s", payload.Source, payload.Verdict, payload.ParentKey),
		Timestamp:    payload.Timestamp,
		Content:      string(content),
		Metadata: map[string]string{
			tagentevent.MetaKeySubtype: payload.Source,
		},
	}
	if err := store.StoreEvent(key, evt); err != nil {
		return 0, fmt.Errorf("feedback: store event: %w", err)
	}
	// 因果边（尽力）：store 暴露 RelationStore 时建立 feedback → parent。
	if provider, ok := store.(RelationStoreProvider); ok {
		if rel := provider.RelationStore(); rel != nil {
			if err := rel.SetParent(key, parentKey); err != nil {
				// 事件已落库，因果边失败不回滚（反馈优先；关系可后补）。
				return key, fmt.Errorf("feedback stored (key=%d) but SetParent failed: %w", key, err)
			}
		}
	}
	return key, nil
}

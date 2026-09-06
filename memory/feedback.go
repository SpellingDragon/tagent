package memory

import (
	"encoding/json"
	"errors"
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
	Verdict   string  `json:"verdict"`          // positive / negative / neutral
	Rating    float64 `json:"rating,omitempty"` // 可选数值评分
	Note      string  `json:"note,omitempty"`   // 可选说明
	Source    string  `json:"source"`           // user / task_settle / api
	ParentKey string  `json:"parent_key"`       // hex 形态的产出事件 key（人可读回溯）
	Timestamp int64   `json:"timestamp"`        // Unix 毫秒
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
		// 8.5（review §8）：sentinel——parent-miss 与「已落库但因果边失败」必须可区分
		//（http 层据此 404 vs 201+warning，防客户端误重试造成重复 feedback）。
		return 0, fmt.Errorf("feedback: parent event %d not found: %w: %v", parentKey, ErrFeedbackParentNotFound, err)
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
	// 8.4（review §8）：继承 parent 的 bundle_id 章——guardrail 沿因果边精确 join 到
	// 产出该事件的 bundle 版本（否则无章 feedback 回退时间窗，跨 bundle 误归因可致误回滚）。
	metadata := map[string]string{
		tagentevent.MetaKeySubtype: payload.Source,
		// §8.11①：verdict 冗余入 Metadata——消费侧（guardrail）不再依赖 JSON 序列化
		// 格式的子串匹配（脆弱：字段序/转义变化即漏判）。
		"verdict": payload.Verdict,
	}
	if bid, ok := parent.Metadata[tagentevent.MetaKeyBundleID]; ok && bid != "" {
		metadata[tagentevent.MetaKeyBundleID] = bid
	}
	evt := FullEvent{
		EventKey:     key,
		PartitionID:  pid,
		EventType:    tagentevent.TypeFeedback,
		EventSummary: fmt.Sprintf("feedback[%s]: %s → %s", payload.Source, payload.Verdict, payload.ParentKey),
		Timestamp:    payload.Timestamp,
		Content:      string(content),
		Metadata:     metadata,
	}
	if err := store.StoreEvent(key, evt); err != nil {
		return 0, fmt.Errorf("feedback: store event: %w", err)
	}
	// 因果边（尽力）：store 暴露 RelationStore 时建立 feedback → parent。
	if provider, ok := store.(RelationStoreProvider); ok {
		if rel := provider.RelationStore(); rel != nil {
			if err := rel.SetParent(key, parentKey); err != nil {
				// 事件已落库，因果边失败不回滚（反馈优先；关系可后补）。
				// 8.5：sentinel 标注「部分成功」——调用方据此 201+warning 而非 404 重试。
				return key, fmt.Errorf("%w: feedback stored (key=%d) but SetParent failed: %v", ErrFeedbackEdgePartial, key, err)
			}
		}
	}
	return key, nil
}

// ErrFeedbackParentNotFound 标记 parent 不存在（8.5）：调用方应视为确定性失败
// （404），不得重试。
var ErrFeedbackParentNotFound = errors.New("feedback-parent-not-found")

// ErrFeedbackEdgePartial 标记「事件已落库但因果边失败」（8.5）：反馈本体成功，
// 调用方应返回成功+warning（201），不得按失败重试（会写重复 feedback）。
var ErrFeedbackEdgePartial = errors.New("feedback-edge-partial")

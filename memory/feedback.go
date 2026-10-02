package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// FeedbackPayload 是 feedback 事件 Content 的结构化 JSON（verdict/rating/note/source）。
type FeedbackPayload struct {
	Verdict   string  `json:"verdict"`
	Rating    float64 `json:"rating,omitempty"`
	Note      string  `json:"note,omitempty"`
	Source    string  `json:"source"`
	ParentKey string  `json:"parent_key"`
	Timestamp int64   `json:"timestamp"`
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
	metadata := map[string]string{
		tagentevent.MetaKeySubtype: payload.Source,
		"verdict":                  payload.Verdict,
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
	if provider, ok := store.(RelationStoreProvider); ok {
		if rel := provider.RelationStore(); rel != nil {
			if err := rel.SetParent(key, parentKey); err != nil {
				return key, fmt.Errorf("%w: feedback stored (key=%d) but SetParent failed: %v", ErrFeedbackEdgePartial, key, err)
			}
		}
	}
	return key, nil
}

// ErrFeedbackParentNotFound 标记 parent 不存在：调用方应视为确定性失败
// （404），不得重试。
var ErrFeedbackParentNotFound = errors.New("feedback-parent-not-found")

// ErrFeedbackEdgePartial 标记「事件已落库但因果边失败」：反馈本体成功，
// 调用方应返回成功+warning（201），不得按失败重试（会写重复 feedback）。
var ErrFeedbackEdgePartial = errors.New("feedback-edge-partial")

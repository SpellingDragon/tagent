package event

import (
	"encoding/json"
	"strconv"
	"strings"

	frameworkevent "trpc.group/trpc-go/trpc-agent-go/event"
)

// MetaKeyEventKey StateDelta 与 FullEvent.Metadata 的键常量：每个键在此定义一次，注入点引用常量，
// 消费方经 ParseEventMeta 解析。谁写谁读见文档的键归属表。
//
// 契约: docs/wiki/event/event-architecture.md#metadata-keys
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

	// MetaKeyCallID 调用关联键：把一条已提交事实对到产生它的那一次模型调用。
	// 键归属——写入方 = MemoryPlugin（经装配根注入的 CallIDResolver，按精确响应 ID
	// 命中才盖；未命中一律不盖，绝不取「最近一次调用」）；读取方 = 离线训练导出，
	// 从 FullEvent.Metadata（rawMetadata）按字面量取用。它不是 StateDelta 投递契约键，
	// 也不带 meta_ 透传前缀，因此事件解析面不消费它。
	// 契约: docs/wiki/event/event-architecture.md#metadata-keys
	MetaKeyCallID = "call_id"
)

// SourceSnapshot 冻结 durable 输入的原始来源与完整业务 Metadata，以 JSON 存于 MetaKeySourceSnapshot。
type SourceSnapshot struct {
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// EncodeSourceSnapshot 渲染持久化用的快照 JSON；来源与元数据皆空时返回空串。
func EncodeSourceSnapshot(source string, metadata map[string]any) (string, error) {
	if source == "" && len(metadata) == 0 {
		return "", nil
	}
	b, err := json.Marshal(SourceSnapshot{Source: source, Metadata: metadata})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeSourceSnapshot 解析快照 JSON；空串得到零值。
func DecodeSourceSnapshot(raw string) (SourceSnapshot, error) {
	if raw == "" {
		return SourceSnapshot{}, nil
	}
	var s SourceSnapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return SourceSnapshot{}, err
	}
	return s, nil
}

// FormatEventKey 以规范小写十六进制渲染 EventKey（负 key 保留前导 -）。
func FormatEventKey(key int64) string {
	if key < 0 {
		return "-" + strconv.FormatInt(-key, 16)
	}
	return strconv.FormatInt(key, 16)
}

// ParseEventKey 解析规范十六进制字符串，并容忍模型回显票据的常见形态：0x 前缀、
// evt_ 前缀、完整 [evt_HEX|type]、尾随 |type 或 ]。
func ParseEventKey(s string) (int64, error) {
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimPrefix(s, "evt_")
	if i := strings.IndexAny(s, "|]"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	v, err := strconv.ParseInt(s, 16, 64)
	if err != nil {
		return 0, err
	}
	if neg {
		v = -v
	}
	return v, nil
}

// EventMeta 是投递事件元数据的解析结果。
type EventMeta struct {
	EventKey      int64
	PartitionID   int
	EventType     string
	EventSummary  string
	TriggerSource string
	Meta          map[string]string
}

// ParseEventMeta 提取元数据契约；缺失字段留零值，Meta 恒非 nil。
func ParseEventMeta(evt *frameworkevent.Event) EventMeta {
	meta := EventMeta{Meta: map[string]string{}}
	if evt == nil || evt.StateDelta == nil {
		return meta
	}
	for k, v := range evt.StateDelta {
		switch k {
		case MetaKeyEventKey:
			if key, err := ParseEventKey(string(v)); err == nil {
				meta.EventKey = key
			}
		case MetaKeyPartitionID:
			if pid, err := strconv.Atoi(string(v)); err == nil {
				meta.PartitionID = pid
			}
		case MetaKeyEventType:
			meta.EventType = string(v)
		case MetaKeyEventSummary:
			meta.EventSummary = string(v)
		case MetaKeyTriggerSource:
			meta.TriggerSource = string(v)
		default:
			if strings.HasPrefix(k, MetaPrefix) && len(v) > 0 {
				meta.Meta[strings.TrimPrefix(k, MetaPrefix)] = string(v)
			}
		}
	}
	return meta
}

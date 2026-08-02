// Package-level event metadata contract (unified-event-projection D4).
//
// Injecting and parsing event metadata is a FRAMEWORK responsibility: every
// key is defined once here, injection points reference these constants, and
// consumers parse through ParseEventMeta instead of reading raw StateDelta
// strings.
package event

import (
	"strconv"
	"strings"

	frameworkevent "trpc.group/trpc-go/trpc-agent-go/event"
)

// StateDelta metadata keys. Injection points:
//   - storage identifiers (MetaKeyEventKey/PartitionID/EventType/EventSummary):
//     written by the event-plugin pipeline (MemoryPlugin) when an event is stored;
//   - MetaKeyTriggerSource: set once per invocation by RunFlow on every
//     forwarded event, for deterministic consumer dispatch;
//   - MetaPrefix* passthrough metadata (e.g. chat routing): propagated onto
//     delivered events from the invocation's root metadata.
const (
	MetaKeyEventKey      = "event_key"
	MetaKeyPartitionID   = "partition_id"
	MetaKeyEventType     = "event_type"
	MetaKeyEventSummary  = "event_summary"
	MetaKeyTriggerSource = "trigger_source"

	// MetaPrefix marks passthrough metadata keys (meta_chat_id, meta_user_name, …).
	MetaPrefix = "meta_"
)

// FormatEventKey renders an EventKey in its CANONICAL string form: lowercase
// hexadecimal (negative summary-reference keys keep a leading '-'). This is
// the single string representation used everywhere a key crosses a text
// boundary — the [evt_KEY|type] timeline prefix, compaction key lists,
// StateDelta metadata, and recall tool I/O. Hex keeps the 19-digit decimal
// form down to ≤16 chars (token-cheaper, and visually an opaque identifier).
func FormatEventKey(key int64) string {
	if key < 0 {
		return "-" + strconv.FormatInt(-key, 16)
	}
	return strconv.FormatInt(key, 16)
}

// ParseEventKey parses the canonical hex string form back into an EventKey.
// Tolerates the forms a model is likely to echo back as a recall key:
// optional 0x/0X prefix, the timeline-rendered "evt_" prefix (every
// [evt_HEX|type] timeline message shows the key this way), the bracketed
// "[evt_HEX|type]" form, and a trailing "|type" or "]".
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

// EventMeta is the parsed metadata of a delivered event.
type EventMeta struct {
	EventKey      int64
	PartitionID   int
	EventType     string
	EventSummary  string
	TriggerSource string
	// Meta holds passthrough metadata with the "meta_" prefix stripped
	// (e.g. "chat_id" → value of StateDelta["meta_chat_id"]).
	Meta map[string]string
}

// ParseEventMeta extracts the metadata contract from a framework event.
// Missing fields yield zero values; Meta is always non-nil.
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

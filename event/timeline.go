package event

import (
	"fmt"
	"strings"
)

// FormatEventPrefix 渲染时间线行的规范前缀 `[evt_<KEY>|<type>] `，KEY 用十六进制。
// 写入端与本文件的读取端 ParseEventKeyAndType 同处一包，渲染与解析不会各自演化。
//
// 契约: docs/wiki/event/event-architecture.md#timeline-prefix
func FormatEventPrefix(key int64, eventType string) string {
	return fmt.Sprintf("[evt_%s|%s]", FormatEventKey(key), eventType)
}

// HasEventPrefix reports whether content starts with a timeline prefix.
func HasEventPrefix(content string) bool {
	return strings.HasPrefix(content, "[evt_")
}

// ParseEventKeyAndType extracts EventKey and EventType from a message content
// with "[evt_<KEY>|<type>] <remainder>" prefix.
// Returns (0, "unknown", content) if no valid prefix is found.
func ParseEventKeyAndType(content string) (key int64, eventType string, remainder string) {
	const prefix = "[evt_"
	if !strings.HasPrefix(content, prefix) {
		return 0, "unknown", content
	}
	closePos := strings.IndexByte(content, ']')
	if closePos < 0 {
		return 0, "unknown", content
	}
	inner := content[len(prefix):closePos]
	barPos := strings.IndexByte(inner, '|')
	if barPos < 0 {
		return 0, "unknown", content
	}
	keyStr := inner[:barPos]
	eventType = inner[barPos+1:]
	k, err := ParseEventKey(keyStr)
	if err != nil {
		return 0, "unknown", content
	}
	remainder = strings.TrimSpace(content[closePos+1:])
	return k, eventType, remainder
}

// StripEventKeyPrefix removes a leading [evt_KEY|type] prefix from content.
// Returns the original content if no prefix is found.
func StripEventKeyPrefix(content string) string {
	_, _, remainder := ParseEventKeyAndType(content)
	return remainder
}

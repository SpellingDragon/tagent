package event

import (
	"fmt"
	"strings"
)

// Timeline prefix contract: every rendered history line carries a
// "[evt_<KEY>|<type>] " prefix (KEY in canonical hex). The WRITE side
// (FormatEventPrefix) and READ side (ParseEventKeyAndType) live together in
// this package — the contract has exactly one home, so producers (timeline
// rendering) and consumers (compression, retained-ref scanning, tests) can
// never drift apart.

// FormatEventPrefix renders the canonical timeline prefix for an event.
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
	// Find the closing bracket
	closePos := strings.IndexByte(content, ']')
	if closePos < 0 {
		return 0, "unknown", content
	}
	// Content between "[evt_" and "]" is "KEY|type"
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
	// Remainder is everything after "] "
	remainder = strings.TrimSpace(content[closePos+1:])
	return k, eventType, remainder
}

// StripEventKeyPrefix removes a leading [evt_KEY|type] prefix from content.
// Returns the original content if no prefix is found.
func StripEventKeyPrefix(content string) string {
	_, _, remainder := ParseEventKeyAndType(content)
	return remainder
}

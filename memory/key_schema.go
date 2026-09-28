package memory

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// keySep 是键各段之间的分隔符。四类键形与"压实段复用同一键形、层位记在段元数据"的
	// 约定见文档。
	keySep = ":"

	// keyPrefixEvt Key type prefixes
	keyPrefixEvt  = "evt"
	keyPrefixIdx  = "idx"
	keyPrefixMeta = "meta"
	keyPrefixTomb = "tomb"

	// DefaultWindowSize Default window size: 1 hour in seconds
	DefaultWindowSize int64 = 3600
)

// WindowTimestamp computes the time window start (epoch seconds) for a given timestamp.
// Aligns timestamp to window boundaries: floor(timestamp / windowSize) * windowSize.
func WindowTimestamp(tsSec int64, windowSize int64) int64 {
	if windowSize <= 0 {
		windowSize = DefaultWindowSize
	}
	return (tsSec / windowSize) * windowSize
}

// WindowTimestampFromEventKey computes the window timestamp from an EventKey's embedded timestamp.
func WindowTimestampFromEventKey(eventKey int64, windowSize int64) int64 {
	tsSec := TimestampFromEventKey(eventKey)
	return WindowTimestamp(tsSec, windowSize)
}

// EventKeyStr builds the RocksDB key for storing event content.
// Format: {pid}:evt:{window_ts}:{seq}
func EventKeyStr(pid int, windowTS int64, seq int) string {
	return fmt.Sprintf("%d%s%s%s%d%s%d", pid, keySep, keyPrefixEvt, keySep, windowTS, keySep, seq)
}

// IndexKeyStr builds the RocksDB key for the segment offset index.
// Format: {pid}:idx:{event_key}
func IndexKeyStr(pid int, eventKey int64) string {
	return fmt.Sprintf("%d%s%s%s%d", pid, keySep, keyPrefixIdx, keySep, eventKey)
}

// MetaKeyStr builds the RocksDB key for segment metadata.
// Format: {pid}:meta:{window_ts}
func MetaKeyStr(pid int, windowTS int64) string {
	return fmt.Sprintf("%d%s%s%s%d", pid, keySep, keyPrefixMeta, keySep, windowTS)
}

// TombstoneKeyStr builds the RocksDB key for a tombstone marker.
// Format: {pid}:tomb:{event_key}
func TombstoneKeyStr(pid int, eventKey int64) string {
	return fmt.Sprintf("%d%s%s%s%d", pid, keySep, keyPrefixTomb, keySep, eventKey)
}

// ParsedKey contains the components extracted from a KV key.
type ParsedKey struct {
	PartitionID int
	KeyType     string
	WindowTS    int64
	Seq         int
	EventKey    int64
}

// ParseKey parses a KV key string into its components.
// Returns error if the key format is invalid.
func ParseKey(key string) (*ParsedKey, error) {
	parts := strings.Split(key, keySep)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid key format: %s", key)
	}

	pid, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid partition ID in key %s: %w", key, err)
	}

	pk := &ParsedKey{
		PartitionID: pid,
		KeyType:     parts[1],
	}

	switch pk.KeyType {
	case keyPrefixEvt:
		if len(parts) < 4 {
			return nil, fmt.Errorf("invalid event key format: %s", key)
		}
		pk.WindowTS, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid window timestamp in key %s: %w", key, err)
		}
		pk.Seq, err = strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("invalid sequence in key %s: %w", key, err)
		}

	case keyPrefixIdx:
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid index key format: %s", key)
		}
		pk.EventKey, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid event key in index key %s: %w", key, err)
		}

	case keyPrefixMeta:
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid meta key format: %s", key)
		}
		pk.WindowTS, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid window timestamp in meta key %s: %w", key, err)
		}

	case keyPrefixTomb:
		if len(parts) < 3 {
			return nil, fmt.Errorf("invalid tombstone key format: %s", key)
		}
		pk.EventKey, err = strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid event key in tombstone key %s: %w", key, err)
		}

	default:
		return nil, fmt.Errorf("unknown key type: %s in key %s", pk.KeyType, key)
	}

	return pk, nil
}

// PartitionPrefix returns the prefix for all keys in a partition.
func PartitionPrefix(pid int) string {
	return fmt.Sprintf("%d%s", pid, keySep)
}

// MetaPrefix returns the prefix for all meta keys in a partition.
// Scanning this prefix returns all segment metadata entries.
func MetaPrefix(pid int) string {
	return fmt.Sprintf("%d%s%s%s", pid, keySep, keyPrefixMeta, keySep)
}

// EventPrefix returns the prefix for all event keys in a partition.
func EventPrefix(pid int) string {
	return fmt.Sprintf("%d%s%s%s", pid, keySep, keyPrefixEvt, keySep)
}

// SegmentEventPrefix returns the prefix for events within a specific time window.
// Scanning this prefix returns all events in the segment.
func SegmentEventPrefix(pid int, windowTS int64) string {
	return fmt.Sprintf("%d%s%s%s%d%s", pid, keySep, keyPrefixEvt, keySep, windowTS, keySep)
}

// TombstonePrefix returns the prefix for all tombstone keys in a partition.
func TombstonePrefix(pid int) string {
	return fmt.Sprintf("%d%s%s%s", pid, keySep, keyPrefixTomb, keySep)
}

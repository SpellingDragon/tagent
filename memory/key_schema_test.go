package memory

import (
	"testing"
)

func TestWindowTimestamp(t *testing.T) {
	tests := []struct {
		tsSec      int64
		windowSize int64
		expected   int64
	}{
		{0, 3600, 0},
		{3599, 3600, 0},
		{3600, 3600, 3600},
		{7199, 3600, 3600},
		{1710678000, 3600, 1710676800}, // 2024-03-17 07:00:00 UTC
		{1710681599, 3600, 1710680400}, // 1 second before window boundary
		{1710681600, 3600, 1710680400}, // at window boundary
	}
	for _, tt := range tests {
		got := WindowTimestamp(tt.tsSec, tt.windowSize)
		if got != tt.expected {
			t.Errorf("WindowTimestamp(%d, %d) = %d, want %d", tt.tsSec, tt.windowSize, got, tt.expected)
		}
	}
}

func TestWindowTimestampDefaultWindow(t *testing.T) {
	got := WindowTimestamp(3600, 0)
	if got != 3600 {
		t.Errorf("WindowTimestamp(3600, 0) = %d, want 3600 (default)", got)
	}
}

func TestKeyBuilders(t *testing.T) {
	pid := 42
	windowTS := int64(1710676800)
	seq := 0
	eventKey := int64(1777198738547555000)

	evtKey := EventKeyStr(pid, windowTS, seq)
	expected := "42:evt:1710676800:0"
	if evtKey != expected {
		t.Errorf("EventKeyStr = %s, want %s", evtKey, expected)
	}

	idxKey := IndexKeyStr(pid, eventKey)
	expected = "42:idx:1777198738547555000"
	if idxKey != expected {
		t.Errorf("IndexKeyStr = %s, want %s", idxKey, expected)
	}

	metaKey := MetaKeyStr(pid, windowTS)
	expected = "42:meta:1710676800"
	if metaKey != expected {
		t.Errorf("MetaKeyStr = %s, want %s", metaKey, expected)
	}

	tombKey := TombstoneKeyStr(pid, eventKey)
	expected = "42:tomb:1777198738547555000"
	if tombKey != expected {
		t.Errorf("TombstoneKeyStr = %s, want %s", tombKey, expected)
	}
}

func TestParseKey_EventKey(t *testing.T) {
	pk, err := ParseKey("42:evt:1710676800:0")
	if err != nil {
		t.Fatalf("ParseKey failed: %v", err)
	}
	if pk.PartitionID != 42 {
		t.Errorf("PartitionID = %d, want 42", pk.PartitionID)
	}
	if pk.KeyType != "evt" {
		t.Errorf("KeyType = %s, want evt", pk.KeyType)
	}
	if pk.WindowTS != 1710676800 {
		t.Errorf("WindowTS = %d, want 1710676800", pk.WindowTS)
	}
	if pk.Seq != 0 {
		t.Errorf("Seq = %d, want 0", pk.Seq)
	}
}

func TestParseKey_IndexKey(t *testing.T) {
	pk, err := ParseKey("42:idx:1777198738547555000")
	if err != nil {
		t.Fatalf("ParseKey failed: %v", err)
	}
	if pk.PartitionID != 42 {
		t.Errorf("PartitionID = %d, want 42", pk.PartitionID)
	}
	if pk.KeyType != "idx" {
		t.Errorf("KeyType = %s, want idx", pk.KeyType)
	}
	if pk.EventKey != 1777198738547555000 {
		t.Errorf("EventKey = %d, want 1777198738547555000", pk.EventKey)
	}
}

func TestParseKey_MetaKey(t *testing.T) {
	pk, err := ParseKey("42:meta:1710676800")
	if err != nil {
		t.Fatalf("ParseKey failed: %v", err)
	}
	if pk.PartitionID != 42 {
		t.Errorf("PartitionID = %d, want 42", pk.PartitionID)
	}
	if pk.KeyType != "meta" {
		t.Errorf("KeyType = %s, want meta", pk.KeyType)
	}
	if pk.WindowTS != 1710676800 {
		t.Errorf("WindowTS = %d, want 1710676800", pk.WindowTS)
	}
}

func TestParseKey_TombstoneKey(t *testing.T) {
	pk, err := ParseKey("42:tomb:1777198738547555000")
	if err != nil {
		t.Fatalf("ParseKey failed: %v", err)
	}
	if pk.PartitionID != 42 {
		t.Errorf("PartitionID = %d, want 42", pk.PartitionID)
	}
	if pk.KeyType != "tomb" {
		t.Errorf("KeyType = %s, want tomb", pk.KeyType)
	}
	if pk.EventKey != 1777198738547555000 {
		t.Errorf("EventKey = %d, want 1777198738547555000", pk.EventKey)
	}
}

func TestParseKey_Invalid(t *testing.T) {
	invalidKeys := []string{
		"",
		"42",
		"42:unknown:123",
		"notanumber:evt:123:0",
	}
	for _, key := range invalidKeys {
		_, err := ParseKey(key)
		if err == nil {
			t.Errorf("Expected error for invalid key: %s", key)
		}
	}
}

func TestPrefixFunctions(t *testing.T) {
	pid := 42

	pp := PartitionPrefix(pid)
	if pp != "42:" {
		t.Errorf("PartitionPrefix = %s, want 42:", pp)
	}

	mp := MetaPrefix(pid)
	if mp != "42:meta:" {
		t.Errorf("MetaPrefix = %s, want 42:meta:", mp)
	}

	ep := EventPrefix(pid)
	if ep != "42:evt:" {
		t.Errorf("EventPrefix = %s, want 42:evt:", ep)
	}

	sep := SegmentEventPrefix(pid, 1710676800)
	if sep != "42:evt:1710676800:" {
		t.Errorf("SegmentEventPrefix = %s, want 42:evt:1710676800:", sep)
	}

	tp := TombstonePrefix(pid)
	if tp != "42:tomb:" {
		t.Errorf("TombstonePrefix = %s, want 42:tomb:", tp)
	}
}

func TestWindowTimestampFromEventKey(t *testing.T) {
	// Generate a Snowflake key with specific timestamp
	pid := PartitionIDFromName("test-window")
	nowMs := int64(1710678000000) // 2024-03-17 07:00:00 UTC
	eventKey := NewSnowflakeEventKey(pid, nowMs)

	windowTS := WindowTimestampFromEventKey(eventKey, 3600)
	expected := WindowTimestamp(1710678000, 3600)
	if windowTS != expected {
		t.Errorf("WindowTimestampFromEventKey = %d, want %d", windowTS, expected)
	}
}

func TestNewSnowflakeEventKey_Uniqueness(t *testing.T) {
	pid := PartitionIDFromName("test-uniqueness")
	nowMs := int64(1710678000000)
	seen := make(map[int64]struct{})
	// 22-bit sequence supports ~4M keys per second; test well beyond old 10-bit limit.
	for i := 0; i < 5000; i++ {
		key := NewSnowflakeEventKey(pid, nowMs)
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate event key generated at index %d: %d", i, key)
		}
		seen[key] = struct{}{}
	}
}

func TestNewSnowflakeEventKey_SequenceExhaustion_WaitsForNextSecond(t *testing.T) {
	pid := PartitionIDFromName("test-exhaustion")
	nowMs := int64(1710678000000)
	// Reset internal counters so we start at sequence 0 for this partition.
	snowflakeSeqMu.Lock()
	delete(snowflakeSeqCnt, pid)
	delete(snowflakeSeqLast, pid)
	snowflakeSeqMu.Unlock()

	// Fill sequence to just below max, then force next key to exhaust.
	snowflakeSeqMu.Lock()
	snowflakeSeqCnt[pid] = sequenceMax - 1
	snowflakeSeqLast[pid] = nowMs/1000 - snowflakeEpoch
	snowflakeSeqMu.Unlock()

	// Next key should succeed without collision (it may wait for next second).
	key1 := NewSnowflakeEventKey(pid, nowMs)
	key2 := NewSnowflakeEventKey(pid, nowMs)
	if key1 == key2 {
		t.Fatalf("expected different keys after sequence exhaustion, got %d", key1)
	}
}

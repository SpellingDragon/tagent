package memory

import (
	"math"
	"math/rand"
	"strings"
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
		{1710678000, 3600, 1710676800},
		{1710681599, 3600, 1710680400},
		{1710681600, 3600, 1710680400},
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
	pid := PartitionIDFromName("test-window")
	nowMs := int64(1710678000000)
	eventKey := NewSnowflakeEventKey(pid, nowMs)

	windowTS := WindowTimestampFromEventKey(eventKey, 3600)
	expected := WindowTimestamp(1710678000, 3600)
	if windowTS != expected {
		t.Errorf("WindowTimestampFromEventKey = %d, want %d", windowTS, expected)
	}
}

// TestSnowflakeEventKey_AlwaysPositive 钉住 locks the sign-bit invariant: real
//
// 契约: docs/wiki/memory/memory-architecture.md#event-key
func TestSnowflakeEventKey_AlwaysPositive(t *testing.T) {
	partitions := []int{0, 1, 143, 786, partitionIDMask}
	for _, pid := range partitions {
		key := NewSnowflakeEventKey(pid, 0)
		if key <= 0 {
			t.Errorf("partition %d produced non-positive key %d (sign bit must stay clear)", pid, key)
		}
		if got := PartitionIDFromEventKey(key); got != pid {
			t.Errorf("partition round-trip: got %d, want %d", got, pid)
		}
	}
	if pid := PartitionIDFromName("plan"); pid > partitionIDMask {
		t.Errorf("PartitionIDFromName must stay within mask, got %d", pid)
	} else if key := NewSnowflakeEventKey(pid, 0); key <= 0 {
		t.Errorf("FNV(plan) partition %d produced non-positive key %d", pid, key)
	}
}

// checkParseKeyNoPanic asserts ParseKey is total: any string yields either an
// error or a non-nil result, never a panic and never a silent half-parse.
func checkParseKeyNoPanic(t testing.TB, key string) {
	t.Helper()
	pk, err := ParseKey(key)
	if err != nil {
		if pk != nil {
			t.Fatalf("ParseKey(%q) returned both a non-nil result and an error: %+v / %v", key, pk, err)
		}
		return
	}
	if pk == nil {
		t.Fatalf("ParseKey(%q) returned (nil, nil)", key)
	}
	switch pk.KeyType {
	case keyPrefixEvt, keyPrefixIdx, keyPrefixMeta, keyPrefixTomb:
	default:
		t.Fatalf("ParseKey(%q) succeeded with unknown key type %q", key, pk.KeyType)
	}
}

// TestKeySchema_BoundedFuzz 钉住 drives many random strings plus structured
//
// 契约: docs/wiki/memory/memory-architecture.md#event-key
func TestKeySchema_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0x7e601d))

	for i := 0; i < 20000; i++ {
		checkParseKeyNoPanic(t, randomKeyString(rng))
	}

	seeds := []string{
		"", ":", ":::", "1:evt:", "1:evt:3600", "1:evt:abc:0", "1:evt:3600:xyz",
		"1:idx:notanumber", "1:meta:", "-9:x:1:2", "1:unknown:2", "99999999999999999999:evt:1:1",
		"1:evt:9223372036854775807:0", "1:tomb:-1", "0x1:evt:1:1", "1:evt: 3600:1",
	}
	for _, s := range seeds {
		checkParseKeyNoPanic(t, s)
	}

	for i := 0; i < 20000; i++ {
		pid := int(rng.Int63n(1<<20)) - (1 << 19)
		wts := rng.Int63()
		seq := int(rng.Int63n(1 << 16))
		ek := int64(rng.Uint64())

		if pk, err := ParseKey(EventKeyStr(pid, wts, seq)); err != nil {
			t.Fatalf("evt round-trip rejected %d/%d/%d: %v", pid, wts, seq, err)
		} else if pk.PartitionID != pid || pk.WindowTS != wts || pk.Seq != seq || pk.KeyType != keyPrefixEvt {
			t.Fatalf("evt round-trip mismatch: got %+v want pid=%d wts=%d seq=%d", pk, pid, wts, seq)
		}
		if pk, err := ParseKey(IndexKeyStr(pid, ek)); err != nil {
			t.Fatalf("idx round-trip rejected %d/%d: %v", pid, ek, err)
		} else if pk.PartitionID != pid || pk.EventKey != ek || pk.KeyType != keyPrefixIdx {
			t.Fatalf("idx round-trip mismatch: got %+v want pid=%d ek=%d", pk, pid, ek)
		}
		if pk, err := ParseKey(MetaKeyStr(pid, wts)); err != nil {
			t.Fatalf("meta round-trip rejected %d/%d: %v", pid, wts, err)
		} else if pk.PartitionID != pid || pk.WindowTS != wts || pk.KeyType != keyPrefixMeta {
			t.Fatalf("meta round-trip mismatch: got %+v", pk)
		}
		if pk, err := ParseKey(TombstoneKeyStr(pid, ek)); err != nil {
			t.Fatalf("tomb round-trip rejected %d/%d: %v", pid, ek, err)
		} else if pk.PartitionID != pid || pk.EventKey != ek || pk.KeyType != keyPrefixTomb {
			t.Fatalf("tomb round-trip mismatch: got %+v", pk)
		}
	}
}

// randomKeyString returns a string assembled from characters that actually
// appear in the key grammar plus their neighbors — pure-random bytes rarely
// reach the numeric-parse branches, but these biased tokens do.
func randomKeyString(rng *rand.Rand) string {
	tokens := []string{":", "evt", "idx", "meta", "tomb", "-1", "0", "1", "3600", "9",
		"abc", "x", " ", "99999999999999999999", "9223372036854775808", "\n", "0x1f"}
	var b strings.Builder
	n := 1 + rng.Intn(6)
	for i := 0; i < n; i++ {
		b.WriteString(tokens[rng.Intn(len(tokens))])
	}
	return b.String()
}

// TestSnowflake_BoundedFuzz 钉住 verifies the EventKey codec's exact round-trip
//
// 契约: docs/wiki/memory/memory-architecture.md#event-key
func TestSnowflake_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5f1ee7))
	for i := 0; i < 50000; i++ {
		pid := int(rng.Int63n(4096))
		tsSec := snowflakeEpoch + rng.Int63n(1<<30)
		nowMs := tsSec * 1000

		key := NewSnowflakeEventKey(pid, nowMs)
		if got := PartitionIDFromEventKey(key); got != pid&partitionIDMask {
			t.Fatalf("partition round-trip: pid=%d key=%d got=%d want=%d", pid, key, got, pid&partitionIDMask)
		}
		if got := TimestampFromEventKey(key); got != tsSec {
			t.Fatalf("timestamp round-trip: tsSec=%d key=%d got=%d", tsSec, key, got)
		}
		if got, want := WindowTimestampFromEventKey(key, DefaultWindowSize), WindowTimestamp(tsSec, DefaultWindowSize); got != want {
			t.Fatalf("window mismatch for tsSec=%d: got %d want %d", tsSec, got, want)
		}
	}
	for _, k := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64} {
		_ = PartitionIDFromEventKey(k)
		_ = TimestampFromEventKey(k)
		_ = SequenceFromEventKey(k)
		_ = WindowTimestampFromEventKey(k, 0)
	}
}

// FuzzParseKey is the native fuzz target for extended digging.
func FuzzParseKey(f *testing.F) {
	for _, s := range []string{"1:evt:3600:0", "2:idx:12345", "0:meta:3600", "7:tomb:99", "bad", "1:evt:x:1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, key string) {
		checkParseKeyNoPanic(t, key)
	})
}

// FuzzSnowflakeDecode is the native fuzz target: decoding any int64 key must
// return without panicking (the render-freeze scans reconstruct fields from
// arbitrary, possibly-corrupt stored keys).
func FuzzSnowflakeDecode(f *testing.F) {
	f.Add(int64(0), int64(3600))
	f.Add(int64(math.MaxInt64), int64(1))
	f.Add(int64(math.MinInt64), int64(0))
	f.Fuzz(func(t *testing.T, key, window int64) {
		_ = PartitionIDFromEventKey(key)
		_ = TimestampFromEventKey(key)
		_ = SequenceFromEventKey(key)
		_ = WindowTimestampFromEventKey(key, window)
	})
}

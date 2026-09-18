package memory

import (
	"math"
	"math/rand"
	"strings"
	"testing"
)

// Bounded fuzz + error injection for the KV key schema and the Snowflake
// EventKey codec (resident-remaining-hardening 3.6, archived 7.6).
//
// Two surfaces are load-bearing invariants the rest of the store trusts:
//
//   - ParseKey NEVER panics on arbitrary bytes (a malformed RocksDB key read
//     back from disk must surface as an error, not a crash during a scan), and
//   - the Snowflake encode/decode round-trip is exact, because the compression
//     render-freeze and causal-chain parent lookup both reconstruct
//     partition/timestamp from a raw int64 key.
//
// The deterministic bounded loop (fixed seed) runs on every `go test`, so the
// normal suite — not only an explicit `go test -fuzz` — carries the signal.
// FuzzParseKey / FuzzSnowflakeRoundTrip are the native targets for extended
// CI-local digging:
//
//	go test ./memory/ -run FuzzParseKey -fuzz FuzzParseKey -fuzztime=30s
//
// A crasher is auto-persisted to testdata/fuzz/FuzzParseKey/<hash> and
// replayed by the plain `go test` seed run afterwards.

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
	// A successful parse must land on one of the four known key types.
	switch pk.KeyType {
	case keyPrefixEvt, keyPrefixIdx, keyPrefixMeta, keyPrefixTomb:
	default:
		t.Fatalf("ParseKey(%q) succeeded with unknown key type %q", key, pk.KeyType)
	}
}

// TestKeySchema_BoundedFuzz drives many random strings plus structured
// near-miss keys through ParseKey: no panics, and builder-produced keys always
// round-trip back to their exact components.
func TestKeySchema_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0x7e601d))

	// 1. Fully random garbage — the never-panic property is the point.
	for i := 0; i < 20000; i++ {
		checkParseKeyNoPanic(t, randomKeyString(rng))
	}

	// 2. Structured near-misses: well-shaped prefix, poisoned tail. These are
	//    the inputs most likely to trip a partial-parse-with-nil-error bug.
	seeds := []string{
		"", ":", ":::", "1:evt:", "1:evt:3600", "1:evt:abc:0", "1:evt:3600:xyz",
		"1:idx:notanumber", "1:meta:", "-9:x:1:2", "1:unknown:2", "99999999999999999999:evt:1:1",
		"1:evt:9223372036854775807:0", "1:tomb:-1", "0x1:evt:1:1", "1:evt: 3600:1",
	}
	for _, s := range seeds {
		checkParseKeyNoPanic(t, s)
	}

	// 3. Exact round-trip for every builder type over a wide value domain.
	for i := 0; i < 20000; i++ {
		pid := int(rng.Int63n(1<<20)) - (1 << 19) // signed partition ints, incl. negatives
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

// TestSnowflake_BoundedFuzz verifies the EventKey codec's exact round-trip:
// partition (masked to its field width) and whole-second timestamp always
// survive encode/decode, and no int64 decode ever panics.
func TestSnowflake_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0x5f1ee7))
	for i := 0; i < 50000; i++ {
		pid := int(rng.Int63n(4096)) // deliberately overshoot the mask to test masking
		tsSec := snowflakeEpoch + rng.Int63n(1<<30)
		nowMs := tsSec * 1000

		key := NewSnowflakeEventKey(pid, nowMs)
		if got := PartitionIDFromEventKey(key); got != pid&partitionIDMask {
			t.Fatalf("partition round-trip: pid=%d key=%d got=%d want=%d", pid, key, got, pid&partitionIDMask)
		}
		if got := TimestampFromEventKey(key); got != tsSec {
			t.Fatalf("timestamp round-trip: tsSec=%d key=%d got=%d", tsSec, key, got)
		}
		// Window derivation must agree with the raw second's window.
		if got, want := WindowTimestampFromEventKey(key, DefaultWindowSize), WindowTimestamp(tsSec, DefaultWindowSize); got != want {
			t.Fatalf("window mismatch for tsSec=%d: got %d want %d", tsSec, got, want)
		}
	}
	// Zero / extremes must decode without panicking and yield a valid window.
	for _, k := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64} {
		_ = PartitionIDFromEventKey(k)
		_ = TimestampFromEventKey(k)
		_ = SequenceFromEventKey(k)
		_ = WindowTimestampFromEventKey(k, 0) // windowSize<=0 → default, no div-by-zero
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
		// Must not panic or divide by zero for any window (<=0 → default).
		_ = WindowTimestampFromEventKey(key, window)
	})
}

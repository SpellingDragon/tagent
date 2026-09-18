package event

import (
	"fmt"
	"math"
	"math/rand"
	"strings"
	"testing"
)

// Bounded fuzz + error injection for the recall-key parser
// (resident-remaining-hardening 3.6, archived 7.6).
//
// ParseEventKey is the ONE place a model-echoed recall key is turned back into
// an int64. It is deliberately tolerant (accepts 0x / evt_ / [..|type] forms),
// which makes two properties load-bearing:
//
//   - FormatEventKey(k) → ParseEventKey == k for every k except math.MinInt64
//     (whose negation overflows the canonical "-…" rendering), and
//   - it never panics on arbitrary bytes — a garbage recall argument must
//     return an error, not crash the tool call.
//
// Deterministic bounded loop runs on every `go test`. Native target for digging:
//
//	go test ./event/ -run FuzzParseEventKey -fuzz FuzzParseEventKey -fuzztime=30s

// checkParseEventKeyNoPanic: ParseEventKey is total (value or error, no panic).
func checkParseEventKeyNoPanic(t testing.TB, s string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ParseEventKey(%q) panicked: %v", s, r)
		}
	}()
	_, _ = ParseEventKey(s)
}

func TestEventKey_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xfeedf00d))

	// 1. Random garbage / adversarial shapes — never panic, never a bogus ok.
	seeds := []string{
		"", " ", "0x", "evt_", "[evt_]", "[]", "-0x", "0x0", "-9223372036854775808",
		"zzz", "12:g4", "[evt_1f|agent_output]", "0XABCDEF", "  1f  ", "- - 1f",
		strings.Repeat("f", 40), "7fffffffffffffffffff", "evt_-1a", "[evt_-2b|x]",
	}
	for i := 0; i < 20000; i++ {
		seeds = append(seeds, randomKeyString(rng))
	}
	for _, s := range seeds {
		checkParseEventKeyNoPanic(t, s)
	}

	// 2. Exact round-trip over the full int64 domain (minus MinInt64), via every
	//    tolerant spelling the parser claims to accept.
	for i := 0; i < 50000; i++ {
		k := int64(rng.Uint64())
		if k == math.MinInt64 {
			continue
		}
		canonical := FormatEventKey(k)
		got, err := ParseEventKey(canonical)
		if err != nil {
			t.Fatalf("ParseEventKey(%q) rejected canonical form of %d: %v", canonical, k, err)
		}
		if got != k {
			t.Fatalf("round-trip: k=%d canonical=%q got=%d", k, canonical, got)
		}
		// Every accepted wrapper must recover the same key.
		for _, variant := range tolerantForms(k, canonical) {
			got, err := ParseEventKey(variant)
			if err != nil {
				t.Fatalf("ParseEventKey(%q) [variant of %d] rejected: %v", variant, k, err)
			}
			if got != k {
				t.Fatalf("variant round-trip: k=%d variant=%q got=%d", k, variant, got)
			}
		}
	}

	// 3. Error injection: a forged uppercase / non-hex bracket must NOT silently
	//    parse as a key (it is a model fabrication, not a real recall token).
	if _, err := ParseEventKey("[AAAA0004G]"); err == nil {
		t.Fatalf("non-hex bracketed text parsed without error (should reject)")
	}
}

// tolerantForms returns the wrapped spellings ParseEventKey is documented to
// accept, all encoding key k.
func tolerantForms(k int64, canonical string) []string {
	if k < 0 {
		// canonical already carries the leading '-', so 0x/evt_ prefixes are
		// only meaningful for the magnitude; keep to the sign-preserving forms.
		return []string{fmt.Sprintf("evt_%s", canonical), fmt.Sprintf("[evt_%s|agent_output]", canonical)}
	}
	return []string{
		"0x" + canonical,
		"0X" + strings.ToUpper(canonical),
		"evt_" + canonical,
		fmt.Sprintf("[evt_%s|external_input]", canonical),
		canonical + "|agent_output",
	}
}

func randomKeyString(rng *rand.Rand) string {
	tokens := []string{"0x", "0X", "evt_", "[", "]", "|", "-", " ", "f", "a", "1", "9",
		"g", "z", "9223372036854775808", "agent_output", "\n", strings.Repeat("0", 30)}
	var b strings.Builder
	n := 1 + rng.Intn(7)
	for i := 0; i < n; i++ {
		b.WriteString(tokens[rng.Intn(len(tokens))])
	}
	return b.String()
}

// FuzzParseEventKey is the native fuzz target for extended digging.
func FuzzParseEventKey(f *testing.F) {
	for _, s := range []string{"1f", "-2a", "0x1f", "evt_1f", "[evt_1f|agent_output]", "bad", "9223372036854775808"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		checkParseEventKeyNoPanic(t, s)
	})
}

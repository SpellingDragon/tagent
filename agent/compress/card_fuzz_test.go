package compress

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// Bounded fuzz + error injection for the compression projection parsers
// (resident-remaining-hardening 3.6, archived 7.6).
//
// guardCondensedCard is the machine ticket guard (design D6): it is the ONLY
// thing standing between a summary-model condensation and the compaction
// payload. Its load-bearing contract is ANTI-FABRICATION — it must never
// accept condensed text carrying a recall ticket that was not present in the
// folded input, because a forged [key] would poison every subsequent
// memory_recall. parseCardTickets / parseCardSection / fitTicketCard back it
// and must be panic-total.
//
// Deterministic bounded loop runs on every `go test`. Native targets:
//
//	go test ./agent/compress/ -run FuzzGuardCondensedCard -fuzz FuzzGuardCondensedCard -fuzztime=30s

var ticketChars = regexp.MustCompile(`^-?[0-9a-f]+$`)

func TestParseCardTickets_BoundedFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xca4dca4d))
	for i := 0; i < 40000; i++ {
		line := randomCardLine(rng)
		for _, k := range parseCardTickets(line) {
			// Every extracted ticket must be canonical-hex and appear verbatim
			// bracketed in the source line.
			if !ticketChars.MatchString(k) {
				t.Fatalf("parseCardTickets(%q) returned non-canonical ticket %q", line, k)
			}
			if !strings.Contains(line, "["+k+"]") {
				t.Fatalf("parseCardTickets(%q) returned ticket %q not present bracketed in input", line, k)
			}
		}
		// Panic-totality of the sibling parsers on the same garbage.
		_, _ = parseCardSection(line)
		_ = parseNarrativeSection(line)
		_, _ = fitTicketCard(line, rng.Intn(64))
	}
}

// TestGuardCondensedCard_AcceptImpliesNoForgery is the core anti-fabrication
// property: whatever the guard ACCEPTS, its tickets must be a subset of the
// input's tickets. Enforced across random input/condensed pairings.
func TestGuardCondensedCard_AcceptImpliesNoForgery(t *testing.T) {
	rng := rand.New(rand.NewSource(0x9a0d))
	for i := 0; i < 40000; i++ {
		input := randomCardLines(rng)
		condensed := randomCardLine(rng)

		inSet := map[string]bool{}
		for _, l := range input {
			for _, k := range parseCardTickets(l) {
				inSet[k] = true
			}
		}
		verdict := guardCondensedCard(condensed, input)
		if verdict != "" {
			continue // rejected: fine, the guard is allowed to reject anything.
		}
		if len(inSet) == 0 {
			continue // "nothing to protect": acceptance without tickets is by design.
		}
		for _, k := range parseCardTickets(condensed) {
			if !inSet[k] {
				t.Fatalf("guard ACCEPTED condensed %q (input %v) containing forged ticket %q not in input",
					condensed, input, k)
			}
		}
	}
}

// TestGuardCondensedCard_ForgeryRejected is the deterministic error-injection
// counter-example: a condensed line that keeps the required head/tail tickets
// but injects one unknown ticket MUST be rejected. If this ever passes the
// guard, the anti-fabrication property is broken.
func TestGuardCondensedCard_ForgeryRejected(t *testing.T) {
	input := []string{
		"- [10] first item",
		"- [20] middle item",
		"- [30] tail item",
	}
	// Head [10] and tail [30] survive; [ff] is fabricated.
	forged := "- [10] [ff] [30] condensed"
	if verdict := guardCondensedCard(forged, input); verdict == "" {
		t.Fatalf("guard accepted a condensed line with forged ticket [ff]; verdict=%q", verdict)
	}
	// The legitimate condensation (subset of input) must be accepted.
	ok := "- [10] [30] condensed"
	if verdict := guardCondensedCard(ok, input); verdict != "" {
		t.Fatalf("guard rejected a legitimate condensation %q: %s", ok, verdict)
	}
}

func randomCardLine(rng *rand.Rand) string {
	toks := []string{"- ", "★ ", "[", "]", "1", "a", "f", "0", "-", " ", "z", "[1f]", "[ff]",
		"[evt_ab|x]", "〔预算截断〕", "\n", "(earlier 3 items retrievable via memory_recall)",
		"〔历史综述〕", "[AAAA0004]", "[10]"}
	var b strings.Builder
	n := 1 + rng.Intn(8)
	for i := 0; i < n; i++ {
		b.WriteString(toks[rng.Intn(len(toks))])
	}
	return b.String()
}

func randomCardLines(rng *rand.Rand) []string {
	n := 1 + rng.Intn(4)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, randomCardLine(rng))
	}
	return out
}

// FuzzParseCardTickets is the native target: parseCardTickets must never panic
// and must never return a ticket absent from the input.
func FuzzParseCardTickets(f *testing.F) {
	f.Add("- [10] [ff] item ★")
	f.Add("[AAAA0004] uppercase not a ticket")
	f.Add("")
	f.Fuzz(func(t *testing.T, line string) {
		for _, k := range parseCardTickets(line) {
			if !ticketChars.MatchString(k) || !strings.Contains(line, "["+k+"]") {
				t.Fatalf("bad ticket %q extracted from %q", k, line)
			}
		}
	})
}

// FuzzGuardCondensedCard is the native target for the anti-fabrication
// contract. Seed corpus is a valid pairing; the fuzzer mutates both halves.
func FuzzGuardCondensedCard(f *testing.F) {
	f.Add("- [10] [30] condensed", "- [10] a\n- [20] b\n- [30] c")
	f.Add("- [ff] forged", "- [10] a\n- [30] c")
	f.Fuzz(func(t *testing.T, condensed string, input string) {
		lines := strings.Split(input, "\n")
		inSet := map[string]bool{}
		for _, l := range lines {
			for _, k := range parseCardTickets(l) {
				inSet[k] = true
			}
		}
		if guardCondensedCard(condensed, lines) == "" && len(inSet) > 0 {
			for _, k := range parseCardTickets(condensed) {
				if !inSet[k] {
					t.Fatalf("accepted condensed %q forged ticket %q (input %q)", condensed, k, input)
				}
			}
		}
	})
}

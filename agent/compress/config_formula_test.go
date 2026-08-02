package compress

import (
	"testing"

	"github.com/SpellingDragon/tagent/memory"
)

// TestConfigFormula_Defaults (rolling-summary-anchor D3): when card_max_chars /
// compact_keys_listed are not explicitly set, they derive from the primary
// knob max_tokens (M): card_max_chars = M/20, compact_keys_listed = card/200.
func TestConfigFormula_Defaults(t *testing.T) {
	sc := NewSmartCompressor()
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 128000, 0.8, 2)
	if cc.cardMaxChars != 6400 { // 128000/20
		t.Errorf("cardMaxChars default = %d, want 6400 (M/20)", cc.cardMaxChars)
	}
	if cc.listedKeysCap != 32 { // 6400/200
		t.Errorf("listedKeysCap default = %d, want 32 (cardMaxChars/200)", cc.listedKeysCap)
	}
}

// TestConfigFormula_ExplicitWins: explicit settings override the formula.
func TestConfigFormula_ExplicitWins(t *testing.T) {
	sc := NewSmartCompressor()
	cc := NewContextCompressor(sc, memory.NewInMemoryStore(), NewDefaultTokenCounter(), 128000, 0.8, 2,
		WithCardMaxChars(6000))
	if cc.cardMaxChars != 6000 {
		t.Errorf("explicit cardMaxChars = %d, want 6000", cc.cardMaxChars)
	}
	// listedKeysCap still derives from the effective cardMaxChars.
	if cc.listedKeysCap != 30 { // 6000/200
		t.Errorf("listedKeysCap = %d, want 30 (6000/200)", cc.listedKeysCap)
	}
}

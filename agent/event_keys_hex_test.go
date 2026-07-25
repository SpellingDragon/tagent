package agent

import (
	"testing"

	tagentevent "github.com/SpellingDragon/tagent/event"
)

// TestToInt64Key_HexContract: event_keys arguments are canonically HEX
// strings (the [evt_...] timeline form). Regression guard for the hex-
// migration gap where decimal-only parsing silently dropped every key the
// model copied from its context.
func TestToInt64Key_HexContract(t *testing.T) {
	k := int64(0x1201a3f4b5c6d)
	hex := tagentevent.FormatEventKey(k)

	if got := toInt64Key(hex); got != k {
		t.Errorf("canonical hex %q must parse to %d, got %d", hex, k, got)
	}
	if got := toInt64Key("evt_" + hex); got != k {
		t.Errorf("evt_-prefixed hex must parse, got %d", got)
	}
	if got := toInt64Key("12345"); got != 0x12345 {
		// Ambiguous digits-only strings resolve as hex (canonical form wins);
		// this documents the tradeoff explicitly.
		t.Errorf("digit-only string resolves as hex first, got %d", got)
	}
	if got := toInt64Key(float64(99)); got != 99 {
		t.Errorf("numeric fallback must survive, got %d", got)
	}
	if got := toInt64Key("not-a-key"); got != 0 {
		t.Errorf("garbage must yield 0, got %d", got)
	}
}

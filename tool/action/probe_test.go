package action

import (
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// C2: probe failure latch — first failure emits once, repeats stay silent,
// a success resets the latch so the next failure emits again.
func TestEmitProbeResult_Latch(t *testing.T) {
	d := NewTmuxSettleDetector("p-test", nil, time.Hour)
	defer d.close()

	d.EmitProbeResult(false, "connection refused")
	select {
	case sig := <-d.Settled():
		if sig.Kind != task.SettleWatch {
			t.Fatalf("kind = %q, want watch", sig.Kind)
		}
		if !strings.Contains(sig.Output, "probe FAILED") {
			t.Fatalf("output %q should carry probe failure", sig.Output)
		}
	default:
		t.Fatal("expected failure signal")
	}

	d.EmitProbeResult(false, "still down") // latch: no second signal
	select {
	case sig := <-d.Settled():
		t.Fatalf("latched failure must not re-emit, got %+v", sig)
	default:
	}

	d.EmitProbeResult(true, "") // success resets
	d.EmitProbeResult(false, "down again")
	select {
	case sig := <-d.Settled():
		if !strings.Contains(sig.Output, "down again") {
			t.Fatalf("post-reset failure should emit with new detail, got %q", sig.Output)
		}
	default:
		t.Fatal("expected signal after success-reset then failure")
	}
}

// C2: success-only history never emits.
func TestEmitProbeResult_SuccessSilent(t *testing.T) {
	d := NewTmuxSettleDetector("p-ok", nil, time.Hour)
	defer d.close()
	for i := 0; i < 5; i++ {
		d.EmitProbeResult(true, "")
	}
	select {
	case sig := <-d.Settled():
		t.Fatalf("healthy probes must not emit, got %+v", sig)
	default:
	}
}

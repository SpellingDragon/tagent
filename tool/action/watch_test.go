package action

import (
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// C1: watch pattern wiring on the detector — hit → SettleWatch signal with
// cumulative count; merge window folds rapid hits; no-watch = no signals.
func TestDetector_Watch_EmitsSettleWatch(t *testing.T) {
	d := NewTmuxSettleDetector("w-test", nil, time.Hour)
	defer d.close()
	if err := d.SetWatch(`ERROR|panic`, 5*time.Second); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	d.OnWatchOutput("boot ok\n")
	select {
	case sig := <-d.Settled():
		t.Fatalf("no signal expected before any match, got %+v", sig)
	default:
	}
	d.OnWatchOutput("doing work\nERROR: disk full\npanic: runtime\n")
	select {
	case sig := <-d.Settled():
		if sig.Kind != task.SettleWatch {
			t.Fatalf("kind = %q, want watch", sig.Kind)
		}
		if !strings.Contains(sig.Output, "x2") {
			t.Fatalf("output %q should report x2 hits", sig.Output)
		}
	default:
		t.Fatal("expected a watch signal after matching output")
	}
}

// C1: merge window — hits within the window fold into one pending signal
// (count grows on the NEXT emit), preventing wake-up storms on log floods.
func TestDetector_Watch_MergeWindow(t *testing.T) {
	d := NewTmuxSettleDetector("w-merge", nil, time.Hour)
	defer d.close()
	if err := d.SetWatch("ERR", 10*time.Second); err != nil {
		t.Fatalf("SetWatch: %v", err)
	}
	d.OnWatchOutput("ERR one\n") // first hit → signal
	select {
	case sig := <-d.Settled():
		if !strings.Contains(sig.Output, "x1") {
			t.Fatalf("first signal should be x1, got %q", sig.Output)
		}
	default:
		t.Fatal("expected first signal")
	}
	d.OnWatchOutput("ERR two\nERR three\n") // inside window → folded
	select {
	case sig := <-d.Settled():
		t.Fatalf("merged hit must not emit immediately, got %+v", sig)
	default:
	}
	d.OnWatchOutput("more output\n") // no hits, still inside window
	select {
	case sig := <-d.Settled():
		t.Fatalf("no-hit refresh must not emit, got %+v", sig)
	default:
	}
	// After the window passes, the NEXT hit emits with cumulative count.
	time.Sleep(20 * time.Millisecond)
	d.watchMu.Lock()
	d.watchLast = time.Now().Add(-15 * time.Second) // push last emit beyond the 10s window
	d.watchMu.Unlock()
	d.OnWatchOutput("ERR four\n")
	select {
	case sig := <-d.Settled():
		// Snapshot-diff semantics: each refresh counts matches in the WHOLE
		// pane buffer, so cumulative grows by the per-refresh DELTA:
		// 1 (first) + 1 ("two/three" pane had 2 vs seen 1) + 1 ("four") = 3.
		if !strings.Contains(sig.Output, "x1") || !strings.Contains(sig.Err.Error(), "3") {
			t.Fatalf("expected x1 (cumulative 3), got %q / %v", sig.Output, sig.Err)
		}
	default:
		t.Fatal("expected post-window signal")
	}
}

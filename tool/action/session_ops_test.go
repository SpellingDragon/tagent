package action

// B3 session-operations contract tests (2026-09-11 async-action overhaul).
// All tests are hermetic: peek uses temp pipe files, send/stop validation
// paths avoid real tmux interaction.

import (
	"os"
	"strings"
	"testing"
)

func newOpTestTool(t *testing.T) (*ActionTool, string) {
	t.Helper()
	dir := t.TempDir()
	ct := &ActionTool{
		tmuxMonitor:  newQuietTestMonitor(&mockInspector{processExists: true}),
		tmuxExecutor: &TmuxExecutor{workspace: dir},
		workspace:    dir,
	}
	return ct, dir
}

func TestOpPeek_IncrementalCursor(t *testing.T) {
	ct, _ := newOpTestTool(t)
	target := "peek-cursor-test"
	pf := ct.tmuxExecutor.PipeFileFor(target)
	t.Cleanup(func() { os.Remove(pf) })

	// Session produces output in two bursts.
	os.WriteFile(pf, []byte("line1\nline2\n"), 0o600)

	r1, err := ct.opPeek(&ActionArgs{}, target)
	if err != nil {
		t.Fatalf("peek1: %v", err)
	}
	out1 := r1.(map[string]any)
	if out1["output"] != "line1\nline2" {
		t.Errorf("peek1 output = %q, want full log", out1["output"])
	}

	// New output arrives.
	f, _ := os.OpenFile(pf, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("line3\n")
	f.Close()

	r2, err := ct.opPeek(&ActionArgs{}, target)
	if err != nil {
		t.Fatalf("peek2: %v", err)
	}
	out2 := r2.(map[string]any)
	if out2["output"] != "line3" {
		t.Errorf("peek2 output = %q, want only new line3", out2["output"])
	}
	if n := out2["bytes_new"].(int); n != len("line3\n") {
		t.Errorf("peek2 bytes_new = %d, want %d", n, len("line3\n"))
	}

	// No new output → empty.
	r3, _ := ct.opPeek(&ActionArgs{}, target)
	if out3 := r3.(map[string]any); out3["output"] != "" {
		t.Errorf("peek3 output = %q, want empty", out3["output"])
	}
}

func TestOpPeek_TailCapsLines(t *testing.T) {
	ct, _ := newOpTestTool(t)
	target := "peek-tail-test"
	pf := ct.tmuxExecutor.PipeFileFor(target)
	t.Cleanup(func() { os.Remove(pf) })
	os.WriteFile(pf, []byte("a\nb\nc\nd\n"), 0o600)

	r, err := ct.opPeek(&ActionArgs{Tail: 2}, target)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	out := r.(map[string]any)
	if out["output"] != "c\nd" {
		t.Errorf("tail output = %q, want last 2 lines", out["output"])
	}
	if out["truncated"] != true {
		t.Errorf("truncated flag = %v, want true", out["truncated"])
	}
}

func TestOpPeek_AnsiStripped(t *testing.T) {
	ct, _ := newOpTestTool(t)
	target := "peek-ansi-test"
	pf := ct.tmuxExecutor.PipeFileFor(target)
	t.Cleanup(func() { os.Remove(pf) })
	os.WriteFile(pf, []byte("\x1b[31mred\x1b[0m plain\n"), 0o600)

	r, _ := ct.opPeek(&ActionArgs{}, target)
	out := r.(map[string]any)["output"].(string)
	if strings.Contains(out, "\x1b") {
		t.Errorf("ANSI escape survived default strip: %q", out)
	}
	if !strings.Contains(out, "red") {
		t.Errorf("text content lost in strip: %q", out)
	}
}

func TestOpPeek_MissingLogGraceful(t *testing.T) {
	ct, _ := newOpTestTool(t)
	r, err := ct.opPeek(&ActionArgs{}, "no-such-session-xyz")
	if err != nil {
		t.Fatalf("missing log should degrade gracefully, got error: %v", err)
	}
	if got := r.(map[string]any)["status"]; got != "no_output_log" {
		t.Errorf("status = %v, want no_output_log", got)
	}
}

func TestOpSend_Validation(t *testing.T) {
	ct, _ := newOpTestTool(t)

	// Missing keys.
	if _, err := ct.opSend(&ActionArgs{}, "s"); err == nil {
		t.Error("op=send without keys accepted")
	}
	// Nonexistent session (real SessionExists against tmux; if a server is
	// present this session id will not exist anyway).
	if _, err := ct.opSend(&ActionArgs{Keys: "hi"}, "no-such-session-xyz"); err == nil {
		t.Error("op=send to nonexistent session accepted")
	}
}

func TestOpStop_AlreadyGone(t *testing.T) {
	ct, _ := newOpTestTool(t)
	if _, err := ct.opStop(&ActionArgs{}, "no-such-session-xyz"); err != nil {
		t.Errorf("op=stop on dead session should be idempotent success, got %v", err)
	}
}

func TestResolveTarget_PrefersSessionID(t *testing.T) {
	ct, _ := newOpTestTool(t)
	got, err := ct.resolveTarget(&ActionArgs{SessionID: "tagent-123", Name: "dev"})
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if got != "tagent-123" {
		t.Errorf("resolveTarget = %q, want explicit session_id to win", got)
	}
	if _, err := ct.resolveTarget(&ActionArgs{}); err == nil {
		t.Error("resolveTarget without id/name accepted")
	}
}

func TestStripANSI_Basic(t *testing.T) {
	in := "\x1b[2J\x1b[Hhello\x1b[0m world"
	if got := stripANSI(in); got != "hello world" {
		t.Errorf("stripANSI = %q", got)
	}
}

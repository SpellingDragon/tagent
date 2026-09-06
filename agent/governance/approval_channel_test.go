package governance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestApprovalRespondFile (3.1/3.2, design-report-closeout): RespondFile is
// the shared human-response primitive (CLI + message channel). It matches by
// digest prefix, flips pending→approved/denied with DecidedBy, and is
// IDEMPOTENT — a repeated response never rewrites or double-counts.
func TestApprovalRespondFile(t *testing.T) {
	dir := t.TempDir()
	// A pending request file as written by ApprovalManager.Request.
	req := ApprovalRequest{
		ID: "appr-1", ToolName: "exec", ArgsDigest: "abcdef1234567890",
		Status: ApprovalPending, CreatedMs: time.Now().UnixMilli(),
		ExpiresMs: time.Now().Add(time.Hour).UnixMilli(),
	}
	raw, _ := json.Marshal(req)
	path := filepath.Join(dir, "appr-1.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	// Short digest prefix (>=8) matches.
	msg, err := RespondFile(dir, "abcdef12", true, "cli")
	if err != nil {
		t.Fatalf("respond: %v", err)
	}
	if !strings.Contains(msg, "approved") {
		t.Fatalf("unexpected msg: %s", msg)
	}
	var got ApprovalRequest
	raw2, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw2, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != ApprovalApproved || got.DecidedBy != "cli" {
		t.Fatalf("file not updated: %+v", got)
	}

	// Idempotent: second response does not rewrite (status stays, note returned).
	msg2, err := RespondFile(dir, "abcdef12", false, "cli")
	if err != nil {
		t.Fatalf("second respond: %v", err)
	}
	if !strings.Contains(msg2, "幂等") {
		t.Fatalf("expected idempotent note, got %s", msg2)
	}
	raw3, _ := os.ReadFile(path)
	var got2 ApprovalRequest
	_ = json.Unmarshal(raw3, &got2)
	if got2.Status != ApprovalApproved {
		t.Fatalf("idempotency violated: status flipped to %s", got2.Status)
	}

	// Unknown digest → explicit error.
	if _, err := RespondFile(dir, "ffffffff", true, "cli"); err == nil {
		t.Fatal("unknown digest must error")
	}
	// Too-short digest → explicit error.
	if _, err := RespondFile(dir, "abc", true, "cli"); err == nil {
		t.Fatal("short digest must error")
	}
}

// TestParseApprovalReply (3.3): the message-channel reply parser — approve/
// reject (and zh verbs) with a >=8-char digest; anything else is NOT an
// approval reply (ok=false, caller handles as normal message).
func TestParseApprovalReply(t *testing.T) {
	cases := []struct {
		text    string
		ok      bool
		approve bool
		digest  string
	}{
		{"approve abcdef12", true, true, "abcdef12"},
		{"REJECT abcdef1234 xx", true, false, "abcdef1234"},
		{"批准 abcdef12", true, true, "abcdef12"},
		{"拒绝 abcdef12", true, false, "abcdef12"},
		{"hello world", false, false, ""},
		{"approve short", false, false, ""}, // digest < 8
		{"approve", false, false, ""},
	}
	for _, tc := range cases {
		digest, approve, ok := ParseApprovalReply(tc.text)
		if ok != tc.ok || (ok && (approve != tc.approve || digest != tc.digest)) {
			t.Fatalf("%q: got (%q,%v,%v) want (%q,%v,%v)", tc.text, digest, approve, ok, tc.digest, tc.approve, tc.ok)
		}
	}
}

// TestApprovalChannelFailureDoesNotBlock (3.1): a failing Deliver never
// blocks Request — the pending file is on disk regardless (gate-not-wall).
func TestApprovalChannelFailureDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	a := NewApprovalManager(dir, time.Hour)
	a.AddChannel(failingChannel{})
	req, err := a.Request("exec", `{"command":"rm -rf /x"}`, "rm -rf /x", "critical", "exec.destructive", "destructive", "")
	if err != nil {
		t.Fatalf("Request must succeed despite channel failure: %v", err)
	}
	if req == nil || req.Status != ApprovalPending {
		t.Fatalf("bad request: %+v", req)
	}
	// Pending file on disk (CLI/file approval path intact).
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("pending file missing — file approval path broken")
	}
}

type failingChannel struct{}

func (failingChannel) Deliver(*ApprovalRequest) error { return os.ErrPermission }

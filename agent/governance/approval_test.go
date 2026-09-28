// 本文件负责批准通道：一请求一文件、外部裁决的重扫可见性与节流、通道故障不阻塞主链路、
// 过期文件清理，以及"超期未裁决按未获批准处理"。
// 契约: docs/wiki/agent/governance-enforcement.md#approval-channel
package governance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestApprovalRespondFile 钉住 RespondFile is。
func TestApprovalRespondFile(t *testing.T) {
	dir := t.TempDir()
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

	if _, err := RespondFile(dir, "ffffffff", true, "cli"); err == nil {
		t.Fatal("unknown digest must error")
	}
	if _, err := RespondFile(dir, "abc", true, "cli"); err == nil {
		t.Fatal("short digest must error")
	}
}

// TestParseApprovalReply 钉住 (3.3): the message-channel reply parser — approve/。
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
		{"approve short", false, false, ""},
		{"approve", false, false, ""},
	}
	for _, tc := range cases {
		digest, approve, ok := ParseApprovalReply(tc.text)
		if ok != tc.ok || (ok && (approve != tc.approve || digest != tc.digest)) {
			t.Fatalf("%q: got (%q,%v,%v) want (%q,%v,%v)", tc.text, digest, approve, ok, tc.digest, tc.approve, tc.ok)
		}
	}
}

// TestApprovalChannelFailureDoesNotBlock 钉住 (3.1): a failing Deliver never。
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
	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("pending file missing — file approval path broken")
	}
}

type failingChannel struct{}

func (failingChannel) Deliver(*ApprovalRequest) error { return os.ErrPermission }

// TestApproval_RescanSeesRuntimeApproval 钉住 W2回归：外部审批者在运行中落盘批准文件后，。
func TestApproval_RescanSeesRuntimeApproval(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)

	req, err := am.Request("exec", `{"cmd":"rm -rf /tmp/x"}`, "rm -rf /tmp/x", "critical", "exec.destructive", "破坏性命令", "")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	digest := req.ArgsDigest

	req.Status = ApprovalApproved
	req.DecidedBy = "human-ops"
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approvals", req.ID+".json"), raw, 0o644); err != nil {
		t.Fatalf("write approval file: %v", err)
	}

	got := am.Check("exec", digest)
	if got == nil {
		t.Fatal("W2: 运行中外部落盘批准后 Check 应经重扫命中（此前恒 nil → critical 恒 Hold）")
	}
	if got.Status != ApprovalApproved || got.DecidedBy != "human-ops" {
		t.Fatalf("应命中外部批准(status=approved,by=human-ops), got status=%s by=%s", got.Status, got.DecidedBy)
	}
	if am.Check("exec", "wrong-digest") != nil {
		t.Fatal("错误 digest 不应命中（精确绑定防批准后换参）")
	}
}

// TestGate_ApprovalAccessorExposed 钉住 W2 回归：Gate 暴露 Approval() 供消息/CLI 审批通道调。
func TestGate_ApprovalAccessorExposed(t *testing.T) {
	dir := t.TempDir()
	g := NewGovernanceGate(GateDeps{
		Classifier: NewRiskClassifier(DefaultRules(), RiskMedium),
		Approval:   NewApprovalManager(dir, 30*time.Minute),
		Config:     GateConfig{Enabled: true},
	})
	am := g.Approval()
	if am == nil {
		t.Fatal("W2: Gate.Approval() 应暴露审批管理器（供微信/CLI 通道调 Decide）")
	}
	req, _ := am.Request("exec", `{"cmd":"sudo x"}`, "sudo x", "critical", "exec.sudo", "提权", "")
	if err := am.Decide(req.ID, ApprovalApproved, "wechat-user"); err != nil {
		t.Fatalf("Decide 经暴露访问器应可用: %v", err)
	}
	if got := am.Check("exec", req.ArgsDigest); got == nil || got.Status != ApprovalApproved {
		t.Fatalf("Decide 后 Check 应命中 approved, got %+v", got)
	}
	// nil Gate 安全。
	var nilGate *GovernanceGate
	if nilGate.Approval() != nil {
		t.Fatal("nil Gate.Approval() 应返回 nil")
	}
}

// TestApproval_RescanThrottled 钉住 W2 Minor⑧+ ⑦补缺：Check 未命中的重扫受。
func TestApproval_RescanThrottled(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)
	base := time.Now()
	cur := base
	am.now = func() time.Time { return cur }
	am.rescanInterval = time.Second

	req, _ := am.Request("exec", `{"cmd":"x"}`, "x", "critical", "exec.sudo", "提权", "")
	_ = am.Check("exec", req.ArgsDigest)
	req.Status = ApprovalApproved
	raw, _ := json.MarshalIndent(req, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "approvals", req.ID+".json"), raw, 0o644)
	cur = base.Add(500 * time.Millisecond)
	if got := am.Check("exec", req.ArgsDigest); got != nil {
		t.Fatal("W2 节流: 窗内第二次 Check 不应重扫目录(应仍 nil,防高频重试反复 IO)")
	}
}

// TestApproval_RescanWindowExpiryResumes 钉住 ⑦正向补缺：节流窗过期后 Check 恢复重扫，。
func TestApproval_RescanWindowExpiryResumes(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)
	base := time.Now()
	cur := base
	am.now = func() time.Time { return cur }
	am.rescanInterval = time.Second

	req, _ := am.Request("exec", `{"cmd":"y"}`, "y", "critical", "exec.sudo", "提权", "")
	_ = am.Check("exec", req.ArgsDigest)
	req.Status = ApprovalApproved
	raw, _ := json.MarshalIndent(req, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "approvals", req.ID+".json"), raw, 0o644)
	cur = base.Add(500 * time.Millisecond)
	if got := am.Check("exec", req.ArgsDigest); got != nil {
		t.Fatal("窗内不应重扫(节流生效)")
	}
	cur = base.Add(1500 * time.Millisecond)
	got := am.Check("exec", req.ArgsDigest)
	if got == nil || got.Status != ApprovalApproved {
		t.Fatalf("W2 节流窗过期后应恢复重扫并命中 approved, got %+v", got)
	}
}

// TestApproval_ExpiredFileCleanup 钉住 Minor④回归：rebuild 清理过期审批文件（防 approvals。
func TestApproval_ExpiredFileCleanup(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)
	expired := &ApprovalRequest{
		ID: "appr-expired", ToolName: "exec", ArgsDigest: "d1",
		Status: ApprovalApproved, CreatedMs: time.Now().Add(-2 * time.Hour).UnixMilli(),
		ExpiresMs: time.Now().Add(-1 * time.Hour).UnixMilli(),
	}
	raw, _ := json.MarshalIndent(expired, "", "  ")
	apprDir := filepath.Join(dir, "approvals")
	if err := os.WriteFile(filepath.Join(apprDir, expired.ID+".json"), raw, 0o644); err != nil {
		t.Fatalf("write expired: %v", err)
	}
	am.rebuild()
	if _, err := os.Stat(filepath.Join(apprDir, expired.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("Minor④: rebuild 应删除过期审批文件(防无界堆积)")
	}
	if am.Check("exec", "d1") != nil {
		t.Fatal("过期请求不应命中(已清理出索引)")
	}
}

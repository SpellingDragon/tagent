package governance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestApproval_RescanSeesRuntimeApproval 是 W2（§8.3）回归：外部审批者在运行中落盘批准文件后，
// Check 节流重扫目录即命中放行。此前 Check 只读构造时索引 → 运行中外部批准永不可见 →
// critical 恒 Hold、重试持续堆积 pending（治理审批闭环断路）。
func TestApproval_RescanSeesRuntimeApproval(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)

	// 登记一个 pending 批准请求（critical 操作）。
	req, err := am.Request("exec", `{"cmd":"rm -rf /tmp/x"}`, "rm -rf /tmp/x", "critical", "exec.destructive", "破坏性命令", "")
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	digest := req.ArgsDigest

	// 模拟外部审批者（人工 digest 文件通道）在运行中把该请求改写为 approved 并落盘。
	// （内存 index 仍是 Request 时的 pending；外部只改文件、不触碰 index——这正是运行中
	// 批准的典型形态：进程外的运维/微信通道写文件。）
	req.Status = ApprovalApproved
	req.DecidedBy = "human-ops"
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approvals", req.ID+".json"), raw, 0o644); err != nil {
		t.Fatalf("write approval file: %v", err)
	}

	// 首次 Check：checkIndex(index 仍 pending)未命中 → rescanDue(lastRescan=0 → true) →
	// rebuild 读入 approved 覆盖 index → 命中放行。这正是 W2 修复：运行中外部落盘批准经
	// 节流重扫可见（此前只读构造时索引 → 恒 nil → critical 恒 Hold）。
	got := am.Check("exec", digest)
	if got == nil {
		t.Fatal("W2: 运行中外部落盘批准后 Check 应经重扫命中（此前恒 nil → critical 恒 Hold）")
	}
	if got.Status != ApprovalApproved || got.DecidedBy != "human-ops" {
		t.Fatalf("应命中外部批准(status=approved,by=human-ops), got status=%s by=%s", got.Status, got.DecidedBy)
	}
	// 精确 digest 匹配：错误 digest 不命中（防「批准后换参数」）。
	if am.Check("exec", "wrong-digest") != nil {
		t.Fatal("错误 digest 不应命中（精确绑定防批准后换参）")
	}
}

// TestGate_ApprovalAccessorExposed 是 W2 回归：Gate 暴露 Approval() 供消息/CLI 审批通道调
// Decide（此前不暴露 → Decide 全仓无调用方）。
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
	// 经暴露的访问器 Decide（模拟微信交互审批通道回写）。
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

// TestApproval_RescanThrottled 是 W2 Minor⑧（§8.9）+ ⑦（§9.2）补缺：Check 未命中的重扫受
// rescanInterval 节流——窗内第二次 Check 不重扫目录（防高频重试反复 IO）。⑦：注入假时钟 +
// 参数化 interval，消除对真实 wall-clock 的依赖（旧测用固定 2s 常量，CI 重载 >2s 会致窗内两次
// Check 实际跨窗被误判为已重扫 → 假失败）。
func TestApproval_RescanThrottled(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)
	// ⑦：注入假时钟（可确定性推进）+ 1s 节流间隔（远小于 ttl，避免 approved 被误判过期）。
	base := time.Now()
	cur := base
	am.now = func() time.Time { return cur }
	am.rescanInterval = time.Second

	req, _ := am.Request("exec", `{"cmd":"x"}`, "x", "critical", "exec.sudo", "提权", "")
	// 首次 Check 未命中 → rescanDue(lastRescan=0 → true) 重扫，lastRescan 更新为 base。
	_ = am.Check("exec", req.ArgsDigest)
	// 立即写 approved 文件（外部审批者运行中落盘）。
	req.Status = ApprovalApproved
	raw, _ := json.MarshalIndent(req, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "approvals", req.ID+".json"), raw, 0o644)
	// 节流窗内（推进 500ms < 1s 间隔）第二次 Check → rescanDue false（不重扫）→ 仍读内存
	// pending → nil（节流生效）。假时钟确定性推进，无真实 sleep、无 wall-clock 竞态。
	cur = base.Add(500 * time.Millisecond)
	if got := am.Check("exec", req.ArgsDigest); got != nil {
		t.Fatal("W2 节流: 窗内第二次 Check 不应重扫目录(应仍 nil,防高频重试反复 IO)")
	}
}

// TestApproval_RescanWindowExpiryResumes 是 ⑦（§9.2）正向补缺：节流窗过期后 Check 恢复重扫，
// 运行中外部落盘的批准文件经重扫可见（闭环不因节流永久断路）。旧测只验窗内节流、未验窗过期恢复。
func TestApproval_RescanWindowExpiryResumes(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)
	base := time.Now()
	cur := base
	am.now = func() time.Time { return cur }
	am.rescanInterval = time.Second

	req, _ := am.Request("exec", `{"cmd":"y"}`, "y", "critical", "exec.sudo", "提权", "")
	_ = am.Check("exec", req.ArgsDigest) // 首次重扫，lastRescan=base
	// 窗内落盘 approved 文件。
	req.Status = ApprovalApproved
	raw, _ := json.MarshalIndent(req, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "approvals", req.ID+".json"), raw, 0o644)
	// 窗内（500ms < 1s）不重扫 → nil。
	cur = base.Add(500 * time.Millisecond)
	if got := am.Check("exec", req.ArgsDigest); got != nil {
		t.Fatal("窗内不应重扫(节流生效)")
	}
	// ⑦正向：推进假时钟越过节流窗（1.5s >= 1s，且 << 30m ttl 免 approved 被判过期）→
	// rescanDue true → 重扫读入 approved → 命中放行（节流窗过期后闭环恢复）。
	cur = base.Add(1500 * time.Millisecond)
	got := am.Check("exec", req.ArgsDigest)
	if got == nil || got.Status != ApprovalApproved {
		t.Fatalf("W2 节流窗过期后应恢复重扫并命中 approved, got %+v", got)
	}
}

// TestApproval_ExpiredFileCleanup 是 Minor④（§8.9）回归：rebuild 清理过期审批文件（防 approvals
// 目录随运行时间无界堆积）。
func TestApproval_ExpiredFileCleanup(t *testing.T) {
	dir := t.TempDir()
	am := NewApprovalManager(dir, 30*time.Minute)
	// 构造一个已过期请求文件（ExpiresMs < now）。
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
	am.rebuild() // 应清理过期文件
	if _, err := os.Stat(filepath.Join(apprDir, expired.ID+".json")); !os.IsNotExist(err) {
		t.Fatal("Minor④: rebuild 应删除过期审批文件(防无界堆积)")
	}
	if am.Check("exec", "d1") != nil {
		t.Fatal("过期请求不应命中(已清理出索引)")
	}
}

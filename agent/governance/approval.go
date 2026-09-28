package governance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ApprovalStatus 是批准状态。
type ApprovalStatus string

const (
	// ApprovalPending 等待外部批准者裁决；此时被治理的操作不会执行。
	ApprovalPending ApprovalStatus = "pending"
	// ApprovalApproved 已批准：允许继续执行该次操作。
	ApprovalApproved ApprovalStatus = "approved"
	// ApprovalDenied 已拒绝：操作终止并作为拒绝结果返回。
	ApprovalDenied ApprovalStatus = "denied"
	// ApprovalExpired 超过有效期未被裁决：按未获批准处理，不得事后凭旧请求继续执行。
	ApprovalExpired ApprovalStatus = "expired"
)

// ApprovalRequest 是一次批准请求（一请求一文件，可被外部审批者读写）。
type ApprovalRequest struct {
	ID          string         `json:"id"`
	ToolName    string         `json:"tool"`
	ArgsDigest  string         `json:"args_digest"`
	ArgsPreview string         `json:"args_preview"`
	RiskLevel   string         `json:"risk"`
	RuleID      string         `json:"rule_id"`
	Reason      string         `json:"reason"`
	GoalID      string         `json:"goal_id,omitempty"`
	CreatedMs   int64          `json:"created_ms"`
	ExpiresMs   int64          `json:"expires_ms"`
	Status      ApprovalStatus `json:"status"`
	DecidedBy   string         `json:"decided_by,omitempty"`
}

// ApprovalManager 管理异步批准请求（文件通道）。并发安全（内存索引 + 文件持久）。
type ApprovalManager struct {
	dir string
	ttl time.Duration

	// channels：审批请求送达通道（微信注入等，装配期
	// AddChannel 注册）。Deliver 失败不阻塞审批门——pending 文件已落盘，CLI/文件批准
	// 始终可用（闸不是墙）。
	channels []ApprovalChannel

	// rescanInterval 是 Check 未命中后重扫 approvals 目录的最小间隔（W2 节流）。默认
	// approvalRescanInterval（2s）；⑦：测试可注入小值 + 假时钟，消除对真实 wall-clock
	// 的依赖（CI 重载 >2s 会致旧节流测假失败——窗内两次 Check 实际跨窗被误判为已重扫）。
	rescanInterval time.Duration

	mu    sync.RWMutex
	index map[string]*ApprovalRequest

	lastRescan int64

	// now 是可注入时钟（默认 time.Now）。⑦：测试注入假时钟以确定性推进节流窗，无需真实 sleep。
	now func() time.Time
}

// NewApprovalManager 构建批准管理器。dir 非空时持久化到 <dir>/approvals/ 并重建索引。
func NewApprovalManager(dir string, ttl time.Duration) *ApprovalManager {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	a := &ApprovalManager{
		ttl:            ttl,
		rescanInterval: approvalRescanInterval,
		index:          make(map[string]*ApprovalRequest),
		now:            time.Now,
	}
	if dir != "" {
		a.dir = filepath.Join(dir, "approvals")
		_ = os.MkdirAll(a.dir, 0o755)
		a.rebuild()
	}
	return a
}

// ArgsDigest 计算参数摘要（sha256），批准绑定此摘要防「批准后换参数」。
func ArgsDigest(argsJSON string) string {
	sum := sha256.Sum256([]byte(argsJSON))
	return hex.EncodeToString(sum[:])
}

// Request 登记一个 pending 批准请求（写文件 + 内存索引）。返回请求（含 ID/ExpiresMs）。
func (a *ApprovalManager) Request(toolName, argsJSON, argsPreview, level, ruleID, reason, goalID string) (*ApprovalRequest, error) {
	now := a.now()
	req := &ApprovalRequest{
		ID:          fmt.Sprintf("appr-%d", now.UnixNano()),
		ToolName:    toolName,
		ArgsDigest:  ArgsDigest(argsJSON),
		ArgsPreview: truncate(argsPreview, 500),
		RiskLevel:   level,
		RuleID:      ruleID,
		Reason:      reason,
		GoalID:      goalID,
		CreatedMs:   now.UnixMilli(),
		ExpiresMs:   now.Add(a.ttl).UnixMilli(),
		Status:      ApprovalPending,
	}
	a.mu.Lock()
	a.index[req.ID] = req
	snapshot := *req
	a.mu.Unlock()
	if err := a.write(&snapshot); err != nil {
		return nil, err
	}
	a.deliverAll(&snapshot)
	return req, nil
}

// approvalRescanInterval 是 Check 未命中后重扫 approvals 目录的最小间隔（W2 节流）。
const approvalRescanInterval = 2 * time.Second

// Check 查找匹配 (toolName, argsDigest) 的、已批准且未过期的请求（批准放行判据）。
// 精确匹配 digest → 防「批准后换参数」。无匹配返回 nil（调用方据此挂起/拒绝）。
// W2：索引未命中时**节流重扫** approvals 目录——外部审批者（人工 digest 文件 / 微信
// 通道回写）在运行中落盘批准文件后须可见。否则 Check 只读构造时索引 → 运行中外部批准永不
// 可见 → critical 恒 Hold、重试持续堆积 pending（治理审批闭环断路）。
func (a *ApprovalManager) Check(toolName, argsDigest string) *ApprovalRequest {
	if req := a.checkIndex(toolName, argsDigest); req != nil {
		return req
	}
	if a.dir != "" && a.rescanDue() {
		a.rebuild()
		return a.checkIndex(toolName, argsDigest)
	}
	return nil
}

// rescanDue 报告是否到了重扫时机（节流：距上次 >= rescanInterval），并更新 lastRescan。
func (a *ApprovalManager) rescanDue() bool {
	now := a.now().UnixNano()
	a.mu.Lock()
	defer a.mu.Unlock()
	if now-a.lastRescan < int64(a.rescanInterval) {
		return false
	}
	a.lastRescan = now
	return true
}

// checkIndex 在当前内存索引查匹配（已批准、未过期、精确 digest），返回最新创建者。
func (a *ApprovalManager) checkIndex(toolName, argsDigest string) *ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := a.now().UnixMilli()
	var best *ApprovalRequest
	for _, req := range a.index {
		if req.ToolName == toolName && req.ArgsDigest == argsDigest &&
			req.Status == ApprovalApproved && req.ExpiresMs > now {
			if best == nil || req.CreatedMs > best.CreatedMs {
				best = req
			}
		}
	}
	return best
}

// Decide 记录批准决策（外部审批者经 CLI/文件调用）。写回文件 + 更新索引。
func (a *ApprovalManager) Decide(id string, status ApprovalStatus, by string) error {
	a.mu.Lock()
	req, ok := a.index[id]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("governance: approval request %s not found", id)
	}
	req.Status = status
	req.DecidedBy = by
	snapshot := *req
	a.mu.Unlock()
	return a.write(&snapshot)
}

// Pending 返回全部未过期 pending 请求（供审批通道展示），按创建时间升序。
func (a *ApprovalManager) Pending() []*ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := a.now().UnixMilli()
	out := make([]*ApprovalRequest, 0)
	for _, req := range a.index {
		if req.Status == ApprovalPending && req.ExpiresMs > now {
			out = append(out, req)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedMs < out[j].CreatedMs })
	return out
}

func (a *ApprovalManager) path(id string) string { return filepath.Join(a.dir, id+".json") }

func (a *ApprovalManager) write(req *ApprovalRequest) error {
	if a.dir == "" {
		return nil
	}
	raw, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return err
	}
	tmp := a.path(req.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.path(req.ID))
}

func (a *ApprovalManager) rebuild() {
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return
	}
	now := a.now().UnixMilli()
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(a.dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req ApprovalRequest
		if err := json.Unmarshal(raw, &req); err != nil || req.ID == "" {
			continue
		}
		if req.ExpiresMs > 0 && req.ExpiresMs < now {
			_ = os.Remove(path)
			delete(a.index, req.ID)
			continue
		}
		a.index[req.ID] = &req
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

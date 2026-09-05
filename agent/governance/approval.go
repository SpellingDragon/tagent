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

// ==================== ApprovalManager（T-G · critical 异步人工批准）====================
//
// 关键设计（报告 D3 §4.6.3/§3.4-3）：critical 操作默认**异步**批准而非阻塞等待——工具
// 调用在框架 ReAct 循环内同步执行，阻塞等待会卡死单消费者 runEventLoop 造成 bus 积压。
// 故：Request 写 pending 文件 + 立即返回 PENDING_APPROVAL 让模型知悉；外部审批者（CLI/
// 微信）写 approved 文件；批准后外部注入新事件触发重试，args_digest 匹配防「批准后换参数」。
// 一请求一文件（<dir>/approvals/<id>.json），外部审批者直接写文件即可决策。

// ApprovalStatus 是批准状态。
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalDenied   ApprovalStatus = "denied"
	ApprovalExpired  ApprovalStatus = "expired"
)

// ApprovalRequest 是一次批准请求（一请求一文件，可被外部审批者读写）。
type ApprovalRequest struct {
	ID          string         `json:"id"`
	ToolName    string         `json:"tool"`
	ArgsDigest  string         `json:"args_digest"`  // sha256——批准绑定参数，防「批准后换参」
	ArgsPreview string         `json:"args_preview"` // 截断的人类可读预览（审批者要看内容）
	RiskLevel   string         `json:"risk"`
	RuleID      string         `json:"rule_id"`
	Reason      string         `json:"reason"`
	GoalID      string         `json:"goal_id,omitempty"`
	CreatedMs   int64          `json:"created_ms"`
	ExpiresMs   int64          `json:"expires_ms"` // 默认 created + TTL
	Status      ApprovalStatus `json:"status"`
	DecidedBy   string         `json:"decided_by,omitempty"`
}

// ApprovalManager 管理异步批准请求（文件通道）。并发安全（内存索引 + 文件持久）。
type ApprovalManager struct {
	dir string        // <dir>/approvals/
	ttl time.Duration // 请求过期时长（默认 30m）

	mu    sync.RWMutex
	index map[string]*ApprovalRequest // id → 请求（内存索引，启动从文件重建）
}

// NewApprovalManager 构建批准管理器。dir 非空时持久化到 <dir>/approvals/ 并重建索引。
func NewApprovalManager(dir string, ttl time.Duration) *ApprovalManager {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	a := &ApprovalManager{ttl: ttl, index: make(map[string]*ApprovalRequest)}
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
	now := time.Now()
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
	snapshot := *req // 值拷贝：write 在锁外读副本，避免与并发访问竞争共享对象
	a.mu.Unlock()
	if err := a.write(&snapshot); err != nil {
		return nil, err
	}
	return req, nil
}

// Check 查找匹配 (toolName, argsDigest) 的、已批准且未过期的请求（批准放行判据）。
// 精确匹配 digest → 防「批准后换参数」。无匹配返回 nil（调用方据此挂起/拒绝）。
func (a *ApprovalManager) Check(toolName, argsDigest string) *ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now().UnixMilli()
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
	snapshot := *req // 值拷贝：write 在锁外读副本，避免与并发 Check/Decide 竞争共享对象
	a.mu.Unlock()
	return a.write(&snapshot)
}

// Pending 返回全部未过期 pending 请求（供审批通道展示），按创建时间升序。
func (a *ApprovalManager) Pending() []*ApprovalRequest {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now().UnixMilli()
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
		return nil // 纯内存模式（测试/无持久化）
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
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(a.dir, e.Name()))
		if err != nil {
			continue
		}
		var req ApprovalRequest
		if err := json.Unmarshal(raw, &req); err == nil && req.ID != "" {
			a.index[req.ID] = &req
		}
	}
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

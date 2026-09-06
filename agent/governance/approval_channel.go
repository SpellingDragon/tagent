package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
)

// ==================== 审批消息流（3.1/3.2/3.3 design-report-closeout） ====================
//
// 统一抽象：审批请求 → 渗透消息（Deliver）→ 人工响应（RespondFile 写回应）→ Check
// 重扫落盘生效。状态机映射既有 ApprovalStatus：requested=pending、responded=
// approved/denied、consumed=Check 命中放行（approved 且未过期）。
// 通道投递失败永不阻塞审批门（闸不是墙）：pending 文件已落盘，人工仍可经
// CLI/文件批准。

// ApprovalChannel 是审批请求的送达通道（微信注入/邮件/IM 等由装配层实现）。
// Deliver 失败仅记日志——审批门不依赖任何通道在线。
type ApprovalChannel interface {
	Deliver(req *ApprovalRequest) error
}

// AddChannel 注册送达通道（装配期）。Request 成功后逐个 Deliver（尽力）。
func (a *ApprovalManager) AddChannel(ch ApprovalChannel) {
	if a == nil || ch == nil {
		return
	}
	a.mu.Lock()
	a.channels = append(a.channels, ch)
	a.mu.Unlock()
}

// deliverAll 尽力投递到全部通道（锁外调用；失败仅日志）。
func (a *ApprovalManager) deliverAll(req *ApprovalRequest) {
	a.mu.RLock()
	channels := append([]ApprovalChannel(nil), a.channels...)
	a.mu.RUnlock()
	for _, ch := range channels {
		if err := ch.Deliver(req); err != nil {
			log.Warnf("[governance] approval channel deliver failed (id=%s): %v — file/CLI approval still available", req.ID, err)
		}
	}
}

// RespondFile 是 CLI 与消息通道共用的**人工回应纯函数**（3.2/3.3）：在 approvals
// 目录中定位 digest 前缀匹配的 pending 请求，写入 approved/denied 状态（DecidedBy
// 留痕）。幂等：已回应（approved/denied）的请求不重复改写，返回说明。
// digest 支持短前缀（≥8 字符，与 approval_list 展示的短 digest 一致）。
func RespondFile(approvalsDir, digest string, approve bool, by string) (string, error) {
	if approvalsDir == "" {
		return "", fmt.Errorf("governance: approvals dir not configured")
	}
	if len(digest) < 8 {
		return "", fmt.Errorf("governance: digest %q too short (need >= 8 hex chars)", digest)
	}
	entries, err := os.ReadDir(approvalsDir)
	if err != nil {
		return "", fmt.Errorf("governance: read approvals dir: %w", err)
	}
	status := ApprovalDenied
	if approve {
		status = ApprovalApproved
	}
	matched := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(approvalsDir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req ApprovalRequest
		if err := json.Unmarshal(raw, &req); err != nil || req.ID == "" {
			continue
		}
		if !strings.HasPrefix(req.ArgsDigest, digest) {
			continue
		}
		matched++
		if req.Status != ApprovalPending {
			// 幂等：已回应不重复改写（重复 approve/reject 无副作用）。
			return fmt.Sprintf("请求 %s 已是 %s 状态（幂等，未改写）", req.ID, req.Status), nil
		}
		req.Status = status
		req.DecidedBy = by
		out, err := json.Marshal(req)
		if err != nil {
			return "", fmt.Errorf("governance: marshal response: %w", err)
		}
		if err := os.WriteFile(path, out, 0o600); err != nil {
			return "", fmt.Errorf("governance: write response: %w", err)
		}
		return fmt.Sprintf("请求 %s（tool=%s）已置为 %s（by %s）；agent 下次 Check 重扫即生效",
			req.ID, req.ToolName, status, by), nil
	}
	if matched == 0 {
		return "", fmt.Errorf("governance: no approval request matches digest prefix %q", digest)
	}
	return "", nil
}

// ParseApprovalReply 解析消息通道的人工回复（3.3 微信注入侧共用纯函数）：
// "approve <digest>" / "reject <digest>"（大小写不敏感，digest ≥8 hex）。
// 非审批回复返回 ok=false（调用方按普通消息处理）。
func ParseApprovalReply(text string) (digest string, approve bool, ok bool) {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	var verb string
	switch {
	case strings.HasPrefix(lower, "approve "):
		verb = "approve "
	case strings.HasPrefix(lower, "reject "):
		verb = "reject "
	case strings.HasPrefix(lower, "批准 "):
		verb = "批准 "
	case strings.HasPrefix(lower, "拒绝 "):
		verb = "拒绝 "
	default:
		return "", false, false
	}
	rest := strings.TrimSpace(trimmed[len(verb):])
	fields := strings.Fields(rest)
	if len(fields) == 0 || len(fields[0]) < 8 {
		return "", false, false
	}
	return fields[0], verb == "approve " || verb == "批准 ", true
}

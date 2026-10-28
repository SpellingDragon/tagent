// wechat_approval.go 是微信审批面：审批请求经微信下发、审批裁决按白名单解析，
// 以及把治理策略适配到宿主每一次调用的准入席位。审批的传输是微信语义，裁决语义
// 仍由核心的治理策略决定，应用侧只做接线。
//
// 契约: docs/wiki/examples/wechat-bot-runtime.md#module-layout
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/features/evolution/governance"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/runtime/task/approval"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// governanceEntry 是文档治理段的读取形状：审批面只关心开关、执行等级、请求目录与有效期。
type governanceEntry struct {
	Enabled            bool   `yaml:"enabled"`
	Enforcement        string `yaml:"enforcement"`
	Dir                string `yaml:"dir"`
	ApprovalTTLMinutes int    `yaml:"approval_ttl_minutes"`
}

// strict reports whether the section raises the gate to the refusing grade.
func (g governanceEntry) strict() bool {
	return strings.EqualFold(strings.TrimSpace(g.Enforcement), "strict")
}

// ErrApprovalNotApprover reports a message-channel ruling from a user outside
// the approver whitelist: an empty whitelist refuses every message ruling.
var ErrApprovalNotApprover = errors.New("wechat-bot: the sender is not in the approval whitelist")

// ErrApprovalAmbiguous reports a ruling whose digest prefix names more than one
// pending request: an ambiguous ruling is refused instead of applied to a guess.
var ErrApprovalAmbiguous = errors.New("wechat-bot: the reply names more than one pending approval")

// minApprovalPrefix is the shortest digest prefix a ruling may name.
const minApprovalPrefix = 4

// approvalSender is the narrow outbound face the gateway announces requests
// over; *wechat.Bot satisfies it.
type approvalSender interface {
	SendTextToUser(ctx context.Context, toUserID, text string) error
}

// approvalGateway is the host-side approval face: it delivers one request to
// every configured approver chat and resolves a reply against the manager, so a
// ruling is bound to the arguments that were held.
type approvalGateway struct {
	manager   *approval.ApprovalManager
	enabled   bool
	strict    bool
	approvers []string

	mu     sync.RWMutex
	sender approvalSender
}

// newApprovalGateway returns the gateway of one governance section: the request
// directory, the approval validity and the approver whitelist.
func newApprovalGateway(gov governanceEntry, approvers []string) *approvalGateway {
	dir := ""
	if gov.Dir != "" {
		dir = gov.Dir
	}
	ttl := time.Duration(gov.ApprovalTTLMinutes) * time.Minute
	g := &approvalGateway{
		manager: approval.NewApprovalManager(dir, ttl),
		enabled: gov.Enabled,
		strict:  gov.strict(),
	}
	for _, id := range approvers {
		if id = strings.TrimSpace(id); id != "" {
			g.approvers = append(g.approvers, id)
		}
	}
	g.manager.AddChannel(g)
	return g
}

// Manager reports the approval manager the execution policy reads its rulings
// through.
func (g *approvalGateway) Manager() *approval.ApprovalManager { return g.manager }

// Enabled reports whether the host attaches the approval policy of this
// governance section to the assembly.
func (g *approvalGateway) Enabled() bool { return g != nil && g.enabled }

// SetSender binds the outbound channel the requests are announced over.
func (g *approvalGateway) SetSender(sender approvalSender) {
	g.mu.Lock()
	g.sender = sender
	g.mu.Unlock()
}

// IsApprover reports whether one user may rule over a request through the
// message channel.
func (g *approvalGateway) IsApprover(userID string) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	for _, id := range g.approvers {
		if id == userID {
			return true
		}
	}
	return false
}

// Deliver offers one approval request to every configured approver chat. No
// approver and no sender are named failures: the manager records them as
// visible delivery gaps and the gate keeps waiting.
func (g *approvalGateway) Deliver(ctx context.Context, req approval.ApprovalRequest) error {
	g.mu.RLock()
	sender := g.sender
	g.mu.RUnlock()
	if len(g.approvers) == 0 {
		return errors.New("wechat-bot: no approval recipient is configured (app.wechat.approvers)")
	}
	if sender == nil {
		return errors.New("wechat-bot: the wechat sender is not ready")
	}
	digest := shortDigest(req.ArgsDigest)
	text := fmt.Sprintf("⚠ 审批请求（tool=%s）\ndigest: %s\n批准回复: approve %s ｜ 拒绝: reject %s",
		req.ToolName, digest, digest, digest)
	var failures []error
	for _, to := range g.approvers {
		if err := sender.SendTextToUser(ctx, to, text); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", to, err))
		}
	}
	return errors.Join(failures...)
}

// Resolve reads one ruling reply. It reports handled=false for a message that is
// not a ruling, so the message continues as a chat turn. A ruling from outside
// the whitelist, an ambiguous prefix and an unknown prefix are named refusals;
// the manager decides the matched request under the digest it holds.
func (g *approvalGateway) Resolve(from, text string) (handled bool, reply string, err error) {
	status, prefix, ok := parseApprovalReply(text)
	if !ok {
		return false, "", nil
	}
	by := strings.TrimSpace(from)
	if by != "cli" && !g.IsApprover(by) {
		return true, "你没有审批权限（不在 app.wechat.approvers 白名单）。请由审批人经 CLI（wechat-bot approve <digest>）执行。", ErrApprovalNotApprover
	}
	req, err := g.match(prefix)
	if err != nil {
		log.Warnf("[approval] ruling from %s refused: %v", by, err)
		return true, "审批失败: " + err.Error(), err
	}
	decided, err := g.manager.Decide(req.ID, status, "wechat:"+by)
	if err != nil {
		log.Warnf("[approval] decide %s refused: %v", req.ID, err)
		return true, "审批失败: " + err.Error(), err
	}
	log.Infof("[approval] request %s decided %s by %s (tool=%s)", decided.ID, decided.Status, by, decided.ToolName)
	return true, fmt.Sprintf("审批已记录：%s %s（tool=%s, digest=%s）",
		decided.ID, decided.Status, decided.ToolName, shortDigest(decided.ArgsDigest)), nil
}

// match reports the one pending request whose digest starts with the prefix. An
// unknown prefix and an ambiguous prefix are refused by name.
func (g *approvalGateway) match(prefix string) (*approval.ApprovalRequest, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if len(prefix) < minApprovalPrefix {
		return nil, fmt.Errorf("%w: 前缀至少 %d 位十六进制字符", approval.ErrApprovalNotFound, minApprovalPrefix)
	}
	var found []*approval.ApprovalRequest
	for _, req := range g.manager.Pending() {
		if strings.HasPrefix(strings.ToLower(req.ArgsDigest), prefix) {
			found = append(found, req)
		}
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("%w: 前缀 %s", approval.ErrApprovalNotFound, prefix)
	case 1:
		return found[0], nil
	default:
		return nil, fmt.Errorf("%w: 前缀 %s 命中 %d 条", ErrApprovalAmbiguous, prefix, len(found))
	}
}

// parseApprovalReply reads one ruling reply: the verb beside the digest prefix
// an approver answers with. A message that carries neither is not a ruling.
func parseApprovalReply(text string) (approval.ApprovalStatus, string, bool) {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return "", "", false
	}
	verb, prefix := strings.ToLower(fields[0]), strings.TrimSpace(fields[1])
	switch verb {
	case "approve", "批准":
		return approval.ApprovalApproved, prefix, true
	case "reject", "拒绝":
		return approval.ApprovalDenied, prefix, true
	default:
		return "", "", false
	}
}

// shortDigest renders the readable prefix of one argument digest.
func shortDigest(digest string) string {
	if len(digest) > 8 {
		return digest[:8]
	}
	return digest
}

// approvalRuling adapts the governance policy of this section onto the approval
// seat the host consults before every call. The policy is the same gate the
// assembly used to attach itself: critical calls wait for an approval, strict
// refusals are terminal, and everything else runs.
func approvalRuling(gateway *approvalGateway) task.Approver {
	policyOpts := []governance.PolicyOption{}
	if gateway != nil {
		policyOpts = append(policyOpts, governance.WithApprovals(gateway.Manager()))
		if gateway.strict {
			policyOpts = append(policyOpts, governance.WithStrict())
		}
	}
	return policyRuling(governance.Open(policyOpts...))
}

// policyRuling is the application-side copy of the core adapter: it consumes the
// one Before pass the call carries and reports the class the task wrapper admits
// the call under. A refused call is terminal, a held call is a wait, and a call
// driven without its pass is refused closed rather than ruled at a guess.
func policyRuling(governor plugin.Governor) task.Approver {
	return func(ctx context.Context, _ string, _ []byte) (task.Ruling, error) {
		pass, ok := plugin.CallPassFrom(ctx)
		if !ok {
			return task.Ruling{}, errors.New("wechat-bot: the call carries no Before pass to rule over")
		}
		decision, err := governor.Judge(pass)
		if err != nil {
			return task.Ruling{}, err
		}
		reason := decision.Reason
		if decision.Rule != "" {
			reason = fmt.Sprintf("%s (rule: %s)", decision.Reason, decision.Rule)
		}
		switch {
		case decision.Refused():
			return task.Ruling{Kind: task.RulingDeny, Reason: reason}, nil
		case decision.Disposition == plugin.DispositionHold:
			return task.Ruling{Kind: task.RulingHold, Reason: reason}, nil
		default:
			return task.Ruling{Kind: task.RulingRun, Reason: reason}, nil
		}
	}
}

// runApprovalCLI resolves one ruling typed on the command line, before the bot
// logs in. The second return value is the process exit code.
func runApprovalCLI(gateway *approvalGateway, args []string) (bool, int) {
	if len(args) < 2 || (args[0] != "approve" && args[0] != "reject") {
		return false, 0
	}
	_, reply, err := gateway.Resolve("cli", args[0]+" "+args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "审批失败: %v\n", err)
		return true, 1
	}
	fmt.Println(reply)
	return true, 0
}

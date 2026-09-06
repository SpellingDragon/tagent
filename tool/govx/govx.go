// Package govx provides the governance face tools (5.1, design-report-closeout):
// goal declaration/query and audit query tools that make the bounded-autonomy
// gate usable from the conversation. Entry-only (wired in tagent.go alongside
// refine); all tools are advisory/record-keeping — the gate itself stays in
// agent/governance.
package govx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/SpellingDragon/tagent/agent/governance"
)

// ==================== goal_declare ====================

type goalDeclareArgs struct {
	Statement  string `json:"statement" jsonschema:"required,description=目标陈述（一句话，说明本次自治边界内要达成什么）"`
	CreatedBy  string `json:"created_by,omitempty" jsonschema:"description=声明者（user/agent，默认 agent）,enum=user,enum=agent"`
	ExpiresInS int64  `json:"expires_in_seconds,omitempty" jsonschema:"description=过期秒数（0=不过期）"`
}

type goalDeclareResult struct {
	OK     bool   `json:"ok"`
	GoalID string `json:"goal_id,omitempty"`
	Note   string `json:"note,omitempty"`
}

// ==================== goal_list ====================

type goalListArgs struct {
	ActiveOnly bool `json:"active_only,omitempty" jsonschema:"description=仅列活跃目标（默认全部）"`
}

type goalListResult struct {
	Goals []goalItem `json:"goals"`
	Count int        `json:"count"`
}

type goalItem struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	Expires   string `json:"expires,omitempty"`
}

// ==================== goal_resolve ====================

type goalResolveArgs struct {
	GoalID string `json:"goal_id" jsonschema:"required,description=目标 ID（g-N）"`
	Status string `json:"status" jsonschema:"required,description=终态,enum=achieved,enum=abandoned"`
}

type goalResolveResult struct {
	OK   bool   `json:"ok"`
	Note string `json:"note,omitempty"`
}

// ==================== denial_query ====================

type denialQueryArgs struct {
	Limit int `json:"limit,omitempty" jsonschema:"description=返回条数上限（默认 20）"`
}

type denialQueryResult struct {
	Records []denialItem `json:"records"`
	Count   int          `json:"count"`
}

type denialItem struct {
	Tool      string `json:"tool"`
	Reason    string `json:"reason"`
	Risk      string `json:"risk"`
	Agent     string `json:"agent,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ==================== approval_list ====================

type approvalListArgs struct{}

type approvalListResult struct {
	Pending []approvalItem `json:"pending"`
	Count   int            `json:"count"`
	Note    string         `json:"note,omitempty"`
}

type approvalItem struct {
	Digest  string `json:"digest"`
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	Expires string `json:"expires,omitempty"`
	HowTo   string `json:"how_to_approve"`
}

// NewGoalTools builds the five governance face tools bound to the shared gate
// (Goals/Ledger/Approval accessors). Entry-only wiring lives in tagent.go.
func NewGoalTools(gate *governance.GovernanceGate) []tool.Tool {
	goals := gate.Goals()
	ledger := gate.Ledger()
	approval := gate.Approval()

	declare := function.NewFunctionTool(
		func(ctx context.Context, args goalDeclareArgs) (goalDeclareResult, error) {
			if strings.TrimSpace(args.Statement) == "" {
				return goalDeclareResult{OK: false}, fmt.Errorf("statement 不能为空")
			}
			createdBy := args.CreatedBy
			if createdBy == "" {
				createdBy = "agent"
			}
			var expiresMs int64
			if args.ExpiresInS > 0 {
				expiresMs = time.Now().Add(time.Duration(args.ExpiresInS) * time.Second).UnixMilli()
			}
			id := goals.Declare(args.Statement, createdBy, expiresMs)
			return goalDeclareResult{
				OK: true, GoalID: id,
				Note: "目标已登记（governance 事件持久化，重启不丢）。high+ 风险操作现在可通过 goal 门。",
			}, nil
		},
		function.WithName("goal_declare"),
		function.WithDescription("登记一条自治目标（有界自治的 goal 门凭证）：high 及以上风险操作要求存在活跃 goal。"+
			"目标陈述应说明本次要达成什么；登记即写治理审计事件。"),
	)

	list := function.NewFunctionTool(
		func(ctx context.Context, args goalListArgs) (goalListResult, error) {
			items := make([]goalItem, 0)
			for _, g := range goals.List() {
				if args.ActiveOnly && g.Status != governance.GoalActive {
					continue
				}
				item := goalItem{ID: g.ID, Statement: g.Statement, Status: string(g.Status), CreatedBy: g.CreatedBy}
				if g.ExpiresMs > 0 {
					item.Expires = time.UnixMilli(g.ExpiresMs).Format(time.RFC3339)
				}
				items = append(items, item)
			}
			return goalListResult{Goals: items, Count: len(items)}, nil
		},
		function.WithName("goal_list"),
		function.WithDescription("列出已登记的自治目标（active_only=true 仅活跃）。"),
	)

	resolve := function.NewFunctionTool(
		func(ctx context.Context, args goalResolveArgs) (goalResolveResult, error) {
			status := governance.GoalStatus(args.Status)
			if status != governance.GoalAchieved && status != governance.GoalAbandoned {
				return goalResolveResult{OK: false}, fmt.Errorf("status 必须是 achieved 或 abandoned")
			}
			if !goals.Resolve(args.GoalID, status) {
				return goalResolveResult{OK: false, Note: "goal 不存在: " + args.GoalID}, nil
			}
			return goalResolveResult{OK: true, Note: fmt.Sprintf("goal %s 已置为 %s", args.GoalID, status)}, nil
		},
		function.WithName("goal_resolve"),
		function.WithDescription("关闭一条自治目标（achieved=达成 / abandoned=放弃）。关闭后 goal 门对该目标失效。"),
	)

	denials := function.NewFunctionTool(
		func(ctx context.Context, args denialQueryArgs) (denialQueryResult, error) {
			limit := args.Limit
			if limit <= 0 {
				limit = 20
			}
			recs := ledger.Query(limit)
			items := make([]denialItem, 0, len(recs))
			for _, r := range recs {
				items = append(items, denialItem{
					Tool: r.ToolName, Reason: r.Reason, Risk: r.Level.String(), Agent: r.AgentName,
					Timestamp: time.UnixMilli(r.Timestamp).Format(time.RFC3339),
				})
			}
			return denialQueryResult{Records: items, Count: len(items)}, nil
		},
		function.WithName("denial_query"),
		function.WithDescription("查询最近的治理拒绝记录（工具/原因/风险级/来源 agent/时刻）——用于自省与调整策略。"),
	)

	approvals := function.NewFunctionTool(
		func(ctx context.Context, args approvalListArgs) (approvalListResult, error) {
			pending := approval.Pending()
			items := make([]approvalItem, 0, len(pending))
			for _, p := range pending {
				short := p.ArgsDigest
				if len(short) > 12 {
					short = short[:12]
				}
				item := approvalItem{
					Digest: p.ArgsDigest, Tool: p.ToolName, Summary: p.ArgsPreview,
					HowTo: "人工批准: tagent approve " + short + "（或经消息通道回复 approve " + short + "）",
				}
				if p.ExpiresMs > 0 {
					item.Expires = time.UnixMilli(p.ExpiresMs).Format(time.RFC3339)
				}
				items = append(items, item)
			}
			note := ""
			if len(items) == 0 {
				note = "当前无待批准项。"
			}
			return approvalListResult{Pending: items, Count: len(items), Note: note}, nil
		},
		function.WithName("approval_list"),
		function.WithDescription("列出待人工批准的 critical 操作（digest/工具/摘要/过期时刻/批准方式）。批准只能由人完成——agent 无批准权。"),
	)

	return []tool.Tool{declare, list, resolve, denials, approvals}
}

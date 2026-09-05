package evolution

import (
	"context"
	"fmt"
	"sort"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// ==================== refine 工具（T-EVO · agent 自我修改通道）====================
//
// 核心主张（报告 D1 §9）：**agent 永远无法直接生效自我修改**——refine 工具**无 activate
// op**。op 白名单：propose（提案 → 发布状态机裁决）/ diff（版本对比）/ status（当前+历史）/
// rollback（回退到历史已验证版本，安全操作）。propose 只是把 draft 提交给 ReleaseManager，
// 激活与否由风险分级发布道决定（快道后验/慢道门后），agent 无直接激活权（调和 T-E/T-F）。
//
// 有界自治（报告 D1 §4.5.2）：op 白名单 + 字段白名单（bundle v1 只含 prompts/params/model，
// 不含工具集）+ 每日提案预算（由调用方 Gate 约束）+ ProtectedPrompts 强制慢道。

// refineArgs 是 refine 工具入参（op 即路由，无 activate）。
type refineArgs struct {
	Op        string `json:"op" jsonschema:"description=操作,enum=propose,enum=diff,enum=status,enum=rollback"`
	PromptKey string `json:"prompt_key,omitempty" jsonschema:"description=propose 要改的提示词逻辑名(如 system)"`
	Content   string `json:"content,omitempty" jsonschema:"description=propose 新提示词内容"`
	Note      string `json:"note,omitempty" jsonschema:"description=propose 修改理由(审计留痕)"`
	TargetID  string `json:"target_id,omitempty" jsonschema:"description=diff/rollback 目标 bundle id"`
}

// bundleInfo 是 bundle 的对外摘要（不暴露全文，避免上下文膨胀）。
type bundleInfo struct {
	ID        string   `json:"id"`
	ParentID  string   `json:"parent_id,omitempty"`
	Note      string   `json:"note,omitempty"`
	CreatedBy string   `json:"created_by,omitempty"`
	PromptKeys []string `json:"prompt_keys"`
	Model     string   `json:"model,omitempty"`
	Active    bool     `json:"active"`
}

// refineResult 是 refine 工具输出。
type refineResult struct {
	Op       string        `json:"op"`
	OK       bool          `json:"ok"`
	Message  string        `json:"message"`
	BundleID string        `json:"bundle_id,omitempty"` // propose 产生的 draft id
	Lane     string        `json:"lane,omitempty"`      // 发布道 fast/slow
	Stage    string        `json:"stage,omitempty"`     // 发布结果阶段
	Diff     *BundleDiff   `json:"diff,omitempty"`
	Active   *bundleInfo   `json:"active,omitempty"`
	History  []*bundleInfo `json:"history,omitempty"`
}

// NewRefineTool 构建 refine 工具（agent 自我修改通道，无 activate op）。
func NewRefineTool(store *BundleStore, rm *ReleaseManager) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args refineArgs) (refineResult, error) {
			switch args.Op {
			case "propose":
				return refinePropose(ctx, store, rm, args)
			case "diff":
				return refineDiff(store, args)
			case "status":
				return refineStatus(store)
			case "rollback":
				return refineRollback(store, args)
			default:
				return refineResult{Op: args.Op, OK: false},
					fmt.Errorf("未知 op %q（白名单：propose/diff/status/rollback；无 activate——激活必经发布状态机）", args.Op)
			}
		},
		function.WithName("refine"),
		function.WithDescription("有界自治的自我改进通道（无直接生效权）。op：① propose——提案修改提示词"+
			"(prompt_key+content+note)，提交发布状态机按风险分级裁决（快道后验评估/慢道门后生效），"+
			"agent 不能直接激活；② diff——对比 active 与 target_id 版本差异；③ status——查看当前 active 与发布历史；"+
			"④ rollback——回退到历史已验证版本(target_id)。bundle v1 只治理 prompts/params/model，不含工具集。"),
	)
}

func refinePropose(ctx context.Context, store *BundleStore, rm *ReleaseManager, args refineArgs) (refineResult, error) {
	if args.PromptKey == "" || args.Content == "" {
		return refineResult{Op: "propose", OK: false}, fmt.Errorf("propose 需 prompt_key 与 content")
	}
	active := store.Active()
	if active == nil {
		return refineResult{Op: "propose", OK: false}, fmt.Errorf("无 active 基线，无法提案（应先初始化基线 bundle）")
	}
	// 从 active 派生 draft：复制 prompts，覆盖目标 key（字段白名单：仅 prompts）。
	prompts := make(map[string]string, len(active.Prompts))
	for k, v := range active.Prompts {
		prompts[k] = v
	}
	prompts[args.PromptKey] = args.Content
	draft, err := store.Create(active, prompts, active.Params, active.Model, "refine", args.Note)
	if err != nil {
		return refineResult{Op: "propose", OK: false}, fmt.Errorf("创建 draft bundle 失败: %w", err)
	}
	// 提交发布状态机——激活与否由风险分级发布道裁决（agent 无直接激活权）。
	if rm == nil {
		return refineResult{Op: "propose", OK: true, BundleID: draft.ID, Stage: string(StageDraft),
			Message: "draft 已创建（无发布状态机，停留 draft；激活需 ReleaseManager）"}, nil
	}
	rec, err := rm.Submit(ctx, draft)
	if err != nil {
		return refineResult{Op: "propose", OK: false, BundleID: draft.ID}, fmt.Errorf("发布状态机失败: %w", err)
	}
	return refineResult{
		Op: "propose", OK: rec.Stage == StageActive, BundleID: draft.ID,
		Lane: string(rec.Lane), Stage: string(rec.Stage),
		Message: fmt.Sprintf("提案经%s道裁决：%s（%s）", rec.Lane, rec.Stage, rec.Reason),
	}, nil
}

func refineDiff(store *BundleStore, args refineArgs) (refineResult, error) {
	active := store.Active()
	if active == nil {
		return refineResult{Op: "diff", OK: false}, fmt.Errorf("无 active 基线")
	}
	target := active
	if args.TargetID != "" {
		b, err := store.Get(args.TargetID)
		if err != nil {
			return refineResult{Op: "diff", OK: false}, err
		}
		target = b
	}
	d := Diff(active, target)
	return refineResult{Op: "diff", OK: true, Diff: &d,
		Message: fmt.Sprintf("active(%s) → target(%s) 差异", shortID(active.ID), shortID(target.ID))}, nil
}

func refineStatus(store *BundleStore) (refineResult, error) {
	res := refineResult{Op: "status", OK: true}
	if a := store.Active(); a != nil {
		res.Active = toBundleInfo(a, true)
	}
	all, err := store.List()
	if err != nil {
		return res, nil
	}
	activeID := ""
	if res.Active != nil {
		activeID = res.Active.ID
	}
	for _, b := range all {
		res.History = append(res.History, toBundleInfo(b, b.ID == activeID))
	}
	res.Message = fmt.Sprintf("active=%s，历史 %d 个 bundle", shortID(activeID), len(res.History))
	return res, nil
}

func refineRollback(store *BundleStore, args refineArgs) (refineResult, error) {
	if args.TargetID == "" {
		return refineResult{Op: "rollback", OK: false}, fmt.Errorf("rollback 需 target_id")
	}
	if _, err := store.Get(args.TargetID); err != nil {
		return refineResult{Op: "rollback", OK: false}, err
	}
	if err := store.Rollback(args.TargetID); err != nil {
		return refineResult{Op: "rollback", OK: false}, fmt.Errorf("回滚失败: %w", err)
	}
	return refineResult{Op: "rollback", OK: true, BundleID: args.TargetID,
		Message: fmt.Sprintf("已回滚到 %s（下一回合边界生效）", shortID(args.TargetID))}, nil
}

func toBundleInfo(b *Bundle, active bool) *bundleInfo {
	keys := make([]string, 0, len(b.Prompts))
	for k := range b.Prompts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	model := b.Model.Name
	if b.Model.Provider != "" {
		model = b.Model.Provider + "/" + b.Model.Name
	}
	return &bundleInfo{
		ID: b.ID, ParentID: b.ParentID, Note: b.Note, CreatedBy: b.CreatedBy,
		PromptKeys: keys, Model: model, Active: active,
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

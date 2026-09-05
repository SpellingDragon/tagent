// recall: the UNIFIED recall entry (stable-context-compaction D7).
//
// One tool — parameters ARE the router. Deterministic shapes never touch an
// LLM:
//
//	orchestrate: true     → explicit opt-in for the RecallAgent LLM
//	                        orchestration engine (checked first); when the
//	                        engine is not wired it returns explicit guidance,
//	                        never silently falling back to a deterministic path
//	items: [{key, hint?}] → engineering recall: batch GetEvent, original
//	                        order, zero hallucination, misses reported
//	turn_key              → causal-chain turn reconstruction: walk back to
//	                        the turn's external_input (recovers HOW a past
//	                        task was executed, incl. compressed tool steps)
//	query + filters       → retrieval-layer recall: QueryOptions keyword
//	                        search (may evolve to vector; protocol unchanged)
//
// Supersedes the retired memory_recall / memory_turn tool names — the model
// side sees one tool; the output protocol ({key, type, summary, content,
// time} entries) is unchanged across shapes.
package recall

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// recallArgs is the unified recall input — the parameter shape selects the
// route.
type recallArgs struct {
	// Orchestrate is the explicit opt-in for LLM multi-hop orchestration.
	// Checked first: an explicit intent overrides the deterministic shapes.
	Orchestrate bool `json:"orchestrate,omitempty" jsonschema:"description=显式请求 LLM 多跳编排检索（未接线时返回指引）"`
	// Items are index-card tickets (highest deterministic precedence).
	Items []recallItem `json:"items,omitempty" jsonschema:"description=票据精确回补: 时间线/卡片行里的 [evt_…] hex key 列表，批量取回原文（优先用）"`
	// TurnKey is a boundary event key (usually an agent_output card) for
	// causal-chain turn reconstruction.
	TurnKey  string `json:"turn_key,omitempty" jsonschema:"description=回合边界 key(通常是 agent_output 卡片的 [evt_…] hex)，沿因果链重建该轮完整执行过程"`
	MaxSteps int    `json:"max_steps,omitempty" jsonschema:"description=turn_key 形态下回走的最大步数(默认 20,上限 50)"`
	// Query + filters for semantic recall (used when no items/turn_key).
	Query      string   `json:"query,omitempty" jsonschema:"description=关键词子串匹配(命中任一词即召回): 用 1~3 个关键词勿整句提问; 回顾近期对话类请求优先用 since/until 时间范围而非关键词"`
	EventTypes []string `json:"event_types,omitempty" jsonschema:"description=按事件类型过滤(可选): external_input/agent_output/thinking_plan/action_command 等"`
	Since      int64    `json:"since,omitempty" jsonschema:"description=时间范围起点(Unix 毫秒,可选): 纯工程召回,不需关键词即可拉取该时间范围的事件"`
	Until      int64    `json:"until,omitempty" jsonschema:"description=时间范围终点(Unix 毫秒,可选): 纯工程召回,与 since 搭配或单用"`
	Limit      int      `json:"limit,omitempty" jsonschema:"description=返回条数上限(默认 10);按时间新→旧返回"`
}

// NewRecallTool creates the unified recall entry (deterministic shapes are
// pure functions; orchestrate is the explicit LLM-orchestration opt-in).
func NewRecallTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args recallArgs) (memoryRecallResult, error) {
			switch {
			case args.Orchestrate:
				// Reserved orchestration form (D7): the RecallAgent engine is
				// not wired into this entry yet — report that honestly with
				// deterministic iteration guidance instead of silently
				// degrading to a single shape.
				return memoryRecallResult{
					Mode: "orchestrate",
					Message: "LLM 多跳编排引擎暂未接线本入口。请用确定性形态自行迭代完成多跳检索：" +
						"先 query 检索线索 → 用 items 按票据精确回补 → 用 turn_key 重建整轮执行过程。",
				}, nil
			case len(args.Items) > 0:
				return recallByItems(accessor, args.Items), nil
			case args.TurnKey != "":
				return recallByTurn(accessor, args)
			case args.Query != "" || args.Since > 0 || args.Until > 0 || len(args.EventTypes) > 0:
				return recallByQuery(ctx, accessor, readPartitionIDs, memoryRecallArgs{
					Query:      args.Query,
					EventTypes: args.EventTypes,
					Since:      args.Since,
					Until:      args.Until,
					Limit:      args.Limit,
				})
			default:
				return memoryRecallResult{}, fmt.Errorf("provide items (index-card tickets), turn_key (causal-chain turn), or query/filters (semantic search); orchestrate=true opts into LLM orchestration")
			}
		},
		function.WithName("recall"),
		function.WithDescription("统一记忆召回（单入口，参数即路由；确定性形态零 LLM）。四种形态："+
			"① items=[{key,hint?}] —— 手里有索引卡/时间线 [evt_...] hex key 时按 key 批量精确回补原文（未命中明确标注 miss）；"+
			"② turn_key —— 给一个回合边界 key（通常是 agent_output 卡片），沿因果链回走重建该轮完整执行过程（含被压缩丢弃的工具步骤）；"+
			"③ query/since/until/event_types/limit —— 检索形态：**回顾近期对话/历史类请求优先用 since/until 时间范围（纯工程拉取，无需猜关键词）**；主题检索才用 query 关键词（1~3 个词，任一命中即召回）；"+
			"④ orchestrate=true —— 显式请求 LLM 多跳编排（未接线时返回指引，不静默降级）。"+
			"同时提供多形态时优先级：orchestrate > items > turn_key > query。"),
	)
}

// recallByTurn: causal-chain turn reconstruction via the shared walker,
// converted to the unified entry protocol.
func recallByTurn(accessor tagenttool.MemoryStoreAccessor, args recallArgs) (memoryRecallResult, error) {
	maxSteps := args.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 20
	}
	if maxSteps > 50 {
		maxSteps = 50
	}
	startKey, err := tagentevent.ParseEventKey(args.TurnKey)
	if err != nil || startKey == 0 {
		return memoryRecallResult{}, fmt.Errorf("turn_key must be a valid hex event key")
	}
	chain, complete, capped, werr := walkTurnChain(accessor, startKey, maxSteps)
	if werr != nil {
		return memoryRecallResult{}, werr
	}
	res := memoryRecallResult{Mode: "turn"}
	for _, it := range chain {
		res.Entries = append(res.Entries, memoryRecallEntry{
			Key:     it.Key,
			Type:    it.Type,
			Summary: it.Summary,
			Content: it.Content,
			Time:    it.Time,
		})
	}
	res.Count = len(res.Entries)
	if !complete {
		if capped {
			res.Message = "causal chain capped at max_steps before reaching external_input; retry with a larger max_steps"
		} else {
			res.Message = "causal chain incomplete (broke before reaching the turn's external_input)"
		}
	}
	return res, nil
}

// recallFactory wires the unified recall entry from the plain-tool config.
func recallFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("recall requires MemStore")
	}
	return NewRecallTool(cfg.MemStore, cfg.ReadPartitionIDs).(tool.CallableTool), nil
}

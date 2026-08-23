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
	Orchestrate bool `json:"orchestrate,omitempty"`
	// Items are index-card tickets (highest deterministic precedence).
	Items []recallItem `json:"items,omitempty"`
	// TurnKey is a boundary event key (usually an agent_output card) for
	// causal-chain turn reconstruction.
	TurnKey  string `json:"turn_key,omitempty"`
	MaxSteps int    `json:"max_steps,omitempty"`
	// Query + filters for semantic recall (used when no items/turn_key).
	Query      string   `json:"query,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`
	Since      int64    `json:"since,omitempty"`
	Until      int64    `json:"until,omitempty"`
	Limit      int      `json:"limit,omitempty"`
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
				return recallByQuery(accessor, readPartitionIDs, memoryRecallArgs{
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
			"③ query 加可选 event_types/since/until/limit —— 只有模糊线索时用关键词检索；"+
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

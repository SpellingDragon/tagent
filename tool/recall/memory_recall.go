// memory_recall: the recall PROTOCOL implementation (unified-memory-curation
// D6), now internal — the model-facing entry is the unified `recall` tool
// (recall.go, stable-context-compaction D7) which routes items/query through
// recallByItems/recallByQuery below.
//
// Index cards are recall tickets. PURE FUNCTION paths — no LLM in the
// deterministic route. Input-shape dispatch (items take precedence):
//
//	items: [{key, hint?}]  → engineering recall: batch GetEvent, original
//	                          order, zero hallucination, misses reported
//	query + filters        → semantic recall: QueryOptions keyword search
//	                          (the retrieval layer may evolve independently —
//	                          keyword → vector — the entry protocol stays)
package recall

import (
	"context"
	"fmt"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// memoryRecallArgs is the unified recall protocol input.
type memoryRecallArgs struct {
	// Items are index-card tickets: canonical hex keys (as shown in [evt_...]
	// prefixes and index cards), each with an optional hint echoed back for
	// reconciliation. When present, items take precedence over query.
	Items []recallItem `json:"items,omitempty"`
	// Query is a free-text semantic recall (keyword match on
	// EventSummary/Content, case-insensitive) used when no items are given.
	Query string `json:"query,omitempty"`
	// Filters for query mode.
	EventTypes []string `json:"event_types,omitempty"`
	Since      int64    `json:"since,omitempty"`
	Until      int64    `json:"until,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

type recallItem struct {
	// Key is the canonical hex event key (ticket from an index card).
	Key string `json:"key"`
	// Hint is the card-line description, echoed back verbatim.
	Hint string `json:"hint,omitempty"`
}

// memoryRecallEntry is the unified output protocol entry.
type memoryRecallEntry struct {
	Key     string `json:"key"`
	Hint    string `json:"hint,omitempty"`
	Type    string `json:"type,omitempty"`
	Summary string `json:"summary,omitempty"`
	Content string `json:"content,omitempty"`
	Time    string `json:"time,omitempty"`
	// Miss marks a ticket whose key was not found — reported explicitly,
	// never silently omitted (the model must not believe it "got" it).
	Miss bool `json:"miss,omitempty"`
}

type memoryRecallResult struct {
	Mode    string              `json:"mode"` // "items" or "query"
	Entries []memoryRecallEntry `json:"entries"`
	Count   int                 `json:"count"`
	Misses  int                 `json:"misses,omitempty"`
	// Message carries the honest-truncation hint for query mode (empty when
	// results did not hit the limit).
	Message string `json:"message,omitempty"`
}

// NewMemoryRecallTool creates the protocol recall tool (pure function).
func NewMemoryRecallTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args memoryRecallArgs) (memoryRecallResult, error) {
			// Input-shape dispatch: items (tickets) take precedence.
			if len(args.Items) > 0 {
				return recallByItems(accessor, args.Items), nil
			}
			if args.Query != "" || args.Since > 0 || args.Until > 0 || len(args.EventTypes) > 0 {
				return recallByQuery(ctx, accessor, readPartitionIDs, args)
			}
			return memoryRecallResult{}, fmt.Errorf("provide items (index-card keys) for precise recall, or query/filters for semantic recall")
		},
		function.WithName("memory_recall"),
		function.WithDescription("统一记忆召回（纯函数，无子 agent 绕行）。两种输入形态：① items=[{key,hint?}] —— 手里有索引卡/时间线上的 [evt_...] hex key 时用这个，按 key 批量精确回补原文（未命中会明确标注 miss）；② query 加可选 event_types/since/until/limit —— 只有模糊线索时用关键词检索（匹配摘要与内容）。items 与 query 同时提供时 items 优先。复杂多跳检索请用 recall agent。"),
	)
}

// recallByItems: engineering recall — batch precise readback, original order.
func recallByItems(accessor tagenttool.MemoryStoreAccessor, items []recallItem) memoryRecallResult {
	res := memoryRecallResult{Mode: "items"}
	for _, it := range items {
		entry := memoryRecallEntry{Key: it.Key, Hint: it.Hint}
		key, err := tagentevent.ParseEventKey(it.Key)
		if err != nil || key == 0 {
			entry.Miss = true
			res.Misses++
			res.Entries = append(res.Entries, entry)
			continue
		}
		evt, err := accessor.GetEvent(key)
		if err != nil || evt == nil {
			entry.Miss = true
			res.Misses++
			res.Entries = append(res.Entries, entry)
			continue
		}
		entry.Type = evt.EventType
		entry.Summary = evt.EventSummary
		entry.Content = evt.Content
		entry.Time = formatTimestamp(evt.Timestamp)
		res.Entries = append(res.Entries, entry)
	}
	res.Count = len(res.Entries)
	return res
}

// recallByQuery: semantic recall via the retrieval layer. T-A 起：accessor 暴露记忆
// 引擎时走 关键词∪向量 RRF 融合（闭环在引擎内），否则纯关键词——协议与工具声明
// 零变化（prefix-cache 不受影响）。
func recallByQuery(ctx context.Context, accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int, args memoryRecallArgs) (memoryRecallResult, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	// T-A hybrid 语义召回：查询词非空且引擎支持向量时优先走引擎融合；引擎报错或
	// 零命中 → 优雅降级到下方纯关键词路径（不报错，行为与现状一致）。
	if args.Query != "" {
		if ep, ok := accessor.(memory.MemoryEngineProvider); ok {
			if eng := ep.MemoryEngine(); eng != nil && eng.Capabilities().Vector {
				if res, ok := recallViaEngine(ctx, eng, accessor, readPartitionIDs, args, limit); ok {
					return res, nil
				}
			}
		}
	}
	opts := memory.QueryOptions{
		Limit:   limit,
		OrderBy: "timestamp_desc",
		Keyword: args.Query,
	}
	if len(readPartitionIDs) > 0 {
		opts.PartitionIDs = readPartitionIDs
	}
	if len(args.EventTypes) > 0 {
		opts.EventTypes = args.EventTypes
	}
	if args.Since > 0 {
		opts.StartTime = args.Since
	}
	if args.Until > 0 {
		opts.EndTime = args.Until
	}
	events, err := accessor.QueryEvents(opts)
	if err != nil {
		return memoryRecallResult{}, fmt.Errorf("memory query failed: %w", err)
	}
	res := memoryRecallResult{Mode: "query"}
	for _, evt := range events {
		res.Entries = append(res.Entries, memoryRecallEntry{
			Key:     tagentevent.FormatEventKey(evt.EventKey),
			Type:    evt.EventType,
			Summary: evt.EventSummary,
			Time:    formatTimestamp(evt.Timestamp),
		})
	}
	res.Count = len(res.Entries)
	res.Message = strings.TrimPrefix(truncationHint(res.Count, limit), "; ")
	// Zero-result honesty: a bare empty list invites the wrong conclusion that
	// the backend holds no history at all (observed in production). State what
	// WAS searched and steer toward the deterministic shapes.
	if res.Count == 0 {
		res.Message = "无可读分区内的匹配事件：已检索本 agent 可读命名空间（自身 + read_namespaces）。" +
			"query 是关键词子串匹配——请改用 1~3 个更短的关键词（勿整句提问）或加时间范围重试；若时间线里有 [evt_…] 票据，用 items 按票据精确回补更可靠"
	}
	return res, nil
}

// recallViaEngine 经记忆引擎做 hybrid 检索并水合为统一协议条目（T-A）。
// 返回 ok=false 表示应降级到纯关键词路径（引擎报错、零命中或全部悬挂）。
// 两段式保持：引擎只返回排序票据（EventKey），摘要/类型经 accessor.GetEvent 水合，
// 悬挂/已删命中在水合时自然消失（零幻觉）。
func recallViaEngine(ctx context.Context, eng memory.MemoryEngine, accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int, args memoryRecallArgs, limit int) (memoryRecallResult, bool) {
	hits, err := eng.Retrieve(ctx, memory.RetrievalQuery{
		Query:        args.Query,
		PartitionIDs: readPartitionIDs,
		EventTypes:   args.EventTypes,
		StartTime:    args.Since,
		EndTime:      args.Until,
		Limit:        limit,
		Mode:         memory.ModeAuto,
	})
	if err != nil {
		log.Warnf("[recall] engine retrieve failed, degrading to keyword: %v", err)
		return memoryRecallResult{}, false
	}
	if len(hits) == 0 {
		return memoryRecallResult{}, false
	}
	res := memoryRecallResult{Mode: "query"}
	for _, h := range hits {
		evt, err := accessor.GetEvent(h.EventKey)
		if err != nil || evt == nil {
			continue // 水合过滤：悬挂/已删向量命中自然消失
		}
		res.Entries = append(res.Entries, memoryRecallEntry{
			Key:     tagentevent.FormatEventKey(evt.EventKey),
			Type:    evt.EventType,
			Summary: evt.EventSummary,
			Time:    formatTimestamp(evt.Timestamp),
		})
	}
	res.Count = len(res.Entries)
	if res.Count == 0 {
		return memoryRecallResult{}, false // 全部悬挂 → 降级关键词
	}
	res.Message = strings.TrimPrefix(truncationHint(res.Count, limit), "; ")
	return res, true
}

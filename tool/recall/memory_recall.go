// memory_recall: the recall protocol implementation, internal to the unified recall entry.
//
// - Pure deterministic paths with no LLM in the route; items take precedence over query.
// - Items resolve by batch GetEvent in original order; misses are reported, never hallucinated.
package recall

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
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
	// EventTypes Filters for query mode.
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
	Mode    string              `json:"mode"`
	Entries []memoryRecallEntry `json:"entries"`
	Count   int                 `json:"count"`
	Misses  int                 `json:"misses,omitempty"`
	// Message carries the honest-truncation hint for query mode (empty when
	// results did not hit the limit).
	Message string `json:"message,omitempty"`
}

// NewMemoryRecallTool 构造协议级召回工具（纯函数，无子 agent 绕行）：items 票据形态优先于 query 形态。
// 检索分层、降级与诚实回报的契约见文档。
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-contract
func NewMemoryRecallTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args memoryRecallArgs) (memoryRecallResult, error) {
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

// maxRecallItems 是 items 水合条数上界：超量部分被丢弃并在 Message 中报出，截断绝不静默。
const maxRecallItems = 50

// recallByItems 按给定顺序批量精确回补票据原文，未命中者逐条标 miss。
func recallByItems(accessor tagenttool.MemoryStoreAccessor, items []recallItem) memoryRecallResult {
	res := memoryRecallResult{Mode: "items"}
	if len(items) > maxRecallItems {
		dropped := len(items) - maxRecallItems
		res.Message = fmt.Sprintf(
			"items truncated: %d of %d tickets dropped (limit %d) — batch the rest into follow-up calls",
			dropped, len(items), maxRecallItems)
		items = items[:maxRecallItems]
	}
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

// recallTracerName 是 recall 内部路径 span 的 tracer 名；该 span 只携带元数据，查询内容零入 span。
const recallTracerName = "github.com/SpellingDragon/tagent/tool/recall"

// recallByQuery 走检索层：查询词非空且引擎支持向量时优先混合检索，引擎报错或零命中则降级到
// 纯关键词路径（不向调用方报错）。零结果必须回报检索范围与确定性下一步形态，见文档。
func recallByQuery(ctx context.Context, accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int, args memoryRecallArgs) (result memoryRecallResult, err error) {
	ctx, span := otel.Tracer(recallTracerName).Start(ctx, "tagent.recall.query")
	defer func() {
		span.SetAttributes(
			attribute.String("recall.mode", result.Mode),
			attribute.Int("recall.query_len", len(args.Query)),
			attribute.Int("recall.partitions", len(readPartitionIDs)),
			attribute.Int("recall.hits", len(result.Entries)),
		)
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
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
	if res.Count == 0 {
		res.Message = "无可读分区内的匹配事件：已检索本 agent 可读命名空间（自身 + read_namespaces）。" +
			"query 是关键词子串匹配——请改用 1~3 个更短的关键词（勿整句提问）或加时间范围重试；若时间线里有 [evt_…] 票据，用 items 按票据精确回补更可靠"
	}
	return res, nil
}

// maxRecallLimit 钳制模型入参 limit，防止单次召回放大为大批水合。
const maxRecallLimit = 100

// recallViaEngine 经引擎做混合检索并水合为统一协议条目。返回 ok=false 表示应降级到关键词路径
// （引擎报错、零命中或水合后全部悬挂）。引擎只给排序票据、全文另取水合的两段式使悬挂与墓碑命中
// 自然消失；引擎按 limit 的两倍返候选，过滤后再裁到 limit，避免死键占据 topK 造成静默少返回。
func recallViaEngine(ctx context.Context, eng memory.MemoryEngine, accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int, args memoryRecallArgs, limit int) (memoryRecallResult, bool) {
	if limit > maxRecallLimit {
		limit = maxRecallLimit
	}
	hits, err := eng.Retrieve(ctx, memory.RetrievalQuery{
		Query:        args.Query,
		PartitionIDs: readPartitionIDs,
		EventTypes:   args.EventTypes,
		StartTime:    args.Since,
		EndTime:      args.Until,
		Limit:        limit * 2,
		Mode:         memory.ModeAuto,
	})
	if err != nil {
		log.Warnf("[recall] engine retrieve failed, degrading to keyword: %v", err)
		return memoryRecallResult{}, false
	}
	if len(hits) == 0 {
		return memoryRecallResult{}, false
	}
	keys := make([]int64, 0, len(hits))
	for _, h := range hits {
		if partitionAllowed(memory.PartitionIDFromEventKey(h.EventKey), readPartitionIDs) {
			keys = append(keys, h.EventKey)
		}
	}
	if len(keys) == 0 {
		return memoryRecallResult{}, false
	}
	events := hydrateKeys(accessor, keys)
	res := memoryRecallResult{Mode: "query"}
	for i := range events {
		evt := &events[i]
		res.Entries = append(res.Entries, memoryRecallEntry{
			Key:     tagentevent.FormatEventKey(evt.EventKey),
			Type:    evt.EventType,
			Summary: evt.EventSummary,
			Time:    formatTimestamp(evt.Timestamp),
		})
		if len(res.Entries) >= limit {
			break
		}
	}
	res.Count = len(res.Entries)
	if res.Count == 0 {
		return memoryRecallResult{}, false
	}
	res.Message = strings.TrimPrefix(truncationHint(res.Count, limit), "; ")
	return res, true
}

// partitionAllowed 报告 pid 是否在白名单（空白名单表示不限）。水合前过滤是分区泄漏的第二道防线。
func partitionAllowed(pid int, whitelist []int) bool {
	if len(whitelist) == 0 {
		return true
	}
	for _, p := range whitelist {
		if p == pid {
			return true
		}
	}
	return false
}

// hydrateKeys 批量取回事件全文，保持入参顺序、跳过缺失（悬挂与墓碑）。优先一次批量取回
// （文件段存储后端下避免逐键子进程）；不支持批量的 accessor 退化为逐键取回。
func hydrateKeys(accessor tagenttool.MemoryStoreAccessor, keys []int64) []memory.FullEvent {
	if batcher, ok := accessor.(interface {
		GetEvents([]int64) ([]memory.FullEvent, error)
	}); ok {
		if events, err := batcher.GetEvents(keys); err == nil {
			return events
		}
	}
	out := make([]memory.FullEvent, 0, len(keys))
	for _, k := range keys {
		if evt, err := accessor.GetEvent(k); err == nil && evt != nil {
			out = append(out, *evt)
		}
	}
	return out
}

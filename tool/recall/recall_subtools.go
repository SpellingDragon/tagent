package recall

import (
	"context"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/SpellingDragon/tagent/agent"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// NewRecallQueryTool creates a tool that queries historical events from memory.
// This is a sub-tool used by RecallAgent for memory retrieval.
// readPartitionIDs lists additional partition IDs to include in queries (injected from config).
func NewRecallQueryTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args recallQueryArgs) (recallQueryResult, error) {
			limit := args.Limit
			if limit <= 0 {
				limit = 10
			}

			opts := memory.QueryOptions{
				Limit:   limit,
				OrderBy: "timestamp_desc", // Latest first
			}

			if len(readPartitionIDs) > 0 {
				opts.PartitionIDs = readPartitionIDs
			}

			if len(args.EventTypes) > 0 {
				opts.EventTypes = args.EventTypes
			}

			// Keyword filter: search EventSummary and Content (case-insensitive)
			if args.Keyword != "" {
				opts.Keyword = args.Keyword
			}

			// Time range filtering
			if args.Since > 0 {
				opts.StartTime = args.Since
			}
			if args.Until > 0 {
				opts.EndTime = args.Until
			}
			if args.Since > 0 && args.Until > 0 && args.Since > args.Until {
				return recallQueryResult{}, fmt.Errorf("invalid time range: since (%d) > until (%d)", args.Since, args.Until)
			}

			events, err := accessor.QueryEvents(opts)
			if err != nil {
				return recallQueryResult{}, fmt.Errorf("memory query failed: %w", err)
			}

			var results []recallEventItem
			for _, evt := range events {
				results = append(results, recallEventItem{
					Key:     tagentevent.FormatEventKey(evt.EventKey),
					Type:    evt.EventType,
					Summary: evt.EventSummary,
					Time:    formatTimestamp(evt.Timestamp),
				})
			}

			return recallQueryResult{
				Events:  results,
				Count:   len(results),
				Message: fmt.Sprintf("found %d events", len(results)) + truncationHint(len(results), limit),
			}, nil
		},
		function.WithName("memory_query"),
		function.WithDescription("Query historical events from memory storage. Supports time range filtering via since/until (Unix ms timestamps), keyword search via keyword (case-insensitive match on EventSummary/Content), and event type filtering. Returns event list sorted by time (newest first)."),
	)
}

// NewRecallGetTool creates a tool that retrieves full event details by key.
// This is a sub-tool used by RecallAgent for detailed memory retrieval.
func NewRecallGetTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args recallGetArgs) (recallGetResult, error) {
			key, err := tagentevent.ParseEventKey(args.Key)
			if args.Key == "" || err != nil {
				return recallGetResult{}, fmt.Errorf("event key is required (hex string, e.g. 1201a3f4b5c6d7e8)")
			}

			evt, err := accessor.GetEvent(key)
			if err != nil {
				return recallGetResult{}, fmt.Errorf("event not found: %w", err)
			}

			result := recallGetResult{
				Key:     tagentevent.FormatEventKey(evt.EventKey),
				Type:    evt.EventType,
				Summary: evt.EventSummary,
				Content: evt.Content,
				Time:    formatTimestamp(evt.Timestamp),
			}

			// Get parent key from RelationStore (content-relation separation)
			var parentKey int64
			if rsp, ok := accessor.(memory.RelationStoreProvider); ok {
				pk, err := rsp.RelationStore().GetParent(key)
				if err != nil {
					log.Errorf("[Recall] GetParent failed key=%d: %v", key, err)
				}
				parentKey = pk
			}
			if parentKey != 0 {
				result.ParentKey = tagentevent.FormatEventKey(parentKey)
			}

			// Optionally include parent event summary
			if args.IncludeParent && parentKey != 0 {
				if parent, err := accessor.GetEvent(parentKey); err == nil && parent != nil {
					result.Parent = &parentEventInfo{
						EventKey:  tagentevent.FormatEventKey(parent.EventKey),
						EventType: parent.EventType,
						Summary:   parent.EventSummary,
						Time:      formatTimestamp(parent.Timestamp),
					}
				}
			}

			return result, nil
		},
		function.WithName("memory_get"),
		function.WithDescription("Get full details of a specific event by its key. Set include_parent=true to also include the parent event summary."),
	)
}

// NewRecallRecentTool creates a tool that retrieves the most recent events.
// This is a sub-tool used by RecallAgent for quick recent memory access.
// readPartitionIDs lists additional partition IDs to include in queries (injected from config).
func NewRecallRecentTool(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args recallRecentArgs) (recallRecentResult, error) {
			limit := args.Limit
			if limit <= 0 {
				limit = 5
			}
			if limit > 20 {
				limit = 20 // Cap at 20
			}

			opts := memory.QueryOptions{
				Limit:   limit,
				OrderBy: "timestamp_desc",
			}

			if len(readPartitionIDs) > 0 {
				opts.PartitionIDs = readPartitionIDs
			}

			// Time range filtering
			if args.Since > 0 {
				opts.StartTime = args.Since
			}
			if args.Until > 0 {
				opts.EndTime = args.Until
			}
			if args.Since > 0 && args.Until > 0 && args.Since > args.Until {
				return recallRecentResult{}, fmt.Errorf("invalid time range: since (%d) > until (%d)", args.Since, args.Until)
			}

			// Keyword filter
			if args.Keyword != "" {
				opts.Keyword = args.Keyword
			}

			events, err := accessor.QueryEvents(opts)
			if err != nil {
				return recallRecentResult{}, fmt.Errorf("failed to get recent events: %w", err)
			}

			var results []recallEventItem
			for _, evt := range events {
				results = append(results, recallEventItem{
					Key:     tagentevent.FormatEventKey(evt.EventKey),
					Type:    evt.EventType,
					Summary: evt.EventSummary,
					Time:    formatTimestamp(evt.Timestamp),
				})
			}

			return recallRecentResult{
				Events:  results,
				Count:   len(results),
				Message: strings.TrimPrefix(truncationHint(len(results), limit), "; "),
			}, nil
		},
		function.WithName("memory_recent"),
		function.WithDescription("Get the most recent events from memory. Supports time range filtering via since/until (Unix ms timestamps) and keyword search via keyword (case-insensitive match on EventSummary/Content)."),
	)
}

// ==================== Recall Sub-tool Argument/Result Types ====================

// recallQueryArgs represents a recall query request.
type recallQueryArgs struct {
	// Natural language query describing what to recall
	Query string `json:"query"`
	// Filter by event types (optional)
	EventTypes []string `json:"event_types,omitempty"`
	// Filter start time (Unix ms timestamp, optional)
	Since int64 `json:"since,omitempty"`
	// Filter end time (Unix ms timestamp, optional)
	Until int64 `json:"until,omitempty"`
	// Keyword filter: case-insensitive match on EventSummary and Content (optional)
	Keyword string `json:"keyword,omitempty"`
	// Maximum number of results (default: 10)
	Limit int `json:"limit,omitempty"`
}

// recallQueryResult represents the result of a recall query.
type recallQueryResult struct {
	Events  []recallEventItem `json:"events"`
	Count   int               `json:"count"`
	Message string            `json:"message"`
}

type recallGetArgs struct {
	// Event key to retrieve (canonical hex string, as shown in [evt_...] prefixes)
	Key string `json:"key"`
	// If true, include parent event summary in result (optional)
	IncludeParent bool `json:"include_parent,omitempty"`
}

type recallGetResult struct {
	Key       string           `json:"key"`
	ParentKey string           `json:"parent_key,omitempty"`
	Type      string           `json:"type"`
	Summary   string           `json:"summary"`
	Content   string           `json:"content"`
	Time      string           `json:"time"`
	Parent    *parentEventInfo `json:"parent,omitempty"`
}

type recallRecentArgs struct {
	// Number of recent events to retrieve (default: 5, max: 20)
	Limit int `json:"limit,omitempty"`
	// Filter start time (Unix ms timestamp, optional)
	Since int64 `json:"since,omitempty"`
	// Filter end time (Unix ms timestamp, optional)
	Until int64 `json:"until,omitempty"`
	// Keyword filter: case-insensitive match on EventSummary and Content (optional)
	Keyword string `json:"keyword,omitempty"`
}

type recallRecentResult struct {
	Events  []recallEventItem `json:"events"`
	Count   int               `json:"count"`
	Message string            `json:"message,omitempty"`
}

// truncationHint returns an honest-truncation notice when the result count
// hit the limit — without it the LLM reads "returned N" as "only N exist"
// and stops looking (the exact failure mode of the 2026-07-31 meditation
// recall incident). Heuristic: count == limit may rarely equal the full set;
// the false "maybe more" then just triggers one harmless narrowed retry.
func truncationHint(count, limit int) string {
	if limit > 0 && count == limit {
		return "; 已达 limit，更旧的匹配未返回——可缩小时间范围、加关键词或增大 limit 继续查询"
	}
	return ""
}

// recallEventItem is a shared event item format for sub-tool results.
// Key is the canonical hex string form (see tagentevent.FormatEventKey).
type recallEventItem struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Time    string `json:"time"`
}

// parentEventInfo is a lightweight parent event reference included on request.
type parentEventInfo struct {
	EventKey  string `json:"event_key"`
	EventType string `json:"event_type"`
	Summary   string `json:"summary"`
	Time      string `json:"time"`
}

// recallTraceArgs represents a causal chain trace request.
type recallTraceArgs struct {
	// Event key to start tracing from (canonical hex string)
	Key string `json:"key"`
	// Maximum steps to trace backward (default: 10, max: 20)
	MaxSteps int `json:"max_steps,omitempty"`
}

// recallTraceResult represents the result of a causal chain trace.
type recallTraceResult struct {
	Events []recallTraceItem `json:"events"`
	Count  int               `json:"count"`
	Capped bool              `json:"capped"`
}

// recallTraceItem is a single event in a causal chain trace.
type recallTraceItem struct {
	Key       string `json:"key"`
	ParentKey string `json:"parent_key,omitempty"`
	Type      string `json:"type"`
	Summary   string `json:"summary"`
	Time      string `json:"time"`
}

// memoryTurnArgs reconstructs a task turn's execution process.
type memoryTurnArgs struct {
	// Boundary event key to start from (canonical hex; usually an agent_output card key).
	Key string `json:"key"`
	// Maximum steps to walk backward (default: 20, max: 50).
	MaxSteps int `json:"max_steps,omitempty"`
}

// memoryTurnResult is a reconstructed task turn (oldest → newest).
type memoryTurnResult struct {
	Events []memoryTurnItem `json:"events"`
	Count  int              `json:"count"`
	// Complete is true when the walk reached the turn's external_input (turn start).
	Complete bool `json:"complete"`
	// Capped is true when MaxSteps was hit before reaching external_input.
	Capped bool `json:"capped"`
}

// memoryTurnItem is one event in a reconstructed turn. Content carries the
// execution detail for tool steps (thinking_plan/action_command) so the model
// can recover HOW a past task was executed.
type memoryTurnItem struct {
	Key     string `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Content string `json:"content,omitempty"`
	Time    string `json:"time"`
}

// formatTimestamp formats a Unix timestamp (milliseconds) to readable string.
func formatTimestamp(ts int64) string {
	if ts == 0 {
		return "unknown"
	}
	t := time.UnixMilli(ts)
	return t.Format("2006-01-02 15:04:05")
}

// NewRecallTraceTool creates a tool that traces the causal chain backward from an event.
// Traverses ParentKey links by repeatedly calling GetEvent(parentKey).
func NewRecallTraceTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args recallTraceArgs) (recallTraceResult, error) {
			maxSteps := args.MaxSteps
			if maxSteps <= 0 {
				maxSteps = 10
			}
			if maxSteps > 20 {
				maxSteps = 20
			}

			var chain []recallTraceItem
			currentKey, err := tagentevent.ParseEventKey(args.Key)
			if args.Key == "" || err != nil {
				return recallTraceResult{}, fmt.Errorf("event key is required (hex string)")
			}

			for step := 0; step < maxSteps && currentKey != 0; step++ {
				evt, err := accessor.GetEvent(currentKey)
				if err != nil {
					if step == 0 {
						return recallTraceResult{}, fmt.Errorf("event not found: %s", tagentevent.FormatEventKey(currentKey))
					}
					// Chain breaks at missing link
					break
				}

				// Get parent from RelationStore (content-relation separation)
				var parentKey int64
				if rsp, ok := accessor.(memory.RelationStoreProvider); ok {
					pk, err := rsp.RelationStore().GetParent(evt.EventKey)
					if err != nil {
						log.Errorf("[Recall] GetParent failed key=%d: %v", evt.EventKey, err)
					}
					parentKey = pk
				}
				item := recallTraceItem{
					Key:     tagentevent.FormatEventKey(evt.EventKey),
					Type:    evt.EventType,
					Summary: evt.EventSummary,
					Time:    formatTimestamp(evt.Timestamp),
				}
				if parentKey != 0 {
					item.ParentKey = tagentevent.FormatEventKey(parentKey)
				}
				chain = append(chain, item)
				currentKey = parentKey
			}

			return recallTraceResult{
				Events: chain,
				Count:  len(chain),
				Capped: len(chain) >= maxSteps && currentKey != 0,
			}, nil
		},
		function.WithName("memory_trace"),
		function.WithDescription("Trace the causal chain backward from an event by following ParentKey links. Provide a starting event key. Returns events from newest to oldest."),
	)
}

// truncateTurnContent caps tool-step content to keep the reconstructed turn
// lightweight (rune-aware to avoid splitting a multi-byte character).
func truncateTurnContent(content string) string {
	const maxRunes = 500
	r := []rune(content)
	if len(r) <= maxRunes {
		return content
	}
	return string(r[:maxRunes]) + "…(截断)"
}

// NewMemoryTurnTool reconstructs a task turn's execution process (compress-
// digest-reconnect). Given a boundary event key (usually an agent_output
// card), it walks the causal chain backward via GetParent until the turn's
// external_input (inclusive), returning all events in the turn — including
// the thinking_plan/action_command steps that skeleton compression dropped
// from the timeline — in chronological order. This is how the model recovers
// HOW a past task was executed: the dropped tool events never leave the
// MemoryStore, and the causal chain (independent of compression) anchors them
// to the kept boundary cards. No forward traversal is needed — the backward
// walk from agent_output to external_input bounds exactly one turn.
func NewMemoryTurnTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args memoryTurnArgs) (memoryTurnResult, error) {
			maxSteps := args.MaxSteps
			if maxSteps <= 0 {
				maxSteps = 20
			}
			if maxSteps > 50 {
				maxSteps = 50
			}

			startKey, err := tagentevent.ParseEventKey(args.Key)
			if args.Key == "" || err != nil {
				return memoryTurnResult{}, fmt.Errorf("event key is required (hex string)")
			}

			var chain []memoryTurnItem // collected newest → oldest
			currentKey := startKey
			reachedInput := false
			for step := 0; step < maxSteps && currentKey != 0; step++ {
				evt, gerr := accessor.GetEvent(currentKey)
				if gerr != nil {
					if step == 0 {
						return memoryTurnResult{}, fmt.Errorf("event not found: %s", tagentevent.FormatEventKey(currentKey))
					}
					break // chain breaks at a missing link
				}
				chain = append(chain, memoryTurnItem{
					Key:     tagentevent.FormatEventKey(evt.EventKey),
					Type:    evt.EventType,
					Summary: truncateTurnContent(evt.EventSummary), // external_input summary = full text; cap it like Content
					Content: truncateTurnContent(evt.Content),
					Time:    formatTimestamp(evt.Timestamp),
				})
				// Stop after recording the turn's external_input (turn start).
				if evt.EventType == tagentevent.TypeExternalInput {
					reachedInput = true
					break
				}
				// Walk backward via the causal chain.
				var parentKey int64
				if rsp, ok := accessor.(memory.RelationStoreProvider); ok {
					pk, perr := rsp.RelationStore().GetParent(evt.EventKey)
					if perr != nil {
						log.Errorf("[Recall] memory_turn GetParent failed key=%d: %v", evt.EventKey, perr)
					}
					parentKey = pk
				}
				currentKey = parentKey
			}

			// Reverse to chronological order (oldest → newest).
			for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
				chain[i], chain[j] = chain[j], chain[i]
			}

			return memoryTurnResult{
				Events:   chain,
				Count:    len(chain),
				Complete: reachedInput,
				Capped:   len(chain) >= maxSteps && !reachedInput,
			}, nil
		},
		function.WithName("memory_turn"),
		function.WithDescription("Reconstruct a task turn's execution process. Provide a boundary event key (usually an agent_output card key). Walks the causal chain backward to the turn's external_input and returns all events in the turn (including thinking_plan/action_command steps dropped by compression), oldest to newest. Use this to recover HOW a past task was executed."),
	)
}

// buildRecallSubTools assembles the sub-tools for RecallAgent.
func buildRecallSubTools(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) []tool.Tool {
	var tools []tool.Tool

	tools = append(tools, NewRecallQueryTool(accessor, readPartitionIDs))
	tools = append(tools, NewRecallGetTool(accessor))
	tools = append(tools, NewRecallRecentTool(accessor, readPartitionIDs))
	tools = append(tools, NewRecallTraceTool(accessor))
	tools = append(tools, NewMemoryTurnTool(accessor))

	return tools
}

// ==================== Plain Tool Factory Registration ====================

// RegisterSubTools registers all recall sub-tools as plain tools in the
// global tool registry. Called by tagent.RegisterBuiltinTools().
//
// Registered tools:
//   - recall_query: query historical events with time range and keyword filtering
//   - recall_get: get full event details by key, optionally include parent event
//   - recall_recent: get the most recent events with optional time range filtering
//   - recall_trace: trace the causal chain backward from an event by following ParentKey links
//   - memory_turn: reconstruct a task turn's execution process (walk chain back to external_input)
//   - memory_recall: the protocol recall tool (items-ticket or query, pure function)
func RegisterSubTools() {
	agent.RegisterPlainTool("recall_query", recallQueryFactory)
	agent.RegisterPlainTool("recall_get", recallGetFactory)
	agent.RegisterPlainTool("recall_recent", recallRecentFactory)
	agent.RegisterPlainTool("recall_trace", recallTraceFactory)
	agent.RegisterPlainTool("memory_turn", memoryTurnFactory)
	// memory_recall: the protocol recall tool (top-level agent, pure function).
	agent.RegisterPlainTool("memory_recall", memoryRecallFactory)
}

func recallQueryFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("recall_query requires MemStore")
	}
	return NewRecallQueryTool(cfg.MemStore, cfg.ReadPartitionIDs).(tool.CallableTool), nil
}

func recallGetFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("recall_get requires MemStore")
	}
	return NewRecallGetTool(cfg.MemStore).(tool.CallableTool), nil
}

func recallRecentFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("recall_recent requires MemStore")
	}
	return NewRecallRecentTool(cfg.MemStore, cfg.ReadPartitionIDs).(tool.CallableTool), nil
}

func recallTraceFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("recall_trace requires MemStore")
	}
	return NewRecallTraceTool(cfg.MemStore).(tool.CallableTool), nil
}

func memoryTurnFactory(cfg agent.PlainToolFactoryConfig) (tool.CallableTool, error) {
	if cfg.MemStore == nil {
		return nil, fmt.Errorf("memory_turn requires MemStore")
	}
	return NewMemoryTurnTool(cfg.MemStore).(tool.CallableTool), nil
}

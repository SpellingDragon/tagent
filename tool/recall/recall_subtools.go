package recall

import (
	"context"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

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
					Key:     evt.EventKey,
					Type:    evt.EventType,
					Summary: evt.EventSummary,
					Time:    formatTimestamp(evt.Timestamp),
				})
			}

			return recallQueryResult{
				Events:  results,
				Count:   len(results),
				Message: fmt.Sprintf("found %d events", len(results)),
			}, nil
		},
		function.WithName("memory_query"),
		function.WithDescription("Query historical events from memory storage. Supports time range filtering via since/until (Unix ms timestamps). Returns event list sorted by time (newest first)."),
	)
}

// NewRecallGetTool creates a tool that retrieves full event details by key.
// This is a sub-tool used by RecallAgent for detailed memory retrieval.
func NewRecallGetTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool {
	return function.NewFunctionTool(
		func(ctx context.Context, args recallGetArgs) (recallGetResult, error) {
			if args.Key == 0 {
				return recallGetResult{}, fmt.Errorf("event key is required")
			}

			evt, err := accessor.GetEvent(args.Key)
			if err != nil {
				return recallGetResult{}, fmt.Errorf("event not found: %w", err)
			}

			result := recallGetResult{
				Key:       evt.EventKey,
				ParentKey: evt.ParentKey,
				Type:      evt.EventType,
				Summary:   evt.EventSummary,
				Content:   evt.Content,
				Time:      formatTimestamp(evt.Timestamp),
			}

			// Optionally include parent event summary
			if args.IncludeParent && evt.ParentKey != 0 {
				if parent, err := accessor.GetEvent(evt.ParentKey); err == nil && parent != nil {
					result.Parent = &parentEventInfo{
						EventKey:  parent.EventKey,
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

			events, err := accessor.QueryEvents(opts)
			if err != nil {
				return recallRecentResult{}, fmt.Errorf("failed to get recent events: %w", err)
			}

			var results []recallEventItem
			for _, evt := range events {
				results = append(results, recallEventItem{
					Key:     evt.EventKey,
					Type:    evt.EventType,
					Summary: evt.EventSummary,
					Time:    formatTimestamp(evt.Timestamp),
				})
			}

			return recallRecentResult{
				Events: results,
				Count:  len(results),
			}, nil
		},
		function.WithName("memory_recent"),
		function.WithDescription("Get the most recent events from memory. Supports time range filtering via since/until (Unix ms timestamps)."),
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
	// Event key to retrieve (Snowflake int64)
	Key int64 `json:"key"`
	// If true, include parent event summary in result (optional)
	IncludeParent bool `json:"include_parent,omitempty"`
}

type recallGetResult struct {
	Key       int64            `json:"key"`
	ParentKey int64            `json:"parent_key,omitempty"`
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
}

type recallRecentResult struct {
	Events []recallEventItem `json:"events"`
	Count  int               `json:"count"`
}

// recallEventItem is a shared event item format for sub-tool results.
type recallEventItem struct {
	Key     int64  `json:"key"`
	Type    string `json:"type"`
	Summary string `json:"summary"`
	Time    string `json:"time"`
}

// parentEventInfo is a lightweight parent event reference included on request.
type parentEventInfo struct {
	EventKey  int64  `json:"event_key"`
	EventType string `json:"event_type"`
	Summary   string `json:"summary"`
	Time      string `json:"time"`
}

// recallTraceArgs represents a causal chain trace request.
type recallTraceArgs struct {
	// Event key to start tracing from
	Key int64 `json:"key"`
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
	Key       int64  `json:"key"`
	ParentKey int64  `json:"parent_key,omitempty"`
	Type      string `json:"type"`
	Summary   string `json:"summary"`
	Time      string `json:"time"`
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
			if args.Key == 0 {
				return recallTraceResult{}, fmt.Errorf("event key is required")
			}

			maxSteps := args.MaxSteps
			if maxSteps <= 0 {
				maxSteps = 10
			}
			if maxSteps > 20 {
				maxSteps = 20
			}

			var chain []recallTraceItem
			currentKey := args.Key

			for step := 0; step < maxSteps && currentKey != 0; step++ {
				evt, err := accessor.GetEvent(currentKey)
				if err != nil {
					if step == 0 {
						return recallTraceResult{}, fmt.Errorf("event not found: %d", currentKey)
					}
					// Chain breaks at missing link
					break
				}
				chain = append(chain, recallTraceItem{
					Key:       evt.EventKey,
					ParentKey: evt.ParentKey,
					Type:      evt.EventType,
					Summary:   evt.EventSummary,
					Time:      formatTimestamp(evt.Timestamp),
				})
				currentKey = evt.ParentKey
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

// buildRecallSubTools assembles the sub-tools for RecallAgent.
func buildRecallSubTools(accessor tagenttool.MemoryStoreAccessor, readPartitionIDs []int) []tool.Tool {
	var tools []tool.Tool

	tools = append(tools, NewRecallQueryTool(accessor, readPartitionIDs))
	tools = append(tools, NewRecallGetTool(accessor))
	tools = append(tools, NewRecallRecentTool(accessor, readPartitionIDs))
	tools = append(tools, NewRecallTraceTool(accessor))

	return tools
}

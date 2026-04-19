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
func NewRecallQueryTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool {
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

			// If event types specified, filter by them
			if len(args.EventTypes) > 0 {
				opts.EventTypes = args.EventTypes
			}

			events, err := accessor.QueryEvents(opts)
			if err != nil {
				return recallQueryResult{}, fmt.Errorf("memory query failed: %w", err)
			}

			// Convert to result format
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
		function.WithDescription("Query historical events from memory storage. Returns event list sorted by time (newest first)."),
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

			return recallGetResult{
				Key:       evt.EventKey,
				ParentKey: evt.ParentKey,
				Type:      evt.EventType,
				Summary:   evt.EventSummary,
				Content:   evt.Content,
				Time:      formatTimestamp(evt.Timestamp),
			}, nil
		},
		function.WithName("memory_get"),
		function.WithDescription("Get full details of a specific event by its key."),
	)
}

// NewRecallRecentTool creates a tool that retrieves the most recent events.
// This is a sub-tool used by RecallAgent for quick recent memory access.
func NewRecallRecentTool(accessor tagenttool.MemoryStoreAccessor) tool.Tool {
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
		function.WithDescription("Get the most recent events from memory."),
	)
}

// ==================== Recall Sub-tool Argument/Result Types ====================

// recallQueryArgs represents a recall query request.
type recallQueryArgs struct {
	// Natural language query describing what to recall
	Query string `json:"query"`
	// Filter by event types (optional)
	EventTypes []string `json:"event_types,omitempty"`
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
}

type recallGetResult struct {
	Key       int64  `json:"key"`
	ParentKey int64  `json:"parent_key,omitempty"`
	Type      string `json:"type"`
	Summary   string `json:"summary"`
	Content   string `json:"content"`
	Time      string `json:"time"`
}

type recallRecentArgs struct {
	// Number of recent events to retrieve (default: 5, max: 20)
	Limit int `json:"limit,omitempty"`
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

// formatTimestamp formats a Unix timestamp (milliseconds) to readable string.
func formatTimestamp(ts int64) string {
	if ts == 0 {
		return "unknown"
	}
	t := time.UnixMilli(ts)
	return t.Format("2006-01-02 15:04:05")
}

// buildRecallSubTools assembles the sub-tools for RecallAgent.
func buildRecallSubTools(accessor tagenttool.MemoryStoreAccessor) []tool.Tool {
	var tools []tool.Tool

	tools = append(tools, NewRecallQueryTool(accessor))
	tools = append(tools, NewRecallGetTool(accessor))
	tools = append(tools, NewRecallRecentTool(accessor))

	return tools
}

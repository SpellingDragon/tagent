package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Verify RecallTool implements tool.CallableTool at compile time.
var _ tool.CallableTool = (*RecallTool)(nil)

// RecallTool retrieves relevant memories from past interactions.
//
// Abstract responsibility: "Intelligent memory recall"
//   - Query: search historical events by natural language
//   - Detail: get full event content by key
//   - The top-level Agent interprets and synthesizes recall results
//
// Design principle: simple CallableTool, no internal LLM loop.
// The top-level LLMAgent drives the React loop and decides how to use recall results.
// RecallTool provides access to memory; interpretation is the Agent's job.
//
// This is a deliberate design decision (from trpcclaw):
// RecallTool does NOT need React Agent because it has a single, clear function
// (query/retrieve events). The "thinking" about what the recalled events mean
// is the responsibility of the top-level Agent.
type RecallTool struct {
	memStore MemoryStoreAccessor
}

// RecallToolOption configures RecallTool.
type RecallToolOption func(*RecallTool)

// WithRecallMemoryStore sets the MemoryStoreAccessor.
func WithRecallMemoryStore(store MemoryStoreAccessor) RecallToolOption {
	return func(rt *RecallTool) {
		rt.memStore = store
	}
}

// NewRecallTool creates a new RecallTool.
func NewRecallTool(opts ...RecallToolOption) *RecallTool {
	rt := &RecallTool{}
	for _, opt := range opts {
		opt(rt)
	}
	return rt
}

// Declaration implements tool.CallableTool.
func (rt *RecallTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "recall",
		Description: "Recall memories from past interactions. Describe what you want to know in natural language.",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"query": {
					Type:        "string",
					Description: "What historical information to recall (natural language)",
				},
				"event_key": {
					Type:        "string",
					Description: "Get full details of a specific event by its key",
				},
				"limit": {
					Type:        "integer",
					Description: "Maximum number of results (default: 10)",
				},
			},
			Required: []string{"query"},
		},
	}
}

// Call implements tool.CallableTool.
func (rt *RecallTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args RecallQuery
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("recall: invalid args: %w", err)
	}

	if args.Query == "" {
		return nil, fmt.Errorf("recall: query is required")
	}

	// If event_key specified, get full details
	if args.EventKey != "" {
		return rt.getEventDetails(args.EventKey)
	}

	// Otherwise, query events
	return rt.queryEvents(ctx, args)
}

// getEventDetails retrieves a single event by key.
func (rt *RecallTool) getEventDetails(eventKey string) (any, error) {
	if rt.memStore == nil {
		return &RecallResponse{Message: "memory store not configured"}, nil
	}

	evt, err := rt.memStore.GetEvent(eventKey)
	if err != nil {
		return nil, fmt.Errorf("recall: event not found: %w", err)
	}

	return RecallEventDetail{
		Key:       evt.EventKey,
		ParentKey: evt.ParentKey,
		Type:      evt.EventType,
		Summary:   evt.EventSummary,
		Content:   evt.Content,
		Timestamp: evt.Timestamp,
	}, nil
}

// queryEvents queries events by natural language query.
func (rt *RecallTool) queryEvents(ctx context.Context, args RecallQuery) (any, error) {
	if rt.memStore == nil {
		return &RecallResponse{Message: "memory store not configured"}, nil
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}

	opts := memory.QueryOptions{
		Limit:   limit,
		OrderBy: "timestamp_desc",
	}

	events, err := rt.memStore.QueryEvents(opts)
	if err != nil {
		return nil, fmt.Errorf("recall: query failed: %w", err)
	}

	// If query has meaningful keywords, try to filter for relevance.
	// When no keywords survive stop-word filtering, return all events (let LLM decide).
	keywords := extractKeywords(args.Query)
	if len(keywords) > 0 {
		filtered := filterByKeywords(events, keywords)
		if len(filtered) > 0 {
			events = filtered
		}
	}

	return &RecallResponse{
		Events:  convertToRecallEvents(events),
		Message: fmt.Sprintf("found %d relevant events", len(events)),
	}, nil
}

// filterByKeywords filters events by matching query keywords against summary.
func filterByKeywords(events []memory.EventReference, keywords []string) []memory.EventReference {
	var filtered []memory.EventReference
	for _, evt := range events {
		summaryLower := strings.ToLower(evt.EventSummary)
		for _, kw := range keywords {
			if strings.Contains(summaryLower, kw) {
				filtered = append(filtered, evt)
				break
			}
		}
	}
	return filtered
}

// convertToRecallEvents converts EventReference list to RecallEvent list.
func convertToRecallEvents(events []memory.EventReference) []RecallEvent {
	result := make([]RecallEvent, 0, len(events))
	for _, e := range events {
		result = append(result, RecallEvent{
			Key:     e.EventKey,
			Type:    e.EventType,
			Summary: e.EventSummary,
		})
	}
	return result
}

// RecallArgs is an alias for RecallQuery for backward compatibility.
// Deprecated: use RecallQuery instead.
type RecallArgs = RecallQuery

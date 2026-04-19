package memory

import (
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// EventReference is a lightweight reference to an event stored in MemoryStore.
// Session only keeps EventReference list, not full event details.
// This implements the "information isolation" design for Phase 3.
//
// Key design: EventSummary is included for LLM reasoning.
// The LLM needs to see event summaries to understand execution results.
type EventReference struct {
	EventKey     string `json:"event_key"`     // Unique key linking to MemoryStore
	EventType    string `json:"event_type"`    // Event type (external_input, agent_output, etc.)
	EventSummary string `json:"event_summary"` // ⭐ Brief summary of event result (for LLM reasoning)
	Timestamp    int64  `json:"timestamp"`     // Unix timestamp in milliseconds
}

// FullEvent represents a complete event with all details stored in MemoryStore.
// This is the single source of truth for event data.
type FullEvent struct {
	EventKey     string                 `json:"event_key"`            // Unique identifier
	ParentKey    string                 `json:"parent_key,omitempty"` // Causal chain: key of the preceding event
	EventType    string                 `json:"event_type"`           // Event type
	EventSummary string                 `json:"event_summary"`        // ⭐ Brief summary (for LLM context)
	Timestamp    int64                  `json:"timestamp"`            // Unix timestamp (ms)
	Content      string                 `json:"content"`              // Event content/text
	ToolCalls    []model.ToolCall       `json:"tool_calls"`           // Tool calls (if any)
	ToolResults  map[string]interface{} `json:"tool_results"`         // Tool execution results
	Metadata     map[string]string      `json:"metadata"`             // Additional metadata

	// Response field for compatibility with Phase 2
	// Will be deprecated in Phase 4 when Tool access is fully implemented
	Response *model.Response `json:"response,omitempty"`
}

// MemoryStore is the interface for event storage and retrieval.
// It serves as the single source of truth for all event data.
type MemoryStore interface {
	// === Write Operations ===

	// StoreEvent stores a single event with its full details.
	StoreEvent(key string, event FullEvent) error

	// StoreEvents stores multiple events in batch.
	StoreEvents(events map[string]FullEvent) error

	// === Read Operations ===

	// GetEvent retrieves a single event by its EventKey.
	GetEvent(key string) (*FullEvent, error)

	// GetEvents retrieves multiple events by their EventKeys.
	// Returns events in the same order as keys.
	// Skips keys that don't exist (no error).
	GetEvents(keys []string) ([]FullEvent, error)

	// QueryEvents queries events based on filters.
	// Returns EventReference list (lightweight).
	QueryEvents(query QueryOptions) ([]EventReference, error)

	// === Management Operations ===

	// DeleteEvent permanently deletes an event from storage.
	// Use with caution - this operation cannot be undone.
	DeleteEvent(key string) error

	// GetStats returns storage statistics.
	GetStats() StoreStats
}

// QueryOptions specifies filters for querying events.
type QueryOptions struct {
	EventTypes []string `json:"event_types"` // Filter by event types (empty = all)
	StartTime  int64    `json:"start_time"`  // Start timestamp (0 = no limit)
	EndTime    int64    `json:"end_time"`    // End timestamp (0 = no limit)
	Limit      int      `json:"limit"`       // Max results (0 = no limit)
	Offset     int      `json:"offset"`      // Offset for pagination
	OrderBy    string   `json:"order_by"`    // "timestamp_asc" or "timestamp_desc"
}

// StoreStats contains storage statistics.
type StoreStats struct {
	TotalEvents int    `json:"total_events"`
	StorageSize int64  `json:"storage_size"` // In bytes
	DataDir     string `json:"data_dir"`
}

// Event type constants for MemoryPlugin event classification.
const (
	EventTypeExternalInput   = "external_input"
	EventTypeAgentOutput     = "agent_output"
	EventTypeActionCommand   = "action_command"
	EventTypeThinkingPlan    = "thinking_plan"
	EventTypeContextCompress = "context_compress"
	// Note: TypeSystemInput is unified with TypeExternalInput.
	// System-injected messages (RoleSystem) are classified as external_input.
)

// NewEventKey creates a new EventKey with the given timestamp and sequence.
func NewEventKey(timestamp int64, sequence int) string {
	return fmt.Sprintf("evt_%d_%03d", timestamp, sequence)
}

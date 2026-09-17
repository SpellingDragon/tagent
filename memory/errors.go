package memory

import (
	"errors"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// Typed storage-contract errors (resident-readiness-plan 2.5): callers must
// be able to distinguish "the key genuinely does not exist" from "storage
// I/O failed" — collapsing the two silently turns storage outages into
// empty recall results and failed recovery into fake empty chains.
var (
	// ErrKeyNotFound is wrapped by KV backends when a key genuinely does not
	// exist. Any other error from a KVGet/KVScan is storage I/O and must
	// propagate, never be treated as a miss.
	ErrKeyNotFound = errors.New("kv key not found")

	// ErrDuplicateEventKey is returned by StoreEvent when the EventKey is
	// already committed. An EventKey IS the event's identity (collision
	// guard D15): overwriting is refused, never silently applied.
	ErrDuplicateEventKey = errors.New("event key already exists")
)

// KeyNotFound wraps err (when non-nil) into a typed missing error carrying
// the key context. KV backends use it so callers can errors.Is.
func KeyNotFound(key string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrKeyNotFound, key)
	}
	return fmt.Errorf("%w: %s: %w", ErrKeyNotFound, key, err)
}

// cloneFullEvent copies the mutable containers of a FullEvent so the store
// and its callers never share Maps/Slices (delta spec「后端不可变与隔离一致
// 性」): a caller mutating a returned event (or the event it passed in) must
// not corrupt stored facts or other readers. Response is intentionally NOT
// deep-copied — model snapshots are treated as read-only by contract
// (documented boundary, one-level containers are the mutation surface).
func cloneFullEvent(e FullEvent) FullEvent {
	if e.Metadata != nil {
		m := make(map[string]string, len(e.Metadata))
		for k, v := range e.Metadata {
			m[k] = v
		}
		e.Metadata = m
	}
	if e.ToolCalls != nil {
		s := make([]model.ToolCall, len(e.ToolCalls))
		copy(s, e.ToolCalls)
		e.ToolCalls = s
	}
	if e.ToolResults != nil {
		m := make(map[string]interface{}, len(e.ToolResults))
		for k, v := range e.ToolResults {
			m[k] = v
		}
		e.ToolResults = m
	}
	return e
}

// IsDuplicateEventKey reports whether err is the typed duplicate-key error
// (cold-eyes Major 1: the replay path treats duplicates as idempotent success).
func IsDuplicateEventKey(err error) bool {
	return errors.Is(err, ErrDuplicateEventKey)
}

package agent

import (
	"strconv"
	"sync"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/event"
)

// SessionProjection is the bounded, lightweight projection of the event flow.
// It mirrors the prototype's `inputs []string`: onEvent appends, ContextManager
// reads, Compactor clears. Full event data lives in MemoryStore.
type SessionProjection struct {
	mu   sync.RWMutex
	refs []memory.EventReference
}

func NewSessionProjection() *SessionProjection {
	return &SessionProjection{refs: make([]memory.EventReference, 0)}
}

func (p *SessionProjection) Append(ref memory.EventReference) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refs = append(p.refs, ref)
}

func (p *SessionProjection) GetAll() []memory.EventReference {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]memory.EventReference, len(p.refs))
	copy(out, p.refs)
	return out
}

func (p *SessionProjection) Replace(refs []memory.EventReference) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refs = refs
}

func (p *SessionProjection) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.refs)
}

// BuildEventReference converts a framework event.Event into a lightweight
// memory.EventReference using the StateDelta fields written by MemoryPlugin.
//
// Field sources:
//   - EventKey, PartitionID, EventType, EventSummary: from StateDelta (written by MemoryPlugin)
//   - Role: from Response message (fallback to "" if no Response)
//   - Timestamp: from event.Timestamp
//
// EventSummary is read from StateDelta["event_summary"] (which contains the
// MemoryPlugin-generated summary, e.g. "调用工具: echo(hello)" for action_command).
// If StateDelta doesn't have event_summary, it falls back to msg.Content.
func BuildEventReference(evt *event.Event) (memory.EventReference, bool) {
	if evt == nil || evt.StateDelta == nil {
		return memory.EventReference{}, false
	}
	keyBytes, ok := evt.StateDelta["event_key"]
	if !ok || len(keyBytes) == 0 {
		return memory.EventReference{}, false
	}
	key, err := strconv.ParseInt(string(keyBytes), 10, 64)
	if err != nil {
		return memory.EventReference{}, false
	}
	ref := memory.EventReference{
		EventKey:  key,
		Timestamp: evt.Timestamp.UnixMilli(),
	}
	if partBytes, ok := evt.StateDelta["partition_id"]; ok && len(partBytes) > 0 {
		if pid, err := strconv.Atoi(string(partBytes)); err == nil {
			ref.PartitionID = pid
		}
	}
	if typeBytes, ok := evt.StateDelta["event_type"]; ok && len(typeBytes) > 0 {
		ref.EventType = string(typeBytes)
	}
	// Prefer event_summary from StateDelta (MemoryPlugin-generated).
	// Fall back to msg.Content for backward compatibility.
	if sumBytes, ok := evt.StateDelta["event_summary"]; ok && len(sumBytes) > 0 {
		ref.EventSummary = string(sumBytes)
	}
	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		msg := evt.Response.Choices[0].Message
		ref.Role = string(msg.Role)
		if ref.EventSummary == "" {
			ref.EventSummary = msg.Content
		}
	}
	return ref, true
}

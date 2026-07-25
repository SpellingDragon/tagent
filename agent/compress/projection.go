package compress

import (
	"sync"

	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/log"
)

// SessionProjection is the bounded, lightweight projection of the event flow.
// It mirrors the prototype's `inputs []string`: onEvent appends, ContextManager
// reads, Compactor clears. Full event data lives in MemoryStore.
type SessionProjection struct {
	mu   sync.RWMutex
	refs []memory.EventReference
	// seen tracks EventKeys (>0) already present, so Append is idempotent —
	// the same event (same key) is never projected twice (conversation-self-heal
	// L1). Rebuilt on Replace. EventKey==0 (unkeyed) never participates.
	seen map[int64]struct{}
}

func NewSessionProjection() *SessionProjection {
	return &SessionProjection{
		refs: make([]memory.EventReference, 0),
		seen: make(map[int64]struct{}),
	}
}

func (p *SessionProjection) Append(ref memory.EventReference) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ref.EventKey > 0 {
		if p.seen == nil {
			p.seen = make(map[int64]struct{})
		}
		if _, dup := p.seen[ref.EventKey]; dup {
			// Idempotent: same event already projected. Skipping prevents the
			// duplicate role=tool messages that cause model-API 4xx. The warn
			// surfaces the duplicate source for diagnosis.
			log.Warnf("[projection] skip duplicate append: key=%d role=%s type=%s",
				ref.EventKey, ref.Role, ref.EventType)
			return
		}
		p.seen[ref.EventKey] = struct{}{}
	}
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
	// Rebuild the seen set so idempotency stays consistent with the new refs:
	// without this, compacted-out keys would linger forever (unbounded growth
	// for a long-running agent) and the compaction summary's new key would be
	// untracked. seen must mirror exactly the keys currently in refs.
	p.seen = make(map[int64]struct{}, len(refs))
	for _, r := range refs {
		if r.EventKey > 0 {
			p.seen[r.EventKey] = struct{}{}
		}
	}
}

func (p *SessionProjection) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.refs)
}

// UpdateSummary updates the EventSummary of the ref at the given index.
// Silently returns if idx is out of bounds.
func (p *SessionProjection) UpdateSummary(idx int, summary string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if idx >= 0 && idx < len(p.refs) {
		p.refs[idx].EventSummary = summary
	}
}

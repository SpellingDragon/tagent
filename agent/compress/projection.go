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
	// 重建按当前 refs 整表重算，不沿用旧集合（键集随重建有界）。
	// the same event (same key) is never projected twice
	//. Rebuilt on Replace. EventKey==0 (unkeyed) never participates.
	seen map[int64]struct{}
}

// NewSessionProjection 构造一个空投影：引用表与去重键集都是空的。
func NewSessionProjection() *SessionProjection {
	return &SessionProjection{
		refs: make([]memory.EventReference, 0),
		seen: make(map[int64]struct{}),
	}
}

// Append 追加一条事件引用，并对带键（EventKey>0）的事件幂等：同一键第二次追加会被跳过并
// 记一条告警（重复投影会让同一条事实出现在上下文两次）。无键（EventKey==0）的引用不参与
// 去重，总是追加。
func (p *SessionProjection) Append(ref memory.EventReference) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ref.EventKey > 0 {
		if p.seen == nil {
			p.seen = make(map[int64]struct{})
		}
		if _, dup := p.seen[ref.EventKey]; dup {
			log.Warnf("[projection] skip duplicate append: key=%d role=%s type=%s",
				ref.EventKey, ref.Role, ref.EventType)
			return
		}
		p.seen[ref.EventKey] = struct{}{}
	}
	p.refs = append(p.refs, ref)
}

// GetAll 返回引用表的**拷贝**：调用方（装配上下文的一侧）改写结果不会影响投影内部状态。
func (p *SessionProjection) GetAll() []memory.EventReference {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]memory.EventReference, len(p.refs))
	copy(out, p.refs)
	return out
}

// Replace 整表替换投影内容，并按新引用**重算**去重键集：因此被折叠掉的键会随之离开键集，
// 键集大小只随重建有界，不会单调增长。
func (p *SessionProjection) Replace(refs []memory.EventReference) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refs = refs
	p.seen = make(map[int64]struct{}, len(refs))
	for _, r := range refs {
		if r.EventKey > 0 {
			p.seen[r.EventKey] = struct{}{}
		}
	}
}

// Len 返回当前投影的引用条数（读锁下），供"是否已折叠/是否空投影"这类判定使用。
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

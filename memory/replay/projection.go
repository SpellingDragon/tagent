// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Cold-start replay: the projection is the pure replay of one fact chain.
//
// The projection holds no state outside the fact chain: a cold start replays the
// committed facts among them and reads the chain back in EventKey write order,
// never by a timestamp approximation.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// ProjectionEntry is one ordered item of a projection: the canonical identity of
// a committed fact, its type and summary, the original text it carries, and its
// time, in the same shape a recall card entry uses.
type ProjectionEntry struct {
	// Key is the canonical identity of the fact, in its hex form.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is the original text of the fact: immutable input, never a view.
	// An assembly shapes the copy a request carries and leaves this text as the
	// fact stated it, so the same entry presents differently under different
	// budgets.
	Content string
	// inputEvents is the input_events metadata the fact carried: the JSON array
	// of the input identities an agent_output consumed. It stays private so the
	// entry's public shape remains the five fields of a projection record, and it
	// stays a string so the entry remains comparable; the segment rule reads it
	// through WithInputEvents and reads an empty or unreadable value as no
	// relation, so an output without the relation closes at the positional boundary.
	inputEvents string
	// messageRole is the role the fact's own message carried: the assembly
	// renders this role when the fact states one and fills the frozen type's
	// role only beside a message that carries none — one rule shared with the
	// consumption path. The field stays private so the entry's public shape
	// remains the five fields of a projection record.
	messageRole string
	// instanceID is the recorder generation the fact behind the entry was
	// written under: the runtime instance identity of the event envelope. It
	// stays private so the entry's public shape remains the five fields of a
	// projection record; the fact→projection mapping fills it through
	// WithInstanceID and the fold reads it through Generation, so a folded line
	// names the generation it came from without the envelope being copied beside
	// the entry or the identity being truncated to a shorter form.
	instanceID event.InstanceID
	// Time is the time the fact was recorded.
	Time time.Time
}

// ChainKey is the causal chain identity one fact chain is isolated under: the
// partition the facts are filed under beside the session they belong to. Two
// users of one agent name share a partition but never a session, so their
// chains never link into one (see causal-chain-isolation).
type ChainKey struct {
	// PartitionID is the partition the chain's facts are filed under.
	PartitionID int
	// SessionID is the session the chain belongs to. It is part of the isolation
	// key: an empty session is refused by name instead of degrading to a
	// partition-only chain that joins every user of the partition.
	SessionID event.SessionID
}

var (
	// ErrChainPageLimit reports a chain page request that asks for no fact at all.
	ErrChainPageLimit = errors.New("memory: chain page must read at least one fact")
)

// CompactionEventType names the frozen event type a compaction payload is
// committed to the fact chain under. A compaction fact is consumed as the
// snapshot it carries and never enters the projection as an entry of its own.
const CompactionEventType = event.TypeContextCompress

// ChainFact is one committed fact as the fact chain replays it: the canonical
// identity and frozen type of the fact, the isolated chain it belongs to, the
// summary stored beside it and the original text it carries. The fields carry
// built-in and event types only, so the storage layers' canonical facts map
// onto them without a package edge.
type ChainFact struct {
	// Key is the canonical identity of the fact.
	Key event.EventID
	// Chain is the causal chain the fact belongs to.
	Chain ChainKey
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is the original text of the fact.
	Content string
	// Time is the time the fact was recorded.
	Time time.Time
}

// EventChain is the fact chain a projection is replayed from: the committed
// facts in EventKey write order, paged by minimum event key, plus the lookup a
// positive compaction ref hydrates through. A failed read is reported by name
// and is never returned as an empty chain.
//
// The redline list (3 rules):
// MUST page the chain in EventKey write order, after the given key.
// MUST refuse a page limit below one by name.
// MUST NOT answer a failed read as an empty chain.
type EventChain interface {
	// ScanFrom pages the chain: at most limit facts whose key sorts strictly
	// after after (the empty key starts the first page), in EventKey write order.
	// A limit below one is refused by name with ErrChainPageLimit.
	ScanFrom(ctx context.Context, after event.EventID, limit int) ([]ChainFact, error)
	// Fact returns the committed fact under key, and reports whether one is held.
	Fact(ctx context.Context, key event.EventID) (ChainFact, bool, error)
}

// MemoryEventChain is the in-memory fact chain: the reference implementation of
// the replay source, holding only what the process holds.
type MemoryEventChain struct {
	mu    sync.Mutex
	facts map[event.EventID]ChainFact
}

// NewMemoryEventChain returns an empty in-memory fact chain.
func NewMemoryEventChain() *MemoryEventChain {
	return &MemoryEventChain{facts: map[event.EventID]ChainFact{}}
}

var _ EventChain = (*MemoryEventChain)(nil)

// Commit stores one fact of the chain. A repeat of the committed fact is
// idempotent, and other content under the same key is refused.
func (c *MemoryEventChain) Commit(ctx context.Context, fact ChainFact) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if fact.Key == "" {
		return ledger.ErrEventKeyRequired
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prior, held := c.facts[fact.Key]; held {
		if prior.Content != fact.Content || prior.Summary != fact.Summary || prior.Type != fact.Type {
			return ledger.ErrContentConflict
		}
		return nil
	}
	c.facts[fact.Key] = fact
	return nil
}

// ScanFrom implements EventChain.ScanFrom: the chain is paged in EventKey write
// order, which the canonical key encodes.
func (c *MemoryEventChain) ScanFrom(ctx context.Context, after event.EventID, limit int) ([]ChainFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 {
		return nil, ErrChainPageLimit
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.facts))
	for key := range c.facts {
		if string(key) > string(after) {
			keys = append(keys, string(key))
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]ChainFact, 0, len(keys))
	for _, key := range keys {
		out = append(out, c.facts[event.EventID(key)])
	}
	return out, nil
}

// Fact implements EventChain.Fact.
func (c *MemoryEventChain) Fact(ctx context.Context, key event.EventID) (ChainFact, bool, error) {
	if err := ctx.Err(); err != nil {
		return ChainFact{}, false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fact, held := c.facts[key]
	return fact, held, nil
}

// CompactionFact is the compaction payload one fold leaves on the fact chain:
// the summary entry the fold reports first, the retained refs of the folded run
// in their interleaved payload order, the event-key boundary the snapshot
// covers and the consumed keys it reseeds. The payload is committed to the
// chain as one compaction event, and the compaction event itself never enters
// the projection as an entry.
type CompactionFact struct {
	// Summary is the summary entry the fold reports first on restore.
	Summary CompactionRef `json:"summary"`
	// Refs are the retained refs of the folded run, in payload order: a
	// synthetic and a positive ref keep the order the payload recorded.
	Refs []CompactionRef `json:"refs"`
	// FullBoundary is the event key the snapshot covers up to. A tail replayed
	// against this payload resumes strictly after it, using this value and never a
	// timestamp approximation.
	FullBoundary event.EventID `json:"full_boundary"`
	// ConsumedKeys are the consumed keys the snapshot reseeds; the tail's own
	// consumed keys are reseeded beside them from the replayed tail.
	ConsumedKeys []event.EventID `json:"consumed_keys,omitempty"`
}

// CompactionRef is one ref of a compaction payload. A synthetic ref carries its
// full text in the payload under a negative key, while a positive key hydrates
// its entry from the fact chain and carries no text of its own — the payload
// never stands in for the chain.
type CompactionRef struct {
	// Key is the ref identity: negative for a synthetic ref, a canonical fact
	// key for a ref the chain hydrates.
	Key event.EventID `json:"key"`
	// Type is the frozen event type of the entry the ref restores.
	Type string `json:"type"`
	// Summary is the stored summary of the entry.
	Summary string `json:"summary"`
	// Content is the full text of a synthetic ref, and is empty for a positive
	// key, which hydrates from the fact chain instead.
	Content string `json:"content,omitempty"`
	// Time is the time the entry was recorded.
	Time time.Time `json:"time"`
}

// SyntheticKey reports whether a ref key names a synthetic ref, whose full text
// the payload carries, rather than a fact the chain hydrates.
func SyntheticKey(key event.EventID) bool {
	return strings.HasPrefix(string(key), "-")
}

// renderEntryLine reports the canonical line RenderProjection writes for one
// entry: the JSON form of the entry, without the newline after it. An entry the
// encoder refuses answers false and adds nothing to the render.
func renderEntryLine(entry ProjectionEntry) ([]byte, bool) {
	line, err := json.Marshal(entry)
	if err != nil {
		return nil, false
	}
	return line, true
}

// renderedEntryVolume reports the bytes one entry adds to the rendered
// projection: its canonical line and the newline RenderProjection writes after
// it.
func renderedEntryVolume(entry ProjectionEntry) int {
	line, ok := renderEntryLine(entry)
	if !ok {
		return 0
	}
	return len(line) + 1
}

// RenderProjection renders the projection in its canonical form: one JSON line
// per entry, in order, carrying the entry's identity, type, summary, model view
// content and time. A rebuild that replays the same fact chain reproduces these
// bytes exactly — the byte-for-byte comparison a cold-start rebuild is judged
// by.
func RenderProjection(entries []ProjectionEntry) []byte {
	var out []byte
	for _, entry := range entries {
		line, ok := renderEntryLine(entry)
		if !ok {
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out
}

// StatedRole reports the role the fact's own message carried, empty beside a
// fact whose message stated none.
func (e ProjectionEntry) StatedRole() string { return e.messageRole }

// WithMessageRole returns the entry beside the role its own message carried:
// the one setter for a field a cross-package assembly fills, so the entry's
// public shape stays closed.
func WithMessageRole(entry ProjectionEntry, role string) ProjectionEntry {
	entry.messageRole = role
	return entry
}

// Generation reports the recorder generation the fact behind the entry was
// written under: the envelope's runtime instance identity, verbatim and never
// truncated. An entry whose fact named no generation answers empty, which the
// fold renders as its explicit gen:? placeholder rather than inventing one.
func (e ProjectionEntry) Generation() event.InstanceID { return e.instanceID }

// WithInstanceID returns the entry beside the recorder generation its fact was
// written under: the one setter for the field the fact→projection mapping fills,
// so the entry's public shape stays closed and the generation is read back only
// through Generation.
func WithInstanceID(entry ProjectionEntry, generation event.InstanceID) ProjectionEntry {
	entry.instanceID = generation
	return entry
}

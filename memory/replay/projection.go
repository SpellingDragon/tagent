// 契约: docs/wiki/memory/memory-architecture.md#event-shape
//
// Cold-start replay: the projection is the pure replay of one fact chain.
//
// The projection holds no state outside the fact chain: a rebuild replays the
// committed facts and the compaction events among them, and nothing else. A
// compaction payload is restored from the chain exactly as the fold left it —
// synthetic refs carry their full text in the payload under a negative key,
// positive keys hydrate from the chain — and the tail is paged by minimum event
// key and appended in EventKey write order, never by timestamp approximation.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// ProjectionEntry is one ordered item of a projection: the canonical identity of
// a committed fact, its type and summary, the content presented for it, and its
// time, in the same shape a recall card entry uses.
type ProjectionEntry struct {
	// Key is the canonical identity of the fact, in its hex form.
	Key event.EventID
	// Type is the frozen event type of the fact.
	Type string
	// Summary is the stored summary of the fact, which is not the full text.
	Summary string
	// Content is what the model view carries for this entry: the full original
	// when the recorded decision is full text, and otherwise the bounded preview
	// or ticket that decision recorded.
	Content string
	// Time is the time the fact was recorded.
	Time time.Time
}

// PresentationMode names how one projection entry is presented to the model.
type PresentationMode int

const (
	// PresentationUnrecorded is the zero value: no decision was recorded for the
	// entry, so it is never presented as if one had been.
	PresentationUnrecorded PresentationMode = iota
	// PresentationFull presents the full original text of the entry.
	PresentationFull
	// PresentationSummary presents the stored summary in place of the original.
	PresentationSummary
	// PresentationTicket presents a bounded preview together with the ticket
	// that reaches the original by segment.
	PresentationTicket
)

// PresentationDecision is the recorded choice of how one projection entry was
// presented to the model, and the basis a replay reproduces.
//
// The recorded decision is what a replay follows, so a recorded preview is never
// promoted to the full original and the original is never reconstructed from the
// decision.
type PresentationDecision struct {
	// Key identifies the entry the decision was recorded for.
	Key event.EventID
	// Mode is the presentation chosen for the entry.
	Mode PresentationMode
	// Preview is the bounded text shown when Mode is not PresentationFull, and
	// is empty for a full-text decision.
	Preview string
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
	// ErrChainKeyRequired reports a chain key that names no session, so a replay
	// under it would join one user's facts to a shared partition chain.
	ErrChainKeyRequired = errors.New("memory: chain key names no session")
	// ErrRebuildUnwired reports a rebuild without the target projection, the fact
	// chain or the relation ledger it maintains the causal chain through.
	ErrRebuildUnwired = errors.New("memory: rebuild needs a target projection, a fact chain and a relation ledger")
	// ErrChainPageLimit reports a chain page request that asks for no fact at all.
	ErrChainPageLimit = errors.New("memory: chain page must read at least one fact")
	// ErrChainScan reports a fact chain read that failed, so an empty result is
	// never read as an empty chain.
	ErrChainScan = errors.New("memory: fact chain read failed")
	// ErrCompactionCorrupt reports a compaction payload that cannot be read back
	// as the fold snapshot it claims to carry.
	ErrCompactionCorrupt = errors.New("memory: compaction payload cannot be read")
	// ErrRebuildPartial reports a rebuild that could not hydrate every positive
	// ref of the snapshot from the fact chain. The result it accompanies is
	// partial and is never reported as a byte-exact rebuild.
	ErrRebuildPartial = errors.New("memory: rebuild is partial: the fact chain misses snapshot keys")
)

// RebuildSkipNotEmpty is the recorded reason of a rebuild that found a
// non-empty projection: the projection is never replaced by a replay.
const RebuildSkipNotEmpty = "projection not empty"

const (
	// RebuildPageSize is the page size the rebuild pages the fact chain with:
	// minimum-event-key paging in EventKey write order.
	RebuildPageSize = 64
	// FallbackEventLimit bounds the fallback replay of a chain that carries no
	// compaction event: the most recent facts are kept and the count left out is
	// reported, so a non-empty chain is never rebuilt as an empty session.
	FallbackEventLimit = 128
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

// MemoryRelations is the causal parent ledger between committed facts: the
// parent of one fact is set and read back here, never on the fact payload. The
// replay maintains these links inside one (PartitionID, SessionID) chain, so a
// walk never crosses from one session's chain into another's.
//
// The ledger is the one relation surface a rebuild writes through: it is the
// only implementation, so the rebuild names it directly rather than declaring a
// port with no second implementer behind it.
//
// 契约: docs/wiki/memory/memory-architecture.md#hard-contracts
type MemoryRelations struct {
	mu      sync.Mutex
	parents map[event.EventID]event.EventID
}

// NewMemoryRelations returns an empty in-memory relation store.
func NewMemoryRelations() *MemoryRelations {
	return &MemoryRelations{parents: map[event.EventID]event.EventID{}}
}

// SetParent sets the parent of child, or clears it when parent is empty.
func (r *MemoryRelations) SetParent(child event.EventID, parent event.EventID) error {
	if child == "" {
		return ledger.ErrKeyRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if parent == "" {
		delete(r.parents, child)
		return nil
	}
	r.parents[child] = parent
	return nil
}

// GetParent returns the parent of child, empty when there is none.
func (r *MemoryRelations) GetParent(child event.EventID) (event.EventID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.parents[child], nil
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
	// FullBoundary is the event key the snapshot covers up to. The rebuild pages
	// the tail strictly after it, using this payload value and never a timestamp
	// approximation.
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

// RebuildOptions shapes one cold-start replay.
type RebuildOptions struct {
	// PageSize is the minimum-event-key page size of the tail pagination; zero
	// means RebuildPageSize.
	PageSize int
	// FallbackBound bounds the fallback replay of a chain without compaction;
	// zero means FallbackEventLimit.
	FallbackBound int
}

// RebuildReport is the structured result of one cold-start replay: what the
// rebuild appended, whether it ran the bounded fallback, and — for a partial
// result that carries a non-nil error — the snapshot keys the chain could not
// hydrate, so a partial rebuild is never reported as byte-exact.
type RebuildReport struct {
	// Entries is the number of entries the rebuild appended.
	Entries int
	// Fallback reports a chain without compaction, replayed under the bound.
	Fallback bool
	// Dropped is the count of chain facts the bounded fallback left out.
	Dropped int
	// Scanned reports a completed chain scan: an empty chain is a scanned empty
	// chain and is never confused with a failed read.
	Scanned bool
	// Skipped is the recorded reason no rebuild ran, empty when one did.
	Skipped string
	// Partial reports snapshot keys the chain could not hydrate.
	Partial bool
	// Missing lists the snapshot keys the chain could not hydrate.
	Missing []event.EventID
	// Consumed lists the consumed keys reseeded, from the snapshot and the
	// tail together.
	Consumed []event.EventID
	// FullBoundary is the boundary value the compaction payload carried.
	FullBoundary event.EventID
}

// entryOf projects one canonical fact onto the projection entry shape: the
// summary and the original text stay apart, never conflated.
func entryOf(fact ChainFact) ProjectionEntry {
	return ProjectionEntry{Key: fact.Key, Type: fact.Type, Summary: fact.Summary, Content: fact.Content, Time: fact.Time}
}

// entryOfRef restores one compaction ref as a projection entry: a synthetic ref
// is restored from the payload text it carries, exactly as the fold left it.
func entryOfRef(ref CompactionRef) ProjectionEntry {
	return ProjectionEntry{Key: ref.Key, Type: ref.Type, Summary: ref.Summary, Content: ref.Content, Time: ref.Time}
}

// RebuildProjectionFromWAL replays one fact chain into an empty projection: the
// cold-start rebuild that runs before any other recovery replay. A non-empty
// projection is never replaced and the skip reason is recorded. With a valid
// compaction the snapshot is restored first — the summary entry first, then the
// retained refs in payload order, positive keys hydrated from the chain and
// synthetic refs from the payload text — and the tail is paged by minimum event
// key and appended in EventKey write order after the payload boundary. Without
// a compaction the rebuild runs the bounded fallback instead of a no-op, and a
// successfully scanned empty chain is reported as empty.
//
// The causal chain is maintained through relations.SetParent inside this
// (PartitionID, SessionID) chain alone: a replay never links one session's
// facts into another's, and an empty session is refused by name.
func RebuildProjectionFromWAL(ctx context.Context, target *ReplayProjection, chain EventChain, relations *MemoryRelations, session ChainKey, opts RebuildOptions) (RebuildReport, error) {
	if target == nil || chain == nil || relations == nil {
		return RebuildReport{}, ErrRebuildUnwired
	}
	if session.SessionID == "" {
		return RebuildReport{}, fmt.Errorf("%w: partition %d", ErrChainKeyRequired, session.PartitionID)
	}
	if target.Len() > 0 {
		return RebuildReport{Skipped: RebuildSkipNotEmpty}, nil
	}
	page := opts.PageSize
	if page < 1 {
		page = RebuildPageSize
	}
	bound := opts.FallbackBound
	if bound < 1 {
		bound = FallbackEventLimit
	}
	report := RebuildReport{}
	var facts []ChainFact
	for after := event.EventID(""); ; {
		batch, err := chain.ScanFrom(ctx, after, page)
		if err != nil {
			return RebuildReport{}, fmt.Errorf("%w: %w", ErrChainScan, err)
		}
		if len(batch) == 0 {
			break
		}
		for _, fact := range batch {
			if fact.Chain == session {
				facts = append(facts, fact)
			}
		}
		after = batch[len(batch)-1].Key
	}
	report.Scanned = true
	var compaction *ChainFact
	for i := range facts {
		if facts[i].Type == CompactionEventType {
			compaction = &facts[i]
		}
	}
	prev := event.EventID("")
	link := func(key event.EventID) error {
		if key == "" || SyntheticKey(key) {
			return nil
		}
		if err := relations.SetParent(key, prev); err != nil {
			return err
		}
		prev = key
		return nil
	}
	seed := func(fact ChainFact) {
		target.Append(ctx, entryOf(fact))
		report.Entries++
		if fact.Type == event.TypeThinkingPlan {
			report.Consumed = append(report.Consumed, fact.Key)
		}
	}
	if compaction != nil {
		var payload CompactionFact
		if err := json.Unmarshal([]byte(compaction.Content), &payload); err != nil {
			return RebuildReport{}, fmt.Errorf("%w: %w", ErrCompactionCorrupt, err)
		}
		report.FullBoundary = payload.FullBoundary
		target.boundary = payload.FullBoundary
		target.Append(ctx, entryOfRef(payload.Summary))
		report.Entries++
		report.Consumed = append(report.Consumed, payload.ConsumedKeys...)
		for _, ref := range payload.Refs {
			if SyntheticKey(ref.Key) {
				target.Append(ctx, entryOfRef(ref))
				report.Entries++
				continue
			}
			fact, held, err := chain.Fact(ctx, ref.Key)
			if err != nil {
				return report, fmt.Errorf("%w: %w", ErrChainScan, err)
			}
			if !held {
				report.Partial = true
				report.Missing = append(report.Missing, ref.Key)
				continue
			}
			seed(fact)
			if err := link(fact.Key); err != nil {
				return report, err
			}
		}
		for _, fact := range facts {
			if fact.Type == CompactionEventType || string(fact.Key) <= string(payload.FullBoundary) {
				continue
			}
			seed(fact)
			if err := link(fact.Key); err != nil {
				return report, err
			}
		}
	} else {
		report.Fallback = true
		run := make([]ChainFact, 0, len(facts))
		for _, fact := range facts {
			if fact.Type != CompactionEventType {
				run = append(run, fact)
			}
		}
		if len(run) > bound {
			report.Dropped = len(run) - bound
			run = run[len(run)-bound:]
		}
		for _, fact := range run {
			seed(fact)
			if err := link(fact.Key); err != nil {
				return report, err
			}
		}
	}
	for _, key := range report.Consumed {
		target.seedConsumed(key)
	}
	target.noteRecovery(report.Entries, report.Fallback)
	if report.Partial {
		return report, fmt.Errorf("%w: %v", ErrRebuildPartial, report.Missing)
	}
	return report, nil
}

// RenderProjection renders the projection in its canonical form: one JSON line
// per entry, in order, carrying the entry's identity, type, summary, model view
// content and time. A rebuild that replays the same fact chain reproduces these
// bytes exactly — the byte-for-byte comparison a cold-start rebuild is judged
// by.
func RenderProjection(entries []ProjectionEntry) []byte {
	var out []byte
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return out
}

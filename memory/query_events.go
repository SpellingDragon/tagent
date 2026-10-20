// 契约: docs/wiki/memory/memory-architecture.md#read-paths
//
// The QueryEvents read face: the time, type and keyword legs one conditional
// retrieval runs on, under the query semantics the event-segment-store read
// port states. A query is declarative: its result is the whole matching set
// filtered, totally ordered and then paged, so no optimisation behind the read
// face may change what the query answers.
package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory/ledger"
)

// QueryOrderByAsc and QueryOrderByDesc are the two orderings a query names:
// oldest first and newest first. An empty OrderBy runs as the ascending
// default — matching the read port, where the default walk is old to new — and
// any other value is refused by name with ErrQueryOrder instead of being
// silently coerced into one of the two.
const (
	QueryOrderByAsc  = "timestamp_asc"
	QueryOrderByDesc = "timestamp_desc"
)

// ErrQueryOrder reports a query that names an order other than the two
// declared ones: the read face would have to invent a third ordering rule to
// answer it, so it is refused by name instead.
var ErrQueryOrder = errors.New("memory: query names an unknown order")

// ErrQueryPaging reports a query carrying a negative offset or limit, so no
// declarative page could answer it.
var ErrQueryPaging = errors.New("memory: query carries a negative offset or limit")

// QueryOptions is the filter one event query runs under: the partitions it may
// scan, the time window in Unix milliseconds, the event types and the keyword
// it narrows to, and the page it asks for. The time axis is milliseconds — the
// same axis as the event time and the recall tool documentation — and both
// bounds are inclusive of the events recorded at the bound itself; a zero
// bound leaves that side open. StartTime and EndTime are never compared
// against second-granularity window boundaries.
//
// The partition scope is default-nothing, not a fallback to the whole store:
// when PartitionIDs and PartitionID both name nothing, the query scans no
// partition at all and answers an empty result rather than every event, and a
// caller authorized to read partitions receives them injected here. Keyword is
// case-insensitive and matches the stored summary or the stored content; an
// empty keyword narrows nothing. EventTypes empty means every type. Limit is
// the page bound where zero means unbounded, and Offset skips that many events
// of the ordered result.
type QueryOptions struct {
	// PartitionID filters to one partition; zero means it names none.
	PartitionID int
	// PartitionIDs filters to the given partitions and takes precedence over
	// PartitionID. Both fields empty means no partition is scanned.
	PartitionIDs []int
	// EventTypes restricts the query to the given frozen event types; empty
	// means every type.
	EventTypes []string
	// StartTime is the inclusive lower time bound in Unix milliseconds; zero
	// leaves the lower side open.
	StartTime int64
	// EndTime is the inclusive upper time bound in Unix milliseconds; zero
	// leaves the upper side open.
	EndTime int64
	// Limit is the page bound: zero means the page is unbounded, never that
	// the query is refused.
	Limit int
	// Offset skips that many events of the totally ordered result before the
	// page is cut.
	Offset int
	// OrderBy names the ordering, one of QueryOrderByAsc and
	// QueryOrderByDesc; empty runs as the ascending default.
	OrderBy string
	// Keyword matches the stored summary or content case-insensitively;
	// empty narrows nothing.
	Keyword string
}

// Validate returns the named refusal that rejects this query before it runs,
// or nil when the query may run: an order that is neither declared ordering,
// or a negative offset or limit.
func (o QueryOptions) Validate() error {
	switch o.OrderBy {
	case "", QueryOrderByAsc, QueryOrderByDesc:
	default:
		return ErrQueryOrder
	}
	if o.Offset < 0 || o.Limit < 0 {
		return ErrQueryPaging
	}
	return nil
}

// resolvePartitions returns the partition set this query may scan, deduplicated
// in the order the query names them. Both fields empty resolves to no
// partition at all — the default-nothing scope of the read path, which is an
// empty result and never a blind scan of every partition.
func (o QueryOptions) resolvePartitions() []int {
	seen := make(map[int]bool, len(o.PartitionIDs)+1)
	partitions := make([]int, 0, len(o.PartitionIDs)+1)
	add := func(partition int) {
		if seen[partition] {
			return
		}
		seen[partition] = true
		partitions = append(partitions, partition)
	}
	if len(o.PartitionIDs) > 0 {
		for _, partition := range o.PartitionIDs {
			add(partition)
		}
		return partitions
	}
	if o.PartitionID != 0 {
		add(o.PartitionID)
	}
	return partitions
}

// EventReference is the light reference one query answers with: the canonical
// key of the event and the small rendering subset of it. The reference carries
// no content and no tool payload — the text behind a hit stays behind the
// original read path, so a page of references is never a second copy of the
// events it names.
type EventReference struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID
	// PartitionID is the partition the event is filed under, so a consumer
	// can re-check the attribution a hit claims.
	PartitionID int
	// Type is the frozen event type of the event.
	Type string
	// Summary is the stored summary of the event, which is not the full text.
	Summary string
	// Timestamp is the event time in Unix milliseconds, on the same axis the
	// query window bounds are stated in.
	Timestamp int64
}

// EventRecord is one event as the query source holds it: the light reference
// fields a query filters and orders on, the content the keyword leg may match,
// and the segment window the record was committed under.
//
// The window is the deduplication version marker: where one event key exists
// in two window layers at once — the crash window of a compaction that writes
// its target layer before deleting the source — the record of the higher
// window is the canonical one and the query returns it exactly once.
type EventRecord struct {
	// Key is the canonical identity of the event, in its hex form.
	Key event.EventID
	// PartitionID is the partition the event is filed under.
	PartitionID int
	// Type is the frozen event type of the event.
	Type string
	// Summary is the stored summary of the event.
	Summary string
	// Content is the stored content of the event, matched by the keyword leg.
	Content string
	// Timestamp is the event time in Unix milliseconds: the one semantic time
	// axis sorting, window filtering and age all read.
	Timestamp int64
	// WindowTS is the time window the record was committed under; a higher
	// window is the higher compaction layer and wins the event key.
	WindowTS int64
}

// Reference returns the light reference of this record: the rendering subset a
// query answers with, carrying no content and no payload. It is the one place
// the reference fields are derived, so a record and the reference a query
// answers for it never disagree.
func (r EventRecord) Reference() EventReference {
	return EventReference{
		Key:         r.Key,
		PartitionID: r.PartitionID,
		Type:        r.Type,
		Summary:     r.Summary,
		Timestamp:   r.Timestamp,
	}
}

// MemoryEventQuery is the in-process query source over recorded events. It is
// the deterministic reference implementation of the read-port query semantics;
// it keeps no durability promise of its own — a new instance answers only the
// records stored into it, and nothing it holds survives a process restart
// unless a durable tier stores and reloads them.
type MemoryEventQuery struct {
	mu    sync.Mutex
	byKey map[event.EventID]EventRecord
}

// NewMemoryEventQuery returns an empty query source.
func NewMemoryEventQuery() *MemoryEventQuery {
	return &MemoryEventQuery{byKey: map[event.EventID]EventRecord{}}
}

// Store records one event. A repeat of the same event key keeps the record of
// the higher window — the higher compaction layer is canonical — and a repeat
// at the same window is an idempotent no-op, so a crash window that replays a
// commit never yields two records for one event key.
func (q *MemoryEventQuery) Store(ctx context.Context, rec EventRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rec.Key == "" {
		return ledger.ErrEventKeyRequired
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if prior, held := q.byKey[rec.Key]; held && prior.WindowTS >= rec.WindowTS {
		return nil
	}
	q.byKey[rec.Key] = rec
	return nil
}

// QueryEvents returns the references matching opts on exactly the declarative
// terms of QueryOptions: the matching set is filtered first (partition scope,
// time window, event types, keyword), then totally ordered by (Timestamp, Key)
// under the named direction, then paged by Offset and Limit.
//
// The total order is the tie-breaker contract: events sharing one millisecond
// are ordered by their event key, and the descending order reverses both keys
// together, so any two runs of the same query return the same sequence
// position by position.
func (q *MemoryEventQuery) QueryEvents(ctx context.Context, opts QueryOptions) ([]EventReference, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return queryEventRecords(q.byKey, opts)
}

// queryEventRecords answers one query over the given records on the
// declarative terms of QueryOptions: the matching set is filtered first, then
// totally ordered by (Timestamp, Key) under the named direction, then paged by
// Offset and Limit. Every query source answers through this one routine, so
// two sources over identical records return identical sequences whatever
// backend holds them.
func queryEventRecords(byKey map[event.EventID]EventRecord, opts QueryOptions) ([]EventReference, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	partitions := opts.resolvePartitions()
	keyword := strings.ToLower(opts.Keyword)

	matched := make([]EventReference, 0, len(byKey))
	for _, rec := range byKey {
		if !partitionAdmitted(partitions, rec.PartitionID) {
			continue
		}
		if opts.StartTime > 0 && rec.Timestamp < opts.StartTime {
			continue
		}
		if opts.EndTime > 0 && rec.Timestamp > opts.EndTime {
			continue
		}
		if len(opts.EventTypes) > 0 && !typeAdmitted(opts.EventTypes, rec.Type) {
			continue
		}
		if keyword != "" && !keywordMatches(keyword, rec) {
			continue
		}
		matched = append(matched, rec.Reference())
	}
	descending := opts.OrderBy == QueryOrderByDesc
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if a.Timestamp != b.Timestamp {
			if descending {
				return a.Timestamp > b.Timestamp
			}
			return a.Timestamp < b.Timestamp
		}
		if descending {
			return a.Key > b.Key
		}
		return a.Key < b.Key
	})
	if opts.Offset > 0 {
		if opts.Offset >= len(matched) {
			return []EventReference{}, nil
		}
		matched = matched[opts.Offset:]
	}
	if opts.Limit > 0 && opts.Limit < len(matched) {
		matched = matched[:opts.Limit]
	}
	return matched, nil
}

// partitionAdmitted reports whether a partition is inside the resolved scope.
// An empty scope admits nothing, so an unscoped query answers empty instead of
// scanning every partition.
func partitionAdmitted(partitions []int, partition int) bool {
	for _, admitted := range partitions {
		if admitted == partition {
			return true
		}
	}
	return false
}

// typeAdmitted reports whether an event type is among the types the query
// restricts to.
func typeAdmitted(types []string, eventType string) bool {
	for _, admitted := range types {
		if admitted == eventType {
			return true
		}
	}
	return false
}

// keywordMatches reports whether the case-folded keyword appears in the stored
// summary or the stored content of the record.
func keywordMatches(keyword string, rec EventRecord) bool {
	return strings.Contains(strings.ToLower(rec.Summary), keyword) ||
		strings.Contains(strings.ToLower(rec.Content), keyword)
}

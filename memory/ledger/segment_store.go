package ledger

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
)

// SegmentRecord is one event on its way into a segment: the partition and window
// it belongs to, the sequence slot it takes, its canonical event key, the event
// time the window meta covers, and the event bytes committed verbatim.
type SegmentRecord struct {
	// PartitionID is the partition the event belongs to.
	PartitionID int
	// WindowTS is the time window the event is grouped under.
	WindowTS int64
	// Seq is the sequence slot the event takes inside that window.
	Seq int64
	// EventKey is the canonical identity of the event.
	EventKey event.EventID
	// EventTime is the actual event time the window meta accounts for.
	EventTime int64
	// Body is the event payload, committed byte for byte.
	Body []byte
	// typ and session ride in the same batch as the body so a restart can find
	// the facts it needs without reading any body. They are set by WithChain.
	typ     string
	session event.SessionID
}

// WithChain states the frozen event type and session this record carries in the
// same batch as its body. The index keeps them as a projection of the fact, so
// a restart can group by type and session without reading event bodies.
func (r SegmentRecord) WithChain(typ string, session event.SessionID) SegmentRecord {
	r.typ = typ
	r.session = session
	return r
}

// StoredEvent is one committed event as a read returns it: the bytes that were
// committed and the index position they were committed at.
type StoredEvent struct {
	// Key is the canonical identity of the event.
	Key event.EventID
	// PartitionID is the partition the event was committed into.
	PartitionID int
	// WindowTS is the time window the event was grouped under.
	WindowTS int64
	// Seq is the sequence slot the event was committed into.
	Seq int64
	// Body is the committed event payload.
	Body []byte
}

// storedWindowMeta is the meta one window carries alongside its events: the
// event count committed into the window and the event time range it covers.
type storedWindowMeta struct {
	PartitionID int   `json:"partition_id"`
	WindowTS    int64 `json:"window_ts"`
	EventCount  int   `json:"event_count"`
	MinTime     int64 `json:"min_time"`
	MaxTime     int64 `json:"max_time"`
}

// windowSlot names one window of one partition.
type windowSlot struct {
	partition int
	window    int64
}

// positionSlot names one sequence slot of one window.
type positionSlot struct {
	partition int
	window    int64
	seq       int64
}

// ErrEventKeyRequired reports a segment record that carries no event key, so no
// read could ever name the event again.
var ErrEventKeyRequired = errors.New("memory: segment record carries no event key")

// ErrSegmentPosition reports a segment record whose window or sequence slot is
// negative, so its key could not name a position.
var ErrSegmentPosition = errors.New("memory: segment record carries a negative window or sequence")

// ErrSegmentBarrier reports a commit whose barrier did not complete, so the
// record reaches no reader and no count.
var ErrSegmentBarrier = errors.New("memory: segment commit barrier failed")

// segmentCacheEntry is one event body the cache holds: the stored event beside
// the bytes it accounts for.
type segmentCacheEntry struct {
	stored StoredEvent
	size   int64
}

// segmentCache is the entry-level LRU of the segment store's read cache: event
// bodies with a byte budget. An evicted body is not lost — the index names its
// position and the read path reads it back from the key-value seam.
type segmentCache struct {
	order   *list.List
	entries map[event.EventID]*list.Element
	bytes   int64
	// frontKey and front mirror the most recently used entry beside the list, so
	// the steady read of one key takes the fast path: a string compare instead of
	// the map lookup and the list element assertion. The mirror is rewritten by
	// every put and by every hit that moves another entry to the front, so it can
	// never name an entry the order dropped.
	frontKey event.EventID
	front    segmentCacheEntry
}

// newSegmentCache returns an empty cache bounded by the declared budget.
func newSegmentCache() *segmentCache {
	return &segmentCache{order: list.New(), entries: map[event.EventID]*list.Element{}}
}

// get answers the stored event held under key and moves it to the front of the
// recency order.
func (c *segmentCache) get(key event.EventID) (StoredEvent, bool) {
	if key == c.frontKey {
		return c.front.stored, true
	}
	element, held := c.entries[key]
	if !held {
		return StoredEvent{}, false
	}
	entry := element.Value.(*segmentCacheEntry)
	if element != c.order.Front() {
		c.order.MoveToFront(element)
	}
	c.frontKey, c.front = key, *entry
	return entry.stored, true
}

// put makes one event resident, evicting the least recently used events until
// the byte budget holds. An event larger than the whole budget is evicted at
// once, so the resident bytes never exceed the budget.
func (c *segmentCache) put(stored StoredEvent) {
	size := segmentCacheEntrySize(stored)
	if element, held := c.entries[stored.Key]; held {
		entry := element.Value.(*segmentCacheEntry)
		c.bytes += size - entry.size
		entry.stored = stored
		entry.size = size
		c.order.MoveToFront(element)
	} else {
		entry := &segmentCacheEntry{stored: stored, size: size}
		c.entries[stored.Key] = c.order.PushFront(entry)
		c.bytes += size
	}
	for c.bytes > segmentCacheBudgetBytes {
		last := c.order.Back()
		if last == nil {
			break
		}
		entry := last.Value.(*segmentCacheEntry)
		c.bytes -= entry.size
		delete(c.entries, entry.stored.Key)
		c.order.Remove(last)
	}
	if front := c.order.Front(); front != nil {
		entry := front.Value.(*segmentCacheEntry)
		c.frontKey, c.front = entry.stored.Key, *entry
	} else {
		c.frontKey, c.front = "", segmentCacheEntry{}
	}
}

// segmentCacheEntrySize is the accounted size of one cached event: its body and
// key bytes plus the fixed cost of its position and payload metadata.
func segmentCacheEntrySize(stored StoredEvent) int64 {
	return int64(len(stored.Key)+len(stored.Body)) + segmentCacheEntryOverheadBytes
}

const (
	// segmentCacheBudgetBytes is the byte budget of the read cache: bodies held
	// in memory at once. The persisted segments are the record; the cache is a
	// read accelerator that never bounds what can be read. The 1 MiB budget
	// saturates before the smoke gate's stable window, so its fill is not read
	// as steady-state growth; cold bodies still come back from the segments.
	segmentCacheBudgetBytes int64 = 1 << 20
	// segmentCacheEntryOverheadBytes is the accounting cost of one cached event
	// beside its key and body bytes.
	segmentCacheEntryOverheadBytes int64 = 128
	// segmentIndexPrefix names the stored index entry of one event key: the
	// resident index is the small projection of exactly these entries, so the
	// read path keeps one index and never opens a second one.
	segmentIndexPrefix = "idx:"
	// segmentSettledKey names the record a relocation pass leaves in the hot
	// journal: how many events it moved into the cold journal. The count is what
	// lets a restart state the whole committed history from a hot journal that
	// holds a fraction of it.
	segmentSettledKey = "settled:events"
	// segmentSettledCommitKey names the record a relocation pass leaves in the hot
	// journal: the greatest commit sequence it moved into the cold journal. It is
	// what lets a newest-first walk of the hot journal answer without opening the
	// cold one: a hot entry whose sequence is past this bound is newer than every
	// cold entry, so the cold journal cannot change the answer and is not read.
	segmentSettledCommitKey = "settled:commit"
)

// ErrSegmentBodyMissing reports an event the index names but whose persisted
// body cannot be read: the read names the failure instead of publishing the
// event as an absent one.
var ErrSegmentBodyMissing = errors.New("memory: committed event body is missing")

// indexPosition is the resident form of one stored index entry: the partition,
// window and sequence slot the event key was committed at. It is the small
// projection of the persisted {pid}:idx:{event_key} entry, so the index stays
// resident while event bodies do not.
//
// commit is the store-wide commit sequence the commit of this entry was assigned.
// A window and a sequence slot number the events of one partition window, so they
// order two entries of one window and say nothing about an entry of another; the
// commit sequence orders every entry of the store, which is what a newest-first
// walk states and what ranks a resident entry against one a relocation moved into
// the cold journal. The two journals each keep their own frame order, and a frame
// order is relative to its own log, so the sequence is the only order the two
// share.
type indexPosition struct {
	partition int
	window    int64
	seq       int64
	commit    int64
	typ       string
	session   event.SessionID
}

// segmentIndexCarried is the index entry material a commit files beside the window
// and the sequence slot of the event: the frozen type and session the entry states
// and the store-wide commit sequence the commit was assigned.
type segmentIndexCarried struct {
	Type    string          `json:"type"`
	Session event.SessionID `json:"session"`
	Commit  int64           `json:"commit"`
}

// commitOrder is the engine capability of stating the order of its commits: the
// keys it holds, named from the newest committed record to the oldest, stopping
// when visit answers false. An append-log engine keeps that order on disk, so
// naming it costs the records walked; a key-ordered key-value engine keeps no
// such order and does not answer this capability at all.
type commitOrder interface {
	eachCommittedNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error
}

// FileSegmentStore commits events into time-window segments over one atomic
// store engine; it names no engine and no file layout, so any engine backs it. One commit stages the event body, its index entry and the window meta
// the window needs as one batch, and publishes the event to the read cache, the
// resident index and the event count only when that commit returns success. A
// commit that fails names the barrier as its step, carries its cause, unstages
// what it wrote, and publishes no part of the record.
//
// The resident state is bounded: the index keeps one small position per event
// key (the projection of the persisted index entries), the order keeps those keys
// in the order their commits landed, the read cache keeps bodies under a byte
// budget, and Load reads only the index, the window meta and the engine's commit
// order at startup. An event body the cache evicted is read back from its
// persisted {pid}:evt:{window}:{seq} entry, so a bounded process still answers
// every read.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
type FileSegmentStore struct {
	mu    sync.Mutex
	kv    AtomicStore
	index map[event.EventID]indexPosition
	// order is the published index in commit order, oldest commit first: the
	// order the records sit in the engine, so a walk from its end answers
	// newest-first and stops where a caller stops instead of ranking the entries
	// against each other. It is append-only, like the commits it projects.
	order   []event.EventID
	cache   *segmentCache
	count   int
	windows map[string]*storedWindowMeta
	// commitSeq is the greatest commit sequence this store assigned, so the next
	// commit takes one past it and no commit ever shares the rank of another. It is
	// past EventCount when an entry whose window meta did not arrive stayed
	// unpublished, because such an entry states a commit the count no longer holds.
	commitSeq int64
	// coldCommit is the greatest commit sequence a relocation moved into the cold
	// journal. A newest-first walk reads the cold journal only while its resident
	// head is not past it, so a store whose resident head is newer answers without
	// opening the settled history at all.
	coldCommit int64
	// coldRoot is the directory of the second journal the settled facts of this
	// store were relocated into, empty for a store that relocates none. The cold
	// view is opened on the first read that misses the resident index, never at
	// boot: a boot that opened it would read the settled history it exists to keep
	// out of the boot.
	coldRoot string
	// cold is the opened cold view, nil until a read misses it.
	cold AtomicStore
	// retire reports whether this store relocates settled facts. It is the
	// deployment's declaration, so a store it answers false for keeps every fact in
	// the hot journal even beside a cold root another deployment filled.
	retire bool
	// retired counts the events a relocation moved out of the hot journal. The
	// count rides in the hot journal so it survives a restart, and EventCount adds
	// it back: the events of a store are every event it committed, whatever
	// fraction of them the hot journal still holds.
	retired int
}

// newFileSegmentStore opens the segment store held in kv and publishes the
// index and window table of every stored record, leaving the event bodies on
// the key-value seam until a read names one.
func newFileSegmentStore(kv AtomicStore) (*FileSegmentStore, error) {
	store := &FileSegmentStore{
		kv:      kv,
		index:   map[event.EventID]indexPosition{},
		cache:   newSegmentCache(),
		windows: map[string]*storedWindowMeta{},
	}
	if err := store.Load(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

// Load rebuilds the resident index and window table from the persisted store
// without returning an event body. It enumerates the engine's key namespaces to
// discover the partitions, then reads the index and meta prefixes of each
// partition: both are small entries, and no {pid}:evt:{window}:{seq} body is
// read. A record whose index and window meta did not both arrive stays out of
// the published state, EventCount counts the published records, and the commit
// order is the one the engine states for the records it published.
func (s *FileSegmentStore) Load(ctx context.Context) error {
	namespaces, err := s.kv.Namespaces(ctx)
	if err != nil {
		return err
	}
	partitions := map[int]struct{}{}
	for _, namespace := range namespaces {
		partition, err := strconv.Atoi(namespace)
		if err != nil {
			continue
		}
		partitions[partition] = struct{}{}
	}
	sorted := make([]int, 0, len(partitions))
	for partition := range partitions {
		sorted = append(sorted, partition)
	}
	sort.Ints(sorted)
	positions := map[event.EventID]indexPosition{}
	meta := map[windowSlot]storedWindowMeta{}
	for _, partition := range sorted {
		idxPairs, err := s.kv.Read(ctx, Fetch{Prefix: fmt.Sprintf("%d:%s", partition, segmentIndexPrefix)})
		if err != nil {
			return err
		}
		for _, pair := range idxPairs {
			eventKey, held := strings.CutPrefix(pair.Key, fmt.Sprintf("%d:%s", partition, segmentIndexPrefix))
			if !held || eventKey == "" {
				continue
			}
			position, named := indexPositionOf(partition, pair.Value)
			if !named {
				continue
			}
			positions[event.EventID(eventKey)] = position
		}
		metaPairs, err := s.kv.Read(ctx, Fetch{Prefix: fmt.Sprintf("%d:meta:", partition)})
		if err != nil {
			return err
		}
		for _, pair := range metaPairs {
			name, held := strings.CutPrefix(pair.Key, fmt.Sprintf("%d:meta:", partition))
			if !held {
				continue
			}
			window, err := strconv.ParseInt(name, 10, 64)
			if err != nil {
				continue
			}
			var entry storedWindowMeta
			if err := json.Unmarshal(pair.Value, &entry); err != nil {
				return fmt.Errorf("%w: window meta %q: %w", ErrSnapshotCorrupt, pair.Key, err)
			}
			meta[windowSlot{partition: partition, window: window}] = entry
		}
	}
	published := map[event.EventID]indexPosition{}
	for key, position := range positions {
		if _, held := meta[windowSlot{partition: position.partition, window: position.window}]; !held {
			continue
		}
		published[key] = position
	}
	order, err := s.commitOrderOf(ctx, published)
	if err != nil {
		return err
	}
	retired, err := s.retiredCountOf(ctx)
	if err != nil {
		return err
	}
	coldCommit, err := s.coldCommitOf(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index = published
	s.windows = map[string]*storedWindowMeta{}
	s.count = len(published) + retired
	s.commitSeq = int64(s.count)
	s.retired = retired
	s.coldCommit = coldCommit
	// A published entry states its own commit sequence, which is past the count
	// when an entry whose window meta did not arrive stayed out of the published
	// state: assigning the next sequence below one already filed would rank a new
	// commit as an old one.
	for _, position := range published {
		if position.commit > s.commitSeq {
			s.commitSeq = position.commit
		}
	}
	s.order = order
	for slot, entry := range meta {
		carried := entry
		s.windows[windowKey(slot.partition, slot.window)] = &carried
	}
	return nil
}

// coldCommitOf answers the greatest commit sequence a relocation moved into the
// cold journal, as the hot journal states it. Zero means no settled entry can be
// newer than any resident one, which is the answer of a store that never
// relocated anything.
func (s *FileSegmentStore) coldCommitOf(ctx context.Context) (int64, error) {
	pairs, err := s.kv.Read(ctx, Fetch{Key: segmentSettledCommitKey})
	if err != nil {
		return 0, err
	}
	if len(pairs) == 0 {
		return 0, nil
	}
	commit, err := strconv.ParseInt(string(pairs[0].Value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: the settled commit %q: %w", ErrSnapshotCorrupt, segmentSettledCommitKey, err)
	}
	if commit < 0 {
		return 0, fmt.Errorf("%w: the settled commit %q states %d", ErrSnapshotCorrupt, segmentSettledCommitKey, commit)
	}
	return commit, nil
}

// retiredCountOf answers the events a relocation pass moved out of the hot journal,
// as the hot journal states them. The count is what keeps EventCount stating the
// whole committed history instead of the fraction the hot journal still holds, so a
// bounded hot journal never reads as a store that lost its history.
func (s *FileSegmentStore) retiredCountOf(ctx context.Context) (int, error) {
	pairs, err := s.kv.Read(ctx, Fetch{Key: segmentSettledKey})
	if err != nil {
		return 0, err
	}
	if len(pairs) == 0 {
		return 0, nil
	}
	retired, err := strconv.Atoi(string(pairs[0].Value))
	if err != nil {
		return 0, fmt.Errorf("%w: the settled count %q: %w", ErrSnapshotCorrupt, segmentSettledKey, err)
	}
	if retired < 0 {
		return 0, fmt.Errorf("%w: the settled count %q states %d", ErrSnapshotCorrupt, segmentSettledKey, retired)
	}
	return retired, nil
}

// commitOrderOf answers the published index entries in the order their commits
// landed, oldest first. The engine states that order itself when it keeps one, so
// the answer costs one walk of the records filed and no sort; an engine that keeps
// no commit order — a key-ordered key-value engine, or a wrapper that does not
// forward this capability — answers the slot order its index entries state
// instead, which costs one sort of the entries and cannot tell that a commit of
// one partition landed after a commit of another.
func (s *FileSegmentStore) commitOrderOf(ctx context.Context, published map[event.EventID]indexPosition) ([]event.EventID, error) {
	order := make([]event.EventID, 0, len(published))
	if ordered, ok := s.kv.(commitOrder); ok {
		seen := make(map[event.EventID]struct{}, len(published))
		if err := ordered.eachCommittedNewestFirst(ctx, func(key string) (bool, error) {
			partition, id, named := segmentIndexEntry(key)
			position, held := published[id]
			if !named || !held || position.partition != partition {
				return true, nil
			}
			if _, placed := seen[id]; placed {
				return true, nil
			}
			seen[id] = struct{}{}
			order = append(order, id)
			return len(order) < len(published), nil
		}); err != nil {
			return nil, err
		}
		if len(order) != len(published) {
			return nil, fmt.Errorf("tagent: the engine states the commits of %d of the %d index entries it published", len(order), len(published))
		}
		slices.Reverse(order)
		return order, nil
	}
	for key := range published {
		order = append(order, key)
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := published[order[i]], published[order[j]]
		if a.partition != b.partition {
			return a.partition < b.partition
		}
		if a.seq != b.seq {
			return a.seq < b.seq
		}
		return order[i] < order[j]
	})
	return order, nil
}

// segmentIndexEntry splits one stored index key into the partition it was filed
// under and the event key it names, and answers false for a key of another
// family.
func segmentIndexEntry(key string) (int, event.EventID, bool) {
	name, rest, held := strings.Cut(key, ":")
	if !held {
		return 0, "", false
	}
	id, held := strings.CutPrefix(rest, segmentIndexPrefix)
	if !held || id == "" {
		return 0, "", false
	}
	partition, err := strconv.Atoi(name)
	if err != nil {
		return 0, "", false
	}
	return partition, event.EventID(id), true
}

// StoreEvent commits one event behind the segment commit barrier: the event
// body, its index entry and the window meta the window needs are one batch
// committed before the event reaches the read cache, the resident index, the
// event count or the caller. A repeat of the committed record returns success
// without a second fact, other bytes under the same event key are refused, and a
// failed commit leaves nothing published.
func (s *FileSegmentStore) StoreEvent(ctx context.Context, rec SegmentRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rec.EventKey == "" {
		return ErrEventKeyRequired
	}
	if rec.Seq < 0 || rec.WindowTS < 0 {
		return ErrSegmentPosition
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, held := s.index[rec.EventKey]; held {
		if prior.partition != rec.PartitionID || prior.window != rec.WindowTS || prior.seq != rec.Seq {
			return ErrContentConflict
		}
		existing, found, err := s.duplicateBodyLocked(ctx, rec.EventKey, prior)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: %s", ErrSegmentBodyMissing, rec.EventKey)
		}
		if !bytes.Equal(existing, rec.Body) {
			return ErrContentConflict
		}
		return nil
	}
	// A fact a relocation moved into the cold journal is still a committed fact: a
	// commit of it again answers as the duplicate it is, never as a second landing
	// of one identity.
	coldStored, coldHeld, err := s.coldEventLocked(ctx, rec.EventKey)
	if err != nil {
		return err
	}
	if coldHeld {
		if coldStored.PartitionID != rec.PartitionID || coldStored.WindowTS != rec.WindowTS || coldStored.Seq != rec.Seq {
			return ErrContentConflict
		}
		if !bytes.Equal(coldStored.Body, rec.Body) {
			return ErrContentConflict
		}
		return nil
	}
	meta := s.nextWindowMeta(rec)
	evtKey := fmt.Sprintf("%d:evt:%d:%d", rec.PartitionID, rec.WindowTS, rec.Seq)
	idxKey := fmt.Sprintf("%d:idx:%s", rec.PartitionID, rec.EventKey)
	metaKey := fmt.Sprintf("%d:meta:%d", rec.PartitionID, rec.WindowTS)
	evtValue := append([]byte(nil), rec.Body...)
	commit := s.commitSeq + 1
	idxMeta, err := json.Marshal(segmentIndexCarried{Type: rec.typ, Session: rec.session, Commit: commit})
	if err != nil {
		return fmt.Errorf("tagent: encode the index type of %s: %w", rec.EventKey, err)
	}
	idxValue := []byte(fmt.Sprintf("%d:%d:%s", rec.WindowTS, rec.Seq, idxMeta))
	metaValue, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	batch := Batch{Puts: []Pair{
		{Key: evtKey, Value: evtValue},
		{Key: idxKey, Value: idxValue},
		{Key: metaKey, Value: metaValue},
	}}
	if err := s.kv.Commit(ctx, batch); err != nil {
		return fmt.Errorf("%w: %w", ErrSegmentBarrier, err)
	}
	stored := StoredEvent{
		Key:         rec.EventKey,
		PartitionID: rec.PartitionID,
		WindowTS:    rec.WindowTS,
		Seq:         rec.Seq,
		Body:        evtValue,
	}
	s.index[rec.EventKey] = indexPosition{partition: rec.PartitionID, window: rec.WindowTS, seq: rec.Seq, commit: commit, typ: rec.typ, session: rec.session}
	s.order = append(s.order, rec.EventKey)
	s.cache.put(stored)
	s.commitSeq = commit
	s.count++
	s.windows[windowKey(rec.PartitionID, rec.WindowTS)] = meta
	return nil
}

// GetEvent returns the committed event under key together with the index
// position it was committed at, and reports whether any is published. A body the
// cache evicted is read back from its persisted entry; an index entry whose body
// cannot be read reports ErrSegmentBodyMissing instead of an absent event.
func (s *FileSegmentStore) GetEvent(ctx context.Context, key event.EventID) (StoredEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return StoredEvent{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, held := s.cache.get(key); held {
		cached.Body = append([]byte(nil), cached.Body...)
		return cached, true, nil
	}
	position, held := s.index[key]
	if !held {
		return s.coldEventLocked(ctx, key)
	}
	body, found, err := s.readBodyLocked(ctx, position)
	if err != nil {
		return StoredEvent{}, false, err
	}
	if !found {
		return StoredEvent{}, false, fmt.Errorf("%w: %s", ErrSegmentBodyMissing, key)
	}
	stored := StoredEvent{
		Key:         key,
		PartitionID: position.partition,
		WindowTS:    position.window,
		Seq:         position.seq,
		Body:        body,
	}
	s.cache.put(stored)
	return stored, true, nil
}

// duplicateBodyLocked answers the committed body of one indexed event for a
// duplicate commit check: the cache first, then the persisted entry. The caller
// holds the lock.
func (s *FileSegmentStore) duplicateBodyLocked(ctx context.Context, key event.EventID, position indexPosition) ([]byte, bool, error) {
	if cached, held := s.cache.get(key); held {
		return append([]byte(nil), cached.Body...), true, nil
	}
	return s.readBodyLocked(ctx, position)
}

// readBodyLocked reads the persisted body of one indexed event. The caller
// holds the lock.
func (s *FileSegmentStore) readBodyLocked(ctx context.Context, position indexPosition) ([]byte, bool, error) {
	return readBodyAt(ctx, s.kv, position)
}

// readBodyAt reads the persisted body of one indexed event from kv.
func readBodyAt(ctx context.Context, kv AtomicStore, position indexPosition) ([]byte, bool, error) {
	pairs, err := kv.Read(ctx, Fetch{Key: fmt.Sprintf("%d:evt:%d:%d", position.partition, position.window, position.seq)})
	if err != nil {
		return nil, false, err
	}
	if len(pairs) == 0 {
		return nil, false, nil
	}
	return append([]byte(nil), pairs[0].Value...), true, nil
}

// indexPositionOf parses one stored index entry value into the position the
// resident index keeps: the window and the sequence slot the event was committed
// at beside the frozen type, the session and the commit sequence the commit filed
// with it.
func indexPositionOf(partition int, value []byte) (indexPosition, bool) {
	fields := strings.SplitN(string(value), ":", 3)
	if len(fields) < 2 {
		return indexPosition{}, false
	}
	window, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return indexPosition{}, false
	}
	seq, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return indexPosition{}, false
	}
	position := indexPosition{partition: partition, window: window, seq: seq}
	if len(fields) == 3 {
		var carried segmentIndexCarried
		if json.Unmarshal([]byte(fields[2]), &carried) == nil {
			position.typ = carried.Type
			position.session = carried.Session
			position.commit = carried.Commit
		}
	}
	return position, true
}

// segmentEventSlot splits one stored event body key into the position it occupies
// and answers false for a key of another family.
func segmentEventSlot(key string) (positionSlot, bool) {
	fields := strings.Split(key, ":")
	if len(fields) != 4 || fields[1] != "evt" {
		return positionSlot{}, false
	}
	partition, err := strconv.Atoi(fields[0])
	if err != nil {
		return positionSlot{}, false
	}
	window, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return positionSlot{}, false
	}
	seq, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return positionSlot{}, false
	}
	return positionSlot{partition: partition, window: window, seq: seq}, true
}

// segmentMetaSlot splits one window meta key into the window it describes and
// answers false for a key of another family.
func segmentMetaSlot(key string) (windowSlot, bool) {
	fields := strings.Split(key, ":")
	if len(fields) != 3 || fields[1] != "meta" {
		return windowSlot{}, false
	}
	partition, err := strconv.Atoi(fields[0])
	if err != nil {
		return windowSlot{}, false
	}
	window, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return windowSlot{}, false
	}
	return windowSlot{partition: partition, window: window}, true
}

// coldViewLocked answers the opened cold journal, opening it on the first read
// that needs it and never at boot. A store with no cold root answers false beside
// no error: it relocates nothing, so nothing of it is cold. A store whose hot
// journal states no settled commit answers false as well, because no fact can be
// in the cold journal before a pass moved one there, so a store that never
// relocated opens nothing it would read empty. The caller holds the lock.
func (s *FileSegmentStore) coldViewLocked(ctx context.Context) (AtomicStore, bool, error) {
	if s.coldRoot == "" || s.coldCommit == 0 {
		return nil, false, nil
	}
	if s.cold != nil {
		return s.cold, true, nil
	}
	cold, err := OpenAtomicStore(ctx, StorageConfig{Type: StorageJournal, Root: s.coldRoot})
	if err != nil {
		return nil, false, fmt.Errorf("tagent: open the cold journal %s: %w", s.coldRoot, err)
	}
	s.cold = cold
	return cold, true, nil
}

// coldEventLocked answers one event the hot journal no longer holds, from the cold
// journal: the bytes that were committed and the position they were committed at,
// so a relocation never changes what a read by identity answers. It answers false
// beside no error for an identity neither journal holds — a key no commit ever
// landed, or a commit that was refused — and it names ErrSegmentBodyMissing for a
// cold index entry whose body cannot be read, instead of reporting the event as an
// absent one. The caller holds the lock.
func (s *FileSegmentStore) coldEventLocked(ctx context.Context, key event.EventID) (StoredEvent, bool, error) {
	cold, held, err := s.coldViewLocked(ctx)
	if err != nil || !held {
		return StoredEvent{}, false, err
	}
	namespaces, err := cold.Namespaces(ctx)
	if err != nil {
		return StoredEvent{}, false, fmt.Errorf("tagent: enumerate the cold journal %s: %w", s.coldRoot, err)
	}
	for _, namespace := range namespaces {
		partition, err := strconv.Atoi(namespace)
		if err != nil {
			continue
		}
		pairs, err := cold.Read(ctx, Fetch{Key: fmt.Sprintf("%d:%s%s", partition, segmentIndexPrefix, key)})
		if err != nil {
			return StoredEvent{}, false, err
		}
		if len(pairs) == 0 {
			continue
		}
		position, named := indexPositionOf(partition, pairs[0].Value)
		if !named {
			return StoredEvent{}, false, fmt.Errorf("%w: the cold index entry of %s", ErrSnapshotCorrupt, key)
		}
		body, found, err := readBodyAt(ctx, cold, position)
		if err != nil {
			return StoredEvent{}, false, err
		}
		if !found {
			return StoredEvent{}, false, fmt.Errorf("%w: %s", ErrSegmentBodyMissing, key)
		}
		return StoredEvent{
			Key:         key,
			PartitionID: position.partition,
			WindowTS:    position.window,
			Seq:         position.seq,
			Body:        body,
		}, true, nil
	}
	return StoredEvent{}, false, nil
}

// ErrColdArchiveUnavailable reports a relocation requested on a store that cannot
// express one: no cold journal is declared, or the engine behind the store keeps no
// rewrite. The refusal is by name rather than a silent no-op, so a deployment that
// declared retirement never reads a bounded store as an unbounded one.
var ErrColdArchiveUnavailable = errors.New("memory: this store keeps no cold journal to relocate settled facts into")

// WithColdRoot declares the directory of the cold journal this store reads the
// settled facts of and relocates them into. A store opened without one keeps every
// fact in its hot journal and refuses a relocation by name.
func (s *FileSegmentStore) WithColdRoot(root string) *FileSegmentStore {
	s.coldRoot = root
	return s
}

// WithRetirement declares whether this store relocates its settled facts, which is
// the deployment's choice: the switch decides whether a pass runs and never whether
// a fact an earlier pass moved out stays readable, so a store that answers false
// here still reads its cold journal by identity and still refuses a relocation by
// name.
func (s *FileSegmentStore) WithRetirement(on bool) *FileSegmentStore {
	s.retire = on
	return s
}

// RetireFacts moves every settled fact of this store into the cold journal and
// rewrites the hot journal to hold the key set the caller states: an event the set
// does not name leaves the hot journal with its index entry, its body and the
// window meta no kept event of that window needs, and every other key of the store
// stays where it is, because a set of event keys names no fact of those families.
//
// The pass is copy, verify, rewrite in that order: the hot journal is rewritten
// only for a copy a fresh open reads back byte for byte, so a key is never removed
// before its bytes landed somewhere readable. Every settled fact stays readable by
// identity from the cold journal and EventCount keeps stating the whole committed
// history, so a bounded hot journal is never a store that lost its history.
//
// The kept key set is the caller's, never one this store derives: which facts the
// recovery authority still reads hot is its answer, and a store that invented its
// own would be a second oracle beside it. The set is what the watermark leaves
// behind — the tail past the checkpoint — because a fact the watermark settled
// stays readable from the cold journal by identity.
//
// A pass settles the keys it read in one snapshot and nothing else: a key a writer
// commits while the pass is deciding is kept hot, because a family whose reader
// cannot reach the cold journal — the task records a boot scans — would otherwise
// move away under the reader that needs it. The next pass reads that key in its own
// snapshot and settles it from there.
func (s *FileSegmentStore) RetireFacts(ctx context.Context, keepKeys map[event.EventID]struct{}) (RelocateReport, error) {
	if err := ctx.Err(); err != nil {
		return RelocateReport{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.coldRoot == "" || !s.retire {
		return RelocateReport{}, ErrColdArchiveUnavailable
	}
	retiring, held := s.kv.(coldRetirement)
	if !held {
		return RelocateReport{}, fmt.Errorf("%w: the engine %T states no journal rewrite", ErrColdArchiveUnavailable, s.kv)
	}
	settle, settled, coldCommit, err := s.keepKeysLocked(ctx, keepKeys)
	if err != nil {
		return RelocateReport{}, err
	}
	if len(settle) == 0 {
		return RelocateReport{}, nil
	}
	retired := s.retired + settled
	report, err := retiring.retireSettled(ctx, s.coldRoot, settle, []journalPut{
		{Key: segmentSettledKey, Value: []byte(strconv.Itoa(retired))},
		{Key: segmentSettledCommitKey, Value: []byte(strconv.FormatInt(coldCommit, 10))},
	})
	if err != nil {
		return report, err
	}
	index := make(map[event.EventID]indexPosition, len(keepKeys))
	order := make([]event.EventID, 0, len(s.order))
	windows := make(map[string]*storedWindowMeta, len(s.windows))
	for _, key := range s.order {
		position, published := s.index[key]
		if !published {
			continue
		}
		if _, kept := keepKeys[key]; !kept {
			continue
		}
		index[key] = position
		order = append(order, key)
		if meta, held := s.windows[windowKey(position.partition, position.window)]; held {
			windows[windowKey(position.partition, position.window)] = meta
		}
	}
	s.index = index
	s.order = order
	s.windows = windows
	s.retired = retired
	s.coldCommit = coldCommit
	// The pass appended the settled keys to the cold journal, so a view opened
	// before it answers the index it read then: the next cold read opens the journal
	// that holds the bytes this pass copied.
	s.cold = nil
	return report, nil
}

// originalFamilyEntry splits one stored original key into the event key it belongs
// to, and answers false for a key of another family. The reference and the
// independent body of one original are the two families a commit files beside a
// fact, and both name the fact they belong to.
func originalFamilyEntry(key string) (event.EventID, bool) {
	for _, prefix := range [2]string{"ref:", "blob:"} {
		if name, held := strings.CutPrefix(key, prefix); held && name != "" {
			return event.EventID(name), true
		}
	}
	return "", false
}

// keepKeysLocked answers the keys one relocation pass settles, from the one
// snapshot it reads, beside the number of settled index entries it drops and the
// greatest commit sequence it settles. An event the caller does not keep takes its
// index entry out with its body and, when no kept event of that window remains, the
// window meta beside them; every key of another family is not settled at all,
// because a set of event keys says nothing about a checkpoint count or a task
// record. A key a writer commits after this snapshot is not in the answer, so the
// pass leaves it hot for the next one. The caller holds the lock.
func (s *FileSegmentStore) keepKeysLocked(ctx context.Context, keepKeys map[event.EventID]struct{}) ([]string, int, int64, error) {
	keys, err := s.kv.Keys(ctx, "")
	if err != nil {
		return nil, 0, 0, err
	}
	keptSlots := make(map[positionSlot]struct{}, len(keepKeys))
	keptWindows := make(map[windowSlot]struct{}, len(keepKeys))
	settledCommit := s.coldCommit
	for key := range keepKeys {
		position, published := s.index[key]
		if !published {
			continue
		}
		keptSlots[positionSlot{partition: position.partition, window: position.window, seq: position.seq}] = struct{}{}
		keptWindows[windowSlot{partition: position.partition, window: position.window}] = struct{}{}
	}
	settle := make([]string, 0, len(keys))
	settled := 0
	for _, key := range keys {
		if partition, id, named := segmentIndexEntry(key); named {
			position, published := s.index[id]
			if _, kept := keepKeys[id]; kept {
				continue
			}
			settle = append(settle, key)
			if published && position.partition == partition {
				settled++
				if position.commit > settledCommit {
					settledCommit = position.commit
				}
			}
			continue
		}
		if slot, named := segmentEventSlot(key); named {
			if _, kept := keptSlots[slot]; !kept {
				settle = append(settle, key)
			}
			continue
		}
		if slot, named := segmentMetaSlot(key); named {
			if _, kept := keptWindows[slot]; !kept {
				settle = append(settle, key)
			}
			continue
		}
		// An original of a fact this pass settles leaves with its fact: the reference
		// and the independent body are the bytes the fact was committed with, so a
		// hot journal that keeps them beside a cold fact keeps the settled history on
		// disk. The two families travel together, which is the pairing the cold copy
		// verification enforces. An original whose fact this store does not state
		// stays where it is: a key set of settled facts judges no fact it cannot name.
		if eventKey, named := originalFamilyEntry(key); named {
			if _, kept := keepKeys[eventKey]; kept {
				continue
			}
			if _, published := s.index[eventKey]; published {
				settle = append(settle, key)
			}
			continue
		}
	}
	return settle, settled, settledCommit, nil
}

// EventCount returns the number of events published by successful commits, so
// the count moves only where durable success was reported.
func (s *FileSegmentStore) EventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// IndexEntries reports the size of the resident index the load published: the
// number of positions the store keeps in memory. It is the quantity a bounded
// recovery is measured by, and never EventCount, which counts the whole committed
// history: a store that publishes every commit it holds answers the same number
// twice, which is exactly the unbounded recovery the measure has to be able to
// tell apart from a bounded one.
func (s *FileSegmentStore) IndexEntries() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.index)
}

// EachEvent streams every published event in index order: partition first, then
// the window it is grouped under, then the sequence slot it takes inside that
// window, with the event key as the last tie-break. The body of each event is
// read on demand and handed to visit alone, so an enumeration never holds the
// whole store in memory. A visit error ends the enumeration and is returned.
func (s *FileSegmentStore) EachEvent(ctx context.Context, visit func(StoredEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	positions := s.snapshotPositions()
	for _, key := range positions.order {
		if err := ctx.Err(); err != nil {
			return err
		}
		stored, held, err := s.eventByKey(ctx, key)
		if err != nil {
			return err
		}
		if !held {
			return fmt.Errorf("%w: %s", ErrSegmentBodyMissing, key)
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	// The settled facts: a store that relocated part of its history still answers
	// every event it committed, so a consumer that reads the whole history — the
	// delivery scan that reconciles what the chain settled — walks them after the
	// resident ones, each answer carrying the position it was committed at. An
	// identity the hot journal also states is walked once, at its resident place:
	// one identity names one immutable fact, and a copy a relocation left in the
	// cold journal is that same fact rather than a second one.
	relocated, err := s.coldEventKeys(ctx)
	if err != nil {
		return err
	}
	for _, key := range relocated {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.published(key) {
			continue
		}
		stored, held, err := s.coldEvent(ctx, key)
		if err != nil {
			return err
		}
		if !held {
			return fmt.Errorf("%w: %s", ErrSegmentBodyMissing, key)
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	return nil
}

// coldEvent answers one event from the cold journal under the store lock.
func (s *FileSegmentStore) coldEvent(ctx context.Context, key event.EventID) (StoredEvent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coldEventLocked(ctx, key)
}

// coldEventKeys answers the identities the cold journal holds, in the index order
// the store enumerates events in. It is the enumeration a consumer that reads the
// whole history pays, never a boot: the cold view is opened here only for a caller
// that asked for every event.
func (s *FileSegmentStore) coldEventKeys(ctx context.Context) ([]event.EventID, error) {
	entries, err := s.coldIndex(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].position, entries[j].position
		if a.partition != b.partition {
			return a.partition < b.partition
		}
		if a.window != b.window {
			return a.window < b.window
		}
		if a.seq != b.seq {
			return a.seq < b.seq
		}
		return entries[i].key < entries[j].key
	})
	out := make([]event.EventID, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.key)
	}
	return out, nil
}

// coldIndexEntry is one event the cold journal holds, with the position and the
// commit sequence it was committed at.
type coldIndexEntry struct {
	key      event.EventID
	position indexPosition
}

// coldIndex answers every event the cold journal holds, in no stated order. An
// entry the cold journal names without the position and the commit sequence a
// commit filed is refused by name: a settled fact whose rank cannot be read would
// otherwise slot into the newest-first walk at an arbitrary place.
func (s *FileSegmentStore) coldIndex(ctx context.Context) ([]coldIndexEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.coldIndexLocked(ctx)
}

// coldIndexLocked answers every event the cold journal holds. The caller holds the
// store lock.
func (s *FileSegmentStore) coldIndexLocked(ctx context.Context) ([]coldIndexEntry, error) {
	cold, held, err := s.coldViewLocked(ctx)
	if err != nil || !held {
		return nil, err
	}
	namespaces, err := cold.Namespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("tagent: enumerate the cold journal %s: %w", s.coldRoot, err)
	}
	entries := make([]coldIndexEntry, 0)
	for _, namespace := range namespaces {
		partition, err := strconv.Atoi(namespace)
		if err != nil {
			continue
		}
		prefix := fmt.Sprintf("%d:%s", partition, segmentIndexPrefix)
		pairs, err := cold.Read(ctx, Fetch{Prefix: prefix})
		if err != nil {
			return nil, err
		}
		for _, pair := range pairs {
			id, named := strings.CutPrefix(pair.Key, prefix)
			if !named || id == "" {
				continue
			}
			position, named := indexPositionOf(partition, pair.Value)
			if !named {
				return nil, fmt.Errorf("%w: the cold index entry of %s", ErrSnapshotCorrupt, pair.Key)
			}
			entries = append(entries, coldIndexEntry{key: event.EventID(id), position: position})
		}
	}
	return entries, nil
}

// segmentPositionOrder is one ordered enumeration of the published index.
type segmentPositionOrder struct {
	order []event.EventID
}

// snapshotPositions freezes the published index keys in index order, so a
// concurrent commit never reshapes an enumeration already under way.
func (s *FileSegmentStore) snapshotPositions() segmentPositionOrder {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]event.EventID, 0, len(s.index))
	for key := range s.index {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := s.index[keys[i]], s.index[keys[j]]
		if a.partition != b.partition {
			return a.partition < b.partition
		}
		if a.window != b.window {
			return a.window < b.window
		}
		if a.seq != b.seq {
			return a.seq < b.seq
		}
		return keys[i] < keys[j]
	})
	return segmentPositionOrder{order: keys}
}

// published reports whether the resident index names key, so a walk that reads
// both journals states an identity the hot journal holds once.
func (s *FileSegmentStore) published(key event.EventID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, held := s.index[key]
	return held
}

// eventByKey answers one event by its key under the store lock.
func (s *FileSegmentStore) eventByKey(ctx context.Context, key event.EventID) (StoredEvent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cached, held := s.cache.get(key); held {
		cached.Body = append([]byte(nil), cached.Body...)
		return cached, true, nil
	}
	position, held := s.index[key]
	if !held {
		return s.coldEventLocked(ctx, key)
	}
	body, found, err := s.readBodyLocked(ctx, position)
	if err != nil || !found {
		return StoredEvent{}, false, err
	}
	stored := StoredEvent{
		Key:         key,
		PartitionID: position.partition,
		WindowTS:    position.window,
		Seq:         position.seq,
		Body:        body,
	}
	s.cache.put(stored)
	return stored, true, nil
}

// NewestOfType answers the identity of the newest index entry of one frozen event
// type: the walk reads the store's commit order from the newest commit backwards
// and stops at the first entry of the type, so a lookup beside a checkpoint filed
// every cadence commits reads a bounded prefix of the order whatever the history
// behind it, copies and sorts nothing, and reads no event body. The answer spans
// partitions: an identity is newer than another because its commit landed later,
// never because its window, its sequence slot or its partition is higher, so a run
// whose owner moved between processes reads the checkpoint its newest process
// filed instead of the one of the highest partition.
//
// The order spans the whole history: an entry a relocation moved into the cold
// journal keeps the commit sequence it was committed with, so it is ranked beside
// the resident entries instead of behind them, and the cold journal is read only
// while it could hold an entry newer than the newest resident one.
//
// A type the store holds no entry of answers found=false and no error.
func (s *FileSegmentStore) NewestOfType(ctx context.Context, typ string) (event.EventID, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	answer := event.EventID("")
	held := false
	err := s.eachIndexEntryNewestFirst(ctx, func(key event.EventID, position indexPosition) (bool, error) {
		if position.typ != typ {
			return true, nil
		}
		answer, held = key, true
		return false, nil
	})
	if err != nil {
		return "", false, err
	}
	return answer, held, nil
}

// EachIndexNewestFirst streams the index of the whole history in commit order,
// newest commit first, handing each entry its key, position, frozen event type and
// session without reading any body. The order is the one the engine's records sit
// in, so it ranks every entry against every other — a commit of one partition that
// landed after a commit of another comes first — and a caller that wants commit
// order walks it backwards. The resident entries are read in place, one entry at a
// time, so a visit may read the store again; the settled entries are the ones a
// relocation moved into the cold journal, ranked into the same order by the commit
// sequence they carry. An entry whose type is empty leaves the caller to read its
// body.
func (s *FileSegmentStore) EachIndexNewestFirst(ctx context.Context, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error) error {
	return s.eachIndexEntryNewestFirst(ctx, func(key event.EventID, position indexPosition) (bool, error) {
		// A visit that answers an error ends the walk and answers it: a caller stops
		// a newest-first walk by returning the sentinel its own order names, so the
		// error is the stop and never a failure the walk swallows.
		if err := visit(key, position.partition, position.seq, position.typ, position.session); err != nil {
			return false, err
		}
		return true, nil
	})
}

// eachIndexEntryNewestFirst walks the index of the whole history in commit order,
// newest first, and stops where visit answers false. The resident entries and the
// settled ones are each in their own log's frame order, and a frame order ranks
// the records of one log alone: the merge takes the newer of the two heads by the
// commit sequence, so an entry a relocation moved out keeps the place its commit
// gave it.
//
// The cold journal is read only when it can change the answer: the settled commit
// the relocation left in the hot journal bounds every entry it moved, so a walk
// whose resident head is past that bound answers from the hot journal alone and
// opens no settled history.
func (s *FileSegmentStore) eachIndexEntryNewestFirst(ctx context.Context, visit func(key event.EventID, position indexPosition) (bool, error)) error {
	s.mu.Lock()
	total := len(s.order)
	coldBound := s.coldCommit
	s.mu.Unlock()
	var settled []coldIndexEntry
	readSettled := false
	at := total - 1
	next := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var key event.EventID
		var position indexPosition
		held := false
		if at >= 0 {
			key, position, held = s.orderEntry(at)
		}
		if !readSettled && (!held || position.commit <= coldBound) {
			// The settled entries can be newer than this resident one, so the two
			// orders have to be merged from here on.
			entries, err := s.coldIndex(ctx)
			if err != nil {
				return err
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].position.commit != entries[j].position.commit {
					return entries[i].position.commit > entries[j].position.commit
				}
				return entries[i].key > entries[j].key
			})
			settled, readSettled = entries, true
		}
		var settledHead *coldIndexEntry
		if readSettled && next < len(settled) {
			settledHead = &settled[next]
		}
		if !held && settledHead == nil {
			return nil
		}
		takeSettled := !held || (settledHead != nil && settledHead.position.commit > position.commit)
		var entryKey event.EventID
		var entry indexPosition
		if takeSettled {
			entryKey, entry = settledHead.key, settledHead.position
			next++
			// An identity the hot journal also states is one fact, and the resident
			// place it takes is the newest commit of it: the cold copy a relocation
			// left behind is skipped rather than ranked a second time.
			if s.published(entryKey) {
				continue
			}
		} else {
			entryKey, entry = key, position
			at--
		}
		more, err := visit(entryKey, entry)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
}

// orderEntry answers one entry of the resident commit order by its position, and
// answers false when the order holds no such position. The order names exactly the
// published index keys, and it only grows at its end while Load stays inside the
// constructor, so a walk reads the same entry at every position it took from the
// order while a visit reads the store again — nothing is copied to stay
// consistent.
func (s *FileSegmentStore) orderEntry(at int) (event.EventID, indexPosition, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if at >= len(s.order) {
		return "", indexPosition{}, false
	}
	key := s.order[at]
	return key, s.index[key], true
}

// Events returns every published event in index order. It is the materialized
// form of EachEvent for callers that need the whole set; the assembly read faces
// stream instead, one event at a time.
func (s *FileSegmentStore) Events(ctx context.Context) ([]StoredEvent, error) {
	var events []StoredEvent
	if err := s.EachEvent(ctx, func(stored StoredEvent) error {
		events = append(events, stored)
		return nil
	}); err != nil {
		return nil, err
	}
	return events, nil
}

// Close ends the store with a final barrier over whatever the backend still
// holds staged, and repeats as a no-op.
func (s *FileSegmentStore) Close() error {
	return s.kv.Commit(context.Background(), Batch{})
}

// nextWindowMeta returns the window meta the record leaves behind: one more
// event in the window, and the event time range widened to cover this one.
func (s *FileSegmentStore) nextWindowMeta(rec SegmentRecord) *storedWindowMeta {
	previous, held := s.windows[windowKey(rec.PartitionID, rec.WindowTS)]
	meta := &storedWindowMeta{PartitionID: rec.PartitionID, WindowTS: rec.WindowTS, MinTime: rec.EventTime, MaxTime: rec.EventTime}
	if held {
		meta.EventCount = previous.EventCount
		meta.MinTime = previous.MinTime
		meta.MaxTime = previous.MaxTime
		if rec.EventTime < meta.MinTime {
			meta.MinTime = rec.EventTime
		}
		if rec.EventTime > meta.MaxTime {
			meta.MaxTime = rec.EventTime
		}
	}
	meta.EventCount++
	return meta
}

// windowKey names one partition window in the store's window meta table.
func windowKey(partition int, window int64) string {
	return fmt.Sprintf("%d:%d", partition, window)
}

// SegmentPosition names one byte position inside a stored original, so one
// segmented read resumes exactly where the segment before it ended.
type SegmentPosition struct {
	// Offset is the byte offset from the start of the stored original.
	Offset int64
}

// SegmentRequest asks for one byte-bounded segment of a stored original: the
// byte position to resume from and the maximum number of bytes to hand back.
//
// The position is the continuation form: it is filled from the Next of the
// previous SegmentResult, so one original is consumed segment by segment and
// the segments of one original concatenate back to its exact bytes. The bound
// is in bytes and always finite: a zero MaxBytes is served as
// DefaultSegmentMaxBytes and never as an unbounded read, a negative MaxBytes is
// refused by ErrSegmentLimit, a negative offset by ErrSegmentOffset, and an
// offset past the end of the stored original by ErrSegmentPastEnd.
type SegmentRequest struct {
	// From is the byte position the segment starts at.
	From SegmentPosition
	// MaxBytes bounds the bytes one segment returns. Zero means
	// DefaultSegmentMaxBytes.
	MaxBytes int64
}

// SegmentResult is one byte-bounded segment of a stored original: the bytes of
// this segment, the byte position the next segment resumes from, and whether
// the end of the original was reached. Segments are cut at byte boundaries
// whatever the content holds, so a multi-byte character or a line ending may
// span two segments and the segments of one original concatenate back to its
// exact bytes. Next is the zero position when EOF is set and carries no
// resumption point then.
type SegmentResult struct {
	// Body holds the bytes of this segment.
	Body []byte
	// Next is the byte position the following segment resumes from.
	Next SegmentPosition
	// EOF reports that this segment reached the end of the stored original.
	EOF bool
}

// DefaultSegmentMaxBytes is the finite bound served when a SegmentRequest
// carries a zero MaxBytes, so a request that states no limit stays bounded
// instead of reading an original whole.
const DefaultSegmentMaxBytes int64 = 64 << 10

// ErrSegmentLimit reports a segment request carrying a negative byte limit, so
// no bounded segment could answer it.
var ErrSegmentLimit = errors.New("memory: segment request carries a negative byte limit")

// ErrSegmentOffset reports a segment request carrying a negative byte offset,
// so no position could name where the segment starts.
var ErrSegmentOffset = errors.New("memory: segment request carries a negative byte offset")

// segmentBody serves one byte-bounded segment of body: at most the requested
// number of bytes from the requested offset, a zero limit served as
// DefaultSegmentMaxBytes, and the resumption point of the next segment. An
// offset at the end of body returns an empty segment marked EOF, an offset past
// the end is refused by ErrSegmentPastEnd, and a malformed request is refused
// by its named error before any byte is chosen.
func segmentBody(body []byte, req SegmentRequest) (SegmentResult, error) {
	if req.From.Offset < 0 {
		return SegmentResult{}, ErrSegmentOffset
	}
	if req.MaxBytes < 0 {
		return SegmentResult{}, ErrSegmentLimit
	}
	limit := req.MaxBytes
	if limit == 0 {
		limit = DefaultSegmentMaxBytes
	}
	offset := req.From.Offset
	if offset > int64(len(body)) {
		return SegmentResult{}, ErrSegmentPastEnd
	}
	end := int64(len(body))
	if limit < end-offset {
		end = offset + limit
	}
	result := SegmentResult{Body: append([]byte(nil), body[offset:end]...)}
	if end < int64(len(body)) {
		result.Next = SegmentPosition{Offset: end}
		return result, nil
	}
	result.EOF = true
	return result, nil
}

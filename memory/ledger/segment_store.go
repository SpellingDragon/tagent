package ledger

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

// ErrSegmentBodyMissing reports an event the index names but whose persisted
// body cannot be read: the read names the failure instead of publishing the
// event as an absent one.
var ErrSegmentBodyMissing = errors.New("memory: committed event body is missing")

// indexPosition is the resident form of one stored index entry: the partition,
// window and sequence slot the event key was committed at. It is the small
// projection of the persisted {pid}:idx:{event_key} entry, so the index stays
// resident while event bodies do not.
type indexPosition struct {
	partition int
	window    int64
	seq       int64
	typ       string
	session   event.SessionID
}

// FileSegmentStore commits events into time-window segments over one atomic
// store engine; it names no engine and no file layout, so any engine backs it. One commit stages the event body, its index entry and the window meta
// the window needs as one batch, and publishes the event to the read cache, the
// resident index and the event count only when that commit returns success. A
// commit that fails names the barrier as its step, carries its cause, unstages
// what it wrote, and publishes no part of the record.
//
// The resident state is bounded: the index keeps one small position per event
// key (the projection of the persisted index entries), the read cache keeps
// bodies under a byte budget, and Load reads only the index and window meta at
// startup. An event body the cache evicted is read back from its persisted
// {pid}:evt:{window}:{seq} entry, so a bounded process still answers every read.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
type FileSegmentStore struct {
	mu      sync.Mutex
	kv      AtomicStore
	index   map[event.EventID]indexPosition
	cache   *segmentCache
	count   int
	windows map[string]*storedWindowMeta
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
// the published state, and EventCount counts the published records.
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
			value := string(pair.Value)
			fields := strings.SplitN(value, ":", 3)
			if len(fields) < 2 {
				continue
			}
			window, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				continue
			}
			seq, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}
			var typ string
			var session event.SessionID
			if len(fields) == 3 {
				var carried struct {
					Type    string          `json:"type"`
					Session event.SessionID `json:"session"`
				}
				if json.Unmarshal([]byte(fields[2]), &carried) == nil {
					typ = carried.Type
					session = carried.Session
				}
			}
			positions[event.EventID(eventKey)] = indexPosition{partition: partition, window: window, seq: seq, typ: typ, session: session}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index = map[event.EventID]indexPosition{}
	s.windows = map[string]*storedWindowMeta{}
	s.count = 0
	for key, position := range positions {
		if _, held := meta[windowSlot{partition: position.partition, window: position.window}]; !held {
			continue
		}
		s.index[key] = position
		s.count++
	}
	for slot, entry := range meta {
		carried := entry
		s.windows[windowKey(slot.partition, slot.window)] = &carried
	}
	return nil
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
	meta := s.nextWindowMeta(rec)
	evtKey := fmt.Sprintf("%d:evt:%d:%d", rec.PartitionID, rec.WindowTS, rec.Seq)
	idxKey := fmt.Sprintf("%d:idx:%s", rec.PartitionID, rec.EventKey)
	metaKey := fmt.Sprintf("%d:meta:%d", rec.PartitionID, rec.WindowTS)
	evtValue := append([]byte(nil), rec.Body...)
	idxMeta, err := json.Marshal(struct {
		Type    string          `json:"type"`
		Session event.SessionID `json:"session"`
	}{Type: rec.typ, Session: rec.session})
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
	s.index[rec.EventKey] = indexPosition{partition: rec.PartitionID, window: rec.WindowTS, seq: rec.Seq, typ: rec.typ, session: rec.session}
	s.cache.put(stored)
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
		return StoredEvent{}, false, nil
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
	pairs, err := s.kv.Read(ctx, Fetch{Key: fmt.Sprintf("%d:evt:%d:%d", position.partition, position.window, position.seq)})
	if err != nil {
		return nil, false, err
	}
	if len(pairs) == 0 {
		return nil, false, nil
	}
	return append([]byte(nil), pairs[0].Value...), true, nil
}

// EventCount returns the number of events published by successful commits, so
// the count moves only where durable success was reported.
func (s *FileSegmentStore) EventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
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
	return nil
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
		return StoredEvent{}, false, nil
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

// EachIndexNewestFirst streams the resident index in the reverse of the commit
// order, handing each entry its key, position, frozen event type and session
// without reading any body. A restart that only needs to find the newest fact
// of a given type walks the index alone; an entry whose type is empty leaves
// the caller to read its body.
func (s *FileSegmentStore) EachIndexNewestFirst(ctx context.Context, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	type slot struct {
		key       event.EventID
		partition int
		seq       int64
		typ       string
		session   event.SessionID
	}
	entries := make([]slot, 0, len(s.index))
	for key, position := range s.index {
		entries = append(entries, slot{key: key, partition: position.partition, seq: position.seq, typ: position.typ, session: position.session})
	}
	s.mu.Unlock()
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].partition != entries[j].partition {
			return entries[i].partition > entries[j].partition
		}
		if entries[i].seq != entries[j].seq {
			return entries[i].seq > entries[j].seq
		}
		return entries[i].key > entries[j].key
	})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visit(entry.key, entry.partition, entry.seq, entry.typ, entry.session); err != nil {
			return err
		}
	}
	return nil
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

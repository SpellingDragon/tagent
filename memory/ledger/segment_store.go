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
	"sync/atomic"

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

// ErrFactCommitUnsupported reports a tier this build cannot commit a whole fact
// through: the caller states the tier it was handed instead of committing one
// family beside a judgment it never made.
var ErrFactCommitUnsupported = errors.New("memory: the tier cannot commit a whole fact")

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
	// segmentIndexPrefix names the stored index entry of one event key: the read
	// path files every position under exactly this entry and reads it back from
	// there, so a store opens no second index beside the chain.
	segmentIndexPrefix = "idx:"
)

// ErrSegmentBodyMissing reports an event the index names but whose persisted
// body cannot be read: the read names the failure instead of publishing the
// event as an absent one.
var ErrSegmentBodyMissing = errors.New("memory: committed event body is missing")

// indexPosition is the decoded form of one stored index entry: the partition,
// window and sequence slot the event key was committed at beside the type, the
// session and the commit sequence the entry states. It is answered by a read of the
// entry and kept by no store between reads.
//
// commit is the store-wide commit sequence the commit of this entry was assigned.
// A window and a sequence slot number the events of one partition window, so they
// order two entries of one window and say nothing about an entry of another; the
// commit sequence orders every entry of the store, which is what a newest-first
// walk states and what ranks two entries of different partitions.
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

// FileSegmentStore commits events into time-window segments over one atomic
// store engine; it names no engine and no file layout, so any engine backs it. One commit stages the event body, its index entry, the window meta
// the window needs, the store's seat and the partition set as one batch, and
// publishes the event to the read cache and the event count only when that commit
// returns success. A commit that fails names the barrier as its step, carries its
// cause, unstages what it wrote, and publishes no part of the record.
//
// The resident state is bounded by the chain's shape and never by its history: the
// seat states the count and the commit sequence, the partition set states the
// partitions holding at least one event, and the read cache keeps bodies under a
// byte budget. Load reads the seat and the partition set and both are single keys,
// so opening a store reads no index entry whatever the history behind it. A point
// read probes the index entry of the identity in each stated partition and reads
// the value that entry spans, and a point read that names the partition probes
// that one entry and consults no set, so the set is an accelerator over the index
// and never the bound a caller that placed the event pays; a newest-first walk
// reads the commit order the engine states and stops where a caller stops; an
// enumeration reads the index of the partitions it names and therefore costs their
// entries. No window meta is held:
// the {pid}:meta:{window} key a window is filed under is the record, and the one
// commit that reads it reads that key.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
type FileSegmentStore struct {
	mu sync.Mutex
	kv AtomicStore
	// partitions are the partitions the chain states it holds an index entry in,
	// ascending: the set a point read of an identity that names no partition probes,
	// so a read of one event pays one small read per partition and never a walk of
	// the history. A read that names the partition its caller placed the event in
	// pays one read and never this set.
	partitions []int
	// entriesRead is the index entries this load enumerated: none beside a chain
	// that states its seat and its partition set, and the whole index beside a chain
	// that states neither.
	entriesRead int
	cache       *segmentCache
	// count is the events this store published: the whole committed history,
	// spelled in the seat of the chain, so an open states it without an aggregate
	// over the entries behind it.
	count int
	// commitSeq is the greatest commit sequence this store assigned, so the next
	// commit takes one past it and no commit ever shares the rank of another. It is
	// past EventCount when an entry whose window meta did not arrive stayed
	// unpublished, because such an entry states a commit the count does not report.
	commitSeq int64
	// indexWalkVisits is the index entries the commit-order walk named,
	// indexWalkValueBytes the value bytes it read to name them and indexWalkValueReads
	// the values those bytes came from. A boot states all three, because an index walk
	// that reads a value per held key and one that reads the index family alone name
	// the same entries at different costs, and only the values read tell them apart.
	indexWalkVisits     atomic.Int64
	indexWalkValueBytes atomic.Int64
	indexWalkValueReads atomic.Int64
}

// newFileSegmentStore opens the segment store held in kv and publishes the
// partitions the chain states, leaving every event body and every index position
// on the key-value seam until a read names one.
func newFileSegmentStore(kv AtomicStore) (*FileSegmentStore, error) {
	store := &FileSegmentStore{
		kv:    kv,
		cache: newSegmentCache(),
	}
	if err := store.Load(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

// Load states the count, the commit sequence and the partitions the chain holds
// events in, without reading one index entry and without returning an event body:
// one read of the seat states all three, so the open costs one small read whatever
// the history behind it.
//   - The seat states the count, the commit sequence and the partitions of the whole history with one read of one key, so neither the count nor the set of partitions a point read probes costs a reader an aggregate over the entries behind them
//   - A seat the chain does not state, states unreadably, states a commit sequence below its count, or states a count beside no partition falls back to the count and the partitions the index states: one enumeration of the index, which is the derived answer a chain that states no seat costs
//   - A store that writes again states the seat in the frame of its next commit, so a root whose seat could not be read is read without an aggregate from the next commit on
func (s *FileSegmentStore) Load(ctx context.Context) error {
	seat, seated, err := s.readSeat(ctx)
	if err != nil {
		return err
	}
	if !seated {
		return s.rederive(ctx, seat, false)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = seat.Count
	s.commitSeq = seat.CommitSeq
	s.partitions = append([]int(nil), seat.Partitions...)
	s.entriesRead = 0
	return nil
}

// rederive states the count, the commit sequence and the partitions from the index
// itself: every partition the chain names, and the entries of one partition the
// index states beside the window meta that publishes them. It is the one
// enumeration a chain that states no seat costs, and it keeps no entry: the three
// answers together are the whole of what it publishes.
func (s *FileSegmentStore) rederive(ctx context.Context, seat storeSeat, seated bool) error {
	partitions, err := s.indexPartitions(ctx)
	if err != nil {
		return err
	}
	entries, err := s.publishedEntriesOf(ctx, partitions)
	if err != nil {
		return err
	}
	count := len(entries)
	commitSeq := int64(count)
	for _, entry := range entries {
		if entry.position.commit > commitSeq {
			commitSeq = entry.position.commit
		}
	}
	if seated {
		count, commitSeq = seat.Count, seat.CommitSeq
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.count = count
	s.commitSeq = commitSeq
	s.partitions = partitions
	s.entriesRead = len(entries)
	return nil
}

// indexPartitions answers the partitions the chain files an index entry under:
// the integer namespaces its held keys carry. A key namespace states no key of its
// own, so the answer costs one enumeration of the namespaces and no index entry.
func (s *FileSegmentStore) indexPartitions(ctx context.Context) ([]int, error) {
	namespaces, err := s.kv.Namespaces(ctx)
	if err != nil {
		return nil, err
	}
	partitions := make([]int, 0, len(namespaces))
	for _, namespace := range namespaces {
		partition, err := strconv.Atoi(namespace)
		if err != nil {
			continue
		}
		partitions = append(partitions, partition)
	}
	sort.Ints(partitions)
	return partitions, nil
}

// indexEntry is one published index entry as an enumeration answers it: the event
// key beside the position it was committed at.
type indexEntry struct {
	key      event.EventID
	position indexPosition
}

// publishedEntriesOf answers every published index entry of the named partitions in
// index order: partition first, then the window it is grouped under, then the
// sequence slot it takes. It reads the index prefix of each partition and the
// window keys that publish it, and reads no window meta value and no event body, so
// the enumeration costs the entries of the partitions it names.
func (s *FileSegmentStore) publishedEntriesOf(ctx context.Context, partitions []int) ([]indexEntry, error) {
	entries := make([]indexEntry, 0, 16)
	for _, partition := range partitions {
		prefix := fmt.Sprintf("%d:%s", partition, segmentIndexPrefix)
		idxPairs, err := s.kv.Read(ctx, Fetch{Prefix: prefix})
		if err != nil {
			return nil, err
		}
		if len(idxPairs) == 0 {
			continue
		}
		windowKeys, err := s.kv.Keys(ctx, fmt.Sprintf("%d:meta:", partition))
		if err != nil {
			return nil, err
		}
		filed := make(map[windowSlot]struct{}, len(windowKeys))
		for _, key := range windowKeys {
			if slot, named := segmentMetaSlot(key); named {
				filed[slot] = struct{}{}
			}
		}
		for _, pair := range idxPairs {
			eventKey, held := strings.CutPrefix(pair.Key, prefix)
			if !held || eventKey == "" {
				continue
			}
			position, named := indexPositionOf(partition, pair.Value)
			if !named {
				continue
			}
			if _, published := filed[windowSlot{partition: position.partition, window: position.window}]; !published {
				continue
			}
			entries = append(entries, indexEntry{key: event.EventID(eventKey), position: position})
		}
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
	return entries, nil
}

// withPartition answers the ascending partition set carrying partition, which the
// commit that files the first entry of a partition states in its own frame.
func withPartition(partitions []int, partition int) []int {
	at := sort.SearchInts(partitions, partition)
	if at < len(partitions) && partitions[at] == partition {
		return partitions
	}
	stated := make([]int, 0, len(partitions)+1)
	stated = append(stated, partitions[:at]...)
	stated = append(stated, partition)
	return append(stated, partitions[at:]...)
}

// readSeat answers the count, the commit sequence and the partitions this store
// states in its seat, and whether the chain states a seat this build can read: one
// read of the seat key, decoded. A seat that is absent, that does not decode, that
// names a negative count, that names a commit sequence below its count, or that
// names events beside no partition is answered false, so the caller counts the
// entries the index states instead of trusting a seat it cannot read. A read that
// failed answers its error, because a store that could not read a key states no
// count at all.
func (s *FileSegmentStore) readSeat(ctx context.Context) (storeSeat, bool, error) {
	pairs, err := s.kv.Read(ctx, Fetch{Key: storeSeatKey})
	if err != nil {
		return storeSeat{}, false, err
	}
	if len(pairs) == 0 {
		return storeSeat{}, false, nil
	}
	var seat storeSeat
	if err := json.Unmarshal(pairs[0].Value, &seat); err != nil {
		return storeSeat{}, false, nil
	}
	if seat.Count < 0 || seat.CommitSeq < int64(seat.Count) {
		return storeSeat{}, false, nil
	}
	if seat.Count > 0 && len(seat.Partitions) == 0 {
		return storeSeat{}, false, nil
	}
	return seat, true, nil
}

// storeSeatKey names the chain key the store states its own count under: the one
// value of the whole committed history, committed in the frame of every event
// family, so a reader of the count reads one key and never an aggregate.
const storeSeatKey = "store:seat"

// storeSeat is the value of storeSeatKey: the events the store published, the
// greatest commit sequence it assigned, and the partitions it filed an index entry
// in. Both scalars travel, because an entry whose window meta did not arrive leaves
// the sequence past the count, and the partitions travel beside them because the
// frame of every event family states the seat: a point read of an identity probes
// the partitions this one key names instead of enumerating the index behind them.
type storeSeat struct {
	Count      int   `json:"count"`
	CommitSeq  int64 `json:"commitSeq"`
	Partitions []int `json:"partitions"`
}

// publishedPositionLocked answers the position of the event filed under key, and
// whether the store published one: the index entry the chain holds for that
// identity in one of the partitions it states, beside the window meta of the
// window the entry names. It probes one key per partition, so the answer costs one
// small read per partition and never the entries behind them. The caller holds the
// lock.
func (s *FileSegmentStore) publishedPositionLocked(ctx context.Context, key event.EventID) (indexPosition, bool, error) {
	for _, partition := range s.partitions {
		position, held, err := s.partitionPositionLocked(ctx, partition, key)
		if err != nil || held {
			return position, held, err
		}
	}
	return indexPosition{}, false, nil
}

// partitionPositionLocked answers the position of the published index entry one
// partition files under key, and false beside an entry that partition does not
// hold or one whose window meta does not publish. The caller holds the lock.
func (s *FileSegmentStore) partitionPositionLocked(ctx context.Context, partition int, key event.EventID) (indexPosition, bool, error) {
	pairs, err := s.kv.Read(ctx, Fetch{Key: fmt.Sprintf("%d:%s%s", partition, segmentIndexPrefix, key)})
	if err != nil {
		return indexPosition{}, false, err
	}
	if len(pairs) == 0 {
		return indexPosition{}, false, nil
	}
	position, named := indexPositionOf(partition, pairs[0].Value)
	if !named {
		return indexPosition{}, false, nil
	}
	return s.publishedPosition(ctx, position)
}

// publishedPosition answers position beside the window meta that publishes it: an
// entry whose window states no meta is not a published record, so a read of it
// answers absence instead of a body the count never covered.
func (s *FileSegmentStore) publishedPosition(ctx context.Context, position indexPosition) (indexPosition, bool, error) {
	pairs, err := s.kv.Read(ctx, Fetch{Key: windowMetaKey(position.partition, position.window)})
	if err != nil {
		return indexPosition{}, false, err
	}
	if len(pairs) == 0 {
		return indexPosition{}, false, nil
	}
	return position, true, nil
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
// committed before the event reaches the read cache, the partition set, the
// event count or the caller.
//
//   - A repeat of the committed record returns success without a second fact, other bytes under the same event key are refused, and a failed commit leaves nothing published.
//   - A fact the store holds is a committed fact: committing it again answers as the duplicate it is, never as a second landing of one identity.
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
	plan, err := s.planEventLocked(ctx, rec)
	if err != nil {
		return err
	}
	if plan.duplicate {
		return nil
	}
	if err := s.kv.Commit(ctx, Batch{Puts: plan.puts}); err != nil {
		return fmt.Errorf("%w: %w", ErrSegmentBarrier, err)
	}
	s.publishEventLocked(plan)
	return nil
}

// planEventLocked answers the event family of one commit: the pairs one batch
// carries, or the duplicate the store already holds under the record's identity.
// The caller checks the record's own fields and holds the lock.
func (s *FileSegmentStore) planEventLocked(ctx context.Context, rec SegmentRecord) (*segmentEventPlan, error) {
	prior, held, err := s.heldPositionLocked(ctx, rec)
	if err != nil {
		return nil, err
	}
	if held {
		if prior.partition != rec.PartitionID || prior.window != rec.WindowTS || prior.seq != rec.Seq {
			return nil, ErrContentConflict
		}
		existing, found, err := s.duplicateBodyLocked(ctx, rec.EventKey, prior)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrSegmentBodyMissing, rec.EventKey)
		}
		if !bytes.Equal(existing, rec.Body) {
			return nil, ErrContentConflict
		}
		return &segmentEventPlan{duplicate: true}, nil
	}
	meta, err := s.nextWindowMeta(ctx, rec)
	if err != nil {
		return nil, err
	}
	evtKey := fmt.Sprintf("%d:evt:%d:%d", rec.PartitionID, rec.WindowTS, rec.Seq)
	idxKey := fmt.Sprintf("%d:idx:%s", rec.PartitionID, rec.EventKey)
	metaKey := windowMetaKey(rec.PartitionID, rec.WindowTS)
	evtValue := append([]byte(nil), rec.Body...)
	commit := s.commitSeq + 1
	idxMeta, err := json.Marshal(segmentIndexCarried{Type: rec.typ, Session: rec.session, Commit: commit})
	if err != nil {
		return nil, fmt.Errorf("tagent: encode the index type of %s: %w", rec.EventKey, err)
	}
	idxValue := []byte(fmt.Sprintf("%d:%d:%s", rec.WindowTS, rec.Seq, idxMeta))
	metaValue, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}
	seat, err := s.seatOfCommitLocked(ctx, rec, commit)
	if err != nil {
		return nil, err
	}
	seatValue, err := json.Marshal(seat)
	if err != nil {
		return nil, fmt.Errorf("tagent: encode the seat of the store: %w", err)
	}
	return &segmentEventPlan{
		puts: []Pair{
			{Key: evtKey, Value: evtValue},
			{Key: idxKey, Value: idxValue},
			{Key: metaKey, Value: metaValue},
			{Key: storeSeatKey, Value: seatValue},
		},
		record: rec,
		commit: commit,
		value:  evtValue,
	}, nil
}

// seatOfCommitLocked answers the seat one commit states: the count one past the
// greater of the count this process published and the count the seat states, the
// commit sequence of the greater of this commit's rank and the sequence the seat
// states, and the partitions of every frame the log already holds beside the one
// this commit lands, so a frame that landed without its barrier naming the outcome
// stays counted and stays stated instead of leaving the seat of the next frame. The
// caller holds the lock.
//   - The frame writes the union of the partitions the store published in this process, the partitions the seat states and the record's own, because a commit publishes nothing to the process state when its barrier names no outcome, and a seat recomputed from that state alone drops the partition an uncertain commit landed in
//   - The seat is read here and written back in the same batch, so the set it states only ever gains a partition, and the write costs one small read whatever the history behind it
func (s *FileSegmentStore) seatOfCommitLocked(ctx context.Context, rec SegmentRecord, commit int64) (storeSeat, error) {
	next := storeSeat{Count: s.count + 1, CommitSeq: commit, Partitions: withPartition(append([]int(nil), s.partitions...), rec.PartitionID)}
	stated, seated, err := s.readSeat(ctx)
	if err != nil {
		return storeSeat{}, err
	}
	if !seated {
		return next, nil
	}
	if stated.Count+1 > next.Count {
		next.Count = stated.Count + 1
	}
	if stated.CommitSeq > next.CommitSeq {
		next.CommitSeq = stated.CommitSeq
	}
	for _, partition := range stated.Partitions {
		next.Partitions = withPartition(next.Partitions, partition)
	}
	return next, nil
}

// heldPositionLocked answers the published position the store holds the record's
// event under, and whether it holds one at all: the record's own partition first,
// so a repeat of a committed fact costs one small read, then every other partition
// the chain states, so an identity filed under another partition is the conflict it
// was. The caller holds the lock.
func (s *FileSegmentStore) heldPositionLocked(ctx context.Context, rec SegmentRecord) (indexPosition, bool, error) {
	position, held, err := s.partitionPositionLocked(ctx, rec.PartitionID, rec.EventKey)
	if err != nil || held {
		return position, held, err
	}
	for _, partition := range s.partitions {
		if partition == rec.PartitionID {
			continue
		}
		position, held, err := s.partitionPositionLocked(ctx, partition, rec.EventKey)
		if err != nil || held {
			return position, held, err
		}
	}
	return indexPosition{}, false, nil
}

// publishEventLocked files one committed event family into the read cache, the
// partition set and the event count.
func (s *FileSegmentStore) publishEventLocked(plan *segmentEventPlan) {
	rec := plan.record
	s.partitions = withPartition(s.partitions, rec.PartitionID)
	s.cache.put(StoredEvent{
		Key:         rec.EventKey,
		PartitionID: rec.PartitionID,
		WindowTS:    rec.WindowTS,
		Seq:         rec.Seq,
		Body:        plan.value,
	})
	s.commitSeq = plan.commit
	s.count++
}

// segmentEventPlan is the event family of one whole-fact commit: the pairs the one
// batch carries and the position the commit publishes once its barrier returned.
type segmentEventPlan struct {
	// puts are the pairs the commit writes for this family.
	puts []Pair
	// duplicate reports that the store already holds this very record.
	duplicate bool
	// record, commit and value are what the publication files.
	record SegmentRecord
	commit int64
	value  []byte
}

// FactCommit is one fact's whole key set: the record that places it and the
// original bytes the recall face reads back. The two name one fact, so the
// record's event key and the reference's key carry one identity.
type FactCommit struct {
	// Record is the segment record the fact commits as.
	Record SegmentRecord
	// Ref names the original the fact commits.
	Ref OriginalRef
	// Body is the original bytes the reference names.
	Body []byte
}

// FactCommitOutcome names what one whole-fact commit left in the store.
type FactCommitOutcome int

const (
	// FactCommitted reports a fact that landed whole and took one sequence slot.
	FactCommitted FactCommitOutcome = iota
	// FactAlreadyPresent reports a fact the store already held whole: no key moved and no slot was taken.
	FactAlreadyPresent
	// FactConflict reports an identity the store holds under another content: no key moved.
	FactConflict
	// FactUncertain reports a commit whose durable outcome the store cannot name: only an open that took no part in it can decide what landed.
	FactUncertain
	// FactRefused reports a fact that landed no key, with the error naming the cause.
	FactRefused
)

// CommitFact commits one fact of one tier as one unit: the original family and the
// event family travel in one batch, so a reader meets the fact whole or not at all
// and neither family ever stands committed beside the other missing.
//   - The duplicate judgment of both families and the commit under it run in one critical section, and the storage root's exclusive write rights keep every other writer out of that section, so no two writers interleave the judgment and the landing of one fact
//   - A fact the store already holds stays the fact it is: no key moves, no sequence slot is taken and the window meta is not refreshed
//   - A refusal the engine decides before writing is reported as the refusal it is, never as an uncertain outcome the caller would re-read: the seam states that a commit returning an error published no part of its batch, so only ErrCommitUncertain leaves the caller a read to make, and every other failure names a fact that landed no key
//   - An uncertain outcome publishes nothing to either store's in-process state, because that view answers absence beside a batch that landed whole
//
// 契约: docs/wiki/memory/memory-architecture.md#event-shape
func CommitFact(ctx context.Context, tier *Tier, fact FactCommit) (FactCommitOutcome, error) {
	if err := ctx.Err(); err != nil {
		return FactRefused, err
	}
	if tier == nil || tier.Segments == nil {
		return FactRefused, fmt.Errorf("%w: no tier", ErrFactCommitUnsupported)
	}
	rec := fact.Record
	if rec.EventKey == "" {
		return FactRefused, ErrEventKeyRequired
	}
	if rec.Seq < 0 || rec.WindowTS < 0 {
		return FactRefused, ErrSegmentPosition
	}
	if fact.Ref.Key == "" {
		return FactRefused, ErrKeyRequired
	}
	originals, ok := tier.Originals.(*kvOriginalStore)
	if !ok {
		return FactRefused, fmt.Errorf("%w: %T", ErrFactCommitUnsupported, tier.Originals)
	}
	digest, length, err := checkOriginalRef(fact.Ref, fact.Body)
	if err != nil {
		return FactRefused, err
	}
	segments := tier.Segments
	segments.mu.Lock()
	defer segments.mu.Unlock()
	originals.mu.Lock()
	defer originals.mu.Unlock()
	eventPlan, err := segments.planEventLocked(ctx, rec)
	if err != nil {
		return factCommitOutcomeOf(err), err
	}
	if eventPlan.duplicate {
		return FactAlreadyPresent, nil
	}
	originalPlan, err := originals.planFactLocked(ctx, fact.Ref, fact.Body, digest, length)
	if err != nil {
		return factCommitOutcomeOf(err), err
	}
	puts := append([]Pair{}, eventPlan.puts...)
	if originalPlan != nil {
		puts = append(puts, originalPlan.puts...)
	}
	if err := segments.kv.Commit(ctx, Batch{Puts: puts}); err != nil {
		return factCommitOutcomeOfCommit(err), fmt.Errorf("%w: %w", ErrSegmentBarrier, err)
	}
	segments.publishEventLocked(eventPlan)
	if originalPlan != nil {
		originals.publishFactLocked(originalPlan)
	}
	return FactCommitted, nil
}

// factCommitOutcomeOf classifies one judgment the plan of a family refused before
// any key of the fact was written.
func factCommitOutcomeOf(err error) FactCommitOutcome {
	if errors.Is(err, ErrContentConflict) {
		return FactConflict
	}
	return FactRefused
}

// factCommitOutcomeOfCommit classifies one commit the storage seam failed: the
// seam states that a commit returning an error published no part of its batch and
// that a durable outcome it cannot name answers ErrCommitUncertain, so the uncertain
// answer is the one outcome a caller must read back and every other failure is a
// refusal that landed no key — whichever name the engine gave it.
func factCommitOutcomeOfCommit(err error) FactCommitOutcome {
	if errors.Is(err, ErrCommitUncertain) {
		return FactUncertain
	}
	return FactRefused
}

// GetEvent returns the committed event under key together with the index
// position it was committed at, and reports whether any is published. The
// position is the index entry of that identity in one of the partitions the chain
// states, so the answer costs one small read per partition and no walk of the
// history behind them. A body the cache evicted is read back from its persisted
// entry; an index entry whose body cannot be read reports ErrSegmentBodyMissing
// instead of an absent event.
//
//   - This read serves the callers an identity reaches without a partition: the seat is the only index such a caller has, because enumerating the index behind it is the walk the seat replaces, so the answer is exactly as wide as the partitions the seat states and the seat that under-states one answers absence beside a body the log holds.
//   - A caller that holds the partition SHALL name it with GetEventInPartition, which addresses that partition's index entry and consults no seat.
func (s *FileSegmentStore) GetEvent(ctx context.Context, key event.EventID) (StoredEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return StoredEvent{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	position, held, err := s.publishedPositionLocked(ctx, key)
	if err != nil || !held {
		return StoredEvent{}, false, err
	}
	return s.storedAtLocked(ctx, key, position)
}

// GetEventInPartition returns the committed event under key that one named
// partition files, and reports whether that partition published one. The read
// addresses the index entry of the identity in the partition the caller names, so
// it pays one small read and probes no partition set. A caller that names the
// partition pays that read whatever the seat states. A body the cache evicted is
// read back from its persisted entry; an index entry whose body cannot be read
// reports ErrSegmentBodyMissing instead of an absent event.
func (s *FileSegmentStore) GetEventInPartition(ctx context.Context, key event.EventID, partition int) (StoredEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return StoredEvent{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	position, held, err := s.partitionPositionLocked(ctx, partition, key)
	if err != nil || !held {
		return StoredEvent{}, false, err
	}
	return s.storedAtLocked(ctx, key, position)
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

// storedAtLocked answers one event at a position the caller already resolved: the
// body from the read cache, or the persisted entry the position names. An index
// entry whose body cannot be read reports ErrSegmentBodyMissing, so a record the
// index names is never published as an absent one. The caller holds the lock.
func (s *FileSegmentStore) storedAtLocked(ctx context.Context, key event.EventID, position indexPosition) (StoredEvent, bool, error) {
	if cached, held := s.cache.get(key); held {
		cached.Body = append([]byte(nil), cached.Body...)
		return cached, true, nil
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

// indexPositionOf parses one stored index entry value into the position the entry
// states: the window and the sequence slot the event was committed at beside the
// frozen type, the session and the commit sequence the commit filed with it.
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

// EventCount returns the number of events published by successful commits, so
// the count moves only where durable success was reported.
func (s *FileSegmentStore) EventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// IndexEntriesRead reports the index entries this store's load enumerated: none
// beside a chain that states its seat and its partition set, and the whole index of
// every partition beside a chain that states neither. It is the quantity a bounded
// recovery is measured by, and never EventCount, which counts the whole committed
// history: a load that enumerated every entry answers the same number twice, which
// is exactly the unbounded recovery the measure has to be able to tell apart from a
// bounded one.
func (s *FileSegmentStore) IndexEntriesRead() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entriesRead
}

// IndexWalkReads reports what the commit-order walks of this store cost: the index
// entries they named, the value bytes they read to name them and how many values
// those bytes were. It is the
// quantity a bounded boot is measured by after the entries stopped being
// materialized, because a walk that reads a value per held key and one that reads
// the index family alone name the same entries at different costs, and only the
// bytes tell the two apart. Both are counted by the walk, so a caller reads what
// the boot paid, never an estimate kept beside it.
func (s *FileSegmentStore) IndexWalkReads() (visits int, valueBytes int64, valueReads int) {
	return int(s.indexWalkVisits.Load()), s.indexWalkValueBytes.Load(), int(s.indexWalkValueReads.Load())
}

// EachEvent streams every published event in index order: partition first, then
// the window it is grouped under, then the sequence slot it takes inside that
// window, with the event key as the last tie-break. The body of each event is
// read on demand and handed to visit alone, so an enumeration never holds the
// whole store in memory. A visit error ends the enumeration and is returned.
//
// It reads the index of every partition the chain states, so the enumeration costs
// the entries of the whole history: that is the cost of the question, and no
// narrower answer names every event.
func (s *FileSegmentStore) EachEvent(ctx context.Context, visit func(StoredEvent) error) error {
	return s.EachEventOfPartitions(ctx, nil, visit)
}

// EachEventOfPartitions streams every published event of the named partitions in
// the index order EachEvent states, reading the index of those partitions alone. A
// nil or empty set names every partition the chain states. It is the face a scoped
// query reads: a caller that names the partitions it may read pays their entries
// rather than the entries of the whole chain.
//
// A partition the chain states no entry in answers nothing rather than a failure: a
// partition no commit filed an event under holds no event to present.
func (s *FileSegmentStore) EachEventOfPartitions(ctx context.Context, partitions []int, visit func(StoredEvent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := s.publishedEntriesOf(ctx, s.statedPartitions(partitions))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		stored, _, err := s.storedOfLocked(ctx, entry.key, entry.position)
		if err != nil {
			return err
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	return nil
}

// statedPartitions answers the partitions one enumeration reads: the ones the
// caller names, or every partition the chain states beside an empty set.
func (s *FileSegmentStore) statedPartitions(partitions []int) []int {
	if len(partitions) > 0 {
		stated := append([]int(nil), partitions...)
		sort.Ints(stated)
		return stated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.partitions...)
}

// storedOfLocked answers one event the enumeration already placed: its body from
// the read cache, or from the persisted entry the position names. The caller states
// the position, so the answer costs the body alone. It holds the lock.
func (s *FileSegmentStore) storedOfLocked(ctx context.Context, key event.EventID, position indexPosition) (StoredEvent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.storedAtLocked(ctx, key, position)
}

// NewestOfType answers the identity of the newest index entry of one frozen event
// type beside the partition that entry landed in: the walk reads the store's commit
// order from the newest commit backwards and stops at the first entry of the type,
// so a lookup beside a checkpoint filed every cadence commits reads a bounded
// prefix of the order whatever the history behind it, sorts nothing, and reads no
// event body.
//
//   - The answer spans partitions: an identity is newer than another because its commit landed later, never because its window, its sequence slot or its partition is higher, so a run whose owner moved between processes reads the checkpoint its newest process filed.
//   - The partition travels beside the identity because the walk already holds the position it answered, so the caller reads that body back with GetEventInPartition and pays one read instead of a probe of the partitions the seat states.
//   - A type the store holds no entry of answers found=false, partition 0 and no error.
func (s *FileSegmentStore) NewestOfType(ctx context.Context, typ string) (event.EventID, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, false, err
	}
	answer := event.EventID("")
	partition := 0
	held := false
	err := s.eachIndexEntryNewestFirst(ctx, func(key event.EventID, position indexPosition) (bool, error) {
		if position.typ != typ {
			return true, nil
		}
		answer, partition, held = key, position.partition, true
		return false, nil
	})
	if err != nil {
		return "", 0, false, err
	}
	return answer, partition, held, nil
}

// EachIndexNewestFirstUntil streams the index in commit order, newest commit
// first, and stops where the visit answers false: a point query reads the prefix
// its own answer bounds instead of the history behind it.
//
// The order is the one the engine's records sit in, so it ranks every entry
// against every other — a commit of one partition that landed after a commit of
// another comes first — and a caller that wants commit order walks it backwards.
// The walk streams the order the engine states, so it holds no entry between two
// visits and costs the commits it walks.
//
//   - false is the stop: the walk ends where the visit stands and answers nil, so a query that has its answer pays the commits down to it and not the ones behind it.
//   - An error ends the walk and is answered as the failure it is, so a stop is never spelled as a failure a caller has to recognize.
//   - A visit reads the store: the walk hands the engine back before every visit, so a visit reads one event body beside the walk and never waits on it.
func (s *FileSegmentStore) EachIndexNewestFirstUntil(ctx context.Context, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) (bool, error)) error {
	return s.eachIndexEntryNewestFirst(ctx, func(key event.EventID, position indexPosition) (bool, error) {
		return visit(key, position.partition, position.seq, position.typ, position.session)
	})
}

// EachIndexNewestFirst streams the index of the whole history in commit order,
// newest commit first, handing each entry its key, position, frozen event type and
// session without reading any body. It is the walk of a caller that names every
// entry — a replay, an export, a listing — and it holds no stop of its own, so a
// caller that reads a bounded prefix asks EachIndexNewestFirstUntil instead.
//
//   - A visit that answers an error ends the walk and answers it.
//   - A visit reads the store: the walk hands the engine back before every visit, so a visit reads one event body beside the walk and never waits on it.
func (s *FileSegmentStore) EachIndexNewestFirst(ctx context.Context, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error) error {
	return s.EachIndexNewestFirstUntil(ctx, func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) (bool, error) {
		if err := visit(key, partition, seq, typ, session); err != nil {
			return false, err
		}
		return true, nil
	})
}

// EachIndexNewestFirstFrom streams the index from the entry of one key onwards in the order EachIndexNewestFirstUntil states.
//   - a caller that already knows the key it is looking for pays its own commit and the ones behind it instead of every commit filed after it
//   - the walk starts where the engine's order-line index states the commit that last stated the key ended, which costs one probe of the identity index and one read of that index
//   - so the walk costs the history committed after the key nothing
//   - an engine that states no such index, a key this store holds no published entry of, and an entry the engine names no commit for all walk the index from the newest entry
//   - so a seek that cannot be placed costs the walk it always took and never an entry it skipped
func (s *FileSegmentStore) EachIndexNewestFirstFrom(ctx context.Context, from event.EventID, visit func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) (bool, error)) error {
	entryVisit := func(key event.EventID, position indexPosition) (bool, error) {
		return visit(key, position.partition, position.seq, position.typ, position.session)
	}
	seek, holds := s.kv.(commitOrderSeek)
	if !holds {
		return s.eachIndexEntryNewestFirst(ctx, entryVisit)
	}
	lineEnd, named, err := s.indexEntrySeek(ctx, seek, from)
	if err != nil {
		return err
	}
	if !named {
		return s.eachIndexEntryNewestFirst(ctx, entryVisit)
	}
	return s.walkIndexValues(entryVisit, func(step func(rawKey string, value []byte) (bool, error)) error {
		return seek.EachCommittedNewestFirstFrom(ctx, lineEnd, step)
	})
}

// indexEntrySeek answers the order byte a walk over the index enters at to reach one key's
// entry: the partition the store published that entry under, named to the engine so the
// engine's own identity index answers the commit, one small read per partition the seat
// states and never a walk of the entries behind them.
func (s *FileSegmentStore) indexEntrySeek(ctx context.Context, seek commitOrderSeek, from event.EventID) (int64, bool, error) {
	s.mu.Lock()
	position, held, err := s.publishedPositionLocked(ctx, from)
	s.mu.Unlock()
	if err != nil || !held {
		return 0, false, err
	}
	return seek.OrderLineEnd(ctx, fmt.Sprintf("%d:%s%s", position.partition, segmentIndexPrefix, from))
}

// indexKeyOrder is one engine whose walk names the held keys newest commit first and
// reads no value, so the store reads the value of the keys it names and never the
// value of the event, window-meta, seat and original families committed beside them.
//   - A walk that names keys alone is what holds the walk to the index family, because the engine cannot tell which key its caller wants a body of
type indexKeyOrder interface {
	eachCommittedNewestFirst(ctx context.Context, visit func(key string) (bool, error)) error
}

// eachIndexEntryNewestFirst walks the published index in commit order, newest
// first, and stops where visit answers false. The order is the one the engine's
// records sit in, so it ranks every entry against every other and costs the commits
// walked; an engine that states no commit order answers the index of every stated
// partition ranked by the commit sequence each entry carries, and the walk still
// stops where visit does.
//
//   - A visit that answers false ends the walk where it stands, so a caller reads the prefix its order bounds instead of the whole history.
//   - The walk reads the value of the index keys it names and no value of another family, so a key a commit filed beside an index entry is never read back by a walk over the index.
//   - indexWalkVisits, indexWalkValueBytes and indexWalkValueReads count what the walk cost: the entries it named, and the values and bytes it read to name them.
func (s *FileSegmentStore) eachIndexEntryNewestFirst(ctx context.Context, visit func(key event.EventID, position indexPosition) (bool, error)) error {
	if keys, holds := s.kv.(indexKeyOrder); holds {
		walked := map[event.EventID]struct{}{}
		return keys.eachCommittedNewestFirst(ctx, func(rawKey string) (bool, error) {
			partition, key, named := segmentIndexEntry(rawKey)
			if !named {
				return true, nil
			}
			if _, placed := walked[key]; placed {
				return true, nil
			}
			pairs, err := s.kv.Read(ctx, Fetch{Key: rawKey})
			if err != nil {
				return false, err
			}
			if len(pairs) == 0 {
				return false, fmt.Errorf("%w: the commit order named %s and the store holds no value for it", ErrJournalCorrupt, rawKey)
			}
			value := pairs[0].Value
			s.indexWalkValueBytes.Add(int64(len(value)))
			s.indexWalkValueReads.Add(1)
			position, named := indexPositionOf(partition, value)
			if !named {
				return true, nil
			}
			walked[key] = struct{}{}
			s.indexWalkVisits.Add(1)
			return visit(key, position)
		})
	}
	if ordered, holds := s.kv.(CommitOrder); holds {
		return s.walkIndexValues(visit, func(step func(rawKey string, value []byte) (bool, error)) error {
			return ordered.EachCommittedNewestFirst(ctx, step)
		})
	}
	entries, err := s.publishedEntriesOf(ctx, s.statedPartitions(nil))
	if err != nil {
		return err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].position.commit > entries[j].position.commit
	})
	for at := 0; at < len(entries); at++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.indexWalkVisits.Add(1)
		more, err := visit(entries[at].key, entries[at].position)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	return nil
}

// walkIndexValues names the index entries one engine's commit-order walk states, newest
// first, and hands each one the position it filed: the body every walk over the index
// family runs, so an entry point decides where the walk starts and never which entries it
// names. It reads the value of the index keys it names and counts what that cost.
func (s *FileSegmentStore) walkIndexValues(visit func(key event.EventID, position indexPosition) (bool, error), walk func(step func(rawKey string, value []byte) (bool, error)) error) error {
	walked := map[event.EventID]struct{}{}
	return walk(func(rawKey string, value []byte) (bool, error) {
		partition, key, named := segmentIndexEntry(rawKey)
		if !named {
			return true, nil
		}
		if _, placed := walked[key]; placed {
			return true, nil
		}
		s.indexWalkValueBytes.Add(int64(len(value)))
		s.indexWalkValueReads.Add(1)
		position, named := indexPositionOf(partition, value)
		if !named {
			return true, nil
		}
		walked[key] = struct{}{}
		s.indexWalkVisits.Add(1)
		return visit(key, position)
	})
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

// Close ends the store with a final barrier over whatever the backend still holds
// staged, releases the write rights this store's open took on its storage root and
// the journal handle behind them, and repeats as a no-op.
func (s *FileSegmentStore) Close() error {
	barrierErr := s.kv.Commit(context.Background(), Batch{})
	closer, ok := s.kv.(interface{ Close() error })
	if !ok {
		return barrierErr
	}
	return errors.Join(barrierErr, closer.Close())
}

// nextWindowMeta returns the window meta the record leaves behind: one more
// event in the window, and the event time range widened to cover this one. The
// meta the window was last committed under is the record of the chain under
// {pid}:meta:{window}, read here and nowhere else, so the store holds no window
// table that a window count could grow.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
func (s *FileSegmentStore) nextWindowMeta(ctx context.Context, rec SegmentRecord) (*storedWindowMeta, error) {
	meta := &storedWindowMeta{PartitionID: rec.PartitionID, WindowTS: rec.WindowTS, MinTime: rec.EventTime, MaxTime: rec.EventTime}
	key := windowMetaKey(rec.PartitionID, rec.WindowTS)
	pairs, err := s.kv.Read(ctx, Fetch{Key: key})
	if err != nil {
		return nil, err
	}
	if len(pairs) > 0 {
		var previous storedWindowMeta
		if err := json.Unmarshal(pairs[0].Value, &previous); err != nil {
			return nil, fmt.Errorf("%w: window meta %q: %w", ErrSnapshotCorrupt, key, err)
		}
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
	return meta, nil
}

// windowMetaKey names the chain key one partition window states its meta under.
func windowMetaKey(partition int, window int64) string {
	return fmt.Sprintf("%d:meta:%d", partition, window)
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

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/SpellingDragon/tagent/event"
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

// FileSegmentStore commits events into time-window segments over one key-value
// backend. One commit stages the event body, its index entry and the window meta
// the window needs as one batch, and publishes the event to the read cache and
// the event count only when that commit returns success. A commit that fails
// names the barrier as its step, carries its cause, unstages what it wrote, and
// publishes no part of the record. Readers are served from published state only,
// so a load publishes exactly the records whose body, index and meta all
// arrived.
//
// 契约: docs/wiki/memory/memory-architecture.md#file-segment-store
type FileSegmentStore struct {
	mu      sync.Mutex
	kv      kvPort
	cache   map[event.EventID]StoredEvent
	count   int
	windows map[string]*storedWindowMeta
}

// newFileSegmentStore opens the segment store held in kv and publishes every
// stored record whose body, index and meta all arrived.
func newFileSegmentStore(kv kvPort) (*FileSegmentStore, error) {
	store := &FileSegmentStore{
		kv:      kv,
		cache:   map[event.EventID]StoredEvent{},
		windows: map[string]*storedWindowMeta{},
	}
	if err := store.Load(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

// Load publishes every stored record whose body, index and meta all arrived, so
// a record one commit left incomplete never reaches a reader or a count.
func (s *FileSegmentStore) Load(ctx context.Context) error {
	pairs, err := s.kv.kvRead(ctx, kvFetch{})
	if err != nil {
		return err
	}
	bodies := map[positionSlot][]byte{}
	positions := map[event.EventID]positionSlot{}
	meta := map[windowSlot]storedWindowMeta{}
	for _, pair := range pairs {
		parts := strings.SplitN(pair.key, ":", 3)
		if len(parts) != 3 {
			continue
		}
		partition, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		switch parts[1] {
		case "evt":
			slot := strings.SplitN(parts[2], ":", 2)
			if len(slot) != 2 {
				continue
			}
			window, err := strconv.ParseInt(slot[0], 10, 64)
			if err != nil {
				continue
			}
			seq, err := strconv.ParseInt(slot[1], 10, 64)
			if err != nil {
				continue
			}
			bodies[positionSlot{partition: partition, window: window, seq: seq}] = pair.value
		case "idx":
			value := string(pair.value)
			at := strings.IndexByte(value, ':')
			if at < 0 {
				continue
			}
			window, err := strconv.ParseInt(value[:at], 10, 64)
			if err != nil {
				continue
			}
			seq, err := strconv.ParseInt(value[at+1:], 10, 64)
			if err != nil {
				continue
			}
			positions[event.EventID(parts[2])] = positionSlot{partition: partition, window: window, seq: seq}
		case "meta":
			window, err := strconv.ParseInt(parts[2], 10, 64)
			if err != nil {
				continue
			}
			var entry storedWindowMeta
			if err := json.Unmarshal(pair.value, &entry); err != nil {
				return fmt.Errorf("%w: window meta %q: %w", ErrSnapshotCorrupt, pair.key, err)
			}
			meta[windowSlot{partition: partition, window: window}] = entry
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, position := range positions {
		body, held := bodies[position]
		if !held {
			continue
		}
		if _, held := meta[windowSlot{partition: position.partition, window: position.window}]; !held {
			continue
		}
		s.cache[key] = StoredEvent{
			Key:         key,
			PartitionID: position.partition,
			WindowTS:    position.window,
			Seq:         position.seq,
			Body:        append([]byte(nil), body...),
		}
		s.count++
	}
	for slot, entry := range meta {
		carried := entry
		s.windows[windowKey(slot.partition, slot.window)] = &carried
	}
	return nil
}

// StoreEvent commits one event behind the segment commit barrier: the event
// body, its index entry and the window meta the window needs are staged as one
// batch and committed before the event reaches the read cache, the event count
// or the caller. A repeat of the committed record returns success without a
// second fact, other bytes under the same event key are refused, and a failed
// commit leaves nothing published.
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
	if prior, held := s.cache[rec.EventKey]; held {
		if prior.PartitionID != rec.PartitionID || prior.WindowTS != rec.WindowTS || prior.Seq != rec.Seq || !bytes.Equal(prior.Body, rec.Body) {
			return ErrContentConflict
		}
		return nil
	}
	meta := s.nextWindowMeta(rec)
	evtKey := fmt.Sprintf("%d:evt:%d:%d", rec.PartitionID, rec.WindowTS, rec.Seq)
	idxKey := fmt.Sprintf("%d:idx:%s", rec.PartitionID, rec.EventKey)
	metaKey := fmt.Sprintf("%d:meta:%d", rec.PartitionID, rec.WindowTS)
	evtValue := append([]byte(nil), rec.Body...)
	idxValue := []byte(fmt.Sprintf("%d:%d", rec.WindowTS, rec.Seq))
	metaValue, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	previousMeta, hadMeta := s.windowMetaValue(rec.PartitionID, rec.WindowTS)
	staged := []kvStaged{{key: evtKey}, {key: idxKey}, {key: metaKey, value: previousMeta, present: hadMeta}}
	batch := kvBatch{puts: []kvPair{
		{key: evtKey, value: evtValue},
		{key: idxKey, value: idxValue},
		{key: metaKey, value: metaValue},
	}}
	if err := s.kv.kvCommit(ctx, batch); err != nil {
		return s.abort(ctx, fmt.Errorf("%w: %w", ErrSegmentBarrier, err), staged)
	}
	s.cache[rec.EventKey] = StoredEvent{
		Key:         rec.EventKey,
		PartitionID: rec.PartitionID,
		WindowTS:    rec.WindowTS,
		Seq:         rec.Seq,
		Body:        evtValue,
	}
	s.count++
	s.windows[windowKey(rec.PartitionID, rec.WindowTS)] = meta
	return nil
}

// GetEvent returns the committed event under key together with the index
// position it was committed at, and reports whether any is published.
func (s *FileSegmentStore) GetEvent(ctx context.Context, key event.EventID) (StoredEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return StoredEvent{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, held := s.cache[key]
	if !held {
		return StoredEvent{}, false, nil
	}
	stored.Body = append([]byte(nil), stored.Body...)
	return stored, true, nil
}

// EventCount returns the number of events published by successful commits, so
// the count moves only where durable success was reported.
func (s *FileSegmentStore) EventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// Events returns every published event in index order: partition first, then
// the window it is grouped under, then the sequence slot it takes inside that
// window, with the event key as the last tie-break. Only records whose commits
// published are listed, so the enumeration never exposes a record a read
// cannot serve.
func (s *FileSegmentStore) Events(ctx context.Context) ([]StoredEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]StoredEvent, 0, len(s.cache))
	for _, stored := range s.cache {
		stored.Body = append([]byte(nil), stored.Body...)
		events = append(events, stored)
	}
	sort.Slice(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.PartitionID != b.PartitionID {
			return a.PartitionID < b.PartitionID
		}
		if a.WindowTS != b.WindowTS {
			return a.WindowTS < b.WindowTS
		}
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		return a.Key < b.Key
	})
	return events, nil
}

// Close ends the store with a final barrier over whatever the backend still
// holds staged, and repeats as a no-op.
func (s *FileSegmentStore) Close() error {
	return s.kv.kvCommit(context.Background(), kvBatch{})
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

// windowMetaValue returns the stored window meta of one partition window in its
// persisted form, when the store already holds one.
func (s *FileSegmentStore) windowMetaValue(partition int, window int64) ([]byte, bool) {
	previous, held := s.windows[windowKey(partition, window)]
	if !held {
		return nil, false
	}
	value, err := json.Marshal(previous)
	if err != nil {
		return nil, false
	}
	return value, true
}

// windowKey names one partition window in the store's window meta table.
func windowKey(partition int, window int64) string {
	return fmt.Sprintf("%d:%d", partition, window)
}

// abort rolls the staged writes of a failed commit back and returns the failure
// that caused it, joined with whatever the rollback itself reports.
func (s *FileSegmentStore) abort(ctx context.Context, cause error, staged []kvStaged) error {
	if err := kvUnstage(ctx, s.kv, staged); err != nil {
		return errors.Join(cause, fmt.Errorf("%w: %w", ErrUnstage, err))
	}
	return cause
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

// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// journalKV is the key-value seam of the localfile tier over its append-only
// journal. A commit appends one record, completes the log's barrier over it and
// only then publishes its values to the resident set, so a successful return
// means the record is durable and a commit whose write or barrier failed leaves
// the view answering exactly the records the file holds.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// journalKV is the localfile tier's key-value view over one journal: positions
// in the log are the persistent state, and memory residency is entry-level with
// a byte budget, so a cold value is evicted from memory and read back from the
// record that carries it.
type journalKV struct {
	mu sync.Mutex
	// log is the append-only record of every commit.
	log *journalLog
	// resident is the entry-level LRU: the keys and values the process holds.
	resident *kvResidentSet
	// lock is the write rights this open took on its storage root, nil beside a
	// read view. The view holds it from the open until Close, which is what makes
	// the duplicate check and the commit of one fact one critical section against
	// every other process.
	lock *rootWriteLock
	// closed reports whether Close ended this view, so a repeated Close releases
	// nothing a second time and a commit states that it met a closed view.
	closed bool
	// maintenanceErr names the maintenance failure that left this view unavailable.
	// The view refuses work until Close releases the root and a new view opens it.
	maintenanceErr error
	// hotLogMaxBytes is the byte budget the hot log is held to, as the config that
	// opened this view declared it. A budget of zero or less states no bound, so a
	// cut closes no segment and the log stays the one unsealed interval it is.
	hotLogMaxBytes int
	// archiveMaxAge is the maximum segment age the view carries for archive
	// selection. Zero states no window.
	archiveMaxAge time.Duration
	// cutRequested states whether the hot bytes stand over the effective limit, so a
	// caller can ask for a checkpoint cut earlier than its cadence. An observation of
	// the window a cut left clears it once that window stands under the limit.
	cutRequested bool
	// hotOverBudget states the current continuous over-budget interval: one while the
	// hot window stands over its effective limit or its configured limit stands below
	// the structural floor, and zero only once an observation of the window states
	// neither. A seal alone does not clear it, because a seal is not a resolved window.
	hotOverBudget int64
	// hotOverBudgetOpen states whether the interval hotOverBudget stands for has been
	// counted, so one continuous interval counts once however many commits it spans.
	hotOverBudgetOpen bool
	// archive locates the values the hot index does not state: the segments one
	// archive manifest carries, read when a key misses the hot index. It is nil
	// beside a view opened without an archive root, so every read of such a view is
	// the hot read it always was.
	archive *archiveReadLocator
	// activeWalks pins the current log while a cross-tier enumeration hands the view
	// to its callback; maintenance waits rather than closing the iterator's files.
	activeWalks int
	// maintenancePending records a cut whose maintenance waited for the last walk.
	maintenancePending bool
}

var _ AtomicStore = (*journalKV)(nil)

// ErrCommitWithoutWriteRights reports a commit carrying keys through a view that
// declared no write rights at open: one root carries one writer, so a view holding
// none reads beside that writer and its commit is refused by name before any byte
// is appended, never admitted as a second writer of the root.
var ErrCommitWithoutWriteRights = errors.New("memory: commit through a view that holds no write rights")

// ErrJournalMaintenanceFailed reports a journal view whose hot-log maintenance
// could not be recovered. The view refuses further work until Close releases the
// root and the storage tier is opened again.
var ErrJournalMaintenanceFailed = errors.New("memory: journal maintenance failed; close and reopen the storage tier")

// errArchiveMaintenanceDeferred reports archive maintenance an active walk holds
// back: the walk's end runs it, so a commit that meets it still landed whole.
var errArchiveMaintenanceDeferred = errors.New("memory: archive maintenance deferred until the active walk finishes")

// openJournalKV opens the key-value view a writer holds over the journal of dir: the
// view that declares the root's write rights.
func openJournalKV(dir string) (*journalKV, error) {
	return openJournalViewFace(dir, true, 0)
}

// openJournalView opens the key-value view over the journal of dir for the face the caller
// declares. The directory that holds the retired sharded-snapshot layout is refused by
// name, never migrated.
//   - A view holding the root's write rights opens the log for append and cuts a torn tail a stopped writer left
//   - A view holding none reads beside the writer, creates no file, cuts nothing, and refuses a commit carrying keys as ErrCommitWithoutWriteRights
func openJournalView(dir string, writeRights bool) (*journalKV, error) {
	return openJournalViewFace(dir, writeRights, 0)
}

// openJournalViewFace opens the key-value view over the journal of dir beside the hot
// log budget the caller declares, so the view that takes the root's write rights
// states a segment boundary for the cut that asks for one and a view beside no budget
// states none. It declares no archive root, so every key its hot index does not state
// is answered as one the view does not hold.
func openJournalViewFace(dir string, writeRights bool, hotLogMaxBytes int) (*journalKV, error) {
	return openJournalViewWithArchive(dir, writeRights, hotLogMaxBytes, "", 0)
}

// openJournalViewWithArchive opens the key-value view over the journal of dir beside
// the hot log budget, archive root and maximum segment age the caller declares.
//   - A key the hot index does not state is located in the segments the archive manifest carries, and the three failures of that read stay named: ErrOriginalMissing for a key no statement carries, ErrArchiveMissing for a segment or record the archive does not hold, and ErrArchiveCorrupt for bytes that fail their digest.
//   - An empty archive root states no archive, so the view reads the hot files alone and answers the keys they state.
//   - A zero maximum age states no time window.
func openJournalViewWithArchive(dir string, writeRights bool, hotLogMaxBytes int, archiveRoot string, archiveMaxAge time.Duration) (*journalKV, error) {
	if writeRights && archiveRoot != "" {
		if err := convergeArchiveStructure(dir, archiveRoot); err != nil {
			return nil, err
		}
	}
	log, err := openJournalLogFace(dir, writeRights)
	if err != nil {
		return nil, err
	}
	kv := &journalKV{log: log, resident: newKVResidentSet(), hotLogMaxBytes: hotLogMaxBytes, archiveMaxAge: archiveMaxAge}
	if archiveRoot != "" {
		kv.archive = &archiveReadLocator{root: archiveRoot, verified: map[string]archiveFileSignature{}}
	}
	return kv, nil
}

// Read returns the pairs the fetch names: the one value held under the key it
// names, or every held pair whose key carries the named prefix, ordered by key.
// A value the resident set evicted is read back from the record that carries it.
//   - A read through a view Close ended is refused as ErrReadAfterClose, never answered as a key the log holds no body for
func (k *journalKV) Read(ctx context.Context, fetch Fetch) ([]Pair, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return nil, err
	}
	if fetch.Key != "" {
		value, held, err := k.valueLocked(fetch.Key)
		if err != nil || !held {
			return nil, err
		}
		return []Pair{{Key: fetch.Key, Value: value}}, nil
	}
	if k.archive == nil {
		out := make([]Pair, 0, 16)
		err := k.log.eachHeld(fetch.Prefix, func(key string, span journalAddressSpan) error {
			value, err := k.valueOfSpanLocked(key, span)
			if err != nil {
				return err
			}
			out = append(out, Pair{Key: key, Value: value})
			return nil
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	held := make(map[string][]byte, 16)
	err := k.log.eachHeld(fetch.Prefix, func(key string, span journalAddressSpan) error {
		value, err := k.valueOfSpanLocked(key, span)
		if err != nil {
			return err
		}
		held[key] = value
		return nil
	})
	if err != nil {
		return nil, err
	}
	archived, err := k.archive.keys(fetch.Prefix)
	if err != nil {
		return nil, err
	}
	for _, key := range archived {
		if _, hot := held[key]; hot {
			continue
		}
		value, present, err := k.valueLocked(key)
		if err != nil {
			return nil, err
		}
		if present {
			held[key] = value
		}
	}
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]Pair, 0, len(keys))
	for _, key := range keys {
		out = append(out, Pair{Key: key, Value: held[key]})
	}
	return out, nil
}

// Keys returns the keys the prefix names, in key order, from the indexes alone: no
// value is read, so the enumeration a loader pays for stays small. A view that
// declares an archive root answers the keys the hot index states held beside the keys
// the archive holds, and a key either index's newest statement deletes is held by
// neither and is answered by no listing.
//   - An enumeration through a closed view is refused as ErrReadAfterClose, never answered as a store holding no key
func (k *journalKV) Keys(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return nil, err
	}
	if k.archive == nil {
		return k.log.keys(prefix)
	}
	return k.heldKeysLocked(prefix)
}

// Namespaces returns the key namespaces the held keys carry, without reading values.
// A view that declares an archive root counts the namespaces of the keys it holds
// across the hot index and the archive alike, so a namespace whose only keys left the
// hot window is still stated.
//   - An enumeration through a closed view is refused as ErrReadAfterClose, never answered as a store holding no namespace
func (k *journalKV) Namespaces(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil, ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return nil, err
	}
	if k.archive == nil {
		return k.log.namespaces()
	}
	keys, err := k.heldKeysLocked("")
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		seen[keyNamespaceOf(key)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out, nil
}

// heldKeysLocked answers every key this view holds under prefix, in key order, from the
// hot index and the archive indexes alike. A key the hot index states deleted is held
// by no interval even beside an archived value, and a key only the archive states is
// held unless the newest archived statement deletes it. No value is read. The caller
// holds the lock.
func (k *journalKV) heldKeysLocked(prefix string) ([]string, error) {
	stated := make(map[string]bool, 16)
	if err := k.hotKeyStatesLocked(prefix, func(key string, held bool) error {
		stated[key] = held
		return nil
	}); err != nil {
		return nil, err
	}
	archived, err := k.archive.keyStates(prefix)
	if err != nil {
		return nil, err
	}
	for key, held := range archived {
		if _, stated := stated[key]; stated {
			continue
		}
		stated[key] = held
	}
	out := make([]string, 0, len(stated))
	for key, held := range stated {
		if held {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

// hotKeyStatesLocked answers the state the hot index states for every key under prefix:
// true beside a key it holds and false beside one a commit deleted. A held record whose
// span is past the bytes this view states is no statement of the log, and the window of
// a read view states the newest commit of a key it names, so it answers in place of the
// table's record. The caller holds the lock.
func (k *journalKV) hotKeyStatesLocked(prefix string, visit func(key string, held bool) error) error {
	addr := k.log.addr
	if addr == nil {
		return nil
	}
	states := make(map[string]bool, addr.used)
	if addr.slots != nil {
		err := addr.eachRecord(func(state uint8, key string, span journalAddressSpan) error {
			if state == journalSlotEmpty || !strings.HasPrefix(key, prefix) {
				return nil
			}
			if state == journalSlotHeld && !addr.stated(span) {
				return nil
			}
			states[key] = state == journalSlotHeld
			return nil
		})
		if err != nil {
			return err
		}
	}
	if addr.window != nil {
		for key, value := range addr.window.values {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			states[key] = !value.deleted
		}
	}
	for key, held := range states {
		if err := visit(key, held); err != nil {
			return err
		}
	}
	return nil
}

// JournalBytesOpen reports the journal bytes this view's open replayed: the read
// cost of one boot at the engine layer, fixed at open and never the append offset
// the log grew to afterwards. It is read-only and takes no lock, because the open
// sets it before any caller holds the view, and it exists so a boot measure names
// the engine's own read instead of a copy kept beside it. A caller that holds the
// engine as an AtomicStore reads it through the interface the method states.
//
// The read it states is the frames past the watermark the index files state, so it
// is zero beside a root those files already reach: whether a log holds history is a
// fact of its length, and this count states the window the open replays into its
// index, not the integrity verification of the whole log.
func (k *journalKV) JournalBytesOpen() int64 {
	if k == nil || k.log == nil {
		return 0
	}
	return k.log.openReadBytes()
}

// IndexUnstatedNotice states why this view read the whole log instead of the commits
// past an index watermark: the index files beside the log state no watermark this build
// reads, so the open replayed every frame and the answer states the read that cost. It
// is empty beside a view whose index files state a watermark. The ledger package keeps
// no logger, so the open states the fallback here and the caller that holds one states it.
func (k *journalKV) IndexUnstatedNotice() string {
	if k == nil || k.log == nil || k.log.addr == nil {
		return ""
	}
	return k.log.addr.notice
}

// TornTailBytesOpen reports the byte of the log the last frame the open read starts at
// when that frame is not whole, and whether the log ended in a torn tail at all. The two
// answers are one measure because the byte alone cannot state the second: a log whose one
// frame is torn from its first byte stops at the zero a log of whole frames stops at.
//   - A view holding the root's write rights cut those bytes off the log at open, and a view holding none served the whole frames before them and cut nothing
func (k *journalKV) TornTailBytesOpen() (int64, bool) {
	if k == nil || k.log == nil {
		return 0, false
	}
	return k.log.tornTail, k.log.torn
}

// HotLogCutRequested reports whether the hot bytes stand over the effective byte
// limit of this view, so the caller can ask for a checkpoint cut earlier than its
// cadence. The observation a cut's seal takes clears the request once the window
// that seal and its maintenance left stands under the limit; the ledger keeps no
// logger, so the caller owns any warning this read raises.
func (k *journalKV) HotLogCutRequested() bool {
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.cutRequested
}

// HotLogOverBudget reports the current continuous over-budget interval: one while
// the hot window stands over its effective limit or the configured limit stands
// below the structural floor, and zero once an observation of the window states
// neither. A successful cut resolves the interval only when the window it leaves
// stands under both; a window below its floor keeps the interval standing for the
// caller to report.
func (k *journalKV) HotLogOverBudget() int64 {
	if k == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.hotOverBudget
}

// hotLogBytesLocked answers the physical bytes the hot root carries: the journal,
// every sealed segment and index still beside it, and the derived order/slot files.
func (k *journalKV) hotLogBytesLocked() (int64, error) {
	paths := []string{
		filepath.Join(k.log.dir, journalFileName),
		filepath.Join(k.log.dir, journalOrderFileName),
		filepath.Join(k.log.dir, journalSlotFileName),
		filepath.Join(k.log.dir, journalOrderLineFileName),
	}
	for _, segment := range k.log.segments {
		paths = append(paths, segment.path, journalSegmentIndexPathOf(segment))
	}
	return hotLogFileBytes(paths)
}

// hotLogFileBytes answers the bytes the named hot files carry. A file the root does
// not hold is no byte: a view that committed no fact holds no order file yet, which
// is a window of zero bytes rather than a failure to measure one.
func hotLogFileBytes(paths []string) (int64, error) {
	var total int64
	for _, path := range paths {
		stat, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("tagent: stat the hot log file %s: %w", path, err)
		}
		total += stat.Size()
	}
	return total, nil
}

// structuralFloorLocked answers the hot bytes no cut can reclaim: the newest sealed
// interval and its index, the journal file the open segment lives in, and the
// derived order and slot files. A move cuts the log in front of the newest interval
// it keeps, so the log keeps the newest interval beside the open segment whatever
// the deployment asks for; every other hot byte belongs to an older interval, whose
// segment file and whose log copy a cut can reclaim.
func (k *journalKV) structuralFloorLocked() (int64, error) {
	paths := []string{
		filepath.Join(k.log.dir, journalFileName),
		filepath.Join(k.log.dir, journalOrderFileName),
		filepath.Join(k.log.dir, journalSlotFileName),
		filepath.Join(k.log.dir, journalOrderLineFileName),
	}
	if len(k.log.segments) > 0 {
		newest := k.log.segments[len(k.log.segments)-1]
		paths = append(paths, newest.path, journalSegmentIndexPathOf(newest))
	}
	return hotLogFileBytes(paths)
}

// configuredHotLimitLocked answers the hot byte limit the deployment asked for: its
// retention window plus the two cut intervals the newest sealed segment may span,
// which are the intervals a window keeps hot whatever its retention.
func (k *journalKV) configuredHotLimitLocked() int64 {
	return int64(k.hotLogMaxBytes) + 2*k.maxSealedSegmentBytesLocked()
}

// maxSealedSegmentBytesLocked answers the largest log interval a cut has sealed,
// which is the observed unit the two-interval overhead is measured in.
func (k *journalKV) maxSealedSegmentBytesLocked() int64 {
	var largest int64
	for _, segment := range k.log.segments {
		if segment.bytes > largest {
			largest = segment.bytes
		}
	}
	return largest
}

// observeHotBudgetLocked states this view's hot-budget decision over the window it holds now.
//   - The effective limit is the larger of the configured limit and the structural floor, because no cut reclaims a floor byte: a deployment asking to hold a window under its floor is held to the floor instead.
//   - The early-cut request stands only while the hot bytes are over that effective limit, so a floor the deployment cannot reach asks for no cut of its own and leaves the cut cadence the deployment's.
//   - The over-budget interval stands while the hot bytes are over the effective limit or the configured limit stands below the floor, and is cleared only when an observation of the window states neither.
//   - It never rotates a segment: only a real checkpoint cut may do that, and an unreachable quiet point leaves the observation standing.
func (k *journalKV) observeHotBudgetLocked() error {
	if k.archive == nil || k.hotLogMaxBytes <= 0 || k.log == nil || k.maintenanceErr != nil {
		return nil
	}
	hotBytes, err := k.hotLogBytesLocked()
	if err != nil {
		return err
	}
	floor, err := k.structuralFloorLocked()
	if err != nil {
		return err
	}
	configured := k.configuredHotLimitLocked()
	limit := configured
	if floor > limit {
		limit = floor
	}
	over := hotBytes > limit
	k.cutRequested = over
	if over || configured < floor {
		if !k.hotOverBudgetOpen {
			k.hotOverBudget++
			k.hotOverBudgetOpen = true
		}
		return nil
	}
	k.hotOverBudget = 0
	k.hotOverBudgetOpen = false
	return nil
}

// journalRecordOf answers the one frame a commit unit is appended as: the puts copied
// beside the deletes of the batch and the commit time shared by the whole batch, under
// the format version this build writes.
func journalRecordOf(batch Batch, committedAtNS int64) journalRecord {
	record := journalRecord{Version: journalFormatVersion, CommittedAtNS: committedAtNS}
	for _, put := range batch.Puts {
		record.Puts = append(record.Puts, journalPut{Key: put.Key, Value: append([]byte{}, put.Value...)})
	}
	record.Deletes = append(record.Deletes, batch.Deletes...)
	return record
}

// Commit appends one record of the batch, completes the barrier over it and
// publishes its values once that barrier returned success. An empty batch
// carries nothing to append and only completes the barrier, which confirms what
// earlier commits appended. The caller holds no staged state across a failed
// append: the record is one frame, so it is all of the batch or none of it. A
// record the frame bound refuses is answered by name: that refusal is decided
// before the write, so it states a commit that landed no key rather than an
// uncertain one. Any other failed append or sync leaves the log's own state that
// the process cannot name, so the failure is reported as ErrCommitUncertain and
// the caller re-reads. That re-read answers absence for the keys of the failed
// batch even beside a batch that landed whole: a view publishes nothing from a
// commit it did not see return success, so an append whose barrier failed is
// reported and not served. A batch that carries keys through a view Close ended is
// refused by name instead, because the record it would append belongs to no
// writer of the root; an empty batch still completes the barrier, so a repeated
// Close stays a no-op.
//   - A batch carrying keys through a view holding no write rights is refused as ErrCommitWithoutWriteRights before any append
//   - A batch stating a cut seals the interval behind it, then observes the window the seal left: a window still over its effective limit moves the oldest eligible intervals in the same commit, so the bound the deployment asked for is the bound the commit returns under
//   - A seal or maintenance failure after the barrier leaves the published values in place: ErrCommitUncertain names the unfinished maintenance, not an uncommitted fact
//   - A maintenance an active walk defers leaves the batch committed and the maintenance pending, so the commit answers success and the walk that holds the log open runs it when it finishes
func (k *journalKV) Commit(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed && (len(batch.Puts) > 0 || len(batch.Deletes) > 0) {
		return ErrCommitAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return err
	}
	if len(batch.Puts) > 0 || len(batch.Deletes) > 0 {
		if k.log.addr != nil && k.log.addr.window != nil {
			return ErrCommitWithoutWriteRights
		}
		record := journalRecordOf(batch, time.Now().UnixNano())
		if err := k.log.append(record); err != nil {
			if errors.Is(err, ErrJournalFrameTooLarge) {
				return err
			}
			return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
		}
	}
	if err := k.log.sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
	}
	for _, put := range batch.Puts {
		k.resident.put(put.Key, append([]byte(nil), put.Value...))
	}
	for _, key := range batch.Deletes {
		k.resident.remove(key)
	}
	partition, seq, checkpoint := checkpointCutOf(batch)
	if k.hotLogMaxBytes > 0 && checkpoint {
		if err := k.log.sealSegment(partition, seq); err != nil {
			return fmt.Errorf("%w: tagent: the commit is on disk, but the log segment behind the checkpoint of partition %d at seq %d was not staked: %w", ErrCommitUncertain, partition, seq, err)
		}
		if err := k.observeHotBudgetLocked(); err != nil {
			return fmt.Errorf("%w: tagent: observe the hot log budget: %w", ErrCommitUncertain, err)
		}
		if k.cutRequested && k.archive != nil {
			if err := k.maintainAfterCutLocked(time.Now()); err != nil {
				if errors.Is(err, errArchiveMaintenanceDeferred) {
					return nil
				}
				return err
			}
			if err := k.observeHotBudgetLocked(); err != nil {
				return fmt.Errorf("%w: tagent: observe the hot log budget: %w", ErrCommitUncertain, err)
			}
		}
		return nil
	}
	if err := k.observeHotBudgetLocked(); err != nil {
		return fmt.Errorf("%w: tagent: observe the hot log budget: %w", ErrCommitUncertain, err)
	}
	return nil
}

// maintainAfterCutLocked moves the oldest eligible sealed intervals and rebuilds
// this view's hot log after a cut barrier. The caller holds k.mu, states the
// over-budget decision the observation of the sealed window took, and does not call
// Commit or Read through this view.
//   - The newest sealed interval and the open interval stay hot.
//   - A call inside an active walk answers errArchiveMaintenanceDeferred, marks the maintenance pending and leaves the log alone, so the walk's end runs it.
//   - A recovered failure reports ErrCommitUncertain and leaves the same view usable.
//   - An unrecovered failure reports ErrJournalMaintenanceFailed and refuses later work until Close and reopen.
func (k *journalKV) maintainAfterCutLocked(now time.Time) error {
	if k.archive == nil {
		return nil
	}
	if k.activeWalks > 0 {
		k.maintenancePending = true
		return errArchiveMaintenanceDeferred
	}
	manifest, _, err := readArchiveManifest(k.archive.root)
	if err != nil {
		return k.recoverAfterMaintenanceLocked(err, false)
	}
	candidates, err := archiveCandidatesAt(k.log.segments, manifest, now, k.archiveMaxAge)
	if err != nil {
		return k.recoverAfterMaintenanceLocked(err, false)
	}
	if len(candidates) == 0 {
		return nil
	}
	window, err := archiveWatermarkAfterMove(k.log, candidates)
	if err != nil {
		return k.recoverAfterMaintenanceLocked(err, false)
	}
	dir := k.log.dir
	if err := k.log.close(); err != nil {
		return k.recoverAfterMaintenanceLocked(err, true)
	}
	stored, err := moveSealedSegmentsToArchive(dir, k.archive.root, candidates, window)
	if err != nil {
		return k.recoverAfterMaintenanceLocked(err, true)
	}
	if err := shrinkHotIndexAfterArchive(dir, stored); err != nil {
		return k.recoverAfterMaintenanceLocked(err, true)
	}
	reopened, err := openJournalLogFace(dir, true)
	if err != nil {
		return k.recoverAfterMaintenanceLocked(err, true)
	}
	k.installLogLocked(reopened)
	return nil
}

// archiveCandidatesAt answers the longest contiguous prefix of sealed intervals
// older than the newest cut and absent from the manifest. An interval that fails
// the age window ends the prefix.
func archiveCandidatesAt(segments []journalSealedSegment, manifest *archiveManifest, now time.Time, age time.Duration) ([]journalSealedSegment, error) {
	stated, _, err := statedArchiveSegments(manifest)
	if err != nil {
		return nil, err
	}
	if len(segments) < 2 {
		return nil, nil
	}
	newest := segments[len(segments)-1].id
	candidates := make([]journalSealedSegment, 0, len(segments)-1)
	var previous int64
	for _, segment := range segments {
		if segment.id >= newest || stated[journalSegmentLabelOf(segment.id)] {
			continue
		}
		if len(candidates) > 0 && segment.id != previous+1 {
			break
		}
		if !segment.archiveMovableAt(now, age) {
			break
		}
		candidates = append(candidates, segment)
		previous = segment.id
	}
	return candidates, nil
}

// recoverAfterMaintenanceLocked closes the old handles when necessary, converges
// the archive structure and opens a fresh hot log. A fresh log is installed even
// when structural recovery fails, so Close still owns a handle it can release.
func (k *journalKV) recoverAfterMaintenanceLocked(cause error, logClosed bool) error {
	dir := k.log.dir
	if !logClosed {
		if err := k.log.close(); err != nil {
			cause = errors.Join(cause, err)
		}
	}
	k.log = nil
	k.resident = newKVResidentSet()
	recoverErr := convergeArchiveStructure(dir, k.archive.root)
	reopened, openErr := openJournalLogFace(dir, true)
	if openErr == nil {
		k.installLogLocked(reopened)
	}
	if recoverErr != nil || openErr != nil {
		k.maintenanceErr = errors.Join(cause, recoverErr, openErr)
		return fmt.Errorf("%w: %w: %w", ErrCommitUncertain, ErrJournalMaintenanceFailed, k.maintenanceErr)
	}
	k.maintenanceErr = nil
	return fmt.Errorf("%w: maintain the hot journal after cut: %w", ErrCommitUncertain, cause)
}

// installLogLocked replaces the whole log handle and drops resident entries and
// digest verification state belonging to the previous hot window.
func (k *journalKV) installLogLocked(log *journalLog) {
	k.log = log
	k.resident = newKVResidentSet()
	if k.archive != nil {
		k.archive.verified = map[string]archiveFileSignature{}
	}
}

// maintenanceFailureLocked answers the named unavailable-view error after an
// unrecovered maintenance failure. The caller holds k.mu.
func (k *journalKV) maintenanceFailureLocked() error {
	if k.maintenanceErr == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrJournalMaintenanceFailed, k.maintenanceErr)
}

// AppendOrdered enters one record of the batch in this view's append order without
// completing the barrier a commit does, so a caller that states a position in the
// order pays one frame and never a flush of the log.
//   - The order is all this face states: every record appended before it stays before it and every record appended after it lands behind it, so the barrier a later commit completes flushes this frame ahead of the frames that commit carries
//   - A successful return states the record is in the order, never that it landed durably: a stop before that later barrier may leave the frame absent or torn at the tail, and the open that reads a torn tail cuts it, so a reader answers the key as absent — the record is whole or not there, never partly applied
//   - No read face answers the record until a commit completes a barrier over the frame: the index states it behind that commit, so a view never serves a position its own barrier did not confirm
//   - An empty batch enters nothing and completes no barrier, because this face states no durability to confirm
//   - An ordered append states an unknown commit time, because this face confirms no commit of its own
//   - A batch carrying keys through a view Close ended is refused as ErrCommitAfterClose and one through a view holding no write rights as ErrCommitWithoutWriteRights, both before any byte is appended
//   - A record the frame bound refuses is answered by name before the write, and any other failed append answers ErrCommitUncertain, because the bytes it left are ones the process cannot name
func (k *journalKV) AppendOrdered(ctx context.Context, batch Batch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(batch.Puts) == 0 && len(batch.Deletes) == 0 {
		return nil
	}
	if k.closed {
		return ErrCommitAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return err
	}
	if k.log.addr != nil && k.log.addr.window != nil {
		return ErrCommitWithoutWriteRights
	}
	if err := k.log.append(journalRecordOf(batch, 0)); err != nil {
		if errors.Is(err, ErrJournalFrameTooLarge) {
			return err
		}
		return fmt.Errorf("%w: %w", ErrCommitUncertain, err)
	}
	return nil
}

// Close ends this view: it releases the journal handle and gives the storage root
// up to the next writer, and repeats as a no-op.
//   - The release is the point the root admits its next writer, so a commit after Close is refused by name rather than appended beside that writer
//   - A read view holds no write rights and releases those of the open it took, so closing one gives up nothing another process was waiting for
func (k *journalKV) Close() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return nil
	}
	k.closed = true
	var logErr error
	if k.log != nil {
		logErr = k.log.close()
	}
	return errors.Join(logErr, k.lock.release())
}

// valueOfSpanLocked answers the value one enumerated key states, from the resident
// set or from the span the enumeration already read, so a prefix read probes the
// table once per key instead of once per listing and once per value. The caller
// holds the lock.
func (k *journalKV) valueOfSpanLocked(key string, span journalAddressSpan) ([]byte, error) {
	if value, held := k.resident.get(key); held {
		return append([]byte{}, value...), nil
	}
	value, _, err := k.log.addr.value(span)
	if err != nil {
		return nil, err
	}
	k.resident.put(key, value)
	return append([]byte{}, value...), nil
}

// valueLocked answers one key's value from the resident set, reading it back
// from its record on a miss, and answers whether the view holds the key at all.
// A held value of zero bytes is a held key: its emptiness is the value the
// caller committed, not the absence of one. A key the hot index states absent is
// not held and does not fall through to an older archive; a key the hot index never
// states is located in the archive when this view declares one. A stated segment or
// record the archive cannot serve reports its own failure. The caller holds the lock.
func (k *journalKV) valueLocked(key string) ([]byte, bool, error) {
	if value, held := k.resident.get(key); held {
		return append([]byte{}, value...), true, nil
	}
	span, state, err := k.log.lookup(key)
	if err != nil {
		return nil, false, err
	}
	if state == journalSlotAbsent {
		return nil, false, nil
	}
	var value []byte
	var held bool
	if state == journalSlotHeld {
		value, held, err = k.log.addr.value(span)
		if err == nil && held {
			k.resident.put(key, value)
		}
	} else if k.archive != nil {
		value, held, err = k.archive.read(key)
		if errors.Is(err, ErrOriginalMissing) {
			return nil, false, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	if !held {
		return nil, false, nil
	}
	return append([]byte{}, value...), true, nil
}

// readsFromArchive reports whether key's hot slot is empty beside an archive
// declaration, so a caller can keep archive bytes out of its read cache.
func (k *journalKV) readsFromArchive(key string) bool {
	if k == nil {
		return false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed || k.maintenanceErr != nil || k.archive == nil || k.log == nil {
		return false
	}
	_, state, err := k.log.lookup(key)
	return err == nil && state == journalSlotEmpty
}

// eachCommittedNewestFirst names the held keys from the newest commit to the oldest
// and stops when visit answers false. It walks the commit order the engine's address
// states, so the answer costs the records walked and never a sort of the index; a key
// the newest commit that touched it deleted is not held and is skipped, and a key
// several commits rewrote is named once, at its newest commit. A view that declares an
// archive root walks the archived intervals behind the hot order on the same terms. The
// walk hands the view back before each visit, the way EachCommittedNewestFirst does, so
// a caller reads the value of a key it names beside the walk instead of under its lock.
func (k *journalKV) eachCommittedNewestFirst(ctx context.Context, visit func(key string) (bool, error)) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return err
	}
	stopped := false
	step := func(key string) (bool, error) {
		k.mu.Unlock()
		defer k.mu.Lock()
		more, err := visit(key)
		if err != nil || !more {
			stopped = true
			return more, err
		}
		return more, nil
	}
	if k.archive == nil {
		return k.log.eachKeyNewestFirst(step)
	}
	start := k.log.addr.orderSize
	hot, err := k.hotStatementsAtOrBelowLocked(start)
	if err != nil {
		return err
	}
	k.activeWalks++
	defer func() {
		if finishErr := k.finishArchiveWalkLocked(); finishErr != nil {
			err = errors.Join(err, finishErr)
		}
	}()
	if err := k.log.eachKeyNewestFirst(step); err != nil || stopped {
		return err
	}
	return k.eachArchivedNewestFirstLocked(ctx, hot, false, func(key string, _ []byte) (bool, error) {
		return visit(key)
	})
}

// commitOrderSeek is the capability of an engine whose commit order can be entered at the commit that last stated one key, instead of only from the newest commit.
//   - it is a capability and not a method every engine carries: an engine that keeps no commit order answers none of it
//   - a caller reads the answer through the type assertion, so a walk without it stays the walk it always was
//   - OrderLineEnd answers where the commit that last stated the key ends in the commit order, in one probe of the identity index and one read of the order-line index
//   - EachCommittedNewestFirstFrom names the held keys from that byte to the oldest on the terms EachCommittedNewestFirst states
//   - so an entry point decides what the walk reads and never what it answers
type commitOrderSeek interface {
	OrderLineEnd(ctx context.Context, key string) (int64, bool, error)
	EachCommittedNewestFirstFrom(ctx context.Context, from int64, visit func(key string, value []byte) (bool, error)) error
}

var _ commitOrderSeek = (*journalKV)(nil)

// OrderLineEnd answers the byte the order line of the commit that last stated one key ends
// at, and whether this view states one. A view that holds no order-line index, a key the
// index holds none of and a record outside the bytes this view states all answer false, so
// a caller walks the order file as it did before this index existed.
func (k *journalKV) OrderLineEnd(ctx context.Context, key string) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, false, err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return 0, false, ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return 0, false, err
	}
	return k.log.addr.orderLineOf(key)
}

// EachCommittedNewestFirstFrom names held keys from a hot commit through the archive.
//   - The full-history walk keeps the same snapshot and early-stop terms as EachCommittedNewestFirst.
func (k *journalKV) EachCommittedNewestFirstFrom(ctx context.Context, from int64, visit func(key string, value []byte) (bool, error)) error {
	return k.eachCommittedNewestFirstFrom(ctx, from, true, visit)
}

// EachHotCommittedNewestFirstFrom names held keys only in the hot order.
//   - Point lookups use this bound walk and fall back to identity reads when it finds no predecessor.
func (k *journalKV) EachHotCommittedNewestFirstFrom(ctx context.Context, from int64, visit func(key string, value []byte) (bool, error)) error {
	return k.eachCommittedNewestFirstFrom(ctx, from, false, visit)
}

func (k *journalKV) eachCommittedNewestFirstFrom(ctx context.Context, from int64, includeArchive bool, visit func(key string, value []byte) (bool, error)) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return err
	}
	start := k.log.addr.orderSize
	if from >= 0 && from < start {
		start = from
	}
	stopped := false
	step := func(key string, value []byte) (bool, error) {
		k.mu.Unlock()
		defer k.mu.Lock()
		more, err := visit(key, value)
		if err != nil || !more {
			stopped = true
			return more, err
		}
		return more, nil
	}
	if k.archive != nil {
		k.activeWalks++
		defer func() {
			if finishErr := k.finishArchiveWalkLocked(); finishErr != nil {
				err = errors.Join(err, finishErr)
			}
		}()
	}
	if k.archive == nil || !includeArchive {
		return k.log.addr.eachNewestFirstAtValue(from, step)
	}
	hot, err := k.hotStatementsAtOrBelowLocked(start)
	if err != nil {
		return err
	}
	if err := k.log.addr.eachNewestFirstAtValue(from, step); err != nil || stopped {
		return err
	}
	return k.eachArchivedNewestFirstLocked(ctx, hot, true, visit)
}

// EachCommittedNewestFirst names each held key once, newest commit first, with its newest value.
//   - A false visit result stops the walk before older records are read.
//   - An archive root extends the same snapshot walk behind the hot order.
//   - The view is unlocked during visits; maintenance waits for the walk to finish.
func (k *journalKV) EachCommittedNewestFirst(ctx context.Context, visit func(key string, value []byte) (bool, error)) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.closed {
		return ErrReadAfterClose
	}
	if err := k.maintenanceFailureLocked(); err != nil {
		return err
	}
	stopped := false
	step := func(key string, value []byte) (bool, error) {
		k.mu.Unlock()
		defer k.mu.Lock()
		more, err := visit(key, value)
		if err != nil || !more {
			stopped = true
			return more, err
		}
		return more, nil
	}
	if k.archive == nil {
		return k.log.eachKeyNewestFirstValue(step)
	}
	start := k.log.addr.orderSize
	hot, err := k.hotStatementsAtOrBelowLocked(start)
	if err != nil {
		return err
	}
	k.activeWalks++
	defer func() {
		if finishErr := k.finishArchiveWalkLocked(); finishErr != nil {
			err = errors.Join(err, finishErr)
		}
	}()
	if err := k.log.eachKeyNewestFirstValue(step); err != nil || stopped {
		return err
	}
	return k.eachArchivedNewestFirstLocked(ctx, hot, true, visit)
}

// eachArchivedNewestFirstLocked walks the archived statements behind the hot snapshot
// and hands each key to visit beside its newest archived value. The caller holds the
// lock, and the walk hands the view back before each visit.
//   - A key the hot snapshot states, whether held or deleted, is skipped so one key is named once.
//   - A hot statement committed after the snapshot does not hide the archived value this walk states.
func (k *journalKV) eachArchivedNewestFirstLocked(ctx context.Context, hot map[string]struct{}, readValues bool, visit func(key string, value []byte) (bool, error)) error {
	if k.archive == nil {
		return nil
	}
	return k.archive.eachNewestFirst(ctx, readValues, func(key string, value []byte) (bool, error) {
		if _, stated := hot[key]; stated {
			return true, nil
		}
		k.mu.Unlock()
		defer k.mu.Lock()
		return visit(key, value)
	})
}

// hotStatementsAtOrBelowLocked snapshots every key the hot index states at or below
// start, including tombstones. The snapshot is taken before a walk releases the lock,
// so later commits neither hide archived values nor rewrite the order being walked.
func (k *journalKV) hotStatementsAtOrBelowLocked(start int64) (map[string]struct{}, error) {
	stated := map[string]struct{}{}
	addr := k.log.addr
	if addr == nil {
		return stated, nil
	}
	if start < addr.orderSize {
		err := addr.eachStatementAtOrBelow(start, func(key string, _ bool) error {
			stated[key] = struct{}{}
			return nil
		})
		return stated, err
	}
	if addr.window != nil {
		for _, key := range addr.window.order {
			stated[key] = struct{}{}
		}
	}
	var err error
	if addr.slots != nil {
		err = addr.eachRecord(func(state uint8, key string, _ journalAddressSpan) error {
			if state != journalSlotEmpty {
				stated[key] = struct{}{}
			}
			return nil
		})
	}
	return stated, err
}

// finishArchiveWalkLocked releases one walk pin and runs maintenance that waited for
// the last walk. The caller holds the lock; a maintenance failure is returned to the
// walk that releases the pin.
func (k *journalKV) finishArchiveWalkLocked() error {
	k.activeWalks--
	if k.activeWalks != 0 || !k.maintenancePending {
		return nil
	}
	k.maintenancePending = false
	if err := k.maintainAfterCutLocked(time.Now()); err != nil {
		return err
	}
	return k.observeHotBudgetLocked()
}

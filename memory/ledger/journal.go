// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
//
// The journal is the localfile tier's persistent unit: one append-only file of
// frames, each carrying the length of its payload and a CRC32C of that payload.
// A frame that is fully present and matches its checksum is a committed record.
// A frame the writer left half-written is a torn tail, and only that tail is cut
// off at open. A complete frame whose checksum does not match is corruption, and
// it refuses the open: the records before it are never served as the whole log,
// because a silently accepted damaged frame is a fact that disappears.
package ledger

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

// ErrJournalCorrupt reports a frame that is fully present in the log and does
// not match the checksum written beside it.
var ErrJournalCorrupt = errors.New("memory: journal frame is corrupt")

// ErrJournalFormat reports a log written under a format version this build does
// not read.
var ErrJournalFormat = errors.New("memory: journal format version is not supported")

// ErrJournalBarrier reports a barrier that did not complete, so nothing staged
// in front of it is confirmed.
var ErrJournalBarrier = errors.New("memory: journal barrier failed")

const (
	// journalFileName is the file one journal directory holds: the whole log of
	// that directory, appended to and never rewritten.
	journalFileName = "journal.log"
	// journalFormatVersion is the format this build writes and the only one it
	// reads; any other value in a record refuses the open.
	journalFormatVersion = 1
	// journalFrameHeaderBytes is the fixed cost of one frame in front of its
	// payload: the payload length and the payload checksum.
	journalFrameHeaderBytes = 8
	// journalMaxFrameBytes bounds one payload, so a damaged length field cannot
	// make the reader allocate an unbounded span. The ceiling is headroom over
	// every payload a commit writes, never a payload size: the largest record a
	// commit files is a framework checkpoint, and a checkpoint states the projection
	// entries a fold leaves instead of the text behind them, which stands at
	// about 2 KiB beside 36 turns of 64 KiB bodies. The round-trip test pins a
	// frame near the ceiling, so a payload past it is refused rather than read.
	journalMaxFrameBytes = 32 << 20
	// journalReplayBufferBytes is the read size the replay takes the log in: one
	// sequential read per buffer instead of one read per frame part.
	journalReplayBufferBytes = 1 << 20
	// journalRewriteFileName is the file a retirement pass writes the kept keys
	// into before it replaces the log with it: a temporary file inside the same
	// directory, renamed over journal.log once it is whole, so a process that stops
	// during the rewrite leaves the log it started with.
	journalRewriteFileName = "journal.rewrite"
	// journalIndexCutFileName is the file beside the log that states the index the
	// open below it already read: the value span of every key the log holds before
	// the cut, in the commit order those records were filed in. It is a derived
	// carrier and never a second truth — a cut that is missing, unreadable or
	// behind the log is rebuilt from the log itself — and it exists so an open pays
	// the tail past the cut instead of the whole history behind it.
	journalIndexCutFileName = "journal.index"
	// journalIndexCutVersion is the cut format this build writes and the only one
	// it reads: another version is a cut this build cannot place, and the log is
	// read whole rather than trusted through a carrier it does not understand.
	journalIndexCutVersion = 1
)

// journalIndexCut is the index one open has already read, as the file beside the
// log states it: the byte the tail starts at, the size the cut was taken at, the
// key positions of everything before that byte and the commit order those
// positions were filed in.
type journalIndexCut struct {
	// Version is the cut format, and a cut of another version is not read.
	Version int `json:"version"`
	// Offset is the byte the open starts replaying at: every frame before it is
	// carried by Index and Order, so the open reads the tail alone.
	Offset int64 `json:"offset"`
	// Size is the log size the cut was taken at. A log shorter than the cut states
	// is a log the cut cannot place, and the open reads it whole.
	Size int64 `json:"size"`
	// Order is the commit order of the records before Offset, oldest first.
	Order []string `json:"order"`
	// Index locates the value of every key the records before Offset wrote.
	Index map[string]journalCutValue `json:"index"`
}

// journalCutValue is one key's position as the cut states it: the byte span of the
// value token the record carries, or the tombstone that deleted the key. It is the
// stated form of journalValue, which keeps its own fields unexported.
type journalCutValue struct {
	// Offset is the byte offset of the value's own JSON token.
	Offset int64 `json:"offset"`
	// Length is the byte length of that token, its quotes included.
	Length int `json:"length"`
	// Null reports a value the record wrote as JSON null.
	Null bool `json:"null,omitempty"`
	// Deleted reports a tombstone: the key is held by no record.
	Deleted bool `json:"deleted,omitempty"`
}

// value answers the position the cut states as the index keeps it.
func (v journalCutValue) value() journalValue {
	return journalValue{
		valueOffset: v.Offset, valueLength: v.Length, valueNull: v.Null, deleted: v.Deleted,
	}
}

// cutValue answers the position the index keeps as the cut states it.
func cutValue(value journalValue) journalCutValue {
	return journalCutValue{
		Offset: value.valueOffset, Length: value.valueLength, Null: value.valueNull, Deleted: value.deleted,
	}
}

// journalCRCTable is the checksum table of the frame format: Castagnoli, the
// CRC32C polynomial.
var journalCRCTable = crc32.MakeTable(crc32.Castagnoli)

// journalPut is one key and its value inside a committed record.
type journalPut struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

// journalRecord is the payload of one frame: the format version it was written
// under beside the writes it commits. One frame carries one commit, so a frame
// is all of a commit or none of it.
type journalRecord struct {
	Version int          `json:"version"`
	Puts    []journalPut `json:"puts,omitempty"`
	Deletes []string     `json:"deletes,omitempty"`
}

// journalValue locates one key's latest value: the byte span of the JSON token
// that carries it inside the frame of the commit that wrote it, or the tombstone
// that deleted it. One commit carries one batch, so a record-level position
// charges every read of a small key for the bytes of the frame beside it; the
// value span makes a read cost exactly the value it answers.
type journalValue struct {
	// valueOffset is the byte offset of the value's own JSON token in the log.
	valueOffset int64
	// valueLength is the byte length of that token, its quotes included.
	valueLength int
	// valueNull reports a value the commit wrote as JSON null.
	valueNull bool
	// deleted reports a tombstone: the key is held by no record.
	deleted bool
}

// RelocateReport counts what one retirement pass did: the keys it settled into the
// cold log, the keys the cold log already held with the same bytes, and the
// batched commits the copy took to land them.
type RelocateReport struct {
	// Settled is the number of keys the hot log no longer holds.
	Settled int
	// Skipped is the number of settled keys the cold log already held.
	Skipped int
	// Batches is the number of cold commits the copy took.
	Batches int
}

// journalLog is one append-only file of committed records: a single writer
// appends whole frames, and the key index is updated only after the write of the
// frame that carries the key returned success. The index holds positions, never
// values, so a log of any size costs the process one small entry per key. A
// frame is appended in one write, so a process that stops mid-write leaves a
// prefix of it, which is the torn tail the next open cuts off.
type journalLog struct {
	mu sync.Mutex
	// dir is the directory the log lives in.
	dir string
	// path is the log file itself.
	path string
	// file is the one writer's handle, opened for append.
	file *os.File
	// size is the offset the next frame starts at.
	size int64
	// openBytes is how many journal bytes the open replayed: the span it read
	// from the start of the file to the end of the file, torn tail included. It is
	// set once at open and never moves, so it measures the read one boot pays
	// instead of the offset the next append starts at (size, which follows every
	// write).
	openBytes int64
	// index locates the latest record of each key.
	index map[string]journalValue
	// order is the log's own commit order: the keys each committed record
	// touched, appended frame by frame in the order the frames sit in the log and,
	// inside one frame, in the order the record writes them. A key a later record
	// rewrites or deletes is named at every one of its records, newest last, so a
	// reader that walks it backwards meets the newest record of a key before any
	// older one.
	order []string
	// unsynced counts the bytes appended since the last completed barrier.
	unsynced int64
	// tail names the keys the records past the index cut wrote, and is nil beside a
	// log no cut covers: nil is a log the open read whole, which is the statement a
	// reader of the tail must not mistake for an empty tail.
	tail map[string]struct{}
	// cutOffset is the byte the index cut states: the frames at or past it are the
	// tail, and zero is the first frame of a log no cut covers.
	cutOffset int64
	// cutNotice states why the cut beside this log was not read, and is empty beside
	// a log whose cut was placed or which holds none. The ledger package keeps no
	// logger, so the open names the fallback here and the caller that holds a logger
	// states it.
	cutNotice string
}

// journalSpans is the index material one record states: the format version it was
// written under beside the value span of every write and the keys it deletes.
type journalSpans struct {
	version int
	puts    []journalPutSpan
	deletes []string
}

// journalPutSpan is one write of a record as the index files it: the key it
// names and the position of its own value token in the log.
type journalPutSpan struct {
	key   string
	value journalValue
}

// journalSpansAt reads the index material of one record payload, whose first
// byte sits at the given offset of the log. The value bytes are located, never
// copied: a replay of a log of any size walks its keys alone, so the cost of
// opening follows the record count instead of the bytes its values carry.
func journalSpansAt(payload []byte, at int64) (journalSpans, error) {
	var out journalSpans
	next := journalScanSpace(payload, 0)
	if next >= len(payload) || payload[next] != '{' {
		return out, errors.New("record carries no object")
	}
	for at2 := next + 1; ; {
		at2 = journalScanSpace(payload, at2)
		if at2 >= len(payload) {
			return out, errors.New("record object is unterminated")
		}
		if payload[at2] == '}' {
			return out, nil
		}
		if payload[at2] == ',' {
			at2++
			continue
		}
		name, after, err := journalScanString(payload, at2)
		if err != nil {
			return out, err
		}
		if at2, err = journalScanColon(payload, after); err != nil {
			return out, err
		}
		switch name {
		case "version":
			end, err := journalSkipValue(payload, at2)
			if err != nil {
				return out, err
			}
			if err := json.Unmarshal(payload[at2:end], &out.version); err != nil {
				return out, fmt.Errorf("read the record version: %w", err)
			}
			at2 = end
		case "puts":
			if at2, err = journalScanPuts(payload, at2, at, &out.puts); err != nil {
				return out, err
			}
		case "deletes":
			if at2, err = journalScanDeletes(payload, at2, &out.deletes); err != nil {
				return out, err
			}
		default:
			if at2, err = journalSkipValue(payload, at2); err != nil {
				return out, err
			}
		}
	}
}

// journalScanPuts reads the writes of one record from the array that opens at
// the given offset, answering the offset just past it.
func journalScanPuts(payload []byte, at2 int, at int64, out *[]journalPutSpan) (int, error) {
	if at2 >= len(payload) || payload[at2] != '[' {
		return 0, errors.New("puts carries no array")
	}
	for at2++; ; {
		at2 = journalScanSpace(payload, at2)
		if at2 >= len(payload) {
			return 0, errors.New("puts array is unterminated")
		}
		if payload[at2] == ']' {
			return at2 + 1, nil
		}
		if payload[at2] == ',' {
			at2++
			continue
		}
		if payload[at2] != '{' {
			return 0, errors.New("a put carries no object")
		}
		span := journalPutSpan{value: journalValue{valueNull: true}}
		for at2++; ; {
			var err error
			at2 = journalScanSpace(payload, at2)
			if at2 >= len(payload) {
				return 0, errors.New("a put object is unterminated")
			}
			if payload[at2] == '}' {
				at2++
				break
			}
			if payload[at2] == ',' {
				at2++
				continue
			}
			name, after, err := journalScanString(payload, at2)
			if err != nil {
				return 0, err
			}
			if at2, err = journalScanColon(payload, after); err != nil {
				return 0, err
			}
			switch name {
			case "key":
				span.key, at2, err = journalScanString(payload, at2)
				if err != nil {
					return 0, err
				}
			case "value":
				if at2 < len(payload) && payload[at2] == '"' {
					end, err := journalSkipValue(payload, at2)
					if err != nil {
						return 0, err
					}
					span.value = journalValue{valueOffset: at + int64(at2), valueLength: end - at2}
					at2 = end
					continue
				}
				end, err := journalSkipValue(payload, at2)
				if err != nil {
					return 0, err
				}
				span.value = journalValue{valueNull: true}
				at2 = end
			default:
				if at2, err = journalSkipValue(payload, at2); err != nil {
					return 0, err
				}
			}
		}
		*out = append(*out, span)
	}
}

// journalScanDeletes reads the keys one record deletes from the array that opens
// at the given offset, answering the offset just past it.
func journalScanDeletes(payload []byte, at2 int, out *[]string) (int, error) {
	if at2 >= len(payload) || payload[at2] != '[' {
		return 0, errors.New("deletes carries no array")
	}
	for at2++; ; {
		at2 = journalScanSpace(payload, at2)
		if at2 >= len(payload) {
			return 0, errors.New("deletes array is unterminated")
		}
		if payload[at2] == ']' {
			return at2 + 1, nil
		}
		if payload[at2] == ',' {
			at2++
			continue
		}
		key, after, err := journalScanString(payload, at2)
		if err != nil {
			return 0, err
		}
		*out = append(*out, key)
		at2 = after
	}
}

// journalScanString reads the JSON string token at the given offset, answering
// its decoded text and the offset just past the closing quote.
func journalScanString(payload []byte, at int) (string, int, error) {
	end, err := journalSkipValue(payload, at)
	if err != nil {
		return "", 0, err
	}
	if at >= len(payload) || payload[at] != '"' {
		return "", 0, errors.New("a field name or key is not a string")
	}
	var text string
	if err := json.Unmarshal(payload[at:end], &text); err != nil {
		return "", 0, fmt.Errorf("read a record string: %w", err)
	}
	return text, end, nil
}

// journalScanColon skips the whitespace and the colon between a field name and
// the value after it, answering the offset of the first byte of that value.
func journalScanColon(payload []byte, at int) (int, error) {
	at = journalScanSpace(payload, at)
	if at >= len(payload) || payload[at] != ':' {
		return 0, errors.New("a record field carries no value")
	}
	return journalScanSpace(payload, at+1), nil
}

// journalScanSpace answers the offset of the first byte at or after at that is
// not JSON whitespace.
func journalScanSpace(payload []byte, at int) int {
	for at < len(payload) {
		switch payload[at] {
		case ' ', '\t', '\n', '\r':
			at++
		default:
			return at
		}
	}
	return at
}

// journalSkipValue answers the offset just past the JSON value that starts at
// at. It walks the token structure alone, so a value the caller does not need is
// skipped rather than decoded into the bytes it stands for.
func journalSkipValue(payload []byte, at int) (int, error) {
	at = journalScanSpace(payload, at)
	if at >= len(payload) {
		return 0, io.ErrUnexpectedEOF
	}
	switch payload[at] {
	case '"':
		for next := at + 1; next < len(payload); next++ {
			switch payload[next] {
			case '\\':
				next++
			case '"':
				return next + 1, nil
			}
		}
		return 0, errors.New("a record string is unterminated")
	case '{', '[':
		depth := 0
		for next := at; next < len(payload); next++ {
			switch payload[next] {
			case '"':
				end, err := journalSkipValue(payload, next)
				if err != nil {
					return 0, err
				}
				next = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return next + 1, nil
				}
			}
		}
		return 0, errors.New("a record container is unterminated")
	default:
		next := at
		for next < len(payload) {
			switch payload[next] {
			case ',', '}', ']', ' ', '\t', '\n', '\r':
				return next, nil
			}
			next++
		}
		return next, nil
	}
}

// journalFrameOf returns the framed bytes of one payload: its length, the
// checksum of the payload, then the payload itself.
func journalFrameOf(payload []byte) []byte {
	frame := make([]byte, journalFrameHeaderBytes+len(payload))
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(payload)))
	binary.BigEndian.PutUint32(frame[4:8], crc32.Checksum(payload, journalCRCTable))
	copy(frame[journalFrameHeaderBytes:], payload)
	return frame
}

// journalPayloadOf reports the payload one complete frame carries, and refuses a
// frame whose checksum does not match the bytes it holds.
func journalPayloadOf(frame []byte) ([]byte, error) {
	if len(frame) < journalFrameHeaderBytes {
		return nil, fmt.Errorf("%w: %d byte(s) is shorter than a frame header", ErrJournalCorrupt, len(frame))
	}
	length := int(binary.BigEndian.Uint32(frame[0:4]))
	if length < 0 || length > journalMaxFrameBytes {
		return nil, fmt.Errorf("%w: frame declares %d payload bytes", ErrJournalCorrupt, length)
	}
	if len(frame) < journalFrameHeaderBytes+length {
		return nil, fmt.Errorf("%w: frame declares %d payload bytes and carries %d",
			ErrJournalCorrupt, length, len(frame)-journalFrameHeaderBytes)
	}
	payload := frame[journalFrameHeaderBytes : journalFrameHeaderBytes+length]
	if want := binary.BigEndian.Uint32(frame[4:8]); crc32.Checksum(payload, journalCRCTable) != want {
		return nil, fmt.Errorf("%w: checksum mismatch", ErrJournalCorrupt)
	}
	return payload, nil
}

// relocate copies every key the caller names as settled out of this log and into
// the cold root, verifies the copy byte for byte through a freshly opened view, and
// only then rewrites this log to hold what it kept. The three steps do not commute:
// a copy that never verified leaves the hot log intact, because a key removed
// before its bytes landed somewhere readable is a fact that disappears.
//
// The cold root accumulates across passes, so a root that already holds keys is
// not a reason to refuse. A key the cold root already holds with the same bytes is
// skipped and a key it holds with other bytes refuses the whole pass: one identity
// carries one immutable fact. The caller holds the log's lock and has stopped the
// writers of this view, so no key moves under the copy.
//
// The settled keys are the caller's snapshot, never the set the engine finds when
// the copy starts: a key a writer filed between that snapshot and this call is one
// the pass never judged, so it keeps its place in the hot log until a pass that
// read it settles it. Which facts a store still needs is the consumer's answer, and
// an engine that moved the rest would be a second oracle beside it.
func (l *journalLog) relocate(ctx context.Context, coldDir string, settle []string, replaced []journalPut) (RelocateReport, error) {
	if err := ctx.Err(); err != nil {
		return RelocateReport{}, err
	}
	if len(settle) == 0 {
		return RelocateReport{}, nil
	}
	keep := make(map[string]struct{}, len(l.index))
	settling := make(map[string]struct{}, len(settle))
	for _, key := range settle {
		settling[key] = struct{}{}
	}
	for key, value := range l.index {
		if value.deleted {
			continue
		}
		if _, moved := settling[key]; moved {
			continue
		}
		keep[key] = struct{}{}
	}
	settled := make([]string, 0, len(settle))
	for _, key := range settle {
		if value, held := l.index[key]; !held || value.deleted {
			continue
		}
		settled = append(settled, key)
	}
	if len(settled) == 0 {
		return RelocateReport{}, nil
	}
	sort.Strings(settled)
	cold, err := openJournalLog(filepath.Join(coldDir, kvDirName))
	if err != nil {
		return RelocateReport{}, fmt.Errorf("tagent: open the cold journal %s: %w", coldDir, err)
	}
	report := RelocateReport{}
	copyErr := l.copyToCold(cold, settled, &report)
	if closeErr := cold.close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return report, copyErr
	}
	// The steps below run only while the caller is still there: a copy that landed
	// cold is the state the next pass finds and verifies, while a rewrite entered
	// after the caller stopped would move the hot journal for a process that reads
	// nothing of it.
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := l.verifyColdCopy(coldDir, settled); err != nil {
		return report, err
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := l.rewrite(keep, replaced); err != nil {
		return report, err
	}
	report.Settled = len(settled)
	return report, nil
}

// copyToCold appends the settled keys of this log to the cold log in bounded
// batches, so one pass pays bounded memory for a settled set of any size. A key
// the cold log already holds is skipped when its bytes match and refuses the pass
// when they do not.
func (l *journalLog) copyToCold(cold *journalLog, settled []string, report *RelocateReport) error {
	for start := 0; start < len(settled); start += migrateBatchKeys {
		end := min(start+migrateBatchKeys, len(settled))
		record := journalRecord{Version: journalFormatVersion}
		for _, key := range settled[start:end] {
			body, held, err := l.read(key)
			if err != nil {
				return err
			}
			if !held {
				continue
			}
			coldBody, coldHeld, err := cold.read(key)
			if err != nil {
				return err
			}
			if coldHeld {
				if !bytes.Equal(coldBody, body) {
					return fmt.Errorf("%w: the cold root already holds %q with other bytes", ErrMigrateMismatch, key)
				}
				report.Skipped++
				continue
			}
			record.Puts = append(record.Puts, journalPut{Key: key, Value: body})
		}
		if len(record.Puts) == 0 {
			continue
		}
		if err := cold.append(record); err != nil {
			return fmt.Errorf("tagent: copy %d settled key(s) to the cold journal: %w", len(record.Puts), err)
		}
		if err := cold.sync(); err != nil {
			return fmt.Errorf("tagent: confirm the cold copy of %d settled key(s): %w", len(record.Puts), err)
		}
		report.Batches++
	}
	return nil
}

// verifyColdCopy reads the settled keys back through a freshly opened view of the
// cold log and compares them with the bytes this log still holds, so the rewrite
// that follows is entered only for a copy a later open reads whole. A reference
// the copy left without its independent body is refused by name, the pairing the
// original store's load enforces at boot.
func (l *journalLog) verifyColdCopy(coldDir string, settled []string) error {
	cold, err := openJournalLog(filepath.Join(coldDir, kvDirName))
	if err != nil {
		return fmt.Errorf("tagent: reopen the cold journal to verify it: %w", err)
	}
	defer cold.close()
	for _, key := range settled {
		want, held, err := l.read(key)
		if err != nil {
			return err
		}
		if !held {
			continue
		}
		got, coldHeld, err := cold.read(key)
		if err != nil {
			return err
		}
		if !coldHeld {
			return fmt.Errorf("%w: the cold root does not hold %q", ErrMigrateMismatch, key)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("%w: the cold root holds %q with other bytes", ErrMigrateMismatch, key)
		}
		if name, ok := strings.CutPrefix(key, "ref:"); ok {
			if _, paired, err := cold.read("blob:" + name); err != nil {
				return err
			} else if !paired {
				return fmt.Errorf("%w: original %q has a reference but no independent body", ErrSnapshotCorrupt, name)
			}
		}
	}
	return nil
}

// rewrite replaces this log with one holding the kept keys in commit order: every
// key appears once, at the record its latest commit sits in, so the walk backwards
// still meets the newest record of a key first. Ordering follows the log's own
// frame order rather than the key order, because a checkpoint recognised by its
// position in the log is a fact read back by where its commit landed.
//
// The rewrite is also the moment the index cut is placed, because it is the only
// moment the log's frames move: the cut states where every kept key landed, so the
// open that follows pays only the records the rewrite files after them. The cut is
// removed before the log is replaced and written after it, so a process that stops
// inside the pass leaves a log with no cut — read whole — and never a cut beside a
// log it does not describe.
//
// The handle is reopened onto the log the rename left before the cut is placed: the
// rename unlinks the inode the open handle names, so a cut that cannot be written
// answers beside a log that reads whole and a handle that is the file a later commit
// reaches, never beside a handle on the unlinked inode that every later commit would
// write into and lose.
func (l *journalLog) rewrite(keep map[string]struct{}, replaced []journalPut) error {
	staged := filepath.Join(l.dir, journalRewriteFileName)
	cut, err := l.rewriteInto(staged, keep, replaced)
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(l.dir, journalIndexCutFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("tagent: retire the index cut of the replaced journal: %w", err)
	}
	if err := os.Rename(staged, l.path); err != nil {
		return fmt.Errorf("tagent: replace the journal with its rewritten form: %w", err)
	}
	if err := l.reopen(cut); err != nil {
		return err
	}
	return writeJournalIndexCut(l.dir, cut)
}

// rewriteInto writes the kept keys of this log into a new file at path, one frame
// per key, at the order their latest records sit in, then the replaced records. It
// answers the cut the file it wrote states: the positions of the kept keys, the
// commit order they were filed in and the byte the tail — the replaced records and
// whatever a later commit appends — starts at.
func (l *journalLog) rewriteInto(path string, keep map[string]struct{}, replaced []journalPut) (journalIndexCut, error) {
	out, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return journalIndexCut{}, fmt.Errorf("tagent: open the rewritten journal: %w", err)
	}
	seen := make(map[string]struct{}, len(keep))
	kept := make([]string, 0, len(keep))
	for at := len(l.order) - 1; at >= 0; at-- {
		key := l.order[at]
		if _, walked := seen[key]; walked {
			continue
		}
		seen[key] = struct{}{}
		if _, keptKey := keep[key]; !keptKey {
			continue
		}
		value, held := l.index[key]
		if !held || value.deleted {
			continue
		}
		kept = append(kept, key)
	}
	slices.Reverse(kept)
	cut := journalIndexCut{
		Version: journalIndexCutVersion,
		Index:   make(map[string]journalCutValue, len(kept)),
	}
	at := int64(0)
	write := func(key string) (journalSpans, error) {
		body, held, err := l.read(key)
		if err != nil {
			return journalSpans{}, err
		}
		if !held {
			return journalSpans{}, nil
		}
		payload, err := json.Marshal(journalRecord{Version: journalFormatVersion, Puts: []journalPut{{Key: key, Value: body}}})
		if err != nil {
			return journalSpans{}, err
		}
		if len(payload) > journalMaxFrameBytes {
			return journalSpans{}, fmt.Errorf("tagent: one kept record carries %d bytes, over the %d-byte frame bound", len(payload), journalMaxFrameBytes)
		}
		frame := journalFrameOf(payload)
		if _, err := out.Write(frame); err != nil {
			return journalSpans{}, fmt.Errorf("tagent: write the rewritten journal: %w", err)
		}
		spans, err := journalSpansAt(payload, at+journalFrameHeaderBytes)
		if err != nil {
			return journalSpans{}, fmt.Errorf("tagent: index the record the rewrite just wrote: %w", err)
		}
		at += int64(len(frame))
		return spans, nil
	}
	for _, key := range kept {
		spans, err := write(key)
		if err != nil {
			out.Close()
			return journalIndexCut{}, err
		}
		if len(spans.puts) == 0 {
			continue
		}
		cut.Index[key] = cutValue(spans.puts[0].value)
		cut.Order = append(cut.Order, key)
	}
	// The tail starts here: every frame from this byte on is read again by the open
	// that follows, so the keys of these records are what a caller sees as the work
	// the cut does not cover.
	cut.Offset = at
	if len(replaced) > 0 {
		payload, err := json.Marshal(journalRecord{Version: journalFormatVersion, Puts: replaced})
		if err != nil {
			out.Close()
			return journalIndexCut{}, err
		}
		if _, err := out.Write(journalFrameOf(payload)); err != nil {
			out.Close()
			return journalIndexCut{}, fmt.Errorf("tagent: write the replaced records of the rewritten journal: %w", err)
		}
		at += int64(len(journalFrameOf(payload)))
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return journalIndexCut{}, fmt.Errorf("%w: confirm the rewritten journal: %w", ErrJournalBarrier, err)
	}
	if err := out.Close(); err != nil {
		return journalIndexCut{}, err
	}
	cut.Size = at
	return cut, nil
}

// reopen releases the writer handle of this log, opens the file the rewrite left
// in its place and seeds the index the cut states, then reads the tail past it, so
// the view a caller reads afterwards is the rewritten log and not the one it
// replaced. It reads no cut file: the caller that places the cut does so after this
// returns, once the handle names the log the cut describes.
func (l *journalLog) reopen(cut journalIndexCut) error {
	if err := l.file.Close(); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: reopen the rewritten journal: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	l.file = file
	l.seedIndexCut(cut)
	l.cutOffset = cut.Offset
	l.tail = map[string]struct{}{}
	l.unsynced = 0
	complete, err := journalReplay(file, cut.Offset, stat.Size(), l.publish)
	if err != nil {
		return err
	}
	l.size = complete
	return nil
}

// journalReplay reads every complete frame of one log from the given offset
// through visit, which is answered the payload and the offset it starts at, and
// answers the offset just past the last complete frame. Bytes shorter than a
// frame, or a frame whose declared payload is not all present, end the replay as a
// torn tail: they are what a writer left when it stopped mid-frame, and the offset
// answered is where they start. A frame that is fully present and fails its
// checksum is not a torn tail, so the replay stops with the named corruption
// instead of dropping the records behind it.
//
// - The frames are read through one sequential buffered reader and one payload buffer, so a log of any size costs one read per buffer instead of two reads and one allocation per frame.
//   - The payload handed to visit is valid for that visit alone: the buffer it points into is reused by the next frame.
func journalReplay(file *os.File, from, size int64, visit func(payload []byte, at int64) error) (int64, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(file, from, size-from), journalReplayBufferBytes)
	offset := from
	header := make([]byte, journalFrameHeaderBytes)
	var payload []byte
	for offset < size {
		if size-offset < journalFrameHeaderBytes {
			return offset, nil
		}
		if _, err := io.ReadFull(reader, header); err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, file.Name(), err)
		}
		length := int(binary.BigEndian.Uint32(header[0:4]))
		if length > journalMaxFrameBytes || size-offset-journalFrameHeaderBytes < int64(length) {
			return offset, nil
		}
		if cap(payload) < length {
			payload = make([]byte, length)
		}
		payload = payload[:length]
		if _, err := io.ReadFull(reader, payload); err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, file.Name(), err)
		}
		if want := binary.BigEndian.Uint32(header[4:8]); crc32.Checksum(payload, journalCRCTable) != want {
			return 0, fmt.Errorf("%w: frame at offset %d", ErrJournalCorrupt, offset)
		}
		if err := visit(payload, offset+journalFrameHeaderBytes); err != nil {
			return 0, err
		}
		offset += journalFrameHeaderBytes + int64(length)
	}
	return offset, nil
}

// openJournalLog opens the journal of one directory, replaying the index of the
// records past the index cut and seeding the records before it from the cut, so an
// open pays the tail the last rewrite left and never the history behind it. A
// directory holding no cut, or a cut this build cannot place, reads the whole log
// and says so at WARN: the cut is a derived carrier and a log is never read through
// one that does not describe it. A torn tail is cut off either way, and a
// directory that holds another format or another layout is refused, so a build
// never reads a log it does not understand and never rewrites the files of one it
// does.
func openJournalLog(dir string) (*journalLog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := rejectLegacyShardLayout(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, journalFileName)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	log := &journalLog{dir: dir, path: path, file: file, index: map[string]journalValue{}}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	size := stat.Size()
	from, seeded := int64(0), false
	if cut, held := readJournalIndexCut(dir); held {
		switch {
		case cut.Offset < 0 || cut.Size > size:
			log.cutNotice = fmt.Sprintf("the index cut of %s states offset=%d beside a %d byte log, so the open read the log whole",
				dir, cut.Offset, size)
		default:
			log.seedIndexCut(cut)
			from, seeded = cut.Offset, true
			log.cutOffset = cut.Offset
			log.tail = map[string]struct{}{}
		}
	}
	complete, err := journalReplay(file, from, size, log.publish)
	if err != nil {
		file.Close()
		return nil, err
	}
	if complete != size {
		if err := file.Truncate(complete); err != nil {
			file.Close()
			return nil, fmt.Errorf("tagent: cut the torn journal tail at %d: %w", complete, err)
		}
	}
	log.size = complete
	log.openBytes = size - from
	if !seeded {
		// The whole log was read, so no frame is past a cut: nil states that, where an
		// empty set would state a tail that is already covered.
		log.tail = nil
	}
	return log, nil
}

// seedIndexCut places the positions and the commit order one cut states, so the
// open below the cut reads no frame the cut already describes.
func (l *journalLog) seedIndexCut(cut journalIndexCut) {
	l.index = make(map[string]journalValue, len(cut.Index))
	for key, value := range cut.Index {
		l.index[key] = value.value()
	}
	l.order = append(l.order[:0], cut.Order...)
}

// tailKeys answers the keys the records past the index cut wrote, and nil beside a
// log no cut covers: a caller that reads this set reads the work the cut does not
// cover, and nil is the whole log rather than nothing.
func (l *journalLog) tailKeys() map[string]struct{} {
	if l == nil || l.tail == nil {
		return nil
	}
	out := make(map[string]struct{}, len(l.tail))
	for key := range l.tail {
		out[key] = struct{}{}
	}
	return out
}

// openReadBytes answers the journal bytes the open replayed. It is the read cost
// of one boot at the engine layer, fixed at open, and it is never the append
// offset the log grew to afterwards.
func (l *journalLog) openReadBytes() int64 {
	return l.openBytes
}

// publish folds one replayed payload into the index: the record it reads, at the
// value spans it occupies. The log's size is set once the replay reports where
// the last complete frame ended.
func (l *journalLog) publish(payload []byte, at int64) error {
	record, err := journalSpansAt(payload, at)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, l.path, err)
	}
	if record.version != journalFormatVersion {
		return fmt.Errorf("%w: record version %d", ErrJournalFormat, record.version)
	}
	l.indexRecord(record)
	l.noteOrder(record)
	l.noteTail(record, at)
	return nil
}

// readJournalIndexCut reads the index cut beside one log, and answers false beside
// a directory that holds none, a cut of another format and a cut that does not
// decode: the three are the cut this build cannot place, and the caller reads the
// log whole rather than trusting a carrier it does not understand.
func readJournalIndexCut(dir string) (journalIndexCut, bool) {
	body, err := os.ReadFile(filepath.Join(dir, journalIndexCutFileName))
	if err != nil {
		return journalIndexCut{}, false
	}
	var cut journalIndexCut
	if err := json.Unmarshal(body, &cut); err != nil {
		return journalIndexCut{}, false
	}
	if cut.Version != journalIndexCutVersion {
		return journalIndexCut{}, false
	}
	return cut, true
}

// writeJournalIndexCut places one cut beside the log it describes: a temporary
// file inside the same directory, renamed over the cut, so a reader meets the cut
// it started with or the whole one and never half of each.
func writeJournalIndexCut(dir string, cut journalIndexCut) error {
	cut.Version = journalIndexCutVersion
	body, err := json.Marshal(cut)
	if err != nil {
		return fmt.Errorf("tagent: state the index cut of %s: %w", dir, err)
	}
	path := filepath.Join(dir, journalIndexCutFileName)
	staged := path + ".tmp"
	if err := os.WriteFile(staged, body, 0o600); err != nil {
		return fmt.Errorf("tagent: write the index cut of %s: %w", dir, err)
	}
	if err := os.Rename(staged, path); err != nil {
		return fmt.Errorf("tagent: place the index cut of %s: %w", dir, err)
	}
	return nil
}

// noteTail files the keys one record wrote into the tail the index cut does not
// cover, so a reader asked what landed past the cut answers the records it has not
// read through the cut. A log beside no cut keeps no tail: nil states that the
// whole log was read, and a set of keys would state a tail that is already covered.
func (l *journalLog) noteTail(record journalSpans, at int64) {
	if l.tail == nil || at < l.cutOffset {
		return
	}
	for _, put := range record.puts {
		l.tail[put.key] = struct{}{}
	}
	for _, key := range record.deletes {
		l.tail[key] = struct{}{}
	}
}

// indexRecord folds one committed record into the index at the value span each
// write occupies, and a delete into the tombstone that supersedes every span.
func (l *journalLog) indexRecord(record journalSpans) {
	for _, put := range record.puts {
		l.index[put.key] = put.value
	}
	for _, key := range record.deletes {
		l.index[key] = journalValue{deleted: true}
	}
}

// noteOrder files the keys one committed record touched into the log's commit
// order: its writes first, then the keys it deleted.
func (l *journalLog) noteOrder(record journalSpans) {
	for _, put := range record.puts {
		l.order = append(l.order, put.key)
	}
	l.order = append(l.order, record.deletes...)
}

// eachKeyNewestFirst answers the keys the committed records touched, from the
// newest record to the oldest, and stops when visit answers false. The walk reads
// the order the frames were appended in, so it needs no sort and no byte of the
// log: a reader that wants the newest record of one kind walks the records filed
// after it instead of the whole log.
func (l *journalLog) eachKeyNewestFirst(visit func(key string) (bool, error)) error {
	for at := len(l.order) - 1; at >= 0; at-- {
		more, err := visit(l.order[at])
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	return nil
}

// append commits one record: it writes the frame in one call and updates the key
// index only after that write returned success, so a record whose write failed
// leaves the index answering exactly the records the file holds.
func (l *journalLog) append(record journalRecord) error {
	if record.Version == 0 {
		record.Version = journalFormatVersion
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(payload) > journalMaxFrameBytes {
		return fmt.Errorf("tagent: one journal record carries %d bytes, over the %d-byte frame bound",
			len(payload), journalMaxFrameBytes)
	}
	frame := journalFrameOf(payload)
	written, err := l.file.Write(frame)
	if err != nil {
		return err
	}
	if written != len(frame) {
		return fmt.Errorf("%w: %d of %d frame bytes written", io.ErrShortWrite, written, len(frame))
	}
	spans, err := journalSpansAt(payload, l.size+journalFrameHeaderBytes)
	if err != nil {
		return fmt.Errorf("tagent: index the record just written: %w", err)
	}
	l.indexRecord(spans)
	l.noteOrder(spans)
	l.noteTail(spans, l.size)
	l.size += int64(len(frame))
	l.unsynced += int64(len(frame))
	return nil
}

// sync completes the barrier over everything appended since the last one, so a
// caller reports durable success only behind a barrier that returned. Nothing
// appended means nothing to flush, and the barrier holds by definition.
func (l *journalLog) sync() error {
	if l.unsynced == 0 {
		return nil
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
	}
	l.unsynced = 0
	return nil
}

// read answers the value one key's latest commit carries, and whether the key is
// held at all. A tombstoned key is not held. The read takes the value's own byte
// span, so a small value committed beside a large one costs its own bytes rather
// than the frame that carries both. The caller holds the lock.
func (l *journalLog) read(key string) ([]byte, bool, error) {
	value, held := l.index[key]
	if !held || value.deleted {
		return nil, false, nil
	}
	if value.valueNull {
		return nil, true, nil
	}
	token := make([]byte, value.valueLength)
	if _, err := l.file.ReadAt(token, value.valueOffset); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, l.path, err)
	}
	var body []byte
	if err := json.Unmarshal(token, &body); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, l.path, err)
	}
	return body, true, nil
}

// keys answers the held keys carrying prefix, in key order. The caller holds the
// lock.
func (l *journalLog) keys(prefix string) []string {
	out := make([]string, 0, len(l.index))
	for key, value := range l.index {
		if value.deleted || !strings.HasPrefix(key, prefix) {
			continue
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// namespaces answers the key namespaces the held keys carry, in name order. The
// caller holds the lock.
func (l *journalLog) namespaces() []string {
	seen := make(map[string]struct{}, len(l.index))
	for key, value := range l.index {
		if value.deleted {
			continue
		}
		seen[keyNamespaceOf(key)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// close releases the one writer's handle.
func (l *journalLog) close() error {
	return l.file.Close()
}

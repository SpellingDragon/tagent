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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
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
	// make the reader allocate an unbounded span. The ceiling is the frame a
	// framework carrier reaches: a checkpoint record states every state slice of
	// the run in one commit, and the measured carrier of a long chain stands at
	// fifty megabytes, so the bound leaves that frame its headroom and stays a
	// fixed ceiling.
	journalMaxFrameBytes = 64 << 20
	// journalReplayBufferBytes is the read size the replay takes the log in: one
	// sequential read per buffer instead of one read per frame part.
	journalReplayBufferBytes = 1 << 20
)

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
	// index locates the latest record of each key.
	index map[string]journalValue
	// unsynced counts the bytes appended since the last completed barrier.
	unsynced int64
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

// journalReplay reads every complete frame of one log through visit, which is
// answered the payload and the offset it starts at, and answers the offset just
// past the last complete frame. Bytes shorter than a frame, or a frame whose
// declared payload is not all present, end the replay as a torn tail: they are
// what a writer left when it stopped mid-frame, and the offset answered is where
// they start. A frame that is fully present and fails its checksum is not a torn
// tail, so the replay stops with the named corruption instead of dropping the
// records behind it.
//
// - The frames are read through one sequential buffered reader and one payload buffer, so a log of any size costs one read per buffer instead of two reads and one allocation per frame.
//   - The payload handed to visit is valid for that visit alone: the buffer it points into is reused by the next frame.
func journalReplay(file *os.File, size int64, visit func(payload []byte, at int64) error) (int64, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(file, 0, size), journalReplayBufferBytes)
	offset := int64(0)
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

// openJournalLog opens the journal of one directory, replaying the index of
// every complete record and cutting off a torn tail. It refuses a directory that
// holds another format or another layout, so a build never reads a log it does
// not understand and never rewrites the files of one it does.
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
	complete, err := journalReplay(file, size, log.publish)
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
	return log, nil
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
	return nil
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

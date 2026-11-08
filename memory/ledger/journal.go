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
	// make the reader allocate an unbounded span.
	journalMaxFrameBytes = 32 << 20
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

// journalValue locates one key's latest record: the byte range of the payload
// that carried it, or the tombstone that deleted it. The payload of a record is
// the batch that carried the key, so a cold read decodes that batch and takes
// the key's own value out of it.
type journalValue struct {
	offset  int64
	length  int
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
func journalReplay(file *os.File, size int64, visit func(payload []byte, at int64) error) (int64, error) {
	offset := int64(0)
	header := make([]byte, journalFrameHeaderBytes)
	for offset < size {
		if size-offset < journalFrameHeaderBytes {
			return offset, nil
		}
		if _, err := file.ReadAt(header, offset); err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, file.Name(), err)
		}
		length := int64(binary.BigEndian.Uint32(header[0:4]))
		if length > journalMaxFrameBytes || size-offset-journalFrameHeaderBytes < length {
			return offset, nil
		}
		payload := make([]byte, length)
		if _, err := file.ReadAt(payload, offset+journalFrameHeaderBytes); err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, file.Name(), err)
		}
		if want := binary.BigEndian.Uint32(header[4:8]); crc32.Checksum(payload, journalCRCTable) != want {
			return 0, fmt.Errorf("%w: frame at offset %d", ErrJournalCorrupt, offset)
		}
		if err := visit(payload, offset+journalFrameHeaderBytes); err != nil {
			return 0, err
		}
		offset += journalFrameHeaderBytes + length
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

// publish folds one replayed payload into the index: the record it decodes, at
// the payload range it occupies. The log's size is set once the replay reports
// where the last complete frame ended.
func (l *journalLog) publish(payload []byte, at int64) error {
	var record journalRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, l.path, err)
	}
	if record.Version != journalFormatVersion {
		return fmt.Errorf("%w: record version %d", ErrJournalFormat, record.Version)
	}
	l.indexRecord(record, journalValue{offset: at, length: len(payload)})
	return nil
}

// indexRecord folds one committed record into the index at the payload range it
// occupies.
func (l *journalLog) indexRecord(record journalRecord, value journalValue) {
	for _, put := range record.Puts {
		l.index[put.Key] = value
	}
	for _, key := range record.Deletes {
		tombstone := value
		tombstone.deleted = true
		l.index[key] = tombstone
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
	value := journalValue{offset: l.size + journalFrameHeaderBytes, length: len(payload)}
	l.indexRecord(record, value)
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

// read answers the value one key's latest record carries, and whether the key is
// held at all. A tombstoned key is not held. The caller holds the lock.
func (l *journalLog) read(key string) ([]byte, bool, error) {
	value, held := l.index[key]
	if !held || value.deleted {
		return nil, false, nil
	}
	payload := make([]byte, value.length)
	if _, err := l.file.ReadAt(payload, value.offset); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, l.path, err)
	}
	var record journalRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, l.path, err)
	}
	for _, put := range record.Puts {
		if put.Key == key {
			return put.Value, true, nil
		}
	}
	return nil, false, nil
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

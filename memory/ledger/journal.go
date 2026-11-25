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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrJournalCorrupt reports a frame that is fully present in the log and does
// not match its checksum, or declares a payload past the frame bound.
var ErrJournalCorrupt = errors.New("memory: journal frame is corrupt")

// ErrJournalFormat reports a log written under a format version this build does
// not read.
var ErrJournalFormat = errors.New("memory: journal format version is not supported")

// ErrJournalBarrier reports a barrier that did not complete, so nothing staged
// in front of it is confirmed.
var ErrJournalBarrier = errors.New("memory: journal barrier failed")

// ErrJournalFrameTooLarge reports one record whose encoded payload is over the
// frame bound: the refusal is decided before any byte of the record is written,
// so no part of it is staged and a caller states the refusal instead of an
// uncertain outcome.
var ErrJournalFrameTooLarge = errors.New("memory: journal record is over the frame bound")

const (
	// journalFileName is the file one journal directory holds: the whole log of
	// that directory, appended to and never rewritten.
	journalFileName = "journal.log"
	// journalFormatVersion is the record format this build writes. Version one
	// remains readable and states an unknown commit time when its field is absent.
	journalFormatVersion   = 2
	journalFormatVersionV1 = 1
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
	// journalOrderFileName is the file beside the log that states one line per
	// commit: the byte the commit's frame ends at and the value span of every key
	// it wrote. Every line carries the checksum of its own statement, so the bytes a
	// walk answers from are the bytes the writer filed. It is a derived index and
	// never a second truth — the spans it states are rebuilt from the log whenever
	// the watermark the slot table states cannot be trusted or a line arrives the
	// checksum refuses — and it exists so an open reads the statements past the
	// watermark instead of replaying the frames behind them.
	journalOrderFileName = "order.log"
	// journalSlotFileName is the file beside the log that addresses every held key:
	// a header stating the capacity, the record width and the watermark, then one
	// fixed-width record per slot. It is the held-key set and the point address at
	// once, so an enumeration reads no value and a point read walks nothing, and it
	// is a derived index: a record states the same span the order file states, and
	// a table that is missing, torn or behind the watermark is stated again from the
	// order file and the log.
	journalSlotFileName = "slots"
	// journalOrderLineFileName is the file beside the log that states where one commit's
	// order line ends, one 8-byte word per slot: the offset of the line the commit that
	// last stated that slot's key filed. The table states one word per slot rather than
	// one per commit because a read enters the order at the commit a key names, and the
	// slot the key addresses is the only place it learns that commit without walking the
	// statements in between. It is a derived index on the same terms as the two files
	// beside it — the table it parallels is itself derived from the log, so a word that is
	// missing, unknown or past the order bytes the header states costs the walk its seek
	// and never its answer, and one that states some other byte is caught by the walk it
	// places: a walk entered at a word names the key that word was read for before it ends,
	// and one that never names it takes the order from the newest commit — and it carries
	// the slot table's watermark, never one of its own: its size is the capacity the header
	// states times the word, so a table stated again, grown or reset states this file again
	// behind it.
	journalOrderLineFileName = "order.line"
	// journalOrderLineUnknown is the word a slot states when no commit stated its key:
	// an empty slot names no order line, so a seek answers none and the caller walks the
	// order file it would have walked without this file. Every other word is a byte of the
	// order file, and zero is one of them, so the unknown word is not zero.
	journalOrderLineUnknown int64 = -1
	// journalOrderLineWordBytes is the width of one word of the order-line index: the
	// byte the order line of one commit ends at, so a file of capacity words states the
	// seek of every slot the table addresses.
	journalOrderLineWordBytes = 8
	// journalSlotVersion is the slot table format this build writes and the only one
	// it reads: another version is a table this build cannot place, and the index
	// files are rebuilt from the log rather than trusted through a record it cannot
	// decode.
	journalSlotVersion = 2
	// journalSlotMagic names the slot table file, so bytes of another index file are
	// read as no table instead of as a header of this build.
	journalSlotMagic = 0x534c4f54
	// journalSlotHeaderBytes is the fixed width of the table header in front of the
	// first slot, so a slot's byte is a function of its index alone.
	journalSlotHeaderBytes = 64
	// journalSlotRecordHeaderBytes is the fixed width of one record in front of its
	// key: the checksum, the state of the key it addresses and the span of that
	// key's value.
	journalSlotRecordHeaderBytes = 20
	// journalSlotStride is the record width a new table states: the record header and
	// room past every key the engine writes today. A key over the room a record leaves
	// widens the stride, because a record that cannot state its whole key cannot
	// answer one key rather than another.
	journalSlotStride = 96
	// journalSlotCapacity is the slot count a new table states. It is a starting
	// point and not a bound: a table whose used slots pass the load bound doubles.
	journalSlotCapacity = 1024
	// journalSlotLoadPercent bounds the share of slots a commit may fill before the
	// table doubles, so a probe chain stays short instead of growing into a walk of
	// the table on every read. Half is the measured bound: at seven tenths the longest
	// chain of 32,000 keys this engine writes reached 54 slots. The share is a cost
	// rule and not a correctness one: a chain longer than any measured value is still
	// the address of legal keys, so a probe walks until it meets an empty slot and the
	// table itself is the only bound it may trust (see journalAddress.probe).
	journalSlotLoadPercent = 50
	// journalSlotScanBatch is the slots one sequential read of an enumeration takes,
	// so listing the held keys costs one read per batch instead of one per slot.
	journalSlotScanBatch = 512
	// journalIndexSyncBytes is the budget one barrier over the derived index files
	// covers: a commit pays the log's barrier alone, and the index files take theirs
	// only once they staged this many bytes. The bound is bytes and not commits
	// because it is also the bound of the replay an open pays when a stop leaves the
	// index files behind, so the read one boot pays stays one chunk.
	journalIndexSyncBytes = 64 << 10
	// journalLogSyncBytes is the budget one barrier covers of the log bytes the
	// index files leave unstated, which is the window an open replays for them. The
	// window is bounded in both quantities because a commit may state few index
	// bytes and carry many: 360 commits of one 300 KiB value staged 71,504 index
	// bytes and left a 134,780,772-byte window. A commit that crosses either budget
	// advances the watermark behind itself, so the window never passes the budget.
	// The budget is one chunk because that is the read one boot stays within, and it
	// stands over the widest window the index budget itself leaves — 41,500 bytes
	// beside five 40-byte values per commit and 49,608 beside thirty-six 56-byte
	// ones — so it never splits a window those commits cover.
	journalLogSyncBytes = 64 << 10
	// journalAddressStageSuffix names the file a table is staged into before it is
	// renamed over the table it replaces, so a reader meets the table it started
	// with or the whole new one and never half of each.
	journalAddressStageSuffix = ".stage"
	// journalOrderChunkBytes is the read a backwards walk of the order file takes:
	// one statement per line, so one chunk states many commits and a walk that
	// stops early reads one chunk instead of the file.
	journalOrderChunkBytes = 64 << 10
	// journalOrderChecksumBytes is what one order line carries in front of its
	// statement: the checksum of the statement bytes as eight hex digits and the
	// space after them. The digits are text, so no checksum byte can be the line
	// break a backwards walk splits the file at.
	journalOrderChecksumBytes = 9
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
// under, the commit time shared by every write in the batch, and the writes it
// commits. One frame carries one commit, so a frame is all of a commit or none of it.
type journalRecord struct {
	Version       int          `json:"version"`
	CommittedAtNS int64        `json:"committed_at_ns"`
	Puts          []journalPut `json:"puts,omitempty"`
	Deletes       []string     `json:"deletes,omitempty"`
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
// appends whole frames, and the index beside the log is updated only after the
// write of the frame that carries the key returned success. That index holds
// positions, never values, so a log of any size costs the process no resident
// entry per key. A frame is appended in one write, so a process that stops
// mid-write leaves a prefix of it, which is the torn tail the next open cuts off.
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
	// openBytes is how many journal bytes the open replays into its index: the span
	// from the watermark the index files state to the end of the file, torn tail
	// included. It is set once at open and never moves, so it measures the index
	// replay one boot pays instead of the offset the next append starts at (size,
	// which follows every write), and it is zero beside a root whose index files
	// state the whole log. The integrity verification of the whole log is a separate
	// pass and is not this span.
	openBytes int64
	// integrityBytes is the log span the open's integrity pass verified: the bytes
	// from the first frame through the last complete one, whose checksums the open
	// read and matched. It is set once at open and never moves, and it is the whole
	// log beside a log of whole frames, so it grows with the history the log holds
	// whatever the watermark states. It is a separate measure from openBytes: the
	// index replay reads the window past the watermark, the integrity pass reads the
	// log whole, and a boot pays both.
	integrityBytes int64
	// integrityBegin and integrityEnd are the two clock reads the open took around
	// that pass: the read before the verification of the log starts and the read the
	// moment it answered. They are set once at open and never move, and they stay
	// zero beside a view that ran no verification pass, which is a pass that did not
	// happen rather than a pass of no duration.
	integrityBegin time.Time
	integrityEnd   time.Time
	// tornTail is the byte the last frame of the log starts at when that frame is not
	// whole, and zero beside a log whose frames are whole. A read view serves the
	// complete frames and names the byte it stopped at instead of cutting the bytes
	// past them, because the writer that owns those bytes may be between the write of
	// that frame and its barrier.
	tornTail int64
	// torn reports that the log ends in a torn frame, which is what tornTail's zero
	// cannot state alone: a log whose one frame is torn from its first byte stops at
	// the same zero byte a log of whole frames does.
	torn bool
	// unsynced counts the bytes appended since the last completed barrier.
	unsynced int64
	// addr is the addressable index of this log — the order file, the key table and the
	// watermark — and the statement every read of this log answers from. Every view opens
	// it before any caller reads the log: the view that holds the root's write rights
	// states it, and a view holding none keeps the read face of it.
	addr *journalAddress
	// segments are the sealed segments of this log, oldest first: each one carries the
	// bytes of the interval [begin, begin+bytes) of the log, copied verbatim behind the
	// header that states it. They are the intervals a cut closed, and the bytes they
	// carry are read from the segment files, never from the open file behind them.
	segments []journalSealedSegment
	// openBase is the log byte the open segment starts at: the sum of the bytes the
	// sealed segments carry. It is zero beside a log no seal has closed, so every read
	// of such a log reads the one file it always read.
	openBase int64
	// baseSeq is the commit rank in front of the first frame of the hot log: one less
	// than the first rank the oldest sealed segment states. A rebuild of the index
	// numbers the hot commits by the ranks the segment headers state, so a seal behind
	// it continues from the interval the log kept. It is zero beside a log whose oldest
	// sealed interval starts at the first commit.
	baseSeq int64
}

// journalSpans is the index material one record states: the format version it was
// written under, its commit time, the value span of every write and the keys it deletes.
type journalSpans struct {
	version int
	// committedAtNS is the time a barrier confirmed. Version two states zero for
	// an ordered append that confirmed no commit; version one states zero because
	// the format carried no time.
	committedAtNS int64
	puts          []journalPutSpan
	deletes       []string
}

// journalPutSpan is one write of a record as the index files it: the key it
// names and the position of its own value token in the log.
type journalPutSpan struct {
	key   string
	value journalValue
}

// journalRecordVersionReadable reports whether a record version is one this build replays.
func journalRecordVersionReadable(version int) bool {
	return version == journalFormatVersionV1 || version == journalFormatVersion
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
		case "committed_at_ns":
			end, err := journalSkipValue(payload, at2)
			if err != nil {
				return out, err
			}
			if err := json.Unmarshal(payload[at2:end], &out.committedAtNS); err != nil {
				return out, fmt.Errorf("read the record commit time: %w", err)
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

// journalByteSource is the bytes of one log as a pass reads them: one address space
// over the sealed segments and the open segment, named as the file the log is so an
// error states the log it read.
type journalByteSource interface {
	io.ReaderAt
	Name() string
}

// journalLogBytes is the bytes of one journal log as its own passes read them: every
// offset is a logical log byte, and a byte of a sealed interval is read from the
// segment file that states it.
type journalLogBytes struct {
	log *journalLog
}

// ReadAt answers the log bytes at one logical offset.
func (b journalLogBytes) ReadAt(p []byte, off int64) (int, error) {
	return b.log.readAt(p, off)
}

// Name answers the log's file, which is the record every position of the log is
// stated against.
func (b journalLogBytes) Name() string {
	return b.log.path
}

// journalReplay reads every complete frame of one log from the given offset
// through visit, which is answered the payload and the offset it starts at, and
// answers the offset just past the last complete frame. Bytes shorter than a
// frame, or a frame whose declared payload is not all present, end the replay as a
// torn tail: they are what a writer left when it stopped mid-frame, and the offset
// answered is where they start. A declared payload past the frame bound, or a
// fully present frame that fails its checksum, is not a torn tail, so the replay
// stops with the named corruption instead of dropping the records behind it. A
// frame header of zero states no
// payload and no checksum, which no commit writes, so the zeros a stopped
// writer leaves past its last frame end the replay as a torn tail at that
// offset, and a log resuming past such zeros is the named corruption.
//
// - The frames are read through one sequential buffered reader and one payload buffer, so a log of any size costs one read per buffer instead of two reads and one allocation per frame.
//   - The payload handed to visit is valid for that visit alone: the buffer it points into is reused by the next frame.
func journalReplay(source journalByteSource, from, size int64, visit func(payload []byte, at int64) error) (int64, error) {
	reader := bufio.NewReaderSize(io.NewSectionReader(source, from, size-from), journalReplayBufferBytes)
	offset := from
	header := make([]byte, journalFrameHeaderBytes)
	var payload []byte
	for offset < size {
		if size-offset < journalFrameHeaderBytes {
			return offset, nil
		}
		if _, err := io.ReadFull(reader, header); err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, source.Name(), err)
		}
		length := int(binary.BigEndian.Uint32(header[0:4]))
		if length == 0 && binary.BigEndian.Uint32(header[4:8]) == 0 {
			zero, err := journalRestStatesZeroBytes(reader)
			if err != nil {
				return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, source.Name(), err)
			}
			if zero {
				return offset, nil
			}
			return 0, fmt.Errorf("%w: frame at offset %d carries no payload", ErrJournalCorrupt, offset)
		}
		if length > journalMaxFrameBytes {
			return 0, fmt.Errorf("%w: frame at offset %d declares %d payload bytes past the %d-byte bound",
				ErrJournalCorrupt, offset, length, journalMaxFrameBytes)
		}
		if size-offset-journalFrameHeaderBytes < int64(length) {
			return offset, nil
		}
		if cap(payload) < length {
			payload = make([]byte, length)
		}
		payload = payload[:length]
		if _, err := io.ReadFull(reader, payload); err != nil {
			return 0, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, source.Name(), err)
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

// journalRestStatesZeroBytes reports whether the bytes left in reader are all
// zero, the fill a stopped writer leaves past its last committed frame.
func journalRestStatesZeroBytes(reader io.Reader) (bool, error) {
	buffer := make([]byte, journalReplayBufferBytes)
	for {
		read, err := reader.Read(buffer)
		for _, b := range buffer[:read] {
			if b != 0 {
				return false, nil
			}
		}
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// openJournalLog opens the journal of one directory as the view that holds its write
// rights: the open replays the records past the watermark the index files state and
// never the history behind that watermark. A directory those files state no readable
// watermark for costs the open a rebuild of them from the log, which is the one read
// this index can never avoid. A torn tail is cut off, and a directory that holds
// another format or another layout is refused, so a build never reads a log it does
// not understand and never rewrites the files of one it does.
func openJournalLog(dir string) (*journalLog, error) {
	return openJournalLogFace(dir, true)
}

// openJournalLogFace opens the journal of one directory for the face the caller
// declares: a view holding the root's write rights creates the directory and the log
// and cuts a torn tail, and a view holding none does none of the three.
//
//   - A torn tail is recorded by both faces: the writing view cuts it off the log and the reading view serves the frames before it, so either open states the bytes its writer never finished
//   - The open verifies the checksum of every complete frame of the log, the history behind the watermark included: the index files are derived from the log, so a watermark they state accelerates the index replay but never vouches for the integrity of the bytes it skips
//   - The integrity verification reads the whole log as one address space: every sealed segment in id order and then the open segment, so the bytes a segment carries are verified at the open that reads them and no seal is trusted on the header's own word
//   - The index replay reads the window past the watermark alone, which is the span openBytes states; the integrity verification reads the whole log and is not that span
func openJournalLogFace(dir string, writeRights bool) (*journalLog, error) {
	if writeRights {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if err := rejectLegacyShardLayout(dir); err != nil {
		return nil, err
	}
	segments, err := openSealedSegments(dir)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, journalFileName)
	if len(segments) > 0 {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s is not held beside the %d sealed segment(s) of %s",
				ErrJournalCorrupt, path, len(segments), dir)
		}
	}
	flags := os.O_RDWR | os.O_CREATE | os.O_APPEND
	if !writeRights {
		flags = os.O_RDONLY
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		if writeRights || !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return openUncreatedJournalReadView(dir, path)
	}
	log := &journalLog{dir: dir, path: path, file: file, segments: segments}
	for _, segment := range segments {
		log.openBase += segment.bytes
	}
	if len(segments) > 0 {
		log.baseSeq = segments[0].firstSeq - 1
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	size := stat.Size()
	from := int64(0)
	if watermark, stated := journalStatedWatermark(dir); stated && watermark <= size {
		from = watermark
	}
	integrityBegin := time.Now()
	complete, err := journalReplay(journalLogBytes{log: log}, 0, size, func([]byte, int64) error { return nil })
	if err != nil {
		file.Close()
		return nil, err
	}
	integrityEnd := time.Now()
	if complete != size {
		log.torn, log.tornTail = true, complete
		if writeRights {
			if err := file.Truncate(complete); err != nil {
				file.Close()
				return nil, fmt.Errorf("tagent: cut the torn journal tail at %d: %w", complete, err)
			}
		}
	}
	log.size = complete
	log.integrityBytes = complete
	log.integrityBegin, log.integrityEnd = integrityBegin, integrityEnd
	log.openBytes = size - from
	if err := log.openAddressIndex(writeRights); err != nil {
		file.Close()
		return nil, err
	}
	return log, nil
}

// openUncreatedJournalReadView answers the read face of a log no writer has created:
// nothing was ever committed, so the view states an empty log and creates no file.
// Index files beside the missing log state commits it once held, so that directory is
// refused by name — a log that disappeared is corruption, not a young root.
func openUncreatedJournalReadView(dir, path string) (*journalLog, error) {
	for _, name := range []string{journalOrderFileName, journalSlotFileName} {
		_, err := os.Stat(filepath.Join(dir, name))
		if err == nil {
			return nil, fmt.Errorf("%w: %s is not held beside %s, which states its commits",
				ErrJournalCorrupt, path, name)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	log := &journalLog{dir: dir, path: path}
	if err := log.openAddressIndex(false); err != nil {
		return nil, err
	}
	return log, nil
}

// journalStatedWatermark answers the log byte the slot table of one directory states,
// which is the byte an open replays from, and answers false beside a table this build
// reads no watermark from: a directory holding none, a torn header and a header whose
// table is shorter than it states the three are the tables the address open rebuilds
// or refuses by name, and the open reads the log whole rather than from a byte nobody
// stated.
func journalStatedWatermark(dir string) (int64, bool) {
	file, err := os.Open(filepath.Join(dir, journalSlotFileName))
	if err != nil {
		return 0, false
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, false
	}
	body := make([]byte, journalSlotHeaderBytes)
	if _, err := file.ReadAt(body, 0); err != nil {
		return 0, false
	}
	header, stated := decodeSlotHeader(body)
	if !stated || !journalHeaderStates(header, stat.Size()) {
		return 0, false
	}
	return header.ValuesSize, true
}

// openReadBytes answers the journal bytes the open replayed: the frames past the
// watermark the index files state. It is the read cost of one boot at the engine layer,
// fixed at open, zero beside a root those files already state, and it is never the
// append offset the log grew to afterwards.
func (l *journalLog) openReadBytes() int64 {
	return l.openBytes
}

// openAddressIndex opens the addressable index of this log for the face the view
// declares: the view that holds the root's write rights states the order file, the key
// table and the watermark at every commit, and a view holding none reads the commits
// past that watermark into a window of its own. It is called once, before any caller
// holds the view.
func (l *journalLog) openAddressIndex(writeRights bool) error {
	if l.addr != nil {
		return nil
	}
	open := openJournalAddress
	if !writeRights {
		open = openJournalReadAddress
	}
	addr, err := open(l)
	if err != nil {
		return err
	}
	l.addr = addr
	return nil
}

// errSlotUnplaceable reports a slot record this build cannot decode: bytes that hold
// no checksum, a state no commit writes, or a key longer than the record. A stop
// inside the write of one record leaves exactly that, and the key it held cannot be
// read back from it, so the repair is to state the table again from the order file.
var errSlotUnplaceable = errors.New("memory: the slot table states no record this build can place")

const (
	// journalSlotEmpty is a slot no commit has taken, and the state that stops the
	// probe chain reaching it.
	journalSlotEmpty uint8 = iota
	// journalSlotHeld is a slot whose key is held at the span it states.
	journalSlotHeld
	// journalSlotAbsent is a slot whose key a commit deleted: it keeps the key so the
	// chain it sits inside stays walkable, and answers absence.
	journalSlotAbsent
)

// journalSlotHeader is the watermark and the shape of the slot table beside one
// log: how many slots it holds, how wide one record is, and the commits, log bytes
// and order bytes every derived index file of this log reaches. It is the only
// statement of that watermark, and it is written after the records and the order
// statements it describes, so a table is never ahead of the commits it addresses.
type journalSlotHeader struct {
	// Magic names the file as a slot table of this build.
	Magic uint32
	// Version is the slot table format this build writes.
	Version uint32
	// Capacity is the number of slots the table holds.
	Capacity int64
	// Stride is the width of one slot record in bytes.
	Stride int64
	// Seq is the commit rank the derived index files reach: the rank of the newest
	// commit they state, counted in the one order of the log.
	Seq int64
	// ValuesSize is the byte of the log those commits end at. The byte is a logical
	// log offset and a segment boundary does not move it: the sealed segments and the
	// open segment are one address space, so a watermark stated before a seal still
	// names the same bytes after it, and an open replays from it across the segment
	// that carries them.
	ValuesSize int64
	// OrderSize is the byte of order.log those commits end at.
	OrderSize int64
	// Used is the number of slots the table states a key in, so the open reads the
	// load a commit's growth rule is measured against from the header alone instead
	// of walking the table it just opened.
	Used int64
	// Sum is the checksum over the bytes before it, so a torn header is read as no
	// header and costs a rebuild rather than a watermark nobody wrote.
	Sum uint32
}

// journalOrderStatement is one commit as the order file states it: the commit's
// rank, the byte of the log its frame ends at, the spans of the keys it wrote and
// the keys it deleted. It states no length of its own line, because a statement
// that named its own length would change that length by stating it; the writer
// counts the bytes it appended instead.
type journalOrderStatement struct {
	// Seq is the commit's rank, counted from one.
	Seq int64 `json:"seq"`
	// ValuesEnd is the byte of journal.log this commit's frame ends at.
	ValuesEnd int64 `json:"valuesEnd"`
	// Puts are the keys this commit wrote, at the span of their own value tokens.
	Puts []journalOrderPut `json:"puts,omitempty"`
	// Deletes are the keys this commit deleted.
	Deletes []string `json:"deletes,omitempty"`
}

// journalOrderPut is one write of a commit as the order file states it: the key
// and the span of its value token inside the frame that carries it.
type journalOrderPut struct {
	// Key is the key this commit wrote.
	Key string `json:"key"`
	journalAddressSpan
}

// journalAddressSpan is one key's position as the slot table states it: the byte
// span of the value token the commit carries. It is the stated form of
// journalValue, so the record a slot holds and the entry the order file states
// decode to the same position.
type journalAddressSpan struct {
	// Offset is the byte offset of the value's own JSON token.
	Offset int64 `json:"offset"`
	// Length is the byte length of that token, its quotes included.
	Length int `json:"length"`
	// Null reports a value the commit wrote as JSON null.
	Null bool `json:"null,omitempty"`
}

// spanOf answers the stated form of one indexed position.
func spanOf(value journalValue) journalAddressSpan {
	return journalAddressSpan{Offset: value.valueOffset, Length: value.valueLength, Null: value.valueNull}
}

// value answers the stated span as the index keeps a position.
func (s journalAddressSpan) value() journalValue {
	return journalValue{valueOffset: s.Offset, valueLength: s.Length, valueNull: s.Null}
}

// journalAddressCommit is one appended commit waiting for the index files to state
// it: the spans its frame carries and the byte that frame ends at. A commit stays
// pending until the order file and the slot table state it, so a statement that
// failed is stated again instead of skipped.
type journalAddressCommit struct {
	spans     journalSpans
	valuesEnd int64
}

// journalAddressWindow is the window one read view replays for itself: the commits a
// slot table's watermark leaves unstated, held in memory alone. It exists because a
// view holding no write rights states nothing and yet answers what the writer's index
// files do not reach, and because the frame is the only statement a commit the writer
// appended and could not confirm leaves behind: an unconfirmed commit carries no line
// in the order file.
type journalAddressWindow struct {
	// end is the byte of the log the window's frames end at, which is the log's own
	// readable end beside a whole last frame.
	end int64
	// order names the keys the window's commits touched, in commit order, oldest
	// first: the writes of one frame then the keys it deletes.
	order []string
	// values locates the newest commit of each key the window names, and states a
	// tombstone for a key its last commit deleted.
	values map[string]journalValue
}

// fold files one replayed frame into the window: every write replaces the position of
// its key and every delete states a tombstone, so a lookup answers the newest commit
// of a key inside the window.
func (w *journalAddressWindow) fold(spans journalSpans) {
	for _, put := range spans.puts {
		w.values[put.key] = put.value
		w.order = append(w.order, put.key)
	}
	for _, key := range spans.deletes {
		w.values[key] = journalValue{deleted: true}
		w.order = append(w.order, key)
	}
}

// journalAddress is the addressable index of one log: the order file that states
// one line per commit, and the slot table that addresses every held key in one
// in-place record. Both index files are derived, so a damaged or missing one is
// stated again from the log instead of trusted, and a commit costs the keys it
// touches rather than the history behind it.
type journalAddress struct {
	// log is the journal this index locates positions inside; it supplies the
	// replayed frames at open and the value bytes a point read answers.
	log *journalLog
	// orderPath is the order file beside the log.
	orderPath string
	// slotsPath is the slot table beside the log.
	slotsPath string
	// linePath is the order-line index beside the log, whose one word per slot states
	// where the order line of the commit that last stated that slot's key ends.
	linePath string
	// order is the writer's append handle on the order file, nil beside an index
	// that retired its index files for a rewrite and has not rebuilt them yet.
	order *os.File
	// slots is the writer's handle on the slot table.
	slots *os.File
	// lines is the handle on the order-line index: the writer's in-place handle, a read
	// view's read-only handle, or nil beside a view that holds no file this build states
	// — a root an older build left, or a read view opened beside one. A nil handle
	// answers no seek, so the caller walks the order file as it did before this index.
	lines *os.File
	// window is the window of a read view: non-nil beside a view that holds no write
	// rights, which states nothing and answers the commits past the stated watermark
	// from its own replay, and nil beside a write view, whose watermark advances with
	// its own commits.
	window *journalAddressWindow
	// notice states why this read view replayed the whole log instead of the commits
	// past a watermark, and is empty beside an index the open read a watermark from.
	// The ledger package keeps no logger, so the open names the fallback here and the
	// caller that holds one states it.
	notice string
	// seq is the commit rank the index files reach, which is the rank of the newest
	// commit they state and the rank in front of the next one they file.
	seq int64
	// baseSeq is the commit rank in front of the log's first hot frame, which a rebuild
	// of the index numbers the hot commits from.
	baseSeq int64
	// valuesSize is the byte of the log those commits end at.
	valuesSize int64
	// orderSize is the byte of the order file those commits end at.
	orderSize int64
	// capacity is the number of slots the table holds.
	capacity int64
	// stride is the width of one slot record in bytes, so a slot's byte is a
	// function of its index alone and no record is addressed by an offset table.
	stride int64
	// used counts the slots that state a key, which is what the load a probe chain
	// pays for is measured against.
	used int64
	// statedSeq states the commits the slot table reached when its header was last
	// written, so the open knows how far the table is behind the order file.
	statedSeq int64
	// statedValuesSize states the log byte that header reached.
	statedValuesSize int64
	// statedOrderSize states the order byte that header reached.
	statedOrderSize int64
	// pending holds the commits appended since the index files last stated one.
	pending []journalAddressCommit
	// staged counts the index bytes written since the last barrier over them,
	// which is what the barrier budget of flush is measured against.
	staged int64
	// stagedLog counts the log bytes appended since the index files last stated a
	// watermark on them, which is the window an open replays for those statements:
	// it is the second quantity the barrier budget of flush is measured against.
	stagedLog int64
	// openBytes is how many bytes the open replay of the index files read: the header,
	// the order statements the watermark leaves unstated and the frames of those
	// statements. It is fixed at open and measures the read one boot pays at the
	// engine layer instead of the append offset the log grew to afterwards.
	openBytes int64
	// valueReads counts the value spans this index read back, so a caller states
	// that an enumeration read no value instead of inferring it.
	valueReads int64
	// slotScanBytes counts the slot table bytes the enumerations of this index read:
	// one full-table pass states capacity records of stride bytes each, so the count
	// follows the table an enumeration walks and never the keys it answers. A point
	// read probes one record and is not an enumeration, so it adds nothing here.
	slotScanBytes int64
	// orderChunks holds the buffers order walks read into: a walk takes one for the
	// chunks it reads and gives it back when it returns, so a walk allocates no chunk
	// beside a pool that holds one and two walks never read into one buffer.
	orderChunks sync.Pool
	// broken reports that a statement failed after the frame landed, so the next
	// statement rebuilds the index files from the log instead of appending beside a
	// half-stated line.
	broken bool
	// passes counts the address-table maintenance passes this index opened, one per
	// pass, and passRecords holds the pair of clock reads each pass took beside the
	// call that ran it. The count is written where a pass opens and the record where
	// it closes, so a reader states that a pass happened from the count and reads the
	// span it took from the record.
	passes      int
	passRecords []journalAddressPass
}

// journalAddressPass is one maintenance pass of the address table as the call that
// ran it states it: the pass, the caller's account of what it was doing, and the two
// clock reads that call took around the pass.
type journalAddressPass struct {
	pass   string
	parent string
	begin  time.Time
	end    time.Time
}

// openPass opens one maintenance pass of the address table and answers the function
// that closes it. The call that runs the pass names it and names what it was doing
// around it, so the pass states its own endpoints and the stage it nests inside.
func (a *journalAddress) openPass(pass, parent string) func() {
	a.passes++
	begin := time.Now()
	return func() {
		a.passRecords = append(a.passRecords, journalAddressPass{
			pass: pass, parent: parent, begin: begin, end: time.Now(),
		})
	}
}

// openJournalAddress opens the addressable index of one log and brings its
// index files to the log's water level, reading the header and the commits past it
// and writing nothing beside a log whose index files already reach the end.
//
//   - The header states the watermark; a header this build cannot read, one behind the log or one whose order file lost statements costs a rebuild from the log, which is the one cost this index can never avoid
//   - A header stating bytes the log does not hold refuses the open: the frames behind the watermark are the committed facts, and a log that lost them is served by no prefix
//   - The order file is truncated to the byte the header states, because a statement past the watermark states a commit the index files have not placed, and every statement past the watermark is stated again from the log instead of reconciled
func openJournalAddress(log *journalLog) (*journalAddress, error) {
	a := &journalAddress{
		log:       log,
		orderPath: filepath.Join(log.dir, journalOrderFileName),
		slotsPath: filepath.Join(log.dir, journalSlotFileName),
		linePath:  filepath.Join(log.dir, journalOrderLineFileName),
		capacity:  journalSlotCapacity,
		stride:    journalSlotStride,
		baseSeq:   log.baseSeq,
	}
	if err := a.openOrder(); err != nil {
		return nil, err
	}
	if err := a.openSlots(); err != nil {
		a.order.Close()
		return nil, err
	}
	if err := a.openLines(); err != nil {
		a.closeIndex()
		return nil, err
	}
	header, stated := a.readHeader()
	orderSize, err := a.orderBytes()
	if err != nil {
		a.closeIndex()
		return nil, err
	}
	if stated && header.ValuesSize > log.size {
		a.closeIndex()
		return nil, fmt.Errorf("%w: %s holds %d bytes beside a header stating %d",
			ErrJournalCorrupt, log.path, log.size, header.ValuesSize)
	}
	if !stated || header.OrderSize > orderSize {
		if err := a.rebuildFrom("open"); err != nil {
			a.closeIndex()
			return nil, err
		}
		return a, nil
	}
	a.seq, a.valuesSize, a.orderSize = header.Seq, header.ValuesSize, header.OrderSize
	a.statedSeq = header.Seq
	if !a.linesState(header.Capacity) {
		if err := a.rebuildFrom("open"); err != nil {
			a.closeIndex()
			return nil, err
		}
		return a, nil
	}
	if orderSize > a.orderSize {
		if err := a.order.Truncate(a.orderSize); err != nil {
			a.closeIndex()
			return nil, fmt.Errorf("tagent: discard the statements past the watermark of %s: %w", log.dir, err)
		}
	}
	if a.valuesSize < log.size {
		if err := a.derive(a.valuesSize, false, "open"); err != nil {
			a.closeIndex()
			return nil, err
		}
	}
	return a, nil
}

// openJournalReadAddress opens the read face of the addressable index: the watermark the
// slot table states, and the commits past it replayed from the log into a window this
// view alone holds. It writes nothing — it creates no index file, states no watermark and
// rebuilds no table — so the writer that owns the root keeps its handles on the bytes it
// appends.
//   - The watermark is the one the slot table states, and a read view advances none: the commits it leaves unstated answer from the window
//   - The window replays the frames from that watermark to the log's readable end as records, not as order lines: an unconfirmed commit states no order line
//   - A table stating no watermark this build reads, and index files a root does not hold, cost the open a replay of the whole log into the same window: the log is the truth and the index files are derived from it, so an index nobody stated costs the read and never the answer
//   - A watermark stating bytes the log does not hold refuses the open as corruption, because that is the log losing bytes it holds rather than an index stating none
func openJournalReadAddress(log *journalLog) (*journalAddress, error) {
	a := &journalAddress{
		log:       log,
		orderPath: filepath.Join(log.dir, journalOrderFileName),
		slotsPath: filepath.Join(log.dir, journalSlotFileName),
		linePath:  filepath.Join(log.dir, journalOrderLineFileName),
		window:    &journalAddressWindow{values: map[string]journalValue{}},
		baseSeq:   log.baseSeq,
	}
	if missing, err := a.openReadHandles(); err != nil {
		return nil, err
	} else if missing != "" {
		return a.wholeLog(fmt.Sprintf("%s is not held beside the log", missing))
	}
	header, stated := a.readHeader()
	if !stated {
		return a.wholeLog(fmt.Sprintf("%s states no header this build reads", a.slotsPath))
	}
	if header.ValuesSize > log.size {
		a.closeIndex()
		return nil, fmt.Errorf("%w: %s holds %d bytes beside a header stating %d",
			ErrJournalCorrupt, log.path, log.size, header.ValuesSize)
	}
	orderSize, err := a.orderBytes()
	if err != nil {
		a.closeIndex()
		return nil, err
	}
	if header.OrderSize > orderSize {
		return a.wholeLog(fmt.Sprintf("%s holds %d byte(s) beside a header stating %d",
			a.orderPath, orderSize, header.OrderSize))
	}
	a.seq, a.valuesSize, a.orderSize = header.Seq, header.ValuesSize, header.OrderSize
	if err := a.replayWindow(header.ValuesSize); err != nil {
		a.closeIndex()
		return nil, err
	}
	return a, nil
}

// wholeLog answers this view from the log alone: the index files beside it state no
// watermark this build reads, so the open replays every frame into the window instead of
// refusing a root it was opened to read. It writes nothing, creates nothing and states no
// watermark, and it names the fallback and the read it cost, because that read grows with
// the history rather than staying inside a budget.
func (a *journalAddress) wholeLog(cause string) (*journalAddress, error) {
	a.closeIndex()
	a.window = &journalAddressWindow{values: map[string]journalValue{}}
	if err := a.replayWindow(0); err != nil {
		return nil, err
	}
	a.notice = fmt.Sprintf("the index of %s states no watermark this build reads (%s): the reading view replayed the whole log, %d byte(s), a read that grows with the history",
		a.log.dir, cause, a.log.size)
	return a, nil
}

// replayWindow folds the commits the stated watermark leaves unstated into this view's
// window, reading the frames from that byte to the log's readable end once. A frame
// that fails its checksum refuses the open as corruption, the refusal the write face's
// replay answers too, so a damaged record is named instead of dropped from the window.
func (a *journalAddress) replayWindow(from int64) error {
	_, err := journalReplay(journalLogBytes{log: a.log}, from, a.log.size, func(payload []byte, at int64) error {
		a.openBytes += int64(journalFrameHeaderBytes + len(payload))
		spans, err := journalSpansAt(payload, at)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, a.log.path, err)
		}
		if !journalRecordVersionReadable(spans.version) {
			return fmt.Errorf("%w: record version %d", ErrJournalFormat, spans.version)
		}
		a.window.fold(spans)
		return nil
	})
	if err != nil {
		return err
	}
	a.window.end = a.log.size
	return nil
}

// openReadHandles opens both index files for reading alone, so a root that states no
// index is answered from its log instead of gaining the files a write view would create
// under the writer that never stated them. It answers the path of the file a root does not
// hold and empty beside an open that holds both, and any other failure to open one is the
// filesystem's own refusal, stated with the file it names.
func (a *journalAddress) openReadHandles() (string, error) {
	order, err := os.Open(a.orderPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return a.orderPath, nil
		}
		return "", fmt.Errorf("tagent: open the order file of %s for reading: %w", a.log.dir, err)
	}
	a.order = order
	slots, err := os.Open(a.slotsPath)
	if err != nil {
		a.order = nil
		order.Close()
		if errors.Is(err, os.ErrNotExist) {
			return a.slotsPath, nil
		}
		return "", fmt.Errorf("tagent: open the slot table of %s for reading: %w", a.log.dir, err)
	}
	a.slots = slots
	a.openReadLines()
	return "", nil
}

// openOrder opens the writer's append handle on the order file of this index.
func (a *journalAddress) openOrder() error {
	order, err := os.OpenFile(a.orderPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the order file of %s: %w", a.log.dir, err)
	}
	a.order = order
	return nil
}

// openSlots opens the writer's in-place handle on the slot table of this index.
func (a *journalAddress) openSlots() error {
	slots, err := os.OpenFile(a.slotsPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the slot table of %s: %w", a.log.dir, err)
	}
	a.slots = slots
	return nil
}

// openLines opens the writer's in-place handle on the order-line index of this address.
func (a *journalAddress) openLines() error {
	lines, err := os.OpenFile(a.linePath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the order-line index of %s: %w", a.log.dir, err)
	}
	a.lines = lines
	return nil
}

// openReadLines opens the read view's handle on the order-line index when the root holds
// one, and answers none beside a root without one: a read view states no word and
// creates no file, so an index it cannot read costs the walk its seek and never its
// answer.
func (a *journalAddress) openReadLines() {
	lines, err := os.Open(a.linePath)
	if err != nil {
		a.lines = nil
		return
	}
	a.lines = lines
}

// linesState reports whether the order-line index states one word for every slot of the
// table the header states. A file of any other size states words for slots the table does
// not hold or none for the slots it does, so it is stated again from the log beside them
// rather than trusted word by word.
func (a *journalAddress) linesState(capacity int64) bool {
	if a.lines == nil {
		return false
	}
	stat, err := a.lines.Stat()
	if err != nil {
		return false
	}
	return stat.Size() == capacity*journalOrderLineWordBytes
}

// resizeLines states an order-line index of capacity words, every one of them unknown:
// a table stated again names no commit, which is the same state an empty slot states.
func (a *journalAddress) resizeLines(capacity int64) error {
	if a.lines == nil {
		return nil
	}
	if err := a.lines.Truncate(0); err != nil {
		return fmt.Errorf("tagent: discard the order-line index of %s: %w", a.log.dir, err)
	}
	if err := a.lines.Truncate(capacity * journalOrderLineWordBytes); err != nil {
		return fmt.Errorf("tagent: size the order-line index of %s: %w", a.log.dir, err)
	}
	word := lineOffsetWord(journalOrderLineUnknown)
	batch := make([]byte, journalOrderLineWordBytes*min(journalSlotScanBatch, capacity))
	for at := 0; at < len(batch); at += journalOrderLineWordBytes {
		copy(batch[at:], word)
	}
	for base := int64(0); base < capacity; base += journalSlotScanBatch {
		records := min(journalSlotScanBatch, capacity-base)
		if _, err := a.lines.WriteAt(batch[:records*journalOrderLineWordBytes], base*journalOrderLineWordBytes); err != nil {
			return fmt.Errorf("tagent: state the unknown words of %s: %w", a.log.dir, err)
		}
	}
	return nil
}

// readLineOffset answers the word one slot states: the byte the order line of the commit
// that last stated that slot's key ends at, or journalOrderLineUnknown beside a slot no
// commit stated or a view that holds no such file.
func (a *journalAddress) readLineOffset(index int64) (int64, error) {
	if a.lines == nil {
		return journalOrderLineUnknown, nil
	}
	word := make([]byte, journalOrderLineWordBytes)
	if _, err := a.lines.ReadAt(word, index*journalOrderLineWordBytes); err != nil {
		return journalOrderLineUnknown, fmt.Errorf("tagent: read the order-line index of %s: %w", a.log.dir, err)
	}
	return int64(binary.LittleEndian.Uint64(word)), nil
}

// writeLineOffset states the word of one slot, which is what makes the next read of that
// key's order line one read of the index instead of a walk of the statements in front of
// it. A view that holds no such file states nothing and is not a failure: the seek is an
// accelerator over the order file, never the order file itself.
func (a *journalAddress) writeLineOffset(index, lineEnd int64) error {
	if a.lines == nil {
		return nil
	}
	if _, err := a.lines.WriteAt(lineOffsetWord(lineEnd), index*journalOrderLineWordBytes); err != nil {
		return fmt.Errorf("tagent: state the order-line index of %s: %w", a.log.dir, err)
	}
	a.staged += journalOrderLineWordBytes
	return nil
}

// lineOffsetWord answers the bytes one word of the order-line index states.
func lineOffsetWord(offset int64) []byte {
	word := make([]byte, journalOrderLineWordBytes)
	binary.LittleEndian.PutUint64(word, uint64(offset))
	return word
}

// closeIndex releases both handles of an index that failed to open.
func (a *journalAddress) closeIndex() {
	if a.order != nil {
		a.order.Close()
		a.order = nil
	}
	if a.slots != nil {
		a.slots.Close()
		a.slots = nil
	}
	if a.lines != nil {
		a.lines.Close()
		a.lines = nil
	}
}

// orderBytes answers the size of the order file this index states.
func (a *journalAddress) orderBytes() (int64, error) {
	stat, err := a.order.Stat()
	if err != nil {
		return 0, fmt.Errorf("tagent: read the order file of %s: %w", a.log.dir, err)
	}
	return stat.Size(), nil
}

// rebuild drops every derived index file and states the whole log
// again, which is what an open pays when the header states no watermark it can
// trust: a header written before this build, a header torn by a stop, a record the
// table cannot place and a header whose order statements were lost all take this
// one path.
func (a *journalAddress) rebuild(parent string) error {
	if err := a.order.Truncate(0); err != nil {
		return fmt.Errorf("tagent: discard the order file of %s: %w", a.log.dir, err)
	}
	a.seq, a.valuesSize, a.orderSize, a.statedSeq = a.baseSeq, 0, 0, 0
	if err := a.resetTable(journalSlotCapacity, journalSlotStride); err != nil {
		return err
	}
	return a.derive(0, true, parent)
}

// rebuildFrom runs one rebuild of the address table as the call that asked for it
// states what it was doing, so the pass records its own pair of clock reads beside
// the account of the caller rather than under a name no caller stated.
func (a *journalAddress) rebuildFrom(parent string) error {
	closed := a.openPass("rebuild", parent)
	defer closed()
	return a.rebuild(parent)
}

// derive states every commit of the log from the given byte on: one order line and
// one slot record per commit, then the watermark. A commit whose frame is torn is
// cut by the open that read the log before this runs, so the span it is given holds
// whole frames alone. The statements already on disk past from are discarded by the
// caller, so a derived index file never merges with the one it replaces.
func (a *journalAddress) derive(from int64, rewrite bool, parent string) error {
	if rewrite {
		if err := a.order.Truncate(0); err != nil {
			return fmt.Errorf("tagent: discard the order file of %s: %w", a.log.dir, err)
		}
	}
	complete, err := journalReplay(journalLogBytes{log: a.log}, from, a.log.size, func(payload []byte, at int64) error {
		a.openBytes += int64(journalFrameHeaderBytes + len(payload))
		spans, err := journalSpansAt(payload, at)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, a.log.path, err)
		}
		if !journalRecordVersionReadable(spans.version) {
			return fmt.Errorf("%w: record version %d", ErrJournalFormat, spans.version)
		}
		return a.state(spans, at+int64(len(payload)), parent)
	})
	if err != nil {
		return err
	}
	if complete != a.log.size {
		return fmt.Errorf("%w: %s holds %d byte(s) past the watermark the index files state",
			ErrJournalCorrupt, a.log.path, a.log.size-complete)
	}
	return a.flush(true)
}

// note files one appended commit for the index files, so the barrier that confirms
// the frame states it instead of leaving it to the next open. The frame's own bytes
// are what the window an open replays grows by, so the commit is counted in them
// beside the statements stating it costs.
func (a *journalAddress) note(spans journalSpans, valuesEnd, frameBytes int64) {
	a.stagedLog += frameBytes
	a.pending = append(a.pending, journalAddressCommit{spans: spans, valuesEnd: valuesEnd})
}

// confirm states every pending commit and advances the watermark behind them. The
// index files take their own barrier only when the bytes they staged reach the
// budget, so a commit pays the log's barrier alone and a stop costs the statements
// inside the budget: those are stated again from the log, which is the one repair
// rule this index has and the reason the commit needs no second barrier here.
func (a *journalAddress) confirm() error {
	if len(a.pending) == 0 {
		return a.flush(false)
	}
	if a.broken {
		a.pending = a.pending[:0]
		a.broken = false
		return a.rebuildFrom("commit")
	}
	seq, valuesSize, orderSize := a.seq, a.valuesSize, a.orderSize
	for _, commit := range a.pending {
		if err := a.state(commit.spans, commit.valuesEnd, "commit"); err != nil {
			a.rollback(seq, valuesSize, orderSize)
			return err
		}
	}
	a.pending = a.pending[:0]
	if err := a.flush(false); err != nil {
		a.rollback(seq, valuesSize, orderSize)
		return err
	}
	return nil
}

// rollback returns this index to the watermark it stated and drops the statements
// past it, so the retry that follows appends behind the same byte instead of beside
// a line it never confirmed. The slot records already written for the dropped
// commits name spans past the watermark they were rolled back to, which every
// reader rejects: a record is answered only inside the bytes this index states. The
// frames those commits appended stay in the log, so the window an open replays keeps
// counting their bytes after a rollback: only the statements are dropped.
func (a *journalAddress) rollback(seq, valuesSize, orderSize int64) {
	a.seq, a.valuesSize, a.orderSize = seq, valuesSize, orderSize
	if err := a.order.Truncate(orderSize); err != nil {
		a.broken = true
	}
}

// state appends one commit's order line and states the slot record of every key it
// names, so the index files move by the keys of that commit and never by the history
// behind it.
func (a *journalAddress) state(spans journalSpans, valuesEnd int64, parent string) error {
	return a.fileCommit(orderStatementOf(a.seq+1, valuesEnd, spans), parent)
}

// orderStatementOf answers one commit as the order file states it: the rank that
// commit holds, the byte of the log its frame ends at, the span of every key it
// wrote and the keys it deleted. A commit and a replay of the log both state their
// records through it, so one commit states the same bytes either way.
func orderStatementOf(seq, valuesEnd int64, spans journalSpans) journalOrderStatement {
	statement := journalOrderStatement{Seq: seq, ValuesEnd: valuesEnd, Deletes: spans.deletes}
	for _, put := range spans.puts {
		statement.Puts = append(statement.Puts, journalOrderPut{Key: put.key, journalAddressSpan: spanOf(put.value)})
	}
	return statement
}

// errOrderUnplaceable reports an order line this build cannot place: bytes shorter
// than a statement checksum, or a statement the checksum in front of it refuses. The
// commits such a line states are stated again from the log, which is the one repair
// rule this index has.
var errOrderUnplaceable = errors.New("memory: the order file states no commit this build can place")

// journalOrderLine answers the bytes one order statement is filed as: the checksum
// of its statement bytes in front of them, then the statement and its line break.
func journalOrderLine(statement journalOrderStatement) ([]byte, error) {
	body, err := json.Marshal(statement)
	if err != nil {
		return nil, err
	}
	line := make([]byte, 0, journalOrderChecksumBytes+len(body)+1)
	line = append(line, fmt.Sprintf("%08x ", crc32.Checksum(body, journalCRCTable))...)
	line = append(line, body...)
	return append(line, '\n'), nil
}

// journalOrderStatementOfLine answers the statement one order line states, and
// refuses a line whose checksum does not cover the bytes behind it.
func journalOrderStatementOfLine(line []byte) (journalOrderStatement, error) {
	var statement journalOrderStatement
	if len(line) < journalOrderChecksumBytes {
		return statement, fmt.Errorf("%w: %d byte(s) is shorter than a statement checksum", errOrderUnplaceable, len(line))
	}
	sum, err := strconv.ParseUint(string(line[:8]), 16, 32)
	if err != nil || line[8] != ' ' {
		return statement, fmt.Errorf("%w: %q states no statement checksum", errOrderUnplaceable, line[:journalOrderChecksumBytes])
	}
	body := line[journalOrderChecksumBytes:]
	if crc32.Checksum(body, journalCRCTable) != uint32(sum) {
		return statement, fmt.Errorf("%w: the %d byte(s) behind its checksum name no commit", errOrderUnplaceable, len(body))
	}
	if err := json.Unmarshal(body, &statement); err != nil {
		return statement, fmt.Errorf("%w: %w", errOrderUnplaceable, err)
	}
	return statement, nil
}

// fileCommit appends one commit's order line and states the slot record of every key
// it names, so the index files move by the keys of that commit and never by the
// history behind it.
func (a *journalAddress) fileCommit(statement journalOrderStatement, parent string) error {
	line, err := journalOrderLine(statement)
	if err != nil {
		return fmt.Errorf("tagent: state the commit of %s: %w", a.log.dir, err)
	}
	if _, err := a.order.Write(line); err != nil {
		return fmt.Errorf("tagent: append the order of %s: %w", a.log.dir, err)
	}
	a.orderSize += int64(len(line))
	a.staged += int64(len(line))
	for _, put := range statement.Puts {
		if err := a.file(put.Key, journalSlotHeld, put.journalAddressSpan, parent, a.orderSize); err != nil {
			return err
		}
		a.staged += a.stride
	}
	for _, key := range statement.Deletes {
		if err := a.file(key, journalSlotAbsent, journalAddressSpan{}, parent, a.orderSize); err != nil {
			return err
		}
		a.staged += a.stride
	}
	a.seq = statement.Seq
	a.valuesSize = statement.ValuesEnd
	return nil
}

// file states one key's record in the slot its key addresses: the first slot of the
// probe chain that holds that key or is empty. A key the table already addresses is
// replaced in place, which is what a later commit of the same identity is, and a key
// a commit deletes is stated absent so the chain it sits inside stays walkable.
func (a *journalAddress) file(key string, state uint8, span journalAddressSpan, parent string, lineEnd int64) error {
	err := a.fileOnce(key, state, span, parent, lineEnd)
	if !errors.Is(err, errSlotUnplaceable) || !a.statesIndexFiles() {
		return err
	}
	if rebuildErr := a.rebuildFrom(parent); rebuildErr != nil {
		return rebuildErr
	}
	return a.fileOnce(key, state, span, parent, lineEnd)
}

// statesIndexFiles reports whether this view states the index files of its log: a
// write view holds both handles and rewrites them, and a read view holds them for
// reading alone, so the repair that states a table again belongs to the write view.
func (a *journalAddress) statesIndexFiles() bool {
	return a.window == nil && a.order != nil && a.slots != nil
}

// fileOnce states one key's record in the table as it reads, which is what the
// repair of file retries after the table is stated again.
func (a *journalAddress) fileOnce(key string, state uint8, span journalAddressSpan, parent string, lineEnd int64) error {
	if int64(len(key)) > a.stride-journalSlotRecordHeaderBytes {
		stride := a.stride
		for int64(len(key)) > stride-journalSlotRecordHeaderBytes {
			stride *= 2
		}
		if err := a.grow(a.capacity, stride, parent); err != nil {
			return err
		}
	}
	if a.used*100 >= a.capacity*journalSlotLoadPercent {
		if err := a.grow(a.capacity*2, a.stride, parent); err != nil {
			return err
		}
	}
	index, taken, err := a.probe(key)
	if err != nil {
		return err
	}
	if err := a.writeRecord(index, encodeSlotRecord(a.stride, state, key, span)); err != nil {
		return err
	}
	if err := a.writeLineOffset(index, lineEnd); err != nil {
		return err
	}
	if !taken {
		a.used++
	}
	return nil
}

// probe answers the slot index one key's record belongs at, and whether the table
// already addresses that key there. It stops at the first empty slot, which is what
// files a key without an offset table.
//
// The slots the walk inspects are bounded by the table and never by a measured chain:
// a linear probe inserts at the first empty slot, so a chain that meets an empty slot
// proves the key absent, and every slot the chain walks but its own holds another key,
// which makes used+1 the tight bound a table of used keys can state. A walk that
// exhausts it met a table whose records disagree with the used count its header
// states, which errSlotUnplaceable reports so the caller repairs the table from the
// order file — the one repair rule this index has.
func (a *journalAddress) probe(key string) (int64, bool, error) {
	base := slotAddressOf(key, a.capacity)
	for step := int64(0); step <= a.used; step++ {
		index := (base + step) % a.capacity
		record, err := a.readRecord(index)
		if err != nil {
			return 0, false, err
		}
		state, name, _, ok := decodeSlotRecord(record)
		if !ok {
			return 0, false, fmt.Errorf("%w: %s at %d", errSlotUnplaceable, a.slotsPath, index)
		}
		if state == journalSlotEmpty {
			return index, false, nil
		}
		if name == key {
			return index, true, nil
		}
	}
	return 0, false, fmt.Errorf("%w: %s holds no empty slot in the %d the chain of %q may walk",
		errSlotUnplaceable, a.slotsPath, a.used+1, key)
}

// lookup answers the span a key's record states and whether the hot index holds
// the key, states it absent, or never states it, rebuilding the table from the order
// file when a record cannot be placed. A read view answers its window first because
// a commit the writer has not stated is named there and in no slot record; a view
// that opened no table answers the window alone.
func (a *journalAddress) lookup(key string) (journalAddressSpan, uint8, error) {
	if value, named := a.windowValue(key); named {
		if value.deleted {
			return journalAddressSpan{}, journalSlotAbsent, nil
		}
		return spanOf(value), journalSlotHeld, nil
	}
	if a.slots == nil {
		return journalAddressSpan{}, journalSlotEmpty, nil
	}
	span, state, err := a.lookupOnce(key)
	if !errors.Is(err, errSlotUnplaceable) || !a.statesIndexFiles() {
		return span, state, err
	}
	if rebuildErr := a.rebuildFrom("read"); rebuildErr != nil {
		return journalAddressSpan{}, journalSlotEmpty, rebuildErr
	}
	return a.lookupOnce(key)
}

// orderLineOf answers the byte the order line of the commit that last stated one key ends
// at, which is where a walk of the commit order starts when the caller already knows the
// key it wants: one probe of the slot table and one read of the order-line index, whatever
// the history committed after that commit holds. It answers false beside a key the table
// holds none of, a record outside the bytes this view states, and a view that states no
// order-line index — a caller that gets no line walks the order file as it did before this
// index existed.
//
// A record the table cannot place takes the one repair rule of this index: a write view
// states both index files again from the log, and a read view answers no seek and walks the
// whole order file, so the repair never answers a line nobody stated.
func (a *journalAddress) orderLineOf(key string) (int64, bool, error) {
	lineEnd, held, err := a.orderLineOnce(key)
	if !errors.Is(err, errSlotUnplaceable) || !a.statesIndexFiles() {
		return lineEnd, held, err
	}
	if rebuildErr := a.rebuildFrom("read"); rebuildErr != nil {
		return 0, false, rebuildErr
	}
	return a.orderLineOnce(key)
}

// orderLineOnce answers the seek of one key from the table as it reads, which is what the
// repair of orderLineOf retries after the table is stated again.
func (a *journalAddress) orderLineOnce(key string) (int64, bool, error) {
	index, state, span, err := a.findRecord(key)
	if err != nil || index < 0 || state != journalSlotHeld || !a.stated(span) {
		return 0, false, err
	}
	lineEnd, err := a.readLineOffset(index)
	if err != nil || lineEnd == journalOrderLineUnknown || lineEnd > a.orderSize {
		return 0, false, err
	}
	return lineEnd, true, nil
}

// windowValue answers the position a read view's window states for one key, and
// whether the window names that key at all: a key it does not name is answered by the
// index files.
func (a *journalAddress) windowValue(key string) (journalValue, bool) {
	if a.window == nil {
		return journalValue{}, false
	}
	value, named := a.window.values[key]
	return value, named
}

// lookupOnce answers one key's span and state from the table as it reads, which
// is what lookup retries after the table is rebuilt.
func (a *journalAddress) lookupOnce(key string) (journalAddressSpan, uint8, error) {
	index, state, span, err := a.findRecord(key)
	if err != nil || index < 0 {
		return journalAddressSpan{}, journalSlotEmpty, err
	}
	if state == journalSlotAbsent {
		return journalAddressSpan{}, state, nil
	}
	if !a.stated(span) {
		return journalAddressSpan{}, journalSlotEmpty, nil
	}
	return span, state, nil
}

// findRecord answers the slot position one key's record sits at as this table reads,
// the state that record states and the span it names, and answers position -1 beside
// a chain that meets an empty slot before the key: an empty slot is the proof that a
// linear probe never filed that key.
//
// The walk is bounded by the table alone, for the reason probe states: used+1 slots
// cover every chain a table of used keys can state, and a walk that exhausts it met
// records that disagree with the used count the header states, which
// errSlotUnplaceable reports instead of answering the key absent — a table this build
// cannot place is not a table that holds no key, and a caller that reads it as absence
// would answer a legal miss as a fact the store lost.
func (a *journalAddress) findRecord(key string) (int64, uint8, journalAddressSpan, error) {
	base := slotAddressOf(key, a.capacity)
	for step := int64(0); step <= a.used; step++ {
		index := (base + step) % a.capacity
		record, err := a.readRecord(index)
		if err != nil {
			return -1, journalSlotEmpty, journalAddressSpan{}, err
		}
		state, name, span, ok := decodeSlotRecord(record)
		if !ok {
			return -1, journalSlotEmpty, journalAddressSpan{}, fmt.Errorf("%w: %s at %d", errSlotUnplaceable, a.slotsPath, index)
		}
		if state == journalSlotEmpty {
			return -1, journalSlotEmpty, journalAddressSpan{}, nil
		}
		if name == key {
			return index, state, span, nil
		}
	}
	return -1, journalSlotEmpty, journalAddressSpan{}, fmt.Errorf("%w: %s holds no empty slot in the %d the chain of %q may walk",
		errSlotUnplaceable, a.slotsPath, a.used+1, key)
}

// stated reports that one span names bytes this view states, which is the guard that
// keeps a rolled back commit's record from answering a value the watermark never
// reached: a write view states the bytes its own commits reached, and a read view
// states the bytes its window replayed, so the slots a writer filed inside that window
// answer instead of reading as unheld.
func (a *journalAddress) stated(span journalAddressSpan) bool {
	end := a.valuesSize
	if a.window != nil {
		end = a.window.end
	}
	return span.Offset >= 0 && span.Offset+int64(span.Length) <= end
}

// readRecord reads one slot's record, which is the one read a point lookup pays
// before it reads the value the record names.
func (a *journalAddress) readRecord(index int64) ([]byte, error) {
	record := make([]byte, a.stride)
	if _, err := a.slots.ReadAt(record, a.recordOffset(index)); err != nil {
		return nil, fmt.Errorf("tagent: read the slot %d of %s: %w", index, a.log.dir, err)
	}
	return record, nil
}

// writeRecord states one slot's record in place, which is the one write a commit
// pays per key it touches.
func (a *journalAddress) writeRecord(index int64, record []byte) error {
	if _, err := a.slots.WriteAt(record, a.recordOffset(index)); err != nil {
		return fmt.Errorf("tagent: state the slot %d of %s: %w", index, a.log.dir, err)
	}
	return nil
}

// recordOffset answers the byte one slot's record starts at.
func (a *journalAddress) recordOffset(index int64) int64 {
	return journalSlotHeaderBytes + index*a.stride
}

// eachRecord visits every slot the table states, stating the table again from the
// order file when a record it meets cannot be placed, which is the same repair a
// point read takes: a listing answers the held keys or nothing, never a prefix of
// them read through a slot nobody wrote.
func (a *journalAddress) eachRecord(visit func(state uint8, key string, span journalAddressSpan) error) error {
	once := func(_ int64, state uint8, key string, span journalAddressSpan) error {
		return visit(state, key, span)
	}
	err := a.eachRecordOnce(once)
	if !errors.Is(err, errSlotUnplaceable) || !a.statesIndexFiles() {
		return err
	}
	if rebuildErr := a.rebuildFrom("read"); rebuildErr != nil {
		return rebuildErr
	}
	return a.eachRecordOnce(once)
}

// eachRecordOnce visits every slot the table reads, in batches, so an enumeration
// pays one sequential read of the table instead of one read per slot.
func (a *journalAddress) eachRecordOnce(visit func(index int64, state uint8, key string, span journalAddressSpan) error) error {
	batch := make([]byte, a.stride*journalSlotScanBatch)
	for base := int64(0); base < a.capacity; base += journalSlotScanBatch {
		records := min(journalSlotScanBatch, a.capacity-base)
		chunk := batch[:records*a.stride]
		if _, err := a.slots.ReadAt(chunk, a.recordOffset(base)); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("tagent: read the slots of %s: %w", a.log.dir, err)
		}
		a.slotScanBytes += records * a.stride
		for at := int64(0); at < records; at++ {
			state, key, span, ok := decodeSlotRecord(chunk[at*a.stride : (at+1)*a.stride])
			if !ok {
				return fmt.Errorf("%w: %s at %d", errSlotUnplaceable, a.slotsPath, base+at)
			}
			if err := visit(base+at, state, key, span); err != nil {
				return err
			}
		}
	}
	return nil
}

// grow states the whole table again at the given shape, through a staged file
// renamed over the table: a table that could not state a key's whole name, or one
// that filled, is replaced by a wider one instead of half rewritten, so a stop
// during the pass leaves the table it started with.
func (a *journalAddress) grow(capacity, stride int64, parent string) error {
	closed := a.openPass("grow", parent)
	defer closed()
	staged := a.slotsPath + journalAddressStageSuffix
	file, err := os.OpenFile(staged, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: open the staged slot table of %s: %w", a.log.dir, err)
	}
	if err := fillEmptySlots(file, capacity, stride); err != nil {
		file.Close()
		return err
	}
	stagedLines, err := a.stageLines(capacity)
	if err != nil {
		file.Close()
		return err
	}
	used := int64(0)
	err = a.eachRecordOnce(func(at int64, state uint8, key string, span journalAddressSpan) error {
		if state == journalSlotEmpty {
			return nil
		}
		index, err := stageProbe(file, capacity, stride, key)
		if err != nil {
			return err
		}
		used++
		if _, err := file.WriteAt(encodeSlotRecord(stride, state, key, span), journalSlotHeaderBytes+index*stride); err != nil {
			return err
		}
		return a.stageLineWord(stagedLines, at, index)
	})
	if err != nil {
		if stagedLines != nil {
			stagedLines.Close()
			os.Remove(stagedLines.Name())
		}
		file.Close()
		return err
	}
	if err := writeSlotHeaderAt(file, headerOf(capacity, stride, used, a.statedSeq, a.statedValuesSize, a.statedOrderSize)); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(staged, a.slotsPath); err != nil {
		return fmt.Errorf("tagent: replace the slot table of %s: %w", a.log.dir, err)
	}
	a.slots.Close()
	reopened, err := os.OpenFile(a.slotsPath, os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("tagent: reopen the slot table of %s: %w", a.log.dir, err)
	}
	a.slots, a.capacity, a.stride, a.used = reopened, capacity, stride, used
	return a.replaceLines(stagedLines, capacity)
}

// stageLines opens the file the order-line index of a table being staged is written into,
// filled with the word an unknown slot states.
func (a *journalAddress) stageLines(capacity int64) (*os.File, error) {
	if a.lines == nil {
		return nil, nil
	}
	staged, err := os.OpenFile(a.linePath+journalAddressStageSuffix, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("tagent: open the staged order-line index of %s: %w", a.log.dir, err)
	}
	live := a.lines
	a.lines = staged
	resizeErr := a.resizeLines(capacity)
	a.lines = live
	if resizeErr != nil {
		staged.Close()
		os.Remove(staged.Name())
		return nil, resizeErr
	}
	return staged, nil
}

// stageLineWord states where the order line of one staged slot ends: the word the slot a key
// moves to takes is the word the slot it came from stated, so the seek survives a table
// stated again at another shape.
func (a *journalAddress) stageLineWord(staged *os.File, from, to int64) error {
	if staged == nil {
		return nil
	}
	lineEnd, err := a.readLineOffset(from)
	if err != nil {
		return err
	}
	if lineEnd == journalOrderLineUnknown {
		return nil
	}
	if _, err := staged.WriteAt(lineOffsetWord(lineEnd), to*journalOrderLineWordBytes); err != nil {
		return fmt.Errorf("tagent: state the staged order-line index of %s: %w", a.log.dir, err)
	}
	return nil
}

// replaceLines states the staged order-line index in place of this address's file and
// reopens it, on the terms the staged table is renamed over the table it replaces: a stop
// during the pass leaves the file it started with.
func (a *journalAddress) replaceLines(staged *os.File, capacity int64) error {
	if staged == nil {
		return nil
	}
	if err := staged.Sync(); err != nil {
		staged.Close()
		return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Rename(staged.Name(), a.linePath); err != nil {
		return fmt.Errorf("tagent: replace the order-line index of %s: %w", a.log.dir, err)
	}
	if a.lines != nil {
		a.lines.Close()
	}
	reopened, err := os.OpenFile(a.linePath, os.O_RDWR, 0o600)
	if err != nil {
		a.lines = nil
		return fmt.Errorf("tagent: reopen the order-line index of %s: %w", a.log.dir, err)
	}
	a.lines = reopened
	return nil
}

// resetTable states an empty table of the given shape at the watermark this index
// reaches, which is what a rebuild starts from. The records are made durable before
// the header states the watermark, so a header never names a table that a stop can
// empty: every slot reads as empty or as a record whose checksum holds.
func (a *journalAddress) resetTable(capacity, stride int64) error {
	closed := a.openPass("resetTable", "rebuild")
	defer closed()
	if err := a.slots.Truncate(0); err != nil {
		return fmt.Errorf("tagent: discard the slot table of %s: %w", a.log.dir, err)
	}
	if err := a.slots.Truncate(journalSlotHeaderBytes + capacity*stride); err != nil {
		return fmt.Errorf("tagent: size the slot table of %s: %w", a.log.dir, err)
	}
	if err := fillEmptySlots(a.slots, capacity, stride); err != nil {
		return err
	}
	if err := a.resizeLines(capacity); err != nil {
		return err
	}
	if err := a.slots.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
	}
	a.capacity, a.stride, a.used = capacity, stride, 0
	return a.writeHeader()
}

// headerOf answers the header one shape and load state at the given watermark.
func headerOf(capacity, stride, used, seq, valuesSize, orderSize int64) journalSlotHeader {
	return journalSlotHeader{
		Magic:      journalSlotMagic,
		Version:    journalSlotVersion,
		Capacity:   capacity,
		Stride:     stride,
		Used:       used,
		Seq:        seq,
		ValuesSize: valuesSize,
		OrderSize:  orderSize,
	}
}

// writeHeader states the watermark of the derived index files in one small in-place
// write at the start of the slot table. It takes no barrier of its own: the header
// is an accelerator over the order file, so a header that is stale or torn by a stop
// costs the statements past the watermark it last stated and never a lost fact.
func (a *journalAddress) writeHeader() error {
	if err := writeSlotHeaderAt(a.slots, headerOf(a.capacity, a.stride, a.used, a.seq, a.valuesSize, a.orderSize)); err != nil {
		return err
	}
	a.statedSeq, a.statedValuesSize, a.statedOrderSize = a.seq, a.valuesSize, a.orderSize
	return nil
}

// flush completes the barriers of the derived index files and advances the watermark
// once the bytes they staged reach the index budget or the log bytes they leave
// unstated reach the log budget, or when a caller asks for the index files to reach
// the log. The budgets are what keep a commit at one barrier: the log's own barrier
// is the truth, and the index files behind it are stated again from the log inside
// the window a budget bounds. Both are measured because the window is the replay an
// open pays, and a commit that states few index bytes may yet carry many log bytes.
//
// A flush over nothing staged confirms nothing: the barrier pair and the header it
// writes state the bytes the last flush stated, so asking for the index files to
// reach a log they already reach takes no barrier of its own.
func (a *journalAddress) flush(force bool) error {
	if a.staged == 0 && a.stagedLog == 0 {
		return nil
	}
	if a.staged < journalIndexSyncBytes && a.stagedLog < journalLogSyncBytes && !force {
		return nil
	}
	if err := a.order.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
	}
	if err := a.slots.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
	}
	if a.lines != nil {
		if err := a.lines.Sync(); err != nil {
			return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
		}
	}
	a.staged, a.stagedLog = 0, 0
	return a.writeHeader()
}

// read answers one key's value from its address: the record that states the span and
// one read of the bytes that span names. A key the table does not address is not
// held, and the read costs the value alone — never the frame that carries it and
// never a walk of the statements behind it.
func (a *journalAddress) read(key string) ([]byte, bool, error) {
	span, state, err := a.lookup(key)
	if err != nil || state != journalSlotHeld {
		return nil, false, err
	}
	return a.value(span)
}

// value answers the bytes one stated span names in the log, which is the read a
// point lookup and a walk alike pay for the value they answer. The span is a logical
// log position, so a value a segment carries is read from that segment and one the
// open segment carries from the writer's file.
func (a *journalAddress) value(span journalAddressSpan) ([]byte, bool, error) {
	if span.Null {
		return nil, true, nil
	}
	a.valueReads++
	token := make([]byte, span.Length)
	if _, err := a.log.readAt(token, span.Offset); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, a.log.path, err)
	}
	var body []byte
	if err := json.Unmarshal(token, &body); err != nil {
		return nil, false, fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, a.log.path, err)
	}
	return body, true, nil
}

// eachHeld answers the held keys carrying prefix with the span each one states, in
// key order, from one pass over the table: a slot states a key only while it is held,
// so no value is read and no statement is walked. A read view answers the rows of the
// table and the keys of its window, less the keys that window's last commits deleted,
// because a commit the writer has not stated is named in no row; a view that opened no
// table answers its window alone. A caller that reads the value of every key it names
// pays one probe of the table rather than one per key.
func (a *journalAddress) eachHeld(prefix string, visit func(key string, span journalAddressSpan) error) error {
	held := make(map[string]journalAddressSpan, a.used)
	if a.slots != nil {
		err := a.eachRecord(func(state uint8, key string, span journalAddressSpan) error {
			if state != journalSlotHeld || !a.stated(span) || !strings.HasPrefix(key, prefix) {
				return nil
			}
			held[key] = span
			return nil
		})
		if err != nil {
			return err
		}
	}
	a.foldWindowSpans(prefix, held)
	out := make([]string, 0, len(held))
	for key := range held {
		out = append(out, key)
	}
	sort.Strings(out)
	for _, key := range out {
		if err := visit(key, held[key]); err != nil {
			return err
		}
	}
	return nil
}

// keys answers the held keys carrying prefix, in key order.
func (a *journalAddress) keys(prefix string) ([]string, error) {
	out := make([]string, 0, a.used)
	err := a.eachHeld(prefix, func(key string, _ journalAddressSpan) error {
		out = append(out, key)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// foldWindowSpans adds the keys a read view's window holds to held, less the keys its
// last commits deleted, so a listing answers the same key set and the same spans a
// read view's point read answers.
func (a *journalAddress) foldWindowSpans(prefix string, held map[string]journalAddressSpan) {
	if a.window == nil {
		return
	}
	for key, value := range a.window.values {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if value.deleted {
			delete(held, key)
			continue
		}
		held[key] = spanOf(value)
	}
}

// namespaces answers the key namespaces the held keys carry, in name order. A
// namespace whose last held key a commit deleted answers nothing, because a namespace
// states no key of its own, and a read view counts the namespaces of the keys it
// holds.
func (a *journalAddress) namespaces() ([]string, error) {
	seen := map[string]struct{}{}
	err := a.eachHeld("", func(key string, _ journalAddressSpan) error {
		seen[keyNamespaceOf(key)] = struct{}{}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out, nil
}

// eachNewestFirst answers the held keys from the newest commit to the oldest and
// stops when visit answers false. The walk reads the order file backwards in
// chunks, so the newest statement of a key decides whether it is held: a commit
// that deleted it answers absence and a commit that wrote it answers its span, and a
// statement answers the keys it wrote last first, which is the order inside one
// commit. No value is read unless visit asks for it, and no slot is read at all. A
// read view names the commits of its window first, newest of them first, because no
// order line states them.
//
// The walk keeps its own chunk buffer and states the offset it begins at once, so a
// visit that reads the view this address belongs to interleaves with it: the order one
// walk states is the one its start stated, and a commit that lands while a visit runs
// is named by the walk that starts after it and not by this one.
//
//   - A line the statement checksum refuses takes the repair rule of this index: a write view states the index files again from the log, and a read view replays the whole log and reads no statement after it, so the keys the walk answers are the log's and it resumes at the keys it has not named
func (a *journalAddress) eachNewestFirst(visit func(key string, span journalAddressSpan) (bool, error)) error {
	return a.eachNewestFirstAt(a.orderSize, visit)
}

// eachNewestFirstAt walks the commits this view states from the given order byte to the
// oldest, newest of them first: a caller that already knows where its own commit stands
// enters the order there instead of naming every commit filed after it. It answers exactly
// the commits, the order inside a commit and the repair of the whole walk, so the entry
// point decides what the walk reads and never what it answers.
//   - A byte past the statements this view states enters at the newest one, so a seek the
//     index cannot place costs the walk it would have taken and never a statement it skipped
func (a *journalAddress) eachNewestFirstAt(from int64, visit func(key string, span journalAddressSpan) (bool, error)) error {
	if from > a.orderSize || from < 0 {
		from = a.orderSize
	}
	seen := map[string]struct{}{}
	err := a.eachNewestFirstOnce(visit, seen, from)
	if !errors.Is(err, errOrderUnplaceable) {
		return err
	}
	if a.window == nil {
		if rebuildErr := a.rebuildFrom("read"); rebuildErr != nil {
			return rebuildErr
		}
		return a.eachNewestFirstOnce(visit, seen, a.orderSize)
	}
	if _, fallbackErr := a.wholeLog(fmt.Sprintf("%s states no commit its checksum holds", a.orderPath)); fallbackErr != nil {
		return fallbackErr
	}
	a.orderSize = 0
	return a.eachNewestFirstOnce(visit, seen, a.orderSize)
}

// eachStatementAtOrBelow walks every hot order statement through start in commit order,
// including deletes and repeated writes. It is the history a cross-tier snapshot needs
// when it enters below the newest slot state.
func (a *journalAddress) eachStatementAtOrBelow(start int64, visit func(key string, deleted bool) error) error {
	if start <= 0 {
		return nil
	}
	if start > a.orderSize {
		start = a.orderSize
	}
	reader := bufio.NewReader(io.NewSectionReader(a.order, 0, start))
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			statement, err := journalOrderStatementOfLine(bytes.TrimSpace(line))
			if err != nil {
				return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, a.orderPath, err)
			}
			for _, key := range statement.Deletes {
				if err := visit(key, true); err != nil {
					return err
				}
			}
			for _, put := range statement.Puts {
				if err := visit(put.Key, false); err != nil {
					return err
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("tagent: read the order file of %s: %w", a.log.dir, readErr)
		}
	}
}

// eachNewestFirstOnce walks the commits this view states and names the ones seen does
// not hold yet, which is what a walk resumes at after the index files were stated
// again from the log.
func (a *journalAddress) eachNewestFirstOnce(visit func(key string, span journalAddressSpan) (bool, error), seen map[string]struct{}, from int64) error {
	if a.window != nil && from >= a.orderSize {
		for at := len(a.window.order) - 1; at >= 0; at-- {
			key := a.window.order[at]
			if _, walked := seen[key]; walked {
				continue
			}
			seen[key] = struct{}{}
			value, named := a.window.values[key]
			if !named || value.deleted {
				continue
			}
			more, err := visit(key, spanOf(value))
			if err != nil {
				return err
			}
			if !more {
				return nil
			}
		}
	}
	var carried []byte
	var chunkBuffer []byte
	if pooled, held := a.orderChunks.Get().([]byte); held {
		chunkBuffer = pooled
	}
	defer func() { a.orderChunks.Put(chunkBuffer) }()
	for pos := from; pos > 0; {
		start := pos - journalOrderChunkBytes
		if start < 0 {
			start = 0
		}
		need := int(pos - start)
		if cap(chunkBuffer) < need {
			chunkBuffer = make([]byte, need)
		}
		chunk := chunkBuffer[:need]
		if _, err := a.order.ReadAt(chunk, start); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("tagent: read the order file of %s: %w", a.log.dir, err)
		}
		if carried != nil {
			chunk = append(chunk, carried...)
			carried = nil
		}
		lines := bytes.Split(chunk, []byte("\n"))
		first := 0
		if start > 0 {
			first = 1
			carried = append([]byte(nil), lines[0]...)
		}
		for at := len(lines) - 1; at >= first; at-- {
			line := bytes.TrimSpace(lines[at])
			if len(line) == 0 {
				continue
			}
			statement, err := journalOrderStatementOfLine(line)
			if err != nil {
				return fmt.Errorf("%w: %s: %w", ErrJournalCorrupt, a.orderPath, err)
			}
			for _, key := range statement.Deletes {
				seen[key] = struct{}{}
			}
			for slot := len(statement.Puts) - 1; slot >= 0; slot-- {
				put := statement.Puts[slot]
				if _, walked := seen[put.Key]; walked {
					continue
				}
				seen[put.Key] = struct{}{}
				more, err := visit(put.Key, put.journalAddressSpan)
				if err != nil {
					return err
				}
				if !more {
					return nil
				}
			}
		}
		pos = start
	}
	return nil
}

// eachNewestFirstValue answers the held keys from the newest commit to the oldest
// beside the value each key's newest statement carries, and stops when visit answers
// false. It is the walk a caller that ranks the entries against the values they state
// reads: one order walk for the keys and one value span read per key it names,
// nothing of the history behind the stop.
func (a *journalAddress) eachNewestFirstValue(visit func(key string, value []byte) (bool, error)) error {
	return a.eachNewestFirstAtValue(a.orderSize, visit)
}

// eachNewestFirstAtValue answers the held keys from the given order byte to the oldest
// beside the value each key's newest statement carries, on the terms eachNewestFirstAt
// states: the same commits, the same order inside a commit and the same repair, entered at
// the commit the caller names instead of at the newest one.
func (a *journalAddress) eachNewestFirstAtValue(from int64, visit func(key string, value []byte) (bool, error)) error {
	return a.eachNewestFirstAt(from, func(key string, span journalAddressSpan) (bool, error) {
		value, held, err := a.value(span)
		if err != nil {
			return false, err
		}
		if !held {
			return false, fmt.Errorf("%w: %s states no value for %s", ErrJournalCorrupt, a.log.path, key)
		}
		return visit(key, value)
	})
}

// close states the index files it leaves behind, then releases both handles: a
// clean stop leaves a table the next open reads alone instead of replaying the
// commits inside the barrier budget.
func (a *journalAddress) close() error {
	if a == nil || a.slots == nil {
		return nil
	}
	flushErr := a.flush(true)
	order, slots, lines := a.order, a.slots, a.lines
	a.order, a.slots, a.lines = nil, nil, nil
	closed := []error{flushErr}
	if order != nil {
		closed = append(closed, order.Close())
	}
	if slots != nil {
		closed = append(closed, slots.Close())
	}
	if lines != nil {
		closed = append(closed, lines.Close())
	}
	return errors.Join(closed...)
}

// readHeader reads the watermark and the shape one slot table states, and answers
// false beside a table that is missing, torn or states no watermark: the three are
// the table this build cannot trust, and the caller rebuilds the index files from
// the log instead of reading through a watermark that describes no bytes.
func (a *journalAddress) readHeader() (journalSlotHeader, bool) {
	body := make([]byte, journalSlotHeaderBytes)
	if _, err := a.slots.ReadAt(body, 0); err != nil {
		return journalSlotHeader{}, false
	}
	a.openBytes += int64(len(body))
	header, stated := decodeSlotHeader(body)
	if !stated {
		return journalSlotHeader{}, false
	}
	stat, err := a.slots.Stat()
	if err != nil || !journalHeaderStates(header, stat.Size()) {
		return journalSlotHeader{}, false
	}
	a.capacity, a.stride, a.used = header.Capacity, header.Stride, header.Used
	a.statedSeq, a.statedValuesSize, a.statedOrderSize = header.Seq, header.ValuesSize, header.OrderSize
	return header, true
}

// decodeSlotHeader answers the header one slot table states, and false beside bytes
// this build cannot place: a magic or version it does not write, a checksum that
// does not hold, or a shape no record can be addressed inside.
func decodeSlotHeader(body []byte) (journalSlotHeader, bool) {
	if len(body) < journalSlotHeaderBytes {
		return journalSlotHeader{}, false
	}
	var header journalSlotHeader
	header.Magic = binary.LittleEndian.Uint32(body[0:4])
	header.Version = binary.LittleEndian.Uint32(body[4:8])
	header.Capacity = int64(binary.LittleEndian.Uint64(body[8:16]))
	header.Stride = int64(binary.LittleEndian.Uint64(body[16:24]))
	header.Seq = int64(binary.LittleEndian.Uint64(body[24:32]))
	header.ValuesSize = int64(binary.LittleEndian.Uint64(body[32:40]))
	header.OrderSize = int64(binary.LittleEndian.Uint64(body[40:48]))
	header.Used = int64(binary.LittleEndian.Uint64(body[48:56]))
	header.Sum = binary.LittleEndian.Uint32(body[56:60])
	if header.Magic != journalSlotMagic || header.Version != journalSlotVersion {
		return journalSlotHeader{}, false
	}
	if crc32.Checksum(body[0:56], journalCRCTable) != header.Sum {
		return journalSlotHeader{}, false
	}
	return header, true
}

// journalHeaderStates reports that one decoded header states a watermark this build
// reads through: a table shape that addresses records, watermark fields no commit
// reaches backwards to, and a table holding every slot the header names.
func journalHeaderStates(header journalSlotHeader, tableBytes int64) bool {
	if header.Capacity <= 0 || header.Stride <= journalSlotRecordHeaderBytes ||
		header.Seq < 0 || header.ValuesSize < 0 || header.OrderSize < 0 ||
		header.Used < 0 || header.Used > header.Capacity {
		return false
	}
	return tableBytes >= journalSlotHeaderBytes+header.Capacity*header.Stride
}

// encodeSlotHeader answers the bytes one header is written as.
func encodeSlotHeader(header journalSlotHeader) []byte {
	body := make([]byte, journalSlotHeaderBytes)
	binary.LittleEndian.PutUint32(body[0:4], header.Magic)
	binary.LittleEndian.PutUint32(body[4:8], header.Version)
	binary.LittleEndian.PutUint64(body[8:16], uint64(header.Capacity))
	binary.LittleEndian.PutUint64(body[16:24], uint64(header.Stride))
	binary.LittleEndian.PutUint64(body[24:32], uint64(header.Seq))
	binary.LittleEndian.PutUint64(body[32:40], uint64(header.ValuesSize))
	binary.LittleEndian.PutUint64(body[40:48], uint64(header.OrderSize))
	binary.LittleEndian.PutUint64(body[48:56], uint64(header.Used))
	binary.LittleEndian.PutUint32(body[56:60], crc32.Checksum(body[0:56], journalCRCTable))
	return body
}

// writeSlotHeaderAt states one header at the start of a table file.
func writeSlotHeaderAt(file *os.File, header journalSlotHeader) error {
	if _, err := file.WriteAt(encodeSlotHeader(header), 0); err != nil {
		return fmt.Errorf("tagent: state the header of %s: %w", file.Name(), err)
	}
	return nil
}

// fillEmptySlots writes an empty record into every slot of a table file, so a slot
// no commit took reads as empty instead of as a record this build cannot place: a
// zero byte of a sparse file holds no checksum, and a table that cannot tell an
// unwritten slot from a torn one costs a rebuild for a table nobody touched.
func fillEmptySlots(file *os.File, capacity, stride int64) error {
	empty := encodeSlotRecord(stride, journalSlotEmpty, "", journalAddressSpan{})
	batch := make([]byte, stride*min(journalSlotScanBatch, capacity))
	for at := 0; at < len(batch); at += int(stride) {
		copy(batch[at:], empty)
	}
	for base := int64(0); base < capacity; base += journalSlotScanBatch {
		records := min(journalSlotScanBatch, capacity-base)
		if _, err := file.WriteAt(batch[:records*stride], journalSlotHeaderBytes+base*stride); err != nil {
			return fmt.Errorf("tagent: state the empty slots of %s: %w", file.Name(), err)
		}
	}
	return nil
}

// stageProbe answers the slot one key's record belongs at inside a table that is
// being staged, which is the same linear probe the table itself answers.
//
// The staged table holds at most the used slots its source states, so capacity is the
// bound its chains can need: every slot the chain walks but its own holds another key,
// and a table whose records fill it states no empty slot for the next key. A walk that
// exhausts it answers errSlotUnplaceable, so the caller repairs the table from the
// order file instead of staging a table that lost a record.
func stageProbe(file *os.File, capacity, stride int64, key string) (int64, error) {
	base := slotAddressOf(key, capacity)
	record := make([]byte, stride)
	for step := int64(0); step < capacity; step++ {
		index := (base + step) % capacity
		if _, err := file.ReadAt(record, journalSlotHeaderBytes+index*stride); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		state, name, _, ok := decodeSlotRecord(record)
		if !ok {
			return 0, fmt.Errorf("%w: the staged table of %s states no slot record at %d", ErrJournalCorrupt, file.Name(), index)
		}
		if state == journalSlotEmpty || name == key {
			return index, nil
		}
	}
	return 0, fmt.Errorf("%w: the staged table of %s holds no empty slot in its %d slot(s) for %q",
		errSlotUnplaceable, file.Name(), capacity, key)
}

// encodeSlotRecord answers the bytes one slot states: its checksum, the state of the
// key it addresses, the span that key's value sits at, and the key itself. The key
// is stated whole, because a record that named a digest of its key would answer one
// key's value for another.
func encodeSlotRecord(stride int64, state uint8, key string, span journalAddressSpan) []byte {
	record := make([]byte, stride)
	record[4] = state
	binary.LittleEndian.PutUint16(record[5:7], uint16(len(key)))
	binary.LittleEndian.PutUint64(record[7:15], uint64(span.Offset))
	binary.LittleEndian.PutUint32(record[15:19], uint32(span.Length))
	if span.Null {
		record[19] = 1
	}
	copy(record[journalSlotRecordHeaderBytes:], key)
	binary.LittleEndian.PutUint32(record[0:4], crc32.Checksum(record[4:], journalCRCTable))
	return record
}

// decodeSlotRecord answers what one slot states, and false beside a record this
// build cannot place. The checksum is what makes a record whole: a stop inside the
// write of one record leaves bytes that name no span, and the key it held is
// unknowable from the record, so the table that holds it is stated again from the
// order file instead of read through a slot nobody wrote.
func decodeSlotRecord(record []byte) (uint8, string, journalAddressSpan, bool) {
	if int64(len(record)) < journalSlotRecordHeaderBytes {
		return journalSlotEmpty, "", journalAddressSpan{}, false
	}
	if crc32.Checksum(record[4:], journalCRCTable) != binary.LittleEndian.Uint32(record[0:4]) {
		return journalSlotEmpty, "", journalAddressSpan{}, false
	}
	state := record[4]
	if state != journalSlotEmpty && state != journalSlotHeld && state != journalSlotAbsent {
		return journalSlotEmpty, "", journalAddressSpan{}, false
	}
	length := int64(binary.LittleEndian.Uint16(record[5:7]))
	if length > int64(len(record))-journalSlotRecordHeaderBytes {
		return journalSlotEmpty, "", journalAddressSpan{}, false
	}
	span := journalAddressSpan{
		Offset: int64(binary.LittleEndian.Uint64(record[7:15])),
		Length: int(binary.LittleEndian.Uint32(record[15:19])),
		Null:   record[19] == 1,
	}
	return state, string(record[journalSlotRecordHeaderBytes : journalSlotRecordHeaderBytes+length]), span, true
}

// slotAddressOf answers the slot one key addresses, which is a function of the key
// and the capacity alone: the table needs no offset file, and a point read computes
// its own address instead of walking anything.
func slotAddressOf(key string, capacity int64) int64 {
	return int64(slotHashOf(key) % uint64(capacity))
}

// slotHashOf answers the 64-bit address of one key. The mixer is what makes the
// address independent of the key family: the keys this engine writes are families
// that differ in a trailing counter, and a hash whose output for such a family is a
// constant difference between neighbours — a checksum such as CRC32C is one — files
// every commit's keys into one chain and fills the probe bound instead of spreading
// them. The address is computed from the key alone and never from a seed, so a table
// written by one process is the table another process reads.
func slotHashOf(key string) uint64 {
	hash := uint64(0xcbf29ce484222325)
	for at := 0; at < len(key); at++ {
		hash ^= uint64(key[at])
		hash *= 0x100000001b3
	}
	hash ^= hash >> 30
	hash *= 0xbf58476d1ce4e5b9
	hash ^= hash >> 27
	hash *= 0x94d049bb133111eb
	hash ^= hash >> 31
	return hash
}

// eachKeyNewestFirst answers the keys the committed records touched, from the
// newest record to the oldest, and stops when visit answers false. The walk reads the
// order this log's address states, so it needs no sort and no byte of the log: a
// reader that wants the newest record of one kind walks the records filed after it
// instead of the whole log.
func (l *journalLog) eachKeyNewestFirst(visit func(key string) (bool, error)) error {
	return l.addr.eachNewestFirst(func(key string, _ journalAddressSpan) (bool, error) {
		return visit(key)
	})
}

// eachKeyNewestFirstValue answers the keys the committed records touched beside the
// value each key's newest statement leaves under it, from the newest record to the
// oldest, and stops when visit answers false. It reads the order this log's address
// states and the value span of every key it names, so a caller that ranks the
// entries beside their values walks one order chunk and the spans it visits, and a
// caller that stops early reads nothing behind the stop.
func (l *journalLog) eachKeyNewestFirstValue(visit func(key string, value []byte) (bool, error)) error {
	return l.addr.eachNewestFirstValue(visit)
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
		return fmt.Errorf("%w: one journal record carries %d bytes, over the %d-byte frame bound",
			ErrJournalFrameTooLarge, len(payload), journalMaxFrameBytes)
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
	l.size += int64(len(frame))
	l.unsynced += int64(len(frame))
	if l.addr != nil {
		l.addr.note(spans, l.size, int64(len(frame)))
	}
	return nil
}

// sync completes the barrier over everything appended since the last one, so a
// caller reports durable success only behind a barrier that returned. Nothing
// appended means nothing to flush, and the barrier holds by definition. A view that
// keeps an addressable index states the commits that barrier confirmed once it
// returned, so the index a later open reads locates every commit this one reported.
func (l *journalLog) sync() error {
	if l.unsynced != 0 {
		if err := l.file.Sync(); err != nil {
			return fmt.Errorf("%w: %w", ErrJournalBarrier, err)
		}
		l.unsynced = 0
	}
	if l.addr != nil {
		if err := l.addr.confirm(); err != nil {
			return err
		}
	}
	return nil
}

// read answers the value one key's latest commit carries, and whether the key is
// held at all, from this log's address: a tombstoned key is not held, and a value
// the commit wrote as JSON null is held and empty. The read costs the value's own
// byte span, so a small value committed beside a large one pays its own bytes rather
// than the frame that carries both. The caller holds the lock.
func (l *journalLog) read(key string) ([]byte, bool, error) {
	return l.addr.read(key)
}

// lookup answers one key's span and hot-index state without reading its value.
// The caller holds the lock.
func (l *journalLog) lookup(key string) (journalAddressSpan, uint8, error) {
	return l.addr.lookup(key)
}

// keys answers the held keys carrying prefix, in key order, from this log's address:
// no value is read. The caller holds the lock.
func (l *journalLog) keys(prefix string) ([]string, error) {
	return l.addr.keys(prefix)
}

// eachHeld answers the held keys carrying prefix with the span each one states, in
// key order, from this log's address. The caller holds the lock.
func (l *journalLog) eachHeld(prefix string, visit func(key string, span journalAddressSpan) error) error {
	return l.addr.eachHeld(prefix, visit)
}

// namespaces answers the key namespaces the held keys carry, in name order, from
// this log's address: a namespace answers nothing once a commit deleted its last
// held key, because a namespace states no key of its own. The caller holds the lock.
func (l *journalLog) namespaces() ([]string, error) {
	return l.addr.namespaces()
}

// close releases the one writer's handle.
func (l *journalLog) close() error {
	var err error
	if l.file != nil {
		err = l.file.Close()
	}
	if l.addr != nil {
		err = errors.Join(err, l.addr.close())
	}
	return err
}

// IntegrityBytesScanned reports the log bytes this view's open verified: the span
// from the first frame through the last complete one, whose checksums the open's
// integrity pass read and matched. It is read-only and takes no lock, because the
// open sets it before any caller holds the view. It is the whole log beside a log
// of whole frames, so it grows with the history the log holds whatever the
// watermark states, and it is a separate measure from JournalBytesOpen: that one
// states the index replay window past the watermark, this one the integrity pass
// over the log whole, and a boot pays both.
func (k *journalKV) IntegrityBytesScanned() int64 {
	if k == nil || k.log == nil {
		return 0
	}
	return k.log.integrityBytes
}

// SlotTableBytesScanned reports the slot table bytes this view's enumerations read
// since its open: one full-table pass adds the capacity records of stride bytes
// each it walks, so the count follows the table an enumeration reads and never the
// keys it answers. A point read probes one record and adds nothing. The caller
// reads it beside the work it measures, under the same lock a read takes, because
// an enumeration moves it while the view is held.
func (k *journalKV) SlotTableBytesScanned() int64 {
	if k == nil || k.log == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.log.addr == nil {
		return 0
	}
	return k.log.addr.slotScanBytes
}

// ValueReads reports the value spans this view read back since its open: a point
// lookup and a walk alike pay one per value they answer, so the count follows the
// keys a boot reads by address and never the table behind them. The caller reads
// it beside the work it measures, under the same lock a read takes, because a read
// moves it while the view is held.
func (k *journalKV) ValueReads() int64 {
	if k == nil || k.log == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.log.addr == nil {
		return 0
	}
	return k.log.addr.valueReads
}

// IntegrityPassSpan reports the two clock reads this view's open took around the
// verification of the log: the read before the pass started and the read the moment
// it answered. Both stay zero beside a view that ran no pass, which is a pass that
// did not happen, so a caller that names the verification stage of a boot reads its
// endpoints here instead of deriving them from another stage. It is read-only and
// takes no lock, because the open sets it before any caller holds the view.
func (k *journalKV) IntegrityPassSpan() (time.Time, time.Time) {
	if k == nil || k.log == nil {
		return time.Time{}, time.Time{}
	}
	return k.log.integrityBegin, k.log.integrityEnd
}

// DirectoryMaintenancePasses reports how many passes of the address table this view
// opened since its open: a pass that grew the table, one that rebuilt it and one that
// reset it are each one. The count is written where a pass opens, so a caller states
// that a pass happened from this count and reads the span it took from
// DirectoryMaintenancePass. The caller reads it under the same lock a commit takes,
// because a pass moves it while the view is held.
func (k *journalKV) DirectoryMaintenancePasses() int {
	if k == nil || k.log == nil {
		return 0
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.log.addr == nil {
		return 0
	}
	return k.log.addr.passes
}

// DirectoryMaintenancePass reports one pass of the address table this view ran: the
// pass, the account the call that ran it stated for itself, and the two clock reads
// that call took around it. A pass of one of these is one entry, oldest first, in the
// order the view ran them. The caller reads it under the same lock a commit takes.
func (k *journalKV) DirectoryMaintenancePass(at int) (pass, parent string, begin, end time.Time, held bool) {
	if k == nil || k.log == nil {
		return "", "", time.Time{}, time.Time{}, false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.log.addr == nil || at < 0 || at >= len(k.log.addr.passRecords) {
		return "", "", time.Time{}, time.Time{}, false
	}
	record := k.log.addr.passRecords[at]
	return record.pass, record.parent, record.begin, record.end, true
}

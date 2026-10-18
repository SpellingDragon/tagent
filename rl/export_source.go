package rl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/SpellingDragon/tagent/event"
)

// ErrExportCaptureUnsealed, ErrExportCaptureMismatch, ErrExportCaptureUnreadable
// and ErrExportPageOffset name the ways the capture-backed export source
// refused a read, so a caller routes each by name.
var (
	// ErrExportCaptureUnsealed reports a source over a capture no writer
	// confirmed: without a seal there are no accounted bytes to hold an export
	// to, and the unsealed partial state is never read as a capture.
	ErrExportCaptureUnsealed = errors.New("rl: the capture holds no writer-confirmed seal to export from")
	// ErrExportCaptureMismatch reports capture bytes that do not recheck
	// against the seal that accounts them: bytes the seal never accounted and
	// accounted bytes no file holds are both this refusal.
	ErrExportCaptureMismatch = errors.New("rl: the capture bytes do not recheck against the seal that accounts them")
	// ErrExportCaptureUnreadable reports a stored record line the export source
	// cannot read back. The read stops at that line and reports it, so a line
	// outside the record schema is never guessed at nor silently dropped.
	ErrExportCaptureUnreadable = errors.New("rl: the capture record file holds a line the export source cannot read")
	// ErrExportPageOffset reports a page offset outside the partition: an
	// offset is refused by name rather than read from an invented position.
	ErrExportPageOffset = errors.New("rl: the export page offset is outside the partition")
)

// CaptureExportSource is the export source over one sealed capture directory:
// the record file the writer confirmed, read under the partition named by the
// capture identity. The source hands over exactly what the writer sealed and
// nothing beside it: the record file's bytes are rechecked against the seal
// that accounts them, and one stored line the read cannot classify stops the
// read by name rather than being skipped. The capture is one partition — the
// grant names the capture and every fact is rechecked under that name — and a
// partition the capture does not hold carries no facts rather than a widened
// read.
//
// Episode records are handed over as the episode account the split groups are
// drawn on, and every decision and attempt record becomes a fact whose body is
// the stored line itself byte for byte, so no stored line falls outside those
// two accounts and an exported sample traces back to the record file without a
// projection in between.
//
// 契约: docs/wiki/rl/rl-architecture.md#training-export
type CaptureExportSource struct {
	dir      string
	manifest CaptureManifest

	mu       sync.Mutex
	loaded   bool
	loadErr  error
	facts    []ExportedFact
	episodes []EpisodeRecord
}

// NewCaptureExportSource returns the export source over one capture directory.
// A directory whose writer never confirmed a seal is refused with
// ErrExportCaptureUnsealed: bytes beside an unsealed capture are never read as
// an export source.
func NewCaptureExportSource(dir string) (*CaptureExportSource, error) {
	manifest, err := ReadCaptureSeal(dir)
	if err != nil {
		return nil, err
	}
	if !manifest.Sealed || manifest.CaptureID == "" {
		return nil, ErrExportCaptureUnsealed
	}
	return &CaptureExportSource{dir: dir, manifest: manifest}, nil
}

var _ ExportSource = (*CaptureExportSource)(nil)

// CaptureID reports the capture identity this source serves as its partition:
// the name a grant authorizes and every fact is rechecked against.
func (s *CaptureExportSource) CaptureID() string {
	return s.manifest.CaptureID
}

// Manifest reports the seal the source reads under, so a caller reconciles the
// export against the capture account that backs it.
func (s *CaptureExportSource) Manifest() CaptureManifest {
	return s.manifest
}

// Page returns at most limit facts of one partition starting at the given
// offset, in stored order. A partition the capture does not hold carries no
// facts, and a page past the end is empty: the source never widens a named
// partition into a read of anything else.
func (s *CaptureExportSource) Page(ctx context.Context, partition string, offset, limit int) ([]ExportedFact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, fmt.Errorf("%w: offset %d", ErrExportPageOffset, offset)
	}
	if limit < 1 || limit > MaxExportPageLimit {
		return nil, ErrExportPageLimit
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	if partition != s.manifest.CaptureID {
		return nil, nil
	}
	if offset >= len(s.facts) {
		return nil, nil
	}
	end := offset + limit
	if end > len(s.facts) {
		end = len(s.facts)
	}
	return append([]ExportedFact(nil), s.facts[offset:end]...), nil
}

// Episodes returns the episode records the capture holds: the account the
// leakage-free split groups are drawn on.
func (s *CaptureExportSource) Episodes(ctx context.Context) ([]EpisodeRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return append([]EpisodeRecord(nil), s.episodes...), nil
}

// load reads the sealed record file once and holds what it read, so one export
// run and a later one face the same bytes and the same refusal.
func (s *CaptureExportSource) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return s.loadErr
	}
	s.loaded = true
	s.loadErr = s.readSealedRecords()
	return s.loadErr
}

// readSealedRecords reads the record file under the seal that accounts it. The
// bytes recheck against the seal digest before any line is parsed; bytes the
// seal never accounted, accounted bytes no file holds and a digest that differs
// are all ErrExportCaptureMismatch, and one line the parse refuses is
// ErrExportCaptureUnreadable with the read stopped at it.
func (s *CaptureExportSource) readSealedRecords() error {
	var accounted *CaptureFileDigest
	for i, file := range s.manifest.Files {
		if file.Name == SinkRecordsFile {
			accounted = &s.manifest.Files[i]
		}
	}
	content, err := os.ReadFile(filepath.Join(s.dir, SinkRecordsFile))
	if errors.Is(err, os.ErrNotExist) {
		if accounted != nil {
			return fmt.Errorf("%w: the seal accounts %s of %d bytes and no file holds them", ErrExportCaptureMismatch, SinkRecordsFile, accounted.Bytes)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if accounted == nil {
		return fmt.Errorf("%w: %s holds %d bytes the seal never accounted", ErrExportCaptureMismatch, SinkRecordsFile, len(content))
	}
	sum := sha256.Sum256(content)
	if rechecked := hex.EncodeToString(sum[:]); rechecked != accounted.SHA256 {
		return fmt.Errorf("%w: %s rechecks to %s and the seal accounts %s", ErrExportCaptureMismatch, SinkRecordsFile, rechecked, accounted.SHA256)
	}
	stored := bytes.Split(content, []byte("\n"))
	for i, raw := range stored {
		if len(raw) == 0 {
			if i > 0 && i == len(stored)-1 {
				continue
			}
			return fmt.Errorf("%w: line %d of %s is empty", ErrExportCaptureUnreadable, i+1, SinkRecordsFile)
		}
		line, err := ParseLine(raw)
		if err != nil {
			return fmt.Errorf("%w: line %d of %s: %w", ErrExportCaptureUnreadable, i+1, SinkRecordsFile, err)
		}
		switch line.Kind {
		case RecordKindEpisode:
			s.episodes = append(s.episodes, *line.Episode)
		case RecordKindDecision:
			s.facts = append(s.facts, decisionFactOf(*line.Decision, line.Schema, s.manifest.CaptureID, raw))
		case RecordKindAttempt:
			s.facts = append(s.facts, attemptFactOf(*line.Attempt, line.Schema, s.manifest.CaptureID, raw))
		}
	}
	return nil
}

// decisionFactOf maps one stored decision record to the fact it stands for: the
// canonical key is the decision identity the record carries and the body is the
// stored line byte for byte. The fields no record carries — call identity and
// parent key — stay absent rather than invented.
func decisionFactOf(record DecisionRecord, schema SchemaVersion, capture string, raw []byte) ExportedFact {
	return ExportedFact{
		EventKey:   event.EventID(record.ID),
		Partition:  capture,
		CaptureID:  capture,
		EpisodeID:  record.EpisodeID,
		DecisionID: record.ID,
		Schema:     schema,
		Timestamp:  record.Timestamp.UnixMilli(),
		Body:       json.RawMessage(raw),
	}
}

// attemptFactOf maps one stored attempt record to the fact it stands for, under
// the same identity and byte rules as decisionFactOf, carrying the attempt
// identity the record observed.
func attemptFactOf(record AttemptRecord, schema SchemaVersion, capture string, raw []byte) ExportedFact {
	return ExportedFact{
		EventKey:   event.EventID(record.ID),
		Partition:  capture,
		CaptureID:  capture,
		EpisodeID:  record.EpisodeID,
		DecisionID: record.DecisionID,
		AttemptID:  record.ID,
		Schema:     schema,
		Timestamp:  record.Timestamp.UnixMilli(),
		Body:       json.RawMessage(raw),
	}
}

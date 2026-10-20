package meditation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// Watermark is the execution watermark of the reflection loop: the moment the
// last reflection turn advanced it to, which heads the freshness window and
// bounds how soon the next reflection may run.
//
// Injection alone never moves the watermark; only a reflection turn that is
// actually consumed advances it, to the moment the turn was injected at. A
// reflection that yields leaves the watermark in place, so the facts that
// reflection covered stay no older than the watermark instead of being burned
// out of the window, and a start with no watermark passes once and then locks
// itself. The watermark is advanced by consumption only, and the freshness
// policy excludes the agent's own reflection traffic, so a notification the
// agent sends about its own work never makes the agent reflect on itself again.
type Watermark struct {
	// Moment is the moment the last consumed reflection turn advanced the
	// watermark to. Its zero value marks a start with no prior execution.
	Moment time.Time
}

// WatermarkGate is the reflection loop's freshness gate over the execution
// watermark.
//
// The gate reads the watermark and advances it after a reflection turn is
// consumed. A watermark that is missing, or that cannot be read or parsed,
// keeps the gate closed and is reported by name; the gate is never opened on a
// fresh default or on a fallback criterion when the watermark cannot be
// established.
// The redline list (3 rules):
// MUST report a watermark it cannot establish as an error and keep the gate closed.
// MUST NOT open the gate on a fresh default or on a fallback criterion.
// MUST NOT advance the watermark past a reflection turn that was deferred.
type WatermarkGate interface {
	// Current returns the current execution watermark. A watermark that cannot
	// be established is reported as an error, and the gate stays closed.
	Current(ctx context.Context) (Watermark, error)

	// Advance moves the watermark to the moment a consumed reflection turn was
	// injected at, and leaves it unmoved when that turn was deferred.
	Advance(ctx context.Context, moment time.Time) error
}

// ErrAnchorStoreUnusable reports an anchor store built without a path: a
// watermark that has nowhere to be recorded cannot carry across a restart.
var ErrAnchorStoreUnusable = errors.New("evolution: the anchor store requires a path")

// ErrAnchorUnreadable reports an anchor record that exists but cannot be read
// or parsed. The gate reads it as an unestablished watermark and stays closed;
// no default and no fallback criterion is substituted for it.
var ErrAnchorUnreadable = errors.New("evolution: the anchor record cannot be read")

// Anchors is the persisted pair of loop anchors: the moment the last reflection
// turn was consumed and the moment the last turn of the session ended.
//
// The reflection watermark heads the freshness window and the rhythm floor, so
// it is the anchor the gate reads and the anchor a consumed reflection advances.
// The session anchor is carried beside it in the same record, and an entry this
// structure does not name is ignored when the record is read rather than
// refused.
type Anchors struct {
	// LastTurnEnd is the moment the last turn of the session ended.
	LastTurnEnd time.Time `json:"last_turn_end"`
	// LastMeditation is the moment of the last consumed reflection turn.
	LastMeditation time.Time `json:"last_meditation"`
}

// AnchorStore is the anchor record of one observing agent: the pair of loop
// anchors kept in one file and rewritten atomically, so a reader never observes
// a half-written record.
//
// The store is the gate's only watermark source. A record that is absent reads
// as a start with no prior execution, while a record that exists but cannot be
// read or parsed is refused by name: an unreadable watermark is never reported
// as a fresh one.
type AnchorStore struct {
	path string
	mu   sync.Mutex
}

// NewAnchorStore returns the anchor store of the record at path, creating its
// directory when the directory is absent. A store built without a path is
// refused with ErrAnchorStoreUnusable.
func NewAnchorStore(path string) (*AnchorStore, error) {
	if path == "" {
		return nil, ErrAnchorStoreUnusable
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("evolution: create the anchor directory: %w", err)
		}
	}
	return &AnchorStore{path: path}, nil
}

// Path reports the record path this store reads and writes.
func (s *AnchorStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load reads the anchor record. An absent record is a start with no prior
// execution and carries no error; a record that cannot be read or parsed is
// refused with ErrAnchorUnreadable.
func (s *AnchorStore) Load() (Anchors, error) {
	if s == nil {
		return Anchors{}, ErrAnchorStoreUnusable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// Save rewrites the anchor record atomically: the new record is written beside
// it and renamed over it, so no reader observes a partial record.
func (s *AnchorStore) Save(record Anchors) error {
	if s == nil {
		return ErrAnchorStoreUnusable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(record)
}

// Current reports the execution watermark the anchor record holds. An absent
// record reports the zero watermark of a start with no prior execution, and an
// unreadable record is refused with ErrAnchorUnreadable so the gate stays closed.
func (s *AnchorStore) Current(ctx context.Context) (Watermark, error) {
	if err := ctx.Err(); err != nil {
		return Watermark{}, err
	}
	record, err := s.Load()
	if err != nil {
		return Watermark{}, err
	}
	return Watermark{Moment: record.LastMeditation}, nil
}

// Advance moves the execution watermark to the moment a consumed reflection turn
// was injected at and keeps the session anchor as it stands. A moment that is not
// after the recorded watermark leaves the record as it is, so the watermark never
// moves backwards.
func (s *AnchorStore) Advance(ctx context.Context, moment time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.loadLocked()
	if err != nil {
		return err
	}
	if !moment.After(record.LastMeditation) {
		return nil
	}
	record.LastMeditation = moment
	return s.saveLocked(record)
}

// loadLocked reads the anchor record while the caller holds the store lock.
func (s *AnchorStore) loadLocked() (Anchors, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return Anchors{}, nil
		}
		return Anchors{}, fmt.Errorf("%w: %s: %v", ErrAnchorUnreadable, s.path, err)
	}
	var record Anchors
	if err := json.Unmarshal(raw, &record); err != nil {
		return Anchors{}, fmt.Errorf("%w: %s: %v", ErrAnchorUnreadable, s.path, err)
	}
	return record, nil
}

// saveLocked rewrites the anchor record while the caller holds the store lock.
func (s *AnchorStore) saveLocked(record Anchors) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("evolution: encode the anchor record: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("evolution: write the anchor record: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("evolution: replace the anchor record: %w", err)
	}
	return nil
}

var _ WatermarkGate = (*AnchorStore)(nil)

// The declared field, methods and interface kind must keep exactly these
// shapes: a renamed field or a changed method signature stops this block from
// compiling.
var (
	_ = Watermark{Moment: time.Time{}}

	_ func(WatermarkGate, context.Context) (Watermark, error) = WatermarkGate.Current

	_ func(WatermarkGate, context.Context, time.Time) error = WatermarkGate.Advance

	_ WatermarkGate = (WatermarkGate)(nil)
)

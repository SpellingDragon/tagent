package rl

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// SinkRecordsFile is the file name the sink writes its record lines under.
//
// - The bounded admission queue of one capture: a record is admitted under three bounds (per-record size, in-flight count, byte budget), and a record a bound refuses is counted in the loss ledger under that bound instead of being dropped silently.
// - Admission never waits on the writer, and the ledger is read from counters rather than from the data queue, so a full queue never hides its own loss account.
// - Sending and closing share one protocol: the seal barrier enters the same queue every record line enters, so a seal accounts exactly what was admitted before it.
// - The terminal of an attempt record is the Kind the model reduction derived, shared through the single observation face and never re-derived here; a capture that ends without a writer seal is accounted partial with its reason.
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
const SinkRecordsFile = "attempts.jsonl"

// DefaultMaxRecordBytes and the other default admission bounds of one capture sink: a config that names no
// bound takes these, and a negative bound is refused rather than read as unlimited.
const (
	// DefaultMaxRecordBytes is the per-record byte bound one sink takes when its
	// config names none: a marshaled record line above it is refused as a
	// counted loss.
	DefaultMaxRecordBytes int64 = 1 << 20
	// DefaultMaxInFlight is the in-flight count bound one sink takes when its
	// config names none: admitted records not yet accounted by the writer stay
	// below it, so the queue never grows past it.
	DefaultMaxInFlight int64 = 4096
	// DefaultMaxBytes is the byte budget one sink takes when its config names
	// none: admitted line bytes at or above it refuse every further record, and
	// the budget is never met by removing or rotating stored history.
	DefaultMaxBytes int64 = 512 << 20
)

// ErrNegativeBound and the other named errors of the bounded capture queue: each names the way a sink was
// refused at construction, so a caller routes it by name.
var (
	// ErrNegativeBound reports a negative bound offered to a sink or a writer: a
	// negative bound is refused rather than read as unlimited.
	ErrNegativeBound = errors.New("rl: the capture bound is negative")
	// ErrCaptureIDMissing reports a sink built without a capture id: the
	// manifest it seals accounts the capture it belongs to.
	ErrCaptureIDMissing = errors.New("rl: the capture sink was built without a capture id")
	// ErrWriterMissing reports a sink built without a writer: the queue drains
	// into one durable write point and never accumulates without one.
	ErrWriterMissing = errors.New("rl: the capture sink was built without a writer")
)

// ErrCaptureRefused reports a record the capture refused at admission on the
// recorder face: the refusal is counted in the loss ledger under the bound that
// refused it and reported by name here, so a write the capture could not take
// reaches the caller's loss account instead of silence.
var ErrCaptureRefused = errors.New("rl: the capture refused the record at admission")

// SinkConfig is the admission bounds of one capture sink. A zero bound takes
// its default, a negative bound is refused at construction, and each bound
// refuses records rather than removing stored history to make room.
type SinkConfig struct {
	// MaxRecordBytes bounds the marshaled line of one record.
	MaxRecordBytes int64
	// MaxInFlight bounds the records admitted and not yet accounted by the
	// writer; it is also the bound on the queue backlog.
	MaxInFlight int64
	// MaxBytes bounds the total marshaled line bytes the capture admits.
	MaxBytes int64
}

// CaptureLedger is the loss account of one capture sink: what it admitted and
// wrote, what each admission bound refused and what failed to reach storage.
// The account is a counter snapshot and never a read of the data queue, so it
// stays readable while the queue is full.
type CaptureLedger struct {
	// Admitted counts the records the sink took into its queue.
	Admitted int64
	// DroppedRecordSize counts the records the per-record byte bound refused.
	DroppedRecordSize int64
	// DroppedInFlight counts the records the in-flight count bound refused.
	DroppedInFlight int64
	// DroppedBytes counts the records the byte budget refused.
	DroppedBytes int64
	// DroppedClosed counts the records refused because the sink was closed.
	DroppedClosed int64
	// WriteErrors counts the admitted records that failed to reach storage.
	WriteErrors int64
	// Written counts the admitted records that reached storage.
	Written int64
	// InFlight counts the admitted records the writer has not accounted yet.
	InFlight int64
}

// Dropped reports every record the capture refused under an admission bound.
func (l CaptureLedger) Dropped() int64 {
	return l.DroppedRecordSize + l.DroppedInFlight + l.DroppedBytes + l.DroppedClosed
}

// captureCounters is the counter set the ledger account is read from; every
// counter is guarded by the sink's lock so one snapshot stays coherent.
type captureCounters struct {
	admitted          int64
	droppedRecordSize int64
	droppedInFlight   int64
	droppedBytes      int64
	droppedClosed     int64
	writeErrors       int64
	written           int64
	inFlight          int64
}

// ledger reports one coherent snapshot of the counters.
func (c *captureCounters) ledger() CaptureLedger {
	return CaptureLedger{
		Admitted:          c.admitted,
		DroppedRecordSize: c.droppedRecordSize,
		DroppedInFlight:   c.droppedInFlight,
		DroppedBytes:      c.droppedBytes,
		DroppedClosed:     c.droppedClosed,
		WriteErrors:       c.writeErrors,
		Written:           c.written,
		InFlight:          c.inFlight,
	}
}

// sinkEntry is one queued unit of the sink's single protocol: a record line on
// its way to the writer, or the seal barrier that closes the queue's account.
type sinkEntry struct {
	name    string
	line    []byte
	barrier bool
}

// sinkState names the admission state of one capture sink: open admits records,
// sealing admits none while the drain runs, and sealed holds the frozen
// manifest.
type sinkState int

const (
	sinkOpen sinkState = iota
	sinkSealing
	sinkSealed
)

// CaptureSink is the bounded admission queue of one capture: records enter
// under the configured bounds, the single writer drains them, and the seal is
// what that writer confirmed. Sending and closing run through one queue and one
// lock, so a concurrent seal is serialized against admission instead of racing
// it.
type CaptureSink struct {
	id       string
	cfg      SinkConfig
	writer   CaptureWriter
	counters captureCounters

	mu         sync.Mutex
	cond       *sync.Cond
	queue      []sinkEntry
	state      sinkState
	budgetUsed int64
	sealed     CaptureManifest
	sealDone   chan struct{}
}

// NewCaptureSink returns the bounded admission queue of one capture, draining
// into writer. A missing capture id or writer is refused, and a negative bound
// is refused rather than read as unlimited: a zero bound takes its default.
func NewCaptureSink(id string, cfg SinkConfig, writer CaptureWriter) (*CaptureSink, error) {
	if id == "" {
		return nil, ErrCaptureIDMissing
	}
	if writer == nil {
		return nil, ErrWriterMissing
	}
	if cfg.MaxRecordBytes < 0 || cfg.MaxInFlight < 0 || cfg.MaxBytes < 0 {
		return nil, ErrNegativeBound
	}
	if cfg.MaxRecordBytes == 0 {
		cfg.MaxRecordBytes = DefaultMaxRecordBytes
	}
	if cfg.MaxInFlight == 0 {
		cfg.MaxInFlight = DefaultMaxInFlight
	}
	if cfg.MaxBytes == 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	s := &CaptureSink{
		id:       id,
		cfg:      cfg,
		writer:   writer,
		sealDone: make(chan struct{}),
	}
	s.cond = sync.NewCond(&s.mu)
	go s.run()
	return s, nil
}

// run drains the queue into the single writer: each record line is handed over
// and accounted, and the seal barrier seals the capture after every entry
// admitted beside it.
func (s *CaptureSink) run() {
	for {
		s.mu.Lock()
		for len(s.queue) == 0 {
			s.cond.Wait()
		}
		entry := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		if entry.barrier {
			s.finish()
			return
		}
		err := s.writer.Append(entry.name, entry.line)
		s.mu.Lock()
		if err != nil {
			s.counters.writeErrors++
		} else {
			s.counters.written++
		}
		s.counters.inFlight--
		s.mu.Unlock()
	}
}

// Record admits one observed attempt record under the sink's bounds and reports
// the bound that refused it when it is not admitted. Admission never waits on
// the writer: a record the bounds refuse is a counted loss with its bound named,
// a record the queue holds no room for is refused under the in-flight bound,
// and a closed capture refuses every record under BoundClosed. The record line
// is marshaled through the versioned line, which refuses a record shape that
// cannot be read back.
func (s *CaptureSink) Record(ctx context.Context, attempt AttemptRecord) (CaptureAdmission, error) {
	if err := ctx.Err(); err != nil {
		return CaptureAdmission{}, err
	}
	line, err := MarshalLine(NewAttemptLine(attempt))
	if err != nil {
		return CaptureAdmission{}, err
	}
	return s.admitLine(line)
}

// RecordOutcome admits the attempt record of one model outcome through the
// single observation face AttemptOf: the terminal it records is the Kind the
// model reduction derived and none is re-derived here, so the capture and the
// model share exactly one terminal account.
func (s *CaptureSink) RecordOutcome(ctx context.Context, attemptID event.AttemptID, decisionID event.DecisionID, episodeID event.EpisodeID, outcome model.Outcome) (CaptureAdmission, error) {
	return s.Record(ctx, AttemptOf(attemptID, decisionID, episodeID, outcome))
}

// RecordEpisode admits the episode record of one business process through the
// single capture entry every record kind enters the queue through. A record the
// entry cannot take is reported as an error naming the bound that refused it
// rather than dropped silently, and the refusal is counted in the loss ledger
// under that bound either way.
func (s *CaptureSink) RecordEpisode(ctx context.Context, record EpisodeRecord) error {
	return s.recordLine(ctx, RecordKindEpisode, NewEpisodeLine(record))
}

// RecordDecision admits the decision record of one model decision through the
// single capture entry, refused and accounted exactly as RecordEpisode is.
func (s *CaptureSink) RecordDecision(ctx context.Context, record DecisionRecord) error {
	return s.recordLine(ctx, RecordKindDecision, NewDecisionLine(record))
}

// RecordAttempt admits the attempt record of one observable model attempt
// through the single capture entry, refused and accounted exactly as
// RecordEpisode is. The terminal it admits is the one the record carries and
// none is re-derived here.
func (s *CaptureSink) RecordAttempt(ctx context.Context, record AttemptRecord) error {
	return s.recordLine(ctx, RecordKindAttempt, NewAttemptLine(record))
}

// recordLine is the unified capture entry of the recorder face: one versioned
// record line is marshaled and admitted through the same queue and the same
// bounds every other entry stands in, and a marshal failure or an admission
// refusal is reported as a named error so the write it stopped reaches the
// caller's loss account instead of silence.
func (s *CaptureSink) recordLine(ctx context.Context, kind RecordKind, line Line) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	content, err := MarshalLine(line)
	if err != nil {
		return err
	}
	admission, err := s.admitLine(content)
	if err != nil {
		return err
	}
	if !admission.Admitted {
		return fmt.Errorf("%w: the %s record under the %s bound: %s", ErrCaptureRefused, kind, admission.Bound, admission.Reason)
	}
	return nil
}

// admitLine admits one marshaled record line under the sink's three bounds and
// reports the bound that refused it when it is not admitted. Every record kind
// reaches the queue through this entry alone, so the per-record, in-flight and
// byte bounds judge an episode, a decision and an attempt line alike, and a
// refusal is counted in the loss ledger under the bound that refused it.
func (s *CaptureSink) admitLine(line []byte) (CaptureAdmission, error) {
	size := int64(len(line))
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.state != sinkOpen:
		s.counters.droppedClosed++
		return refused(BoundClosed, "the capture is closed"), nil
	case size > s.cfg.MaxRecordBytes:
		s.counters.droppedRecordSize++
		return refused(BoundRecordSize, fmt.Sprintf("the record line is %d bytes and the per-record bound is %d bytes", size, s.cfg.MaxRecordBytes)), nil
	case s.counters.inFlight >= s.cfg.MaxInFlight:
		s.counters.droppedInFlight++
		return refused(BoundInFlight, fmt.Sprintf("%d records are in flight and the in-flight bound is %d", s.counters.inFlight, s.cfg.MaxInFlight)), nil
	case s.budgetUsed+size > s.cfg.MaxBytes:
		s.counters.droppedBytes++
		return refused(BoundBytes, fmt.Sprintf("the capture admitted %d bytes, the record line is %d bytes and the byte budget is %d bytes", s.budgetUsed, size, s.cfg.MaxBytes)), nil
	}
	s.budgetUsed += size
	s.counters.admitted++
	s.counters.inFlight++
	s.queue = append(s.queue, sinkEntry{name: SinkRecordsFile, line: line})
	s.cond.Signal()
	return CaptureAdmission{Admitted: true, Bound: BoundNone}, nil
}

// Ledger reports the loss account of the capture from counters, readable while
// the queue is full and never drained through it.
func (s *CaptureSink) Ledger() CaptureLedger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counters.ledger()
}

// Seal closes the capture and returns the manifest a real writer confirmed. The
// seal barrier enters the same queue every record line enters, so the seal
// accounts exactly what was admitted before it and a concurrent send is judged
// against the same bounds as the close. A second seal returns the first
// manifest and never seals twice. A seal the writer did not confirm within ctx
// is reported as the explicit partial state of an abandoned drain, carrying its
// in-flight and loss counts, and never as complete.
func (s *CaptureSink) Seal(ctx context.Context) (CaptureManifest, error) {
	s.mu.Lock()
	switch s.state {
	case sinkSealed:
		manifest := s.sealed
		s.mu.Unlock()
		return manifest, nil
	case sinkSealing:
		done := s.sealDone
		s.mu.Unlock()
		return s.await(ctx, done)
	}
	s.state = sinkSealing
	s.queue = append(s.queue, sinkEntry{barrier: true})
	s.cond.Signal()
	done := s.sealDone
	s.mu.Unlock()
	return s.await(ctx, done)
}

// await reports the frozen manifest once the writer confirms the seal within
// ctx, and the partial account of an abandoned drain when it does not.
func (s *CaptureSink) await(ctx context.Context, done <-chan struct{}) (CaptureManifest, error) {
	select {
	case <-done:
		s.mu.Lock()
		manifest := s.sealed
		s.mu.Unlock()
		return manifest, nil
	case <-ctx.Done():
		return s.abandoned(ctx.Err()), nil
	}
}

// finish seals the capture at the single writer and freezes the manifest: the
// seal evidence is the writer's own file digests and sync result, and the
// manifest is persisted through the writer commit so a restarted reader reads
// this account back instead of guessing one. A seal without a completed sync or
// a persisted manifest is partial with its reason, never complete.
func (s *CaptureSink) finish() {
	files, sync := s.writer.Seal()
	s.mu.Lock()
	ledger := s.counters.ledger()
	reasons := lossReasons(ledger)
	if !sync.Synced {
		reasons = append(reasons, fmt.Sprintf("no writer seal: the writer reported no sync: %s", sync.Reason))
	}
	manifest := CaptureManifest{
		CaptureID: s.id,
		Sealed:    sync.Synced,
		Partial:   len(reasons) > 0,
		Records:   ledger.Written,
		Dropped:   ledger.Dropped(),
		InFlight:  ledger.InFlight,
		Files:     files,
		Sync:      sync,
		Reasons:   reasons,
	}
	if err := s.writer.Commit(manifest); err != nil {
		manifest.Sealed = false
		manifest.Partial = true
		manifest.Reasons = append(manifest.Reasons, fmt.Sprintf("no writer seal: the seal could not be persisted: %v", err))
	}
	s.sealed = manifest
	s.state = sinkSealed
	s.mu.Unlock()
	close(s.sealDone)
}

// abandoned builds the partial account of a seal the writer did not confirm in
// time: there is no writer evidence to report, and the in-flight and loss
// counts stand as they were at the abandonment rather than as zero.
func (s *CaptureSink) abandoned(err error) CaptureManifest {
	s.mu.Lock()
	defer s.mu.Unlock()
	ledger := s.counters.ledger()
	reasons := append(lossReasons(ledger), fmt.Sprintf("no writer seal: the drain was abandoned before the writer confirmed the seal: %v", err))
	return CaptureManifest{
		CaptureID: s.id,
		Sealed:    false,
		Partial:   true,
		Records:   ledger.Written,
		Dropped:   ledger.Dropped(),
		InFlight:  ledger.InFlight,
		Reasons:   reasons,
	}
}

// refused builds the admission report of a record one bound refused.
func refused(bound CaptureBound, reason string) CaptureAdmission {
	return CaptureAdmission{Admitted: false, Bound: bound, Reason: reason}
}

// lossReasons names every loss the ledger holds as a reason the capture is not
// an intact account: records in flight at the close, records refused under an
// admission bound and records that failed to reach storage each stay named
// rather than folded into silence.
func lossReasons(ledger CaptureLedger) []string {
	var reasons []string
	if ledger.InFlight > 0 {
		reasons = append(reasons, fmt.Sprintf("%d records were still in flight when the capture closed", ledger.InFlight))
	}
	if dropped := ledger.Dropped(); dropped > 0 {
		reasons = append(reasons, fmt.Sprintf("%d records were refused under an admission bound (%d record size, %d in flight, %d bytes, %d closed)", dropped, ledger.DroppedRecordSize, ledger.DroppedInFlight, ledger.DroppedBytes, ledger.DroppedClosed))
	}
	if ledger.WriteErrors > 0 {
		reasons = append(reasons, fmt.Sprintf("%d admitted records failed to reach storage", ledger.WriteErrors))
	}
	return reasons
}

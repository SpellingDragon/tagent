package telemetryexample

import (
	"context"
	"errors"
	"sync"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/telemetry"
)

// errAuditNotLanded is what the canonical audit sink reports for a record it could not
// land.
var errAuditNotLanded = errors.New("audit: record did not land")

// ledgerSink is the canonical audit sink: it keeps one landed record per observed fact,
// is safe for concurrent use, and reports a record it could not land as an error without
// touching the fact the record describes.
type ledgerSink struct {
	mu     sync.Mutex
	landed []telemetry.AuditRecord
	off    bool
}

// Audit keeps record as landed, or reports it as a missing observation when the sink is
// switched off.
func (s *ledgerSink) Audit(_ context.Context, record telemetry.AuditRecord) error {
	if s.off {
		return errAuditNotLanded
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.landed = append(s.landed, record)
	return nil
}

var _ telemetry.AuditSink = (*ledgerSink)(nil)

// ExampleAuditSink hands one layered record to the audit side: the record lands and the
// fact keeps its identity, while a record that does not land comes back as an error
// instead of being dropped quietly.
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#telemetry-ladder
func ExampleAuditSink() {
	sink := &ledgerSink{}
	record := telemetry.AuditRecord{
		Fact: event.EventID("fact-1"),
		Attempts: []telemetry.Attempt{
			{Layer: telemetry.LayerTrace},
			{Layer: telemetry.LayerMetric, Skipped: true},
		},
	}
	if err := sink.Audit(context.Background(), record); err != nil {
		return
	}
	sink.off = true
	if err := sink.Audit(context.Background(), record); err == nil {
		return
	}
	_ = sink.landed[0].Fact
}

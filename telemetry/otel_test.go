package telemetry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// invariantSpan records the attributes and the end of one span.
type invariantSpan struct {
	trace.Span
	name    string
	attrs   []attribute.KeyValue
	ended   bool
	endedAt int
}

// SetAttributes records the attributes the trace layer rendered.
func (s *invariantSpan) SetAttributes(kv ...attribute.KeyValue) { s.attrs = append(s.attrs, kv...) }

// End records that the span was closed.
func (s *invariantSpan) End(...trace.SpanEndOption) { s.ended = true }

// invariantTracer records every span the trace layer started.
type invariantTracer struct {
	trace.Tracer
	mu    sync.Mutex
	spans []*invariantSpan
}

// Start records one span and answers it as the started span.
func (t *invariantTracer) Start(ctx context.Context, name string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	t.mu.Lock()
	defer t.mu.Unlock()
	span := &invariantSpan{name: name, endedAt: len(t.spans)}
	t.spans = append(t.spans, span)
	return ctx, span
}

// count reports how many spans were started.
func (t *invariantTracer) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.spans)
}

// invariantAdd is one counter addition the metric layer made.
type invariantAdd struct {
	value int64
	attrs []attribute.KeyValue
}

// invariantCounter records every addition beside the attributes it carried.
type invariantCounter struct {
	metric.Int64Counter
	meter *invariantMeter
}

// Add records one addition.
func (c *invariantCounter) Add(_ context.Context, inc int64, opts ...metric.AddOption) {
	cfg := metric.NewAddConfig(opts)
	set := cfg.Attributes()
	c.meter.mu.Lock()
	defer c.meter.mu.Unlock()
	c.meter.adds = append(c.meter.adds, invariantAdd{value: inc, attrs: set.ToSlice()})
}

// invariantMeter records the counters it created and the additions made to them.
type invariantMeter struct {
	metric.Meter
	mu           sync.Mutex
	names        []string
	descriptions []string
	units        []string
	adds         []invariantAdd
	counterErr   error
}

// Int64Counter records the counter construction and answers one recording counter.
func (m *invariantMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	cfg := metric.NewInt64CounterConfig(opts...)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names = append(m.names, name)
	m.descriptions = append(m.descriptions, cfg.Description())
	m.units = append(m.units, cfg.Unit())
	return &invariantCounter{meter: m}, nil
}

// additions copies the additions recorded so far.
func (m *invariantMeter) additions() []invariantAdd {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]invariantAdd(nil), m.adds...)
}

// invariantAudit records every layered audit record it received.
type invariantAudit struct {
	mu      sync.Mutex
	records []plugin.AuditRecord
	err     error
}

// Audit records the layered audit record and reports the configured failure.
func (a *invariantAudit) Audit(_ context.Context, record plugin.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return a.err
}

// only reports the single layered record that landed.
func (a *invariantAudit) only(t *testing.T) plugin.AuditRecord {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.records) != 1 {
		t.Fatalf("audit records = %d, want exactly one landed record", len(a.records))
	}
	return a.records[0]
}

// invariantEvent builds one committed fact carrying the identity and frozen type.
func invariantEvent(key event.EventID, kind string) event.Event {
	return event.Event{Key: key, Type: kind}
}

// invariantAttr returns the value of one attribute of a rendered set.
func invariantAttr(attrs []attribute.KeyValue, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range attrs {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

// TestOTelSwitchInvariants pins that closing OTel changes the observation face and nothing else.
// - a closed observer creates no counter and calls neither the tracer nor the counter
// - a closed observer still lands one audit record carrying the fact and two skipped attempts
// - an open observer runs trace then metric once per observed fact and counts each fact once
// - the span and the counter carry the fact identity and frozen type and no content
// - a failed audit record is a wrapped missing observation while the SDK layers still ran
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestOTelSwitchInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a closed observer creates no counter and makes no SDK call", func(t *testing.T) {
		tracer := &invariantTracer{}
		meter := &invariantMeter{}
		audit := &invariantAudit{}
		observer, err := NewSDKObserver(audit, WithOTelDisabled(), WithTracer(tracer), WithMeter(meter))
		if err != nil {
			t.Fatalf("assemble a closed observer: %v", err)
		}
		if observer.counter != nil {
			t.Fatalf("closed observer counter = %v, want no counter created while OTel is closed", observer.counter)
		}
		if len(meter.names) != 0 {
			t.Fatalf("counters created = %v, want the meter untouched while OTel is closed", meter.names)
		}
		if err := observer.Observe(ctx, invariantEvent("event-1", "turn.completed")); err != nil {
			t.Fatalf("observe with OTel closed: %v", err)
		}
		if tracer.count() != 0 {
			t.Fatalf("spans = %d, want no span recorded while OTel is closed", tracer.count())
		}
		if len(meter.additions()) != 0 {
			t.Fatalf("counter additions = %+v, want no count while OTel is closed", meter.additions())
		}
		record := audit.only(t)
		if record.Fact != "event-1" {
			t.Fatalf("audit fact = %q, want the observed fact still named", record.Fact)
		}
		if len(record.Attempts) != 2 {
			t.Fatalf("attempts = %+v, want one entry per layer", record.Attempts)
		}
		if record.Attempts[0].Layer != plugin.LayerTrace || record.Attempts[1].Layer != plugin.LayerMetric {
			t.Fatalf("attempt order = (%q, %q), want trace then metric", record.Attempts[0].Layer, record.Attempts[1].Layer)
		}
		for _, attempt := range record.Attempts {
			if !attempt.Skipped {
				t.Fatalf("attempt = %+v, want a closed layer recorded as skipped instead of omitted", attempt)
			}
		}
	})

	t.Run("an open observer runs trace then metric once per fact", func(t *testing.T) {
		tracer := &invariantTracer{}
		meter := &invariantMeter{}
		audit := &invariantAudit{}
		observer, err := NewSDKObserver(audit, WithTracer(tracer), WithMeter(meter))
		if err != nil {
			t.Fatalf("assemble an open observer: %v", err)
		}
		if observer.counter == nil {
			t.Fatalf("open observer counter = nil, want the counter created once at assembly")
		}
		if len(meter.names) != 1 || meter.names[0] != counterName {
			t.Fatalf("counters created = %v, want the one named %q", meter.names, counterName)
		}
		if meter.units[0] != "1" || meter.descriptions[0] == "" {
			t.Fatalf("counter construction = (unit %q, description %q), want unit 1 beside its description",
				meter.units[0], meter.descriptions[0])
		}
		if err := observer.Observe(ctx, invariantEvent("event-1", "turn.completed")); err != nil {
			t.Fatalf("observe with OTel open: %v", err)
		}
		if tracer.count() != 1 {
			t.Fatalf("spans = %d, want one span for one fact", tracer.count())
		}
		span := tracer.spans[0]
		if span.name != spanName {
			t.Fatalf("span name = %q, want %q", span.name, spanName)
		}
		if !span.ended {
			t.Fatalf("span ended = false, want the trace layer closed before the metric layer")
		}
		value, ok := invariantAttr(span.attrs, attrEventKey)
		if !ok || value.AsString() != "event-1" {
			t.Fatalf("span attributes = %v, want the fact identity", span.attrs)
		}
		value, ok = invariantAttr(span.attrs, attrEventType)
		if !ok || value.AsString() != "turn.completed" {
			t.Fatalf("span attributes = %v, want the frozen event type", span.attrs)
		}
		adds := meter.additions()
		if len(adds) != 1 || adds[0].value != 1 {
			t.Fatalf("counter additions = %+v, want the fact counted exactly once", adds)
		}
		value, ok = invariantAttr(adds[0].attrs, attrEventKey)
		if !ok || value.AsString() != "event-1" {
			t.Fatalf("counter attributes = %v, want the fact identity", adds[0].attrs)
		}
		if _, ok := invariantAttr(adds[0].attrs, attrEventType); ok {
			t.Fatalf("counter attributes = %v, want no content beyond the fact identity", adds[0].attrs)
		}
		record := audit.only(t)
		if record.Fact != "event-1" {
			t.Fatalf("audit fact = %q, want the observed fact", record.Fact)
		}
		for _, attempt := range record.Attempts {
			if attempt.Skipped {
				t.Fatalf("attempt = %+v, want an open layer recorded as made", attempt)
			}
		}
	})

	t.Run("one fact is counted once whatever earlier facts were observed", func(t *testing.T) {
		tracer := &invariantTracer{}
		meter := &invariantMeter{}
		audit := &invariantAudit{}
		observer, err := NewSDKObserver(audit, WithTracer(tracer), WithMeter(meter))
		if err != nil {
			t.Fatalf("assemble an open observer: %v", err)
		}
		for _, key := range []event.EventID{"event-1", "event-2", "event-3"} {
			if err := observer.Observe(ctx, invariantEvent(key, "turn.completed")); err != nil {
				t.Fatalf("observe %q: %v", key, err)
			}
		}
		if tracer.count() != 3 {
			t.Fatalf("spans = %d, want one span per fact and no repetition", tracer.count())
		}
		adds := meter.additions()
		if len(adds) != 3 {
			t.Fatalf("counter additions = %+v, want one addition per fact", adds)
		}
		for i, add := range adds {
			if add.value != 1 {
				t.Fatalf("addition %d = %d, want every fact counted once", i, add.value)
			}
		}
		audit.mu.Lock()
		records := append([]plugin.AuditRecord(nil), audit.records...)
		audit.mu.Unlock()
		if len(records) != 3 {
			t.Fatalf("audit records = %d, want one record per fact", len(records))
		}
		seen := map[event.EventID]int{}
		for _, record := range records {
			seen[record.Fact]++
			if len(record.Attempts) != 2 {
				t.Fatalf("attempts of %q = %d, want one per layer per fact", record.Fact, len(record.Attempts))
			}
		}
		for _, key := range []event.EventID{"event-1", "event-2", "event-3"} {
			if seen[key] != 1 {
				t.Fatalf("record count for %q = %d, want exactly one", key, seen[key])
			}
		}
	})

	t.Run("a failed audit is a wrapped missing observation", func(t *testing.T) {
		tracer := &invariantTracer{}
		meter := &invariantMeter{}
		refused := errors.New("the audit side refused the record")
		audit := &invariantAudit{err: refused}
		observer, err := NewSDKObserver(audit, WithTracer(tracer), WithMeter(meter))
		if err != nil {
			t.Fatalf("assemble an open observer: %v", err)
		}
		err = observer.Observe(ctx, invariantEvent("event-1", "turn.completed"))
		if !errors.Is(err, refused) {
			t.Fatalf("observe = %v, want the audit failure reported", err)
		}
		if !strings.Contains(err.Error(), "telemetry: audit layer:") {
			t.Fatalf("observe = %q, want the failed layer named", err)
		}
		if tracer.count() != 1 || len(meter.additions()) != 1 {
			t.Fatalf("SDK layers = (spans %d, additions %d), want them unaffected by the audit failure",
				tracer.count(), len(meter.additions()))
		}
	})

	t.Run("a counter that cannot be created refuses the observer", func(t *testing.T) {
		meter := &invariantMeter{counterErr: errors.New("no meter provider")}
		observer, err := NewSDKObserver(&invariantAudit{}, WithMeter(meter))
		if err == nil || observer != nil {
			t.Fatalf("assembled observer = (%v, %v), want a named refusal", observer, err)
		}
		if !strings.Contains(err.Error(), counterName) {
			t.Fatalf("refusal = %q, want the counter named", err)
		}
	})

	t.Run("a nil observer answers an error instead of panicking", func(t *testing.T) {
		var observer *SDKObserver
		if err := observer.Observe(ctx, invariantEvent("event-1", "turn.completed")); err == nil {
			t.Fatalf("observe on a nil observer = nil, want a named error")
		}
	})
}

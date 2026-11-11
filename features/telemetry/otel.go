// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// The OTel/audit face of the observation bypass: the layered SDK observation
// attempted for one committed fact, beside the audit record that layering
// leaves behind. The tracer and meter handles are injected by the assembly that
// owns the export pipeline, so this package reads no process-level default and
// rewrites none; every layer is attempted after the fact committed and never
// before it.
//
// Nothing here owns runtime state. The observation is switched off by
// configuration and never by data: with OTel closed the trace and metric
// layers are recorded as skipped attempts, the audit record still lands, and
// every committed fact stays readable exactly as before, so closing OTel
// changes neither a committed fact nor any query read.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/plugin"
)

// SDKObserver is the post-commit observer over the framework OTel SDK. It
// attempts the trace and metric layers in the fixed layer order, records each
// attempt into the layered audit record, and hands that record to the audit
// sink: SDK attempts at observation are observed layer by layer instead of
// collapsing into one opaque success.
//
// With OTel closed the layers are recorded as skipped and no SDK call is
// made; with OTel open the layers run against the tracer and meter handles,
// which stay the framework's noop handles until the framework configures an
// exporter. Every failure here is a missing observation: it is returned to the
// caller, recorded as a gap beside the plugin that failed, and never rolls the
// committed fact back or re-runs the observed work.
type SDKObserver struct {
	tracer  trace.Tracer
	meter   metric.Meter
	counter metric.Int64Counter
	audit   plugin.AuditSink
	enabled bool
}

// spanName is the span the trace layer records for one committed fact.
const spanName = "tagent.commit"

// meterName is the instrumentation scope the metric layer records under.
const meterName = "tagent/telemetry"

// counterName is the counter the metric layer adds to for one committed fact.
const counterName = "tagent.committed.facts"

// attrEventType is the only attribute the trace and metric layers carry: the
// committed fact's frozen type. The fact identity and the event content stay
// out of every span and metric, because a per-fact label multiplies the series
// count by the number of facts the runtime commits.
var attrEventType = attribute.Key("tagent.event.type")

// Option configures one SDKObserver at assembly time.
type Option func(*SDKObserver)

// WithTracer injects the tracer handle the trace layer records through, so the
// assembly that owns the export pipeline decides where the span goes.
func WithTracer(tracer trace.Tracer) Option {
	return func(o *SDKObserver) { o.tracer = tracer }
}

// WithMeter injects the meter handle the metric layer records through, so the
// assembly that owns the export pipeline decides where the counter goes.
func WithMeter(meter metric.Meter) Option {
	return func(o *SDKObserver) { o.meter = meter }
}

// WithOTelDisabled switches the OTel layers off by configuration: the trace
// and metric layers are recorded as skipped attempts, no SDK call is made, and
// the audit record still lands. Closing OTel is a legitimate state and never a
// reason to withhold any committed fact from any reader.
func WithOTelDisabled() Option {
	return func(o *SDKObserver) { o.enabled = false }
}

// NewSDKObserver assembles one post-commit observer: the audit sink the
// layered records land in, beside the handles and switches the assembly
// injects. Without an injected handle the layers record into the OpenTelemetry
// noop SDK, so an unconfigured runtime pays nothing for observation without
// this package reading a process-level default or installing one.
func NewSDKObserver(audit plugin.AuditSink, opts ...Option) (*SDKObserver, error) {
	o := &SDKObserver{
		tracer:  tracenoop.NewTracerProvider().Tracer(meterName),
		meter:   metricnoop.NewMeterProvider().Meter(meterName),
		audit:   audit,
		enabled: true,
	}
	for _, opt := range opts {
		opt(o)
	}
	if !o.enabled {
		return o, nil
	}
	counter, err := o.meter.Int64Counter(counterName,
		metric.WithDescription("Committed facts observed through the post-commit bypass"),
		metric.WithUnit("1"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: counter %s: %w", counterName, err)
	}
	o.counter = counter
	return o, nil
}

// Observe observes one committed fact through the layered SDK attempt and
// leaves the layered audit record behind. It runs after the fact is durable,
// so a slow or failing observation can never hold that fact back, reorder it,
// or withhold it from any reader.
//
// A returned error records a missing observation: a failed layer attempt or an
// audit record that did not land. It never records an undone fact, and the
// observed work is never re-run to satisfy an observer.
func (o *SDKObserver) Observe(ctx context.Context, evt event.Event) error {
	if o == nil {
		return errors.New("telemetry: observe on a nil SDK observer")
	}
	record := plugin.AuditRecord{Fact: evt.Key, Attempts: make([]plugin.Attempt, 0, 2)}
	var failures []error

	traceAttempt := plugin.Attempt{Layer: plugin.LayerTrace}
	if !o.enabled {
		traceAttempt.Skipped = true
	} else {
		_, span := o.tracer.Start(ctx, spanName)
		span.SetAttributes(attrEventType.String(evt.Type))
		span.End()
	}
	record.Attempts = append(record.Attempts, traceAttempt)

	metricAttempt := plugin.Attempt{Layer: plugin.LayerMetric}
	if !o.enabled {
		metricAttempt.Skipped = true
	} else {
		o.counter.Add(ctx, 1, metric.WithAttributes(attrEventType.String(evt.Type)))
	}
	record.Attempts = append(record.Attempts, metricAttempt)

	if o.audit != nil {
		if err := o.audit.Audit(ctx, record); err != nil {
			failures = append(failures, fmt.Errorf("telemetry: audit layer: %w", err))
		}
	}
	return errors.Join(failures...)
}

// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
//
// The OTel/audit face of the observation bypass: the layered SDK observation
// attempted for one committed fact, beside the audit record that layering
// leaves behind. The observer is a thin wrapper over the tracer and meter
// handles the locked framework exposes, so the framework's own OTel SDK is
// reused rather than re-hosted, and every layer is attempted after the fact
// committed and never before it.
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
	"go.opentelemetry.io/otel/trace"

	fmetric "trpc.group/trpc-go/trpc-agent-go/telemetry/metric"
	ftrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	"github.com/SpellingDragon/tagent/event"
)

// Layer names one layer of the layered SDK observation attempted for one
// committed fact. The layers are attempted in the fixed order the constants
// declare, so one audit record always reads trace then metric regardless of
// how many facts one run observes.
type Layer string

const (
	// LayerTrace is the trace layer: the span the SDK records for one
	// committed fact.
	LayerTrace Layer = "trace"

	// LayerMetric is the metric layer: the counter the SDK adds for one
	// committed fact.
	LayerMetric Layer = "metric"
)

// Attempt is one SDK observation attempt at one layer: the layer it was made
// at and whether it was skipped because the observation is switched off. A
// skipped attempt is a legitimate recorded state, not a silent omission: an
// audit reader sees which layers the SDK attempted and which it deliberately
// left alone.
type Attempt struct {
	// Layer names the layer the attempt was made at.
	Layer Layer

	// Skipped reports the attempt was not made because the observation is
	// switched off. A layer that ran records the attempt as made, whether or
	// not the framework behind it holds a real exporter yet.
	Skipped bool
}

// AuditRecord is the layered audit record of one committed fact's observation:
// one entry per SDK layer, in the fixed layer order, describing the attempt
// that layer made for that fact. The record is evidence about the observation,
// never about the fact: it carries the fact's identity and the layered
// attempts and rewrites nothing the fact already holds.
type AuditRecord struct {
	// Fact is the identity of the committed fact the attempts were made for.
	Fact event.EventID

	// Attempts holds one entry per SDK layer, in the fixed layer order
	// trace then metric.
	Attempts []Attempt
}

// AuditSink is the audit side of the observation bypass: it receives the
// layered audit record of one observed fact and reports whether the record
// landed. An implementation must be safe for concurrent use, because the
// post-commit pass may observe facts from several writers at once.
//
// The redline list (3 rules):
// MUST be safe for concurrent use.
// MUST report a record that did not land as a missing observation.
// MUST NOT touch the committed fact the record describes.
type AuditSink interface {
	// Audit hands one layered audit record to the audit side. A returned
	// error means the record did not land and is reported to the caller as a
	// missing observation; it never touches the committed fact the record
	// describes.
	Audit(ctx context.Context, record AuditRecord) error
}

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
	audit   AuditSink
	enabled bool
}

// spanName is the span the trace layer records for one committed fact.
const spanName = "tagent.commit"

// meterName is the instrumentation scope the metric layer records under.
const meterName = "tagent/telemetry"

// counterName is the counter the metric layer adds to for one committed fact.
const counterName = "tagent.committed.facts"

// attrEventKey and attrEventType are the only attributes the trace and metric
// layers carry: the committed fact's identity and frozen type. Event content
// and message text never enter a span or a metric.
var (
	attrEventKey  = attribute.Key("tagent.event.key")
	attrEventType = attribute.Key("tagent.event.type")
)

// Option configures one SDKObserver at assembly time.
type Option func(*SDKObserver)

// WithTracer replaces the default framework tracer handle, so a caller that
// hosts its own tracer provider can point the trace layer at it.
func WithTracer(tracer trace.Tracer) Option {
	return func(o *SDKObserver) { o.tracer = tracer }
}

// WithMeter replaces the default framework meter handle, so a caller that
// hosts its own meter provider can point the metric layer at it.
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
// layered records land in, beside the options that redirect or close the OTel
// layers. The default handles are the framework's own, so an unconfigured
// runtime observes through the framework's noop SDK at zero cost and a
// configured one exports through the framework's OTel pipeline without this
// package re-hosting an SDK.
func NewSDKObserver(audit AuditSink, opts ...Option) (*SDKObserver, error) {
	o := &SDKObserver{
		tracer:  ftrace.Tracer,
		meter:   fmetric.GetMeterProvider().Meter(meterName),
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
	record := AuditRecord{Fact: evt.Key, Attempts: make([]Attempt, 0, 2)}
	var failures []error

	traceAttempt := Attempt{Layer: LayerTrace}
	if !o.enabled {
		traceAttempt.Skipped = true
	} else {
		_, span := o.tracer.Start(ctx, spanName)
		span.SetAttributes(attrEventKey.String(string(evt.Key)), attrEventType.String(evt.Type))
		span.End()
	}
	record.Attempts = append(record.Attempts, traceAttempt)

	metricAttempt := Attempt{Layer: LayerMetric}
	if !o.enabled {
		metricAttempt.Skipped = true
	} else {
		o.counter.Add(ctx, 1, metric.WithAttributes(attrEventKey.String(string(evt.Key))))
	}
	record.Attempts = append(record.Attempts, metricAttempt)

	if o.audit != nil {
		if err := o.audit.Audit(ctx, record); err != nil {
			failures = append(failures, fmt.Errorf("telemetry: audit layer: %w", err))
		}
	}
	return errors.Join(failures...)
}

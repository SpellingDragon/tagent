// The 05.3 acceptance tests of the post-commit observation bypass over a real
// runtime assembly: the layered OTel/audit face beside the query reads it must
// never own.
package inspection

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/query"
	"github.com/SpellingDragon/tagent/telemetry"
)

// commitSet is the committed-fact face of these tests: it records the facts
// whose canonical commit happened and refuses every identity that did not
// commit, so the post-commit wiring can only ever observe a real fact.
type commitSet struct {
	mu    sync.Mutex
	facts map[event.EventID]event.Event
}

// newCommitSet returns an empty commit set.
func newCommitSet() *commitSet {
	return &commitSet{facts: map[event.EventID]event.Event{}}
}

// commit records one canonical commit of a fact.
func (s *commitSet) commit(evt event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.facts[evt.Key] = evt
}

// CommittedFact returns the committed fact the identity names, refusing an
// identity that never committed.
func (s *commitSet) CommittedFact(ctx context.Context, id event.EventID) (event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	evt, ok := s.facts[id]
	if !ok {
		return event.Event{}, fmt.Errorf("commit set: %s did not commit", id)
	}
	return evt, nil
}

var _ plugin.FactLookup = (*commitSet)(nil)

// eventSpy is one observation sink that reports every fact it received and can
// fail on demand, so a delivered fact and a failed observation are both
// checkable at the wiring's boundary.
type eventSpy struct {
	mu     sync.Mutex
	events []event.Event
	err    error
}

// Observe records the fact it was handed and reports the failure it was armed
// with.
func (s *eventSpy) Observe(ctx context.Context, evt event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
	return s.err
}

var _ plugin.ObservationSink = (*eventSpy)(nil)

// auditLog is the in-memory audit sink of these tests: it keeps the layered
// records as received and can fail on demand.
type auditLog struct {
	mu      sync.Mutex
	records []telemetry.AuditRecord
	err     error
}

// Audit keeps one layered audit record and reports the failure it was armed
// with.
func (a *auditLog) Audit(ctx context.Context, record telemetry.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, record)
	return a.err
}

func (a *auditLog) snapshot() []telemetry.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]telemetry.AuditRecord(nil), a.records...)
}

var _ telemetry.AuditSink = (*auditLog)(nil)

// observePlugin is the registration unit the pipeline dispatches into the
// post-commit phase: one stable name beside the observation wiring it serves.
type observePlugin struct {
	observer plugin.PostCommitObserver
}

// Name returns the stable plugin name recorded in every gap.
func (p observePlugin) Name() string { return "observation" }

// Phase reports the post-commit observation phase this plugin serves.
func (p observePlugin) Phase() plugin.Phase { return plugin.PhasePostCommitObserve }

// ObserveCommit delegates one committed fact to the wired observer.
func (p observePlugin) ObserveCommit(ctx context.Context, id event.EventID) error {
	return p.observer.ObserveCommit(ctx, id)
}

var (
	_ plugin.Plugin             = observePlugin{}
	_ plugin.PostCommitObserver = observePlugin{}
)

// newObservePipeline assembles the After pass around one observation wiring.
func newObservePipeline(t *testing.T, observer plugin.PostCommitObserver) plugin.Pipeline {
	t.Helper()
	pipeline, err := plugin.NewPipeline([]plugin.Plugin{observePlugin{observer: observer}})
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	return pipeline
}

// observationFact returns one committed fact of the given identity.
func observationFact(key event.EventID) event.Event {
	return event.Event{Key: key, Type: event.TypeActionCommand}
}

// TestObservationDoesNotOwnState proves the post-commit observation bypass of the plugin pipeline over the SDK face:
// - 提交后才观察: the observer sees one fact only after its commit, and an uncommitted identity is a named gap
// - 失败记缺口不回滚事实: a failing observation is a gap beside its plugin name and the committed fact stays readable
// - 审计记录分层: every observed fact leaves one layered audit record naming each SDK layer attempt
// - 关闭 OTel 不影响 Query: with the observation switched off every query face reads exactly as before
// - query 读面零遥测依赖: the query package never imports the observation face
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestObservationDoesNotOwnState(t *testing.T) {
	t.Run("S1 提交后才观察: the observer sees one fact only after its commit", func(t *testing.T) {
		ctx := context.Background()
		spy := &eventSpy{}
		store := newCommitSet()
		pipeline := newObservePipeline(t, plugin.NewCommitObserver(store, spy))
		evt := observationFact("fact-a1")
		store.commit(evt)
		if got := len(spy.events); got != 0 {
			t.Fatalf("提交即被观察: %d observations before the After pass", got)
		}
		pres := pipeline.After(ctx, evt.Key)
		if len(pres.Gaps) != 0 {
			t.Fatalf("提交后观察留缺口: %+v", pres.Gaps)
		}
		if got := len(spy.events); got != 1 {
			t.Fatalf("观察次数 = %d, want 1", got)
		}
		if !reflect.DeepEqual(spy.events[0], evt) {
			t.Fatalf("事实被改写: got %+v, want %+v", spy.events[0], evt)
		}
		presUncommitted := pipeline.After(ctx, event.EventID("fact-never-committed"))
		if len(presUncommitted.Gaps) != 1 {
			t.Fatalf("未提交事实的缺口数 = %d, want 1", len(presUncommitted.Gaps))
		}
		gap := presUncommitted.Gaps[0]
		if gap.Plugin != "observation" || gap.Fact != event.EventID("fact-never-committed") || gap.Err == nil {
			t.Fatalf("缺口未按名记录: %+v", gap)
		}
		if got := len(spy.events); got != 1 {
			t.Fatalf("未提交事实被观察: %d observations", got)
		}
	})

	t.Run("S2 失败记缺口不回滚事实: a failing observation is a gap and the fact stays", func(t *testing.T) {
		ctx := context.Background()
		refusal := errors.New("the collector refused the observation")
		spy := &eventSpy{err: refusal}
		store := newCommitSet()
		pipeline := newObservePipeline(t, plugin.NewCommitObserver(store, spy))
		evt := observationFact("fact-b1")
		store.commit(evt)

		pres := pipeline.After(ctx, evt.Key)
		if len(pres.Gaps) != 1 {
			t.Fatalf("失败观察的缺口数 = %d, want 1", len(pres.Gaps))
		}
		if pres.Gaps[0].Plugin != "observation" || !errors.Is(pres.Gaps[0].Err, refusal) {
			t.Fatalf("缺口未记失败原因: %+v", pres.Gaps[0])
		}
		if got := len(spy.events); got != 1 {
			t.Fatalf("观察被重跑: %d observations", got)
		}
		stored, err := store.CommittedFact(ctx, evt.Key)
		if err != nil {
			t.Fatalf("观察失败后事实不可读: %v", err)
		}
		if !reflect.DeepEqual(stored, evt) {
			t.Fatalf("观察失败回滚了事实: %+v, want %+v", stored, evt)
		}
	})

	t.Run("S3 审计记录分层: one layered audit record names every SDK layer attempt", func(t *testing.T) {
		ctx := context.Background()
		spans := tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
		defer func() { _ = tp.Shutdown(context.Background()) }()
		reader := sdkmetric.NewManualReader()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
		defer func() { _ = mp.Shutdown(context.Background()) }()

		audit := &auditLog{}
		obs, err := telemetry.NewSDKObserver(audit,
			telemetry.WithTracer(tp.Tracer("inspection")),
			telemetry.WithMeter(mp.Meter("inspection")))
		if err != nil {
			t.Fatalf("NewSDKObserver: %v", err)
		}
		store := newCommitSet()
		pipeline := newObservePipeline(t, plugin.NewCommitObserver(store, obs))
		evt := observationFact("fact-c1")
		store.commit(evt)

		pres := pipeline.After(ctx, evt.Key)
		if len(pres.Gaps) != 0 {
			t.Fatalf("开启态观察留缺口: %+v", pres.Gaps)
		}
		records := audit.snapshot()
		if len(records) != 1 {
			t.Fatalf("审计记录数 = %d, want 1", len(records))
		}
		record := records[0]
		if record.Fact != evt.Key {
			t.Fatalf("审计记录事实 = %s, want %s", record.Fact, evt.Key)
		}
		if len(record.Attempts) != 2 {
			t.Fatalf("分层尝试数 = %d, want 2", len(record.Attempts))
		}
		if record.Attempts[0].Layer != telemetry.LayerTrace || record.Attempts[0].Skipped {
			t.Fatalf("trace 层尝试未留痕: %+v", record.Attempts[0])
		}
		if record.Attempts[1].Layer != telemetry.LayerMetric || record.Attempts[1].Skipped {
			t.Fatalf("metric 层尝试未留痕: %+v", record.Attempts[1])
		}
		exported := spans.GetSpans()
		if len(exported) != 1 || exported[0].Name != "tagent.commit" {
			t.Fatalf("内存 exporter 收到 %d 个 span，want 1 个 tagent.commit: %+v", len(exported), exported)
		}
		var keyed bool
		for _, attr := range exported[0].Attributes {
			if string(attr.Key) == "tagent.event.key" && attr.Value.AsString() == string(evt.Key) {
				keyed = true
			}
		}
		if !keyed {
			t.Fatalf("span 未记事实身份: %+v", exported[0].Attributes)
		}
		if got := collectCounter(t, ctx, reader); got != 1 {
			t.Fatalf("metric 层计数 = %d, want 1", got)
		}

		auditOff := &auditLog{}
		obsOff, err := telemetry.NewSDKObserver(auditOff,
			telemetry.WithTracer(tp.Tracer("inspection")),
			telemetry.WithMeter(mp.Meter("inspection")),
			telemetry.WithOTelDisabled())
		if err != nil {
			t.Fatalf("NewSDKObserver (off): %v", err)
		}
		pipelineOff := newObservePipeline(t, plugin.NewCommitObserver(store, obsOff))
		evtOff := observationFact("fact-c2")
		store.commit(evtOff)
		presOff := pipelineOff.After(ctx, evtOff.Key)
		if len(presOff.Gaps) != 0 {
			t.Fatalf("关断态观察留缺口: %+v", presOff.Gaps)
		}
		offRecords := auditOff.snapshot()
		if len(offRecords) != 1 {
			t.Fatalf("关断态审计记录数 = %d, want 1", len(offRecords))
		}
		for _, attempt := range offRecords[0].Attempts {
			if !attempt.Skipped {
				t.Fatalf("关断态层未记 skipped: %+v", attempt)
			}
		}
		if got := len(spans.GetSpans()); got != 1 {
			t.Fatalf("关断态仍产 span: %d", got)
		}
		if got := collectCounter(t, ctx, reader); got != 1 {
			t.Fatalf("关断态仍计 metric: %d", got)
		}
	})

	t.Run("S4 关闭 OTel 不影响 Query: query reads stay complete with observation on or off", func(t *testing.T) {
		f := newRuntimeFixture(t)
		proj := newTestProjection(f.inventory, 8)
		baseline := readQueryFaces(t, f.ctx, f.inventory, proj)

		store := newCommitSet()
		obsOff, err := telemetry.NewSDKObserver(&auditLog{}, telemetry.WithOTelDisabled())
		if err != nil {
			t.Fatalf("NewSDKObserver (off): %v", err)
		}
		pipelineOff := newObservePipeline(t, plugin.NewCommitObserver(store, obsOff))
		for _, key := range []event.EventID{"fact-e1", "fact-e2"} {
			store.commit(observationFact(key))
			if pres := pipelineOff.After(f.ctx, key); len(pres.Gaps) != 0 {
				t.Fatalf("关断态观察留缺口: %+v", pres.Gaps)
			}
		}
		afterOff := readQueryFaces(t, f.ctx, f.inventory, proj)
		if !reflect.DeepEqual(baseline, afterOff) {
			t.Fatalf("关闭 OTel 改变了 Query 读面:\nbefore %+v\nafter  %+v", baseline, afterOff)
		}

		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter()))
		defer func() { _ = tp.Shutdown(context.Background()) }()
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader()))
		defer func() { _ = mp.Shutdown(context.Background()) }()
		obsOn, err := telemetry.NewSDKObserver(&auditLog{},
			telemetry.WithTracer(tp.Tracer("inspection")),
			telemetry.WithMeter(mp.Meter("inspection")))
		if err != nil {
			t.Fatalf("NewSDKObserver (on): %v", err)
		}
		pipelineOn := newObservePipeline(t, plugin.NewCommitObserver(store, obsOn))
		store.commit(observationFact("fact-e3"))
		if pres := pipelineOn.After(f.ctx, event.EventID("fact-e3")); len(pres.Gaps) != 0 {
			t.Fatalf("开启态观察留缺口: %+v", pres.Gaps)
		}
		afterOn := readQueryFaces(t, f.ctx, f.inventory, proj)
		if !reflect.DeepEqual(baseline, afterOn) {
			t.Fatalf("开启 OTel 改变了 Query 读面（观测不拥有状态）:\nbefore %+v\nafter  %+v", baseline, afterOn)
		}
	})

	t.Run("S5 query 读面零遥测依赖: the query package never imports the observation face", func(t *testing.T) {
		dir := filepath.Join("..", "..", "..", "query")
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read query dir: %v", err)
		}
		fset := token.NewFileSet()
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", entry.Name(), err)
			}
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if strings.Contains(path, "telemetry") {
					t.Fatalf("query 读面依赖观测面: %s imports %s", entry.Name(), path)
				}
			}
		}
	})
}

// readState is one complete pass over the query read faces: the inventory
// view, a snapshot, one page, one detail and one subscription increment batch.
type readState struct {
	agents      []query.AgentEntry
	sessions    []query.SessionEntry
	invocations []query.InvocationEntry
	tasks       []query.TaskEntry
	notices     []query.NoticeEntry
	leases      []query.LeaseEntry
	details     []query.Detail
	watermarks  []query.SourceWatermark
	page        []query.Detail
	detail      query.Detail
	updates     []query.Update
}

// readQueryFaces takes one complete read over every query face of the
// assembly, so two readings of an untouched runtime compare equal field by
// field and any state an observer owned would show.
func readQueryFaces(t *testing.T, ctx context.Context, inv *query.Inventory, proj *query.Projection) readState {
	t.Helper()
	view, err := inv.View(ctx)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	snap, cur, err := proj.Snapshot(ctx, filterAll)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	page, err := proj.List(snap.Revision, cur, filterAll, query.ObjectRef{}, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var detail query.Detail
	if len(snap.Details) > 0 {
		detail, err = proj.Detail(snap.Revision, cur, filterAll, snap.Details[0].Ref)
		if err != nil {
			t.Fatalf("Detail: %v", err)
		}
	}
	followFrom := snap.Revision
	if _, _, err := proj.Snapshot(ctx, filterAll); err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	_, updates, err := proj.Follow(ctx, query.Subscription{Revision: followFrom, Filter: filterAll}, 8)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	return readState{
		agents:      view.Agents(),
		sessions:    view.Sessions(),
		invocations: view.Invocations(),
		tasks:       view.Tasks(),
		notices:     view.Notices(),
		leases:      view.Leases(),
		details:     cloneDetails(snap.Details),
		watermarks:  append([]query.SourceWatermark(nil), snap.Watermarks...),
		page:        cloneDetails(page),
		detail:      cloneDetails([]query.Detail{detail})[0],
		updates:     updates,
	}
}

// collectCounter reads the manual reader and reports the value of the
// post-commit counter.
func collectCounter(t *testing.T, ctx context.Context, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "tagent.committed.facts" {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s carries %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, dp := range data.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

// countingSpanExporter forwards every batch to the real exporter and counts
// the spans it forwarded, so delivery evidence stays non-empty without the
// counting standing in for the collector's own acceptance.
type countingSpanExporter struct {
	sdktrace.SpanExporter
	mu    sync.Mutex
	spans int
}

// ExportSpans forwards one batch and counts the spans in it.
func (e *countingSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	e.spans += len(spans)
	e.mu.Unlock()
	return e.SpanExporter.ExportSpans(ctx, spans)
}

// spansSent reports how many spans were forwarded to the real exporter.
func (e *countingSpanExporter) spansSent() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spans
}

// countingMetricExporter forwards every batch to the real exporter and counts
// the data points it forwarded.
type countingMetricExporter struct {
	sdkmetric.Exporter
	mu     sync.Mutex
	points int
}

// Export forwards one batch and counts the data points in it.
func (e *countingMetricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	count := 0
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				count += len(data.DataPoints)
			case metricdata.Sum[float64]:
				count += len(data.DataPoints)
			case metricdata.Gauge[int64]:
				count += len(data.DataPoints)
			case metricdata.Gauge[float64]:
				count += len(data.DataPoints)
			case metricdata.Histogram[float64]:
				count += len(data.DataPoints)
			case metricdata.Histogram[int64]:
				count += len(data.DataPoints)
			}
		}
	}
	e.mu.Lock()
	e.points += count
	e.mu.Unlock()
	return e.Exporter.Export(ctx, rm)
}

// pointsSent reports how many data points were forwarded to the real exporter.
func (e *countingMetricExporter) pointsSent() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.points
}

// newRealSpanExporter builds the OTLP span exporter for the enabled collector
// gate, honouring OTEL_EXPORTER_OTLP_PROTOCOL beside the endpoint.
func newRealSpanExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL") == "grpc" {
		return otlptracegrpc.New(ctx)
	}
	return otlptracehttp.New(ctx)
}

// newRealMetricExporter builds the OTLP metric exporter for the enabled
// collector gate, honouring OTEL_EXPORTER_OTLP_PROTOCOL beside the endpoint.
func newRealMetricExporter(ctx context.Context) (sdkmetric.Exporter, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL") == "grpc" {
		return otlpmetricgrpc.New(ctx)
	}
	return otlpmetrichttp.New(ctx)
}

// TestRealCollectorDelivery proves the real collector receives the spans and metrics of the post-commit observation:
// - 启用门: the test runs only with OTEL_EXPORTER_OTLP_ENDPOINT set, and is a legal Skip without it
// - 非空接收: the collector accepts a non-empty span batch and a non-empty metric batch
// - 零产出拒收: an observation run that forwards nothing fails instead of passing
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestRealCollectorDelivery(t *testing.T) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		t.Skip("real collector gate not enabled: OTEL_EXPORTER_OTLP_ENDPOINT is unset (09.5 runs this under authorization and budget)")
	}
	ctx := context.Background()
	spanExporter, err := newRealSpanExporter(ctx)
	if err != nil {
		t.Fatalf("span exporter for the enabled collector %s: %v", endpoint, err)
	}
	countingSpans := &countingSpanExporter{SpanExporter: spanExporter}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(countingSpans))
	metricExporter, err := newRealMetricExporter(ctx)
	if err != nil {
		t.Fatalf("metric exporter for the enabled collector %s: %v", endpoint, err)
	}
	countingPoints := &countingMetricExporter{Exporter: metricExporter}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(countingPoints)))

	audit := &auditLog{}
	obs, err := telemetry.NewSDKObserver(audit,
		telemetry.WithTracer(tp.Tracer("inspection")),
		telemetry.WithMeter(mp.Meter("inspection")))
	if err != nil {
		t.Fatalf("NewSDKObserver: %v", err)
	}
	store := newCommitSet()
	pipeline := newObservePipeline(t, plugin.NewCommitObserver(store, obs))
	for _, key := range []event.EventID{"collector-fact-1", "collector-fact-2"} {
		evt := observationFact(key)
		store.commit(evt)
		if pres := pipeline.After(ctx, key); len(pres.Gaps) != 0 {
			t.Fatalf("观察 %s 留缺口: %+v", key, pres.Gaps)
		}
	}

	if err := tp.ForceFlush(ctx); err != nil {
		t.Fatalf("span export to the real collector failed: %v", err)
	}
	if err := mp.ForceFlush(ctx); err != nil {
		t.Fatalf("metric export to the real collector failed: %v", err)
	}
	if err := tp.Shutdown(ctx); err != nil {
		t.Fatalf("span shutdown against the real collector: %v", err)
	}
	if err := mp.Shutdown(ctx); err != nil {
		t.Fatalf("metric shutdown against the real collector: %v", err)
	}
	spans := countingSpans.spansSent()
	points := countingPoints.pointsSent()
	if spans == 0 {
		t.Fatalf("零产出拒收: the collector accepted no span at all")
	}
	if points == 0 {
		t.Fatalf("零产出拒收: the collector accepted no metric point at all")
	}
	if records := audit.snapshot(); len(records) != 2 {
		t.Fatalf("审计记录数 = %d, want 2", len(records))
	}
	t.Logf("collector delivery evidence: spans=%d metric_points=%d", spans, points)
}

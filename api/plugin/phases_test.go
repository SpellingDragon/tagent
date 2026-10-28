package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// invariantPlugin is one registration element reporting the port it serves.
type invariantPlugin struct {
	name string
	port Port
}

// Name reports the stable name of the element.
func (p invariantPlugin) Name() string { return p.name }

// Port reports the one port the element serves.
func (p invariantPlugin) Port() Port { return p.port }

// invariantTransform is a call-transform element running the given correction.
type invariantTransform struct {
	invariantPlugin
	run func(*trpcmodel.ToolCall) error
}

// TransformCall runs the correction the element was built with.
func (p *invariantTransform) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	if p.run != nil {
		return p.run(call)
	}
	return nil
}

// invariantRisk is a risk element rendering one fixed grade.
type invariantRisk struct {
	invariantPlugin
	grade RiskLevel
}

// AssessRisk renders the grade the element was built with.
func (p invariantRisk) AssessRisk(context.Context, *trpcmodel.ToolCall) RiskLevel { return p.grade }

// invariantRiskBoth is one risk element carrying both faces of the risk port.
type invariantRiskBoth struct {
	invariantPlugin
	grade RiskLevel
}

// AssessRisk renders the grade the element was built with.
func (p invariantRiskBoth) AssessRisk(context.Context, *trpcmodel.ToolCall) RiskLevel {
	return p.grade
}

// Judge renders the ruling the element was built with.
func (p invariantRiskBoth) Judge(CallPass) (Decision, error) {
	return Decision{Disposition: DispositionAllow, Risk: p.grade}, nil
}

// invariantObserver is a post-commit observer recording its turn and failing when asked to.
type invariantObserver struct {
	invariantPlugin
	order *[]string
	err   error
}

// ObserveCommit records the observation and reports the configured failure.
func (p invariantObserver) ObserveCommit(_ context.Context, _ event.EventID) error {
	*p.order = append(*p.order, "observe:"+p.name)
	return p.err
}

// invariantPresenter is a final presenter recording its turn beside its answer.
type invariantPresenter struct {
	invariantPlugin
	order   *[]string
	seen    *[]event.EventID
	text    string
	dropped bool
}

// Present records the presentation and answers with the configured text.
func (p invariantPresenter) Present(_ context.Context, id event.EventID) (string, bool) {
	*p.order = append(*p.order, "present:"+p.name)
	if p.seen != nil {
		*p.seen = append(*p.seen, id)
	}
	return p.text, p.dropped
}

// invariantTrigger is a compress-trigger element recording each request it answers
// beside the refusal it was built with.
type invariantTrigger struct {
	invariantPlugin
	order *[]string
	seen  *[]event.EventID
	err   error
}

// TriggerCompress records the request it was dispatched for and answers the configured
// refusal.
func (p invariantTrigger) TriggerCompress(_ context.Context, request event.CompressRequest) error {
	if p.order != nil {
		*p.order = append(*p.order, "trigger:"+p.name)
	}
	if p.seen != nil {
		*p.seen = append(*p.seen, request.ID)
	}
	return p.err
}

// invariantInject is an environment-inject element answering the blocks it was built
// with and recording the assembly it was dispatched for.
type invariantInject struct {
	invariantPlugin
	order  *[]string
	seen   *[]EnvironmentRequest
	blocks []string
	err    error
}

// InjectEnvironment records the assembly it was dispatched for and answers its blocks.
func (p invariantInject) InjectEnvironment(_ context.Context, request EnvironmentRequest) ([]string, error) {
	if p.order != nil {
		*p.order = append(*p.order, "inject:"+p.name)
	}
	if p.seen != nil {
		*p.seen = append(*p.seen, request)
	}
	return p.blocks, p.err
}

// invariantRequest builds one compress request the way a producer files it.
func invariantRequest() event.CompressRequest {
	return event.CompressRequestFor("memory.meditation", "sequence-1", "the card sequence exceeds the bound")
}

// invariantCall builds one native tool call for the pass to transform.
func invariantCall(id, tool, args string) trpcmodel.ToolCall {
	return trpcmodel.ToolCall{ID: id, Function: trpcmodel.FunctionDefinitionParam{Name: tool, Arguments: []byte(args)}}
}

// invariantIndex returns a pointer to one streaming index.
func invariantIndex(index int) *int { return &index }

// TestPipelineRegistrationInvariants pins how one registration list becomes the port face sets.
// - one element is dispatched into the face set of the port it reports, and a risk element may carry both faces of that port
// - a nil element refuses the pipeline by name instead of being trimmed away
// - an element reporting a port outside the closed table is refused by name
// - an element reporting a port it does not implement is refused by name
// - the order inside one port is the order the elements were declared in
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPipelineRegistrationInvariants(t *testing.T) {
	t.Run("every element is dispatched into the face set of the port it reports", func(t *testing.T) {
		var order []string
		transform := &invariantTransform{invariantPlugin: invariantPlugin{name: "transform", port: Port{Reads: MaterialCall, Produces: ProductCall}}}
		risk := invariantRisk{invariantPlugin: invariantPlugin{name: "risk", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskLow}
		dual := invariantRiskBoth{invariantPlugin: invariantPlugin{name: "dual", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskHigh}
		observer := invariantObserver{invariantPlugin: invariantPlugin{name: "observer", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		presenter := invariantPresenter{invariantPlugin: invariantPlugin{name: "presenter", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order}

		pipeline, err := NewFlow([]Instance{transform, risk, dual, observer, presenter})
		if err != nil {
			t.Fatalf("register one element per port: %v", err)
		}
		if len(pipeline.transforms) != 1 || len(pipeline.risks) != 2 || len(pipeline.governors) != 1 {
			t.Fatalf("pre-execution faces = (transforms %d, risks %d, governors %d), want (1, 2, 1)",
				len(pipeline.transforms), len(pipeline.risks), len(pipeline.governors))
		}
		if len(pipeline.observers) != 1 || len(pipeline.presenters) != 1 {
			t.Fatalf("post-commit faces = (observers %d, presenters %d), want (1, 1)",
				len(pipeline.observers), len(pipeline.presenters))
		}
		if pipeline.risks[0].name != "risk" || pipeline.risks[1].name != "dual" {
			t.Fatalf("risk registration order = (%q, %q), want the declared order", pipeline.risks[0].name, pipeline.risks[1].name)
		}
		if pipeline.governors[0].name != "dual" {
			t.Fatalf("governor registration = %q, want the element carrying the second face", pipeline.governors[0].name)
		}
	})

	t.Run("the faces inside one port run in registration order", func(t *testing.T) {
		var order []string
		first := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run: func(call *trpcmodel.ToolCall) error {
				order = append(order, "first:"+call.Function.Name)
				return nil
			},
		}
		second := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run: func(call *trpcmodel.ToolCall) error {
				order = append(order, "second:"+call.Function.Name)
				return nil
			},
		}
		pipeline, err := NewFlow([]Instance{first, second})
		if err != nil {
			t.Fatalf("register two transforms: %v", err)
		}
		if _, err := pipeline.Before(context.Background(), invariantCall("call-1", "read", `{}`)); err != nil {
			t.Fatalf("run the before pass: %v", err)
		}
		if len(order) != 2 || order[0] != "first:read" || order[1] != "second:read" {
			t.Fatalf("transform order = %v, want the two faces in declared order", order)
		}
	})

	t.Run("a nil element refuses the pipeline and registers nothing", func(t *testing.T) {
		transform := &invariantTransform{invariantPlugin: invariantPlugin{name: "transform", port: Port{Reads: MaterialCall, Produces: ProductCall}}}
		pipeline, err := NewFlow([]Instance{transform, nil})
		if err == nil || !strings.Contains(err.Error(), "nil instance") {
			t.Fatalf("register a nil element = (%v, %v), want a refusal naming the nil instance", pipeline, err)
		}
		if len(pipeline.transforms) != 0 || len(pipeline.risks) != 0 {
			t.Fatalf("refused pipeline = %+v, want no partially registered faces", pipeline)
		}
	})

	t.Run("an undeclared port is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{invariantPlugin{name: "odd", port: Port{Reads: "odd", Produces: ProductCall}}})
		if err == nil || !strings.Contains(err.Error(), "not a declared material of the flow") {
			t.Fatalf("register an undeclared port = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "odd") {
			t.Fatalf("refusal = %q, want the element named", err)
		}
	})

	t.Run("a declared port without its face is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{invariantPlugin{name: "empty", port: Port{Reads: MaterialCall, Produces: ProductCall}}})
		if err == nil || !strings.Contains(err.Error(), "implements no narrow interface") {
			t.Fatalf("register a faceless element = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "call->call") {
			t.Fatalf("refusal = %q, want the declared port named", err)
		}
	})
}

// TestBeforePassInvariants pins the one pre-execution pass of a tool call.
// - a transform corrects a private copy, so the recorded call stays byte-identical
// - a transform that answers an error refuses the whole pass before any later face runs
// - a transform that moves the call identity is refused by name
// - the pass reports the strongest grade its risk faces rendered and leaves an empty grade empty
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestBeforePassInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("a transform corrects a private copy of the recorded call", func(t *testing.T) {
		transform := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "correct", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run: func(call *trpcmodel.ToolCall) error {
				call.Function.Name = "write"
				call.Function.Arguments[0] = '['
				*call.Index = 9
				call.ExtraFields["signature"] = "rewritten"
				return nil
			},
		}
		pipeline, err := NewFlow([]Instance{transform})
		if err != nil {
			t.Fatalf("register the transform: %v", err)
		}
		call := invariantCall("call-1", "read", `{"path":"a"}`)
		call.Index = invariantIndex(3)
		call.ExtraFields = map[string]any{"signature": "sig"}

		pass, err := pipeline.Before(ctx, call)
		if err != nil {
			t.Fatalf("run the before pass: %v", err)
		}
		if pass.Final.Function.Name != "write" || string(pass.Final.Function.Arguments) != `["path":"a"}` {
			t.Fatalf("final call = %q %s, want the corrected copy", pass.Final.Function.Name, pass.Final.Function.Arguments)
		}
		if pass.Final.ID != "call-1" {
			t.Fatalf("final call ID = %q, want the resolved identity kept", pass.Final.ID)
		}
		if call.Function.Name != "read" || string(call.Function.Arguments) != `{"path":"a"}` {
			t.Fatalf("recorded call = %q %s, want it left byte-identical", call.Function.Name, call.Function.Arguments)
		}
		if *call.Index != 3 || call.ExtraFields["signature"] != "sig" {
			t.Fatalf("recorded call index/extra = (%d, %v), want the original values kept", *call.Index, call.ExtraFields)
		}
		if &pass.Final.Function.Arguments[0] == &call.Function.Arguments[0] {
			t.Fatalf("final and recorded arguments share one backing array, want a private copy")
		}
		if pass.Final.Index == call.Index {
			t.Fatalf("final and recorded calls share one index pointer, want a private copy")
		}
	})

	t.Run("a transform error refuses the pass before any later face runs", func(t *testing.T) {
		refused := errors.New("the correction refused the call")
		var ran int
		failing := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "failing", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run:             func(*trpcmodel.ToolCall) error { return refused },
		}
		later := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "later", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run: func(*trpcmodel.ToolCall) error {
				ran++
				return nil
			},
		}
		risk := invariantRisk{invariantPlugin: invariantPlugin{name: "risk", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskCritical}
		pipeline, err := NewFlow([]Instance{failing, later, risk})
		if err != nil {
			t.Fatalf("register the refusing pipeline: %v", err)
		}
		pass, err := pipeline.Before(ctx, invariantCall("call-1", "read", `{}`))
		if !errors.Is(err, refused) {
			t.Fatalf("refused pass = %v, want %v", err, refused)
		}
		if !strings.Contains(err.Error(), "failing") {
			t.Fatalf("refusal = %q, want the refusing transform named", err)
		}
		if ran != 0 {
			t.Fatalf("later transform runs = %d, want none after the refusal", ran)
		}
		if pass.Final.ID != "" || pass.Final.Function.Name != "" || pass.Risk != "" {
			t.Fatalf("refused pass = %+v, want no call pass answered", pass)
		}
		if pass.Risk != "" {
			t.Fatalf("refused pass risk = %q, want no grade rendered", pass.Risk)
		}
	})

	t.Run("a transform that moves the call identity is refused by name", func(t *testing.T) {
		moving := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "moving", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run:             func(call *trpcmodel.ToolCall) error { call.ID = "moved"; return nil },
		}
		pipeline, err := NewFlow([]Instance{moving})
		if err != nil {
			t.Fatalf("register the moving transform: %v", err)
		}
		pass, err := pipeline.Before(ctx, invariantCall("call-1", "read", `{}`))
		if !errors.Is(err, ErrCallIdentityChanged) {
			t.Fatalf("moved identity = %v, want %v", err, ErrCallIdentityChanged)
		}
		if !strings.Contains(err.Error(), "call-1") || !strings.Contains(err.Error(), "moved") {
			t.Fatalf("refusal = %q, want both identities named", err)
		}
		if pass.Final.ID != "" || pass.Final.Function.Name != "" || pass.Risk != "" {
			t.Fatalf("refused pass = %+v, want no call pass answered", pass)
		}
	})

	t.Run("the pass reports the strongest rendered grade", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			invariantRisk{invariantPlugin: invariantPlugin{name: "low", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskLow},
			invariantRisk{invariantPlugin: invariantPlugin{name: "critical", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskCritical},
			invariantRisk{invariantPlugin: invariantPlugin{name: "medium", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskMedium},
		})
		if err != nil {
			t.Fatalf("register the risk faces: %v", err)
		}
		pass, err := pipeline.Before(ctx, invariantCall("call-1", "read", `{}`))
		if err != nil {
			t.Fatalf("run the before pass: %v", err)
		}
		if pass.Risk != RiskCritical {
			t.Fatalf("pass risk = %q, want the strongest rendered grade %q", pass.Risk, RiskCritical)
		}
		if pass.Risk == RiskLow || pass.Risk == RiskMedium {
			t.Fatalf("pass risk = %q, want a weaker grade never to win over a stronger one", pass.Risk)
		}

		unrated, err := NewFlow([]Instance{&invariantTransform{invariantPlugin: invariantPlugin{name: "only", port: Port{Reads: MaterialCall, Produces: ProductCall}}}})
		if err != nil {
			t.Fatalf("register a pipeline without risk faces: %v", err)
		}
		unratedPass, err := unrated.Before(ctx, invariantCall("call-2", "read", `{}`))
		if err != nil {
			t.Fatalf("run the unrated pass: %v", err)
		}
		if unratedPass.Risk != "" {
			t.Fatalf("unrated pass risk = %q, want the empty grade left empty", unratedPass.Risk)
		}
	})
}

// TestAfterPassInvariants pins the one post-commit pass of a committed fact.
// - observers read the committed fact before the presenters render it
// - a failed observation is a named gap beside the fact instead of a retraction or an error return
// - a dropped presentation is reported as a dropped view, never omitted in silence
// - the pass still presents the fact after an earlier observation failed
// - several presenters answer the one fact with a view each, in declared order
// - every view names its producer beside the fact it was rendered from, so the products stay traceable
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestAfterPassInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("observers run in registration order, then the presenter renders", func(t *testing.T) {
		var order []string
		observerOne := invariantObserver{invariantPlugin: invariantPlugin{name: "one", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		observerTwo := invariantObserver{invariantPlugin: invariantPlugin{name: "two", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		presenter := invariantPresenter{invariantPlugin: invariantPlugin{name: "presenter", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "rendered"}
		pipeline, err := NewFlow([]Instance{observerOne, presenter, observerTwo})
		if err != nil {
			t.Fatalf("register the after pass: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if presentation.Fact != "event-1" {
			t.Fatalf("presentation fact = %q, want %q", presentation.Fact, "event-1")
		}
		want := []string{"observe:one", "observe:two", "present:presenter"}
		if strings.Join(order, ",") != strings.Join(want, ",") {
			t.Fatalf("after pass order = %v, want observers first in registration order %v", order, want)
		}
		if len(presentation.Views) != 1 || presentation.Views[0].Plugin != "presenter" {
			t.Fatalf("views = %+v, want the one view the presenter rendered", presentation.Views)
		}
	})

	t.Run("a failed observation becomes a named gap beside the fact", func(t *testing.T) {
		var order []string
		var seen []event.EventID
		failure := errors.New("the observation failed")
		broken := invariantObserver{invariantPlugin: invariantPlugin{name: "broken", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order, err: failure}
		sound := invariantObserver{invariantPlugin: invariantPlugin{name: "sound", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		presenter := invariantPresenter{
			invariantPlugin: invariantPlugin{name: "presenter", port: Port{Reads: MaterialFact, Produces: ProductView}},
			order:           &order,
			seen:            &seen,
			text:            "rendered",
		}
		pipeline, err := NewFlow([]Instance{broken, sound, presenter})
		if err != nil {
			t.Fatalf("register the failing pass: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if len(presentation.Gaps) != 1 {
			t.Fatalf("gaps = %+v, want the one failed observation recorded", presentation.Gaps)
		}
		gap := presentation.Gaps[0]
		if gap.Plugin != "broken" || gap.Fact != "event-1" || !errors.Is(gap.Err, failure) {
			t.Fatalf("gap = %+v, want the plugin, fact and cause named", gap)
		}
		if len(presentation.Views) != 1 || presentation.Views[0].Text != "rendered" {
			t.Fatalf("views = %+v, want the presentation still rendered", presentation.Views)
		}
		if len(seen) != 1 || seen[0] != "event-1" {
			t.Fatalf("presenter saw %v, want the committed fact presented after the gap", seen)
		}
	})

	t.Run("a dropped presentation is reported as a dropped view", func(t *testing.T) {
		var order []string
		dropping := invariantPresenter{
			invariantPlugin: invariantPlugin{name: "dropping", port: Port{Reads: MaterialFact, Produces: ProductView}},
			order:           &order,
			text:            "rendered",
			dropped:         true,
		}
		pipeline, err := NewFlow([]Instance{dropping})
		if err != nil {
			t.Fatalf("register the dropping presenter: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if len(presentation.Views) != 1 {
			t.Fatalf("views = %+v, want the dropped presentation reported instead of omitted", presentation.Views)
		}
		view := presentation.Views[0]
		if !view.Dropped || view.Text != "rendered" || view.Plugin != "dropping" {
			t.Fatalf("view = %+v, want the drop reported beside its text and plugin", view)
		}
		if len(presentation.Gaps) != 0 {
			t.Fatalf("gaps = %+v, want a drop kept outside the observation gaps", presentation.Gaps)
		}
	})

	t.Run("a pipeline carrying no presenter answers no view", func(t *testing.T) {
		var order []string
		observer := invariantObserver{invariantPlugin: invariantPlugin{name: "watcher", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		pipeline, err := NewFlow([]Instance{observer})
		if err != nil {
			t.Fatalf("register an observer alone: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if presentation.Fact != "event-1" {
			t.Fatalf("presentation fact = %q, want the source fact kept beside no view", presentation.Fact)
		}
		if len(presentation.Views) != 0 {
			t.Fatalf("views = %+v, want no view when no presenter is declared", presentation.Views)
		}
		if len(order) != 1 || order[0] != "observe:watcher" {
			t.Fatalf("after pass order = %v, want the observer still run for the fact", order)
		}
	})

	t.Run("several presenters answer the one fact with a view each", func(t *testing.T) {
		var order []string
		var firstSeen, secondSeen []event.EventID
		first := invariantPresenter{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, seen: &firstSeen, text: "first view"}
		second := invariantPresenter{invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, seen: &secondSeen, text: "second view"}
		pipeline, err := NewFlow([]Instance{first, second})
		if err != nil {
			t.Fatalf("register two presenters: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if strings.Join(order, ",") != "present:first,present:second" {
			t.Fatalf("presenter order = %v, want the declared order", order)
		}
		if len(presentation.Views) != 2 {
			t.Fatalf("views = %+v, want one view per presenter", presentation.Views)
		}
		for i, want := range []struct{ plugin, text string }{{"first", "first view"}, {"second", "second view"}} {
			got := presentation.Views[i]
			if got.Plugin != want.plugin || got.Text != want.text || got.Dropped {
				t.Fatalf("view %d = %+v, want the view %q answered by %q", i, got, want.text, want.plugin)
			}
			if presentation.Fact != "event-1" {
				t.Fatalf("presentation fact = %q, want the source fact every view traces back to", presentation.Fact)
			}
		}
		if len(firstSeen) != 1 || firstSeen[0] != "event-1" || len(secondSeen) != 1 || secondSeen[0] != "event-1" {
			t.Fatalf("presenters saw (%v, %v), want both rendered the one source fact", firstSeen, secondSeen)
		}
		if len(presentation.Gaps) != 0 {
			t.Fatalf("gaps = %+v, want none", presentation.Gaps)
		}
	})
}

// TestPipelineCompositionContracts pins the composition contracts one plugin chain is held to at this boundary.
// - the faces run in the order the plugins were declared, not the order of their names
// - several presenters over the one projection field are legal: each answers its own view, in declared order
// - a name declared twice is refused by name, because a product's name must trace back to the one element that wrote it
// - a multi-plugin chain answers one Presentation per fact, and that Presentation carries one view per presenter
// - every view names its producer beside the fact it was rendered from, so each product stays traceable
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPipelineCompositionContracts(t *testing.T) {
	ctx := context.Background()

	t.Run("the declared order is the order the faces run, not the name order", func(t *testing.T) {
		var order []string
		zeta := invariantObserver{invariantPlugin: invariantPlugin{name: "zeta", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		alpha := invariantObserver{invariantPlugin: invariantPlugin{name: "alpha", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		presenter := invariantPresenter{invariantPlugin: invariantPlugin{name: "presenter", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "rendered"}
		pipeline, err := NewFlow([]Instance{zeta, alpha, presenter})
		if err != nil {
			t.Fatalf("register the declared chain: %v", err)
		}
		pipeline.After(ctx, "event-1")
		want := []string{"observe:zeta", "observe:alpha", "present:presenter"}
		if strings.Join(order, ",") != strings.Join(want, ",") {
			t.Fatalf("after pass order = %v, want the declared order %v", order, want)
		}
	})

	t.Run("several presenters over the one projection field answer their own views", func(t *testing.T) {
		var order []string
		first := invariantPresenter{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "first view"}
		second := invariantPresenter{invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "second view"}
		pipeline, err := NewFlow([]Instance{first, second})
		if err != nil {
			t.Fatalf("register two presenters: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if len(presentation.Views) != 2 || presentation.Views[0].Plugin != "first" || presentation.Views[1].Plugin != "second" {
			t.Fatalf("views = %+v, want one view per presenter in declared order", presentation.Views)
		}
		if strings.Join(order, ",") != "present:first,present:second" {
			t.Fatalf("presenter order = %v, want the declared order", order)
		}
	})

	t.Run("a name declared twice is refused by name", func(t *testing.T) {
		var order []string
		first := invariantPresenter{invariantPlugin: invariantPlugin{name: "shared", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order}
		second := invariantPresenter{invariantPlugin: invariantPlugin{name: "shared", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order}
		pipeline, err := NewFlow([]Instance{first, second})
		if err == nil {
			t.Fatalf("register one name twice = %+v, want the second element refused by name", pipeline)
		}
		if !strings.Contains(err.Error(), `"shared"`) || !strings.Contains(err.Error(), "declared twice") {
			t.Fatalf("refusal = %v, want it to name the shared name and the repeat", err)
		}
		if !strings.Contains(err.Error(), "trace back") {
			t.Fatalf("refusal = %v, want it to state the provenance it keeps", err)
		}
		if len(pipeline.presenters) != 0 || len(pipeline.observers) != 0 {
			t.Fatalf("refused pipeline = %+v, want no partially built faces", pipeline)
		}
		if len(order) != 0 {
			t.Fatalf("presenters ran %v, want a refused pipeline to present nothing", order)
		}
	})

	t.Run("a multi-plugin chain answers every fact through every presenter", func(t *testing.T) {
		var order []string
		var seenFirst, seenSecond []event.EventID
		observer := invariantObserver{invariantPlugin: invariantPlugin{name: "watcher", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		first := invariantPresenter{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, seen: &seenFirst, text: "first view"}
		second := invariantPresenter{invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, seen: &seenSecond, text: "second view"}
		pipeline, err := NewFlow([]Instance{observer, first, second})
		if err != nil {
			t.Fatalf("register the multi-plugin chain: %v", err)
		}
		facts := []event.EventID{"fact-1", "fact-2", "fact-3"}
		for _, fact := range facts {
			presentation := pipeline.After(ctx, fact)
			if presentation.Fact != fact {
				t.Fatalf("presentation fact = %q, want the source fact %q every view traces back to", presentation.Fact, fact)
			}
			if len(presentation.Views) != 2 || presentation.Views[0].Plugin != "first" || presentation.Views[1].Plugin != "second" {
				t.Fatalf("views of %s = %+v, want one view per presenter in declared order", fact, presentation.Views)
			}
			if len(presentation.Gaps) != 0 {
				t.Fatalf("gaps of %s = %+v, want none", fact, presentation.Gaps)
			}
		}
		if len(seenFirst) != len(facts) || len(seenSecond) != len(facts) {
			t.Fatalf("presenters saw (%v, %v), want each fact rendered by each presenter", seenFirst, seenSecond)
		}
		for i, fact := range facts {
			if seenFirst[i] != fact || seenSecond[i] != fact {
				t.Fatalf("presenters saw (%v, %v), want the facts in the order they were presented", seenFirst, seenSecond)
			}
		}
	})
}

// TestCompressTriggerPassInvariants pins the trigger pass of the compress-trigger port.
// - an element reporting the compress-trigger port is dispatched into that port's face set alone
// - every face answers the request the producer filed, in declared order, and the request is left as filed
// - a face that refuses refuses the pass by name and stops the later faces
// - a pipeline carrying no compress face answers nil and refuses nothing
// - an element reporting the compress port without its face is refused by name
// - a port past the declared table is refused by name, so the two further ports open no boundary
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestCompressTriggerPassInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("every face answers the filed request in declared order", func(t *testing.T) {
		var order []string
		var seen []event.EventID
		first := invariantTrigger{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialFact, Produces: ProductCompression}}, order: &order, seen: &seen}
		second := invariantTrigger{invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialFact, Produces: ProductCompression}}, order: &order}
		pipeline, err := NewFlow([]Instance{first, second})
		if err != nil {
			t.Fatalf("register two compress faces: %v", err)
		}
		if len(pipeline.triggers) != 2 || len(pipeline.transforms) != 0 || len(pipeline.injects) != 0 {
			t.Fatalf("face sets = (triggers %d, transforms %d, injects %d), want the two compress faces alone",
				len(pipeline.triggers), len(pipeline.transforms), len(pipeline.injects))
		}
		request := invariantRequest()
		if err := pipeline.TriggerCompress(ctx, request); err != nil {
			t.Fatalf("run the trigger pass: %v", err)
		}
		if strings.Join(order, ",") != "trigger:first,trigger:second" {
			t.Fatalf("trigger order = %v, want the two faces in declared order", order)
		}
		if len(seen) != 1 || seen[0] != request.ID {
			t.Fatalf("faces saw %v, want the identity the producer filed %q", seen, request.ID)
		}
		if request.Subject != "sequence-1" || request.Producer != "memory.meditation" {
			t.Fatalf("request = %+v, want the filed judgment left as it stands", request)
		}
	})

	t.Run("a refusing face refuses the pass by name", func(t *testing.T) {
		refused := errors.New("the curator is not available")
		var order []string
		failing := invariantTrigger{invariantPlugin: invariantPlugin{name: "failing", port: Port{Reads: MaterialFact, Produces: ProductCompression}}, order: &order, err: refused}
		later := invariantTrigger{invariantPlugin: invariantPlugin{name: "later", port: Port{Reads: MaterialFact, Produces: ProductCompression}}, order: &order}
		pipeline, err := NewFlow([]Instance{failing, later})
		if err != nil {
			t.Fatalf("register the refusing pipeline: %v", err)
		}
		err = pipeline.TriggerCompress(ctx, invariantRequest())
		if !errors.Is(err, refused) {
			t.Fatalf("refused trigger pass = %v, want %v", err, refused)
		}
		if !strings.Contains(err.Error(), "failing") {
			t.Fatalf("refusal = %q, want the refusing face named", err)
		}
		if strings.Join(order, ",") != "trigger:failing" {
			t.Fatalf("trigger order = %v, want no later face after the refusal", order)
		}
	})

	t.Run("a pipeline carrying no compress face refuses nothing", func(t *testing.T) {
		pipeline, err := NewFlow(nil)
		if err != nil {
			t.Fatalf("register an empty pipeline: %v", err)
		}
		if err := pipeline.TriggerCompress(ctx, invariantRequest()); err != nil {
			t.Fatalf("trigger pass over no face = %v, want the request carried as it stands", err)
		}
	})

	t.Run("an element of the compress port without its face is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{invariantPlugin{name: "empty", port: Port{Reads: MaterialFact, Produces: ProductCompression}}})
		if err == nil || !strings.Contains(err.Error(), "implements no narrow interface") {
			t.Fatalf("register a faceless element = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "fact->compression") {
			t.Fatalf("refusal = %q, want the declared port named", err)
		}
	})

	t.Run("a port past the declared table is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{invariantTrigger{invariantPlugin: invariantPlugin{name: "outward", port: Port{Reads: MaterialCall, Produces: ProductView}}}})
		if err == nil || !strings.Contains(err.Error(), "not one of the declared ports of the flow") {
			t.Fatalf("register a port past the table = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "outward") {
			t.Fatalf("refusal = %q, want the element named", err)
		}
	})
}

// TestEnvironmentInjectPassInvariants pins the injection pass of the request->environment port.
// - the face answers the assembly for the agent the request names
// - every face answers its blocks in declared order and a face answering no text contributes no block
// - one face may answer several blocks, and each block stands as its own product
// - a request that consumes no fact still answers every block from the request it was dispatched with
// - a face that refuses refuses the assembly by name instead of carrying a block it could not render
// - a pipeline carrying no environment face answers no block
// - an element reporting the environment port without its face is refused by name
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestEnvironmentInjectPassInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("the face answers the assembly it was dispatched for", func(t *testing.T) {
		var seen []EnvironmentRequest
		block := "## Environment\n- agent: agent-a"
		inject := invariantInject{invariantPlugin: invariantPlugin{name: "environment", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, seen: &seen, blocks: []string{block}}
		pipeline, err := NewFlow([]Instance{inject})
		if err != nil {
			t.Fatalf("register the environment face: %v", err)
		}
		got, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-a"})
		if err != nil {
			t.Fatalf("run the injection pass: %v", err)
		}
		if len(got) != 1 || got[0] != block {
			t.Fatalf("blocks = %q, want the one rendered environment block %q", got, block)
		}
		if len(seen) != 1 || seen[0].Agent != "agent-a" {
			t.Fatalf("faces saw %+v, want the agent the assembly runs as", seen)
		}
	})

	t.Run("several faces render in declared order beside an empty one", func(t *testing.T) {
		var order []string
		first := invariantInject{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, order: &order, blocks: []string{"first block"}}
		silent := invariantInject{invariantPlugin: invariantPlugin{name: "silent", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, order: &order}
		third := invariantInject{invariantPlugin: invariantPlugin{name: "third", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, order: &order, blocks: []string{"third block"}}
		pipeline, err := NewFlow([]Instance{first, silent, third})
		if err != nil {
			t.Fatalf("register three environment faces: %v", err)
		}
		got, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-a"})
		if err != nil {
			t.Fatalf("run the injection pass: %v", err)
		}
		if len(got) != 2 || got[0] != "first block" || got[1] != "third block" {
			t.Fatalf("blocks = %q, want the rendered blocks in declared order beside the empty face left out", got)
		}
		if strings.Join(order, ",") != "inject:first,inject:silent,inject:third" {
			t.Fatalf("injection order = %v, want the three faces in declared order", order)
		}
	})

	t.Run("one face answers several blocks as separate products", func(t *testing.T) {
		both := invariantInject{invariantPlugin: invariantPlugin{name: "both", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, blocks: []string{"first product", "second product"}}
		third := invariantInject{invariantPlugin: invariantPlugin{name: "third", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, blocks: []string{"third product"}}
		pipeline, err := NewFlow([]Instance{both, third})
		if err != nil {
			t.Fatalf("register the environment faces: %v", err)
		}
		got, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-a"})
		if err != nil {
			t.Fatalf("run the injection pass: %v", err)
		}
		if len(got) != 3 || got[0] != "first product" || got[1] != "second product" || got[2] != "third product" {
			t.Fatalf("blocks = %q, want every answer element kept as its own product in declared order", got)
		}
	})

	t.Run("an assembly that consumes no fact still answers every block from its request", func(t *testing.T) {
		var seen []EnvironmentRequest
		first := invariantInject{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, seen: &seen, blocks: []string{"first block"}}
		second := invariantInject{invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, seen: &seen, blocks: []string{"second block"}}
		pipeline, err := NewFlow([]Instance{first, second})
		if err != nil {
			t.Fatalf("register two environment faces: %v", err)
		}
		request := EnvironmentRequest{Agent: "agent-a"}
		got, err := pipeline.InjectEnvironment(ctx, request)
		if err != nil {
			t.Fatalf("run the injection pass: %v", err)
		}
		if len(got) != 2 || got[0] != "first block" || got[1] != "second block" {
			t.Fatalf("blocks = %q, want every block rendered for the one request", got)
		}
		if len(seen) != 2 || seen[0] != request || seen[1] != request {
			t.Fatalf("faces saw %+v, want the one dispatched request as each block's source", seen)
		}
	})

	t.Run("a refusing face refuses the assembly by name", func(t *testing.T) {
		refused := errors.New("the runtime is not readable")
		failing := invariantInject{invariantPlugin: invariantPlugin{name: "failing", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, blocks: []string{"partial"}, err: refused}
		pipeline, err := NewFlow([]Instance{failing})
		if err != nil {
			t.Fatalf("register the refusing pipeline: %v", err)
		}
		got, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-a"})
		if !errors.Is(err, refused) {
			t.Fatalf("refused assembly = (%q, %v), want %v", got, err, refused)
		}
		if !strings.Contains(err.Error(), "failing") {
			t.Fatalf("refusal = %q, want the refusing face named", err)
		}
		if len(got) != 0 {
			t.Fatalf("blocks of a refused assembly = %q, want no block carried", got)
		}
	})

	t.Run("a pipeline carrying no environment face answers no block", func(t *testing.T) {
		pipeline, err := NewFlow(nil)
		if err != nil {
			t.Fatalf("register an empty pipeline: %v", err)
		}
		got, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-a"})
		if err != nil || len(got) != 0 {
			t.Fatalf("injection pass over no face = (%q, %v), want no block and no refusal", got, err)
		}
	})

	t.Run("an element of the environment port without its face is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{invariantPlugin{name: "empty", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}})
		if err == nil || !strings.Contains(err.Error(), "implements no narrow interface") {
			t.Fatalf("register a faceless element = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "request->environment") {
			t.Fatalf("refusal = %q, want the declared port named", err)
		}
	})
}

// faceLog records the face lines the pipeline rendered: the DEBUG and INFO
// levels the observation writes, and no other level.
type faceLog struct {
	trpclog.Logger
	mu         sync.Mutex
	lines      []string
	panicsLeft int
}

// take records one rendered line or refuses it by panicking.
func (l *faceLog) take(level, text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.panicsLeft > 0 {
		l.panicsLeft--
		panic("the logger refused the write")
	}
	l.lines = append(l.lines, level+": "+text)
}

// Debugf records one formatted debug line.
func (l *faceLog) Debugf(format string, args ...any) {
	l.take("debug", fmt.Sprintf(format, args...))
}

// Infof records one formatted info line.
func (l *faceLog) Infof(format string, args ...any) {
	l.take("info", fmt.Sprintf(format, args...))
}

// at reports the lines taken at the named level.
func (l *faceLog) at(level string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range l.lines {
		if strings.HasPrefix(line, level+": ") {
			out = append(out, line)
		}
	}
	return out
}

// mentions reports whether one line at the level carries every fragment.
func (l *faceLog) mentions(level string, fragments ...string) bool {
	for _, line := range l.at(level) {
		holds := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				holds = false
			}
		}
		if holds {
			return true
		}
	}
	return false
}

// watchFaceLog points the face observation at the given logger for the
// duration of one check.
func watchFaceLog(tb testing.TB, logger trpclog.Logger) {
	tb.Helper()
	original := trpclog.Default
	trpclog.Default = logger
	tb.Cleanup(func() { trpclog.Default = original })
}

// TestFaceObservation checks the plugin-face observation of the bypass.
// - every executed face of both passes renders one DEBUG line carrying the port, the face, the plugin, its result and the dispatched identity
// - the line reports the result the face answered: a refusal, a risk grade, a dropped presentation
// - the two dispatch passes render one line per executed face, carrying the request and the agent they were dispatched for
// - the face lines write nothing at INFO, so the bounded stream stays with turn boundaries and model requests
// - a logger that fails does not refuse a pass or drop a presentation
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestFaceObservation(t *testing.T) {
	ctx := context.Background()

	t.Run("both passes render one line per executed face", func(t *testing.T) {
		logger := &faceLog{}
		watchFaceLog(t, logger)
		var order []string
		transform := &invariantTransform{invariantPlugin: invariantPlugin{name: "corrector", port: Port{Reads: MaterialCall, Produces: ProductCall}}}
		risk := invariantRisk{invariantPlugin: invariantPlugin{name: "grader", port: Port{Reads: MaterialCall, Produces: ProductRuling}}, grade: RiskHigh}
		observer := invariantObserver{invariantPlugin: invariantPlugin{name: "watcher", port: Port{Reads: MaterialFact, Produces: ProductObservation}}, order: &order}
		presenter := invariantPresenter{invariantPlugin: invariantPlugin{name: "renderer", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "done"}
		pipeline, err := NewFlow([]Instance{transform, risk, observer, presenter})
		if err != nil {
			t.Fatalf("register one element per port: %v", err)
		}

		if _, err := pipeline.Before(ctx, invariantCall("call-1", "echo", `{"x":1}`)); err != nil {
			t.Fatalf("run the before pass: %v", err)
		}
		pipeline.After(ctx, "fact-1")

		if !logger.mentions("debug", "plugin face", "face=transform", "plugin=corrector", "port=call->call", "result=ok", "call=call-1") {
			t.Fatalf("debug lines = %v, want the transform face line of call-1", logger.at("debug"))
		}
		if !logger.mentions("debug", "plugin face", "face=risk", "plugin=grader", "port=call->ruling", "result=high", "call=call-1") {
			t.Fatalf("debug lines = %v, want the risk face line carrying its grade", logger.at("debug"))
		}
		if !logger.mentions("debug", "plugin face", "face=observe", "plugin=watcher", "port=fact->observation", "result=ok", "fact=fact-1") {
			t.Fatalf("debug lines = %v, want the observation face line of fact-1", logger.at("debug"))
		}
		if !logger.mentions("debug", "plugin face", "face=present", "plugin=renderer", "port=fact->view", "result=rendered", "fact=fact-1") {
			t.Fatalf("debug lines = %v, want the presentation face line of fact-1", logger.at("debug"))
		}
		if lines := logger.at("debug"); len(lines) != 4 {
			t.Fatalf("debug lines = %v, want one line per executed face", lines)
		}
		if lines := logger.at("info"); len(lines) != 0 {
			t.Fatalf("info lines = %v, want the face lines kept at DEBUG", lines)
		}
	})

	t.Run("every presenter renders its own line for the one fact", func(t *testing.T) {
		logger := &faceLog{}
		watchFaceLog(t, logger)
		var order []string
		first := invariantPresenter{invariantPlugin: invariantPlugin{name: "first", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "one"}
		second := invariantPresenter{invariantPlugin: invariantPlugin{name: "second", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "two"}
		pipeline, err := NewFlow([]Instance{first, second})
		if err != nil {
			t.Fatalf("register two presenters: %v", err)
		}
		pipeline.After(ctx, "fact-1")

		if !logger.mentions("debug", "plugin face", "face=present", "plugin=first", "port=fact->view", "fact=fact-1") ||
			!logger.mentions("debug", "plugin face", "face=present", "plugin=second", "port=fact->view", "fact=fact-1") {
			t.Fatalf("debug lines = %v, want one presentation line per presenter", logger.at("debug"))
		}
		if lines := logger.at("debug"); len(lines) != 2 {
			t.Fatalf("debug lines = %v, want exactly one line per presenter", lines)
		}
	})

	t.Run("both dispatch passes render one line per executed face", func(t *testing.T) {
		logger := &faceLog{}
		watchFaceLog(t, logger)
		var order []string
		var seen []event.EventID
		trigger := invariantTrigger{invariantPlugin: invariantPlugin{name: "curator", port: Port{Reads: MaterialFact, Produces: ProductCompression}}, order: &order, seen: &seen}
		inject := invariantInject{invariantPlugin: invariantPlugin{name: "environment", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}}, blocks: []string{"clock=noon"}}
		pipeline, err := NewFlow([]Instance{trigger, inject})
		if err != nil {
			t.Fatalf("register one element per dispatch port: %v", err)
		}

		request := invariantRequest()
		if err := pipeline.TriggerCompress(ctx, request); err != nil {
			t.Fatalf("run the compress-trigger pass: %v", err)
		}
		blocks, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-a"})
		if err != nil || len(blocks) != 1 || blocks[0] != "clock=noon" {
			t.Fatalf("run the environment-inject pass = (%q, %v), want the block of the registered face", blocks, err)
		}

		if !logger.mentions("debug", "plugin face", "face=trigger", "plugin=curator", "port=fact->compression", "result=ok", "request="+string(request.ID)) {
			t.Fatalf("debug lines = %v, want the compress-trigger face line of %s", logger.at("debug"), request.ID)
		}
		if !logger.mentions("debug", "plugin face", "face=inject", "plugin=environment", "port=request->environment", "result=ok", "agent=agent-a") {
			t.Fatalf("debug lines = %v, want the environment-inject face line of agent-a", logger.at("debug"))
		}
		if lines := logger.at("debug"); len(lines) != 2 {
			t.Fatalf("debug lines = %v, want one line per executed dispatch face", lines)
		}
		if lines := logger.at("info"); len(lines) != 0 {
			t.Fatalf("info lines = %v, want the dispatch faces kept at DEBUG", lines)
		}
	})

	t.Run("a refused face and a dropped presentation keep their own result", func(t *testing.T) {
		logger := &faceLog{}
		watchFaceLog(t, logger)
		var order []string
		refusing := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "refusing", port: Port{Reads: MaterialCall, Produces: ProductCall}},
			run:             func(*trpcmodel.ToolCall) error { return errRefused },
		}
		dropping := invariantPresenter{invariantPlugin: invariantPlugin{name: "dropping", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, dropped: true}
		pipeline, err := NewFlow([]Instance{refusing, dropping})
		if err != nil {
			t.Fatalf("register the refusing transform: %v", err)
		}

		if _, err := pipeline.Before(ctx, invariantCall("call-2", "echo", `{}`)); !errors.Is(err, errRefused) {
			t.Fatalf("before pass error = %v, want the refusal of the transform reported", err)
		}
		pipeline.After(ctx, "fact-2")

		if !logger.mentions("debug", "plugin=refusing", "result=refused", "call=call-2") {
			t.Fatalf("debug lines = %v, want the refused transform reported as refused", logger.at("debug"))
		}
		if !logger.mentions("debug", "plugin=dropping", "result=dropped", "fact=fact-2") {
			t.Fatalf("debug lines = %v, want the dropped presentation reported as dropped", logger.at("debug"))
		}
	})

	t.Run("a logger that fails does not refuse a pass", func(t *testing.T) {
		watchFaceLog(t, &faceLog{panicsLeft: 2})
		var order []string
		transform := &invariantTransform{invariantPlugin: invariantPlugin{name: "corrector", port: Port{Reads: MaterialCall, Produces: ProductCall}}}
		presenter := invariantPresenter{invariantPlugin: invariantPlugin{name: "renderer", port: Port{Reads: MaterialFact, Produces: ProductView}}, order: &order, text: "done"}
		pipeline, err := NewFlow([]Instance{transform, presenter})
		if err != nil {
			t.Fatalf("register the pipeline elements: %v", err)
		}

		pass, err := pipeline.Before(ctx, invariantCall("call-3", "echo", `{}`))
		if err != nil || pass.Final.ID != "call-3" {
			t.Fatalf("before pass = (%+v, %v), want the pass of call-3 through a failing logger", pass.Final, err)
		}
		presentation := pipeline.After(ctx, "fact-3")
		if len(presentation.Views) != 1 || presentation.Views[0].Dropped {
			t.Fatalf("views = %+v, want the rendered presentation kept through a failing logger", presentation.Views)
		}
	})
}

// errRefused is the refusal one transform answers in these checks.
var errRefused = errors.New("the transform refused the call")

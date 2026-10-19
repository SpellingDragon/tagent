package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// invariantPlugin is one registration element reporting the phase it serves.
type invariantPlugin struct {
	name  string
	phase Phase
}

// Name reports the stable name of the element.
func (p invariantPlugin) Name() string { return p.name }

// Phase reports the one phase the element serves.
func (p invariantPlugin) Phase() Phase { return p.phase }

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

// invariantRiskBoth is one risk element carrying both faces of the risk phase.
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

// invariantCall builds one native tool call for the pass to transform.
func invariantCall(id, tool, args string) trpcmodel.ToolCall {
	return trpcmodel.ToolCall{ID: id, Function: trpcmodel.FunctionDefinitionParam{Name: tool, Arguments: []byte(args)}}
}

// invariantIndex returns a pointer to one streaming index.
func invariantIndex(index int) *int { return &index }

// TestPipelineRegistrationInvariants pins how one registration list becomes the phase face sets.
// - one element is dispatched into the one phase it reports, and a risk element may carry both faces of that phase
// - a nil element refuses the pipeline by name instead of being trimmed away
// - an element reporting an undeclared phase is refused by name
// - an element reporting a phase it does not implement is refused by name
// - the order inside one phase is the order the elements were declared in
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPipelineRegistrationInvariants(t *testing.T) {
	t.Run("every element is dispatched into the face set of the phase it reports", func(t *testing.T) {
		var order []string
		transform := &invariantTransform{invariantPlugin: invariantPlugin{name: "transform", phase: PhaseCallTransform}}
		risk := invariantRisk{invariantPlugin: invariantPlugin{name: "risk", phase: PhaseRiskPolicy}, grade: RiskLow}
		dual := invariantRiskBoth{invariantPlugin: invariantPlugin{name: "dual", phase: PhaseRiskPolicy}, grade: RiskHigh}
		observer := invariantObserver{invariantPlugin: invariantPlugin{name: "observer", phase: PhasePostCommitObserve}, order: &order}
		presenter := invariantPresenter{invariantPlugin: invariantPlugin{name: "presenter", phase: PhaseFinalPresentation}, order: &order}

		pipeline, err := NewPipeline([]Plugin{transform, risk, dual, observer, presenter})
		if err != nil {
			t.Fatalf("register one element per phase: %v", err)
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

	t.Run("the faces inside one phase run in registration order", func(t *testing.T) {
		var order []string
		first := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "first", phase: PhaseCallTransform},
			run: func(call *trpcmodel.ToolCall) error {
				order = append(order, "first:"+call.Function.Name)
				return nil
			},
		}
		second := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "second", phase: PhaseCallTransform},
			run: func(call *trpcmodel.ToolCall) error {
				order = append(order, "second:"+call.Function.Name)
				return nil
			},
		}
		pipeline, err := NewPipeline([]Plugin{first, second})
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
		transform := &invariantTransform{invariantPlugin: invariantPlugin{name: "transform", phase: PhaseCallTransform}}
		pipeline, err := NewPipeline([]Plugin{transform, nil})
		if err == nil || !strings.Contains(err.Error(), "nil plugin") {
			t.Fatalf("register a nil element = (%v, %v), want a refusal naming the nil plugin", pipeline, err)
		}
		if len(pipeline.transforms) != 0 || len(pipeline.risks) != 0 {
			t.Fatalf("refused pipeline = %+v, want no partially registered faces", pipeline)
		}
	})

	t.Run("an undeclared phase is refused by name", func(t *testing.T) {
		pipeline, err := NewPipeline([]Plugin{invariantPlugin{name: "odd", phase: Phase(99)}})
		if err == nil || !strings.Contains(err.Error(), "not one of the declared phases") {
			t.Fatalf("register an undeclared phase = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "odd") {
			t.Fatalf("refusal = %q, want the element named", err)
		}
	})

	t.Run("a declared phase without its face is refused by name", func(t *testing.T) {
		pipeline, err := NewPipeline([]Plugin{invariantPlugin{name: "empty", phase: PhaseCallTransform}})
		if err == nil || !strings.Contains(err.Error(), "implements no narrow interface") {
			t.Fatalf("register a faceless element = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "call-transform") {
			t.Fatalf("refusal = %q, want the served phase named", err)
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
			invariantPlugin: invariantPlugin{name: "correct", phase: PhaseCallTransform},
			run: func(call *trpcmodel.ToolCall) error {
				call.Function.Name = "write"
				call.Function.Arguments[0] = '['
				*call.Index = 9
				call.ExtraFields["signature"] = "rewritten"
				return nil
			},
		}
		pipeline, err := NewPipeline([]Plugin{transform})
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
			invariantPlugin: invariantPlugin{name: "failing", phase: PhaseCallTransform},
			run:             func(*trpcmodel.ToolCall) error { return refused },
		}
		later := &invariantTransform{
			invariantPlugin: invariantPlugin{name: "later", phase: PhaseCallTransform},
			run: func(*trpcmodel.ToolCall) error {
				ran++
				return nil
			},
		}
		risk := invariantRisk{invariantPlugin: invariantPlugin{name: "risk", phase: PhaseRiskPolicy}, grade: RiskCritical}
		pipeline, err := NewPipeline([]Plugin{failing, later, risk})
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
			invariantPlugin: invariantPlugin{name: "moving", phase: PhaseCallTransform},
			run:             func(call *trpcmodel.ToolCall) error { call.ID = "moved"; return nil },
		}
		pipeline, err := NewPipeline([]Plugin{moving})
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
		pipeline, err := NewPipeline([]Plugin{
			invariantRisk{invariantPlugin: invariantPlugin{name: "low", phase: PhaseRiskPolicy}, grade: RiskLow},
			invariantRisk{invariantPlugin: invariantPlugin{name: "critical", phase: PhaseRiskPolicy}, grade: RiskCritical},
			invariantRisk{invariantPlugin: invariantPlugin{name: "medium", phase: PhaseRiskPolicy}, grade: RiskMedium},
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

		unrated, err := NewPipeline([]Plugin{&invariantTransform{invariantPlugin: invariantPlugin{name: "only", phase: PhaseCallTransform}}})
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
// - observers read the committed fact before any presenter renders it
// - a failed observation is a named gap beside the fact instead of a retraction or an error return
// - a dropped presentation is reported as a dropped view, never omitted in silence
// - the pass still presents the fact after an earlier observation failed
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestAfterPassInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("observers run before presenters, each in registration order", func(t *testing.T) {
		var order []string
		observerOne := invariantObserver{invariantPlugin: invariantPlugin{name: "one", phase: PhasePostCommitObserve}, order: &order}
		observerTwo := invariantObserver{invariantPlugin: invariantPlugin{name: "two", phase: PhasePostCommitObserve}, order: &order}
		presenterOne := invariantPresenter{invariantPlugin: invariantPlugin{name: "one", phase: PhaseFinalPresentation}, order: &order}
		presenterTwo := invariantPresenter{invariantPlugin: invariantPlugin{name: "two", phase: PhaseFinalPresentation}, order: &order}
		pipeline, err := NewPipeline([]Plugin{observerOne, presenterOne, observerTwo, presenterTwo})
		if err != nil {
			t.Fatalf("register the after pass: %v", err)
		}
		presentation := pipeline.After(ctx, "event-1")
		if presentation.Fact != "event-1" {
			t.Fatalf("presentation fact = %q, want %q", presentation.Fact, "event-1")
		}
		want := []string{"observe:one", "observe:two", "present:one", "present:two"}
		if strings.Join(order, ",") != strings.Join(want, ",") {
			t.Fatalf("after pass order = %v, want observers first in registration order %v", order, want)
		}
	})

	t.Run("a failed observation becomes a named gap beside the fact", func(t *testing.T) {
		var order []string
		var seen []event.EventID
		failure := errors.New("the observation failed")
		broken := invariantObserver{invariantPlugin: invariantPlugin{name: "broken", phase: PhasePostCommitObserve}, order: &order, err: failure}
		sound := invariantObserver{invariantPlugin: invariantPlugin{name: "sound", phase: PhasePostCommitObserve}, order: &order}
		presenter := invariantPresenter{
			invariantPlugin: invariantPlugin{name: "presenter", phase: PhaseFinalPresentation},
			order:           &order,
			seen:            &seen,
			text:            "rendered",
		}
		pipeline, err := NewPipeline([]Plugin{broken, sound, presenter})
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
			invariantPlugin: invariantPlugin{name: "dropping", phase: PhaseFinalPresentation},
			order:           &order,
			text:            "rendered",
			dropped:         true,
		}
		pipeline, err := NewPipeline([]Plugin{dropping})
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
}

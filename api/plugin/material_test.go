package plugin

import (
	"context"
	"reflect"
	"strings"
	"testing"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"

	"github.com/SpellingDragon/tagent/api/event"
)

// flowInstance is one flow instance carrying the name and the port it declares.
type flowInstance struct {
	name string
	port Port
}

// Name reports the stable name of the instance.
func (i flowInstance) Name() string { return i.name }

// Port reports the port the instance declares.
func (i flowInstance) Port() Port { return i.port }

// flowTransform is one call->call instance answering a corrected call.
type flowTransform struct {
	flowInstance
	run func(*trpcmodel.ToolCall) error
}

// TransformCall runs the correction the instance was built with.
func (i flowTransform) TransformCall(_ context.Context, call *trpcmodel.ToolCall) error {
	return i.run(call)
}

// flowRisk is one call->ruling instance answering a fixed grade.
type flowRisk struct {
	flowInstance
	grade RiskLevel
}

// AssessRisk answers the grade the instance was built with.
func (i flowRisk) AssessRisk(context.Context, *trpcmodel.ToolCall) RiskLevel { return i.grade }

// flowObserver is one fact->observation instance recording each fact it reads.
type flowObserver struct {
	flowInstance
	order *[]string
	err   error
}

// ObserveCommit records the instance and answers the configured failure.
func (i flowObserver) ObserveCommit(_ context.Context, _ event.EventID) error {
	*i.order = append(*i.order, "observe:"+i.name)
	return i.err
}

// flowPresenter is one fact->view instance recording each fact it reads.
type flowPresenter struct {
	flowInstance
	order   *[]string
	text    string
	dropped bool
}

// Present records the instance and answers the configured view.
func (i flowPresenter) Present(_ context.Context, _ event.EventID) (string, bool) {
	*i.order = append(*i.order, "present:"+i.name)
	return i.text, i.dropped
}

// flowTrigger is one fact->compression instance recording each request it starts.
type flowTrigger struct {
	flowInstance
	order *[]string
	err   error
}

// TriggerCompress records the instance and answers the configured refusal.
func (i flowTrigger) TriggerCompress(_ context.Context, _ event.CompressRequest) error {
	*i.order = append(*i.order, "trigger:"+i.name)
	return i.err
}

// flowInject is one request->environment instance recording each assembly it serves.
type flowInject struct {
	flowInstance
	order  *[]string
	blocks []string
	err    error
}

// InjectEnvironment records the instance and answers the configured blocks.
func (i flowInject) InjectEnvironment(_ context.Context, _ EnvironmentRequest) ([]string, error) {
	*i.order = append(*i.order, "inject:"+i.name)
	return i.blocks, i.err
}

// faceShape spells the registered names of every face set in one assembled flow.
type faceShape struct {
	Transforms []string
	Risks      []string
	Governors  []string
	Injects    []string
	Observers  []string
	Presenters []string
	Triggers   []string
}

// servedNames spells the registered names of one face set in declared order.
func servedNames[T any](faces []served[T]) []string {
	if len(faces) == 0 {
		return nil
	}
	names := make([]string, 0, len(faces))
	for _, face := range faces {
		names = append(names, face.name)
	}
	return names
}

// observedNames spells the registered names of the fact->observation face set in
// declared order: the set carries a duty beside every name, so it reads its own
// shape instead of the one servedNames spells for the ports that carry none.
func observedNames(faces []observedFace) []string {
	if len(faces) == 0 {
		return nil
	}
	names := make([]string, 0, len(faces))
	for _, face := range faces {
		names = append(names, face.name)
	}
	return names
}

// shapeOf spells the face sets one assembled flow carries.
func shapeOf(p Pipeline) faceShape {
	return faceShape{
		Transforms: servedNames(p.transforms),
		Risks:      servedNames(p.risks),
		Governors:  servedNames(p.governors),
		Injects:    servedNames(p.injects),
		Observers:  observedNames(p.observers),
		Presenters: servedNames(p.presenters),
		Triggers:   servedNames(p.triggers),
	}
}

// TestPortClosedTable pins the closed port table the flow dispatches by.
// - each of the six declared ports is accepted as one port of the flow
// - an undeclared material, an undeclared product and an undeclared port pair are each refused by name
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPortClosedTable(t *testing.T) {
	declared := []Port{
		{Reads: MaterialCall, Produces: ProductCall},
		{Reads: MaterialCall, Produces: ProductRuling},
		{Reads: MaterialRequest, Produces: ProductEnvironment},
		{Reads: MaterialFact, Produces: ProductObservation},
		{Reads: MaterialFact, Produces: ProductView},
		{Reads: MaterialFact, Produces: ProductCompression},
	}
	for _, port := range declared {
		t.Run(port.String(), func(t *testing.T) {
			if err := validatePort(port); err != nil {
				t.Fatalf("declare %s: %v, want one port of the closed table", port, err)
			}
		})
	}

	t.Run("an undeclared material is refused by name", func(t *testing.T) {
		err := validatePort(Port{Reads: Material("ticket"), Produces: ProductCall})
		if err == nil || !strings.Contains(err.Error(), `"ticket" is not a declared material`) {
			t.Fatalf("declare an undeclared material = %v, want a named refusal", err)
		}
	})

	t.Run("an undeclared product is refused by name", func(t *testing.T) {
		err := validatePort(Port{Reads: MaterialCall, Produces: Product("summary")})
		if err == nil || !strings.Contains(err.Error(), `"summary" is not a declared product`) {
			t.Fatalf("declare an undeclared product = %v, want a named refusal", err)
		}
	})

	t.Run("a pair outside the closed table is refused by name", func(t *testing.T) {
		for _, port := range []Port{
			{Reads: MaterialRequest, Produces: ProductCall},
			{Reads: MaterialFact, Produces: ProductCall},
			{Reads: MaterialCall, Produces: ProductObservation},
			{Reads: MaterialRequest, Produces: ProductView},
		} {
			err := validatePort(port)
			if err == nil || !strings.Contains(err.Error(), port.String()+" is not one of the declared ports") {
				t.Fatalf("declare %s = %v, want a named refusal", port, err)
			}
		}
	})
}

// TestFlowDispatchesByMaterial pins the dispatch of every pass by the material it reads.
// - the call material runs the call->call and call->ruling faces, never a fact or request face
// - the fact material runs the observation faces before the view and compression faces, in declared order inside one port
// - the request material runs only the request->environment face
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestFlowDispatchesByMaterial(t *testing.T) {
	ctx := context.Background()
	request := invariantRequest()
	var order []string

	presenter := flowPresenter{
		flowInstance: flowInstance{name: "presenter", port: Port{Reads: MaterialFact, Produces: ProductView}},
		order:        &order,
		text:         "the view",
	}
	observer := flowObserver{
		flowInstance: flowInstance{name: "observer", port: Port{Reads: MaterialFact, Produces: ProductObservation}},
		order:        &order,
	}
	transform := flowTransform{
		flowInstance: flowInstance{name: "transformer", port: Port{Reads: MaterialCall, Produces: ProductCall}},
		run: func(call *trpcmodel.ToolCall) error {
			order = append(order, "call->call")
			call.Function.Name = "write"
			return nil
		},
	}
	risk := flowRisk{
		flowInstance: flowInstance{name: "risk-assessor", port: Port{Reads: MaterialCall, Produces: ProductRuling}},
		grade:        RiskHigh,
	}
	trigger := flowTrigger{
		flowInstance: flowInstance{name: "trigger", port: Port{Reads: MaterialFact, Produces: ProductCompression}},
		order:        &order,
	}
	inject := flowInject{
		flowInstance: flowInstance{name: "injector", port: Port{Reads: MaterialRequest, Produces: ProductEnvironment}},
		order:        &order,
		blocks:       []string{"env:one", "", "env:two"},
	}

	pipeline, err := NewFlow([]Instance{presenter, inject, risk, trigger, transform, observer})
	if err != nil {
		t.Fatalf("assemble one instance per port: %v", err)
	}
	want := faceShape{
		Transforms: []string{"transformer"},
		Risks:      []string{"risk-assessor"},
		Injects:    []string{"injector"},
		Observers:  []string{"observer"},
		Presenters: []string{"presenter"},
		Triggers:   []string{"trigger"},
	}
	if got := shapeOf(pipeline); !reflect.DeepEqual(got, want) {
		t.Fatalf("assembled faces = %+v, want every instance in its own port's set %+v", got, want)
	}

	pass, err := pipeline.Before(ctx, invariantCall("call-1", "read", `{}`))
	if err != nil {
		t.Fatalf("run the call material pass: %v", err)
	}
	if pass.Final.Function.Name != "write" || pass.Risk != RiskHigh {
		t.Fatalf("call pass = (%q, %q), want the transform and the grade of the call material", pass.Final.Function.Name, pass.Risk)
	}
	if !reflect.DeepEqual(order, []string{"call->call"}) {
		t.Fatalf("faces run for the call material = %v, want only the call->call face", order)
	}

	order = nil
	presentation := pipeline.After(ctx, "fact-1")
	if !reflect.DeepEqual(order, []string{"observe:observer", "present:presenter"}) {
		t.Fatalf("faces run for the fact material = %v, want the observation before the view", order)
	}
	if len(presentation.Views) != 1 || presentation.Views[0].Plugin != "presenter" || presentation.Views[0].Text != "the view" {
		t.Fatalf("presentation = %+v, want the one view of the fact->view face", presentation)
	}
	if len(presentation.Gaps) != 0 {
		t.Fatalf("presentation gaps = %v, want none", presentation.Gaps)
	}

	order = nil
	if err := pipeline.TriggerCompress(ctx, request); err != nil {
		t.Fatalf("run the compression material pass: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"trigger:trigger"}) {
		t.Fatalf("faces run for the compression material = %v, want only the fact->compression face", order)
	}

	order = nil
	blocks, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-1"})
	if err != nil {
		t.Fatalf("run the request material pass: %v", err)
	}
	if !reflect.DeepEqual(order, []string{"inject:injector"}) {
		t.Fatalf("faces run for the request material = %v, want only the request->environment face", order)
	}
	if !reflect.DeepEqual(blocks, []string{"env:one", "env:two"}) {
		t.Fatalf("environment blocks = %v, want every non-empty block in declared order", blocks)
	}
}

// TestNewFlowRefusals pins the named refusals of the assembled flow.
// - a nil instance refuses the flow and registers nothing
// - an undeclared material, product or port pair is refused beside the instance that declared it
// - a declared port without its narrow face is refused by name
// - a name declared twice is refused, so a product keeps the one element that wrote it
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestNewFlowRefusals(t *testing.T) {
	t.Run("a nil instance refuses the flow", func(t *testing.T) {
		var unset Instance
		pipeline, err := NewFlow([]Instance{
			flowTransform{flowInstance: flowInstance{name: "transform", port: Port{Reads: MaterialCall, Produces: ProductCall}}, run: func(*trpcmodel.ToolCall) error { return nil }},
			unset,
		})
		if err == nil || !strings.Contains(err.Error(), "nil instance") {
			t.Fatalf("assemble a nil instance = (%v, %v), want a named refusal", pipeline, err)
		}
		if len(pipeline.transforms) != 0 {
			t.Fatalf("refused flow faces = %+v, want no partially assembled faces", pipeline)
		}
	})

	t.Run("an undeclared material is refused beside its instance", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowInstance{name: "foreign", port: Port{Reads: Material("ticket"), Produces: ProductCall}},
		})
		if err == nil || !strings.Contains(err.Error(), `"ticket" is not a declared material`) {
			t.Fatalf("assemble an undeclared material = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "foreign") {
			t.Fatalf("refusal = %q, want the declaring instance named", err)
		}
	})

	t.Run("an undeclared product is refused beside its instance", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowInstance{name: "foreign", port: Port{Reads: MaterialCall, Produces: Product("summary")}},
		})
		if err == nil || !strings.Contains(err.Error(), `"summary" is not a declared product`) {
			t.Fatalf("assemble an undeclared product = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "foreign") {
			t.Fatalf("refusal = %q, want the declaring instance named", err)
		}
	})

	t.Run("a port pair outside the closed table is refused beside its instance", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowInstance{name: "crossed", port: Port{Reads: MaterialRequest, Produces: ProductCall}},
		})
		if err == nil || !strings.Contains(err.Error(), "request->call is not one of the declared ports") {
			t.Fatalf("assemble a crossed port = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "crossed") {
			t.Fatalf("refusal = %q, want the declaring instance named", err)
		}
	})

	t.Run("a declared port without its narrow face is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowInstance{name: "faceless", port: Port{Reads: MaterialCall, Produces: ProductCall}},
		})
		if err == nil || !strings.Contains(err.Error(), "implements no narrow interface") {
			t.Fatalf("assemble a faceless instance = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "call->call") {
			t.Fatalf("refusal = %q, want the declared port named", err)
		}
	})

	t.Run("a face of another port is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowPresenter{flowInstance: flowInstance{name: "misplaced", port: Port{Reads: MaterialFact, Produces: ProductObservation}}},
		})
		if err == nil || !strings.Contains(err.Error(), "implements no narrow interface") {
			t.Fatalf("assemble a misplaced face = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "fact->observation") {
			t.Fatalf("refusal = %q, want the declared port named", err)
		}
	})

	t.Run("a name declared twice is refused and keeps provenance", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowTransform{flowInstance: flowInstance{name: "same", port: Port{Reads: MaterialCall, Produces: ProductCall}}, run: func(*trpcmodel.ToolCall) error { return nil }},
			flowObserver{flowInstance: flowInstance{name: "same", port: Port{Reads: MaterialFact, Produces: ProductObservation}}},
		})
		if err == nil || !strings.Contains(err.Error(), "is declared twice") {
			t.Fatalf("assemble a duplicate name = (%v, %v), want a named refusal", pipeline, err)
		}
		if !strings.Contains(err.Error(), "trace back to the one element") {
			t.Fatalf("refusal = %q, want provenance stated", err)
		}
		if len(pipeline.transforms) != 0 || len(pipeline.observers) != 0 {
			t.Fatalf("refused flow faces = %+v, want no partially assembled faces", pipeline)
		}
	})
}

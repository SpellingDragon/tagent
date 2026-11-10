package plugin

import (
	"context"
	"errors"
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
// - a name outside the six is refused by name, so a routing nobody declared never reaches a face
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPortClosedTable(t *testing.T) {
	declared := []Port{
		PortCallTransform,
		PortCallRuling,
		PortRequestEnvironment,
		PortFactObservation,
		PortFactView,
		PortFactCompression,
	}
	for _, port := range declared {
		t.Run(port.String(), func(t *testing.T) {
			if err := validatePort(port); err != nil {
				t.Fatalf("declare %s: %v, want one port of the closed table", port, err)
			}
		})
	}

	t.Run("a name outside the closed table is refused by name", func(t *testing.T) {
		for _, port := range []Port{"ticket->call", "call->summary", "request->call", "fact->view->compression"} {
			err := validatePort(port)
			if err == nil || !strings.Contains(err.Error(), `"`+port.String()+`" is not one of the declared ports`) {
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
		flowInstance: flowInstance{name: "presenter", port: PortFactView},
		order:        &order,
		text:         "the view",
	}
	observer := flowObserver{
		flowInstance: flowInstance{name: "observer", port: PortFactObservation},
		order:        &order,
	}
	transform := flowTransform{
		flowInstance: flowInstance{name: "transformer", port: PortCallTransform},
		run: func(call *trpcmodel.ToolCall) error {
			order = append(order, "call->call")
			call.Function.Name = "write"
			return nil
		},
	}
	risk := flowRisk{
		flowInstance: flowInstance{name: "risk-assessor", port: PortCallRuling},
		grade:        RiskHigh,
	}
	trigger := flowTrigger{
		flowInstance: flowInstance{name: "trigger", port: PortFactCompression},
		order:        &order,
	}
	inject := flowInject{
		flowInstance: flowInstance{name: "injector", port: PortRequestEnvironment},
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
			flowTransform{flowInstance: flowInstance{name: "transform", port: PortCallTransform}, run: func(*trpcmodel.ToolCall) error { return nil }},
			unset,
		})
		if err == nil || !strings.Contains(err.Error(), "nil instance") {
			t.Fatalf("assemble a nil instance = (%v, %v), want a named refusal", pipeline, err)
		}
		if len(pipeline.transforms) != 0 {
			t.Fatalf("refused flow faces = %+v, want no partially assembled faces", pipeline)
		}
	})

	t.Run("a port outside the closed table is refused beside its instance", func(t *testing.T) {
		for _, port := range []Port{"ticket->call", "call->summary", "request->call"} {
			pipeline, err := NewFlow([]Instance{
				flowInstance{name: "foreign", port: port},
			})
			if err == nil || !strings.Contains(err.Error(), `"`+port.String()+`" is not one of the declared ports`) {
				t.Fatalf("assemble %s = (%v, %v), want a named refusal", port, pipeline, err)
			}
			if !strings.Contains(err.Error(), "foreign") {
				t.Fatalf("refusal = %q, want the declaring instance named", err)
			}
		}
	})

	t.Run("a declared port without its narrow face is refused by name", func(t *testing.T) {
		pipeline, err := NewFlow([]Instance{
			flowInstance{name: "faceless", port: PortCallTransform},
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
			flowPresenter{flowInstance: flowInstance{name: "misplaced", port: PortFactObservation}},
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
			flowTransform{flowInstance: flowInstance{name: "same", port: PortCallTransform}, run: func(*trpcmodel.ToolCall) error { return nil }},
			flowObserver{flowInstance: flowInstance{name: "same", port: PortFactObservation}},
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

// statesMaterial is the material one face states beside the port it serves.
type statesMaterial struct {
	material Material
}

// Material reports the declaration the face was built with.
func (d statesMaterial) Material() Material { return d.material }

// migratedTransform is a call->call face declaring its material.
type migratedTransform struct {
	*invariantTransform
	statesMaterial
}

// migratedRisk is a call->ruling face declaring its material.
type migratedRisk struct {
	invariantRisk
	statesMaterial
}

// migratedObserver is a fact->observation face declaring its material.
type migratedObserver struct {
	invariantObserver
	statesMaterial
}

// migratedPresenter is a fact->view face declaring its material.
type migratedPresenter struct {
	invariantPresenter
	statesMaterial
}

// migratedTrigger is a fact->compression face declaring its material.
type migratedTrigger struct {
	invariantTrigger
	statesMaterial
}

// migratedInject is a request->environment face declaring its material.
type migratedInject struct {
	invariantInject
	statesMaterial
}

// markerObserver is an observation face carrying the driver duty marker beside the material it declares.
type markerObserver struct {
	invariantObserver
	statesMaterial
}

// DrivesObligation marks the driver duty the face carries whether or not its declaration keeps one.
func (markerObserver) DrivesObligation() {}

// observationMaterial is the declaration one observation face states under the failure it lands.
func observationMaterial(failure Failure) Material {
	return Material{Reads: PortFactObservation, Timing: TimingPostCommit, Failure: failure}
}

// TestMaterialDispatchesByFailureStrategy pins two faces reading one material under two failure semantics.
// - the two faces read the same material at the same timing and keep the failure each declared
// - the face declaring the gap semantics leaves a reading gap the pass carries on past
// - the face declaring the obligation semantics reports the work it still owes beside the cause
// - the later faces of the pass still run, because neither declaration refuses a committed fact
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestMaterialDispatchesByFailureStrategy(t *testing.T) {
	ctx := context.Background()
	var order []string
	failure := errors.New("the reading did not land")

	gapReading := migratedObserver{
		invariantObserver: invariantObserver{
			invariantPlugin: invariantPlugin{name: "gap-reading", port: PortFactObservation},
			order:           &order,
			err:             failure,
		},
		statesMaterial: statesMaterial{material: observationMaterial(RecordGap)},
	}
	owedReading := migratedObserver{
		invariantObserver: invariantObserver{
			invariantPlugin: invariantPlugin{name: "owed-reading", port: PortFactObservation},
			order:           &order,
			err:             failure,
		},
		statesMaterial: statesMaterial{material: observationMaterial(PreserveObligation)},
	}
	presenter := invariantPresenter{
		invariantPlugin: invariantPlugin{name: "presenter", port: PortFactView},
		order:           &order,
		text:            "the view",
	}

	pipeline, err := NewFlow([]Instance{gapReading, owedReading, presenter})
	if err != nil {
		t.Fatalf("register two faces reading the same material: %v", err)
	}
	gapMaterial := pipeline.observers[0].material
	owedMaterial := pipeline.observers[1].material
	if gapMaterial.Reads != owedMaterial.Reads || gapMaterial.Timing != owedMaterial.Timing {
		t.Fatalf("declarations = (%+v, %+v), want one material read at one timing", gapMaterial, owedMaterial)
	}
	if gapMaterial.Failure != RecordGap || owedMaterial.Failure != PreserveObligation {
		t.Fatalf("declarations = (%+v, %+v), want the two failure semantics kept as declared", gapMaterial, owedMaterial)
	}

	presentation := pipeline.After(ctx, "fact-1")
	if len(presentation.Gaps) != 2 {
		t.Fatalf("gaps = %+v, want one gap per failed reading", presentation.Gaps)
	}
	gap := presentation.Gaps[0]
	if gap.Plugin != "gap-reading" || !errors.Is(gap.Err, failure) {
		t.Fatalf("first gap = %+v, want the failed reading named beside its cause", gap)
	}
	if errors.Is(gap.Err, ErrObservationObligationRetained) {
		t.Fatalf("first gap error = %v, want the reading gap its declaration named, not a retained obligation", gap.Err)
	}
	owed := presentation.Gaps[1]
	if owed.Plugin != "owed-reading" || !errors.Is(owed.Err, failure) {
		t.Fatalf("second gap = %+v, want the failed reading named beside the cause it keeps", owed)
	}
	if !errors.Is(owed.Err, ErrObservationObligationRetained) {
		t.Fatalf("second gap error = %v, want the obligation its declaration named beside the cause", owed.Err)
	}
	if !reflect.DeepEqual(order, []string{"observe:gap-reading", "observe:owed-reading", "present:presenter"}) {
		t.Fatalf("faces run = %v, want both readings answered and the later presenter dispatched", order)
	}
	if len(presentation.Views) != 1 || presentation.Views[0].Text != "the view" {
		t.Fatalf("views = %+v, want the fact still presented after both readings failed", presentation.Views)
	}
}

// TestPortNameInfersNoStrategy pins that no pass reads a failure semantics out of the port a face serves.
// - a face carrying the driver duty marker is answered by the gap semantics its declaration names
// - a face carrying no marker is answered by the obligation semantics its declaration names
// - a face whose port contradicts the material it declares is refused by name, never filed by one of the two
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestPortNameInfersNoStrategy(t *testing.T) {
	ctx := context.Background()

	t.Run("the marker does not outrank the declared gap semantics", func(t *testing.T) {
		var order []string
		failure := errors.New("the reading did not land")
		marked := markerObserver{
			invariantObserver: invariantObserver{
				invariantPlugin: invariantPlugin{name: "marked", port: PortFactObservation},
				order:           &order,
				err:             failure,
			},
			statesMaterial: statesMaterial{material: observationMaterial(RecordGap)},
		}
		pipeline, err := NewFlow([]Instance{marked})
		if err != nil {
			t.Fatalf("register the marked face: %v", err)
		}
		gaps := pipeline.After(ctx, "fact-1").Gaps
		if len(gaps) != 1 {
			t.Fatalf("gaps = %+v, want the failed reading recorded", gaps)
		}
		if errors.Is(gaps[0].Err, ErrObservationObligationRetained) {
			t.Fatalf("gap error = %v, want a reading gap: the face declared the gap semantics and owes nothing", gaps[0].Err)
		}
	})

	t.Run("the missing marker does not outweigh the declared obligation semantics", func(t *testing.T) {
		var order []string
		failure := errors.New("the settle fact could not be read")
		unmarked := migratedObserver{
			invariantObserver: invariantObserver{
				invariantPlugin: invariantPlugin{name: "unmarked", port: PortFactObservation},
				order:           &order,
				err:             failure,
			},
			statesMaterial: statesMaterial{material: observationMaterial(PreserveObligation)},
		}
		pipeline, err := NewFlow([]Instance{unmarked})
		if err != nil {
			t.Fatalf("register the unmarked face: %v", err)
		}
		gaps := pipeline.After(ctx, "fact-1").Gaps
		if len(gaps) != 1 {
			t.Fatalf("gaps = %+v, want the failed reading recorded", gaps)
		}
		if !errors.Is(gaps[0].Err, ErrObservationObligationRetained) {
			t.Fatalf("gap error = %v, want the retained obligation the face declared", gaps[0].Err)
		}
		if !errors.Is(gaps[0].Err, failure) {
			t.Fatalf("gap error = %v, want the cause still readable through it", gaps[0].Err)
		}
	})

	t.Run("a port contradicting the declared material is refused by name", func(t *testing.T) {
		var order []string
		contradicting := migratedObserver{
			invariantObserver: invariantObserver{
				invariantPlugin: invariantPlugin{name: "contradicting", port: PortFactObservation},
				order:           &order,
			},
			statesMaterial: statesMaterial{material: Material{Reads: PortFactView, Timing: TimingPostCommit, Failure: ReportLoss}},
		}
		_, err := NewFlow([]Instance{contradicting})
		if err == nil || !strings.Contains(err.Error(), "declares the port fact->observation but its material reads fact->view") {
			t.Fatalf("register the contradicting face = %v, want a named refusal of the two faces it declared", err)
		}
	})
}

// TestSixPortFailureBehaviorUnchangedAfterMigration pins that every face of the six declared ports answers what it answered before.
// - for each port, the material a port-only face reads is the material a migrated face of that port declares
// - for each port, the pass answers the same outcome for the port-only face and for the migrated face
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestSixPortFailureBehaviorUnchangedAfterMigration(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("the face failed")

	rows := []struct {
		name     string
		reads    Port
		legacy   Instance
		migrated Instance
	}{
		{
			name:   "call->call",
			reads:  PortCallTransform,
			legacy: &invariantTransform{invariantPlugin: invariantPlugin{name: "face", port: PortCallTransform}, run: func(*trpcmodel.ToolCall) error { return failure }},
			migrated: &migratedTransform{
				invariantTransform: &invariantTransform{invariantPlugin: invariantPlugin{name: "face", port: PortCallTransform}, run: func(*trpcmodel.ToolCall) error { return failure }},
				statesMaterial:     statesMaterial{material: Material{Reads: PortCallTransform, Timing: TimingPreSideEffect, Failure: FailClosed}},
			},
		},
		{
			name:     "call->ruling",
			reads:    PortCallRuling,
			legacy:   invariantRisk{invariantPlugin: invariantPlugin{name: "face", port: PortCallRuling}, grade: RiskHigh},
			migrated: migratedRisk{invariantRisk: invariantRisk{invariantPlugin: invariantPlugin{name: "face", port: PortCallRuling}, grade: RiskHigh}, statesMaterial: statesMaterial{material: Material{Reads: PortCallRuling, Timing: TimingPreSideEffect, Failure: FailClosed}}},
		},
		{
			name:   "request->environment",
			reads:  PortRequestEnvironment,
			legacy: invariantInject{invariantPlugin: invariantPlugin{name: "face", port: PortRequestEnvironment}, blocks: []string{"env"}, err: failure},
			migrated: migratedInject{
				invariantInject: invariantInject{invariantPlugin: invariantPlugin{name: "face", port: PortRequestEnvironment}, blocks: []string{"env"}, err: failure},
				statesMaterial:  statesMaterial{material: Material{Reads: PortRequestEnvironment, Timing: TimingRead, Failure: FailClosed}},
			},
		},
		{
			name:   "fact->observation observer-only",
			reads:  PortFactObservation,
			legacy: &invariantObserver{invariantPlugin: invariantPlugin{name: "face", port: PortFactObservation}, order: newOrder(), err: failure},
			migrated: migratedObserver{
				invariantObserver: invariantObserver{invariantPlugin: invariantPlugin{name: "face", port: PortFactObservation}, order: newOrder(), err: failure},
				statesMaterial:    statesMaterial{material: observationMaterial(RecordGap)},
			},
		},
		{
			name:     "fact->observation driver",
			reads:    PortFactObservation,
			legacy:   &layeredDriver{layeredReading: layeredReading{invariantPlugin: invariantPlugin{name: "face", port: PortFactObservation}, seen: newSeen(), err: failure}, owed: new(bool)},
			migrated: &markerObserver{invariantObserver: invariantObserver{invariantPlugin: invariantPlugin{name: "face", port: PortFactObservation}, order: newOrder(), err: failure}, statesMaterial: statesMaterial{material: observationMaterial(PreserveObligation)}},
		},
		{
			name:   "fact->view",
			reads:  PortFactView,
			legacy: invariantPresenter{invariantPlugin: invariantPlugin{name: "face", port: PortFactView}, order: newOrder(), text: "the view", dropped: true},
			migrated: migratedPresenter{
				invariantPresenter: invariantPresenter{invariantPlugin: invariantPlugin{name: "face", port: PortFactView}, order: newOrder(), text: "the view", dropped: true},
				statesMaterial:     statesMaterial{material: Material{Reads: PortFactView, Timing: TimingPostCommit, Failure: ReportLoss}},
			},
		},
		{
			name:   "fact->compression",
			reads:  PortFactCompression,
			legacy: invariantTrigger{invariantPlugin: invariantPlugin{name: "face", port: PortFactCompression}, err: failure},
			migrated: migratedTrigger{
				invariantTrigger: invariantTrigger{invariantPlugin: invariantPlugin{name: "face", port: PortFactCompression}, err: failure},
				statesMaterial:   statesMaterial{material: Material{Reads: PortFactCompression, Timing: TimingPostCommit, Failure: PreserveObligation}},
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if legacy, migrated := materialOf(row.legacy), materialOf(row.migrated); legacy != migrated {
				t.Fatalf("material = (%+v, %+v), want the migrated declaration to be the material the port already read", legacy, migrated)
			}
			legacyOutcome := passOutcome(t, newFlow(t, row.legacy), row.reads, ctx)
			migratedOutcome := passOutcome(t, newFlow(t, row.migrated), row.reads, ctx)
			if legacyOutcome != migratedOutcome {
				t.Fatalf("outcome = (%q, %q), want the same answer before and after the migration", legacyOutcome, migratedOutcome)
			}
		})
	}
}

// TestMigrationSilentlyRejudgesNothing pins the answers a migration must not change.
// - a failed reading never refuses the fact it read: the presentation still carries the fact
// - a refused compression still refuses: the pass answers the failure and reports nothing compressed
// - a failed call-side face refuses the pass instead of degrading to a gap
// - a failed environment face refuses the assembly instead of carrying a block it could not render
// - a declaration its port's pass cannot run or land is refused at load, by name
// 契约: docs/wiki/plugin/plugin-architecture.md#overview
func TestMigrationSilentlyRejudgesNothing(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("the face failed")

	t.Run("a failed reading never refuses the fact it read", func(t *testing.T) {
		owed := true
		driver := &layeredDriver{
			layeredReading: layeredReading{invariantPlugin: invariantPlugin{name: "driver", port: PortFactObservation}, seen: newSeen(), err: failure},
			owed:           &owed,
		}
		presentation := newFlow(t, driver).After(ctx, "fact-1")
		if presentation.Fact != "fact-1" {
			t.Fatalf("presentation fact = %q, want the committed fact the pass read", presentation.Fact)
		}
		if !owed {
			t.Fatalf("the obligation was cleared by the failed reading: it stays in the account that owes it")
		}
	})

	t.Run("a refused compression still refuses", func(t *testing.T) {
		trigger := migratedTrigger{
			invariantTrigger: invariantTrigger{invariantPlugin: invariantPlugin{name: "trigger", port: PortFactCompression}, err: failure},
			statesMaterial:   statesMaterial{material: Material{Reads: PortFactCompression, Timing: TimingPostCommit, Failure: PreserveObligation}},
		}
		err := newFlow(t, trigger).TriggerCompress(ctx, invariantRequest())
		if err == nil || !strings.Contains(err.Error(), "compress trigger trigger") {
			t.Fatalf("compress pass = %v, want the refused request named: the request stays pending", err)
		}
		if !errors.Is(err, failure) {
			t.Fatalf("compress pass = %v, want the cause readable through the refusal", err)
		}
	})

	t.Run("a failed call-side face refuses the pass", func(t *testing.T) {
		transform := &migratedTransform{
			invariantTransform: &invariantTransform{invariantPlugin: invariantPlugin{name: "transform", port: PortCallTransform}, run: func(*trpcmodel.ToolCall) error { return failure }},
			statesMaterial:     statesMaterial{material: Material{Reads: PortCallTransform, Timing: TimingPreSideEffect, Failure: FailClosed}},
		}
		pass, err := newFlow(t, transform).Before(ctx, invariantCall("call-1", "write", `{}`))
		if err == nil || !errors.Is(err, failure) {
			t.Fatalf("call pass = (%+v, %v), want the refusal carrying the cause", pass, err)
		}
		if pass.Final.Function.Name != "" || pass.Risk != "" {
			t.Fatalf("call pass = %+v, want no final call and no grade: the call never runs", pass)
		}
	})

	t.Run("a failed environment face refuses the assembly", func(t *testing.T) {
		inject := migratedInject{
			invariantInject: invariantInject{invariantPlugin: invariantPlugin{name: "inject", port: PortRequestEnvironment}, blocks: []string{"env"}, err: failure},
			statesMaterial:  statesMaterial{material: Material{Reads: PortRequestEnvironment, Timing: TimingRead, Failure: FailClosed}},
		}
		blocks, err := newFlow(t, inject).InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-1"})
		if err == nil || !errors.Is(err, failure) {
			t.Fatalf("environment pass = (%v, %v), want the refusal carrying the cause", blocks, err)
		}
		if blocks != nil {
			t.Fatalf("environment blocks = %v, want none: no assembly is built over a block that failed", blocks)
		}
	})

	t.Run("a declaration its port's pass cannot run or land is refused at load", func(t *testing.T) {
		declarations := []struct {
			name     string
			material Material
			want     string
		}{
			{name: "a pre-side-effect face declaring a gap", material: Material{Reads: PortCallTransform, Timing: TimingPreSideEffect, Failure: RecordGap}, want: "call->call lands [fail-closed], not record-gap"},
			{name: "a pre-side-effect face declared post-commit", material: Material{Reads: PortCallRuling, Timing: TimingPostCommit, Failure: FailClosed}, want: "call->ruling runs at the pre-side-effect timing, not post-commit"},
			{name: "a read-time face declared post-commit", material: Material{Reads: PortRequestEnvironment, Timing: TimingPostCommit, Failure: FailClosed}, want: "request->environment runs at the read timing, not post-commit"},
			{name: "an observation face declared pre-side-effect", material: Material{Reads: PortFactObservation, Timing: TimingPreSideEffect, Failure: RecordGap}, want: "fact->observation runs at the post-commit timing, not pre-side-effect"},
			{name: "a view face declaring a gap", material: Material{Reads: PortFactView, Timing: TimingPostCommit, Failure: RecordGap}, want: "fact->view lands [report-loss], not record-gap"},
			{name: "a compression face declared closed", material: Material{Reads: PortFactCompression, Timing: TimingPostCommit, Failure: FailClosed}, want: "fact->compression lands [preserve-obligation], not fail-closed"},
		}
		for _, declaration := range declarations {
			t.Run(declaration.name, func(t *testing.T) {
				face := migratedObserver{
					invariantObserver: invariantObserver{invariantPlugin: invariantPlugin{name: "declared", port: declaration.material.Reads}, order: newOrder()},
					statesMaterial:    statesMaterial{material: declaration.material},
				}
				_, err := NewFlow([]Instance{face})
				if err == nil || !strings.Contains(err.Error(), declaration.want) {
					t.Fatalf("register the declaration = %v, want a named refusal containing %q", err, declaration.want)
				}
			})
		}
	})
}

// newOrder returns one empty record of the faces a pass dispatched.
func newOrder() *[]string {
	order := make([]string, 0, 2)
	return &order
}

// newSeen returns one empty record of the facts a reading was dispatched for.
func newSeen() *[]event.EventID {
	seen := make([]event.EventID, 0, 1)
	return &seen
}

// newFlow assembles one flow holding the faces it was handed.
func newFlow(t *testing.T, instances ...Instance) Pipeline {
	t.Helper()
	pipeline, err := NewFlow(instances)
	if err != nil {
		t.Fatalf("assemble the flow: %v", err)
	}
	return pipeline
}

// passOutcome runs the one pass the material belongs to and spells its answer.
func passOutcome(t *testing.T, pipeline Pipeline, reads Port, ctx context.Context) string {
	t.Helper()
	switch reads {
	case PortCallTransform:
		if _, err := pipeline.Before(ctx, invariantCall("call-1", "write", `{}`)); err != nil {
			return "refused: " + namedRefusal(t, err)
		}
		return "admitted"
	case PortCallRuling:
		pass, err := pipeline.Before(ctx, invariantCall("call-1", "read", `{}`))
		if err != nil {
			return "refused: " + namedRefusal(t, err)
		}
		return "grade: " + string(pass.Risk)
	case PortRequestEnvironment:
		blocks, err := pipeline.InjectEnvironment(ctx, EnvironmentRequest{Agent: "agent-1"})
		if err != nil {
			return "refused: " + namedRefusal(t, err)
		}
		return "injected: " + strings.Join(blocks, ",")
	case PortFactObservation:
		presentation := pipeline.After(ctx, "fact-1")
		if len(presentation.Gaps) == 0 {
			return "read"
		}
		if errors.Is(presentation.Gaps[0].Err, ErrObservationObligationRetained) {
			return "obligation: " + presentation.Gaps[0].Plugin
		}
		return "gap: " + presentation.Gaps[0].Plugin
	case PortFactView:
		presentation := pipeline.After(ctx, "fact-1")
		if len(presentation.Views) != 1 {
			return "no view"
		}
		return "view: " + presentation.Views[0].Plugin + ":" + facePresentation(presentation.Views[0].Dropped)
	case PortFactCompression:
		if err := pipeline.TriggerCompress(ctx, invariantRequest()); err != nil {
			return "refused: " + namedRefusal(t, err)
		}
		return "started"
	default:
		t.Fatalf("reads = %s, want one of the six declared ports", reads)
		return ""
	}
}

// namedRefusal spells which face a refusal names, so two refusals compare by provenance instead of text.
func namedRefusal(t *testing.T, err error) string {
	t.Helper()
	switch {
	case strings.Contains(err.Error(), "transform "):
		return "transform"
	case strings.Contains(err.Error(), "environment inject "):
		return "environment inject"
	case strings.Contains(err.Error(), "compress trigger "):
		return "compress trigger"
	default:
		t.Fatalf("refusal = %v, want the face it refused named", err)
		return ""
	}
}

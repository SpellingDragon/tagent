package plugin

import (
	"errors"
	"fmt"
)

// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Port names one declared port of the material flow. Exactly six exist, each
// spelling the routing it stands for — the material the face reads beside the
// product it answers — and a face serves one of them by name. Nothing routes by
// a material or a product read on its own, because no pass ever walks the
// materials as a set: a port constant is the whole declaration.
//
// The redline list (2 rules) and the one candidate note:
// MUST serve one of the six declared ports; a name outside the set is refused rather than matched to a port.
// MUST implement the narrow interface of the declared port.
// - `route` (event->ownership: one committed event read as the decision over whom it belongs to) is the candidate for a seventh port, recorded as a note and holding no slot: no constant, no narrow interface and no dispatch beside the six, so a face declaring it is refused by name like any undeclared name. It opens only when the work-relation core it would decide on exists and a real consumer declares it; until then the table stays closed at six.
type Port string

const (
	// PortCallTransform is the port a call transform serves: it reads the call a
	// tool is about to run and answers the corrected call of the same identity.
	PortCallTransform Port = "call->call"

	// PortCallRuling is the port the pre-execution ruling serves: it reads the
	// call and answers the ruling over it, the risk grade beside the decision.
	PortCallRuling Port = "call->ruling"

	// PortRequestEnvironment is the port environment injection serves: it reads
	// one request assembly and answers the environment blocks of it.
	PortRequestEnvironment Port = "request->environment"

	// PortFactObservation is the port a post-commit observer serves: it reads one
	// committed fact and answers a read-only observation that never rewrites it.
	PortFactObservation Port = "fact->observation"

	// PortFactView is the port a final presenter serves: it reads one committed
	// fact and answers the views rendered from it.
	PortFactView Port = "fact->view"

	// PortFactCompression is the port a compress trigger serves: it reads one
	// committed fact and answers the compression a filed request asks for.
	PortFactCompression Port = "fact->compression"
)

// String spells one port as the routing it stands for.
func (p Port) String() string { return string(p) }

// validatePort refuses a port the closed table does not declare, by name. The
// table holds the six ports above, so a face is dispatched by the one narrow
// interface its port names, never by a classification of the parts the name
// spells.
func validatePort(port Port) error {
	switch port {
	case PortCallTransform, PortCallRuling, PortRequestEnvironment, PortFactObservation, PortFactView, PortFactCompression:
		return nil
	default:
		return fmt.Errorf("plugin: %q is not one of the declared ports of the flow", string(port))
	}
}

// Instance is one instance of the material flow. The drive that runs one
// decision, the tick that runs one reflection and the assembler that builds one
// request are built-in instances; a plugin is an instance the composition root
// mounts. What an instance is differs only in who mounts it: the flow dispatches
// every instance by the material it declares, read from the instance itself at
// assembly, and the port a face answers is the compatibility read beside that
// declaration while the named ports migrate.
//
// The redline list (3 rules):
// MUST declare the one port it serves and implement that port's narrow interface.
// MUST NOT declare a port it does not implement.
// MUST NOT declare a material its port's pass cannot run or land.
type Instance interface {
	// Name returns the stable name of this instance.
	Name() string

	// Port reports the port this instance serves: the compatibility read of the
	// material the face declares, kept until every face declares one.
	Port() Port
}

// Timing names when a face consumes the material it reads. It is a strategy
// field of the declaration, orthogonal to the failure semantics: the same
// material may be read before a side effect, beside a commit, or while one
// request is assembled, and each timing answers a failure its own way.
type Timing string

const (
	// TimingPreSideEffect is the timing of a face that runs before the side effect
	// it guards: its answer is part of the pass that decides whether that side
	// effect runs, so nothing it guards has run and nothing of it is owed.
	TimingPreSideEffect Timing = "pre-side-effect"

	// TimingPostCommit is the timing of a face that runs beside a fact whose
	// canonical commit already happened: the pass it runs in answers no error of
	// its own and never holds the commit back.
	TimingPostCommit Timing = "post-commit"

	// TimingRead is the timing of a face that runs while one request assembly is
	// built: an idempotent derivation with no durable effect, recomputable from
	// the material it reads.
	TimingRead Timing = "read"
)

// Failure names what one failed face leaves behind. It is the second strategy
// field of the declaration: a face of one timing may answer a failure in more
// than one way, and the declaration states which one it answers.
type Failure string

const (
	// FailClosed refuses the pass: the work the face guards neither runs nor is
	// owed, and no later face of that pass runs.
	FailClosed Failure = "fail-closed"

	// RecordGap records the failure as a named gap beside what already stands and
	// carries on with the later faces: the committed fact keeps standing.
	RecordGap Failure = "record-gap"

	// PreserveObligation keeps the work owed in the account of the face that owes
	// it, reported beside ErrObservationObligationRetained instead of as a missing
	// reading, and neither retried nor cleared by the pass.
	PreserveObligation Failure = "preserve-obligation"

	// ReportLoss reports the product as dropped: the fact it was rendered from
	// keeps standing, the drop is reported instead of leaving an empty view, and
	// nothing is retried.
	ReportLoss Failure = "report-loss"
)

// Material is the one declaration a face makes: the material it reads, when it
// reads it, and what a failure of that face leaves behind. The port name carries
// the routing alone; the timing and the failure are strategy fields, so a face
// states its own semantics and the flow dispatches it by that statement.
type Material struct {
	// Reads is the material this face consumes, spelled as the routing the port
	// constants stand for: the material read beside the product answered.
	Reads Port

	// Timing is when this face consumes the material it reads.
	Timing Timing

	// Failure is what a failure of this face leaves behind.
	Failure Failure
}

// materialDeclarer is the declaration a face may carry beside the port it serves.
// The declaration is read from the instance itself at assembly — the same way the
// observation duty is read — so a face states its timing and its failure once and
// no pass infers either from a name.
//
// The redline list (2 rules):
// MUST declare the timing and the failure its own pass runs and lands.
// MUST declare the reads of the port the instance serves.
type materialDeclarer interface {
	// Material reports the material this face declares.
	Material() Material
}

// admittedMaterial is the material one declared port's pass runs at: the timing
// that pass runs at beside every failure semantics it can land.
type admittedMaterial struct {
	timing   Timing
	failures []Failure
}

// admitted is the frozen mapping of the six declared ports onto the timing their
// pass runs at and the failures that pass can land. A pass answering its caller
// an error can only refuse; a pass running beside a commit answers no error of
// its own, so its failure lands as the gap, the retained obligation or the
// dropped product its face declares. The observation port is the one port
// carrying two duties, and the order here is the compatibility read: a face
// declaring no material reads the first failure, and a face carrying the driver
// duty reads the second.
var admitted = map[Port]admittedMaterial{
	PortCallTransform:      {timing: TimingPreSideEffect, failures: []Failure{FailClosed}},
	PortCallRuling:         {timing: TimingPreSideEffect, failures: []Failure{FailClosed}},
	PortRequestEnvironment: {timing: TimingRead, failures: []Failure{FailClosed}},
	PortFactObservation:    {timing: TimingPostCommit, failures: []Failure{RecordGap, PreserveObligation}},
	PortFactView:           {timing: TimingPostCommit, failures: []Failure{ReportLoss}},
	PortFactCompression:    {timing: TimingPostCommit, failures: []Failure{PreserveObligation}},
}

// materialOf reports the material one instance declares: a face declaring
// Material() states its own timing and failure and the passes answer by that
// statement, while a face declaring only its port reads the compatibility mapping
// of that port until it migrates.
func materialOf(instance Instance) Material {
	if declarer, ok := instance.(materialDeclarer); ok {
		return declarer.Material()
	}
	return compatMaterialOf(instance)
}

// compatMaterialOf reads the material of a face that declares none: the port it
// serves already spells the material it reads beside the product it answers, so
// the mapping answers the timing that port's pass runs at and the failure that
// pass lands. A face whose failed reading leaves work owed carries the driver
// duty, so it answers the obligation semantics and every other observation the
// gap semantics.
func compatMaterialOf(instance Instance) Material {
	port := instance.Port()
	rule, declared := admitted[port]
	if !declared {
		return Material{Reads: port}
	}
	material := Material{Reads: port, Timing: rule.timing, Failure: rule.failures[0]}
	if dutyOfObserved(instance) && len(rule.failures) > 1 {
		material.Failure = rule.failures[1]
	}
	return material
}

// validateMaterial refuses a declared material its port's pass cannot run or
// land, by naming the port it reads: a pass that answered its caller an error
// cannot be answered a gap of its own, and a pass running beside a commit cannot
// refuse a fact already committed. The check keeps a declaration from being
// reshaped by the port name in silence.
func validateMaterial(material Material) error {
	if err := validatePort(material.Reads); err != nil {
		return err
	}
	rule := admitted[material.Reads]
	if material.Timing != rule.timing {
		return fmt.Errorf("%s runs at the %s timing, not %s", material.Reads, rule.timing, material.Timing)
	}
	for _, failure := range rule.failures {
		if material.Failure == failure {
			return nil
		}
	}
	return fmt.Errorf("%s lands %v, not %s", material.Reads, rule.failures, material.Failure)
}

// NewFlow assembles the material flow out of its instances, in the declared
// order. A nil instance, a material the closed table of ports does not declare, a
// material its port's pass cannot run or land, a port contradicting the material
// the instance declares, an instance that implements no narrow interface of the
// port it declares, and a name declared twice each refuse the flow by name instead
// of being trimmed in silence. The name refusal keeps provenance: a product's name
// must trace back to the one element that wrote it.
func NewFlow(instances []Instance) (Pipeline, error) {
	declarations := make([]declaration, 0, len(instances))
	for _, instance := range instances {
		if instance == nil {
			return Pipeline{}, errors.New("plugin: the flow holds a nil instance")
		}
		material := materialOf(instance)
		if err := validateMaterial(material); err != nil {
			return Pipeline{}, fmt.Errorf("plugin: %q declares %v", instance.Name(), err)
		}
		declarations = append(declarations, declaration{name: instance.Name(), port: instance.Port(), material: material, impl: instance})
	}
	return assemble(declarations)
}

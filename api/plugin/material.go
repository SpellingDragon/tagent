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
// The redline list (2 rules):
// MUST serve one of the six declared ports; a name outside the set is refused rather than matched to a port.
// MUST implement the narrow interface of the declared port.
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
// every instance by the port it declares.
//
// The redline list (3 rules):
// MUST declare the one port it serves and implement that port's narrow interface.
// MUST NOT declare a port it does not implement.
// MUST NOT serve two ports: one instance is one port, and the ruling port may carry both of its faces.
type Instance interface {
	// Name returns the stable name of this instance.
	Name() string

	// Port reports the port this instance serves.
	Port() Port
}

// NewFlow assembles the material flow out of its instances, in the declared
// order. A nil instance, a port the closed table does not declare, an instance
// that implements no narrow interface of its port, and a name declared twice
// each refuse the flow by name instead of being trimmed in silence. The name
// refusal keeps provenance: a product's name must trace back to the one element
// that wrote it.
func NewFlow(instances []Instance) (Pipeline, error) {
	declarations := make([]declaration, 0, len(instances))
	for _, instance := range instances {
		if instance == nil {
			return Pipeline{}, errors.New("plugin: the flow holds a nil instance")
		}
		declarations = append(declarations, declaration{name: instance.Name(), port: instance.Port(), impl: instance})
	}
	return assemble(declarations)
}

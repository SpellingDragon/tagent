package plugin

import (
	"errors"
	"fmt"
)

// 契约: docs/wiki/plugin/plugin-architecture.md#overview
//
// Material names one kind of material the flow moves between its faces: the call
// a tool is about to run, the request an assembly builds, and the fact a commit
// wrote. A face reads exactly one material and never rewrites it in place.
type Material string

const (
	// MaterialCall is the call material: one tool call before its side effect.
	MaterialCall Material = "call"

	// MaterialRequest is the request material: one request assembly.
	MaterialRequest Material = "request"

	// MaterialFact is the fact material: one committed fact.
	MaterialFact Material = "fact"
)

// Product names what one face answers out of the material it reads: the
// corrected call, the ruling over a call, the observation of a fact, the view of
// a fact, the compression a filed request asks for and the environment blocks of
// a request.
type Product string

const (
	// ProductCall is the corrected call: the same identity, a corrected name and
	// corrected arguments.
	ProductCall Product = "call"

	// ProductRuling is the ruling over one call: the grade of the call beside the
	// one pre-execution decision.
	ProductRuling Product = "ruling"

	// ProductObservation is the observation of one fact: a read-only reading that
	// never rewrites the fact.
	ProductObservation Product = "observation"

	// ProductView is the presentation of one fact: one view per presenter, traced
	// to the fact it was rendered from.
	ProductView Product = "view"

	// ProductCompression is the compression one filed request asks for: the chain
	// the request drives, never the curation itself.
	ProductCompression Product = "compression"

	// ProductEnvironment is the environment material one request assembly carries:
	// one block per product, each block traced to the request.
	ProductEnvironment Product = "environment"
)

// Port is the declaration of one flow instance: the material it reads and the
// product it answers. The pair is the whole routing of the flow — the narrow
// interface the instance is held to and the pass that dispatches it are both
// readings of it.
//
// The redline list (2 rules):
// MUST declare exactly one read material and one product; an element serving two ports is two instances.
// MUST implement the narrow interface of the declared port.
type Port struct {
	// Reads is the material this instance reads.
	Reads Material

	// Produces is the product this instance answers.
	Produces Product
}

// String spells one port as the material it reads beside the product it answers.
func (p Port) String() string { return string(p.Reads) + "->" + string(p.Produces) }

// validatePort refuses a port the closed table of the flow does not declare, by
// name. The table declares six ports — call->call, call->ruling,
// request->environment, fact->observation, fact->view and fact->compression — so
// a face is dispatched by the one narrow interface its port names, never by a
// classification of the port.
//
// A material the table does not declare, a product it does not declare, and a
// pair no port serves are each refused by name instead of falling back to a
// declared port.
func validatePort(port Port) error {
	switch port.Reads {
	case MaterialCall, MaterialRequest, MaterialFact:
	default:
		return fmt.Errorf("plugin: %q is not a declared material of the flow", string(port.Reads))
	}
	switch port.Produces {
	case ProductCall, ProductRuling, ProductObservation, ProductView, ProductCompression, ProductEnvironment:
	default:
		return fmt.Errorf("plugin: %q is not a declared product of the flow", string(port.Produces))
	}
	switch port {
	case Port{Reads: MaterialCall, Produces: ProductCall},
		Port{Reads: MaterialCall, Produces: ProductRuling},
		Port{Reads: MaterialRequest, Produces: ProductEnvironment},
		Port{Reads: MaterialFact, Produces: ProductObservation},
		Port{Reads: MaterialFact, Produces: ProductView},
		Port{Reads: MaterialFact, Produces: ProductCompression}:
		return nil
	default:
		return fmt.Errorf("plugin: the port %s is not one of the declared ports of the flow", port)
	}
}

// Instance is one instance of the material flow. The drive that runs one
// decision, the tick that runs one reflection and the assembler that builds one
// request are built-in instances; a plugin is an instance the composition root
// mounts. What an instance is differs only in who mounts it: the flow dispatches
// every instance by the material its port reads.
//
// The redline list (3 rules):
// MUST declare the one port it serves and implement that port's narrow interface.
// MUST NOT declare a port it does not implement.
// MUST NOT serve two ports: one instance is one port, and the ruling port may carry both of its faces.
type Instance interface {
	// Name returns the stable name of this instance.
	Name() string

	// Port reports the material this instance reads and the product it answers.
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

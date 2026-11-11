// 契约: docs/adr/0007-generation-scoped-runtime-plugin-lifecycle.md
//
// The generation-scoped consumer registry of one assembly: a Boot-once
// consumer registers the builder of its per-generation instance here, and the
// publication path drives the registry without naming any consumer. The
// registry owns construction order and the fail-closed release of a candidate
// that stops part way; the whole-generation switch, the borrow drain and the
// retirement release stay with the publication entry and its ledger.
package state

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/SpellingDragon/tagent/api/config"
)

// GenBuilder constructs the per-generation instance of one registered consumer
// from the declaration the candidate generation publishes. The builder
// installs the instance as the face the candidate's binding construction
// reads, and answers the exclusive resources the construction acquired. A
// builder that fails leaves the face it serves as it was.
type GenBuilder func(ctx context.Context, decl *config.Declaration, gen Generation) (GenResources, error)

// GenResources is what one per-generation consumer construction answers: the
// ledger of the exclusive resources it acquired beside the restore that puts
// the previous face back when the candidate leaves without taking effect.
type GenResources struct {
	// Resources carries the exclusive resources the construction acquired. Its
	// ReleaseAll frees them once each when the candidate is refused or the
	// generation retires, and never touches the shared faces or the resources
	// already in service. A consumer that acquires nothing exclusive carries
	// nil and is registered nowhere.
	Resources *Leases
	// Restore puts the face the previous generation served back. The
	// publication path calls it when the candidate did not take effect; a
	// candidate that published keeps its face installed.
	Restore func()
}

// GenRegistration is one consumer's mount on the generation-scoped registry:
// the name its exclusive registration is keyed by beside the builder that
// constructs its per-generation instance.
type GenRegistration struct {
	// Name names the consumer. The candidate-exclusive registration of its
	// instance is keyed by GenLeaseKey under this name.
	Name string
	// BuildGen constructs the instance of one candidate generation.
	BuildGen GenBuilder
}

// GenBuilt is one constructed consumer of one candidate generation: its
// registered name beside the resources its construction answered.
type GenBuilt struct {
	// Name is the consumer's registered name.
	Name string
	// Resources is what the consumer's builder answered.
	Resources GenResources
}

// GenInstances is the constructed consumer set of one candidate generation, in
// registration order.
type GenInstances []GenBuilt

// Restore puts the face each built consumer installed back, in reverse
// registration order. The publication path calls it when the candidate left
// without taking effect; the resources themselves are released by the
// candidate ledger or by Release.
func (g GenInstances) Restore() {
	for i := len(g) - 1; i >= 0; i-- {
		if g[i].Resources.Restore != nil {
			g[i].Resources.Restore()
		}
	}
}

// Release frees the exclusive resources of one built candidate generation in
// reverse registration order and once each, joining the failures it finds. It
// is the release a preparation that stopped after the consumers were built
// runs; a candidate that reached the publication entry is released by the
// entry's own candidate ledger instead.
func (g GenInstances) Release(ctx context.Context) error {
	var errs []error
	for i := len(g) - 1; i >= 0; i-- {
		resources := g[i].Resources.Resources
		if resources == nil {
			continue
		}
		if err := resources.ReleaseAll(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// GenLeaseKey names the candidate-exclusive registration of one
// generation-scoped consumer: the generation the candidate publishes beside
// the consumer's registered name, in the frozen form "gen:<id>:<consumer>".
func GenLeaseKey(gen Generation, consumer string) string {
	return fmt.Sprintf("gen:%d:%s", gen, consumer)
}

// GenRegistry is the generation-scoped consumer registry of one assembly. A
// consumer mounts itself through the option that wires it; the publication
// path drives the registry and never names a consumer.
type GenRegistry struct {
	mu            sync.Mutex
	registrations []GenRegistration
}

// NewGenRegistry returns an empty generation-scoped consumer registry.
func NewGenRegistry() *GenRegistry {
	return &GenRegistry{}
}

// Register mounts one consumer's builder. An empty name, a missing builder or
// a name already mounted refuses the registration by name, so a wiring mistake
// stops the assembly instead of silently dropping a consumer.
func (g *GenRegistry) Register(reg GenRegistration) error {
	if reg.Name == "" {
		return errors.New("state: the consumer registration names no consumer")
	}
	if reg.BuildGen == nil {
		return fmt.Errorf("state: the consumer %q registers no generation builder", reg.Name)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, held := range g.registrations {
		if held.Name == reg.Name {
			return fmt.Errorf("state: the consumer %q is already registered", reg.Name)
		}
	}
	g.registrations = append(g.registrations, reg)
	return nil
}

// Build constructs the per-generation instances of every registered consumer
// in registration order. A builder that fails refuses the whole candidate
// generation: the instances built before it are released and their faces restored,
// and the failure is the returned cause. The faces of a returned set stay
// installed until the publication path restores them.
func (g *GenRegistry) Build(ctx context.Context, decl *config.Declaration, gen Generation) (GenInstances, error) {
	g.mu.Lock()
	registrations := append([]GenRegistration(nil), g.registrations...)
	g.mu.Unlock()
	var built GenInstances
	for _, reg := range registrations {
		if err := ctx.Err(); err != nil {
			built.Restore()
			return nil, errors.Join(err, built.Release(ctx))
		}
		resources, err := reg.BuildGen(ctx, decl, gen)
		if err != nil {
			built.Restore()
			return nil, errors.Join(fmt.Errorf("state: build the %s consumer of generation %d: %w", reg.Name, gen, err), built.Release(ctx))
		}
		built = append(built, GenBuilt{Name: reg.Name, Resources: resources})
	}
	return built, nil
}

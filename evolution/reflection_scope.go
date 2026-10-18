package evolution

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SpellingDragon/tagent/memory"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// ReflectionScope is the resolved observation face of one reflecting agent: the
// namespaces it observes and the storage partitions those namespaces resolve to.
//
// The face is an application authorization rather than a capability the
// reflection grants itself. A declared face stays within the namespaces the
// application authorized for reading plus the agent's own partition, an
// undeclared face is the agent's own partition, and a declared face that reaches
// past that boundary is refused at assembly.
type ReflectionScope struct {
	// Own is the observing agent's own namespace, which is always observably
	// legal and is the face of an undeclared observation surface.
	Own string
	// Names lists the observed namespaces in declared order, deduplicated.
	Names []string
	// Partitions lists the storage partition each name resolves to, in the same
	// order as Names; the query scans exactly these partitions.
	Partitions []int
}

// ResolveObservationScope resolves the observation face of one agent from its
// declared namespaces, the namespaces it is authorized to read and its own
// namespace.
//
// An undeclared face is the agent's own partition and never falls back to the
// authorized read scope: what an agent may read is not what it reflects on. A
// declared name that is neither the agent's own nor inside the read scope is
// refused with ErrObservationScopeOutOfBounds naming every such namespace.
func ResolveObservationScope(own string, declared, read []string) (ReflectionScope, error) {
	if own == "" {
		return ReflectionScope{}, ErrReflectionAgentUnnamed
	}
	authorized := make(map[string]bool, len(read)+1)
	authorized[own] = true
	for _, name := range read {
		authorized[name] = true
	}
	names := make([]string, 0, len(declared)+1)
	if len(declared) == 0 {
		names = append(names, own)
	}
	seen := make(map[string]bool, len(declared))
	var refused []string
	for _, name := range declared {
		if name == "" || seen[name] {
			continue
		}
		if !authorized[name] {
			refused = append(refused, name)
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		return ReflectionScope{}, fmt.Errorf("%w: %s", ErrObservationScopeOutOfBounds, strings.Join(refused, ", "))
	}
	scope := ReflectionScope{Own: own, Names: names, Partitions: make([]int, 0, len(names))}
	for _, name := range names {
		scope.Partitions = append(scope.Partitions, memory.PartitionIDFromName(name))
	}
	return scope, nil
}

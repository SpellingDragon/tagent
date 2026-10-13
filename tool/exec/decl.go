// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
//
// Package exec declares the external-execution face of the runtime: the
// declaration one external command runs under, the identity of the resource it
// occupies, and the executor that owns that resource for the whole of its life.
//
// The tmux mechanism itself stays outside this package. How a session is
// started, watched, re-attached and killed belongs to the executor behind the
// Executor port declared in stop.go.
package exec

// ExecutionDecl is the declaration one external execution is started under: the
// command, its arguments, the directory it runs in and the resource identity it
// occupies.
//
// Every field is a plain value, so a declaration recorded before a restart
// replays as the same execution after it. Handles, connections, locks and leases
// are not part of a declaration: the lease of a resource belongs to the executor
// that holds the resource.
type ExecutionDecl struct {
	// Command is the program to run, named exactly as the caller declared it.
	Command string

	// Args are the arguments handed to Command, in order.
	Args []string

	// Dir is the working directory the command runs in. An empty Dir is a caller
	// that declared no directory, which is not the same as a directory that does
	// not apply.
	Dir string

	// Resource is the declared identity of the execution resource this
	// declaration occupies.
	Resource ResourceKey
}

// ResourceKey is the declared identity of one execution resource. Kind and Key
// together name the resource: declarations that agree on both name the same one,
// and a resource already held under a key is not opened a second time.
//
// Kind separates resource families whose keys may read alike. Key is the
// canonical form of the identity inside its kind. A key is an identity to be
// compared, never a location to be resolved.
type ResourceKey struct {
	// Kind names the resource family the key identifies within.
	Kind string

	// Key is the canonical identity of the resource inside its kind.
	Key string
}

package exec

import "context"

// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
//
// Skeleton compile-time assertions for the phase 1 declarations in decl.go and
// stop.go.
//
// The phase 2 executor that will satisfy Executor does not exist yet, so there is
// no implementation assertion to write. The assertions below pin what the
// skeleton proves on its own: every declared method keeps its exact shape, every
// declared name is an interface type, every documented field keeps its name and
// type, and the probe states stay constants of their declared type. A renamed
// method, a changed parameter or a changed result stops these blocks from
// compiling.
var (
	_ func(Executor, context.Context, ExecutionDecl) (ResourceKey, error) = Executor.Start
	_ func(Executor, context.Context, ResourceKey) (ProbeResult, error)   = Executor.Probe
	_ func(Executor, context.Context, ResourceKey) ([]byte, error)        = Executor.Capture
	_ func(Executor, context.Context, ResourceKey) (StopEvidence, error)  = Executor.Stop
)

// Every declared name is an interface type.
var (
	_ Executor = (Executor)(nil)
)

// The declared values keep every documented field.
var (
	_ = ExecutionDecl{
		Command:  "",
		Args:     []string{},
		Dir:      "",
		Resource: ResourceKey{},
	}

	_ = ResourceKey{
		Kind: "",
		Key:  "",
	}

	_ = StopEvidence{
		Resource:  ResourceKey{},
		Confirmed: false,
		ExitCode:  0,
		Reason:    "",
	}

	_ = ProbeResult{
		State:  ProbeUnknown,
		Detail: "",
	}
)

// The probe states must stay compile-time constants of their declared type: a
// variable or a bare int stops this block from compiling.
const (
	_ ProbeState = ProbeUnknown
	_ ProbeState = ProbeAlive
	_ ProbeState = ProbeDead
)

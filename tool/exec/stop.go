package exec

import "context"

// 契约: docs/wiki/tool/tmux-action.md#quiet-vs-dead
//
// Executor is the port through which the runtime starts, probes, captures from
// and stops one external execution, and through which it reports what was
// observed rather than what was requested.
//
// An Executor owns the lease of every resource it holds. A stop it cannot
// confirm leaves the resource held under its key and refuses a fresh execution
// under that key, so an unconfirmed exit is never released to a second writer
// and never reported as a clean one.
type Executor interface {
	// Start begins the external execution declared by decl and returns the
	// canonical key of the resource it now holds.
	Start(ctx context.Context, decl ExecutionDecl) (ResourceKey, error)

	// Probe reports the state one probe observed for key.
	//
	// A state the probe cannot determine is reported as state, not as an error
	// and not as death, so a caller escalates diagnosis without reading
	// uncertainty as an exit.
	Probe(ctx context.Context, key ResourceKey) (ProbeResult, error)

	// Capture returns the output recorded for key: the frame observed while the
	// execution ran, not a partial read that overwrote it.
	Capture(ctx context.Context, key ResourceKey) ([]byte, error)

	// Stop asks the execution behind key to terminate and returns the evidence of
	// what actually stopped.
	//
	// Asking for a termination and observing one are two different facts, and the
	// evidence reports the observation: a stop that was only requested is
	// reported as unconfirmed.
	Stop(ctx context.Context, key ResourceKey) (StopEvidence, error)
}

// StopEvidence is the proof a stop produced: what was observed to have actually
// ended, kept apart from the request to end it.
//
// A cancellation acknowledgement is not this proof. Cancellation is a signal the
// execution may not have observed yet, so an executor holding nothing but an
// acknowledgement reports the stop as unconfirmed.
type StopEvidence struct {
	// Resource is the resource identity this evidence was observed for.
	Resource ResourceKey

	// Confirmed reports whether the execution was observed to have stopped.
	Confirmed bool

	// ExitCode is the exit code observed for the stopped execution. It is
	// meaningful only where Confirmed reports an observed exit.
	ExitCode int

	// Reason carries why the stop is unconfirmed where Confirmed is false.
	Reason string
}

// ProbeResult is what one probe observed about an execution: alive, dead, or not
// determinable.
//
// A probe that cannot tell reports an unknown state instead of guessing. Unknown
// is a diagnostic escalation and never a verdict: no run of unknown probes,
// however long, is a death decision, and death is reported only for an execution
// confirmed absent.
type ProbeResult struct {
	// State is the state this probe observed.
	State ProbeState

	// Detail carries the observation behind a state that is not alive: the probe
	// command that failed, or the evidence of absence.
	Detail string
}

// ProbeState is the three-valued outcome of one liveness probe. The zero value
// is ProbeUnknown, so a result that was never set reads as undetermined rather
// than as alive.
type ProbeState int

const (
	// ProbeUnknown is the zero value: the probe could not determine the state. It
	// escalates diagnosis and never decides death.
	ProbeUnknown ProbeState = iota

	// ProbeAlive reports an execution observed to be alive.
	ProbeAlive

	// ProbeDead reports an execution confirmed absent.
	ProbeDead
)

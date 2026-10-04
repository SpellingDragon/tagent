// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
package agent

// turnStatus is the reduced terminal state of a single RunFlow turn, observed
// from every channel the framework reports through (, design 决策5 L120,
// spec L94/L130).
//
// The old contract collapsed all of these into "RunFlow returned nil", which
// the persistent loop read as *success* — so a turn whose model call failed
// (the framework carries an API failure as an event with Response.Error, which
// RunFlow never inspected) and a turn cut short by a shutdown cancellation
// (deliverEvent returning false with ctx done) BOTH fell through to the ACK
// path. That is the "返回 nil 就是完成" fallacy this type exists to eliminate:
// a nil transport return means only "no transport error", never "the turn
// completed".
type turnStatus int

const (
	// turnCompleted: the event stream drained to its natural end, no
	// response-internal error was observed, and the ctx was not cancelled. The
	// model really produced a terminal response.
	turnCompleted turnStatus = iota
	// turnFailed: a definite model/framework failure — a runner start error that
	// spent the transport retry budget, or a response-internal error event. Per
	// design L121 a full failure is a *processing result* (batch_result=failed),
	// not a delivery proof; it still forms a completion.
	turnFailed
	// turnCancelled: the ctx was cancelled mid-turn with no terminal state. Per
	// spec L90/L130 a shutdown cancellation forms NO completion and the claim
	// MUST be retained (replayed next process), so the loop must NOT ack.
	turnCancelled
)

// String renders the status for logs and for the completion audit trail.
func (s turnStatus) String() string {
	switch s {
	case turnCompleted:
		return "completed"
	case turnFailed:
		return "failed"
	case turnCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// turnOutcome is a reduced turn result: its terminal status plus a bounded error
// summary retained for a failed turn. The completion
// freeze in / consumes the batch-level outcome;  is responsible only
// for producing an honest, fully-classified result.
type turnOutcome struct {
	status turnStatus
	// err is a short, bounded summary of the failure. Empty unless
	// status == turnFailed.
	err string
}

// completedOutcome is the outcome of a normally finished turn.
func completedOutcome() turnOutcome { return turnOutcome{status: turnCompleted} }

// failedOutcome records a definite model/framework failure with a bounded
// summary. Long messages are truncated so the value is safe to freeze into a
// completion and to log.
func failedOutcome(reason string) turnOutcome {
	return turnOutcome{status: turnFailed, err: boundErrSummary(reason)}
}

// cancelledOutcome records a mid-turn shutdown cancellation (no terminal state).
func cancelledOutcome() turnOutcome { return turnOutcome{status: turnCancelled} }

// maxErrSummary bounds the error summary carried by a failed outcome so a
// completion frozen from it (and the log line that renders it) stays small and
// never embeds an unbounded model payload.
const maxErrSummary = 512

func boundErrSummary(s string) string {
	if len(s) <= maxErrSummary {
		return s
	}
	return s[:maxErrSummary] + "…(truncated)"
}

// reduceTurnOutcome is the single decision table the persistent loop and the
// RunFlow drain share, so the classification is unit-testable without driving a
// full framework turn. It takes the two independent failure channels RunFlow can
// observe — a transport/start error (startErr, non-nil) and a response-internal
// error captured from the event stream (respErr, non-empty) — plus whether the
// ctx was cancelled mid-drain, and returns the reduced per-attempt outcome.
//
// Ordering is deliberate: a cancellation outranks an error observed in the same
// drain (a turn that was cut short reached no terminal state, so spec L130 says
// it forms no completion even if a partial error was already seen), and a
// transport error outranks a response error (a runner that failed to start never
// produced a real response to classify).
func reduceTurnOutcome(startErr error, respErr string, cancelled bool) turnOutcome {
	switch {
	case cancelled:
		return cancelledOutcome()
	case startErr != nil:
		return failedOutcome(startErr.Error())
	case respErr != "":
		return failedOutcome(respErr)
	default:
		return completedOutcome()
	}
}

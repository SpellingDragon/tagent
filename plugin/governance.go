package plugin

import (
	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
)

// 契约: docs/wiki/agent/governance-enforcement.md#disposition-and-risk
//
// Disposition is the ruling a tool call receives before its side effect runs:
// the call is allowed, allowed with an audit record, or held for approval. A
// hold is a wait, not a denial.
type Disposition string

const (
	// DispositionAllow lets the call run with no extra trace duty.
	DispositionAllow Disposition = "allow"

	// DispositionRecord lets the call run and requires this execution to be
	// written to the audit ledger.
	DispositionRecord Disposition = "record"

	// DispositionHold stops the call before its side effect and hands it to the
	// approval channel. The call has not run and may still be approved, so a
	// hold is a wait and is never reported as a denial.
	DispositionHold Disposition = "hold"
)

// RiskLevel grades the risk of one tool call, from low to critical. The
// critical grade always takes the asynchronous approval path, and the grading
// input is plain data: the tool name, the argument JSON and the trigger source.
//
// The zero value is not a grade a face renders: it reads as "no grade was
// rendered". A ruling that receives it reads RiskMedium, so the call is ruled as
// an ordinary mutating call instead of passing ungraded; a rule threshold that
// carries it fires at every grade instead of at none.
type RiskLevel string

const (
	// RiskLow is the grade of a read-only call; it runs with no trace duty.
	RiskLow RiskLevel = "low"

	// RiskMedium is the grade of an ordinary mutating call; it runs and is
	// recorded.
	RiskMedium RiskLevel = "medium"

	// RiskHigh is the grade of an irreversible or privileged call; it runs and
	// is recorded, and an exhausted budget refuses it.
	RiskHigh RiskLevel = "high"

	// RiskCritical is the grade of a call that always needs approval.
	RiskCritical RiskLevel = "critical"
)

// Denial is the terminal refusal of one tool call.
//
// The call does not run, and the model receives the native result paired to the
// call instead of an error, so it can correct itself on its next turn. A denial
// is never a hold: a held call has not been refused and may still run after
// approval.
type Denial struct {
	// CallID is the native call identifier the paired result answers.
	CallID string

	// Tool is the name of the tool the model called.
	Tool string

	// Risk is the grade the refusal was rendered at.
	Risk RiskLevel

	// Rule names the rule that refused the call.
	Rule string

	// Result is the native tool result paired to the call; its text carries the
	// reason and the rule for the model to read.
	Result trpcmodel.Message
}

// Decision is the outcome of the ruling one tool call receives before it runs.
//
// An allowed or recorded call runs; a held call waits for approval and keeps
// Denial nil; a refused call carries Denial and does not run. A hold is thus
// never misreported as a denial.
type Decision struct {
	// Disposition is the ruling for this call.
	Disposition Disposition

	// Risk is the grade the ruling was rendered at.
	Risk RiskLevel

	// Rule names the matched rule the ruling came from.
	Rule string

	// Reason is the correction material the model reads when the call is held or
	// refused.
	Reason string

	// Denial is set only for a terminal refusal, and stays nil for an allowed,
	// recorded or held call.
	Denial *Denial
}

// Governor renders the single pre-execution ruling for one tool call.
//
// The redline list (3 rules):
// MUST render one ruling per call, over the one Before pass that call carries.
// MUST NOT execute the call, and MUST NOT bypass a hold it rendered.
// MUST NOT report a hold as a denial.
type Governor interface {
	// Judge returns the ruling for one tool call before its side effect runs,
	// over the one Before pass the call carries: the final call, the risk grade
	// the risk faces already rendered and the trigger source the dispatch
	// stated. The ruling is rendered exactly once per call and is used both for
	// the execution and for the record. A hold is a wait, not a denial.
	Judge(pass CallPass) (Decision, error)
}

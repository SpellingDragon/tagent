package agent

import (
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// defaultFinalRoundPrompt is the closing instruction one drive hands the model
// once its budget is spent: the round answers what it already produced instead
// of continuing to produce.
const defaultFinalRoundPrompt = "tagent: the budget of this invocation is spent — this is your final round. Answer now: summarise what you completed, deliver the artefacts this invocation already produced, and close every reference you opened. Tool calls are not admitted in this round."

// finalRoundRefusal is the paired answer of one tool call the closing round
// issued: the call is answered so its assistant message keeps one answer per
// call, and it is admitted nowhere.
const finalRoundRefusal = "tagent: the budget of this invocation is spent; this call was not admitted — the closing round delivers what already exists"

// defaultFinalRoundTimeout bounds the closing round of one drive whose policy
// names no timeout of its own.
const defaultFinalRoundTimeout = 30 * time.Second

// FinalRoundReason names why the closing round of a drive ran, and stands empty
// on a drive that ended on its own terminal.
type FinalRoundReason string

// FinalRoundNone, FinalRoundRounds and FinalRoundBudget name the reasons a
// closing round runs under.
const (
	// FinalRoundNone reports a round that is not the closing round of its drive.
	FinalRoundNone FinalRoundReason = ""
	// FinalRoundRounds reports a closing round the spent round budget owed.
	FinalRoundRounds FinalRoundReason = "rounds"
	// FinalRoundBudget reports a closing round the spent time budget owed.
	FinalRoundBudget FinalRoundReason = "budget"
)

// FinalRoundPolicy is the watchdog budget of one drive: how many tool rounds the
// drive may run and how long it may spend before its next decision becomes the
// closing round, the instruction that round carries and the short timeout it
// runs under.
//
// The zero policy keeps every drive of the loop unbounded, exactly as a drive
// runs with no watchdog attached: a loop that attaches no policy never meets a
// closing round. The closing round is the round the model delivers in — it runs
// once the budget is spent, it is marked as the closing round on the request
// boundary of its decision, and the drive ends when it returns.
// The redline list (4 rules):
// MUST keep every drive unbounded while both budgets are zero.
// MUST read the budget at a round boundary and never cut a decision already in flight.
// MUST run one closing round at most, and end the drive when it returns.
// MUST NOT admit a tool call the closing round issues: delivering opens no new obligation.
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
type FinalRoundPolicy struct {
	// MaxRounds bounds the tool rounds one drive may run; 0 removes the bound.
	MaxRounds int
	// Budget bounds the time one drive may spend; 0 removes the bound.
	Budget time.Duration
	// Timeout bounds the closing round itself; 0 takes defaultFinalRoundTimeout.
	Timeout time.Duration
	// Prompt is the closing instruction handed to the model; "" takes
	// defaultFinalRoundPrompt.
	Prompt string
}

// SetFinalRound attaches the watchdog budget the drives of this loop run under
// while their agent declares none of its own. A zero policy keeps every drive
// unbounded. The setter is separate from the constructor so the declared
// construction shape of the loop stays as it is.
func (l *Loop) SetFinalRound(policy FinalRoundPolicy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.final = policy
}

// SetAgentFinalRound attaches the watchdog budget the drives of one agent run
// under: a drive whose address names this agent reads the policy set here, and
// every other drive reads the loop's default. The budget is per agent, so two
// agents of one declaration close their own drives on their own bounds.
func (l *Loop) SetAgentFinalRound(agent event.AgentID, policy FinalRoundPolicy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.finals == nil {
		l.finals = map[event.AgentID]FinalRoundPolicy{}
	}
	l.finals[agent] = policy
}

// SetAgentFinalRounds replaces the per-agent budgets of this loop with the
// table given: an agent the table does not name reads the loop's default, so a
// publication that drops a budget leaves no earlier entry behind. A nil or
// empty table clears every per-agent entry.
func (l *Loop) SetAgentFinalRounds(policies map[event.AgentID]FinalRoundPolicy) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(policies) == 0 {
		l.finals = nil
		return
	}
	finals := make(map[event.AgentID]FinalRoundPolicy, len(policies))
	for agent, policy := range policies {
		finals[agent] = policy
	}
	l.finals = finals
}

// finalRoundFor reports the watchdog budget the drives of one agent run under:
// the policy the agent declares for itself, or the loop's default while it
// declares none.
func (l *Loop) finalRoundFor(agent event.AgentID) FinalRoundPolicy {
	l.mu.Lock()
	defer l.mu.Unlock()
	if policy, ok := l.finals[agent]; ok {
		return policy
	}
	return l.final
}

// closingPrompt reports the instruction the closing round carries.
func (p FinalRoundPolicy) closingPrompt() string {
	if p.Prompt != "" {
		return p.Prompt
	}
	return defaultFinalRoundPrompt
}

// closingTimeout reports the short timeout the closing round runs under.
func (p FinalRoundPolicy) closingTimeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return defaultFinalRoundTimeout
}

// decisionRound names the round the next decision of one drive runs: an ordinary
// round, which may dispatch the tool calls its terminal carries, or the one
// closing round the spent budget leaves for delivery.
type decisionRound struct {
	// final marks the closing round of the drive.
	final bool
	// reason names why that round runs.
	reason FinalRoundReason
	// prompt is the closing instruction the request of that round carries.
	prompt string
	// timeout bounds the closing round of the drive.
	timeout time.Duration
}

// closingRoundDue reports why the closing round of one drive is due now, or
// FinalRoundNone while the drive may continue. Both budgets are read at a round
// boundary, so a decision already in flight is never cut by this reading.
func closingRoundDue(policy FinalRoundPolicy, rounds int, elapsed time.Duration) FinalRoundReason {
	if policy.MaxRounds > 0 && rounds >= policy.MaxRounds {
		return FinalRoundRounds
	}
	if policy.Budget > 0 && elapsed >= policy.Budget {
		return FinalRoundBudget
	}
	return FinalRoundNone
}

// closingRound reports the round the next decision of one drive runs: the
// closing round a spent budget owes, or the ordinary round the drive continues
// with.
func closingRound(policy FinalRoundPolicy, rounds int, elapsed time.Duration) decisionRound {
	reason := closingRoundDue(policy, rounds, elapsed)
	if reason == FinalRoundNone {
		return decisionRound{}
	}
	return decisionRound{
		final:   true,
		reason:  reason,
		prompt:  policy.closingPrompt(),
		timeout: policy.closingTimeout(),
	}
}

// refuseClosingCalls answers every tool call the closing round issued with the
// one paired refusal and admits none of them: the closing round delivers what
// the invocation already produced, so it opens no obligation beside the drain
// that follows the drive. No call of that round reaches admission, so the
// plugin pass of the generation runs for none of them.
func refuseClosingCalls(calls []model.ToolCall) []model.Message {
	receipts := make([]model.Message, 0, len(calls))
	for _, call := range calls {
		receipts = append(receipts, paired(call.ID, call.Function.Name, finalRoundRefusal))
	}
	return receipts
}

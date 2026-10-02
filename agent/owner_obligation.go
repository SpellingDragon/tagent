// 契约: docs/wiki/agent/execution-generations.md#lifecycle-convergence
package agent

import "fmt"

// ObligationReport answers /D7's question for one resident owner: is anything still
// depending on it that would be broken by retiring it?
//
// - The three axes are disjoint by construction, each read from the accounting that owns it, so retirement never invents a parallel notion of "busy".
// - Executions: references held by this owner's own execution generations (resident turns, inherited sub-calls, post-ACK background runs) — the same lease accounting that gates per-generation reclaim.
// - Invocations: invocation-private contexts currently running on this owner; a delegation builds its own context instead of opening a turn on the sub-agent's resident generations, so this axis is what keeps a removed owner from being retired underneath a call it is still serving.
// - LiveTasks: entries still in a live state on this owner's task board (running, stable service sessions, suspect, alive-detached). A live board entry is an obligation even with nothing executing, because resume/relaunch and recovery reconciliation still route through this owner.
// - Terminal board entries are deliberately not obligations: retained data (history, rollback config, a name that once existed) is never a reason to keep a running instance alive.
type ObligationReport struct {
	Executions  int
	Invocations int
	LiveTasks   int
}

// Idle reports whether nothing depends on the owner any more.
func (o ObligationReport) Idle() bool {
	return o.Executions == 0 && o.Invocations == 0 && o.LiveTasks == 0
}

// String renders the report for diagnostics and refusal reasons: a held retirement
// must say WHICH obligation holds it, so the host can act instead of guessing.
func (o ObligationReport) String() string {
	return fmt.Sprintf("executions=%d invocations=%d live_tasks=%d", o.Executions, o.Invocations, o.LiveTasks)
}

// Obligations reads the three axes for this instance. Safe on a partially built
// instance (nil context manager / no board) — absent machinery counts as absent
// obligation, which is what a bare unit-test instance represents.
func (ta *TagentAgent) Obligations() ObligationReport {
	var rep ObligationReport
	if ta == nil {
		return rep
	}
	if cm := ta.ContextManager(); cm != nil {
		rep.Executions = cm.OutstandingRefs()
	}
	rep.Invocations = ta.LiveCMCount()
	if tm := ta.TaskManager(); tm != nil {
		for _, tk := range tm.List() {
			if tk.Status().Live() {
				rep.LiveTasks++
			}
		}
	}
	return rep
}

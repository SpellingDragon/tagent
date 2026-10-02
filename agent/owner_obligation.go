package agent

import "fmt"

// ObligationReport answers /D7's question for one resident owner: is anything
// still depending on it that would be broken by retiring it? The three axes are
// disjoint by construction and each is read from the accounting that OWNS it, so
// retirement never invents a parallel notion of "busy":
//
// - Executions: references held by this owner's own execution generations — the
// resident turns, inherited sub-calls and post-ACK background runs tracked by
// the same lease accounting that gates per-generation reclaim.
// - Invocations: invocation-private contexts currently running on this owner. A
// delegation into a sub-agent builds its own context rather than opening a
// turn on the sub-agent's resident generations, so this axis is what keeps a
// removed owner from being retired underneath a call it is still serving.
// - LiveTasks: entries still in a live state on this owner's task board, i.e.
// accepted inputs whose work has not settled (running, stable service
// sessions, suspect, alive-detached). A board entry in a live state is an
// obligation even when nothing is executing right now — resume/relaunch and
// the recovery reconciliation still route through this owner.
//
// Terminal board entries are deliberately NOT obligations:  forbids using
// retained DATA (history, rollback config, a name that once existed) as a reason
// to keep a running INSTANCE alive.
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

// 契约: docs/wiki/agent/task-lifecycle.md#status-machine
//
// This file holds the logical status of a task: the classification of how long
// the task's recorded state has stood unchanged. It is a second read-out beside
// the status machine, never a value the machine stores: the status a task holds
// is written by the transitions it commits, and this classification is derived
// from the movement stamps those transitions leave, so reading one leaves the
// other exactly as it stands.
package task

import (
	"context"
	"sort"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// LogicalStatus is the classification of a task by how long its recorded state
// has stood unchanged. A task keeps the status its transitions committed; this
// classification names the silence that state was read under, so a stable or a
// suspected task is reported as such without any status of it moving.
type LogicalStatus string

const (
	// LogicalActive is the zero value: the task's state moved within the watch
	// threshold of its policy, or its movement carries no stamp, so no silence
	// is identified.
	LogicalActive LogicalStatus = ""
	// LogicalWatch is a task whose state has stood unchanged for the watch
	// threshold: the silence is observed and nothing is judged of it.
	LogicalWatch LogicalStatus = "watch"
	// LogicalStable is a task whose state has stood unchanged for the stable
	// threshold: the silence itself is the report, and neither the status nor
	// the obligations of the task change with it.
	LogicalStable LogicalStatus = "stable"
	// LogicalSuspect is a task whose state has stood unchanged for the suspect
	// threshold: the task is reported as holding no live execution of its own.
	LogicalSuspect LogicalStatus = "suspect"
	// LogicalTimedOut is a task whose execution has run past the execution
	// timeout of its policy: the caller stops holding the wait for that
	// execution, and its result arrives as a notification when it settles.
	LogicalTimedOut LogicalStatus = "timed-out"
	// LogicalExpired is a task that stayed unsettled past the time to live of
	// its policy.
	LogicalExpired LogicalStatus = "expired"
)

// Policy is the threshold set one task's logical status is read under. A zero
// threshold places no bound on the threshold it names, so a policy states only
// the silences it wants identified and the zero policy identifies none.
type Policy struct {
	// WatchAfter is how long a task's state may stand unchanged before that
	// silence is observed.
	WatchAfter time.Duration
	// StableAfter is how long it may stand unchanged before the silence is
	// reported as the stable state of the task.
	StableAfter time.Duration
	// SuspectAfter is how long it may stand unchanged before the task is
	// reported as holding no live execution.
	SuspectAfter time.Duration
	// ExecutionTimeout is how long one round of execution may run before the
	// caller stops holding the wait for its result.
	ExecutionTimeout time.Duration
	// TTL is how long a task may stay unsettled after its admission.
	TTL time.Duration
}

// PolicyDefault, PolicyResponsive and PolicyPatient are the declared task policy
// names: the name an agent declaration selects through its task policy, and the
// threshold sets those names resolve to.
const (
	// PolicyDefault is the name of DefaultPolicy; the empty name selects it too.
	PolicyDefault = "default"
	// PolicyResponsive is the name of the threshold set for tasks whose caller
	// wants an earlier report of silence.
	PolicyResponsive = "responsive"
	// PolicyPatient is the name of the threshold set for tasks that stay in
	// execution for a long while by design.
	PolicyPatient = "patient"
)

// DefaultPolicy is the threshold set a task is read under when its declaration
// names no other policy: silence is observed after half a minute, reported
// stable after two minutes and suspected after ten, one round of execution is
// waited for half an hour, and a task unsettled for two hours expires.
var DefaultPolicy = Policy{
	WatchAfter:       30 * time.Second,
	StableAfter:      2 * time.Minute,
	SuspectAfter:     10 * time.Minute,
	ExecutionTimeout: 30 * time.Minute,
	TTL:              2 * time.Hour,
}

// PolicyNamed reports the threshold set the declared task policy name selects and
// whether the name is one this table declares. The empty name selects
// DefaultPolicy; a name outside the table is reported as unknown rather than read
// as the default, so a misspelled declaration is visible where it is resolved.
func PolicyNamed(name string) (Policy, bool) {
	switch name {
	case "", PolicyDefault:
		return DefaultPolicy, true
	case PolicyResponsive:
		return Policy{
			WatchAfter:       15 * time.Second,
			StableAfter:      time.Minute,
			SuspectAfter:     5 * time.Minute,
			ExecutionTimeout: 15 * time.Minute,
			TTL:              time.Hour,
		}, true
	case PolicyPatient:
		return Policy{
			WatchAfter:       2 * time.Minute,
			StableAfter:      10 * time.Minute,
			SuspectAfter:     30 * time.Minute,
			ExecutionTimeout: time.Hour,
			TTL:              4 * time.Hour,
		}, true
	}
	return Policy{}, false
}

// stamps are the movement points one task's logical status is read from: the
// instant it was admitted, the instant its status last changed and the instant
// the round it runs entered execution. A record carrying no movement stamp is
// read as moving, so a task rebuilt from its fact chain is never classified from
// a silence the registry did not observe over it.
type stamps struct {
	admitted time.Time
	changed  time.Time
	running  time.Time
}

// classify reports the logical status of one task at the given instant. A task
// that reached a terminal fact, or whose status is unknown, carries no logical
// status: its silence is its stored outcome rather than an observation of it.
func (p Policy) classify(status Status, s stamps, at time.Time) LogicalStatus {
	if status == StatusUnknown || isTerminal(status) {
		return LogicalActive
	}
	if p.TTL > 0 && !s.admitted.IsZero() && at.Sub(s.admitted) >= p.TTL {
		return LogicalExpired
	}
	if p.ExecutionTimeout > 0 && isExecuting(status) && !s.running.IsZero() && at.Sub(s.running) >= p.ExecutionTimeout {
		return LogicalTimedOut
	}
	if s.changed.IsZero() {
		return LogicalActive
	}
	silent := at.Sub(s.changed)
	switch {
	case p.SuspectAfter > 0 && silent >= p.SuspectAfter:
		return LogicalSuspect
	case p.StableAfter > 0 && silent >= p.StableAfter:
		return LogicalStable
	case p.WatchAfter > 0 && silent >= p.WatchAfter:
		return LogicalWatch
	}
	return LogicalActive
}

// isExecuting reports whether a status holds an execution whose result the caller
// still waits for.
func isExecuting(status Status) bool {
	return status == StatusRunning || status == StatusProgress
}

// WithPolicy sets the threshold set every task of the registry is read under. A
// registry built beside no policy reads its tasks under DefaultPolicy.
func WithPolicy(policy Policy) RegistryOption {
	return func(r *MemoryRegistry) { r.policy = policy }
}

// LogicalChange is one identified logical status change of one task: the task it
// belongs to, the status the machine holds beside both logical statuses of the
// change and the instant it was identified at.
type LogicalChange struct {
	// TaskID is the task the change belongs to.
	TaskID event.TaskID
	// Status is the status the machine holds, unchanged by the change.
	Status Status
	// From is the logical status the task was last reported under.
	From LogicalStatus
	// To is the logical status the task is reported under now.
	To LogicalStatus
	// Since is the instant the change was identified at.
	Since time.Time
}

// Logical reports the logical status of one task at the given instant: the
// silence its recorded state has stood in, read beside the status machine. The
// status, revision, mutations and obligations of the task stay as they stand.
func (r *MemoryRegistry) Logical(ctx context.Context, taskID event.TaskID, at time.Time) (LogicalStatus, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.tasks[taskID]
	if !ok {
		return LogicalActive, ErrTaskNotFound
	}
	return r.policy.classify(entry.status, entry.stamps(), at), nil
}

// Watch identifies the logical status changes of every task the registry holds
// non-terminal at the given instant, in task identity order. Each change is
// reported once: a task already reported under a logical status is reported again
// only when that status moves. The report reads the silence of the tasks it
// covers and moves no status, revision or obligation of any of them.
func (r *MemoryRegistry) Watch(ctx context.Context, at time.Time) []LogicalChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]event.TaskID, 0, len(r.tasks))
	for id := range r.tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var changes []LogicalChange
	for _, id := range ids {
		entry := r.tasks[id]
		if isTerminal(entry.status) {
			continue
		}
		identified := r.policy.classify(entry.status, entry.stamps(), at)
		if identified == entry.reported {
			continue
		}
		changes = append(changes, LogicalChange{
			TaskID: id,
			Status: entry.status,
			From:   entry.reported,
			To:     identified,
			Since:  at,
		})
		entry.reported = identified
	}
	return changes
}

// stamps reports the movement points this record was read under.
func (t *memoryTask) stamps() stamps {
	return stamps{admitted: t.admittedAt, changed: t.changedAt, running: t.runSince}
}

// markMovement records the movement one committed transition left on this task:
// the instant its status last changed and the instant the round it runs entered
// execution. A transition that keeps the status moves nothing.
func (t *memoryTask) markMovement(to Status, at time.Time) {
	if to == t.status {
		return
	}
	t.changedAt = at
	if isExecuting(to) && !isExecuting(t.status) {
		t.runSince = at
	}
}

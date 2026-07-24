package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskStatus is the lifecycle state of a Task.
type TaskStatus string

const (
	TaskRunning       TaskStatus = "running"        // in flight, no settle yet
	TaskStable        TaskStatus = "stable"         // output stable, process alive (usable, maybe waiting)
	TaskAliveDetached TaskStatus = "alive_detached" // service-type: settled once, still alive (Phase 2)
	TaskCompleted     TaskStatus = "completed"      // finished successfully
	TaskFailed        TaskStatus = "failed"         // finished with error
	TaskSuspect       TaskStatus = "suspect"        // quiet too long — likely hung
	TaskDead          TaskStatus = "dead"           // abandoned; spec retained for relaunch
	TaskCancelled     TaskStatus = "cancelled"      // explicitly cancelled
)

// SettleKind classifies how a task reached a settle point. Detectors emit this
// deterministically; the LLM interprets ambiguous kinds (stable/suspect) later.
type SettleKind string

const (
	SettleCompleted SettleKind = "completed" // runnable exited — definitely done
	SettleStable    SettleKind = "stable"    // output stable, still alive — usable but maybe waiting
	SettleSuspect   SettleKind = "suspect"   // quiet beyond fake-dead threshold — likely hung
)

// SettleSignal is emitted by a SettleDetector when a task reaches a settle point.
type SettleSignal struct {
	Kind   SettleKind
	Output string // captured result/output at settle time
	Err    error  // non-nil when the task failed
}

// SettleDetector observes a running task and emits SettleSignals. Different task
// types provide different detectors:
//   - tmux command → wraps TmuxMonitor (stable/completed/suspect)  [Phase 1]
//   - sub-agent    → RunFlow returns                               [Phase 3]
//   - generic      → goroutine returns                             [Phase 0]
//
// Settled() MUST be closed when the detector will emit no further signals.
type SettleDetector interface {
	// Settled returns a channel delivering settle signals, closed when done.
	Settled() <-chan SettleSignal
	// Detached returns a channel that fires (is closed) once when the detector's
	// dense phase ends without a settle — the sync→async boundary. Spawn selects
	// on settle-or-detach; a detach means "stop blocking, notify later". A
	// detector that never wants to force-detach may return a nil/never channel.
	Detached() <-chan struct{}
	// Cancel stops the underlying work.
	Cancel()
}

// defaultDenseDuration is the default dense-phase length (≈ the retired
// sync_wait): how long a detector blocks before signalling detach.
const defaultDenseDuration = 10 * time.Second

// DetachAfter returns a channel that is closed after d elapses, unless stop
// fires first (e.g. the detector settled/cancelled). It is the shared timer
// primitive behind the settle-or-detach contract; the returned channel closing
// means "dense phase ended — detach".
func DetachAfter(d time.Duration, stop <-chan struct{}) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
			close(ch)
		case <-stop:
		}
	}()
	return ch
}

// TaskSpec captures enough to describe and (re)launch a task.
type TaskSpec struct {
	Kind string // "command" | "subagent" | "generic"
	Desc string // human-readable (board + logs)
	// Key is the idempotency key: while a task with this Key is active, a
	// repeat Spawn returns the existing task instead of creating a duplicate.
	// Empty Key disables dedup.
	Key string
	// Relaunch, when non-nil, re-spawns an equivalent task from scratch (used by
	// relaunch(id)). For command tasks it re-runs the original command in a
	// fresh session. Nil → the task is not relaunchable.
	Relaunch func() (SpawnResult, error)
}

// Task is a unit of async work tracked by the TaskManager.
type Task struct {
	ID        string
	Spec      TaskSpec
	StartedAt time.Time

	mu        sync.Mutex
	status    TaskStatus
	result    string
	resultRef string
	err       error
	settledAt time.Time

	detector      SettleDetector
	firstSettle   chan SettleSignal // cap 1: carries the first settle into the sync-wait window
	windowClosed  bool              // true once the sync-wait window ended (inline settle OR timeout)
	aliveDetached bool              // true once a service task's first stable "ready" was emitted (D4)
}

// Status returns the task's current status (thread-safe snapshot).
func (t *Task) Status() TaskStatus { t.mu.Lock(); defer t.mu.Unlock(); return t.status }

// Result returns the latest captured output (thread-safe snapshot).
func (t *Task) Result() string { t.mu.Lock(); defer t.mu.Unlock(); return t.result }

// isActive reports whether the task is still live (dedup targets these).
func (t *Task) isActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch t.status {
	case TaskRunning, TaskStable, TaskAliveDetached, TaskSuspect:
		return true
	default:
		return false
	}
}

// SpawnResult is returned by Spawn.
type SpawnResult struct {
	Task    *Task
	Settled bool         // true: settled within the sync-wait window (inline)
	Signal  SettleSignal // valid when Settled
	Deduped bool         // true: an equivalent active task already existed
}

// TaskSpawner is the narrow interface tools (e.g. ActionTool) use to hand a
// SettleDetector to the task layer at call time — injected via the invocation's
// RuntimeState — so tools stay free of any task-lifecycle state. *TaskManager
// implements it.
type TaskSpawner interface {
	Spawn(spec TaskSpec, detector SettleDetector) SpawnResult
}

var _ TaskSpawner = (*TaskManager)(nil)

// TaskController is the broader task-management surface used by the live board
// and the LLM task tools (list/get/cancel). It embeds TaskSpawner so a single
// injected value serves both tool spawning and task management. *TaskManager
// implements it.
type TaskController interface {
	TaskSpawner
	List() []*Task
	Get(id string) (*Task, bool)
	Cancel(id string) bool
	Relaunch(id string) (SpawnResult, error)
}

var _ TaskController = (*TaskManager)(nil)

// taskSpawnerCtxKey is the private context key for the injected TaskSpawner.
type taskSpawnerCtxKey struct{}

// WithTaskSpawner returns a context carrying the given TaskSpawner. tagent sets
// this on the context before RunFlow so tools (e.g. ActionTool) can retrieve it
// during Call via TaskSpawnerFromContext. Context values propagate through the
// framework flow down to tool.Call (the same path that carries the invocation).
func WithTaskSpawner(ctx context.Context, s TaskSpawner) context.Context {
	return context.WithValue(ctx, taskSpawnerCtxKey{}, s)
}

// TaskSpawnerFromContext returns the TaskSpawner injected via WithTaskSpawner,
// or (nil, false) when none is present (in which case tools fall back to their
// synchronous behavior).
func TaskSpawnerFromContext(ctx context.Context) (TaskSpawner, bool) {
	s, ok := ctx.Value(taskSpawnerCtxKey{}).(TaskSpawner)
	return s, ok && s != nil
}

// TaskControllerFromContext returns the injected value as a TaskController (the
// broader surface used by the LLM task tools). The value injected via
// WithTaskSpawner is a *TaskManager, which satisfies TaskController.
func TaskControllerFromContext(ctx context.Context) (TaskController, bool) {
	c, ok := ctx.Value(taskSpawnerCtxKey{}).(TaskController)
	return c, ok && c != nil
}

// TaskManagerConfig configures a TaskManager.
type TaskManagerConfig struct {
	// OnSettle is invoked when a task settles AFTER its window closed (detach) —
	// i.e. a background settle that must be written back as a task_settled event.
	// May be nil.
	OnSettle func(task *Task, sig SettleSignal)
}

// TaskManager is a deterministic (non-LLM) registry + scheduler for async tasks.
// The sync→async boundary is owned by each detector's detach signal (adaptive
// poll schedule); TaskManager holds no sync_wait knob.
type TaskManager struct {
	mu       sync.Mutex
	tasks    map[string]*Task // id → task
	byKey    map[string]string
	onSettle func(task *Task, sig SettleSignal)
}

// NewTaskManager creates a TaskManager.
func NewTaskManager(cfg TaskManagerConfig) *TaskManager {
	return &TaskManager{
		tasks:    make(map[string]*Task),
		byKey:    make(map[string]string),
		onSettle: cfg.OnSettle,
	}
}

// Spawn starts a task and blocks until the first of {settle, detach}.
//   - Settle first  → SpawnResult{Settled: true, Signal} (inline).
//   - Detach first  → SpawnResult{Settled: false} (ack; tracked in background).
//   - Equivalent task active → SpawnResult{Deduped: true} (no new task).
//
// Multiple concurrent Spawn calls each wait their own detector's window in
// parallel (blocking ≈ the slowest, not the sum).
func (tm *TaskManager) Spawn(spec TaskSpec, detector SettleDetector) SpawnResult {
	// Idempotent dedup: an active task with the same Key short-circuits.
	tm.mu.Lock()
	if spec.Key != "" {
		if id, ok := tm.byKey[spec.Key]; ok {
			if existing, ok := tm.tasks[id]; ok && existing.isActive() {
				tm.mu.Unlock()
				detector.Cancel() // never double-run
				return SpawnResult{Task: existing, Deduped: true}
			}
		}
	}
	task := &Task{
		ID:          uuid.NewString(),
		Spec:        spec,
		StartedAt:   time.Now(),
		status:      TaskRunning,
		detector:    detector,
		firstSettle: make(chan SettleSignal, 1),
	}
	tm.tasks[task.ID] = task
	if spec.Key != "" {
		tm.byKey[spec.Key] = task.ID
	}
	tm.mu.Unlock()

	go tm.watch(task)

	// Wait for the first of {settle, detach}. The detach signal (dense→sparse
	// boundary, owned by the detector) is the sync→async ack point — there is no
	// separate sync_wait timer. A detector with no detach channel (nil) blocks
	// here until settle (pure synchronous).
	select {
	case sig := <-task.firstSettle:
		tm.closeWindow(task, false) // settle closed the window; already consumed
		return SpawnResult{Task: task, Settled: true, Signal: sig}
	case <-detector.Detached():
		tm.closeWindow(task, true) // detach closed the window; drain any boundary settle
		return SpawnResult{Task: task, Settled: false}
	}
}

// watch consumes the detector's settle signals, updates task state, and routes
// each signal either into the sync-wait window (before it closes) or to OnSettle
// (after). The routing decision is made under task.mu together with the buffered
// send, so no settle is lost at the window boundary.
func (tm *TaskManager) watch(task *Task) {
	for sig := range task.detector.Settled() {
		tm.applyStatus(task, sig)

		task.mu.Lock()
		if task.windowClosed {
			task.mu.Unlock()
			tm.emitBackground(task, sig)
		} else {
			// Still inside the window: hand the first settle to Spawn.
			select {
			case task.firstSettle <- sig:
			default: // buffer already holds one — ignore extras within window
			}
			task.mu.Unlock()
		}
	}
}

// closeWindow marks the sync-wait window closed. When drainToBg is true (timeout
// path), any settle that landed in the buffer at the boundary is routed to the
// background handler so it is never dropped.
func (tm *TaskManager) closeWindow(task *Task, drainToBg bool) {
	task.mu.Lock()
	if task.windowClosed {
		task.mu.Unlock()
		return
	}
	task.windowClosed = true
	var pending *SettleSignal
	if drainToBg {
		select {
		case sig := <-task.firstSettle:
			pending = &sig
		default:
		}
	}
	task.mu.Unlock()
	if pending != nil {
		tm.emitBackground(task, *pending)
	}
}

// emitBackground invokes the OnSettle hook for a settle that occurred after the
// sync-wait window closed (a background/reclaim settle), applying alive-detached
// semantics for service-type tasks (D4):
//   - first stable → transition to alive-detached and emit the one-time "ready"
//     notification;
//   - once detached, subsequent stable/suspect signals (e.g. output changes, a
//     quiet service) are suppressed to avoid reclaim spam / permanent board churn;
//   - completion/failure (process death) always emits and ends the task.
func (tm *TaskManager) emitBackground(task *Task, sig SettleSignal) {
	task.mu.Lock()
	switch sig.Kind {
	case SettleStable:
		if task.aliveDetached {
			task.mu.Unlock()
			return // already detached; suppress repeat "still alive" signals
		}
		task.aliveDetached = true
		task.status = TaskAliveDetached
	case SettleSuspect:
		if task.aliveDetached {
			task.mu.Unlock()
			return // a detached service going quiet is expected; do not spam
		}
	}
	task.mu.Unlock()

	if tm.onSettle != nil {
		tm.onSettle(task, sig)
	}
}

// applyStatus maps a settle kind to the task's status and records the result.
func (tm *TaskManager) applyStatus(task *Task, sig SettleSignal) {
	task.mu.Lock()
	defer task.mu.Unlock()
	task.result = sig.Output
	task.err = sig.Err
	task.settledAt = time.Now()
	switch sig.Kind {
	case SettleCompleted:
		if sig.Err != nil {
			task.status = TaskFailed
		} else {
			task.status = TaskCompleted
		}
	case SettleStable:
		// Do not revert an already-detached service back to plain stable on a
		// subsequent output change; emitBackground keeps it alive-detached.
		if task.status != TaskAliveDetached {
			task.status = TaskStable
		}
	case SettleSuspect:
		if task.status != TaskAliveDetached {
			task.status = TaskSuspect
		}
	}
}

// Get returns a task by id.
func (tm *TaskManager) Get(id string) (*Task, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	t, ok := tm.tasks[id]
	return t, ok
}

// List returns a snapshot of all tracked tasks.
func (tm *TaskManager) List() []*Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	out := make([]*Task, 0, len(tm.tasks))
	for _, t := range tm.tasks {
		out = append(out, t)
	}
	return out
}

// Cancel stops a task's underlying work and marks it cancelled.
func (tm *TaskManager) Cancel(id string) bool {
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return false
	}
	t.detector.Cancel()
	t.mu.Lock()
	t.status = TaskCancelled
	t.mu.Unlock()
	return true
}

// Relaunch re-spawns an equivalent task from the original task's spec. It runs
// the spec's Relaunch closure (set by the tool that spawned it — e.g. ActionTool
// re-runs the command in a fresh session). Returns an error if the task is
// unknown or not relaunchable.
func (tm *TaskManager) Relaunch(id string) (SpawnResult, error) {
	tm.mu.Lock()
	t, ok := tm.tasks[id]
	tm.mu.Unlock()
	if !ok {
		return SpawnResult{}, fmt.Errorf("task %s not found", id)
	}
	if t.Spec.Relaunch == nil {
		return SpawnResult{}, fmt.Errorf("task %s is not relaunchable", id)
	}
	return t.Spec.Relaunch()
}

// funcSettleDetector is a generic detector that runs fn in a goroutine and emits
// a single settle signal when fn returns (Completed on success, Completed+Err on
// failure), then closes. It doubles as the "generic goroutine task" detector.
type funcSettleDetector struct {
	ch     chan SettleSignal
	cancel context.CancelFunc
	detach <-chan struct{}
}

// NewFuncSettleDetector runs fn under a cancelable context and settles on return.
// An optional denseDuration overrides the default dense phase after which, if fn
// has not returned, the detector signals detach (→ async ack).
func NewFuncSettleDetector(ctx context.Context, fn func(context.Context) (string, error), denseDuration ...time.Duration) SettleDetector {
	cctx, cancel := context.WithCancel(ctx)
	d := &funcSettleDetector{ch: make(chan SettleSignal, 1), cancel: cancel}
	dd := defaultDenseDuration
	if len(denseDuration) > 0 && denseDuration[0] > 0 {
		dd = denseDuration[0]
	}
	d.detach = DetachAfter(dd, cctx.Done())
	go func() {
		out, err := fn(cctx)
		d.ch <- SettleSignal{Kind: SettleCompleted, Output: out, Err: err}
		close(d.ch)
		cancel() // stop the detach timer — the task settled
	}()
	return d
}

func (d *funcSettleDetector) Settled() <-chan SettleSignal { return d.ch }
func (d *funcSettleDetector) Detached() <-chan struct{}    { return d.detach }
func (d *funcSettleDetector) Cancel()                      { d.cancel() }

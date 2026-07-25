package agent

// Task-layer symbols moved to the agent/task sub-package (zero inbound
// coupling made it a natural boundary). All in-repo consumers now import
// the sub-package directly; these aliases remain ONLY as an external-facing
// compatibility facade (and for the agent package itself, where the `task`
// identifier is shadowed by variable names).

import (
	"context"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
)

// Types.
type (
	TaskStatus        = task.TaskStatus
	SettleKind        = task.SettleKind
	SettleSignal      = task.SettleSignal
	SettleDetector    = task.SettleDetector
	TaskSpec          = task.TaskSpec
	Task              = task.Task
	SpawnResult       = task.SpawnResult
	TaskSpawner       = task.TaskSpawner
	TaskController    = task.TaskController
	OriginSpawner     = task.OriginSpawner
	TaskManagerConfig = task.TaskManagerConfig
	TaskManager       = task.TaskManager
)

// Task status constants.
const (
	TaskRunning       = task.TaskRunning
	TaskStable        = task.TaskStable
	TaskAliveDetached = task.TaskAliveDetached
	TaskCompleted     = task.TaskCompleted
	TaskFailed        = task.TaskFailed
	TaskSuspect       = task.TaskSuspect
	TaskDead          = task.TaskDead
	TaskCancelled     = task.TaskCancelled
)

// Settle kind constants.
const (
	SettleCompleted = task.SettleCompleted
	SettleStable    = task.SettleStable
	SettleSuspect   = task.SettleSuspect
)

// Constructors and helpers.
var (
	NewTaskManager        = task.NewTaskManager
	NewFuncSettleDetector = task.NewFuncSettleDetector
	DetachAfter           = task.DetachAfter
)

// Context plumbing (used by tool/* to resolve the spawner/controller).
func WithTaskSpawner(ctx context.Context, s TaskSpawner) context.Context {
	return task.WithTaskSpawner(ctx, s)
}

func TaskSpawnerFromContext(ctx context.Context) (TaskSpawner, bool) {
	return task.TaskSpawnerFromContext(ctx)
}

func TaskControllerFromContext(ctx context.Context) (TaskController, bool) {
	return task.TaskControllerFromContext(ctx)
}

var _ = time.Duration(0) // keep time import stable for alias signatures

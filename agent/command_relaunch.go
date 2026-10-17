// 契约: docs/wiki/agent/execution-generations.md#reentry-resolution
package agent

import (
	"context"
	"fmt"

	"github.com/SpellingDragon/tagent/agent/task"
)

type commandTaskRelauncher interface {
	RelaunchTask(context.Context, task.Declarative) (task.SpawnResult, error)
}

func withCommandRelauncherForLease(ctx context.Context, lease *ExecLease) context.Context {
	if binding := lease.pinnedBinding(); binding != nil {
		for _, candidate := range binding.face.Tools {
			for candidate != nil {
				if relauncher, ok := candidate.(commandTaskRelauncher); ok {
					return task.WithCommandRelauncher(ctx, relauncher.RelaunchTask)
				}
				switch wrapper := candidate.(type) {
				case transparentUnwrapper:
					candidate = wrapper.Unwrap()
				case transparentInner:
					candidate = wrapper.Inner()
				default:
					candidate = nil
				}
			}
		}
	}
	return task.WithCommandRelauncher(ctx, func(context.Context, task.Declarative) (task.SpawnResult, error) {
		return task.SpawnResult{}, fmt.Errorf("command relaunch unavailable in execution generation %d: action tool is not declared", lease.Generation())
	})
}

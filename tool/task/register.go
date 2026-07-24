package task

import (
	"github.com/SpellingDragon/tagent/agent"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// RegisterSubTools registers the async task-management tools as built-in plain
// tools, so agents can opt into them via config (kind: tool):
//   - list_tasks       — list all tracked tasks
//   - get_task_result  — fetch a task's full result by id
//   - cancel_task      — cancel a running task by id
//   - relaunch_task    — re-run a task from its original command by id
//
// The tools are stateless; they resolve the TaskController from the invocation
// context at Call time, so a single registration works for any agent.
func RegisterSubTools() {
	agent.RegisterPlainTool("list_tasks", func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return NewListTasksTool(), nil
	})
	agent.RegisterPlainTool("get_task_result", func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return NewGetTaskResultTool(), nil
	})
	agent.RegisterPlainTool("cancel_task", func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return NewCancelTaskTool(), nil
	})
	agent.RegisterPlainTool("relaunch_task", func(agent.PlainToolFactoryConfig) (trpctool.CallableTool, error) {
		return NewRelaunchTaskTool(), nil
	})
}

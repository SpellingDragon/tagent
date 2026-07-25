// Package task provides LLM-facing tools for managing async background tasks
// tracked by the agent's TaskManager: listing, fetching full results, cancelling,
// and relaunching. The tools are stateless — they retrieve the TaskController
// from the invocation context (injected by the agent before each turn), so they
// work with whatever TaskManager the running agent owns.
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SpellingDragon/tagent/agent"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

const noControllerMsg = "任务管理当前不可用（未启用 task 层）。"

// taskIDArgs is the shared argument shape for id-taking task tools.
type taskIDArgs struct {
	TaskID string `json:"task_id"`
}

// resolveTask looks up a task by exact id, falling back to a unique id-prefix
// match (the board/list show short ids). Ambiguous prefixes resolve to nothing.
func resolveTask(ctrl agent.TaskController, id string) (*agent.Task, bool) {
	if id == "" {
		return nil, false
	}
	if tk, ok := ctrl.Get(id); ok {
		return tk, true
	}
	var match *agent.Task
	n := 0
	for _, tk := range ctrl.List() {
		if strings.HasPrefix(tk.ID, id) {
			match = tk
			n++
		}
	}
	if n == 1 {
		return match, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// list_tasks
// ---------------------------------------------------------------------------

// ListTasksTool lists all tracked tasks (active + recently settled).
type ListTasksTool struct{}

var _ tool.CallableTool = (*ListTasksTool)(nil)

// NewListTasksTool creates a list_tasks tool.
func NewListTasksTool() *ListTasksTool { return &ListTasksTool{} }

// Declaration implements tool.CallableTool.
func (t *ListTasksTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "list_tasks",
		Description: "列出当前所有后台任务（进行中与最近已结算）及其 id、状态、命令。用于查看和管理异步任务。",
		InputSchema: &tool.Schema{Type: "object", Properties: map[string]*tool.Schema{}},
	}
}

// Call implements tool.CallableTool.
func (t *ListTasksTool) Call(ctx context.Context, _ []byte) (any, error) {
	ctrl, ok := agent.TaskControllerFromContext(ctx)
	if !ok {
		return noControllerMsg, nil
	}
	tasks := ctrl.List()
	if len(tasks) == 0 {
		return "当前没有后台任务。", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个后台任务：\n", len(tasks))
	for _, tk := range tasks {
		fmt.Fprintf(&b, "- id=%s [%s] %s\n", tk.ID, tk.Status(), tk.Spec.Desc)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ---------------------------------------------------------------------------
// get_task_result
// ---------------------------------------------------------------------------

// GetTaskResultTool returns a task's full captured result.
type GetTaskResultTool struct{}

var _ tool.CallableTool = (*GetTaskResultTool)(nil)

// NewGetTaskResultTool creates a get_task_result tool.
func NewGetTaskResultTool() *GetTaskResultTool { return &GetTaskResultTool{} }

// Declaration implements tool.CallableTool.
func (t *GetTaskResultTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "get_task_result",
		Description: "按 task_id 获取某个后台任务的完整结果/输出（task_settled 通知里的结果可能被截断，用本工具拉全量）。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"task_id": {Type: "string", Description: "任务 id（可用 list_tasks 或看板中的 id，支持前缀）"},
			},
			Required: []string{"task_id"},
		},
	}
}

// Call implements tool.CallableTool.
func (t *GetTaskResultTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args taskIDArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("get_task_result: invalid args: %w", err)
	}
	ctrl, ok := agent.TaskControllerFromContext(ctx)
	if !ok {
		return noControllerMsg, nil
	}
	tk, ok := resolveTask(ctrl, args.TaskID)
	if !ok {
		return fmt.Sprintf("未找到任务 %q。", args.TaskID), nil
	}
	result := tk.Result()
	if result == "" {
		result = "(无输出)"
	}
	return fmt.Sprintf("任务 %s [%s] %s\n结果：\n%s", tk.ID, tk.Status(), tk.Spec.Desc, result), nil
}

// ---------------------------------------------------------------------------
// cancel_task
// ---------------------------------------------------------------------------

// CancelTaskTool cancels a running task.
type CancelTaskTool struct{}

var _ tool.CallableTool = (*CancelTaskTool)(nil)

// NewCancelTaskTool creates a cancel_task tool.
func NewCancelTaskTool() *CancelTaskTool { return &CancelTaskTool{} }

// Declaration implements tool.CallableTool.
func (t *CancelTaskTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "cancel_task",
		Description: "按 task_id 取消一个后台任务（终止其底层进程/会话）。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"task_id": {Type: "string", Description: "任务 id（支持前缀）"},
			},
			Required: []string{"task_id"},
		},
	}
}

// Call implements tool.CallableTool.
func (t *CancelTaskTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args taskIDArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("cancel_task: invalid args: %w", err)
	}
	ctrl, ok := agent.TaskControllerFromContext(ctx)
	if !ok {
		return noControllerMsg, nil
	}
	tk, ok := resolveTask(ctrl, args.TaskID)
	if !ok {
		return fmt.Sprintf("未找到任务 %q。", args.TaskID), nil
	}
	if !ctrl.Cancel(tk.ID) {
		return fmt.Sprintf("任务 %s 取消失败。", tk.ID), nil
	}
	return fmt.Sprintf("已取消任务 %s（%s）。", tk.ID, tk.Spec.Desc), nil
}

// ---------------------------------------------------------------------------
// relaunch_task
// ---------------------------------------------------------------------------

// RelaunchTaskTool re-runs a task from its original spec.
type RelaunchTaskTool struct{}

var _ tool.CallableTool = (*RelaunchTaskTool)(nil)

// NewRelaunchTaskTool creates a relaunch_task tool.
func NewRelaunchTaskTool() *RelaunchTaskTool { return &RelaunchTaskTool{} }

// Declaration implements tool.CallableTool.
func (t *RelaunchTaskTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "relaunch_task",
		Description: "按 task_id 基于原命令重新发起一个后台任务（例如重跑一个已死/假死的任务）。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"task_id": {Type: "string", Description: "要重跑的任务 id（支持前缀）"},
			},
			Required: []string{"task_id"},
		},
	}
}

// Call implements tool.CallableTool.
func (t *RelaunchTaskTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args taskIDArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("relaunch_task: invalid args: %w", err)
	}
	ctrl, ok := agent.TaskControllerFromContext(ctx)
	if !ok {
		return noControllerMsg, nil
	}
	tk, ok := resolveTask(ctrl, args.TaskID)
	if !ok {
		return fmt.Sprintf("未找到任务 %q。", args.TaskID), nil
	}
	res, err := ctrl.Relaunch(tk.ID)
	if err != nil {
		return fmt.Sprintf("重跑任务 %s 失败：%v", tk.ID, err), nil
	}
	if res.Task != nil {
		return fmt.Sprintf("已重跑任务（原 %s，新 %s）：%s", tk.ID, res.Task.ID, tk.Spec.Desc), nil
	}
	return fmt.Sprintf("已重跑任务 %s：%s", tk.ID, tk.Spec.Desc), nil
}

// ---------------------------------------------------------------------------
// resume_task
// ---------------------------------------------------------------------------

// ResumeTaskTool feeds new input into an alive-detached task's live session
// (the state machine's alive-detached → running edge). The resumed round
// reuses the standard dense→ACK→settle lifecycle under the SAME task id.
type ResumeTaskTool struct{}

var _ tool.CallableTool = (*ResumeTaskTool)(nil)

// NewResumeTaskTool creates a resume_task tool.
func NewResumeTaskTool() *ResumeTaskTool { return &ResumeTaskTool{} }

// Declaration implements tool.CallableTool.
func (t *ResumeTaskTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "resume_task",
		Description: "向一个存活的后台任务（alive 状态，如服务/REPL 会话）继续输入命令或指令。快命令内联返回本轮增量输出；慢响应返回 ACK，结算后以 task_settled 通知回写（同一 task id）。终态任务请用 relaunch_task。",
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"task_id": {Type: "string", Description: "目标任务 id（支持前缀）"},
				"input":   {Type: "string", Description: "继续输入的命令/指令文本"},
			},
			Required: []string{"task_id", "input"},
		},
	}
}

type resumeArgs struct {
	TaskID string `json:"task_id"`
	Input  string `json:"input"`
}

// Call implements tool.CallableTool.
func (t *ResumeTaskTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args resumeArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("resume_task: invalid args: %w", err)
	}
	if args.Input == "" {
		return "resume_task 需要非空 input。", nil
	}
	ctrl, ok := agent.TaskControllerFromContext(ctx)
	if !ok {
		return noControllerMsg, nil
	}
	tk, ok := resolveTask(ctrl, args.TaskID)
	if !ok {
		return fmt.Sprintf("未找到任务 %q。", args.TaskID), nil
	}
	res, err := ctrl.Resume(tk.ID, args.Input)
	if err != nil {
		return fmt.Sprintf("重入任务 %s 失败：%v", tk.ID, err), nil
	}
	if res.Settled {
		if res.Signal.Err != nil {
			return fmt.Sprintf("重入任务 %s 本轮执行出错：%v", tk.ID, res.Signal.Err), nil
		}
		return fmt.Sprintf("重入任务 %s 完成，本轮输出：\n%s", tk.ID, res.Signal.Output), nil
	}
	return fmt.Sprintf("已向任务 %s 继续输入，正在后台执行；结算后将以 task_settled 通知回写（同一 task id）。", tk.ID), nil
}

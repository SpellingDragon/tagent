package task // import "github.com/SpellingDragon/tagent/tool/task"

Package task provides LLM-facing tools for managing async background tasks
tracked by the agent's TaskManager: listing, cancelling, and relaunching.
The tools are stateless — they retrieve the TaskController from the invocation
context (injected by the agent before each turn), so they work with whatever
TaskManager the running agent owns. Full settled results are NOT fetched by a
tool here: task_settled events carry the full body in the event store and are
recallable by event-key ticket (stable-context- compaction D6).

FUNCTIONS

func RegisterSubTools()
    RegisterSubTools registers the async task-management tools as built-in plain
    tools, so agents can opt into them via config (kind: tool): - list_tasks
    — list all tracked tasks - cancel_task — cancel a running task by id -
    relaunch_task — re-run a task from its original command by id

    The tools are stateless; they resolve the TaskController from the invocation
    context at Call time, so a single registration works for any agent.

TYPES

type CancelTaskTool struct{}
    CancelTaskTool cancels a running task.

func NewCancelTaskTool() *CancelTaskTool
    NewCancelTaskTool creates a cancel_task tool.

func (t *CancelTaskTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements tool.CallableTool.

func (t *CancelTaskTool) Declaration() *tool.Declaration
    Declaration implements tool.CallableTool.

type ListTasksTool struct{}
    ListTasksTool lists all tracked tasks (active + recently settled).

func NewListTasksTool() *ListTasksTool
    NewListTasksTool creates a list_tasks tool.

func (t *ListTasksTool) Call(ctx context.Context, _ []byte) (any, error)
    Call implements tool.CallableTool.

func (t *ListTasksTool) Declaration() *tool.Declaration
    Declaration implements tool.CallableTool.

type RelaunchTaskTool struct{}
    RelaunchTaskTool re-runs a task from its original spec.

func NewRelaunchTaskTool() *RelaunchTaskTool
    NewRelaunchTaskTool creates a relaunch_task tool.

func (t *RelaunchTaskTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements tool.CallableTool.

func (t *RelaunchTaskTool) Declaration() *tool.Declaration
    Declaration implements tool.CallableTool.

type ResumeTaskTool struct{}
    ResumeTaskTool feeds new input into an alive-detached task's live session
    (the state machine's alive-detached → running edge). The resumed round
    reuses the standard dense→ACK→settle lifecycle under the SAME task id.

func NewResumeTaskTool() *ResumeTaskTool
    NewResumeTaskTool creates a resume_task tool.

func (t *ResumeTaskTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements tool.CallableTool.

func (t *ResumeTaskTool) Declaration() *tool.Declaration
    Declaration implements tool.CallableTool.

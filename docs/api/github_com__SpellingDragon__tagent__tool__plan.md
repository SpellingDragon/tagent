package plan // import "github.com/SpellingDragon/tagent/tool/plan"

Package plan implements the PlanAgent — a TagentAgent wrapper with custom Run
that bypasses the LLM for progress queries.

Design: follows the prototype's "Run is replaceable" pattern. PlanAgent embeds
*tagentagent.TagentAgent and overrides Run to intercept action=progress
requests, handling them via direct file I/O instead of the full ReAct loop.

TYPES

type PlanAgent struct {
	*tagentagent.TagentAgent
	// Has unexported fields.
}
    PlanAgent 在 TagentAgent 之上加一个自定义 Run：action 为 progress 时绕开模型，直接扫描 变更目录（由
    openSpecDir 拼出）得出进度摘要。 For all other actions, it delegates to the standard
    TagentAgent.Run.

func NewPlanAgent(inner *tagentagent.TagentAgent, openSpecDir string) *PlanAgent
    NewPlanAgent creates a PlanAgent wrapping the given TagentAgent. openSpecDir
    is the root directory containing the openspec/ folder.

func (pa *PlanAgent) Run(ctx context.Context, inv *trpcagent.Invocation) (<-chan *event.Event, error)
    Run implements agent.Agent. It inspects the action field from the invocation
    message and routes progress queries to direct file I/O.

type TaskItem struct {
	ID    string
	Title string
	Done  bool
}
    TaskItem represents a single task parsed from tasks.md.

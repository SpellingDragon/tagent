package prototype // import "github.com/SpellingDragon/tagent/prototype"

Package prototype contains the original 126-line tagent skeleton: intentionally
minimal and self-contained, it proves an agent can be built from an event bus,
a bounded projection, a tool registry, one model tool, a persistent loop and a
compact step.

- The production implementation in ../agent/ still follows this abstraction;

TYPES

type BaseTAgent struct {
	Run             func()
	ModelCompletion func(inputs []string) string
	Compact         func()
	OnEvents        func(event []Event) Event
	// Has unexported fields.
}
    BaseTAgent is the prototype agent: the minimal state an event-driven agent
    needs.

    - The production TagentAgent keeps the same conceptual pieces, wired

func (agent *BaseTAgent) DefaultCompact()
    DefaultCompact resets the bounded projection.

    This is the prototype version of production
    Compactor.Compact/SmartCompressor: it discards the working memory without
    touching the event bus or permanent storage, because the events have already
    flowed through eventBus.

func (agent *BaseTAgent) DefaultOnEvents(events []Event) Event
    DefaultOnEvents is the prototype event processor.

    It appends input/output events to the inputs projection, dispatches execute
    events to tools asynchronously, and finally invokes ModelCompletion if there
    is any input to respond to. In production this logic is split between:
      - ContextManager.BuildInvocation (merge external_input events)
      - ContextManager.RunFlow (call framework runner)
      - onEvent callback (append EventReference to SessionProjection)
      - framework Runner (dispatch tool execution)

func (agent *BaseTAgent) DefaultRun()
    DefaultRun is the prototype persistent event loop.

    It blocks waiting for the first event, drains all pending events
    into a batch, calls OnEvents to process the batch, and publishes
    any non-empty output back to the bus. The production equivalent is
    TagentAgent.runEventLoop, which additionally handles context cancellation,
    event merging via ContextManager.BuildInvocation, and framework ReAct
    execution via ContextManager.RunFlow.

func (agent *BaseTAgent) Input(input string)
    Input injects an external input event into the event bus.

    Corresponds to TagentAgent.InjectMessage in production.

func (agent *BaseTAgent) New()
    New initializes the prototype agent with default hooks.

func (agent *BaseTAgent) RegisterModel(model *Model)
    RegisterModel installs the model completion function.

    The prototype treats model completion as a tool that consumes the current
    inputs and returns a string. In production, model invocation is handled by
    the framework's llmagent/runner, but the conceptual role is the same.

func (agent *BaseTAgent) RegisterTool(name string, tool func(inputs []string) string)
    RegisterTool adds a callable tool.

    Tool outputs must be published back to eventBus; the loop will pick them
    up on the next iteration. This rule is preserved in production: framework
    tool results become events that flow through ContextManager.RunFlow and are
    appended to SessionProjection by the onEvent callback.

type Event struct {
	// EventType is the kind of event carried by the struct: 1 input, 2 execute, 3 output.
	EventType int
	EventData string
}
    Event is the unit of work on the event bus.

    In the production implementation this corresponds to agent.AgentEvent,
    which carries typed payloads (external_input, tool_use, etc.) instead of a
    single int-based EventType.

type Model struct {
	Completion func(inputs []string) ModelOutput
}
    Model is a function that maps inputs to a structured output.

    In production this corresponds to model.Model (the trpc-agent-go interface),
    which streams *model.Response instead of returning a simple struct.

func MockModel() *Model
    MockModel returns a deterministic model for testing the prototype.

type ModelOutput struct {
	ToolCalls []ToolCall
	Reasoning string
	Output    string
}
    ModelOutput is the mock model's structured response.

type ToolCall struct {
	Name     string
	Args     string
	Outputs  string
	Finished bool
}
    ToolCall describes a single tool invocation in the mock model output.

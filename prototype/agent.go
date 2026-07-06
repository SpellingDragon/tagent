// Package prototype contains the original 126-line tagent skeleton.
//
// This file is intentionally minimal and self-contained. It defines the core
// abstraction that the production implementation in ../agent/ still follows:
//
//   - eventBus: an ordered event queue that decouples producers from the loop
//   - inputs:   a bounded projection of the event flow (the "working memory")
//   - tools:    callable functions whose outputs are fed back into the eventBus
//   - model:    one of the tools, invoked when inputs is non-empty
//   - Run:      the persistent event loop (Pull → OnEvents → model → publish)
//   - Compact:  resetting the bounded projection without touching the event bus
//
// The prototype proves that an agent can be built from just these pieces.
// The production code maps these pieces to trpc-agent-go primitives while
// keeping the same semantics:
//
//	prototype eventBus          → agent.EventBus
//	prototype DefaultRun        → TagentAgent.runEventLoop
//	prototype OnEvents          → ContextManager.BuildInvocation + onEvent callback
//	prototype Compact           → agent.Compactor + SmartCompressor
//	prototype ModelCompletion   → framework runner.Run with llmagent
//	prototype tools["model"]    → framework LLM tool integrated by runner.Run
//	prototype tools[...]        → registered trpc-agent-go tools
//
// The three invariants documented in README.md are preserved in the production
// implementation:
//  1. inputs (SessionProjection) is a projection of the event flow
//  2. Compact only modifies the projection, never MemoryStore or EventBus
//  3. tool results and model outputs flow back through the event bus
package prototype

import (
	"sync"

	jsoniter "github.com/json-iterator/go"
)

// Event is the unit of work on the event bus.
//
// In the production implementation this corresponds to agent.AgentEvent, which
// carries typed payloads (external_input, tool_use, etc.) instead of a single
// int-based EventType.
type Event struct {
	EventType int // 1: input, 2: execute, 3: output
	EventData string
}

// BaseTAgent is the prototype agent.
//
// It demonstrates the minimal state needed for an event-driven agent:
//   - a mutex for serializing access to inputs
//   - an event bus for inbound and internal events
//   - a tool registry
//   - a bounded inputs slice (the projection)
//   - a model completion function
//   - lifecycle hooks: Run, ModelCompletion, Compact, OnEvents
//
// The production TagentAgent keeps the same conceptual pieces but wires them
// through trpc-agent-go interfaces and adds persistence/compression/A2A.
type BaseTAgent struct {
	mu              sync.Mutex
	eventBus        chan Event
	tools           map[string]func(inputs []string) string
	inputs          []string
	model           *Model
	Run             func()
	ModelCompletion func(inputs []string) string
	Compact         func()
	OnEvents        func(event []Event) Event
}

// DefaultCompact resets the bounded projection.
//
// This is the prototype version of production Compactor.Compact/SmartCompressor:
// it discards the working memory without touching the event bus or permanent
// storage, because the events have already flowed through eventBus.
func (agent *BaseTAgent) DefaultCompact() {
	agent.inputs = agent.inputs[:0]
}

// RegisterModel installs the model completion function.
//
// The prototype treats model completion as a tool that consumes the current
// inputs and returns a string. In production, model invocation is handled by
// the framework's llmagent/runner, but the conceptual role is the same.
func (agent *BaseTAgent) RegisterModel(model *Model) {
	agent.ModelCompletion = func(inputs []string) string {
		modelData, _ := jsoniter.MarshalToString(model.Completion(inputs))
		return modelData
	}
}

// RegisterTool adds a callable tool.
//
// Tool outputs must be published back to eventBus; the loop will pick them up
// on the next iteration. This rule is preserved in production: framework tool
// results become events that flow through ContextManager.RunFlow and are
// appended to SessionProjection by the onEvent callback.
func (agent *BaseTAgent) RegisterTool(name string, tool func(inputs []string) string) {
	agent.tools[name] = tool
}

// Input injects an external input event into the event bus.
//
// Corresponds to TagentAgent.InjectMessage in production.
func (agent *BaseTAgent) Input(input string) {
	agent.eventBus <- Event{EventType: 1, EventData: input}
}

// DefaultRun is the prototype persistent event loop.
//
// It blocks waiting for the first event, drains all pending events into a
// batch, calls OnEvents to process the batch, and publishes any non-empty
// output back to the bus. The production equivalent is
// TagentAgent.runEventLoop, which additionally handles context cancellation,
// event merging via ContextManager.BuildInvocation, and framework ReAct
// execution via ContextManager.RunFlow.
func (agent *BaseTAgent) DefaultRun() {
	select {
	case <-agent.eventBus:
		eventLen := len(agent.eventBus)
		batchEvents := make([]Event, eventLen)
		for i := 0; i < eventLen; i++ {
			batchEvents[i] = <-agent.eventBus
		}
		output := agent.OnEvents(batchEvents)
		if output.EventType != 0 {
			agent.eventBus <- output
		}
	}
}

// DefaultOnEvents is the prototype event processor.
//
// It appends input/output events to the inputs projection, dispatches execute
// events to tools asynchronously, and finally invokes ModelCompletion if there
// is any input to respond to. In production this logic is split between:
//   - ContextManager.BuildInvocation (merge external_input events)
//   - ContextManager.RunFlow (call framework runner)
//   - onEvent callback (append EventReference to SessionProjection)
//   - framework Runner (dispatch tool execution)
func (agent *BaseTAgent) DefaultOnEvents(events []Event) Event {
	outputEvent := Event{}
	for _, event := range events {
		switch event.EventType {
		case 1:
			agent.inputs = append(agent.inputs, event.EventData)
		case 2:
			go func() {
				output := agent.tools[event.EventData]([]string{event.EventData})
				agent.eventBus <- Event{EventType: 3, EventData: output}
			}()
		case 3:
			agent.inputs = append(agent.inputs, event.EventData)
		}
	}
	if len(agent.inputs) > 0 {
		outputEvent.EventType = 3
		outputEvent.EventData = agent.ModelCompletion(agent.inputs)
	}
	return outputEvent
}

// New initializes the prototype agent with default hooks.
func (agent *BaseTAgent) New() {
	agent.eventBus = make(chan Event)
	agent.tools = make(map[string]func(inputs []string) string)
	agent.tools["model"] = agent.ModelCompletion
	agent.Run = agent.DefaultRun
	agent.OnEvents = agent.DefaultOnEvents
	agent.Compact = agent.DefaultCompact
}

// MockModel returns a deterministic model for testing the prototype.
func MockModel() *Model {
	return &Model{
		Completion: func(inputs []string) ModelOutput {
			return ModelOutput{
				ToolCalls: []ToolCall{
					{
						Name:     "model",
						Args:     inputs[0],
						Outputs:  "mock output",
						Finished: true,
					},
				},
				Reasoning: "mock reasoning",
				Output:    "mock output",
			}
		},
	}
}

// ToolCall describes a single tool invocation in the mock model output.
type ToolCall struct {
	Name     string
	Args     string
	Outputs  string
	Finished bool
}

// ModelOutput is the mock model's structured response.
type ModelOutput struct {
	ToolCalls []ToolCall
	Reasoning string
	Output    string
}

// Model is a function that maps inputs to a structured output.
//
// In production this corresponds to model.Model (the trpc-agent-go interface),
// which streams *model.Response instead of returning a simple struct.
type Model struct {
	Completion func(inputs []string) ModelOutput
}

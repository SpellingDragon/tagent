# remote-agent-communication Specification

## Purpose

本规范定义 remote-agent-communication 能力。AgentToolWrapper SHALL hold an `agent.Agent` interface (not `*TagentAgent`), enabling both local TagentAgent and remote A2AAgent to be wrapped uniformly.

## Requirements

### Requirement: AgentToolWrapper accepts agent.Agent interface

AgentToolWrapper SHALL hold an `agent.Agent` interface (not `*TagentAgent`), enabling both local TagentAgent and remote A2AAgent to be wrapped uniformly.

#### Scenario: Local TagentAgent wrapped
- **WHEN** a TagentAgent is passed to NewAgentToolWrapper
- **THEN** the wrapper calls `agent.Run(ctx, inv)` which invokes TagentAgent.Run directly (in-process method call)

#### Scenario: Remote A2AAgent wrapped
- **WHEN** an a2aagent.A2AAgent is passed to NewAgentToolWrapper
- **THEN** the wrapper calls `agent.Run(ctx, inv)` which invokes A2AAgent.Run, sending an A2A protocol message via trpc-a2a-go

#### Scenario: Declaration uses agent.Info()
- **WHEN** AgentToolWrapper.Declaration() is called
- **THEN** the tool name is derived from `agent.Info().Name`, not from a struct field

### Requirement: Context passed via RuntimeState

AgentToolWrapper.Call SHALL serialize resolved external events into `Invocation.RunOptions.RuntimeState["external_context"]` as JSON, replacing the `IngestExternalEvents` struct field assignment.

#### Scenario: EventKey resolution to RuntimeState
- **WHEN** LLM passes event_keys in tool arguments
- **THEN** the wrapper resolves each key via `parentStore.GetEvent(key)` to obtain FullEvent
- **AND** serializes only EventKey, EventType, EventSummary into `[]ExternalContextEntry` JSON
- **AND** stores the JSON in `inv.RunOptions.RuntimeState["external_context"]`
- **AND** does NOT call `IngestExternalEvents` or `RunSimple`

#### Scenario: No event_keys provided
- **WHEN** LLM does not pass event_keys in tool arguments
- **THEN** the wrapper constructs Invocation without "external_context" in RuntimeState
- **AND** the sub-agent runs without external context (injectExternalContext is a no-op)

### Requirement: ExternalContextEntry serialization format

The external context SHALL be serialized as a JSON array of `ExternalContextEntry` structs, each containing only `event_key` (int64), `event_type` (string), and `event_summary` (string). Full event Content SHALL NOT be included.

#### Scenario: Serialization produces compact JSON
- **WHEN** two external events with EventSummary "user asked about deployment" and "agent responded with plan" are serialized
- **THEN** the RuntimeState value is a JSON array with two objects
- **AND** each object contains only event_key, event_type, event_summary fields
- **AND** the Content field is absent from the JSON

### Requirement: TagentAgent.Run reads RuntimeState

TagentAgent.Run SHALL check `inv.RunOptions.RuntimeState["external_context"]` and, if present, deserialize it into `[]FullEvent` (with Content left empty) and inject via `IngestExternalEvents` before processing the message.

#### Scenario: RuntimeState context injection
- **WHEN** Run is called with RuntimeState containing "external_context"
- **THEN** the external context entries are deserialized
- **AND** IngestExternalEvents is called with the deserialized events
- **AND** injectExternalContext prepends the event summaries to the user message
- **AND** the message is processed through the normal runner.Run pipeline

#### Scenario: RuntimeState absent — struct field fallback
- **WHEN** Run is called without "external_context" in RuntimeState
- **AND** pendingExternalEvents struct field is non-empty
- **THEN** the struct field events are used for context injection (existing behavior preserved)

#### Scenario: Both paths empty
- **WHEN** Run is called with no RuntimeState context and no pending external events
- **THEN** the message is processed without external context injection

### Requirement: Remote sub-agent via ToolRef.Remote

When `ToolRef.Remote` is configured with a URL, the system SHALL create an `a2aagent.A2AAgent` with `WithAgentCardURL(url)` and `WithTransferStateKey("external_context")` instead of creating a local TagentAgent.

#### Scenario: Remote agent creation
- **WHEN** ToolRef.Remote.URL is "http://knowledge-service:8088"
- **THEN** an A2AAgent is created with WithAgentCardURL("http://knowledge-service:8088")
- **AND** WithTransferStateKey("external_context") is set
- **AND** the A2AAgent is wrapped by AgentToolWrapper
- **AND** no local TagentAgent is created for this tool ref

#### Scenario: Local agent when Remote is nil
- **WHEN** ToolRef.Remote is nil
- **THEN** the existing factory path creates a local TagentAgent
- **AND** the TagentAgent is wrapped by AgentToolWrapper

### Requirement: A2A metadata automatic mapping

The A2A communication chain SHALL transfer external_context from client RuntimeState to server RuntimeState automatically, without manual hooks.

#### Scenario: Client-side metadata injection
- **WHEN** A2AAgent.Run is called with RuntimeState containing "external_context"
- **THEN** buildA2AMessage copies the value into `message.Metadata["external_context"]`
- **AND** the A2A message is sent to the remote server

#### Scenario: Server-side RuntimeState extraction
- **WHEN** the A2A server receives a message with Metadata containing "external_context"
- **THEN** server.go automatically calls `agent.WithRuntimeState(message.Metadata)`
- **AND** the resulting Invocation.RunOptions.RuntimeState contains "external_context"
- **AND** TagentAgent.Run reads and injects the external context

### Requirement: TagentAgent as A2A Server

The system SHALL provide `NewA2AServer(ta *TagentAgent, host string)` that creates an A2A server exposing the TagentAgent via A2A protocol.

#### Scenario: A2A server startup
- **WHEN** NewA2AServer is called with a TagentAgent and host "0.0.0.0:8088"
- **THEN** an A2A server is created via `a2a.New(WithAgent(ta, true), WithHost(host))`
- **AND** the server can be started with `server.Start(host)`
- **AND** the TagentAgent's agent card is published at `/.well-known/agent.json`

#### Scenario: Remote request processing
- **WHEN** a remote A2A client sends a message to the server
- **THEN** the server converts the A2A message to an Invocation
- **AND** metadata is automatically mapped to RuntimeState
- **AND** TagentAgent.Run is called with the Invocation
- **AND** the response event stream is converted back to A2A protocol

### Requirement: Configuration layer separation

tagent YAML configuration SHALL focus on agent definition (model, hyperparameters, prompts), while communication configuration (A2A URL) is declared as a `remote` field within ToolRef.

#### Scenario: tagent YAML defines agent properties
- **WHEN** a tool agent is configured in tagent YAML
- **THEN** the configuration includes model, prompt, max_tool_iterations, temperature
- **AND** the `remote` field, if present, contains only `url`

#### Scenario: trpc communication options derived from remote.url
- **WHEN** ToolRef.Remote.URL is set
- **THEN** tagent.go creates A2AAgent with WithAgentCardURL(url) and WithTransferStateKey("external_context")
- **AND** no trpc_go.yaml or separate communication config file is required

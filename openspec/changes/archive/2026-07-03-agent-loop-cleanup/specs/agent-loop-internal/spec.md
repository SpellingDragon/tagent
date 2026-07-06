## ADDED Requirements

### Requirement: tool_use dispatched on consumption not production

AgentLoop SHALL dispatch tool_use events when they are consumed from the EventBus (in the main Run loop), not when they are produced in handleResponse.

handleResponse SHALL only publish tool_use events to the bus and invoke onEvent for the assistant response. It SHALL NOT call dispatchToolUse.

The main Run loop SHALL scan each pulled batch for tool_use events and dispatch them before calling Preprocessor.Process.

#### Scenario: tool_use consumed from bus triggers dispatch

- **WHEN** AgentLoop pulls a batch containing a tool_use event
- **THEN** the tool_use event is dispatched to the appropriate tool handler via goroutine
- **AND** Preprocessor.Process is called with the remaining external_input events
- **AND** shouldCallModel is false when the batch contains only tool_use events

#### Scenario: handleResponse does not dispatch

- **WHEN** model returns a response with tool_calls
- **THEN** handleResponse publishes tool_use events to the bus
- **AND** handleResponse invokes onEvent for the assistant response
- **AND** handleResponse does NOT call dispatchToolUse

#### Scenario: mixed batch with tool_use and external_input

- **WHEN** AgentLoop pulls a batch containing both tool_use and external_input events
- **THEN** tool_use events are dispatched via goroutine
- **AND** external_input events are persisted via onEvent
- **AND** Preprocessor.Process is called and shouldCallModel is true

### Requirement: Preprocessor has no session field

Preprocessor SHALL NOT maintain a session field. Process SHALL receive the session as a parameter.

#### Scenario: Process uses parameter session

- **WHEN** Process is called with a session parameter
- **THEN** it builds messages from that session's Events
- **AND** it does not reference any internal session field

### Requirement: AgentLoop has no dead config fields

AgentLoopConfig SHALL NOT include Session or SessionSvc fields. Session attachment SHALL use SetSession method only.

#### Scenario: AgentLoopConfig without Session field

- **WHEN** AgentLoop is created via NewAgentLoop
- **THEN** the config does not accept Session or SessionSvc fields
- **AND** session is attached via SetSession method call

### Requirement: Run creates Preprocessor from config not private fields

TagentAgent.Run SHALL create a Preprocessor for sub-agent invocations using ta.config fields, not by accessing the parent Preprocessor's private fields.

#### Scenario: Run creates independent Preprocessor

- **WHEN** Run is called for a sub-agent invocation
- **THEN** a new Preprocessor is created using ta.config.MaxTokens and ta.config.CompressThreshold
- **AND** no private fields of ta.preprocessor are accessed

### Requirement: Run has no legacy runner.Run fallback

TagentAgent.Run SHALL NOT fall back to runner.Run when preprocessor or config is nil.

#### Scenario: Run without fallback

- **WHEN** Run is called on a TagentAgent with valid config
- **THEN** it creates an independent EventBus + AgentLoop and runs it
- **AND** it does not call ta.runner.Run under any circumstances

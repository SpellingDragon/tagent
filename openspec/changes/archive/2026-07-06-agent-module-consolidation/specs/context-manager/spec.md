## ADDED Requirements

### Requirement: ContextManager creates unified Runner with LLMAgent + Plugins

`ContextManager` SHALL create the sole `runner.Runner` instance. The Runner SHALL be configured with:
1. `LLMAgent` (containing `SmartCompressor` and `Compactor` as BeforeModel callbacks).
2. `MemoryPlugin` (registered via `runner.WithPlugins` — framework automatically calls `OnEvent` for each event).
3. `SummaryPlugin` (registered via `runner.WithPlugins`).
4. `SessionService` (registered via `runner.WithSessionService`).

The framework Runner internally performs `sessionService.AppendEvent` (runner.go:769/944) and `Plugin.OnEvent` (runner.go:764/818) for every event. `TagentAgent` SHALL NOT create a separate Runner.

#### Scenario: ContextManager Runner handles all persistence

- **WHEN** `ContextManager.RunFlow` calls `runner.Run` and the framework produces events
- **THEN** the framework Runner automatically calls `MemoryPlugin.OnEvent` (writes MemoryStore + StateDelta)
- **AND** the framework Runner automatically calls `sessionService.AppendEvent` (appends to session.Events)
- **AND** `makeOnEventCallback` does NOT call `memPlugin.OnEvent` or `sessionSvc.AppendEvent`

### Requirement: makeOnEventCallback only does projection.Append

`makeOnEventCallback` SHALL only perform `projection.Append` via `BuildEventReference`. It SHALL NOT call `memPlugin.OnEvent` (handled by framework Plugin) or `sessionSvc.AppendEvent` (handled by framework Runner).

#### Scenario: onEvent appends to projection only

- **WHEN** `onEvent` is called with a framework event
- **THEN** `projection.Append` is called (via `BuildEventReference`)
- **AND** `memPlugin.OnEvent` is NOT called
- **AND** `sessionSvc.AppendEvent` is NOT called

### Requirement: ContextManager provides unified message building and flow execution

`ContextManager` SHALL be the single component responsible for:
1. Building `[]model.Message` from `[]memory.EventReference`.
2. Injecting `[evt_KEY|type]` prefixes.
3. Determining `ShouldCallModel` from the bus batch.
4. Building a merged `model.Message` from a batch of `AgentEvent`.
5. Registering `SmartCompressor` and `Compactor` as ordered `BeforeModel` callbacks.
6. Executing `runner.Run` and forwarding events to `outputCh` and `EventBus`.

#### Scenario: ContextManager builds messages from projection

- **WHEN** `BuildMessages` is called with `EventReference` slice
- **THEN** recent references are resolved via `MemoryStore.GetEvent`
- **AND** older references use `EventSummary`
- **AND** `[evt_KEY|type]` prefixes are injected

#### Scenario: ContextManager executes framework Flow

- **WHEN** `RunFlow` is called with a message
- **THEN** `runner.Run` is invoked
- **AND** each event triggers `onEvent` for `projection.Append` (only)
- **AND** final responses are written back to `EventBus` with `Source == "agent_output"`

#### Scenario: BeforeModel callbacks in order

- **WHEN** `ContextManager` constructs the LLMAgent
- **THEN** `SmartCompressor` is registered first
- **AND** `Compactor` is registered second

### Requirement: SmartCompressor uses injected TokenCounter

`SmartCompressor` SHALL accept a `TokenCounter` via `WithTokenCounter` option. It SHALL NOT call `NewDefaultTokenCounter()` internally.
## ADDED Requirements

### Requirement: ContextManager creates unified Runner with LLMAgent + Plugins

`ContextManager` SHALL create the sole `runner.Runner` instance. The Runner SHALL be configured with:
1. `LLMAgent` (containing `SmartCompressor` and `Compactor` as BeforeModel callbacks).
2. `MemoryPlugin` (registered via `runner.WithPlugins` — framework automatically calls `OnEvent` for each event).
3. `SummaryPlugin` (registered via `runner.WithPlugins`).
4. `SessionService` (registered via `runner.WithSessionService`).

`TagentAgent` SHALL NOT create a separate `identityOnlyAgent` Runner. The `TagentAgent.runner` field SHALL NOT exist. `Close()` SHALL delegate to `ContextManager.Close()`. `Runner()` SHALL return `ContextManager.runner` or SHALL NOT exist if unused.

#### Scenario: ContextManager Runner triggers MemoryPlugin.OnEvent

- **WHEN** `ContextManager.RunFlow` calls `runner.Run` and the framework produces events
- **THEN** the framework Runner automatically calls `MemoryPlugin.OnEvent` via the Plugin mechanism
- **AND** MemoryStore is written with FullEvent + StateDelta
- **AND** `makeOnEventCallback` does NOT also call `memPlugin.OnEvent`

#### Scenario: TagentAgent Close delegates to ContextManager

- **WHEN** `TagentAgent.Close()` is called
- **THEN** `ContextManager.Close()` is called
- **AND** the unified Runner is closed (releasing LLMAgent + Plugin + SessionService resources)

### Requirement: ContextManager provides unified message building and flow execution

`ContextManager` SHALL be the single component responsible for:
1. Building `[]model.Message` from `[]memory.EventReference`.
2. Injecting `[evt_KEY|type]` prefixes.
3. Determining `ShouldCallModel` from the bus batch.
4. Building a merged `model.Message` from a batch of `AgentEvent`.
5. Registering `SmartCompressor` and `Compactor` as ordered `BeforeModel` callbacks.
6. Executing `runner.Run` and forwarding events to `outputCh` and `EventBus`.

#### Scenario: ContextManager builds messages from projection

- **WHEN** `BuildMessages` is called with `EventReference` slice
- **THEN** recent references are resolved via `MemoryStore.GetEvent`
- **AND** older references use `EventSummary`
- **AND** `[evt_KEY|type]` prefixes are injected

#### Scenario: ContextManager executes framework Flow

- **WHEN** `RunFlow` is called with a message
- **THEN** `runner.Run` is invoked
- **AND** each event triggers `onEvent` for `sessionSvc.AppendEvent` + `projection.Append`
- **AND** final responses are written back to `EventBus` with `Source == "agent_output"`
- **AND** `MemoryPlugin.OnEvent` is handled by the unified Runner's Plugin mechanism (not by `onEvent`)

#### Scenario: BeforeModel callbacks in order

- **WHEN** `ContextManager` constructs the LLMAgent
- **THEN** `SmartCompressor` is registered first
- **AND** `Compactor` is registered second
- **AND** Compactor uses `ContextManager.BuildMessages` (not temporary construction)

### Requirement: makeOnEventCallback does not call MemoryPlugin.OnEvent

`makeOnEventCallback` SHALL only perform `sessionSvc.AppendEvent` and `projection.Append`. MemoryStore writes are handled by the unified Runner's Plugin mechanism.

#### Scenario: onEvent appends to session and projection only

- **WHEN** `onEvent` is called with a framework event
- **THEN** `sessionSvc.AppendEvent` is called
- **AND** `projection.Append` is called
- **AND** `memPlugin.OnEvent` is NOT called

### Requirement: SmartCompressor uses injected TokenCounter

`SmartCompressor` SHALL accept a `TokenCounter` via `WithTokenCounter` option. It SHALL NOT call `NewDefaultTokenCounter()` internally.

#### Scenario: SmartCompressor uses injected TokenCounter

- **WHEN** `Compress` estimates tokens
- **THEN** it uses the injected `TokenCounter`
- **AND** does NOT call `NewDefaultTokenCounter()`

## ADDED Requirements

### Requirement: contextmgr package encapsulates context management

A new `contextmgr` package SHALL contain: `context_manager.go` (ContextManager, BeforeModel callback chain), `smart_compress.go` (SmartCompressor), `task_segmenter.go` (TaskSegmenter), `compactor.go` (Compactor, split from task_segmenter), `chunk_splitter.go` (ChunkSplitter), `plan_progress_tracker.go` (PlanProgressTracker). The package SHALL import `agent` for EventBus, SessionProjection, and AgentEvent types. `newContextManagerFromConfig` SHALL remain in the `agent` package (it assembles ContextManager + TagentAgent).

#### Scenario: contextmgr package structure

- **WHEN** listing source files in the `contextmgr/` directory
- **THEN** these files SHALL exist: `context_manager.go`, `smart_compress.go`, `task_segmenter.go`, `compactor.go`, `chunk_splitter.go`, `plan_progress_tracker.go`
- **AND** the package SHALL import `github.com/SpellingDragon/tagent/agent`

#### Scenario: Compactor in separate file

- **WHEN** inspecting `contextmgr/compactor.go`
- **THEN** it SHALL contain the `Compactor` type and `NewCompactor` function
- **AND** `contextmgr/task_segmenter.go` SHALL NOT contain the `Compactor` type

### Requirement: agent package retains core + toolwrap + rl

The `agent` package SHALL retain: `tagent_agent.go`, `event_bus.go`, `projection.go`, `meditation.go`, `tool_agent.go`, `output_limit_tool.go`, `trajectory_recorder.go`, `http_api.go`. Context management files SHALL be moved to `contextmgr`. No new packages SHALL be created for tool wrapping or RL integration.

#### Scenario: agent package file list after split

- **WHEN** listing source files in the `agent/` directory
- **THEN** these files SHALL exist: `tagent_agent.go`, `event_bus.go`, `projection.go`, `meditation.go`, `tool_agent.go`, `output_limit_tool.go`, `trajectory_recorder.go`, `http_api.go`
- **AND` these files SHALL NOT exist: `context_manager.go`, `smart_compress.go`, `task_segmenter.go`, `chunk_splitter.go`, `plan_progress_tracker.go`

### Requirement: no circular dependencies

The dependency graph SHALL be: `contextmgr` → `agent`; `agent` does NOT import `contextmgr`. The `tagent` root package imports both. No import cycles.

#### Scenario: go build succeeds

- **WHEN** running `go build ./...`
- **THEN** the build SHALL succeed without "import cycle" errors

### Requirement: cross-package symbols exported

Types referenced from `agent` package in `contextmgr` SHALL be exported: `EventBus`, `AgentEvent`, `NewExternalInputEvent`, `SessionProjection`, `NewSessionProjection`, `BuildEventReference`, `Closer`, `CompressConfig`, `TagentConfig`, `MeditationConfig`, `FrameworkPrompt`. Types referenced from `contextmgr` in `agent` SHALL be exported: `ContextManager`, `ContextManagerConfig`, `NewContextManager`, `SmartCompressor`, `NewSmartCompressor`, `Compactor`, `NewCompactor`, `TokenCounter`, `DefaultTokenCounter`, `NewDefaultTokenCounter`, `TaskSegment`, `SegmentMessages`, `SmartCompressorOption`, `PlanProgressTracker`, `NewPlanProgressTracker`.

#### Scenario: cross-package references compile

- **WHEN** `agent/tagent_agent.go` references `contextmgr.NewContextManager`
- **THEN** the reference SHALL compile without error
## ADDED Requirements

### Requirement: agent package contains only core event loop components

The `agent` package SHALL contain only: `tagent_agent.go` (TagentAgent lifecycle, runEventLoop, InjectMessage, Run, StartLoop), `event_bus.go` (EventBus, AgentEvent), `projection.go` (SessionProjection, BuildEventReference), `meditation.go` (MeditationManager). Context management, tool wrapping, and RL integration SHALL be moved to separate packages. `FrameworkPrompt` constant SHALL be exported and remain in the agent package.

#### Scenario: agent package file list

- **WHEN** listing files in the `agent/` directory
- **THEN** only these source files SHALL exist: `tagent_agent.go`, `event_bus.go`, `projection.go`, `meditation.go`
- **AND** `FrameworkPrompt` SHALL be exported (capitalized)

### Requirement: contextmgr package encapsulates context management

A new `contextmgr` package SHALL contain: `context_manager.go` (ContextManager, BeforeModel callback chain), `smart_compress.go` (SmartCompressor), `task_segmenter.go` (TaskSegmenter), `compactor.go` (Compactor, split from task_segmenter), `chunk_splitter.go` (ChunkSplitter), `plan_progress_tracker.go` (PlanProgressTracker). The package SHALL import `agent` for EventBus, SessionProjection, and AgentEvent types. `newContextManagerFromConfig` SHALL remain in the `agent` package (it assembles ContextManager + TagentAgent).

#### Scenario: contextmgr package structure

- **WHEN** listing files in the `contextmgr/` directory
- **THEN** these source files SHALL exist: `context_manager.go`, `smart_compress.go`, `task_segmenter.go`, `compactor.go`, `chunk_splitter.go`, `plan_progress_tracker.go`
- **AND** the package SHALL import `github.com/SpellingDragon/tagent/agent`

### Requirement: toolwrap package encapsulates tool wrapping

A new `toolwrap` package SHALL contain: `tool_agent.go` (AgentToolWrapper, ExternalContextEntry, RegisterToolAgent), `output_limit_tool.go` (OutputLimitTool). The package SHALL import `agent` for SessionProjection and EventBus types.

#### Scenario: toolwrap package structure

- **WHEN** listing files in the `toolwrap/` directory
- **THEN** these source files SHALL exist: `tool_agent.go`, `output_limit_tool.go`
- **AND** the package SHALL import `github.com/SpellingDragon/tagent/agent`

### Requirement: rl package encapsulates RL integration

A new `rl` package SHALL contain: `trajectory_recorder.go` (TrajectoryRecorder), `http_api.go` (HTTPAPI). The package SHALL import `agent` for TagentAgent type. `TrajectoryRecorder` SHALL be injected via agent option pattern.

#### Scenario: rl package structure

- **WHEN** listing files in the `rl/` directory
- **THEN** these source files SHALL exist: `trajectory_recorder.go`, `http_api.go`
- **AND** the package SHALL import `github.com/SpellingDragon/tagent/agent`

### Requirement: no circular dependencies between packages

The dependency graph SHALL be: `tagent` (root) → `agent`, `contextmgr`, `toolwrap`, `rl`; `contextmgr` → `agent`; `toolwrap` → `agent`; `rl` → `agent`. No package SHALL import a package that transitively imports it.

#### Scenario: go build succeeds

- **WHEN** running `go build ./...`
- **THEN** the build SHALL succeed without "import cycle" errors

### Requirement: all types and functions accessible across packages are exported

Any type, function, constant, or variable referenced from a different package SHALL be exported (capitalized). This includes: `FrameworkPrompt`, `CompressConfig`, `TagentConfig`, `MeditationConfig`, `Closer` (in agent); `ContextManager`, `ContextManagerConfig`, `SmartCompressor`, `Compactor`, `PlanProgressTracker`, `TokenCounter` (in contextmgr); `AgentToolWrapper`, `OutputLimitTool`, `ExternalContextKey`, `RegisterToolAgent`, `ToolAgentFactory` (in toolwrap); `TrajectoryRecorder`, `HTTPAPI` (in rl).

#### Scenario: cross-package references compile

- **WHEN** `tagent.go` references `contextmgr.NewContextManager`
- **THEN** the reference SHALL compile without error
- **AND** `tagent.go` SHALL import `github.com/SpellingDragon/tagent/contextmgr`

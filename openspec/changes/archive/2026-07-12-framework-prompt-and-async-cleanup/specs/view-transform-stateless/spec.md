## ADDED Requirements

### Requirement: AsyncTaskChecker removed from Run()

The `AsyncTaskChecker` interface, `RegisterAsyncTaskChecker`, `hasPendingAsyncTasks`, and the `persistentBus` temporary redirection in `Run()` SHALL be removed. `Run()` SHALL return immediately after the first final response (original sub-agent single-turn semantics), without checking for pending async tasks.

#### Scenario: Run returns immediately on final response

- **WHEN** the sub-agent produces a final response (no tool_calls)
- **THEN** Run() SHALL proceed to drain mode and return
- **AND** SHALL NOT check for pending async tasks

### Requirement: handleStateChange injects to entry agent

`ActionTool.SetMessageInjector` SHALL be called with the entry agent (tagent main agent) as the injector target, not the action sub-agent. This ensures tmux completion events are injected into the entry agent's persistentBus, which is consumed by the entry agent's `runEventLoop`.

#### Scenario: tmux completion reaches entry agent

- **WHEN** a tmux session completes
- **AND** handleStateChange calls InjectMessage
- **THEN** the event SHALL be published to the entry agent's persistentBus
- **AND** the entry agent's runEventLoop SHALL pull it via bus.Pull
- **AND** InjectBusInputs SHALL inject it into the next LLM request

#### Scenario: action sub-agent does not block on async tasks

- **WHEN** action sub-agent's LLM sees {status:"waiting_async_response"} and produces a final response
- **THEN** Run() SHALL return immediately
- **AND** the final response SHALL be delivered to the entry agent as tool result
- **AND** the entry agent SHALL continue its ReAct loop (can call other tools or respond to user)

## ADDED Requirements

### Requirement: tagent framework prompt injected before user system prompt

ContextManager SHALL prepend a framework runtime description before the user-configured system prompt (from AGENTS.md/SOUL.md/etc). The framework prompt describes: (1) async tool execution semantics and `[action_tool_result]` events, (2) `[evt_KEY|type]` event identifiers and recall tool usage, (3) context compression and `[context_compress]` summaries. This ensures the LLM understands tagent's event-driven mechanisms without requiring users to manually document them.

#### Scenario: Framework prompt prepended

- **WHEN** a TagentAgent is created with a user system prompt
- **THEN** the effective system prompt SHALL be: framework prompt + "\n\n" + user system prompt
- **AND** the framework prompt SHALL describe async tool, event identifiers, and context compression

#### Scenario: Framework prompt applies to all agents

- **WHEN** both the entry agent and sub-agents (action, knowledge, recall) are created
- **THEN** each agent's ContextManager SHALL prepend the framework prompt to its respective system prompt

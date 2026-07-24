# event-key-visibility Specification

## Purpose

本规范定义 event-key-visibility 能力。ContextIntervention.BeforeModel SHALL inject event_key prefixes into message content by positionally matching args.Request.Messages to Session.Events.

## Requirements

### Requirement: Event key injection in BeforeModel

ContextIntervention.BeforeModel SHALL inject event_key prefixes into message content by positionally matching args.Request.Messages to Session.Events. The prefix format SHALL be `[evt_<KEY>|<type>] ` prepended to the original message content.

The injection SHALL be idempotent: if a message's content already starts with `[evt_`, the injection SHALL be skipped for that message. This prevents duplicate prefixes when LLM outputs imitate the prefix format and those outputs are subsequently read back from session.Events by ContentRequestProcessor.

#### Scenario: First-time injection adds prefix

- **WHEN** a message with content "hello" is processed by InjectEventKeys
- **AND** the message does not already start with `[evt_`
- **THEN** the content SHALL become `[evt_1234|external_input] hello`

#### Scenario: Idempotent injection skips existing prefix

- **WHEN** a message with content `[evt_1234|external_input] hello` is processed by InjectEventKeys
- **THEN** the content SHALL remain unchanged (no duplicate prefix added)

#### Scenario: LLM output with imitated prefix is not double-injected

- **WHEN** LLM outputs `[evt_1234|external_input] 我来帮你获取` and this message is stored in session.Events
- **AND** the next ReAct iteration's ContentRequestProcessor reads this message from session.Events
- **AND** InjectEventKeys processes this message
- **THEN** the prefix SHALL NOT be duplicated; the message retains a single `[evt_` prefix

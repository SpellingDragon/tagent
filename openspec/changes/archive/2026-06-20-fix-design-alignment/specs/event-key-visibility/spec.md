## ADDED Requirements

### Requirement: Event key injection in BeforeModel

ContextIntervention.BeforeModel SHALL inject event_key prefixes into message content by positionally matching args.Request.Messages to Session.Events. The prefix format SHALL be `[evt_<KEY>|<type>] ` prepended to the original message content.

#### Scenario: Normal injection with matching messages and events

- **WHEN** BeforeModel is called with 5 messages (1 system + 4 user/assistant) and Session has 4 events with event_keys
- **THEN** system message is skipped, 4 user/assistant messages get `[evt_<KEY>|<type>]` prefix, collectCompressedKeys can parse keys from prefixed content

#### Scenario: Tool result messages are skipped

- **WHEN** a message with Role=tool appears in the message list
- **THEN** the tool message is not given a prefix (it belongs to the previous assistant event, not a separate event)

#### Scenario: Invocation or Session is nil

- **WHEN** inv is nil or inv.Session is nil
- **THEN** no prefixes are injected, collectCompressedKeys returns empty slice (degraded behavior, no crash)

#### Scenario: More messages than events

- **WHEN** message count exceeds event count (e.g., InjectMessage added extra messages)
- **THEN** unmatched messages are skipped without prefix injection (safe degradation)

### Requirement: collectCompressedKeys extracts keys from prefixed messages

SmartCompressor.collectCompressedKeys SHALL parse `[evt_<KEY>|<type>]` prefixes from message content to extract event_keys of compressed segments. This is the existing parseEventKeyFromPrefix logic, which now works because BeforeModel injects prefixes.

#### Scenario: Compressed segments contain event_keys

- **WHEN** old segments contain messages with `[evt_12345|agent_output]` prefix
- **THEN** collectCompressedKeys returns []int64{12345}

#### Scenario: Duplicate keys are deduplicated

- **WHEN** multiple messages in old segments have the same event_key prefix
- **THEN** collectCompressedKeys returns each key only once

### Requirement: buildCompressEvent outputs key list

buildCompressEvent SHALL include the compressed event key list in the context_compress event content when keys are non-empty. The format SHALL be: `[context_compress] 压缩了 N 个对话片段，被压缩的事件 key 列表: [k1, k2, ...]`

#### Scenario: Keys available

- **WHEN** collectCompressedKeys returns [12345, 12346]
- **THEN** compress event content includes `被压缩的事件 key 列表: [12345, 12346]`

#### Scenario: Keys empty (degraded)

- **WHEN** collectCompressedKeys returns empty (no prefixes injected)
- **THEN** compress event content includes `压缩了 N 个对话片段` without key list (current behavior)

### Requirement: LLM can pass event_keys to AgentToolWrapper

AgentToolWrapper.Declaration SHALL declare event_keys parameter (array of Snowflake int64). AgentToolWrapper.Call SHALL resolve event_keys to full events from parent MemStore and inject via IngestExternalEvents. This chain is activated when LLM sees event_key prefixes in its context. The LLM explicitly selects and passes event_keys — there is no framework-level auto-injection from StateDelta.

#### Scenario: LLM passes event_keys to sub-agent

- **WHEN** LLM sees `[evt_12345|agent_output]` in its context and calls a sub-agent tool with event_keys=[12345]
- **THEN** AgentToolWrapper fetches FullEvent 12345 from parent MemStore and calls IngestExternalEvents

#### Scenario: LLM does not pass event_keys

- **WHEN** LLM calls a sub-agent tool without event_keys
- **THEN** AgentToolWrapper runs the sub-agent without external context (current behavior)

#### Scenario: event_keys from compressed events

- **WHEN** LLM sees a context_compress event containing key list [12345, 12346] and passes those keys to a sub-agent
- **THEN** AgentToolWrapper fetches both FullEvents from parent MemStore, enabling recall of compressed-away context

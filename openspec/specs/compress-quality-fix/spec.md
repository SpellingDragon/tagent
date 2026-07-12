## ADDED Requirements

### Requirement: generateSummary splits and re-summarizes when result exceeds target

When the LLM-generated summary exceeds `targetChars * 1.5`, `generateSummary` SHALL split the original segments into two sub-batches and independently summarize each with halved targetChars. The two sub-summaries SHALL be concatenated. If a sub-summary still exceeds its target (recursion depth limit: 2), it SHALL be hard-truncated to targetChars. The final summary message SHALL have Role=assistant. This ensures compression always reduces token count (invariant 1).

#### Scenario: LLM returns oversized summary, triggers re-summarization

- **WHEN** generateSummary receives a 59629-char summary from the LLM
- **AND** targetChars is 6759
- **THEN** generateSummary SHALL split the original segments into 2 sub-batches
- **AND** SHALL call generateSummary on each sub-batch with targetChars/2
- **AND** SHALL concatenate the two sub-summaries
- **AND** the concatenated result SHALL NOT exceed targetChars * 1.5

#### Scenario: Re-summarization still oversized, hard truncate

- **WHEN** re-summarization at recursion depth 2 still produces oversized result
- **THEN** the result SHALL be hard-truncated to targetChars
- **AND** a truncation marker SHALL be appended

#### Scenario: LLM summary within target

- **WHEN** generateSummary receives a 6000-char summary
- **AND** targetChars is 6759
- **THEN** the result SHALL NOT be split or truncated

#### Scenario: Summary message Role is assistant

- **WHEN** summarizeBatches wraps a summary into a message
- **THEN** the message Role SHALL be model.RoleAssistant

### Requirement: resolveReferenceToMessage infers Role from EventType

When `full.Response == nil` (event has no LLM response), `resolveReferenceToMessage` SHALL infer the message Role from `ref.EventType` using a deterministic mapping: external_input→user, agent_output→assistant, action_command→tool, thinking_plan→assistant. If `ref.EventType` is also empty, SHALL default to RoleUser (safe degradation). This SHALL NOT produce messages with empty Role.

#### Scenario: Event without Response gets correct Role

- **WHEN** GetEvent returns a FullEvent with Response=nil
- **AND** the EventReference has EventType="external_input"
- **THEN** the message Role SHALL be "user"
- **AND** the message Content SHALL be ref.EventSummary

#### Scenario: EventReference has empty EventType and Role

- **WHEN** both ref.EventType and ref.Role are empty
- **THEN** the message Role SHALL default to "user"

### Requirement: BuildEventReference infers Role when Response is nil

`BuildEventReference` SHALL set `ref.Role` from `evt.StateDelta["event_type"]` when `evt.Response == nil`, using the same EventType→Role mapping. This ensures all EventReferences have a non-empty Role.

#### Scenario: Event without Response gets Role from EventType

- **WHEN** BuildEventReference processes an event with StateDelta["event_type"]="external_input"
- **AND** evt.Response is nil
- **THEN** ref.Role SHALL be "user"

### Requirement: findPendingUserMessage deduplicates by event key

Before appending a pending user message to the result, `Compress` SHALL parse the event key from the message's `[evt_KEY|type]` prefix. It SHALL then scan recentSegments for any user message containing the same event key prefix. If found, the duplicate SHALL NOT be appended.

#### Scenario: Pending user message already in recent segments (same event key)

- **WHEN** findPendingUserMessage returns a message with prefix `[evt_1297370957781938176|external_input]`
- **AND** recentSegments contains a user message with the same `[evt_1297370957781938176|external_input]` prefix
- **THEN** the message SHALL NOT be appended to result

#### Scenario: Pending user message not in recent segments (different event key)

- **WHEN** findPendingUserMessage returns a message with prefix `[evt_9999|external_input]`
- **AND** recentSegments does not contain any message with `[evt_9999|` prefix
- **THEN** the message SHALL be appended to result

#### Scenario: Pending user message has no event key prefix

- **WHEN** findPendingUserMessage returns a message without `[evt_KEY|type]` prefix
- **THEN** the message SHALL be appended to result (no key to deduplicate against)

### Requirement: resolveReferenceToMessage logs distinguish error types

The warn log in `resolveReferenceToMessage` SHALL distinguish between "GetEvent returned error" and "GetEvent succeeded but Response is nil".

#### Scenario: GetEvent returns error

- **WHEN** GetEvent returns (nil, error)
- **THEN** log SHALL say "GetEvent failed for key=N: <error>"

#### Scenario: GetEvent succeeds but no Response

- **WHEN** GetEvent returns (full, nil) but full.Response is nil
- **THEN** log SHALL say "event key=N has no Response, falling back to EventType inference"
## ADDED Requirements

### Requirement: summarizeBatch truncates oversized LLM summary

When the LLM-generated summary exceeds `targetChars * 1.5`, `summarizeBatch` SHALL truncate the result to `targetChars` and append `...(摘要已截断，原始长度 N 字符)`. This ensures compression always reduces token count (invariant 1).

#### Scenario: LLM returns oversized summary

- **WHEN** summarizeBatch receives a 59629-char summary from the LLM
- **AND** targetChars is 6759
- **THEN** the result SHALL be truncated to 6759 chars + truncation marker
- **AND** the total result length SHALL NOT exceed targetChars * 1.5

#### Scenario: LLM summary within target

- **WHEN** summarizeBatch receives a 6000-char summary
- **AND** targetChars is 6759
- **THEN** the result SHALL NOT be truncated

### Requirement: resolveReferenceToMessage infers Role from EventType

When `full.Response == nil` (event has no LLM response), `resolveReferenceToMessage` SHALL infer the message Role from `ref.EventType` using a deterministic mapping: external_input→user, agent_output→assistant, action_command→tool, thinking_plan→assistant. If `ref.EventType` is also empty, SHALL default to RoleUser (safe degradation). This SHALL NOT produce messages with empty Role.

#### Scenario: Event without Response gets correct Role

- **WHEN** GetEvent returns a FullEvent with Response=nil
- **AND** the EventReference has EventType="external_input"
- **THEN** the message Role SHALL be "user"
- **AND** the message Content SHALL be ref.EventSummary

#### Scenario: EventReference has empty EventType and Role

- **WHEN** both ref.EventType and ref.Role are empty
- **THEN** the message Role SHALL default to "user"

### Requirement: BuildEventReference infers Role when Response is nil

`BuildEventReference` SHALL set `ref.Role` from `evt.StateDelta["event_type"]` when `evt.Response == nil`, using the same EventType→Role mapping. This ensures all EventReferences have a non-empty Role.

#### Scenario: Event without Response gets Role from EventType

- **WHEN** BuildEventReference processes an event with StateDelta["event_type"]="external_input"
- **AND** evt.Response is nil
- **THEN** ref.Role SHALL be "user"

### Requirement: findPendingUserMessage does not duplicate

Before appending a pending user message to the result, `Compress` SHALL check if a message with the same Content already exists in recentSegments. If it does, the duplicate SHALL NOT be appended.

#### Scenario: Pending user message already in recent segments

- **WHEN** findPendingUserMessage returns a message with Content="没看到呢"
- **AND** recentSegments already contains a user message with Content="没看到呢"
- **THEN** the message SHALL NOT be appended to result

#### Scenario: Pending user message not in recent segments

- **WHEN** findPendingUserMessage returns a message with Content="新问题"
- **AND** recentSegments does not contain this message
- **THEN** the message SHALL be appended to result

### Requirement: resolveReferenceToMessage logs distinguish error types

The warn log in `resolveReferenceToMessage` SHALL distinguish between "GetEvent returned error" and "GetEvent succeeded but Response is nil".

#### Scenario: GetEvent returns error

- **WHEN** GetEvent returns (nil, error)
- **THEN** log SHALL say "GetEvent failed for key=N: <error>"

#### Scenario: GetEvent succeeds but no Response

- **WHEN** GetEvent returns (full, nil) but full.Response is nil
- **THEN** log SHALL say "event key=N has no Response, falling back to EventType inference"

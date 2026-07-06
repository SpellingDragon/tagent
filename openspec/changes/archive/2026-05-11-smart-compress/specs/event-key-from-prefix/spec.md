## ADDED Requirements

### Requirement: Compressed segment event_key extraction

The system SHALL extract Snowflake EventKeys from compressed message segments by parsing the event view prefix `[evt_<KEY>|<type>]` directly from message content, instead of using Session.Events content fingerprint matching.

#### Scenario: Single segment with one event key

- **WHEN** a compressed segment contains a message with prefix `[evt_123456789|task] user request content`
- **THEN** the system SHALL extract event_key `123456789` and include it in the compressed keys list

#### Scenario: Multiple segments with distinct keys

- **WHEN** old segments contain messages with prefixes `[evt_111|task]` and `[evt_222|tool_call]` and `[evt_333|task]`
- **THEN** the system SHALL extract keys `[111, 222, 333]` (deduplicated) in the compressed keys list

#### Scenario: Duplicate event keys across segments

- **WHEN** two different segments contain messages referencing the same event_key `[evt_999|task]`
- **THEN** the system SHALL include `999` only once in the compressed keys list

#### Scenario: Message without event prefix

- **WHEN** a compressed segment contains a system message without `[evt_` prefix (e.g., previous `context_compress` message)
- **THEN** the system SHALL skip that message and not extract any key from it

#### Scenario: Malformed prefix

- **WHEN** a message starts with `[evt_` but has no closing `|` separator (e.g., `[evt_invalid text...`)
- **THEN** the system SHALL return 0 (no key) for that message, which is filtered out

### Requirement: Deletion assessment of SmartCompressor

The system SHALL retain SmartCompressor in its entirety. Stage 1 segment dropping is the sole token budget enforcement mechanism; Stage 2 LLM summary is an optional enhancement. The compressor MUST NOT be removed or disabled.

#### Scenario: Long conversation triggers compression

- **WHEN** a conversation accumulates messages exceeding the token budget threshold
- **THEN** Stage 1 SHALL drop old task segments and Stage 2 MAY generate an LLM summary (if summaryModel is configured)
- **THEN** the `context_compress` message SHALL include the correct list of compressed event keys

#### Scenario: No summaryModel configured

- **WHEN** `SmartCompressor.summaryModel` is nil
- **THEN** Stage 2 SHALL be skipped without error, and the `context_compress` message SHALL still include compressed event keys from Stage 1

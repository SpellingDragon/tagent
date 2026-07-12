## ADDED Requirements

### Requirement: Compressed summaries are persisted to MemoryStore

When `SmartCompressor` compresses a segment using `summary` or `reference` processing, it SHALL write a lightweight summary event to `MemoryStore`. The stored event SHALL have `EventType=context_compress_summary`, a new positive `EventKey`, and `EventSummary` containing the generated summary plus key facts.

#### Scenario: Summary event persisted

- **WHEN** segment with event key `123` is compressed using `summary`
- **THEN** a new event with type `context_compress_summary` SHALL be written to `MemoryStore`
- **AND** its `EventSummary` SHALL contain the generated summary text
- **AND** its metadata SHALL reference the original event key `123`

#### Scenario: Reference-only events also persisted

- **WHEN** segment with event key `456` is compressed using `reference`
- **THEN** a summary event SHALL be written containing at minimum the key facts and a reference hint
- **AND** the original event content SHALL remain retrievable via `recall({"event_keys": [456]})`

### Requirement: Context replaces compressed events with reference messages

After archiving a summary to `MemoryStore`, `SmartCompressor` SHALL replace the original compressed messages in the active context with a single system message. The message SHALL contain the original event key, the summary event key, and a recall hint.

#### Scenario: Reference message format

- **WHEN** event `123` is archived to summary key `789`
- **THEN** the context SHALL contain a system message similar to:
  ```
  [context_archive] evt_123 已归档，摘要 key=789。可用 recall({"event_keys": [789]}) 获取完整摘要。
  ```

#### Scenario: Dropped events omitted from context

- **WHEN** a segment has `processing=drop`
- **THEN** its messages SHALL NOT appear in the compressed context
- **AND** no archive message is required unless configuration mandates audit logging

### Requirement: Recall retrieves archived summaries by summary key

The `recall` tool SHALL support retrieving events of type `context_compress_summary` using their `EventKey`. When a summary event is recalled, its `EventSummary` SHALL be injected into the current context as a system message.

#### Scenario: Recall by summary key

- **WHEN** `recall({"event_keys": [789]})` is called and event `789` is a `context_compress_summary`
- **THEN** the system SHALL return the summary content
- **AND** inject it into the active context as a system message prefixed with `[recall]`

#### Scenario: Recall original event still works

- **WHEN** `recall({"event_keys": [123]})` is called and event `123` still exists in `MemoryStore`
- **THEN** the system SHALL return the original full event
- **AND** inject it into the context

### Requirement: Summary storage deduplicates identical inputs

`SmartCompressor` SHALL maintain a local cache of already-archived summary keys keyed by the content hash of the source segment and the summary prompt version. Identical segments SHALL reuse the existing summary key instead of writing a new event.

#### Scenario: Cache hit reuses summary key

- **WHEN** the same segment content is compressed twice with the same prompt version
- **THEN** the second compression SHALL reuse the existing summary key
- **AND** SHALL NOT call `MemoryStore.StoreEvent` again for that segment

#### Scenario: Prompt version change invalidates cache

- **WHEN** the summary prompt version changes
- **THEN** cached summary keys from older prompt versions SHALL be treated as invalid
- **AND** recompression SHALL generate a new summary event

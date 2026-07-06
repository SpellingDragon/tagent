## ADDED Requirements

### Requirement: L3 archive applies per-type summarization policy

`PartitionDefaults` SHALL expose `archive_summary_types` as a map from event type to summarization strategy. The valid strategies SHALL be:

- `full` — retain the complete event unchanged in L3
- `summary` — drop `content`; retain metadata (event_key, timestamp, type, parent_key, partition_id, etc.) plus an `EventSummary` string
- `partial` — retain metadata plus truncated `content` (first `N` characters, `N` configurable per-type)

`CompactL2ToL3()` SHALL apply the strategy per event when constructing the L3 output segment.

#### Scenario: Full retention for critical types

- **WHEN** `archive_summary_types: { "user_input": "full", "tool_call": "full" }` and `CompactL2ToL3()` processes a `user_input` event
- **THEN** the L3 output SHALL contain the complete event verbatim

#### Scenario: Summary strategy drops content

- **WHEN** `archive_summary_types: { "assistant_response": "summary" }` and the event has `content = "..." (5 KB)`
- **THEN** the L3 output SHALL retain metadata, set `content = ""`, and set `EventSummary = <generated_summary>`

#### Scenario: Partial strategy truncates content

- **WHEN** `archive_summary_types: { "tool_result": { strategy: "partial", max_chars: 500 } }` and the event has `content = "..." (5 KB)`
- **THEN** the L3 output SHALL retain metadata and truncate `content` to the first 500 characters

#### Scenario: Unknown type defaults to full

- **WHEN** an event's type is not found in `archive_summary_types`
- **THEN** the L3 output SHALL treat it as `full` retention (defensive default)

### Requirement: EventSummary field carries archive summary

`FullEvent` SHALL have an `EventSummary string` field. L0-L2 events SHALL leave it as empty string. L3 events using `summary` strategy SHALL populate it via the `generateSummary` hook.

#### Scenario: L0-L2 events have empty summary

- **WHEN** a fresh event is stored at L0 via `StoreEvent()`
- **THEN** `EventSummary` SHALL be `""` in the persisted JSON

#### Scenario: L3 summary strategy populates EventSummary

- **WHEN** `CompactL2ToL3()` produces an event under `summary` strategy
- **THEN** `EventSummary` SHALL be set to the output of `generateSummary(event)`

### Requirement: generateSummary hook is pluggable

A `SummaryGenerator` interface SHALL be defined with method `Generate(event FullEvent) (string, error)`. The Compactor SHALL accept a `SummaryGenerator` via constructor. The default implementation (`PassthroughSummarizer`) SHALL return `""` or a simple char-truncation; a future change (`llm-event-summary`) SHALL replace it with an LLM-backed implementation without further Compactor changes.

#### Scenario: Default summarizer returns empty string

- **WHEN** `Compactor` is constructed with `PassthroughSummarizer` and `generateSummary(event)` is called
- **THEN** the returned summary SHALL be `""` (content-dropping stub)

#### Scenario: LLM summarizer injected via constructor

- **WHEN** a future change provides `LLMSummarizer` implementing `SummaryGenerator`
- **THEN** the only Compactor change required SHALL be the constructor argument

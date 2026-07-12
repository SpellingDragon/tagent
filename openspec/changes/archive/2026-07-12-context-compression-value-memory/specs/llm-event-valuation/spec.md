## ADDED Requirements

### Requirement: EventValuator interface evaluates events in batch

The `agent` package SHALL define an `EventValuator` interface. Given a slice of `*TaskSegment`, the implementation SHALL return a slice of `EventValue` containing at minimum `EventKey`, `ValueScore` (0.0 to 1.0), `Processing` strategy, and `KeyFacts`.

#### Scenario: Interface contract

- **WHEN** a caller invokes `EventValuator.Evaluate(ctx, segments)`
- **THEN** the returned slice SHALL have the same length as the input segment slice
- **AND** each `EventValue.EventKey` SHALL match the first valid event key found in that segment

#### Scenario: Default no-op evaluator

- **WHEN** `SmartCompressor` is constructed without an `EventValuator`
- **THEN** a default pass-through evaluator SHALL be used
- **AND** it SHALL return `processing=summary` and `value_score=0.5` for every segment

### Requirement: LLM-based valuation outputs structured JSON

`LLMEventValuator` SHALL prompt the configured `model.Model` to evaluate each event and return a JSON array. Each element SHALL contain:
- `event_key`: int64
- `value_score`: float64 in [0.0, 1.0]
- `processing`: one of `keep`, `truncate`, `keyfacts`, `summary`, `reference`, `drop`
- `key_facts`: string (concise bullet list)
- `reason`: string (optional)

#### Scenario: Valid JSON valuation

- **WHEN** the LLM returns `[{"event_key":123,"value_score":0.9,"processing":"keep","key_facts":"用户原始需求"}]`
- **THEN** `LLMEventValuator` SHALL parse it into one `EventValue`
- **AND** `ValueScore` SHALL equal 0.9 and `Processing` SHALL equal `keep`

#### Scenario: Missing fields use safe defaults

- **WHEN** the LLM returns `[{"event_key":123,"value_score":0.9}]` without `processing`
- **THEN** `Processing` SHALL default to `summary`
- **AND** `KeyFacts` SHALL default to the empty string

### Requirement: Batch valuation combines with summary in one LLM call

`LLMEventValuator` SHALL request the LLM to produce both per-event valuation JSON and an overall batch summary in a single response. The summary SHALL be returned separately from the valuation array.

#### Scenario: Single call returns both

- **WHEN** `LLMEventValuator.Evaluate` is called with 5 segments
- **THEN** the LLM SHALL be invoked exactly once
- **AND** the result SHALL contain 5 `EventValue` entries plus one combined summary string

#### Scenario: Malformed response triggers fallback

- **WHEN** the LLM response cannot be parsed as the required structure
- **THEN** `LLMEventValuator` SHALL return a non-nil error
- **AND** `SmartCompressor` SHALL catch the error and fall back to rule-based compression

### Requirement: Value score respects event type floor

`LLMEventValuator` SHALL apply per-event-type floor values before returning. For example, `external_input` SHALL never receive a `value_score` below 0.5, regardless of LLM output.

#### Scenario: Floor clamps user input

- **WHEN** the LLM assigns `value_score=0.1` to an `external_input` event
- **THEN** the returned `EventValue.ValueScore` SHALL be 0.5

#### Scenario: Non-floor types pass through

- **WHEN** the LLM assigns `value_score=0.1` to a `thinking_plan` event and no floor is configured for that type
- **THEN** the returned `EventValue.ValueScore` SHALL be 0.1

# value-driven-compression Specification

## Purpose

本规范定义 value-driven-compression 能力。`SmartCompressor` SHALL compute a `value_density` for each compressible segment as `total_value_score / total_tokens`.

## Requirements

### Requirement: Segment value density drives compression order

`SmartCompressor` SHALL compute a `value_density` for each compressible segment as `total_value_score / total_tokens`. Compression planning SHALL sort compressible segments by value density ascending (lowest density first) and compress them in that order until the token budget is met.

#### Scenario: Low-density segment compressed before high-density segment

- **WHEN** segment A has value_score=0.1 and tokens=1000 (density=0.0001), segment B has value_score=0.8 and tokens=200 (density=0.004)
- **THEN** segment A SHALL be selected for compression before segment B
- **AND** segment B SHALL remain intact if the budget is met by compressing A

#### Scenario: Recent segments remain protected regardless of density

- **WHEN** `KeepRecentTasks=2` and the two most recent segments have the lowest value density
- **THEN** those two recent segments SHALL NOT be assigned level 3 compression
- **AND** they MAY be assigned level 1 or level 2 if excess tokens remain after older segments

### Requirement: Value score combined with token budget selects compression level

For each segment selected for compression, `SmartCompressor` SHALL choose the lowest compression level that satisfies the remaining excess while respecting the per-segment `processing` recommendation from `EventValuator`:
- `keep` → level 0 (no compression)
- `truncate` or `keyfacts` → level 1 (selective)
- `summary` → level 2 or level 3 depending on budget
- `reference` or `drop` → level 3 (full replacement by reference/drop)

#### Scenario: keep recommendation prevents compression

- **WHEN** a segment has `processing=keep` and `value_score=0.95`
- **THEN** `SmartCompressor` SHALL NOT compress that segment even if age-based ordering would have compressed it

#### Scenario: summary recommendation maps to level 2 or 3

- **WHEN** a segment has `processing=summary` and remaining excess is small
- **THEN** the segment SHALL be assigned level 2 (preserve user input, summarize exec)
- **AND** when remaining excess is large, the same segment SHALL be assigned level 3 (full summary)

### Requirement: Fallback to age-based compression when valuation unavailable

When `value_driven` is disabled OR `EventValuator` returns an error OR the valuation output is unparseable, `SmartCompressor` SHALL fall back to the existing age-based greedy compression algorithm.

#### Scenario: value_driven disabled

- **WHEN** `value_driven=false` in configuration
- **THEN** compression SHALL proceed from oldest to newest without using value scores
- **AND** the result SHALL be equivalent to the current behavior

#### Scenario: valuation parse failure fallback

- **WHEN** `value_driven=true` but the LLM returns malformed JSON
- **THEN** `SmartCompressor` SHALL log a warning
- **AND** SHALL fall back to age-based compression for that invocation

## MODIFIED Requirements

### Requirement: SmartCompressor and Compactor trigger from ContextManager BeforeModel callbacks

`SmartCompressor.Compress` and `Compactor.Compact` SHALL be invoked from `BeforeModel` callbacks registered by `ContextManager` on the framework `LLMAgent`. `SmartCompressor` SHALL be registered as the first callback, `Compactor` as the second. There SHALL be no inline compression or compaction logic in `AgentLoop.Run` or any other component.

The `Preprocessor.Process` method SHALL NOT exist. All compression and compaction triggering is handled by `ContextManager`'s `BeforeModel` callback chain.

`SmartCompressor` SHALL accept a `TokenCounter` via constructor option and use it for all token estimation. It SHALL NOT call `NewDefaultTokenCounter()` internally.

#### Scenario: ContextManager triggers SmartCompress from BeforeModel

- **WHEN** the framework constructs messages exceeding the token threshold
- **THEN** the SmartCompressor `BeforeModel` callback reduces `args.Request.Messages`
- **AND** `SessionProjection` is not modified by SmartCompressor
- **AND** SmartCompressor uses the injected `TokenCounter` for estimation

#### Scenario: ContextManager triggers Compactor when SmartCompressor insufficient

- **WHEN** SmartCompressor cannot bring token count below `maxTokens`
- **THEN** the Compactor `BeforeModel` callback compacts `SessionProjection`
- **AND** `args.Request.Messages` is rebuilt from the compacted projection using `ContextManager.BuildMessages` (not a temporary `NewPreprocessor`)

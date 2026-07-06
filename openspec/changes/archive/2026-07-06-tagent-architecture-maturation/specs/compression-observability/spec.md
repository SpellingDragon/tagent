## ADDED Requirements

### Requirement: SmartCompressor 输出结构化压缩指标

SmartCompressor.Compress 触发时 SHALL 通过 `log.Infof` 输出 JSON 格式的结构化指标，包含以下字段：`event`（"smart_compress"）、`before_tokens`、`after_tokens`、`discarded_segments`、`kept_segments`、`summary_generated`（bool）、`duration_ms`。如果配置了 SummaryModel 且 Stage 2 生成摘要，`summary_generated` 为 true。

#### Scenario: SmartCompressor 触发并输出指标

- **WHEN** SmartCompressor.Compress 执行，before_tokens=8000，after_tokens=3000，丢弃 3 段，保留 2 段，Stage 2 生成了摘要，耗时 450ms
- **THEN** log.Infof 输出包含 `{"event":"smart_compress","before_tokens":8000,"after_tokens":3000,"discarded_segments":3,"kept_segments":2,"summary_generated":true,"duration_ms":450}`

#### Scenario: SmartCompressor 未触发

- **WHEN** usedTokens <= threshold
- **THEN** 不输出压缩指标日志

### Requirement: Compactor 输出结构化清理指标

Compactor.Compact 触发时 SHALL 通过 `log.Infof` 输出 JSON 格式的结构化指标，包含以下字段：`event`（"compactor"）、`before_refs`、`after_refs`、`compacted_tasks`、`duration_ms`。

#### Scenario: Compactor 触发并输出指标

- **WHEN** Compactor.Compact 执行，before_refs=50，after_refs=12，压缩了 3 个旧任务为 1 个 summary reference，耗时 2ms
- **THEN** log.Infof 输出包含 `{"event":"compactor","before_refs":50,"after_refs":12,"compacted_tasks":3,"duration_ms":2}`

#### Scenario: Compactor 未触发

- **WHEN** SmartCompressor 压缩后 usedTokens <= maxTokens
- **THEN** Compactor 不执行，不输出清理指标日志

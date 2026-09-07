# event-segment-store 规格增量

## ADDED Requirements

### Requirement: WAL 中间坏行容错
LocalFileKV.replayWAL 遇**中间**坏行时 MUST 跳过该行并计入 quarantine 计数（log warn + Stats 暴露），继续重放后续行；**尾部**坏行保持截断语义；kv.json 快照本体损坏仍启动失败（fail-fast）。

#### Scenario: 单比特翻转不致记忆全失
- **WHEN** WAL 中部一行因位翻转损坏而其余行完好
- **THEN** 启动成功，坏行被隔离计数（可观测），其余事件全部恢复

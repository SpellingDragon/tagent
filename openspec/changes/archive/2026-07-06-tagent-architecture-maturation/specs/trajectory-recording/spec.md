## ADDED Requirements

### Requirement: TrajectoryRecorder Flush 方法

TrajectoryRecorder SHALL 提供 `Flush()` 方法，将缓冲区中未写入的 trajectory 数据强制落盘。`Close()` SHALL 在关闭文件前调用 Flush。Flush SHALL 是幂等的。

#### Scenario: Flush 强制落盘

- **WHEN** buffer 中有未写入的记录
- **AND** 调用 Flush()
- **THEN** 所有缓冲数据写入 JSONL 文件

#### Scenario: Close 前自动 Flush

- **WHEN** Close() 被调用
- **THEN** 先执行 Flush() 确保数据落盘
- **AND** 然后关闭文件句柄

## MODIFIED Requirements

### Requirement: TrajectoryRecorder wraps model.Model

The system SHALL provide a `TrajectoryRecorder` type that implements the `model.Model` interface and wraps an inner `model.Model`. All LLM requests forwarded through TrajectoryRecorder SHALL be recorded before being passed to the inner model. The inner model's response SHALL be recorded before being returned to the caller. TrajectoryRecorder SHALL provide a `Flush()` method to force-write buffered records to disk. `Close()` SHALL call Flush before closing the file handle. Recording SHALL use append mode with one JSON line per record. Process crash SHALL at most lose the last unflushed record.

#### Scenario: Normal mode recording

- **WHEN** tagent runs in normal mode (ZhipuAI) with `trajectory_dump: true`
- **AND** TrajectoryRecorder wraps the ZhipuAI model
- **THEN** every LLM call's request messages, response content, tool calls, usage, and timing SHALL be recorded to a JSONL file

#### Scenario: RL mode recording

- **WHEN** tagent runs in RL mode (AReaL proxy) with `trajectory_dump: true`
- **AND** TrajectoryRecorder wraps SwappableModel
- **THEN** every LLM call through the AReaL proxy SHALL be recorded to a JSONL file

#### Scenario: Recording disabled

- **WHEN** `trajectory_dump` is `false` or unset
- **THEN** TrajectoryRecorder SHALL NOT be instantiated
- **AND** no trajectory files SHALL be written

#### Scenario: Flush before close

- **WHEN** Close() is called on TrajectoryRecorder
- **THEN** Flush() is called first to ensure all buffered records are written to disk
- **AND** then the file handle is closed

#### Scenario: Crash safety

- **WHEN** process crashes after writing record 5 but before Flush
- **THEN** the JSONL file contains records 1-4 (complete)
- **AND** record 5 may be truncated
- **AND** on restart, new records can be appended (truncated line is skipped)

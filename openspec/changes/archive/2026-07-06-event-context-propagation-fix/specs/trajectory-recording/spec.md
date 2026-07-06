## MODIFIED Requirements

### Requirement: TrajectoryRecorder wraps model.Model

The system SHALL provide a `TrajectoryRecorder` type that implements the `model.Model` interface and wraps an inner `model.Model`. All LLM requests forwarded through TrajectoryRecorder SHALL be recorded before being passed to the inner model. TrajectoryRecorder SHALL provide a `Flush()` method to force-write buffered records to disk. `Close()` SHALL call Flush before closing the file handle. Recording SHALL use append mode with one JSON line per record. `TagentAgent.Close()` SHALL call `trajectoryRecorder.Close()` after `contextManager.Close()` to ensure all buffered trajectory data is flushed and file handles are released.

#### Scenario: Normal mode recording

- **WHEN** tagent runs in normal mode with `trajectory_dump: true`
- **AND** TrajectoryRecorder wraps the model
- **THEN** every LLM call's request messages, response content, tool calls, usage, and timing SHALL be recorded to a JSONL file

#### Scenario: Recording disabled

- **WHEN** `trajectory_dump` is `false` or unset
- **THEN** TrajectoryRecorder SHALL NOT be instantiated
- **AND** no trajectory files SHALL be written

#### Scenario: Flush before close

- **WHEN** Close() is called on TrajectoryRecorder
- **THEN** Flush() is called first to ensure all buffered records are written to disk
- **AND** then the file handle is closed

#### Scenario: TagentAgent.Close calls TrajectoryRecorder.Close

- **WHEN** TagentAgent.Close() is called and trajectoryRecorder is set
- **THEN** trajectoryRecorder.Close() is called after contextManager.Close()
- **AND** writeLoop goroutine flushes all records and exits
- **AND** all JSONL files are Synced and closed

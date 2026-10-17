## ADDED Requirements

### Requirement: Output stability does not prove process completion

For every command mode, the monitor MUST distinguish quiet output from native process death. A live pane reaching its stability threshold SHALL emit an observation and MUST NOT become completed solely because output stopped. Exit polarity SHALL derive from pane_dead and native exit status. Resident readiness and interactive input readiness SHALL require their explicit pattern or successful probe evidence.

#### Scenario: Quiet oneshot remains live
- **WHEN** a oneshot command is quiet beyond stable_duration while its native pane is alive
- **THEN** it remains active and its exit wait is not completed by stability

#### Scenario: Native exit determines completion
- **WHEN** the native pane exits with status zero or nonzero
- **THEN** the result reports completed or failed with the captured native exit evidence

## REMOVED Requirements

### Requirement: Non-interactive commands complete on stable

**Reason**: 稳定输出不能证明native进程已退出。

**Migration**: 使用pane原生存活/退出及显式ready/input证据。

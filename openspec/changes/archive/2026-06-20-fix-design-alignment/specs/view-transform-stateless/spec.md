## ADDED Requirements

### Requirement: KeepRecentTasks is restored after BeforeModel

ContextIntervention.BeforeModel SHALL save the original KeepRecentTasks value before the compression loop and restore it after. The compression loop MAY temporarily decrement KeepRecentTasks, but the value SHALL be restored to the original before returning.

#### Scenario: Multiple compression rounds

- **WHEN** BeforeModel runs 3 compression rounds, decrementing KeepRecentTasks from 2 to 1
- **THEN** after BeforeModel returns, KeepRecentTasks is restored to 2

#### Scenario: Next request uses original value

- **WHEN** the next BeforeModel call occurs
- **THEN** KeepRecentTasks starts at the original configured value (2), not the decremented value (1)

#### Scenario: Panic during compression

- **WHEN** the compression loop panics
- **THEN** defer restoration still executes, KeepRecentTasks is restored to original value

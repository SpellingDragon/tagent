## ADDED Requirements

### Requirement: TagentAgent session context is thread-safe

TagentAgent SHALL protect lastUserID and lastSessionID fields with a mutex. Run/RunSimple SHALL acquire the mutex when writing, and InjectMessage SHALL acquire the mutex when reading.

#### Scenario: Concurrent Run and InjectMessage

- **WHEN** Run writes lastSessionID while InjectMessage reads lastSessionID concurrently
- **THEN** no data race occurs; InjectMessage reads a consistent session ID

#### Scenario: InjectMessage with no prior Run

- **WHEN** InjectMessage is called before any Run/RunSimple
- **THEN** lastUserID and lastSessionID are empty, InjectMessage returns early (current behavior preserved)

### Requirement: TmuxMonitor.running is atomically accessed

TmuxMonitor.running SHALL be a `sync/atomic.Bool`. Start/Stop SHALL atomically set the value. All reads SHALL use the atomic load method.

#### Scenario: Concurrent Start and Stop

- **WHEN** Start and Stop are called concurrently
- **THEN** no data race occurs; exactly one goroutine runs the monitor loop

#### Scenario: CommandTool checks if monitor is running

- **WHEN** CommandTool needs to check tmuxMonitor.running
- **THEN** it calls IsRunning() method which uses atomic load, not direct field access

### Requirement: TmuxMonitor.checkSession modifies session fields under lock

checkAllSessions SHALL hold the session map lock while calling checkSession. checkSession SHALL modify session Status/LastOutput/LastOutputMD5/StableSince fields only while the lock is held.

#### Scenario: Concurrent checkAllSessions and session modification

- **WHEN** checkAllSessions iterates sessions and modifies fields
- **THEN** all field modifications occur under the lock; no concurrent access to session fields

### Requirement: InMemRelationStore.truncateJournal is locked

truncateJournal SHALL acquire the write lock before closing and reopening the journal file. This prevents concurrent SetParent from writing to a closed file handle.

#### Scenario: Concurrent SaveSnapshotToFile and SetParent

- **WHEN** SaveSnapshotToFile calls truncateJournal while SetParent is appending to journal
- **THEN** SetParent completes its journal write before truncateJournal acquires the write lock; truncateJournal safely closes and reopens the journal file

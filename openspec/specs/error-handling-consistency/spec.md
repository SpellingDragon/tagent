## ADDED Requirements

### Requirement: Log package is unified

All Go source files in tagent SHALL use `trpc-agent-go/log` for logging. The standard library `log` package SHALL NOT be used.

#### Scenario: command_tool.go log usage

- **WHEN** command_tool.go logs a message
- **THEN** it uses `log.Infof`/`log.Errorf` from `trpc-agent-go/log`, not standard library `log.Printf`

### Requirement: Error return values are checked

All error return values from KVStore operations (KVPut/KVGet/KVDelete/KVScan), RelationStore operations (SetParent/GetParent), and TombstoneSet operations (MarkTombstone) SHALL be checked. Silent swallowing (`_ = err`) SHALL be replaced with at minimum a log statement.

#### Scenario: KVPut fails for segment metadata

- **WHEN** segment_store.go calls kv.KVPut for meta and it returns an error
- **THEN** the error is logged with context (partition ID, window TS), not silently ignored

#### Scenario: GetParent fails in recall subtools

- **WHEN** recall_subtools.go calls RelationStore.GetParent and it returns an error
- **THEN** the error is logged and the tool returns a meaningful error to the LLM, not silently ignored

#### Scenario: SetParent fails during compaction repair

- **WHEN** compaction.go calls rel.SetParent during repairDanglingRefs and it returns an error
- **THEN** the error is logged with context (event key, ancestor key), not silently ignored

### Requirement: Stale comments referencing removed mechanisms are cleaned

All comments referencing removed FullEvent.ParentKey field or Phase 1 event view transformation SHALL be updated or removed.

#### Scenario: memory_plugin.go comment

- **WHEN** reading memory_plugin.go line 22 ("Build FullEvent with ParentKey")
- **THEN** the comment is updated to reflect that FullEvent no longer contains ParentKey

#### Scenario: smart_compress.go comment

- **WHEN** reading smart_compress.go collectCompressedKeys comment
- **THEN** the comment describes the new injection mechanism, not "Phase 1 event view transformation"

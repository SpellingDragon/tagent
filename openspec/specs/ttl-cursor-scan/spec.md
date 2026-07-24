# ttl-cursor-scan Specification

## Purpose

本规范定义 ttl-cursor-scan 能力。Each partition SHALL maintain `{pid}:ttl_cursor` → JSON `{next_scan_window, last_scan_time}`.

## Requirements

### Requirement: TTL scan uses per-partition time cursor

Each partition SHALL maintain `{pid}:ttl_cursor` → JSON `{next_scan_window, last_scan_time}`. `LifecycleManager.checkTTL(pid)` SHALL start scanning from `next_scan_window`, advancing the cursor only past windows whose age exceeds the TTL threshold.

#### Scenario: First TTL scan starts from oldest window

- **WHEN** `checkTTL(1)` is called for the first time (no `1:ttl_cursor`)
- **THEN** scan SHALL start from the oldest `{1}:meta:` window discovered

#### Scenario: Cursor advances only past expired windows

- **WHEN** windows 1710000000, 1710003600, 1710007200 exist, TTL is 2 hours, and `now = 1710010800`
- **THEN** `checkTTL` SHALL process windows 1710000000 and 1710003600 (age > 2h), tombstone their events, and update `ttl_cursor.next_scan_window = 1710007200`
- **AND** window 1710007200 (age = 1h) SHALL NOT be processed; cursor SHALL stop before it

#### Scenario: Cursor persisted within same scan batch

- **WHEN** `checkTTL` completes a scan batch
- **THEN** the updated cursor SHALL be persisted to `{pid}:ttl_cursor` via KV put before returning

### Requirement: TTL scan complexity bounded by expired window count

`checkTTL` SHALL have time complexity O(expired_windows_per_partition), not O(total_windows_per_partition). With 10 events/s over 3 years (≈ 26000 hourly windows per partition), each hourly scan SHALL process at most a few expired windows (typically 1 per scan cycle), not all 26000.

#### Scenario: Scale-safe TTL scan

- **WHEN** a partition has 26000 historical sealed windows and TTL is 30 days
- **THEN** `checkTTL` SHALL scan at most ~24 windows per hour (those crossing the TTL boundary), with ttl_cursor memorizing progress

### Requirement: TTL cursor recovers gracefully from restart

`LifecycleManager.Start()` SHALL load each partition's `ttl_cursor` on startup. If a partition's cursor is missing, it SHALL be initialized to the oldest window for that partition.

#### Scenario: Restart reuses cursor progress

- **WHEN** tagent restarts and partition `1` has `ttl_cursor.next_scan_window = 1710003600`
- **THEN** the next `checkTTL(1)` invocation SHALL resume from window `1710003600`, not rescan earlier windows

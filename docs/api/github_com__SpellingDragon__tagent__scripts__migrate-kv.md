Command migrate-kv copies a retired sharded-snapshot root into a new journal
root through ledger.MigrateShardedRoot: the source stays read-only, the target
must hold no keys, and the copy reads back byte for byte before it reports.


// Command migrate-kv copies a retired sharded-snapshot root into a new journal
// root through ledger.MigrateShardedRoot: the source stays read-only, the target
// must hold no keys, and the copy reads back byte for byte before it reports.
//
// 契约: docs/wiki/memory/memory-architecture.md#local-file-kv
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/SpellingDragon/tagent/memory/ledger"
)

func main() {
	source := flag.String("source", "", "retired sharded-snapshot root, read only")
	target := flag.String("target", "", "new journal root, must hold no keys")
	flag.Parse()
	if *source == "" || *target == "" {
		fail(fmt.Errorf("--source and --target are required"))
	}
	report, err := ledger.MigrateShardedRoot(context.Background(), *source, *target)
	if err != nil {
		fail(err)
	}
	fmt.Printf("migrate-kv: %d keys, %d bytes, %d batches: %s -> %s\n", report.Keys, report.Bytes, report.Batches, *source, *target)
	fmt.Println("migrate-kv: point every agent's memory.path at the target root")
}

// fail reports one migration failure to stderr and exits nonzero.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "migrate-kv:", err)
	os.Exit(1)
}

package memoryledgerexample // import "github.com/SpellingDragon/tagent/examples/contract/memory_ledger"

Package memoryledgerexample holds the canonical examples of the ledger faces a
reclaimer, a retention guard and a scoped read drive the original store through:
the protection guard, the lifecycle store, the original-text store and its
scoped read.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleGuard()
    ExampleGuard holds one original, reports it as protected, releases it under
    its lease and reports that a foreign lease is refused while the hold still
    stands.

func ExampleLifecycleStore()
    ExampleLifecycleStore enumerates the keys a store holds, tombstones the one
    past its window and reports that compaction destroyed exactly the tombstoned
    key.

func ExampleOriginalStore()
    ExampleOriginalStore commits one original, reads it back byte for byte,
    reads its first line as a segment and reports an uncommitted key as missing
    by name.

func ExampleScopedOriginalStore()
    ExampleScopedOriginalStore reads one original inside the scope it was
    registered under, cuts one byte-bounded segment at a byte boundary and
    reports the same key as missing under another scope.


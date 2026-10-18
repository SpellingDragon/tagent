package memoryexample // import "github.com/SpellingDragon/tagent/examples/contract/memory"

Package memoryexample holds the canonical examples of the memory faces a
projection, a reclaimer and a recall path drive the store through.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleEmbedder()
    ExampleEmbedder embeds one text under the binding that borrowed the seam and
    reports that empty text is refused by name instead of answered with a zero
    vector.

func ExampleEventChain()
    ExampleEventChain replays one page of the chain after a given key, then
    hydrates the fact a compaction ref names.

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


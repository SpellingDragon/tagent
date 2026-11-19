package memoryledgerexample // import "github.com/SpellingDragon/tagent/examples/contract/memory_ledger"

Package memoryledgerexample holds the canonical examples of the ledger faces a
reclaimer and a boot tail walk drive: the original-text store, its scoped read
and the commit order.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleCommitOrder()
    ExampleCommitOrder walks the records five commits landed: it names each key
    once at its newest commit from the newest record to the oldest, skips the
    key whose newest commit deleted it, and ends a second walk where the visit
    answers false.

func ExampleOriginalStore()
    ExampleOriginalStore commits one original, reads it back byte for byte,
    reads its first line as a segment and reports an uncommitted key as missing
    by name.

func ExampleScopedOriginalStore()
    ExampleScopedOriginalStore reads one original inside the scope it was
    registered under, cuts one byte-bounded segment at a byte boundary and
    reports the same key as missing under another scope.


package stateexample // import "github.com/SpellingDragon/tagent/examples/contract/runtime_state"

Package stateexample holds the canonical examples of the state owner's faces:
the per-request borrowing face, the publication and reporting ports, the shared
store face, and the durable and migration seams a rebuild reads through.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleArchiveTaskReader()
    ExampleArchiveTaskReader enumerates the tasks the chain holds facts for,
    reads one chain in recorded order and reports a task it holds nothing for by
    name.

func ExampleArchiveTaskWriter()
    ExampleArchiveTaskWriter declares one migrated task, commits its fact and
    reports that a replayed commit records no second fact.

func ExampleBorrow()
    ExampleBorrow borrows the generation in effect for one agent, serves a
    delegated request that same generation and reports a reference minted by
    another holder as unknown.

func ExampleFactLog()
    ExampleFactLog stages two keys, reads one back, scans them all in full and
    completes the barrier before anything is reported durable.

func ExampleGovernance()
    ExampleGovernance answers one publication candidate check by check, so a
    required check that reports a reject and an exempt check that passed nothing
    both read as what they are.

func ExampleOriginalMigrator()
    ExampleOriginalMigrator lands one archived original beside its reference
    metadata, reads it back byte for byte and reports that a later Put keeps the
    first metadata effective.

func ExampleOriginalReader()
    ExampleOriginalReader reads back the bytes one migrated key holds and
    reports an absent key as not held rather than as an empty original.

func ExamplePublicationRecorder()
    ExamplePublicationRecorder commits one publication record carrying every
    check disposition the publication recorded.

func ExampleStore()
    ExampleStore establishes one site, publishes two generations of which the
    repeated content yields no new one, closes the site and accepts the same
    notification twice as one fact.


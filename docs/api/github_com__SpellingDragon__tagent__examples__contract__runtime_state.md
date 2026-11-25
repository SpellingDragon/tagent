package stateexample // import "github.com/SpellingDragon/tagent/examples/contract/runtime_state"

Package stateexample holds the canonical examples of the state owner's faces:
the per-request borrowing face, the publication and reporting ports, and the
shared store face.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleBorrow()
    ExampleBorrow borrows the generation in effect for one agent, serves a
    delegated request that same generation and reports a reference minted by
    another holder as unknown.

func ExampleGovernance()
    ExampleGovernance answers one publication candidate check by check, so a
    required check that reports a reject and an exempt check that passed nothing
    both read as what they are.

func ExamplePublicationRecorder()
    ExamplePublicationRecorder commits one publication record carrying every
    check disposition the publication recorded.

func ExampleStore()
    ExampleStore establishes one site, publishes two generations of which the
    repeated content yields no new one, accepts the same notification twice as
    one fact, claims it once and closes the site.


package evolutionmeditationexample // import "github.com/SpellingDragon/tagent/examples/contract/evolution_meditation"

Package evolutionmeditationexample holds the canonical examples of the
meditation faces a reflection loop reads and writes through: the bounded digest,
the observed facts, the reflection input and the watermark gate.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleDigest()
    ExampleDigest renders the summary of a face that holds more facts than the
    bound renders, so the count left out travels with the body.

func ExampleEventSink()
    ExampleEventSink commits one summary into the target session's chain and
    reads back the identity the commit recorded.

func ExampleObservedFacts()
    ExampleObservedFacts runs the conditional retrieval a reflection judgment
    runs: the newest references are hydrated one by one and the read stops at
    its first hit.

func ExampleReflectionInput()
    ExampleReflectionInput injects one fired reflection and reports that only
    a consumed turn advances the watermark: a deferred turn leaves the input to
    the next tick.

func ExampleSessionQuery()
    ExampleSessionQuery resolves one declared target into its live sessions and
    reports that an agent with no binding answers none.

func ExampleWatermarkGate()
    ExampleWatermarkGate reads the watermark, advances it past a consumed turn
    and reports that a gate without an established watermark stays closed.


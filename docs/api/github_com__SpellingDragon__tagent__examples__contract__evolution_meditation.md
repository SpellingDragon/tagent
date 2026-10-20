package evolutionmeditationexample // import "github.com/SpellingDragon/tagent/examples/contract/evolution_meditation"

Package evolutionmeditationexample holds the canonical examples of the
meditation faces a reflection loop reads and writes through: the bounded digest,
the observed facts, the reflection input and the watermark gate.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleCompressPhase()
    ExampleCompressPhase drives one meditation tick through the plugin pipeline
    that satisfies the port: the request the tick read is handed to the pipeline
    face before the pass it drives commits its product, the face answers the
    request as filed, and the tick retires it only after the products of its
    compression are committed.

func ExampleDigest()
    ExampleDigest renders the summary of a face that holds more facts than the
    bound renders, so the count left out travels with the body.

func ExampleObservedFacts()
    ExampleObservedFacts runs the conditional retrieval a reflection judgment
    runs: the newest references are hydrated one by one and the read stops at
    its first hit.

func ExampleReflectionInput()
    ExampleReflectionInput injects one fired reflection and reports that only
    a consumed turn advances the watermark: a deferred turn leaves the input to
    the next tick.

func ExampleWatermarkGate()
    ExampleWatermarkGate reads the watermark, advances it past a consumed turn
    and reports that a gate without an established watermark stays closed.


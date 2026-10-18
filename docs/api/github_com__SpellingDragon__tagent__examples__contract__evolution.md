package evolutionexample // import "github.com/SpellingDragon/tagent/examples/contract/evolution"

Package evolutionexample holds the canonical examples of the six evolution faces
a reflection loop reads and writes through: the bounded digest, the structural
gate, the observed facts, the controlled refinement, the reflection input and
the watermark gate.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleDigest()
    ExampleDigest renders the summary of a face that holds more facts than the
    bound renders, so the count left out travels with the body.

func ExampleObservedFacts()
    ExampleObservedFacts runs the conditional retrieval a reflection judgment
    runs: the newest references are hydrated one by one and the read stops at
    its first hit.

func ExampleRefine()
    ExampleRefine registers one improvement over a controlled file, lists the
    ledger and reverts a commit the ledger does not record.

func ExampleReflectionInput()
    ExampleReflectionInput injects one fired reflection and reports that only
    a consumed turn advances the watermark: a deferred turn leaves the input to
    the next tick.

func ExampleStructuralGate()
    ExampleStructuralGate passes one candidate through the three ordered
    segments: a candidate with recorded evidence passes, and one without it is
    reported as insufficient evidence rather than as a pass.

func ExampleWatermarkGate()
    ExampleWatermarkGate reads the watermark, advances it past a consumed turn
    and reports that a gate without an established watermark stays closed.


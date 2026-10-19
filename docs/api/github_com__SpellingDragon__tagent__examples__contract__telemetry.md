package telemetryexample // import "github.com/SpellingDragon/tagent/examples/contract/telemetry"

Package telemetryexample holds the canonical examples of the observation bypass
implementation faces: one file per interface, a minimal implementation beside
the Example function that drives it through its real caller.

FUNCTIONS

func ExampleRecorder()
    ExampleRecorder admits committed facts into a bounded queue: an admission
    past the byte budget is refused and counted instead of blocking the caller,
    and the run is reported sealed only when the writer confirmed the seal.


package telemetryexample // import "github.com/SpellingDragon/tagent/examples/contract/telemetry"

Package telemetryexample holds the canonical examples of the observation bypass:
the two ports a committed fact is observed through and the bounded recorder that
carries those observations to a writer.

One file per interface, each a minimal implementation beside the Example
function that drives it.

FUNCTIONS

func ExampleAuditSink()
    ExampleAuditSink hands one layered record to the audit side: the record
    lands and the fact keeps its identity, while a record that does not land
    comes back as an error instead of being dropped quietly.

func ExampleObserver()
    ExampleObserver reports one committed fact after it is durable: the fact
    arrives unchanged and is tallied, and a failure to record it is returned as
    an observation gap while the fact itself stays committed.

func ExampleRecorder()
    ExampleRecorder admits committed facts into a bounded queue: an admission
    past the byte budget is refused and counted instead of blocking the caller,
    and the run is reported sealed only when the writer confirmed the seal.


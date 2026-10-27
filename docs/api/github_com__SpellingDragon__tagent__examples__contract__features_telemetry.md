package telemetryexample // import "github.com/SpellingDragon/tagent/examples/contract/features_telemetry"

Package telemetryexample holds the canonical examples of the observation bypass
implementation faces: one file per interface, a minimal implementation beside
the Example function that drives it through its real caller.

FUNCTIONS

func ExampleLogObserver()
    ExampleLogObserver renders one turn's facts through the framework logger:
    the turn boundary is an INFO line, the tool call of that turn is a DEBUG
    line carrying the tool and the task, and neither line renders the payload
    content.


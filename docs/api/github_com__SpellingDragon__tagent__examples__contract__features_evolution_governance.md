package evolutiongovernanceexample // import "github.com/SpellingDragon/tagent/examples/contract/features_evolution_governance"

Package evolutiongovernanceexample holds the canonical examples of the
governance domain faces: one file per interface, a minimal implementation beside
the Example function that drives it through its real caller.

FUNCTIONS

func ExampleGovernanceEventLog()
    ExampleGovernanceEventLog stages and replays one governance event: the write
    is durable once Sync returns nil, and the prefix scan rebuilds the stream in
    key order.


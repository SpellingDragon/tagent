package evolutionexample // import "github.com/SpellingDragon/tagent/examples/contract/features_evolution"

Package evolutionexample holds the canonical example of the structural gate a
reflection loop's verdicts pass through: the verdict states a gate reports and
the one verdict it permits.

The file is the minimal implementation beside the Example function that drives
it through its real caller.

FUNCTIONS

func ExampleStructuralGate()
    ExampleStructuralGate passes one candidate through the three ordered
    segments: a candidate with recorded evidence passes, and one without it is
    reported as insufficient evidence rather than as a pass.


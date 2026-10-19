package evolutionrefineexample // import "github.com/SpellingDragon/tagent/examples/contract/evolution_refine"

Package evolutionrefineexample holds the canonical example of the controlled
refinement face a reflection loop improves itself through: the improvement
ledger that records one improvement over a controlled set, lists it back and
reverts it by its commit.

The file is the minimal implementation beside the Example function that drives
it through its real caller.

FUNCTIONS

func ExampleRefine()
    ExampleRefine registers one improvement over a controlled file, lists the
    ledger and reverts a commit the ledger does not record.


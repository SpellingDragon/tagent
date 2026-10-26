package execexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_exec"

Package execexample holds the canonical example of the external-execution face:
the one port the runtime starts, probes, captures from and stops an execution
through.

The file is the minimal implementation beside the Example function that drives
it.

FUNCTIONS

func ExampleExecutor()
    ExampleExecutor starts one external execution and then reports what
    it observed: the started resource is held under its canonical key,
    an unconfirmed exit keeps the lease instead of releasing it, and a second
    start under the same key is refused.


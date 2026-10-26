package specexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_spec"

Package specexample holds the canonical example of the plan-backend face:
the one port the spec tool dispatches plan operations through, answering with
the backend's exit code and output beside the hint that names the next step.

The file is the minimal implementation beside the Example function that drives
it.

FUNCTIONS

func ExampleBackend()
    ExampleBackend drives the canonical backend through the distinction the port
    is built on: a refused archive is a Result carrying its hint, an operation
    outside the closed set is a named error that reaches no backend work,
    and the workspace root is the backend's own storage fact rather than a
    deployment setting.


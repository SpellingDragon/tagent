package subagentexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_subagent"

Package subagentexample holds the canonical example of the delegation face one
sub-agent call is admitted through: the admission that answers the caller with a
receipt and the terminal report of that same task.

The file is the minimal implementation beside the Example function that drives
it.

FUNCTIONS

func ExampleDelegation()
    ExampleDelegation admits one sub-agent call, reports the receipt that
    answers its caller, reports the terminal notice of that same task and
    reports that a task never admitted is refused rather than answered with a
    terminal.


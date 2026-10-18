package taskexample // import "github.com/SpellingDragon/tagent/examples/contract/task"

Package taskexample holds the canonical examples of the task-layer faces a
registry, a manager and a restore path drive one task through.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleCommit()
    ExampleCommit stores one transition with its notification, reports a
    repeated mutation identity with the same content as committed and refuses
    one that carries other content.

func ExampleManager()
    ExampleManager admits one task, reports the obligation the admission opened,
    hands the task's future notifications to another instance and flushes them
    while the obligation is still open.

func ExampleOwnerBinding()
    ExampleOwnerBinding resolves the owner of one restored target from the tool
    set the current binding holds, and reports a target that set does not hold
    by name.

func ExampleRegistry()
    ExampleRegistry registers one task, reports a repeated admission as a dedup
    of the same task, resumes it under the same identity and cancels it without
    touching any other task.


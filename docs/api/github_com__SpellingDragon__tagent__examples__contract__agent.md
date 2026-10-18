package agentexample // import "github.com/SpellingDragon/tagent/examples/contract/agent"

Package agentexample holds the canonical example of every execution face
the agent loop dispatches through: one file per interface, each a minimal
implementation beside the Example function that drives it through its real
caller.

FUNCTIONS

func ExampleAgent()
    ExampleAgent drives one invocation through the three faces of its execution:
    the decision entry, the delegation it admits and the close that ends it.

func ExampleDelegationTasks()
    ExampleDelegationTasks admits one delegated call, settles it beside its
    notification and reports that the obligation cleared only once it was
    delivered.

func ExampleHistoryFace()
    ExampleHistoryFace commits the transcript of one ended drive and reads it
    back beside the input the next drive aggregates.

func ExampleReconciler()
    ExampleReconciler books one admitted call, lands its terminal and reports
    that no booking stays open once the landing ran.

func ExampleRecorder()
    ExampleRecorder drives one decision's recording: the episode the range
    starts, the decision it reduced and the attempt beside it. A write failure
    stays an observation and never reaches the model decision.


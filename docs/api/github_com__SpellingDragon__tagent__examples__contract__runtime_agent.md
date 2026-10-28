package agentexample // import "github.com/SpellingDragon/tagent/examples/contract/runtime_agent"

Package agentexample shows the loop-side capture port.

Package agentexample shows the notice feed the loop reads.

Package agentexample holds the canonical example of every execution face
the agent loop dispatches through: one file per interface, each a minimal
implementation beside the Example function that drives it through its real
caller.

FUNCTIONS

func ExampleAgent()
    ExampleAgent drives one invocation through the three faces of its execution:
    the decision entry, the delegation it admits and the close that ends it.

func ExampleCapture()
    ExampleCapture demonstrates the three points one turn fires.

func ExampleDelegationTasks()
    ExampleDelegationTasks admits one delegated call, settles it beside its
    notification and reports that the obligation cleared only once it was
    delivered.

func ExampleHandoffEnvelope()
    ExampleHandoffEnvelope renders one cross-agent handoff in structured form:
    the envelope names the task, the acceptance constraints, the grants,
    the pending decisions and the refs, validates itself and renders
    the external-context block a delegated turn reads in front of its
    natural-language request. The remote channel carries the same record as one
    data part, so a data part read back names the same fields and is held to the
    same validation.

func ExampleHistoryFace()
    ExampleHistoryFace commits the transcript of one ended drive and reads it
    back beside the input the next drive aggregates.

func ExampleNoticeFeed()
    ExampleNoticeFeed demonstrates the incremental notice read from the chain.

func ExampleReconciler()
    ExampleReconciler books one admitted call, lands its terminal and reports
    that no booking stays open once the landing ran.


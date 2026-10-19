package pluginexample // import "github.com/SpellingDragon/tagent/examples/contract/api_plugin"

Package pluginexample holds the canonical example of every narrow face
the plugin pipeline dispatches: one file per interface, each a minimal
implementation beside the Example function that drives it through its real
caller.

The observation bypass faces sit in the same directory because they are the same
package surface: the post-commit observer, the bounded recorder that carries
its records to a writer, and the audit sink those records land in, one file per
interface.

FUNCTIONS

func ExampleAuditSink()
    ExampleAuditSink hands one layered record to the audit side: the record
    lands and the fact keeps its identity, while a record that does not land
    comes back as an error instead of being dropped quietly.

func ExampleFactLookup()
    ExampleFactLookup resolves one committed fact: the fact comes back as it
    committed, and an identity with no committed fact behind it is refused
    instead of answered with an empty fact.

func ExampleFinalPresenter()
    ExampleFinalPresenter renders one committed fact through the After pass:
    the rendered view reaches the presentation read-out beside the drop flag,
    and the committed fact the view was rendered from stays.

func ExampleGovernor()
    ExampleGovernor renders the ruling of two calls: the allow ruling runs
    with no trace duty, and the recorded ruling runs beside its audit record.
    A hold rendered here stays a wait, never a denial.

func ExampleObservationSink()
    ExampleObservationSink hands one committed fact to the observation side: the
    sink never rewrites the fact and never withholds it, so a failed observation
    stays a gap beside the committed fact.

func ExampleObservationSource()
    ExampleObservationSource reads the authorized face of one reflection:
    an undeclared face is the agent's own partition, and a chain that cannot be
    read keeps the gate closed instead of falling back to another criterion.

func ExampleObserver()
    ExampleObserver reports one committed fact after it is durable: the fact
    arrives unchanged and is tallied, and a failure to record it is returned as
    an observation gap while the fact itself stays committed.

func ExamplePhase_riskPolicy()
    ExamplePhase_riskPolicy drives the two faces of the risk phase beside each
    other: one element grades a call and renders the ruling over that grade,
    and a pass that carries no grade is ruled as an ordinary mutating call
    instead of passing ungraded.

func ExamplePlugin()
    ExamplePlugin shows both outcomes of one registration unit: a plugin that
    reports a phase without the narrow face of that phase is refused by name,
    while a plugin that carries the face is dispatched into that phase's set and
    runs in the one Before pass.

func ExamplePostCommitObserver()
    ExamplePostCommitObserver observes one committed fact through the After
    pass: the fact is read after its canonical commit, and a failed observation
    returns as a named gap that never rolls the committed fact back.

func ExamplePresentation()
    ExamplePresentation drives one committed fact through the whole After pass:
    the fact identity is the event.EventID of the canonical commit, the rendered
    views sit beside the visible gaps the failed observers left, and the fact
    itself is never withheld or retracted by either outcome.

func ExampleRiskAssessor()
    ExampleRiskAssessor grades one call through the Before pass: the pass
    reports the strongest grade its risk faces rendered, and the pre-execution
    ruling reads that grade instead of running a second risk face.

func ExampleTransformCall()
    ExampleTransformCall drives one transform through the Before pass: every
    transform corrects its own copy, and the final call the pass reports is the
    one the core re-checks, executes and records.

func ExampleTransformCall_refusal()
    ExampleTransformCall_refusal drives both named refusals of the transform
    face through the Before pass: a transform error refuses the whole pass and
    answers no CallPass, so the call neither runs nor is recorded, and a moved
    CallID is refused as ErrCallIdentityChanged while the call the model issued
    stays as it was issued.

TYPES

type TransformFace struct{}
    TransformFace is the canonical registration unit: one name, the one phase it
    serves and the one narrow face of that phase.

func (TransformFace) Name() string
    Name reports the stable name of the element.

func (TransformFace) Phase() plugin.Phase
    Phase reports the one phase the element serves.

func (TransformFace) TransformCall(context.Context, *trpcmodel.ToolCall) error
    TransformCall corrects one call before its side effect; the corrected call
    is the one the core re-checks, executes and records.

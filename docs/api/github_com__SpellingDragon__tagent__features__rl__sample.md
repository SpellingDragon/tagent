package sample // import "github.com/SpellingDragon/tagent/features/rl/sample"

Package sample is the training sample loop of one rl capture: it reads a fixed
request back through its snapshot reference, judges the task's artifacts
independently of the execution path's self-report, and evaluates whether one
sample carries all three elements before the capture seals.

The package is the consumer of the rl record vocabulary: it writes no record
of its own kind, derives no reward, and never reads the execution path's
self-report as an acceptance verdict.

CONSTANTS

const (
	// VerdictPass is the independent verifier's positive verdict: the task
	// artifacts satisfy the task definition.
	VerdictPass = "pass"
	// VerdictFail is the independent verifier's negative verdict: the task
	// artifacts are missing or do not satisfy the task definition. A failed
	// sample is still a valid sample and is recorded as a negative one.
	VerdictFail = "fail"
	// VerdictUnrated is the annex verdict when no independent verdict exists:
	// the sample is incomplete and the absence is recorded rather than invented
	// as a pass.
	VerdictUnrated = "unrated"
	// VerdictIncomplete is the gate's persistent marker: the sample is missing
	// at least one of the three elements and is kept as an incomplete sample.
	VerdictIncomplete = "incomplete"
)
    VerdictPass, VerdictFail, VerdictUnrated and VerdictIncomplete name the
    acceptance and gate verdicts one annex line carries: pass and fail come
    from the independent verifier, unrated marks the absence of a verdict, and
    incomplete is the gate's persistent marker for a sample missing an element.

const (
	// RuleVersionV1 is the first accepted version of the independent
	// acceptance rules.
	RuleVersionV1 = "v1"
	// GateRuleVersionV1 is the first version of the sample qualification gate.
	GateRuleVersionV1 = "gate/v1"
)
    RuleVersionV1 and GateRuleVersionV1 name the derivation rule versions an
    annex line carries: the acceptance rules a verdict was reached under,
    and the gate rules an incomplete marker was reached under. A later rule
    change writes a new version rather than editing an earlier verdict.

VARIABLES

var (
	// ErrSampleCountMismatch reports a gate run whose read-back evidences do
	// not pair one-to-one with its samples.
	ErrSampleCountMismatch = errors.New("sample: the read-back evidences do not pair with the samples")
	// ErrSampleSinkMissing reports a gate run beside no capture sink: the gate
	// writes its verdict into the annex, and a gate with no write point cannot
	// make a sample complete.
	ErrSampleSinkMissing = errors.New("sample: the gate has no capture sink to write the annex through")
	// ErrSampleVerdictUnknown reports an annex verdict outside the acceptance
	// vocabulary; an unknown verdict is never counted as a pass or as unrated.
	ErrSampleVerdictUnknown = errors.New("sample: the annex verdict is outside the acceptance vocabulary")
)
    ErrSampleCountMismatch and the other named errors of the sample gate:
    each names the way a gate run was refused rather than producing a rate over
    a shape it could not read.

FUNCTIONS

func Accept(attemptID event.AttemptID, task bench.Task, workspace, output string) rl.AnnexRecord

    Accept is the independent acceptance of one training sample: the verdict
    is reached by running the bench task's verifier over the task's artifacts,
    never from the execution path's self-report. The acceptance annex it returns
    carries the verdict, the acceptance rule version and the artifact references
    the verdict read, bound to the attempt and the bench task together.

    A task the verifier rejects is not an error here: the sample is a valid
    negative sample and its verdict is recorded as fail. The execution path's
    claim of success or failure is not an input to this function at all,
    so a self-report that contradicts the artifacts cannot change the verdict.

func Evaluate(ctx context.Context, sink *rl.CaptureSink, samples []rl.AnnexRecord, readback []bool) (total int, complete int, err error)
    Evaluate runs the sample qualification gate over one run's samples
    before the seal, writes every sample's annex through the capture sink,
    and reports the sample count and how many carry all three elements.
    - a sample is complete only with read-back proof, an independent pass/fail
    verdict, and that verdict written into the annex - a missing element appends
    an incomplete marker under the same identity; the earlier verdict is never
    rewritten - the rate counts every sample in its denominator, incomplete ones
    included - the gate runs before the seal; the sink's post-seal refusal is

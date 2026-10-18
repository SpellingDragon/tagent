package rlexample // import "github.com/SpellingDragon/tagent/examples/contract/rl"

Package rlexample holds the canonical examples of the training observation faces
a request boundary, a production point and an authorized export run the runtime
through.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleCapture()
    ExampleCapture fixes one request view, admits an attempt up to the bound
    and reports that the attempt past the bound is refused by name rather than
    dropped silently.

func ExampleCaptureWriter()
    ExampleCaptureWriter persists two record lines, seals the files it wrote and
    commits the confirmed manifest.

func ExampleExportSource()
    ExampleExportSource reads one page of a partition, reports the offset past
    the end as an empty page and returns the episodes the split groups are drawn
    on.

func ExampleExporter()
    ExampleExporter runs one authorized export over the granted partition and
    reports that a request naming no partition is refused before any read.

func ExampleFeedbackTargetResolver()
    ExampleFeedbackTargetResolver resolves the decision and attempt one event
    key names and reports a key the evidence does not hold as unknown instead of
    a resolved target.

func ExampleRecorder()
    ExampleRecorder writes one episode, one decision and one attempt at the
    points they originate, and reports an attempt that names no decision as an
    error rather than dropping it.


package memorysearchexample // import "github.com/SpellingDragon/tagent/examples/contract/memory_search"

Package memorysearchexample holds the canonical examples of the retrieval
faces a hybrid recall runs one query through: the authorized-scope query port,
the fusion port, and the two seams a retrieval reads its legs from.

One file per interface, each a minimal implementation beside the Example
function that drives it through its real caller.

FUNCTIONS

func ExampleFusion()
    ExampleFusion fuses one vector leg and one keyword leg into a single
    reference set that carries each hit once and no continuation.

func ExampleReferenceSource()
    ExampleReferenceSource reads one page of matching references in (Timestamp,
    Key) order and hydrates the recall reference of the key the page names.

func ExampleSearcher()
    ExampleSearcher answers one query inside the authorized partitions,
    bounded by the requested page, and reports an unauthorized partition as no
    hit rather than a widened read.

func ExampleVectorSource()
    ExampleVectorSource reads the vector of one completed generation and reports
    a key whose generation has not completed as not ready instead of inferring a
    vector for it.


package memoryexample // import "github.com/SpellingDragon/tagent/examples/contract/memory"

Package memoryexample holds the canonical example of the embedding seam a recall
path generates its vectors through.

The file is the minimal implementation beside the Example function that drives
it through its real caller.

FUNCTIONS

func ExampleEmbedder()
    ExampleEmbedder embeds one text under the binding that borrowed the seam and
    reports that empty text is refused by name instead of answered with a zero
    vector.


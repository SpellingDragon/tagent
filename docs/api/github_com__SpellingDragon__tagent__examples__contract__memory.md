package memoryexample // import "github.com/SpellingDragon/tagent/examples/contract/memory"

Package memoryexample holds the canonical example of the query-time embedder a
recall path calls for candidate vectors.

The file is the minimal implementation beside the Example function that drives
it through its real caller.

FUNCTIONS

func ExampleEmbedder()
    ExampleEmbedder embeds one candidate text under the bound embedder and
    reports that empty text is refused by name instead of answered with a zero
    vector.


package memoryreplayexample // import "github.com/SpellingDragon/tagent/examples/contract/memory_replay"

Package memoryreplayexample holds the canonical example of the fact chain a
replay scans and a projection rebuilds: the committed facts in their EventKey
write order, read forward from an identity and by the key one fact is quoted by.

The file is the minimal implementation beside the Example function that drives
it through its real caller.

FUNCTIONS

func ExampleEventChain()
    ExampleEventChain replays one page of the chain after a given key, then
    hydrates the fact a compaction ref names.


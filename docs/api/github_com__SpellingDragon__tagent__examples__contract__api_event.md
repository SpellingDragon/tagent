package eventexample // import "github.com/SpellingDragon/tagent/examples/contract/api_event"

Package eventexample holds the canonical example of the fact category contract:
the identity a fact is quoted by and the frozen type it is read as, read the way
a consumer that speaks only the vocabulary reads them.

The file is the minimal implementation beside the Example function that drives
it.

FUNCTIONS

func ExampleFact()
    ExampleFact files one event family fact and one compress request family
    fact, reports that a restatement on a non-identity field keeps the identity
    and type it was filed under, and reports that a second fact quoting the same
    identity is refused.


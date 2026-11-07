package recallexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_recall"

Package recallexample holds the canonical example of the durable read face the
recall tools use: the declarative query page, the reference of one event read
from the chain, and the original reference the chain states for it.

The file is the minimal implementation beside the Example function that drives
it.

FUNCTIONS

func ExampleTable()
    ExampleTable demonstrates the durable read face's partition scope, total
    order and exact original reference, with no observation table or landing
    pass.


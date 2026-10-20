package rootexample // import "github.com/SpellingDragon/tagent/examples/contract/root"

Package rootexample shows how a host satisfies the MessageSender port.

FUNCTIONS

func ExampleMessageSender()
    ExampleMessageSender demonstrates the port a host fills: one address,
    one message, one error — no batching, no retry, no channel semantics.

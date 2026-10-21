package rootexample // import "github.com/SpellingDragon/tagent/examples/contract/root"

Package rootexample shows how a host satisfies the MessageSender port.

Package rootexample shows how a host satisfies the SessionFolds port.

FUNCTIONS

func ExampleMessageSender()
    ExampleMessageSender demonstrates the port a host fills: one address,
    one message, one error — no batching, no retry, no channel semantics.

func ExampleSessionFolds()
    ExampleSessionFolds demonstrates the face a host fills for the age fold
    plugin: the session of one commit, the projection and window that session
    registered, and the filing of the judgment a fold answered — the plugin
    holds no chain and no ledger of its own.

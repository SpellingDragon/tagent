package assemblyexample // import "github.com/SpellingDragon/tagent/examples/contract/assembly"

Package assemblyexample shows how a host satisfies the MessageSender port.

Package assemblyexample shows how a host satisfies the SessionFolds port.

Package assemblyexample shows how a host satisfies the SettleWake port.

FUNCTIONS

func ExampleMessageSender()
    ExampleMessageSender demonstrates the port a host fills: one address,
    one message, one error — no batching, no retry, no channel semantics.

func ExampleSessionFolds()
    ExampleSessionFolds demonstrates the face a host fills for the age fold
    plugin: the session of one commit, the projection and window that session
    registered, and the filing of the judgment a fold answered — the plugin
    holds no chain and no ledger of its own.

func ExampleSettleWake()
    ExampleSettleWake demonstrates the account the settle pump drives its turns
    from: one entry is claimed for exactly one turn, and only its consumption
    receipt closes it — a turn that stopped before its delivery releases the
    entry back, so the work stays owed instead of dying with the turn.

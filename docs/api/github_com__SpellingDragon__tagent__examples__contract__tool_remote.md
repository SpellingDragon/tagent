package remoteexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_remote"

Package remoteexample holds the canonical examples of the remote delegation
face: the explicitly addressed connection, the session association of that call,
and the dispatcher that owns the retry shape of one call.

One file per interface, each a minimal implementation beside the Example
function that drives it.

FUNCTIONS

func ExampleConnector()
    ExampleConnector opens one explicitly addressed connection: an endpoint
    the caller named gets a client, while a target that names none is refused
    instead of falling back to a default destination.

func ExampleDispatcher()
    ExampleDispatcher sends one addressed call: the request goes out under
    the identity and target it was handed, a call that names no invocation is
    refused before anything is sent, and the call leaves one outcome instead of
    one outcome per retry.

func ExampleSessionBinder()
    ExampleSessionBinder resolves the session one remote call runs under:
    the scope names the local agent and session the call is attributed to,
    while a target that carries no identity is refused rather than bound to a
    scope the caller cannot attribute.


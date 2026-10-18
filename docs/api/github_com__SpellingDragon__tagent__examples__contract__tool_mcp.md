package mcpexample // import "github.com/SpellingDragon/tagent/examples/contract/tool_mcp"

Package mcpexample holds the canonical examples of the MCP gateway: the live
tool-set registry, the native connection, discovery over that registry, and the
re-issued declarations of the built-in gateway tools.

One file per interface, each a minimal implementation beside the Example
function that drives it.

FUNCTIONS

func ExampleConnector()
    ExampleConnector builds one native tool set from its declaration: a
    declaration naming no known transport is refused before anything is dialled,
    so the registry is never handed a set whose session was never up.

func ExampleDeclarationReloader()
    ExampleDeclarationReloader re-issues the gateway declarations: the two
    built-in tool names come back in request order and each carries its input
    schema, so a request is handed the same declarations whether or not a server
    connection changed.

func ExampleDiscovery()
    ExampleDiscovery resolves the tools reachable through one registry: the call
    is handed the registry and reads the tools from it, while a refresh of no
    live set is refused instead of answered from a listing taken earlier.

func ExampleRegistry()
    ExampleRegistry registers one live server and then removes it: the names
    come back in ascending order, the removed registration is gone, and a name
    nobody registered is refused instead of answered with an empty set.


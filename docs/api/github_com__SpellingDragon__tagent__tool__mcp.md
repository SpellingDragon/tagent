package mcp // import "github.com/SpellingDragon/tagent/tool/mcp"

Package mcp adapts the locked tRPC-Agent-Go MCP tool set to the runtime: it
registers live server connections, reads discovery from the generation a request
borrowed, and declares the fixed gateway tools the model sees.

The MCP wire protocol is not reimplemented here. Connection setup,
session initialization and tool listing stay with the native
trpc.group/trpc-go/trpc-agent-go/tool/mcp types this package registers and hands
out.

The declaration face of the MCP axis: one generation of named servers as the
deployment declares them, bound onto the live registry the gateways read.
The binding moves connections only — no agent's declared tool set changes when a
server appears or disappears, because the gateway declarations are constants.

CONSTANTS

const (
	// ToolNameMCPCall is the fixed name of the built-in MCP call gateway tool.
	ToolNameMCPCall = "mcp_call"

	// ToolNameMCPDiscover is the fixed name of the built-in MCP discovery tool.
	ToolNameMCPDiscover = "mcp_discover"

	// MCPCallArgServer is the argument key naming the target MCP server.
	MCPCallArgServer = "server"

	// MCPCallArgTool is the argument key naming the tool on that server.
	MCPCallArgTool = "tool"

	// MCPCallArgArgs is the argument key carrying the remote tool arguments.
	MCPCallArgArgs = "args"
)
    ToolNameMCPCall and ToolNameMCPDiscover name the built-in gateway tools,
    and the MCPCallArg keys name the three arguments of the call gateway.
    Registry content changes none of them, so an agent's declared tool prefix
    stays byte-identical as servers are added or removed.

const MCPCallInputSchemaJSON = `{
  "type": "object",
  "properties": {
    "server": {"type": "string"},
    "tool": {"type": "string"},
    "args": {"type": "object"}
  },
  "required": ["server", "tool", "args"]
}`
    MCPCallInputSchemaJSON is the canonical JSON Schema of the mcp_call gateway
    declaration: an object with exactly the three required keys server, tool and
    args, where args carries the argument object forwarded to the remote tool.

    A schema constant, not a function of the registry: a call reaches whatever
    the registry holds at call time while the declaration the model was given
    stays the same value.

const MCPDiscoverInputSchemaJSON = `{
  "type": "object",
  "properties": {
    "query": {"type": "string"}
  }
}`
    MCPDiscoverInputSchemaJSON is the canonical JSON Schema of the mcp_discover
    gateway declaration: an object with one optional query key filtering the
    discovered tools. A schema constant, not a function of the registry.

VARIABLES

var (
	// ErrNameMissing reports a registration under no server name.
	ErrNameMissing = errors.New("mcp: a server name is required")

	// ErrServerExists reports a registration under a name already taken.
	ErrServerExists = errors.New("mcp: a server is already registered under that name")

	// ErrServerUnknown reports a removal of, or a lookup by, a name the table
	// does not hold.
	ErrServerUnknown = errors.New("mcp: no server is registered under that name")

	// ErrRegistryClosed reports a registration attempted after the shutdown
	// released the table.
	ErrRegistryClosed = errors.New("mcp: the registry is shut down")
)
    ErrNameMissing and the other named failures of the live registry face each
    name the state they report, so a caller can tell a duplicate registration
    from a missing one and a shut-down table from an empty one.

var ErrRegistryMissing = errors.New("mcp: the declaration names servers beside no live registry")
    ErrRegistryMissing reports a binding beside no live registry: the servers
    would be declared and unreachable, so the assembly is refused by name.

var ErrServerDeclaredTwice = errors.New("mcp: the declaration names the same server twice")
    ErrServerDeclaredTwice reports one generation naming the same server twice.

var ErrServerKeyMissing = errors.New("mcp: the key variable of the declared server is not set")
    ErrServerKeyMissing reports a declared server whose key variable is unset:
    the declaration promises a connection this process cannot authenticate,
    so the assembly is refused by name instead of opening an anonymous one.

var ErrServerURLMissing = errors.New("mcp: the declared server names no url")
    ErrServerURLMissing reports a declared server that names no endpoint.

FUNCTIONS

func ValidateDeclared(declared []DeclaredServer) error
    ValidateDeclared reports whether one generation can be bound at all: every
    server names an endpoint, no name repeats and every key variable is set.

TYPES

type Binder struct {
	// Has unexported fields.
}
    Binder binds one generation of declared servers onto one live registry and
    remembers the generation it applied, so a reload only moves the servers
    whose declaration changed.

func NewBinder(registry Registry) *Binder
    NewBinder returns the binder of one live registry.

func (b *Binder) Bind(ctx context.Context, declared []DeclaredServer) error
    Bind brings the live registry onto one generation of declared servers:
    a server the generation dropped or redeclared is released, a server it added
    or redeclared is registered, and a server whose declaration did not change
    is left untouched. Registration stays lazy — the native tool set connects at
    its first use, so one unreachable server never blocks its siblings.

type CallTool struct {
	// Has unexported fields.
}

    CallTool is the fixed mcp_call gateway: one plain tool with the constant
    server/tool/args declaration whose calls reach whatever the injected live
    registry holds at call time. The declaration never reads the registry, so
    runtime registration changes the reachable tool surface and never the tool
    set an agent declared. Every failure comes back as a named result carrying
    the material to correct the next call, and never as a Go error.

func NewCallTool(registry Registry) *CallTool
    NewCallTool builds the call gateway over the injected live registry.
    A missing registry must still build: it changes what a call answers,
    never whether the tool exists.

func (t *CallTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call routes one call to the named tool of the named server through the live
    registry. The wire protocol stays with the locked framework tool set the
    registry hands out. Failures answer as named results: an empty table names
    both registration sources, an unknown server echoes the registered names,
    an unknown tool echoes that server's tool list, and a failed inner call
    carries the target tool's input schema.

    - A successful call returns the native result untouched: the gateway adds no
    envelope of its own to a call that answered.

func (t *CallTool) Declaration() *trpctool.Declaration
    Declaration returns the fixed declaration of the call gateway: the same
    name, description and schema whatever the registry holds.

type Connector interface {
	// Connect creates and initializes the native tool set described by config.
	Connect(ctx context.Context, config trpcmcp.ConnectionConfig, opts ...trpcmcp.ToolSetOption) (*trpcmcp.ToolSet, error)
}
    Connector builds one native MCP tool set from its connection declaration and
    brings its session up before the registry hands the tool set out.

    The redline list (3 rules): MUST build one native tool set from its
    connection declaration. MUST bring the tool set's session up before the
    registry hands it out. MUST NOT hand out a tool set whose session is not up.

type DeclarationReloader interface {
	// ReloadDeclarations returns the declarations of the built-in gateway tools
	// in the order they are handed to a request.
	ReloadDeclarations(ctx context.Context) []*trpctool.Declaration
}
    DeclarationReloader re-issues the declarations of the built-in gateway
    tools.

    The redline list (2 rules): MUST return the declarations in the order they
    are handed to a request. MUST NOT answer a declaration of a tool the gateway
    does not serve.

type DeclaredServer struct {
	// Name is the registry identity the gateways address the server by.
	Name string
	// Transport is the declared transport spelling; an empty spelling means
	// the streamable HTTP transport.
	Transport string
	// URL is the endpoint of the streamable or SSE transport.
	URL string
	// APIKeyEnv names the environment variable holding the key; an empty name
	// declares a server that needs no key.
	APIKeyEnv string
}
    DeclaredServer is one MCP server the organization declaration names:
    the transport, the endpoint and the environment variable holding its key.

type DiscoverTool struct {
	// Has unexported fields.
}

    DiscoverTool is the fixed mcp_discover gateway: one plain tool whose calls
    read the injected live registry at call time, so a server registered at
    runtime is immediately discoverable and a removed one immediately gone.
    Its declaration is a constant of the package and never reads the registry.

func NewDiscoverTool(registry Registry, discovery Discovery) *DiscoverTool
    NewDiscoverTool builds the discovery gateway over the injected live
    registry. A missing registry or discovery face must still build: it changes
    what a read answers, never whether the tool exists.

func (t *DiscoverTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call reads the reachable tool surface of the live registry and answers
    the flat list with its per-tool call guidance. An empty registry answers
    tools=[] and count=0 without an error, and a broken server is reported as a
    named failure beside the results of its healthy siblings.

func (t *DiscoverTool) Declaration() *trpctool.Declaration
    Declaration returns the fixed declaration of the discovery gateway: the same
    name, description and schema whatever the registry holds.

type DiscoveredTool struct {
	Server string
	Decl   *trpctool.Declaration
}
    DiscoveredTool is one reachable tool as discovery reports it: the fixed
    server identity it is reachable under beside the declaration the native tool
    set carries for it. It declares the target tool's input schema, so a model
    can shape the arguments before it calls the gateway.

func (d *DiscoveredTool) Declaration() *trpctool.Declaration
    Declaration returns the declaration of the remote tool.

func (d *DiscoveredTool) ServerName() string
    ServerName returns the server the tool is reachable through.

type Discovery interface {
	// Discover returns the tools reachable through registry as read for the
	// current request, and carries the input schema of every tool it returns.
	Discover(ctx context.Context, registry Registry) ([]trpctool.Tool, error)

	// Refresh re-lists the tools of one live tool set.
	Refresh(ctx context.Context, toolset trpctool.ToolSet) error
}
    Discovery resolves the tools reachable through one registry and re-lists a
    live tool set.

    The redline list (3 rules): MUST carry the input schema of every tool it
    returns. MUST re-list a live tool set instead of reusing a listing taken
    earlier. MUST NOT list a tool reachable only outside the registry it was
    handed.

type Gateways struct {
	// Has unexported fields.
}

    Gateways carries the two built-in gateway tools of one injected live
    registry beside the fixed declarations they hand to a request. The
    declarations are constants of the tools, so re-issuing them always answers
    the same values.

func NewGateways(registry Registry, discovery Discovery) *Gateways
    NewGateways builds both gateway tools over the same live registry: one table
    the call gateway and the discovery read, so a server registered at runtime
    is immediately both callable and discoverable.

func (g *Gateways) CallTool() *CallTool
    CallTool returns the mcp_call gateway.

func (g *Gateways) DiscoverTool() *DiscoverTool
    DiscoverTool returns the mcp_discover gateway.

func (g *Gateways) ReloadDeclarations(_ context.Context) []*trpctool.Declaration
    ReloadDeclarations re-issues the declarations of the built-in gateway tools
    in the order they are handed to a request: fixed values, independent of the
    registry content.

type NativeConnector struct{}

    NativeConnector builds one native MCP tool set from its connection
    declaration and brings its session up before the tool set is handed out:
    the handshake and the tool listing run through the locked framework types
    the connector returns, and nothing here implements the wire protocol.
    A session that fails to come up is released before the failure is reported,
    so a refused connection never leaks a client.

func NewNativeConnector() *NativeConnector
    NewNativeConnector returns the connector over the locked framework tool set.

func (c *NativeConnector) Connect(ctx context.Context, config trpcmcp.ConnectionConfig, opts ...trpcmcp.ToolSetOption) (*trpcmcp.ToolSet, error)
    Connect creates the native tool set described by config and initializes its
    session. The transport spellings the configuration layer accepts are folded
    onto the spellings the locked framework knows before the tool set is built:
    the remote spellings collapse onto streamable while the framework's own
    spellings pass through unchanged.

type Registry interface {
	// Get returns the tool set registered under name and reports whether it is
	// present.
	Get(ctx context.Context, name string) (trpctool.ToolSet, bool)

	// List returns every registered tool set.
	List(ctx context.Context) []trpctool.ToolSet

	// Names returns the registered server names in ascending order.
	Names(ctx context.Context) []string

	// Add registers a live tool set under name.
	Add(ctx context.Context, name string, toolset trpctool.ToolSet) error

	// Remove unregisters name and releases its tool set.
	Remove(ctx context.Context, name string) error
}
    Registry is the concurrent-safe map from server name to live native tool set
    that discovery and the gateway read.

    The redline list (4 rules): MUST be safe for concurrent use. MUST list the
    registered server names in ascending order. MUST NOT change any agent's
    declared tool set when a connection is added or removed. MUST NOT hand out a
    tool set once its registration ended.

type RegistryDiscovery struct{}

    RegistryDiscovery resolves the tools reachable through the live registry:
    one read per registered server through the native tool set the registry
    hands out, with a named failure per server that cannot be read. Nothing here
    speaks the wire protocol — listing and re-listing run through the locked
    framework tool sets.

func NewRegistryDiscovery() *RegistryDiscovery
    NewRegistryDiscovery returns the discovery over the live registry.

func (d *RegistryDiscovery) Discover(ctx context.Context, registry Registry) ([]trpctool.Tool, error)
    Discover reads every registered server once and answers the flat tool list
    beside the named failures: a server whose refresh fails is reported by name
    and never blocks the results of its siblings. A server removed between the
    name listing and the borrow is simply gone — that is the live registry,
    not a failure.

func (d *RegistryDiscovery) Refresh(ctx context.Context, toolset trpctool.ToolSet) error
    Refresh re-lists the tools of one live tool set through the native re-list
    face the locked framework exposes on its MCP tool sets. A failure names the
    server it belongs to. A tool set without a re-list face keeps the listing it
    already has — there is nothing to refresh for it.

type ServerFailure struct {
	Server string `json:"server"`
	Error  string `json:"error"`
}
    ServerFailure names one server whose discovery did not fully succeed.

type ServerFailures struct {
	Failures []ServerFailure
}
    ServerFailures carries the named failures of one discovery read. Partial
    results stand beside it: one broken server never hides the tools of the
    others, and its failure is named instead of silently dropped.

func (f *ServerFailures) Error() string
    Error reports every named failure on one line per server.

type ServerRegistry struct {
	// Has unexported fields.
}

    ServerRegistry is the concurrent-safe table from server name to the live
    native tool set registered under it — the one table the call gateway and
    the discovery read at call time. Registration moves live connections only:
    no method here changes any agent's declared tool set, because the gateway
    tools declare fixed schemas and read this table when they are called.

    The wire protocol stays inside the locked framework: this registry hands
    out and releases the native trpc.group/trpc-go/trpc-agent-go/tool/mcp tool
    sets and speaks no MCP message of its own. A release runs the tool set's
    Close exactly once — a removal or the shutdown of a server whose call is
    still in flight waits for that call to end before the connection closes,
    and the registry keeps every release error on its close ledger to read back
    through Close.

func NewServerRegistry() *ServerRegistry
    NewServerRegistry opens an empty live registry.

func (r *ServerRegistry) Add(_ context.Context, name string, toolset trpctool.ToolSet) error
    Add registers one live tool set under name. Registration is lazy: it moves
    the connection the caller already holds and performs no MCP traffic of its
    own, so a server whose connection fails later fails at its first use and
    never blocks a sibling.

func (r *ServerRegistry) Borrow(_ context.Context, name string) (trpctool.ToolSet, func(), bool)
    Borrow hands out the tool set registered under name beside the release
    that ends one use of it: from the borrow until its release the connection
    stays alive even if the name is removed or the registry shuts down, so an
    in-flight call never loses the connection it is running on. The release runs
    no more than once, and a release that performs the deferred close keeps that
    close's error on the close ledger.

func (r *ServerRegistry) Close() error
    Close shuts the registry down and releases every registered tool set:
    a release whose call is still in flight waits for that call to end, and
    every tool set's Close runs exactly once. Close is idempotent — a repeated
    call releases nothing anew and answers with the release errors accumulated
    so far, so a close a late release performed stays readable through it.

func (r *ServerRegistry) Get(_ context.Context, name string) (trpctool.ToolSet, bool)
    Get returns the tool set registered under name and reports whether the table
    still holds it. A server whose release is pending on an in-flight call is
    already gone from the table.

func (r *ServerRegistry) List(_ context.Context) []trpctool.ToolSet
    List returns the live tool sets of the table in ascending name order,
    so a caller that only wants the sets sees the same order the names read in.

func (r *ServerRegistry) Names(_ context.Context) []string
    Names returns the registered server names in ascending order.

func (r *ServerRegistry) Remove(_ context.Context, name string) error
    Remove unregisters name and releases its tool set. A release whose call is
    still in flight waits for that call to end before the connection closes,
    so the call keeps the connection it was handed; however the release lands,
    the tool set's Close runs exactly once.

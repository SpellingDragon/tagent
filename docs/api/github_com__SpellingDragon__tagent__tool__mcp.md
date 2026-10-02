package mcp // import "github.com/SpellingDragon/tagent/tool/mcp"

Package mcp provides the MCP server registry and the mcp_call gateway tool
implementing tagent's discovery-execution loop for MCP :

- Registry: a concurrency-safe name → ToolSet table, declared via the top-level
YAML mcp_servers section and mutable at runtime (Go API + config-file mtime
hot-sync). Registry mutations never touch any agent's tool declaration set,
so the prompt prefix (tools region) stays byte-stable and prefix caches are
never invalidated by MCP changes. - mcp_call: a fixed-declaration gateway tool
(server/tool/args) that resolves the target through the registry at call time.

Discovery (mcp_discover, in tool/knowledge) reads the same registry,
so runtime-registered servers become discoverable and callable immediately.

CONSTANTS

const CallToolName = "mcp_call"
    CallToolName is the registry ID of the mcp_call gateway tool.

FUNCTIONS

func NormalizeTransport(t string) string
    NormalizeTransport maps user-facing transport aliases to the identifiers
    accepted by trpc-agent-go/tool/mcp ("stdio", "sse", "streamable").

func RegisterTool()
    RegisterTool registers mcp_call as a builtin plain tool. Called by
    tagent.RegisterBuiltinTools(). The factory succeeds even without a wired
    registry (mirroring mcp_discover's empty-stub behavior) so YAML references
    never fail at build time.

TYPES

type CallTool struct {
	// Has unexported fields.
}
    CallTool is the generic MCP execution gateway. Its declaration is CONSTANT
    (server/tool/args) regardless of registry content, so agents holding it keep
    a byte-stable tools prefix while the reachable MCP surface changes freely
    underneath.

    Failures return a callErrorResult with nil error (the same pattern as
    web_search's Message field) so the self-correction material — available
    servers/tools, the target's InputSchema — reaches the model as a normal tool
    result it can act on.

func NewCallTool(reg tagenttool.MCPRegistry) *CallTool
    NewCallTool creates the mcp_call gateway over the given registry.

func (t *CallTool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements trpctool.CallableTool.

func (t *CallTool) Declaration() *trpctool.Declaration
    Declaration implements trpctool.Tool. The schema is fixed by design — see
    the CallTool doc comment.

func (t *CallTool) SetDegradation(d *reliability.DegradationManager)
    SetDegradation 注入退化状态机（工厂从 PlainToolFactoryConfig.Degradation）。nil = 不上报。

func (t *CallTool) SetMCPProbeEvery(n int)
    SetMCPProbeEvery配置熔断半开探测间隔：DepMCP degraded 时 每 N 次调用放行 1 次真探测（其余直接返回熔断
    result），探测成功经既有成功上报路径 触发恢复。N<=0 = 关闭熔断（零行为变化）。

type Option func(*Registry)
    Option configures a Registry.

func WithConfigPath(path string) Option
    WithConfigPath binds the registry to a config file whose mcp_servers section
    is lazily hot-synced on each read. Empty disables hot-sync.

type Registry struct {
	// Has unexported fields.
}
    Registry is a concurrency-safe MCP server registry. Reads (Get/List/ Names)
    reflect the CURRENT content: when a config path is bound, each read lazily
    checks the file mtime and diff-applies the mcp_servers section first (same
    pattern as prompt.Source hot-reload).

    Registry mutations never change any agent's tool declaration set — only what
    mcp_discover/mcp_call can resolve at call time.

func NewRegistry(opts ...Option) *Registry
    NewRegistry creates an empty registry.

func (r *Registry) Add(name string, ts trpctool.ToolSet)
    Add registers (or replaces) a toolset under name at runtime. Entries
    added here are manual: config hot-sync never removes them, but a config
    declaration with the same name takes precedence (declared source of truth)
    with a warning.

func (r *Registry) Close() error
    Close closes all registered toolsets. Idempotent; the registry rejects
    further mutations afterwards. Registered on the entry agent via
    RegisterCloser for graceful shutdown.

func (r *Registry) Get(name string) (trpctool.ToolSet, bool)
    Get returns the toolset registered under name.

func (r *Registry) List() []trpctool.ToolSet
    List returns all registered toolsets, sorted by name (deterministic discover
    output and error listings).

func (r *Registry) Names() []string
    Names returns all registered server names, sorted.

func (r *Registry) Remove(name string) bool
    Remove unregisters name and closes its toolset. Returns true if removed.

func (r *Registry) Seed(servers map[string]ServerConfig)
    Seed registers config-declared servers at build time. Invalid specs
    are skipped with a warning (Config.Validate reports them earlier on the
    LoadConfig path). Seeding baselines the bound config file's mtime so the
    next read does not immediately re-sync the file it was seeded from.

type ServerConfig struct {
	// Transport selects the connection type: "stdio", "sse" or "streamable".
	// Common aliases ("streamable-http", "streamable_http", "http",
	// "streamableHttp") are normalized via NormalizeTransport.
	Transport string `json:"transport" yaml:"transport"`

	// URL is the server endpoint for sse/streamable transports.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// Headers are explicit HTTP headers. A same-name key overrides the
	// Authorization header derived from APIKeyEnv.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// APIKeyEnv names an environment variable holding the API key; when set,
	// "Authorization: Bearer <value>" is added at toolset creation time.
	// A missing variable does NOT block registration — auth errors surface
	// later at lazy connection time as tool errors the model can react to.
	APIKeyEnv string `json:"api_key_env,omitempty" yaml:"api_key_env,omitempty"`

	// Command/Args configure the stdio transport.
	Command string   `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string `json:"args,omitempty" yaml:"args,omitempty"`

	// Timeout is a duration string (e.g. "30s") applied to MCP operations.
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}
    ServerConfig declares one MCP server connection (YAML mcp_servers entry).
    The root package aliases this type as tagent.MCPServerConfig so the
    registry's config hot-sync can re-parse the same shape without importing the
    root package.

func (c ServerConfig) Validate(name string) error
    Validate checks the declaration after transport normalization:
    sse/streamable require url, stdio requires command.

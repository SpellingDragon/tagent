package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// ErrNameMissing and the other named failures of the live registry face each name the state they
// report, so a caller can tell a duplicate registration from a missing one and a shut-down table from
// an empty one.
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

// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// ServerRegistry is the concurrent-safe table from server name to the live
// native tool set registered under it — the one table the call gateway and the
// discovery read at call time. Registration moves live connections only: no
// method here changes any agent's declared tool set, because the gateway tools
// declare fixed schemas and read this table when they are called.
//
// The wire protocol stays inside the locked framework: this registry hands out
// and releases the native trpc.group/trpc-go/trpc-agent-go/tool/mcp tool sets
// and speaks no MCP message of its own. A release runs the tool set's Close
// exactly once — a removal or the shutdown of a server whose call is still in
// flight waits for that call to end before the connection closes, and the
// registry keeps every release error on its close ledger to read back through
// Close.
type ServerRegistry struct {
	mu       sync.Mutex
	entries  map[string]*registryEntry
	closed   bool
	closeErr error
}

// registryEntry is one registered live tool set beside its release bookkeeping:
// the borrows still in flight, whether the name has been unregistered, and
// whether the tool set's Close already ran.
type registryEntry struct {
	name     string
	toolset  trpctool.ToolSet
	refs     int
	unlisted bool
	closed   bool
}

// NewServerRegistry opens an empty live registry.
func NewServerRegistry() *ServerRegistry {
	return &ServerRegistry{entries: map[string]*registryEntry{}}
}

// Get returns the tool set registered under name and reports whether the table
// still holds it. A server whose release is pending on an in-flight call is
// already gone from the table.
func (r *ServerRegistry) Get(_ context.Context, name string) (trpctool.ToolSet, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[name]
	if !ok {
		return nil, false
	}
	return entry.toolset, true
}

// List returns the live tool sets of the table in ascending name order, so a
// caller that only wants the sets sees the same order the names read in.
func (r *ServerRegistry) List(_ context.Context) []trpctool.ToolSet {
	r.mu.Lock()
	defer r.mu.Unlock()
	toolsets := make([]trpctool.ToolSet, 0, len(r.entries))
	for _, name := range r.sortedNames() {
		toolsets = append(toolsets, r.entries[name].toolset)
	}
	return toolsets
}

// Names returns the registered server names in ascending order.
func (r *ServerRegistry) Names(_ context.Context) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sortedNames()
}

// sortedNames reports the registered names in ascending order. The caller
// holds the lock.
func (r *ServerRegistry) sortedNames() []string {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Add registers one live tool set under name. Registration is lazy: it moves
// the connection the caller already holds and performs no MCP traffic of its
// own, so a server whose connection fails later fails at its first use and
// never blocks a sibling.
func (r *ServerRegistry) Add(_ context.Context, name string, toolset trpctool.ToolSet) error {
	if strings.TrimSpace(name) == "" {
		return ErrNameMissing
	}
	if toolset == nil {
		return fmt.Errorf("mcp: server %q carries no tool set", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("%w: %q was offered after the shutdown", ErrRegistryClosed, name)
	}
	if _, ok := r.entries[name]; ok {
		return fmt.Errorf("%w: %q", ErrServerExists, name)
	}
	r.entries[name] = &registryEntry{name: name, toolset: toolset}
	return nil
}

// Remove unregisters name and releases its tool set. A release whose call is
// still in flight waits for that call to end before the connection closes, so
// the call keeps the connection it was handed; however the release lands, the
// tool set's Close runs exactly once.
func (r *ServerRegistry) Remove(_ context.Context, name string) error {
	r.mu.Lock()
	entry, ok := r.entries[name]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrServerUnknown, name)
	}
	delete(r.entries, name)
	entry.unlisted = true
	closeNow := entry.refs == 0 && !entry.closed
	if closeNow {
		entry.closed = true
	}
	toolset := entry.toolset
	r.mu.Unlock()
	if !closeNow {
		return nil
	}
	return r.recordClose(toolset.Close())
}

// Borrow hands out the tool set registered under name beside the release that
// ends one use of it: from the borrow until its release the connection stays
// alive even if the name is removed or the registry shuts down, so an in-flight
// call never loses the connection it is running on. The release runs no more
// than once, and a release that performs the deferred close keeps that close's
// error on the close ledger.
func (r *ServerRegistry) Borrow(_ context.Context, name string) (trpctool.ToolSet, func(), bool) {
	r.mu.Lock()
	entry, ok := r.entries[name]
	if !ok {
		r.mu.Unlock()
		return nil, nil, false
	}
	entry.refs++
	toolset := entry.toolset
	r.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() {
			r.mu.Lock()
			entry.refs--
			closeNow := entry.unlisted && entry.refs == 0 && !entry.closed
			if closeNow {
				entry.closed = true
			}
			r.mu.Unlock()
			if closeNow {
				_ = r.recordClose(toolset.Close())
			}
		})
	}
	return toolset, release, true
}

// Close shuts the registry down and releases every registered tool set: a
// release whose call is still in flight waits for that call to end, and every
// tool set's Close runs exactly once. Close is idempotent — a repeated call
// releases nothing anew and answers with the release errors accumulated so far,
// so a close a late release performed stays readable through it.
func (r *ServerRegistry) Close() error {
	r.mu.Lock()
	if r.closed {
		err := r.closeErr
		r.mu.Unlock()
		return err
	}
	r.closed = true
	var pending []trpctool.ToolSet
	for name, entry := range r.entries {
		delete(r.entries, name)
		entry.unlisted = true
		if entry.refs == 0 && !entry.closed {
			entry.closed = true
			pending = append(pending, entry.toolset)
		}
	}
	r.mu.Unlock()
	err := r.closeErr
	for _, toolset := range pending {
		err = errors.Join(err, r.recordClose(toolset.Close()))
	}
	return err
}

// recordClose keeps one release error on the close ledger and reports it back
// to the caller that performed the release.
func (r *ServerRegistry) recordClose(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeErr = errors.Join(r.closeErr, err)
	return err
}

// borrower is the pinning face of a live registry: one use of a server borrows
// its tool set, and the connection stays alive until the borrow is released
// even when the name is removed or the registry shuts down meanwhile. A
// registry that does not offer this face is read straight through Get and pins
// nothing of its own.
type borrower interface {
	Borrow(ctx context.Context, name string) (trpctool.ToolSet, func(), bool)
}

// borrowServer borrows the named tool set for one use and reports whether the
// registry still holds the name. The release must run on every path.
func borrowServer(ctx context.Context, registry Registry, name string) (trpctool.ToolSet, func(), bool) {
	if registry == nil {
		return nil, nil, false
	}
	if pinning, ok := registry.(borrower); ok {
		return pinning.Borrow(ctx, name)
	}
	toolset, ok := registry.Get(ctx, name)
	if !ok {
		return nil, nil, false
	}
	return toolset, func() {}, true
}

// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
//
// NativeConnector builds one native MCP tool set from its connection
// declaration and brings its session up before the tool set is handed out: the
// handshake and the tool listing run through the locked framework types the
// connector returns, and nothing here implements the wire protocol. A session
// that fails to come up is released before the failure is reported, so a
// refused connection never leaks a client.
type NativeConnector struct{}

// NewNativeConnector returns the connector over the locked framework tool set.
func NewNativeConnector() *NativeConnector {
	return &NativeConnector{}
}

// Connect creates the native tool set described by config and initializes its
// session. The transport spellings the configuration layer accepts are folded
// onto the spellings the locked framework knows before the tool set is built:
// the remote spellings collapse onto streamable while the framework's own
// spellings pass through unchanged.
func (c *NativeConnector) Connect(ctx context.Context, config trpcmcp.ConnectionConfig, opts ...trpcmcp.ToolSetOption) (*trpcmcp.ToolSet, error) {
	config.Transport = normalizeTransport(config.Transport)
	toolset := trpcmcp.NewMCPToolSet(config, opts...)
	if err := toolset.Init(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("mcp: connect to %s: %w", connectionTarget(config), err), toolset.Close())
	}
	return toolset, nil
}

// normalizeTransport folds the transport spellings the connection declaration
// allows onto the ones the locked framework accepts.
func normalizeTransport(transport string) string {
	switch transport {
	case "streamable-http", "streamable_http", "http":
		return "streamable"
	default:
		return transport
	}
}

// connectionTarget names the endpoint one connection declaration points at, for
// a failure to name what it could not reach.
func connectionTarget(config trpcmcp.ConnectionConfig) string {
	if config.ServerURL != "" {
		return fmt.Sprintf("%q", config.ServerURL)
	}
	if config.Command != "" {
		return fmt.Sprintf("command %q", config.Command)
	}
	return config.Transport
}

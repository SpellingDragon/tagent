// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
//
// The declaration face of the MCP axis: one generation of named servers as the
// deployment declares them, bound onto the live registry the gateways read.
// The binding moves connections only — no agent's declared tool set changes
// when a server appears or disappears, because the gateway declarations are
// constants.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// ErrServerURLMissing reports a declared server that names no endpoint.
var ErrServerURLMissing = errors.New("mcp: the declared server names no url")

// ErrServerKeyMissing reports a declared server whose key variable is unset:
// the declaration promises a connection this process cannot authenticate, so
// the assembly is refused by name instead of opening an anonymous one.
var ErrServerKeyMissing = errors.New("mcp: the key variable of the declared server is not set")

// ErrServerDeclaredTwice reports one generation naming the same server twice.
var ErrServerDeclaredTwice = errors.New("mcp: the declaration names the same server twice")

// ErrRegistryMissing reports a binding beside no live registry: the servers
// would be declared and unreachable, so the assembly is refused by name.
var ErrRegistryMissing = errors.New("mcp: the declaration names servers beside no live registry")

// DeclaredServer is one MCP server the organization declaration names: the
// transport, the endpoint and the environment variable holding its key.
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

// Binder binds one generation of declared servers onto one live registry and
// remembers the generation it applied, so a reload only moves the servers
// whose declaration changed.
type Binder struct {
	registry Registry
	applied  map[string]DeclaredServer
}

// NewBinder returns the binder of one live registry.
func NewBinder(registry Registry) *Binder {
	return &Binder{registry: normalizeRegistry(registry), applied: map[string]DeclaredServer{}}
}

// ValidateDeclared reports whether one generation can be bound at all: every
// server names an endpoint, no name repeats and every key variable is set.
func ValidateDeclared(declared []DeclaredServer) error {
	_, err := normalizeDeclared(declared)
	return err
}

// Bind brings the live registry onto one generation of declared servers: a
// server the generation dropped or redeclared is released, a server it added
// or redeclared is registered, and a server whose declaration did not change is
// left untouched. Registration stays lazy — the native tool set connects at its
// first use, so one unreachable server never blocks its siblings.
func (b *Binder) Bind(ctx context.Context, declared []DeclaredServer) error {
	next, err := normalizeDeclared(declared)
	if err != nil {
		return err
	}
	if b == nil || b.registry == nil {
		if len(next) == 0 {
			return nil
		}
		return ErrRegistryMissing
	}
	for name := range b.applied {
		if _, kept := next[name]; kept {
			continue
		}
		if err := b.registry.Remove(ctx, name); err != nil && !errors.Is(err, ErrServerUnknown) {
			return fmt.Errorf("mcp: release the undeclared server %q: %w", name, err)
		}
		delete(b.applied, name)
	}
	for _, name := range sortedServerNames(next) {
		server := next[name]
		if applied, ok := b.applied[name]; ok {
			if applied == server {
				continue
			}
			if err := b.registry.Remove(ctx, name); err != nil {
				return fmt.Errorf("mcp: release the redeclared server %q: %w", name, err)
			}
			delete(b.applied, name)
		}
		toolset := declaredToolSet(server)
		if err := b.registry.Add(ctx, name, toolset); err != nil {
			return fmt.Errorf("mcp: register the declared server %q: %w", name, err)
		}
		b.applied[name] = server
	}
	return nil
}

// normalizeDeclared folds one generation onto its validated form: names
// trimmed, empty transports defaulted, endpoints and key variables required.
func normalizeDeclared(declared []DeclaredServer) (map[string]DeclaredServer, error) {
	next := make(map[string]DeclaredServer, len(declared))
	for _, server := range declared {
		name := strings.TrimSpace(server.Name)
		if name == "" {
			return nil, ErrNameMissing
		}
		if _, ok := next[name]; ok {
			return nil, fmt.Errorf("%w: %q", ErrServerDeclaredTwice, name)
		}
		server.Name = name
		server.Transport = normalizeDeclaredTransport(server.Transport)
		server.URL = strings.TrimSpace(server.URL)
		server.APIKeyEnv = strings.TrimSpace(server.APIKeyEnv)
		if server.URL == "" {
			return nil, fmt.Errorf("%w: %q", ErrServerURLMissing, name)
		}
		if server.APIKeyEnv != "" {
			if key := strings.TrimSpace(os.Getenv(server.APIKeyEnv)); key == "" {
				return nil, fmt.Errorf("%w: server %q names %q", ErrServerKeyMissing, name, server.APIKeyEnv)
			}
		}
		next[name] = server
	}
	return next, nil
}

// declaredToolSet builds the native tool set one declared server names. The
// set carries the declaration as its connection configuration and connects at
// its first use, so binding a generation performs no MCP traffic.
func declaredToolSet(server DeclaredServer) trpctool.ToolSet {
	config := trpcmcp.ConnectionConfig{
		Transport: server.Transport,
		ServerURL: server.URL,
	}
	if server.APIKeyEnv != "" {
		config.Headers = map[string]string{
			"Authorization": "Bearer " + strings.TrimSpace(os.Getenv(server.APIKeyEnv)),
		}
	}
	return trpcmcp.NewMCPToolSet(config, trpcmcp.WithName(server.Name))
}

// normalizeDeclaredTransport folds the transport spellings a declaration may
// write onto the spellings the locked framework knows, and an empty spelling
// onto the streamable HTTP transport the deployment declares by default.
func normalizeDeclaredTransport(transport string) string {
	trimmed := strings.TrimSpace(transport)
	if trimmed == "" {
		return "streamable"
	}
	return normalizeTransport(trimmed)
}

// sortedServerNames reports the names of one generation in ascending order, so
// a binding registers servers in one order across restarts.
func sortedServerNames(servers map[string]DeclaredServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

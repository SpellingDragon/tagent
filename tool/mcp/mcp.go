// Package mcp provides the MCP server registry and the mcp_call gateway
// tool implementing tagent's discovery-execution loop for MCP
// (mcp-discovery-execution-loop):
//
//   - Registry: a concurrency-safe name → ToolSet table, declared via the
//     top-level YAML mcp_servers section and mutable at runtime (Go API +
//     config-file mtime hot-sync). Registry mutations never touch any
//     agent's tool declaration set, so the prompt prefix (tools region)
//     stays byte-stable and prefix caches are never invalidated by MCP
//     changes.
//   - mcp_call: a fixed-declaration gateway tool (server/tool/args) that
//     resolves the target through the registry at call time.
//
// Discovery (mcp_discover, in tool/knowledge) reads the same registry, so
// runtime-registered servers become discoverable and callable immediately.
package mcp

import (
	"fmt"
	"os"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
)

// ServerConfig declares one MCP server connection (YAML mcp_servers entry).
// The root package aliases this type as tagent.MCPServerConfig so the
// registry's config hot-sync can re-parse the same shape without importing
// the root package.
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

// NormalizeTransport maps user-facing transport aliases to the identifiers
// accepted by trpc-agent-go/tool/mcp ("stdio", "sse", "streamable").
func NormalizeTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "streamable-http", "streamable_http", "streamablehttp", "http":
		return "streamable"
	default:
		return strings.ToLower(strings.TrimSpace(t))
	}
}

// Validate checks the declaration after transport normalization:
// sse/streamable require url, stdio requires command.
func (c ServerConfig) Validate(name string) error {
	switch NormalizeTransport(c.Transport) {
	case "stdio":
		if c.Command == "" {
			return fmt.Errorf("mcp server %q: stdio transport requires command", name)
		}
	case "sse", "streamable":
		if c.URL == "" {
			return fmt.Errorf("mcp server %q: %s transport requires url", name, NormalizeTransport(c.Transport))
		}
	default:
		return fmt.Errorf("mcp server %q: unsupported transport %q (use stdio, sse, streamable)", name, c.Transport)
	}
	return nil
}

// newToolSet builds a trpc MCP toolset from the declaration. The API key
// env is read HERE (creation time); connection itself stays lazy — the
// first Tools()/call triggers it.
func newToolSet(name string, cfg ServerConfig) trpctool.ToolSet {
	headers := map[string]string{}
	if cfg.APIKeyEnv != "" {
		if v := os.Getenv(cfg.APIKeyEnv); v != "" {
			headers["Authorization"] = "Bearer " + v
		} else {
			log.Warnf("[mcp] server %q: env %s is not set; requests will likely fail with an auth error", name, cfg.APIKeyEnv)
		}
	}
	// Explicit headers win over the derived Authorization header.
	for k, v := range cfg.Headers {
		headers[k] = v
	}

	conn := trpcmcp.ConnectionConfig{
		Transport: NormalizeTransport(cfg.Transport),
		ServerURL: cfg.URL,
		Headers:   headers,
		Command:   cfg.Command,
		Args:      cfg.Args,
	}
	if cfg.Timeout != "" {
		if d, err := time.ParseDuration(cfg.Timeout); err == nil && d > 0 {
			conn.Timeout = d
		} else {
			log.Warnf("[mcp] server %q: invalid timeout %q, ignoring", name, cfg.Timeout)
		}
	}
	return trpcmcp.NewMCPToolSet(conn, trpcmcp.WithName(name))
}

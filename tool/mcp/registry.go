package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"trpc.group/trpc-go/trpc-agent-go/log"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"

	tagenttool "github.com/SpellingDragon/tagent/tool"
)

// Verify Registry satisfies the read interface consumed by tools.
var _ tagenttool.MCPRegistry = (*Registry)(nil)

// entry tracks a registered toolset plus the metadata hot-sync needs.
type entry struct {
	ts trpctool.ToolSet
	// spec is the declaration that produced ts (zero for manual entries).
	spec ServerConfig
	// fromConfig marks entries managed by config hot-sync; manual entries
	// (Go API / WithMCPToolSets) are never removed by a config re-sync.
	fromConfig bool
}

// Registry is a concurrency-safe MCP server registry. Reads (Get/List/
// Names) reflect the CURRENT content: when a config path is bound, each
// read lazily checks the file mtime and diff-applies the mcp_servers
// section first (same pattern as prompt.Source hot-reload).
//
// Registry mutations never change any agent's tool declaration set — only
// what mcp_discover/mcp_call can resolve at call time.
type Registry struct {
	mu         sync.Mutex
	entries    map[string]*entry
	configPath string
	lastMod    time.Time
	closed     bool
}

// Option configures a Registry.
type Option func(*Registry)

// WithConfigPath binds the registry to a config file whose mcp_servers
// section is lazily hot-synced on each read. Empty disables hot-sync.
func WithConfigPath(path string) Option {
	return func(r *Registry) { r.configPath = path }
}

// NewRegistry creates an empty registry.
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{entries: map[string]*entry{}}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Seed registers config-declared servers at build time. Invalid specs are
// skipped with a warning (Config.Validate reports them earlier on the
// LoadConfig path). Seeding baselines the bound config file's mtime so the
// next read does not immediately re-sync the file it was seeded from.
func (r *Registry) Seed(servers map[string]ServerConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for name, spec := range servers {
		if err := spec.Validate(name); err != nil {
			log.Warnf("[mcp] registry seed: skipping invalid server: %v", err)
			continue
		}
		r.entries[name] = &entry{ts: newToolSet(name, spec), spec: spec, fromConfig: true}
	}
	if r.configPath != "" {
		if info, err := os.Stat(r.configPath); err == nil {
			r.lastMod = info.ModTime()
		}
	}
}

// Add registers (or replaces) a toolset under name at runtime. Entries
// added here are manual: config hot-sync never removes them, but a config
// declaration with the same name takes precedence (declared source of
// truth) with a warning.
func (r *Registry) Add(name string, ts trpctool.ToolSet) {
	if name == "" || ts == nil {
		return
	}
	r.mu.Lock()
	var old trpctool.ToolSet
	if !r.closed {
		if e, ok := r.entries[name]; ok {
			old = e.ts
		}
		r.entries[name] = &entry{ts: ts}
	}
	r.mu.Unlock()
	if old != nil {
		closeToolSets([]trpctool.ToolSet{old})
	}
}

// Remove unregisters name and closes its toolset. Returns true if removed.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	e, ok := r.entries[name]
	if ok {
		delete(r.entries, name)
	}
	r.mu.Unlock()
	if !ok {
		return false
	}
	closeToolSets([]trpctool.ToolSet{e.ts})
	return true
}

// Get returns the toolset registered under name.
func (r *Registry) Get(name string) (trpctool.ToolSet, bool) {
	r.mu.Lock()
	toClose := r.maybeSyncLocked()
	var ts trpctool.ToolSet
	e, ok := r.entries[name]
	if ok {
		ts = e.ts
	}
	r.mu.Unlock()
	closeToolSets(toClose)
	return ts, ok
}

// List returns all registered toolsets, sorted by name (deterministic
// discover output and error listings).
func (r *Registry) List() []trpctool.ToolSet {
	r.mu.Lock()
	toClose := r.maybeSyncLocked()
	names := make([]string, 0, len(r.entries))
	for n := range r.entries {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]trpctool.ToolSet, 0, len(names))
	for _, n := range names {
		out = append(out, r.entries[n].ts)
	}
	r.mu.Unlock()
	closeToolSets(toClose)
	return out
}

// Names returns all registered server names, sorted.
func (r *Registry) Names() []string {
	r.mu.Lock()
	toClose := r.maybeSyncLocked()
	names := make([]string, 0, len(r.entries))
	for n := range r.entries {
		names = append(names, n)
	}
	r.mu.Unlock()
	closeToolSets(toClose)
	sort.Strings(names)
	return names
}

// Close closes all registered toolsets. Idempotent; the registry rejects
// further mutations afterwards. Registered on the entry agent via
// RegisterCloser for graceful shutdown.
func (r *Registry) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	toClose := make([]trpctool.ToolSet, 0, len(r.entries))
	for _, e := range r.entries {
		toClose = append(toClose, e.ts)
	}
	r.entries = map[string]*entry{}
	r.mu.Unlock()
	closeToolSets(toClose)
	return nil
}

// maybeSyncLocked hot-syncs config-origin entries from the bound config
// file when its mtime changed. Returns toolsets to close AFTER the lock is
// released (Close may block on network). Parse failures keep the current
// registry content (graceful degradation, same as prompt.Source).
func (r *Registry) maybeSyncLocked() []trpctool.ToolSet {
	if r.closed || r.configPath == "" {
		return nil
	}
	info, err := os.Stat(r.configPath)
	if err != nil {
		return nil
	}
	mt := info.ModTime()
	if !mt.After(r.lastMod) {
		return nil
	}
	// Baseline first so a broken file is not re-parsed on every read; the
	// fix bumps the mtime again and triggers a fresh sync.
	r.lastMod = mt

	desired, err := parseServersFile(r.configPath)
	if err != nil {
		log.Warnf("[mcp] registry hot-sync: parse %s failed, keeping current servers: %v", r.configPath, err)
		return nil
	}

	var toClose []trpctool.ToolSet
	// Remove config-origin entries no longer declared.
	for name, e := range r.entries {
		if !e.fromConfig {
			continue
		}
		if _, ok := desired[name]; !ok {
			toClose = append(toClose, e.ts)
			delete(r.entries, name)
			log.Infof("[mcp] registry hot-sync: removed server %q", name)
		}
	}
	// Add new entries and rebuild changed ones.
	for name, spec := range desired {
		if err := spec.Validate(name); err != nil {
			log.Warnf("[mcp] registry hot-sync: skipping invalid server: %v", err)
			continue
		}
		if e, ok := r.entries[name]; ok {
			if e.fromConfig && reflect.DeepEqual(e.spec, spec) {
				continue // unchanged — keep the live instance
			}
			if !e.fromConfig {
				log.Warnf("[mcp] registry hot-sync: config declaration %q replaces manually registered toolset", name)
			}
			toClose = append(toClose, e.ts)
			log.Infof("[mcp] registry hot-sync: rebuilding server %q", name)
		} else {
			log.Infof("[mcp] registry hot-sync: added server %q", name)
		}
		r.entries[name] = &entry{ts: newToolSet(name, spec), spec: spec, fromConfig: true}
	}
	return toClose
}

// configFileServers extracts just the mcp_servers section of a config file.
type configFileServers struct {
	MCPServers map[string]ServerConfig `json:"mcp_servers" yaml:"mcp_servers"`
}

// parseServersFile reads the mcp_servers section from a YAML or JSON
// config file (extension-detected, mirroring tagent.LoadConfig).
func parseServersFile(path string) (map[string]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var section configFileServers
	if strings.ToLower(filepath.Ext(path)) == ".json" {
		if err := json.Unmarshal(data, &section); err != nil {
			return nil, err
		}
		return section.MCPServers, nil
	}
	if err := yaml.Unmarshal(data, &section); err != nil {
		return nil, err
	}
	return section.MCPServers, nil
}

// closeToolSets closes toolsets outside the registry lock.
func closeToolSets(sets []trpctool.ToolSet) {
	for _, ts := range sets {
		if ts == nil {
			continue
		}
		if err := ts.Close(); err != nil {
			log.Warnf("[mcp] close toolset %q: %v", ts.Name(), err)
		}
	}
}

package mcpexample

import (
	"context"
	"errors"
	"sort"
	"sync"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"

	"github.com/SpellingDragon/tagent/tool/mcp"
)

// errRegistryName and errRegistryUnknown are what the canonical registry reports for an
// empty server name and for a name it does not hold.
var (
	errRegistryName    = errors.New("mcp: the registration names no server")
	errRegistryUnknown = errors.New("mcp: no server is registered under that name")
)

// liveRegistry is the canonical registry: a concurrent-safe map from server name to live
// native tool set that lists names in ascending order and releases a tool set as it is
// removed.
type liveRegistry struct {
	mu   sync.RWMutex
	sets map[string]trpctool.ToolSet
}

// Get returns the tool set registered under name and reports whether it is present.
func (r *liveRegistry) Get(_ context.Context, name string) (trpctool.ToolSet, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	set, ok := r.sets[name]
	return set, ok
}

// List returns every registered tool set.
func (r *liveRegistry) List(_ context.Context) []trpctool.ToolSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sets := make([]trpctool.ToolSet, 0, len(r.sets))
	for _, set := range r.sets {
		sets = append(sets, set)
	}
	return sets
}

// Names returns the registered server names in ascending order.
func (r *liveRegistry) Names(_ context.Context) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.sets))
	for name := range r.sets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Add registers toolset under name.
func (r *liveRegistry) Add(_ context.Context, name string, toolset trpctool.ToolSet) error {
	if name == "" {
		return errRegistryName
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sets == nil {
		r.sets = map[string]trpctool.ToolSet{}
	}
	r.sets[name] = toolset
	return nil
}

// Remove unregisters name and closes the tool set it held.
func (r *liveRegistry) Remove(_ context.Context, name string) error {
	r.mu.Lock()
	set, ok := r.sets[name]
	delete(r.sets, name)
	r.mu.Unlock()
	if !ok {
		return errRegistryUnknown
	}
	if set == nil {
		return nil
	}
	return set.Close()
}

var _ mcp.Registry = (*liveRegistry)(nil)

// ExampleRegistry registers one live server and then removes it: the names come back in
// ascending order, the removed registration is gone, and a name nobody registered is
// refused instead of answered with an empty set.
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func ExampleRegistry() {
	ctx := context.Background()
	registry := &liveRegistry{}
	set := trpcmcp.NewMCPToolSet(trpcmcp.ConnectionConfig{Transport: "stdio", Command: "true"}, trpcmcp.WithName("files"))
	if err := registry.Add(ctx, "files", set); err != nil {
		return
	}
	if names := registry.Names(ctx); len(names) != 1 || names[0] != "files" {
		return
	}
	if got, ok := registry.Get(ctx, "files"); !ok || got != trpctool.ToolSet(set) {
		return
	}
	if err := registry.Add(ctx, "", set); err == nil {
		return
	}
	if err := registry.Remove(ctx, "files"); err != nil {
		return
	}
	if err := registry.Remove(ctx, "files"); err == nil {
		return
	}
}

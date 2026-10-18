package mcp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestServerRegistryKeepsOneReleasePerRegistration pins the live table: a name owns one tool set and that tool set is closed exactly once.
// - a blank name and a server without a tool set are refused before the table changes
// - a duplicate name is refused by name and the first registration stays in place
// - names and the tool set list read in ascending name order
// - removing a name runs the tool set close exactly once and a second removal is refused by name
// - a borrow keeps the connection alive after removal until its release, and the release closes it exactly once
// - a repeated release runs nothing a second time
// - close is idempotent and releases every server exactly once
// - close joins every release error onto its ledger and a later close reads them back
// - a registration after close is refused by name
// - a release performed after close keeps its error readable through the next close
// - concurrent borrows and releases close the connection exactly once
// - a registry without a borrow face is read through get and pins nothing of its own
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func TestServerRegistryKeepsOneReleasePerRegistration(t *testing.T) {
	ctx := context.Background()

	t.Run("add refuses every incomplete registration", func(t *testing.T) {
		registry := NewServerRegistry()
		if err := registry.Add(ctx, "", &fakeToolSet{name: "s"}); !errors.Is(err, ErrNameMissing) {
			t.Fatalf("registration under no name = %v, want ErrNameMissing", err)
		}
		if err := registry.Add(ctx, "   ", &fakeToolSet{name: "s"}); !errors.Is(err, ErrNameMissing) {
			t.Fatalf("registration under a blank name = %v, want ErrNameMissing", err)
		}
		if err := registry.Add(ctx, "s", nil); err == nil || !strings.Contains(err.Error(), "carries no tool set") {
			t.Fatalf("registration without a tool set = %v, want a named refusal", err)
		}
		if names := registry.Names(ctx); len(names) != 0 {
			t.Fatalf("refused registrations left %v in the table", names)
		}
	})

	t.Run("duplicate name keeps the first registration", func(t *testing.T) {
		registry := NewServerRegistry()
		first := &fakeToolSet{name: "server"}
		second := &fakeToolSet{name: "server"}
		if err := registry.Add(ctx, "server", first); err != nil {
			t.Fatalf("registration = %v", err)
		}
		if err := registry.Add(ctx, "server", second); !errors.Is(err, ErrServerExists) {
			t.Fatalf("duplicate registration = %v, want ErrServerExists", err)
		}
		held, ok := registry.Get(ctx, "server")
		if !ok || held != first {
			t.Fatalf("duplicate registration replaced the first tool set: %v", held)
		}
	})

	t.Run("names and tools read in ascending order", func(t *testing.T) {
		registry := NewServerRegistry()
		beta, alpha := &fakeToolSet{name: "beta"}, &fakeToolSet{name: "alpha"}
		for _, entry := range []struct {
			name string
			set  *fakeToolSet
		}{{"beta", beta}, {"alpha", alpha}} {
			if err := registry.Add(ctx, entry.name, entry.set); err != nil {
				t.Fatalf("registration of %q = %v", entry.name, err)
			}
		}
		names := registry.Names(ctx)
		if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
			t.Fatalf("names = %v, want [alpha beta]", names)
		}
		toolsets := registry.List(ctx)
		if len(toolsets) != 2 || toolsets[0] != alpha || toolsets[1] != beta {
			t.Fatalf("tool sets = %v, want them in the name order [alpha beta]", toolsets)
		}
		if _, ok := registry.Get(ctx, "missing"); ok {
			t.Fatalf("get of an unregistered name reported a tool set")
		}
	})

	t.Run("remove closes exactly once", func(t *testing.T) {
		registry := NewServerRegistry()
		toolset := &fakeToolSet{name: "removed"}
		if err := registry.Add(ctx, "removed", toolset); err != nil {
			t.Fatalf("registration = %v", err)
		}
		if err := registry.Remove(ctx, "removed"); err != nil {
			t.Fatalf("removal = %v", err)
		}
		if got := toolset.closeCount(); got != 1 {
			t.Fatalf("close count after removal = %d, want 1", got)
		}
		if err := registry.Remove(ctx, "removed"); !errors.Is(err, ErrServerUnknown) {
			t.Fatalf("second removal = %v, want ErrServerUnknown", err)
		}
		if got := toolset.closeCount(); got != 1 {
			t.Fatalf("second removal closed the tool set again: %d", got)
		}
	})

	t.Run("borrow outlives removal until release", func(t *testing.T) {
		registry := NewServerRegistry()
		toolset := &fakeToolSet{name: "borrowed"}
		if err := registry.Add(ctx, "borrowed", toolset); err != nil {
			t.Fatalf("registration = %v", err)
		}
		held, release, ok := registry.Borrow(ctx, "borrowed")
		if !ok || held != toolset || release == nil {
			t.Fatalf("borrow = (held %v, ok %v), want the registered tool set", held, ok)
		}
		if err := registry.Remove(ctx, "borrowed"); err != nil {
			t.Fatalf("removal beside a borrow = %v", err)
		}
		if _, ok := registry.Get(ctx, "borrowed"); ok {
			t.Fatalf("a server pending release is still readable through get")
		}
		if got := toolset.closeCount(); got != 0 {
			t.Fatalf("removal closed a borrowed connection: %d", got)
		}
		if names := registry.Names(ctx); len(names) != 0 {
			t.Fatalf("names after removal = %v, want none", names)
		}
		release()
		if got := toolset.closeCount(); got != 1 {
			t.Fatalf("release closed the tool set %d times, want 1", got)
		}
		release()
		if got := toolset.closeCount(); got != 1 {
			t.Fatalf("repeated release closed the tool set %d times, want 1", got)
		}
		if _, _, ok := registry.Borrow(ctx, "borrowed"); ok {
			t.Fatalf("borrow of a removed name reported a tool set")
		}
	})

	t.Run("close is idempotent and joins the ledger", func(t *testing.T) {
		registry := NewServerRegistry()
		firstErr := errors.New("first connection refused to close")
		secondErr := errors.New("second connection refused to close")
		first := &fakeToolSet{name: "first", closeErr: firstErr}
		second := &fakeToolSet{name: "second", closeErr: secondErr}
		if err := registry.Add(ctx, "first", first); err != nil {
			t.Fatalf("registration = %v", err)
		}
		if err := registry.Add(ctx, "second", second); err != nil {
			t.Fatalf("registration = %v", err)
		}
		err := registry.Close()
		if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
			t.Fatalf("close = %v, want both release errors on the ledger", err)
		}
		if first.closeCount() != 1 || second.closeCount() != 1 {
			t.Fatalf("close counts = %d and %d, want one each", first.closeCount(), second.closeCount())
		}
		again := registry.Close()
		if !errors.Is(again, firstErr) || !errors.Is(again, secondErr) {
			t.Fatalf("repeated close = %v, want the same ledger", again)
		}
		if first.closeCount() != 1 || second.closeCount() != 1 {
			t.Fatalf("repeated close released a tool set again: %d and %d", first.closeCount(), second.closeCount())
		}
		if err := registry.Add(ctx, "late", &fakeToolSet{name: "late"}); !errors.Is(err, ErrRegistryClosed) {
			t.Fatalf("registration after close = %v, want ErrRegistryClosed", err)
		}
	})

	t.Run("release after close stays readable", func(t *testing.T) {
		registry := NewServerRegistry()
		lateErr := errors.New("the connection closed late")
		toolset := &fakeToolSet{name: "late", closeErr: lateErr}
		if err := registry.Add(ctx, "late", toolset); err != nil {
			t.Fatalf("registration = %v", err)
		}
		_, release, ok := registry.Borrow(ctx, "late")
		if !ok {
			t.Fatalf("borrow reported no tool set")
		}
		if err := registry.Close(); err != nil {
			t.Fatalf("close beside an in-flight borrow = %v, want no error yet", err)
		}
		if got := toolset.closeCount(); got != 0 {
			t.Fatalf("close shut down a borrowed connection: %d", got)
		}
		release()
		if got := toolset.closeCount(); got != 1 {
			t.Fatalf("release after close = %d closes, want 1", got)
		}
		if err := registry.Close(); !errors.Is(err, lateErr) {
			t.Fatalf("later close = %v, want the late release error", err)
		}
	})

	t.Run("concurrent borrows close once", func(t *testing.T) {
		registry := NewServerRegistry()
		toolset := &fakeToolSet{name: "shared"}
		if err := registry.Add(ctx, "shared", toolset); err != nil {
			t.Fatalf("registration = %v", err)
		}
		const callers = 16
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, release, ok := registry.Borrow(ctx, "shared")
				if !ok {
					t.Errorf("concurrent borrow reported no tool set")
					return
				}
				release()
			}()
		}
		wg.Wait()
		if err := registry.Remove(ctx, "shared"); err != nil {
			t.Fatalf("removal after concurrent borrows = %v", err)
		}
		if got := toolset.closeCount(); got != 1 {
			t.Fatalf("close count after concurrent borrows = %d, want 1", got)
		}
	})

	t.Run("read-only registry is read through get", func(t *testing.T) {
		toolset := &fakeToolSet{name: "read-only"}
		registry := &getOnlyRegistry{entries: map[string]trpctool.ToolSet{"read-only": toolset}}
		held, release, ok := borrowServer(ctx, registry, "read-only")
		if !ok || held != toolset {
			t.Fatalf("borrow over a read-only registry = (held %v, ok %v)", held, ok)
		}
		release()
		release()
		if got := toolset.closeCount(); got != 0 {
			t.Fatalf("the read-only path closed the tool set: %d", got)
		}
		if _, _, ok := borrowServer(ctx, registry, "missing"); ok {
			t.Fatalf("borrow over an unregistered name reported a tool set")
		}
		if _, _, ok := borrowServer(ctx, nil, "read-only"); ok {
			t.Fatalf("borrow over no registry reported a tool set")
		}
	})
}

type fakeToolSet struct {
	name     string
	tools    []trpctool.Tool
	closes   atomic.Int32
	closeErr error
	initErr  error
}

func (f *fakeToolSet) Tools(context.Context) []trpctool.Tool { return f.tools }

func (f *fakeToolSet) Close() error {
	f.closes.Add(1)
	return f.closeErr
}

func (f *fakeToolSet) Name() string { return f.name }

func (f *fakeToolSet) Init(context.Context) error { return f.initErr }

func (f *fakeToolSet) closeCount() int { return int(f.closes.Load()) }

type fakeTool struct {
	decl   *trpctool.Declaration
	callFn func(ctx context.Context, args []byte) (any, error)
}

func (f *fakeTool) Declaration() *trpctool.Declaration { return f.decl }

func (f *fakeTool) Call(ctx context.Context, args []byte) (any, error) {
	if f.callFn == nil {
		return "called", nil
	}
	return f.callFn(ctx, args)
}

type declarationOnlyTool struct {
	decl *trpctool.Declaration
}

func (f *declarationOnlyTool) Declaration() *trpctool.Declaration { return f.decl }

type getOnlyRegistry struct {
	entries map[string]trpctool.ToolSet
}

func (r *getOnlyRegistry) Get(_ context.Context, name string) (trpctool.ToolSet, bool) {
	toolset, ok := r.entries[name]
	return toolset, ok
}

func (r *getOnlyRegistry) List(context.Context) []trpctool.ToolSet {
	sets := make([]trpctool.ToolSet, 0, len(r.entries))
	for _, toolset := range r.entries {
		sets = append(sets, toolset)
	}
	return sets
}

func (r *getOnlyRegistry) Names(context.Context) []string {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	return names
}

func (r *getOnlyRegistry) Add(context.Context, string, trpctool.ToolSet) error { return nil }

func (r *getOnlyRegistry) Remove(context.Context, string) error { return nil }

var (
	_ Registry         = (*ServerRegistry)(nil)
	_ Registry         = (*getOnlyRegistry)(nil)
	_ trpctool.ToolSet = (*fakeToolSet)(nil)
)

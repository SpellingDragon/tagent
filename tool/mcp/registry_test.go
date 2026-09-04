package mcp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// countingToolSet is a manual mock toolset recording Close calls.
type countingToolSet struct {
	name   string
	mu     sync.Mutex
	closed int
	tools  []trpctool.Tool
}

func (m *countingToolSet) Tools(_ context.Context) []trpctool.Tool { return m.tools }
func (m *countingToolSet) Name() string                            { return m.name }
func (m *countingToolSet) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	return nil
}

func (m *countingToolSet) closeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// ==================== Transport normalization ====================

func TestNormalizeTransport(t *testing.T) {
	cases := map[string]string{
		"streamable-http": "streamable",
		"streamable_http": "streamable",
		"streamableHttp":  "streamable",
		"http":            "streamable",
		"STREAMABLE-HTTP": "streamable",
		"streamable":      "streamable",
		"sse":             "sse",
		"stdio":           "stdio",
		" sse ":           "sse",
		"websocket":       "websocket", // passed through, rejected by Validate
	}
	for in, want := range cases {
		assert.Equal(t, want, NormalizeTransport(in), "input %q", in)
	}
}

func TestServerConfigValidate(t *testing.T) {
	require.NoError(t, ServerConfig{Transport: "streamable-http", URL: "https://x/mcp"}.Validate("a"))
	require.NoError(t, ServerConfig{Transport: "stdio", Command: "bin"}.Validate("a"))

	err := ServerConfig{Transport: "sse"}.Validate("no-url")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-url")
	assert.Contains(t, err.Error(), "url")

	err = ServerConfig{Transport: "stdio"}.Validate("no-cmd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command")

	err = ServerConfig{Transport: "websocket", URL: "https://x"}.Validate("bad-transport")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported transport")
}

// ==================== Registry lifecycle ====================

func TestRegistry_AddRemoveClose(t *testing.T) {
	r := NewRegistry()
	a := &countingToolSet{name: "a"}
	b := &countingToolSet{name: "b"}

	r.Add("b", b)
	r.Add("a", a)

	assert.Equal(t, []string{"a", "b"}, r.Names(), "Names must be sorted")
	list := r.List()
	require.Len(t, list, 2)
	assert.Equal(t, "a", list[0].Name(), "List must be sorted by name")

	got, ok := r.Get("a")
	require.True(t, ok)
	assert.Same(t, a, got.(*countingToolSet))

	// Remove closes the toolset.
	require.True(t, r.Remove("a"))
	assert.Equal(t, 1, a.closeCount())
	_, ok = r.Get("a")
	assert.False(t, ok)
	assert.False(t, r.Remove("a"), "second remove returns false")

	// Add-replace closes the old instance.
	b2 := &countingToolSet{name: "b"}
	r.Add("b", b2)
	assert.Equal(t, 1, b.closeCount())

	// Close closes everything and is idempotent.
	require.NoError(t, r.Close())
	assert.Equal(t, 1, b2.closeCount())
	require.NoError(t, r.Close())
	assert.Equal(t, 1, b2.closeCount(), "Close must be idempotent")
	assert.Empty(t, r.Names())
}

// ==================== Config hot-sync ====================

func writeConfig(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestRegistry_HotSync(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tagent.yaml")
	base := time.Now().Add(-time.Hour)

	writeConfig(t, cfgPath, `
mcp_servers:
  alpha:
    transport: streamable-http
    url: https://example.com/alpha
`, base)

	r := NewRegistry(WithConfigPath(cfgPath))
	// First access lazily syncs the file (no Seed needed).
	assert.Equal(t, []string{"alpha"}, r.Names())
	p1, ok := r.Get("alpha")
	require.True(t, ok)

	// Same content, newer mtime → instance retained (spec unchanged).
	writeConfig(t, cfgPath, `
mcp_servers:
  alpha:
    transport: streamable-http
    url: https://example.com/alpha
`, base.Add(2*time.Second))
	p2, ok := r.Get("alpha")
	require.True(t, ok)
	assert.Same(t, p1, p2, "unchanged spec must keep the live instance")

	// Add beta + change alpha URL → beta added, alpha rebuilt.
	writeConfig(t, cfgPath, `
mcp_servers:
  alpha:
    transport: streamable-http
    url: https://example.com/alpha-v2
  beta:
    transport: sse
    url: https://example.com/beta
`, base.Add(4*time.Second))
	assert.Equal(t, []string{"alpha", "beta"}, r.Names())
	p3, ok := r.Get("alpha")
	require.True(t, ok)
	assert.NotSame(t, p1, p3, "changed spec must rebuild the toolset")

	// Remove alpha → gone, beta stays.
	writeConfig(t, cfgPath, `
mcp_servers:
  beta:
    transport: sse
    url: https://example.com/beta
`, base.Add(6*time.Second))
	assert.Equal(t, []string{"beta"}, r.Names())

	// Broken YAML → keep current content.
	writeConfig(t, cfgPath, "mcp_servers: [broken", base.Add(8*time.Second))
	assert.Equal(t, []string{"beta"}, r.Names(), "parse failure must keep current servers")

	// Invalid spec → skipped, valid ones applied.
	writeConfig(t, cfgPath, `
mcp_servers:
  beta:
    transport: sse
    url: https://example.com/beta
  broken:
    transport: sse
`, base.Add(10*time.Second))
	assert.Equal(t, []string{"beta"}, r.Names(), "invalid server must be skipped")

	require.NoError(t, r.Close())
}

func TestRegistry_HotSync_ManualEntriesSurvive(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tagent.yaml")
	base := time.Now().Add(-time.Hour)
	writeConfig(t, cfgPath, "mcp_servers: {}\n", base)

	r := NewRegistry(WithConfigPath(cfgPath))
	manual := &countingToolSet{name: "manual"}
	r.Add("manual", manual)

	// Config change without the manual entry → manual survives.
	writeConfig(t, cfgPath, `
mcp_servers:
  alpha:
    transport: sse
    url: https://example.com/alpha
`, base.Add(2*time.Second))
	assert.Equal(t, []string{"alpha", "manual"}, r.Names())
	assert.Equal(t, 0, manual.closeCount())

	require.NoError(t, r.Close())
	assert.Equal(t, 1, manual.closeCount())
}

func TestRegistry_Seed_BaselinesMtime(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tagent.yaml")
	writeConfig(t, cfgPath, `
mcp_servers:
  alpha:
    transport: sse
    url: https://example.com/alpha
`, time.Now())

	r := NewRegistry(WithConfigPath(cfgPath))
	r.Seed(map[string]ServerConfig{
		"alpha": {Transport: "sse", URL: "https://example.com/alpha"},
	})
	p1, ok := r.Get("alpha")
	require.True(t, ok)
	// Access right after Seed must not rebuild from the just-seeded file.
	p2, _ := r.Get("alpha")
	assert.Same(t, p1, p2)
	require.NoError(t, r.Close())
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.Add("s", &countingToolSet{name: "s"})
				r.Get("s")
				r.Names()
				r.List()
				r.Remove("s")
			}
		}(i)
	}
	wg.Wait()
	require.NoError(t, r.Close())
}

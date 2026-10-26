package capabilities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
	trpcmcp "trpc.group/trpc-go/trpc-agent-go/tool/mcp"
	tmcp "trpc.group/trpc-go/trpc-mcp-go"
)

// TestMCPProtocolLifecycle drives the MCP connection, discovery and gateway lifecycle end to end over a fake server:
// - the wire protocol runs between the locked framework tool set and an in-process fake MCP server
// - the connector brings one native tool set up and runtime registration is visible without a rebuild
// - the discovery guidance is executable in the same generation and reaches the same tool set
// - registry add and remove change no gateway declaration and no request tool prefix bytes
// - an unknown server name answers with the registered server list
// - an empty registry answers an explicit empty state naming both registration sources
// - an unknown tool name answers with that server's tool list
// - an inner call failure names the server and tool and carries the target input schema
// - one broken server does not block the others and the refresh failure is named
// - an in-flight call keeps its connection across a removal and the shutdown releases everything exactly once
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestMCPProtocolLifecycle(t *testing.T) {
	t.Run("the connector brings one native tool set up and runtime registration is visible without a rebuild", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")

		toolset := connectOne(ctx, t, alpha)
		if toolset.Name() != "alpha" {
			t.Fatalf("tool set name = %q, want the server identity the declaration pinned", toolset.Name())
		}
		if listed := toolset.Tools(ctx); len(listed) != 1 {
			t.Fatalf("listed tools = %d, want the one tool the fake server registers", len(listed))
		}

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		if err := registry.Add(ctx, "alpha", &countingToolSet{ToolSet: toolset}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		if err := registry.Add(ctx, "alpha", &countingToolSet{ToolSet: toolset}); !errors.Is(err, mcpgw.ErrServerExists) {
			t.Fatalf("duplicate registration error = %v, want the named duplicate failure", err)
		}
		beta := newMCPFake(t, "beta")
		beta.registerEcho(t, "echo", "echo the text back")
		betaSet := connectOne(ctx, t, beta)
		if err := registry.Add(ctx, "beta", &countingToolSet{ToolSet: betaSet}); err != nil {
			t.Fatalf("register beta at runtime: %v", err)
		}
		names := registry.Names(ctx)
		if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
			t.Fatalf("registered names = %v, want both servers in ascending order without a rebuild", names)
		}
		if listed := registry.List(ctx); len(listed) != 2 {
			t.Fatalf("listed tool sets = %d, want the runtime registration immediately visible", len(listed))
		}
		if _, ok := registry.Get(ctx, "gamma"); ok {
			t.Fatalf("lookup of an unregistered name reported present")
		}
	})

	t.Run("the discovery guidance is executable in the same generation and reaches the same tool set", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")
		alpha.registerEcho(t, "web_search_prime", "prime web search of the fake server")

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		if err := registry.Add(ctx, "alpha", &countingToolSet{ToolSet: connectOne(ctx, t, alpha)}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		gateways := mcpgw.NewGateways(registry, mcpgw.NewRegistryDiscovery())
		discover, call := gateways.DiscoverTool(), gateways.CallTool()

		rawDiscover := invoke(t, ctx, discover, `{"query":"echo"}`)
		text, ok := rawDiscover.(string)
		if !ok || !strings.Contains(text, "mcp_call(server=") {
			t.Fatalf("discovery result = %#v, want content carrying the mcp_call guidance", rawDiscover)
		}
		found := discoverResultOf(t, rawDiscover)
		if found.Count != 1 || len(found.Tools) != 1 {
			t.Fatalf("discovery result = %+v, want the single echo tool", found)
		}
		entry := found.Tools[0]
		if entry.Server != "alpha" || entry.Tool != "echo" || entry.Source != "mcp:alpha" {
			t.Fatalf("discovered entry = %+v, want the server identity of the tool set it came from", entry)
		}
		if entry.InputSchema == nil || entry.InputSchema["properties"] == nil {
			t.Fatalf("discovered entry = %+v, want the target tool input schema carried along", entry)
		}

		byTokens := discoverResultOf(t, invoke(t, ctx, discover, `{"query":"web search"}`))
		if byTokens.Count != 1 || byTokens.Tools[0].Tool != "web_search_prime" {
			t.Fatalf("token query result = %+v, want web_search_prime matched by its tokens", byTokens)
		}

		match := regexp.MustCompile(`mcp_call\(server="([^"]+)", tool="([^"]+)"`).FindStringSubmatch(entry.Call)
		if match == nil {
			t.Fatalf("guidance %q does not name the server and the tool", entry.Call)
		}
		rawCall := invoke(t, ctx, call, fmt.Sprintf(`{"server":%q,"tool":%q,"args":{"text":"hi"}}`, match[1], match[2]))
		payload, err := json.Marshal(rawCall)
		if err != nil {
			t.Fatalf("marshal the guided call result: %v", err)
		}
		if !strings.Contains(string(payload), "echo:hi") {
			t.Fatalf("guided call answered %s, want the fake tool result of the same tool set", payload)
		}
		if got := alpha.callCount("echo"); got != 1 {
			t.Fatalf("fake echo executions = %d, want the guided call to land exactly once", got)
		}
	})

	t.Run("registry add and remove change no gateway declaration and no request tool prefix bytes", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptFile(t, root, filepath.Join("prompts", "system.md"), "system prompt\n")
		writeSkillFolder(t, root, "greet", "body v1\n")

		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")
		gamma := newMCPFake(t, "gamma")
		gamma.registerEcho(t, "echo", "echo the text back")

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		betaSet := &countingToolSet{ToolSet: connectOne(ctx, t, alpha)}
		if err := registry.Add(ctx, "beta", betaSet); err != nil {
			t.Fatalf("register beta: %v", err)
		}
		gammaSet := &countingToolSet{ToolSet: connectOne(ctx, t, gamma)}

		gateways := mcpgw.NewGateways(registry, mcpgw.NewRegistryDiscovery())
		callBefore := declarationBytes(t, gateways.CallTool().Declaration())
		discoverBefore := declarationBytes(t, gateways.DiscoverTool().Declaration())
		reloadedBefore := gateways.ReloadDeclarations(ctx)
		if len(reloadedBefore) != 2 || reloadedBefore[0].Name != mcpgw.ToolNameMCPCall || reloadedBefore[1].Name != mcpgw.ToolNameMCPDiscover {
			t.Fatalf("reloaded declarations = %+v, want the two gateways in request order", reloadedBefore)
		}

		mutate := function.NewFunctionTool(
			func(ctx context.Context, in ntFnInput) (string, error) {
				if err := registry.Add(ctx, "gamma", gammaSet); err != nil {
					return "", err
				}
				if err := registry.Remove(ctx, "beta"); err != nil {
					return "", err
				}
				return "mutated:" + in.Text, nil
			},
			function.WithName("mutate_registry"),
			function.WithDescription("change the registry content behind the request"),
		)

		var mu sync.Mutex
		round := 0
		prefixRound1 := ""
		provider := &ntModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			mu.Lock()
			round++
			mu.Unlock()
			switch round {
			case 1:
				prefixRound1 = toolsPrefixBytes(t, req.Tools)
				return ntToolTurn(ntCall("call-mutate", "mutate_registry", `{"text":"go"}`))
			case 2:
				if req.Tools[mcpgw.ToolNameMCPCall] == nil || req.Tools[mcpgw.ToolNameMCPDiscover] == nil {
					t.Errorf("second request tools carry no gateway, want the fixed gateways still declared")
					return ntFinalTurn("settled")
				}
				if got := toolsPrefixBytes(t, req.Tools); got != prefixRound1 {
					t.Errorf("request tool prefix bytes changed across the registry mutation, want byte-identical declarations")
				}
				if got := declarationBytes(t, req.Tools[mcpgw.ToolNameMCPCall].Declaration()); got != callBefore {
					t.Errorf("mcp_call declaration = %s, want the fixed declaration of the first round", got)
				}
				if got := declarationBytes(t, req.Tools[mcpgw.ToolNameMCPDiscover].Declaration()); got != discoverBefore {
					t.Errorf("mcp_discover declaration = %s, want the fixed declaration of the first round", got)
				}
				return ntFinalTurn("settled")
			default:
				return ntFinalTurn("unexpected round")
			}
		}}

		run, err := tagent.Assemble(ctx, declarationOf(root), map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{gateways.CallTool(), gateways.DiscoverTool(), mutate}},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-mcp-declaration",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-mcp-declaration"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "hello"}},
		})
		if err != nil {
			t.Fatalf("invoke the runtime: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}

		if got := declarationBytes(t, gateways.CallTool().Declaration()); got != callBefore {
			t.Fatalf("mcp_call declaration = %s, want the fixed declaration across the mutation", got)
		}
		if got := declarationBytes(t, gateways.DiscoverTool().Declaration()); got != discoverBefore {
			t.Fatalf("mcp_discover declaration = %s, want the fixed declaration across the mutation", got)
		}
		reloadedAfter := gateways.ReloadDeclarations(ctx)
		for i, decl := range reloadedAfter {
			if got := declarationBytes(t, decl); got != declarationBytes(t, reloadedBefore[i]) {
				t.Fatalf("reloaded declaration %d changed, want the fixed values re-issued", i)
			}
		}
		names := registry.Names(ctx)
		if len(names) != 1 || names[0] != "gamma" {
			t.Fatalf("registry names = %v, want gamma in and beta out after the runtime mutation", names)
		}
		if got := betaSet.closeCount(); got != 1 {
			t.Fatalf("removed beta closes = %d, want the removed connection released exactly once", got)
		}
	})

	t.Run("an unknown server name answers with the registered server list", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		for _, name := range []string{"alpha", "beta"} {
			fake := newMCPFake(t, name)
			fake.registerEcho(t, "echo", "echo the text back")
			if err := registry.Add(ctx, name, &countingToolSet{ToolSet: connectOne(ctx, t, fake)}); err != nil {
				t.Fatalf("register %s: %v", name, err)
			}
		}
		call := mcpgw.NewCallTool(registry)
		failure := callFailureOf(t, invoke(t, ctx, call, `{"server":"not-exist","tool":"echo","args":{}}`))
		if !strings.Contains(failure.Error, "not-exist") {
			t.Fatalf("failure error = %q, want the unknown name echoed back", failure.Error)
		}
		if len(failure.Servers) != 2 || failure.Servers[0] != "alpha" || failure.Servers[1] != "beta" {
			t.Fatalf("failure servers = %v, want the current registered server list", failure.Servers)
		}
	})

	t.Run("an empty registry answers an explicit empty state naming both registration sources", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		gateways := mcpgw.NewGateways(registry, mcpgw.NewRegistryDiscovery())

		failure := callFailureOf(t, invoke(t, ctx, gateways.CallTool(), `{"server":"alpha","tool":"echo","args":{}}`))
		if !strings.Contains(failure.Error, "no MCP server is registered") {
			t.Fatalf("empty-state error = %q, want the explicit empty state", failure.Error)
		}
		if !strings.Contains(failure.Hint, "configuration file") || !strings.Contains(failure.Hint, "runtime") {
			t.Fatalf("empty-state hint = %q, want both registration sources named", failure.Hint)
		}

		typedNil := mcpgw.NewCallTool((*mcpgw.ServerRegistry)(nil))
		failure = callFailureOf(t, invoke(t, ctx, typedNil, `{"server":"alpha","tool":"echo","args":{}}`))
		if !strings.Contains(failure.Error, "no MCP server is registered") {
			t.Fatalf("typed-nil registry error = %q, want the empty state instead of a crash", failure.Error)
		}

		discovered := discoverResultOf(t, invoke(t, ctx, gateways.DiscoverTool(), `{}`))
		if discovered.Count != 0 || len(discovered.Tools) != 0 || discovered.Error != "" {
			t.Fatalf("empty discovery = %+v, want tools=[] and count=0 without an error", discovered)
		}
	})

	t.Run("an unknown tool name answers with that server's tool list", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")
		alpha.registerEcho(t, "sum", "add two numbers")

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		if err := registry.Add(ctx, "alpha", &countingToolSet{ToolSet: connectOne(ctx, t, alpha)}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		failure := callFailureOf(t, invoke(t, ctx, mcpgw.NewCallTool(registry), `{"server":"alpha","tool":"nope","args":{}}`))
		if !strings.Contains(failure.Error, "nope") || failure.Server != "alpha" {
			t.Fatalf("failure = %+v, want the unknown tool named beside its server", failure)
		}
		tools := append([]string(nil), failure.Tools...)
		sort.Strings(tools)
		if len(tools) != 2 || tools[0] != "echo" || tools[1] != "sum" {
			t.Fatalf("failure tools = %v, want the live tool list of that server", tools)
		}
	})

	t.Run("an inner call failure names the server and tool and carries the target input schema", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		if err := registry.Add(ctx, "alpha", &countingToolSet{ToolSet: connectOne(ctx, t, alpha)}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		call := mcpgw.NewCallTool(registry)

		alpha.kill()
		failure := callFailureOf(t, invoke(t, ctx, call, `{"server":"alpha","tool":"echo","args":{"text":"hi"}}`))
		if !strings.Contains(failure.Error, "alpha") || !strings.Contains(failure.Error, "echo") || !strings.Contains(failure.Error, "failed") {
			t.Fatalf("inner failure error = %q, want the server and the tool named", failure.Error)
		}
		properties, _ := failure.InputSchema["properties"].(map[string]any)
		if properties["text"] == nil {
			t.Fatalf("inner failure schema = %+v, want the target tool input schema for the retry", failure.InputSchema)
		}

		dead := trpcmcp.NewMCPToolSet(
			trpcmcp.ConnectionConfig{Transport: "streamable", ServerURL: "http://127.0.0.1:1/mcp"},
			trpcmcp.WithName("beta"),
		)
		if err := registry.Add(ctx, "beta", &countingToolSet{ToolSet: dead}); err != nil {
			t.Fatalf("register the dead server: %v", err)
		}
		failure = callFailureOf(t, invoke(t, ctx, call, `{"server":"beta","tool":"echo","args":{}}`))
		if !strings.Contains(failure.Error, "MCP dependency failure") {
			t.Fatalf("empty enumeration error = %q, want the named MCP dependency failure", failure.Error)
		}
	})

	t.Run("one broken server does not block the others and the refresh failure is named", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")
		beta := newMCPFake(t, "beta")
		beta.registerEcho(t, "echo", "echo the text back")

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		if err := registry.Add(ctx, "alpha", &countingToolSet{ToolSet: connectOne(ctx, t, alpha)}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		if err := registry.Add(ctx, "beta", &countingToolSet{ToolSet: connectOne(ctx, t, beta)}); err != nil {
			t.Fatalf("register beta: %v", err)
		}
		beta.kill()

		discovered := discoverResultOf(t, invoke(t, ctx, mcpgw.NewDiscoverTool(registry, mcpgw.NewRegistryDiscovery()), `{}`))
		if discovered.Count != 1 || len(discovered.Tools) != 1 || discovered.Tools[0].Server != "alpha" {
			t.Fatalf("discovery = %+v, want the healthy server's tools only", discovered)
		}
		if len(discovered.Failures) != 1 || discovered.Failures[0].Server != "beta" {
			t.Fatalf("discovery failures = %+v, want the broken server named", discovered.Failures)
		}
		if !strings.Contains(discovered.Failures[0].Error, "refresh") {
			t.Fatalf("discovery failure error = %q, want the named refresh failure", discovered.Failures[0].Error)
		}
	})

	t.Run("an in-flight call keeps its connection across a removal and closes exactly once", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		alpha := newMCPFake(t, "alpha")
		entered := make(chan struct{})
		release := make(chan struct{})
		alpha.registerBlocking(t, "block", entered, release)

		registry := mcpgw.NewServerRegistry()
		defer registry.Close()
		counting := &countingToolSet{ToolSet: connectOne(ctx, t, alpha)}
		if err := registry.Add(ctx, "alpha", counting); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		call := mcpgw.NewCallTool(registry)

		type outcome struct {
			result any
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			result, err := call.Call(ctx, []byte(`{"server":"alpha","tool":"block","args":{}}`))
			done <- outcome{result: result, err: err}
		}()

		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatalf("the blocking call never reached the fake server")
		}
		if err := registry.Remove(ctx, "alpha"); err != nil {
			t.Fatalf("remove the server under the in-flight call: %v", err)
		}
		if _, ok := registry.Get(ctx, "alpha"); ok {
			t.Fatalf("removed server still reported present")
		}
		if got := counting.closeCount(); got != 0 {
			t.Fatalf("tool set closes = %d during the in-flight call, want the connection kept alive", got)
		}

		close(release)
		select {
		case out := <-done:
			if out.err != nil {
				t.Fatalf("in-flight call errored: %v", out.err)
			}
			payload, err := json.Marshal(out.result)
			if err != nil {
				t.Fatalf("marshal the in-flight result: %v", err)
			}
			if !strings.Contains(string(payload), "released") {
				t.Fatalf("in-flight call answered %s, want its real result after the removal", payload)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("the in-flight call never returned after its release")
		}
		if got := counting.closeCount(); got != 1 {
			t.Fatalf("tool set closes = %d after the call ended, want exactly one release", got)
		}
		if err := registry.Remove(ctx, "alpha"); !errors.Is(err, mcpgw.ErrServerUnknown) {
			t.Fatalf("repeated removal error = %v, want the named unknown-server failure", err)
		}
		if got := counting.closeCount(); got != 1 {
			t.Fatalf("tool set closes = %d after the repeated removal, want no second release", got)
		}
	})

	t.Run("shutdown releases every connection exactly once and repeats nothing", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		registry := mcpgw.NewServerRegistry()
		countings := map[string]*countingToolSet{}
		for _, name := range []string{"alpha", "beta"} {
			fake := newMCPFake(t, name)
			fake.registerEcho(t, "echo", "echo the text back")
			counting := &countingToolSet{ToolSet: connectOne(ctx, t, fake)}
			countings[name] = counting
			if err := registry.Add(ctx, name, counting); err != nil {
				t.Fatalf("register %s: %v", name, err)
			}
		}
		if err := registry.Close(); err != nil {
			t.Fatalf("shut the registry down: %v", err)
		}
		for name, counting := range countings {
			if got := counting.closeCount(); got != 1 {
				t.Fatalf("%s closes = %d after the shutdown, want exactly one release", name, got)
			}
		}
		if err := registry.Close(); err != nil {
			t.Fatalf("shut the registry down again: %v", err)
		}
		for name, counting := range countings {
			if got := counting.closeCount(); got != 1 {
				t.Fatalf("%s closes = %d after the repeated shutdown, want no second release", name, got)
			}
		}
		if err := registry.Add(ctx, "gamma", countings["alpha"]); !errors.Is(err, mcpgw.ErrRegistryClosed) {
			t.Fatalf("registration after shutdown error = %v, want the named shut-down failure", err)
		}
		if err := registry.Remove(ctx, "alpha"); !errors.Is(err, mcpgw.ErrServerUnknown) {
			t.Fatalf("removal after shutdown error = %v, want the named unknown-server failure", err)
		}
	})
}

// mcpFake is one in-process MCP server: the locked protocol implementation
// behind httptest, so both ends of every call in these sub-cases run inside
// the locked dependency stack and nothing reaches a real external service.
type mcpFake struct {
	name string
	srv  *tmcp.Server
	ts   *httptest.Server
	url  string

	mu     sync.Mutex
	calls  map[string]int
	closed bool
}

// newMCPFake starts one in-process MCP server and stops it with the test.
func newMCPFake(t *testing.T, name string) *mcpFake {
	t.Helper()
	srv := tmcp.NewServer(name, "0.0.1-test")
	ts := httptest.NewServer(srv.Handler())
	fake := &mcpFake{name: name, srv: srv, ts: ts, url: ts.URL + srv.Path(), calls: map[string]int{}}
	t.Cleanup(fake.kill)
	return fake
}

// kill stops the listener behind the registered tool sets: the broken
// connection state the failure sub-cases must name.
func (f *mcpFake) kill() {
	f.mu.Lock()
	already := f.closed
	f.closed = true
	f.mu.Unlock()
	if already {
		return
	}
	f.ts.Close()
}

// registerEcho registers one text tool answering "<name>:<text>" under a call
// counter, so a guided call can be shown to land on exactly one tool set.
func (f *mcpFake) registerEcho(t *testing.T, name, description string) {
	t.Helper()
	f.srv.RegisterTool(
		tmcp.NewTool(name, tmcp.WithDescription(description), tmcp.WithString("text")),
		func(_ context.Context, req *tmcp.CallToolRequest) (*tmcp.CallToolResult, error) {
			f.mu.Lock()
			f.calls[name]++
			f.mu.Unlock()
			text, _ := req.Params.Arguments["text"].(string)
			return tmcp.NewTextResult(name + ":" + text), nil
		},
	)
}

// registerBlocking registers one tool whose handler blocks until the test
// releases it, so a call can be held in flight while the registry changes.
func (f *mcpFake) registerBlocking(t *testing.T, name string, entered chan<- struct{}, release <-chan struct{}) {
	t.Helper()
	f.srv.RegisterTool(
		tmcp.NewTool(name, tmcp.WithDescription("blocks until the test releases it")),
		func(_ context.Context, _ *tmcp.CallToolRequest) (*tmcp.CallToolResult, error) {
			entered <- struct{}{}
			<-release
			return tmcp.NewTextResult("released"), nil
		},
	)
}

// callCount reports how often one fake tool was executed.
func (f *mcpFake) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

// connectOne connects the native tool set of one fake server through the
// connector: the transport spelling the configuration layer uses, folded onto
// the spelling the locked framework knows, and the server identity pinned by
// the declaration.
func connectOne(ctx context.Context, t *testing.T, fake *mcpFake) *trpcmcp.ToolSet {
	t.Helper()
	toolset, err := mcpgw.NewNativeConnector().Connect(
		ctx,
		trpcmcp.ConnectionConfig{Transport: "streamable-http", ServerURL: fake.url},
		trpcmcp.WithName(fake.name),
		trpcmcp.WithMCPOptions(tmcp.WithClientGetSSEEnabled(false)),
	)
	if err != nil {
		t.Fatalf("connect to the fake server %q: %v", fake.name, err)
	}
	return toolset
}

// countingToolSet counts the Close calls one registered tool set receives, so
// the exactly-once release contract is observable from the outside.
type countingToolSet struct {
	trpctool.ToolSet
	mu     sync.Mutex
	closes int
}

// Close counts the release and forwards it to the wrapped tool set.
func (c *countingToolSet) Close() error {
	c.mu.Lock()
	c.closes++
	c.mu.Unlock()
	return c.ToolSet.Close()
}

// closeCount reports how often the wrapped tool set was released.
func (c *countingToolSet) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closes
}

// Init forwards the native re-list face when the wrapped set carries one, so a
// counting wrapper refreshes exactly the way the real tool sets do.
func (c *countingToolSet) Init(ctx context.Context) error {
	if relist, ok := c.ToolSet.(interface{ Init(context.Context) error }); ok {
		return relist.Init(ctx)
	}
	return nil
}

// mcpCallResult is the parsed failure payload of the call gateway.
type mcpCallResult struct {
	OK          bool           `json:"ok"`
	Server      string         `json:"server"`
	Tool        string         `json:"tool"`
	Error       string         `json:"error"`
	Servers     []string       `json:"servers"`
	Tools       []string       `json:"tools"`
	InputSchema map[string]any `json:"input_schema"`
	Hint        string         `json:"hint"`
}

// mcpDiscoverResult is the parsed payload of the discovery gateway.
type mcpDiscoverResult struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	Tools []struct {
		Server      string         `json:"server"`
		Tool        string         `json:"tool"`
		Description string         `json:"description"`
		Source      string         `json:"source"`
		InputSchema map[string]any `json:"input_schema"`
		Call        string         `json:"call"`
	} `json:"tools"`
	Failures []struct {
		Server string `json:"server"`
		Error  string `json:"error"`
	} `json:"failures"`
	Error string `json:"error"`
}

// invoke drives one gateway tool with raw JSON arguments and answers its
// result.
func invoke(t *testing.T, ctx context.Context, tool trpctool.CallableTool, args string) any {
	t.Helper()
	result, err := tool.Call(ctx, []byte(args))
	if err != nil {
		t.Fatalf("call %s: %v", tool.Declaration().Name, err)
	}
	return result
}

// callFailureOf parses one gateway failure result and checks it stayed a
// result — named, self-correcting, and not a Go error.
func callFailureOf(t *testing.T, result any) mcpCallResult {
	t.Helper()
	text, ok := result.(string)
	if !ok {
		t.Fatalf("gateway result = %#v, want the JSON text of a named failure", result)
	}
	var parsed mcpCallResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("gateway result %q does not parse: %v", text, err)
	}
	if parsed.OK {
		t.Fatalf("gateway result %q reports ok, want a named failure", text)
	}
	return parsed
}

// discoverResultOf parses one discovery result.
func discoverResultOf(t *testing.T, result any) mcpDiscoverResult {
	t.Helper()
	text, ok := result.(string)
	if !ok {
		t.Fatalf("discovery result = %#v, want the JSON text of a discovery read", result)
	}
	var parsed mcpDiscoverResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("discovery result %q does not parse: %v", text, err)
	}
	return parsed
}

// declarationBytes renders one declaration into stable bytes.
func declarationBytes(t *testing.T, decl *trpctool.Declaration) string {
	t.Helper()
	raw, err := json.Marshal(decl)
	if err != nil {
		t.Errorf("marshal the declaration: %v", err)
		return ""
	}
	return string(raw)
}

// toolsPrefixBytes renders the declared tool map of one request into stable
// bytes — sorted names, each beside the JSON of its declaration. The prefix
// the model sees must not change when the registry content does.
func toolsPrefixBytes(t *testing.T, tools map[string]trpctool.Tool) string {
	t.Helper()
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		raw, err := json.Marshal(tools[name].Declaration())
		if err != nil {
			t.Errorf("marshal the declaration of %q: %v", name, err)
			continue
		}
		sb.WriteString(name)
		sb.WriteByte('\n')
		sb.Write(raw)
		sb.WriteByte('\n')
	}
	return sb.String()
}

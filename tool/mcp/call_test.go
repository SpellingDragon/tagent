package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestCallGatewayAnswersFailuresAsNamedResults pins the call gateway: every failure is a result naming what the model must correct, never a Go error.
// - an argument object missing the server or tool key answers with the schema and the registered server names
// - an argument object that does not parse answers with the schema and no Go error
// - a registry with no servers answers the empty-table hint without an error
// - a typed nil registry is folded onto no registry instead of a live table
// - an unknown server echoes the registered names
// - a server listing no tools answers as a dependency failure instead of an empty success
// - an unknown tool echoes that server's tool names
// - a tool without a call face answers as a dependency failure beside the tool names
// - an inner call failure carries the server, the tool, the cause and the target input schema
// - a successful call returns the native result untouched
// - the declaration is the same value whatever the registry holds
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestCallGatewayAnswersFailuresAsNamedResults(t *testing.T) {
	ctx := context.Background()

	alphaRegistry := func(t *testing.T, tools ...trpctool.Tool) *ServerRegistry {
		t.Helper()
		registry := NewServerRegistry()
		if err := registry.Add(ctx, "alpha", &fakeToolSet{name: "alpha", tools: tools}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		return registry
	}
	namedTool := func(name string) *fakeTool {
		return &fakeTool{decl: &trpctool.Declaration{
			Name:        name,
			Description: "a tool named " + name,
			InputSchema: &trpctool.Schema{Type: "object"},
		}}
	}

	t.Run("missing server or tool argument", func(t *testing.T) {
		tool := NewCallTool(alphaRegistry(t, namedTool("t1")))
		for _, args := range []string{`{"server":"alpha"}`, `{"tool":"t1"}`, `{}`} {
			result, err := tool.Call(ctx, []byte(args))
			refusal := decodeCallResult(t, result, err)
			if !strings.Contains(refusal.Error, MCPCallArgServer) || !strings.Contains(refusal.Error, MCPCallArgTool) {
				t.Fatalf("Call(%s) error = %q, want it to name both arguments", args, refusal.Error)
			}
			if refusal.InputSchema == nil {
				t.Fatalf("Call(%s) carries no input schema", args)
			}
			if !reflect.DeepEqual(refusal.Servers, []string{"alpha"}) {
				t.Fatalf("Call(%s) servers = %v, want [alpha]", args, refusal.Servers)
			}
		}
	})

	t.Run("unparsable argument object", func(t *testing.T) {
		tool := NewCallTool(alphaRegistry(t))
		answer, callErr := tool.Call(ctx, []byte(`{"server":`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, "do not parse") {
			t.Fatalf("error = %q, want the parse failure", refusal.Error)
		}
		if refusal.InputSchema == nil {
			t.Fatalf("parse refusal carries no input schema")
		}
	})

	t.Run("empty registry", func(t *testing.T) {
		tool := NewCallTool(NewServerRegistry())
		answer, callErr := tool.Call(ctx, []byte(`{"server":"alpha","tool":"t1"}`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, "no MCP server is registered") {
			t.Fatalf("error = %q, want the empty-table statement", refusal.Error)
		}
		if refusal.Hint == "" {
			t.Fatalf("empty-table refusal carries no hint: %+v", refusal)
		}
		if refusal.InputSchema != nil {
			t.Fatalf("empty-table refusal carries an input schema for no tool: %+v", refusal.InputSchema)
		}
	})

	t.Run("typed nil registry reads as no registry", func(t *testing.T) {
		var typedNil *ServerRegistry
		tool := NewCallTool(typedNil)
		answer, callErr := tool.Call(ctx, []byte(`{"server":"alpha","tool":"t1"}`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, "no MCP server is registered") {
			t.Fatalf("typed nil registry answered %q, want the empty-table statement", refusal.Error)
		}
	})

	t.Run("unknown server", func(t *testing.T) {
		tool := NewCallTool(alphaRegistry(t, namedTool("t1")))
		answer, callErr := tool.Call(ctx, []byte(`{"server":"beta","tool":"t1"}`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, `"beta"`) {
			t.Fatalf("error = %q, want the unknown server named", refusal.Error)
		}
		if !reflect.DeepEqual(refusal.Servers, []string{"alpha"}) {
			t.Fatalf("servers = %v, want the registered [alpha]", refusal.Servers)
		}
	})

	t.Run("server listing no tools", func(t *testing.T) {
		registry := NewServerRegistry()
		if err := registry.Add(ctx, "empty", &fakeToolSet{name: "empty"}); err != nil {
			t.Fatalf("register empty: %v", err)
		}
		answer, callErr := NewCallTool(registry).Call(ctx, []byte(`{"server":"empty","tool":"t1"}`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, "listed no tools") {
			t.Fatalf("error = %q, want the dependency failure", refusal.Error)
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		tool := NewCallTool(alphaRegistry(t, namedTool("t1")))
		answer, callErr := tool.Call(ctx, []byte(`{"server":"alpha","tool":"t2"}`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, `"t2"`) {
			t.Fatalf("error = %q, want the unknown tool named", refusal.Error)
		}
		if !reflect.DeepEqual(refusal.Tools, []string{"t1"}) {
			t.Fatalf("tools = %v, want the listed [t1]", refusal.Tools)
		}
	})

	t.Run("tool without a call face", func(t *testing.T) {
		tool := NewCallTool(alphaRegistry(t, &declarationOnlyTool{decl: &trpctool.Declaration{Name: "decl-only"}}))
		answer, callErr := tool.Call(ctx, []byte(`{"server":"alpha","tool":"decl-only"}`))
		refusal := decodeCallResult(t, answer, callErr)
		if !strings.Contains(refusal.Error, "without a call face") {
			t.Fatalf("error = %q, want the missing call face named", refusal.Error)
		}
		if !reflect.DeepEqual(refusal.Tools, []string{"decl-only"}) {
			t.Fatalf("tools = %v, want [decl-only]", refusal.Tools)
		}
	})

	t.Run("inner call failure", func(t *testing.T) {
		target := namedTool("t1")
		target.callFn = func(context.Context, []byte) (any, error) {
			return nil, errors.New("quota exhausted")
		}
		answer, callErr := NewCallTool(alphaRegistry(t, target)).Call(ctx, []byte(`{"server":"alpha","tool":"t1"}`))
		refusal := decodeCallResult(t, answer, callErr)
		for _, want := range []string{"alpha", "t1", "quota exhausted"} {
			if !strings.Contains(refusal.Error, want) {
				t.Fatalf("inner failure %q does not name %q", refusal.Error, want)
			}
		}
		if refusal.InputSchema == nil || refusal.InputSchema.Type != "object" {
			t.Fatalf("inner failure carries %+v, want the target input schema", refusal.InputSchema)
		}
	})

	t.Run("successful call is returned untouched", func(t *testing.T) {
		target := namedTool("t1")
		want := map[string]any{"answer": "42", "parts": []any{"a", "b"}}
		target.callFn = func(context.Context, []byte) (any, error) { return want, nil }
		got, err := NewCallTool(alphaRegistry(t, target)).Call(ctx, []byte(`{"server":"alpha","tool":"t1","args":{}}`))
		if err != nil {
			t.Fatalf("successful call = %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("successful call = %#v, want the native result untouched %#v", got, want)
		}
	})

	t.Run("declaration ignores registry content", func(t *testing.T) {
		registry := NewServerRegistry()
		tool := NewCallTool(registry)
		declaration := tool.Declaration()
		if declaration.Name != ToolNameMCPCall {
			t.Fatalf("declaration name = %q, want %q", declaration.Name, ToolNameMCPCall)
		}
		if !reflect.DeepEqual(declaration.InputSchema.Required, []string{MCPCallArgServer, MCPCallArgTool, MCPCallArgArgs}) {
			t.Fatalf("required keys = %v", declaration.InputSchema.Required)
		}
		if err := registry.Add(ctx, "alpha", &fakeToolSet{name: "alpha"}); err != nil {
			t.Fatalf("register alpha: %v", err)
		}
		if tool.Declaration() != declaration {
			t.Fatalf("the declaration moved when a server was registered")
		}
		if err := registry.Remove(ctx, "alpha"); err != nil {
			t.Fatalf("remove alpha: %v", err)
		}
		if tool.Declaration() != declaration {
			t.Fatalf("the declaration moved when a server was removed")
		}
	})
}

// TestDiscoverGatewayReadsTheLiveRegistry pins that discovery reads the injected table at call time and never hides a broken sibling.
// - an empty registry answers an empty tool list and count zero without an error
// - a server registered after construction appears at the next call
// - a query matches names by substring and by token-AND across name and description
// - a broken server is reported by name beside the tools of its healthy siblings
// - a server listing no tools contributes no failure
// - every discovered entry carries the exact call guidance and the input schema
// - reload answers the call declaration before the discovery declaration
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func TestDiscoverGatewayReadsTheLiveRegistry(t *testing.T) {
	ctx := context.Background()

	discover := func(t *testing.T, registry Registry, query string) discoverResult {
		t.Helper()
		tool := NewDiscoverTool(registry, nil)
		result, err := tool.Call(ctx, []byte(`{"query":`+strconvQuote(query)+`}`))
		if err != nil {
			t.Fatalf("discover = %v, want a result", err)
		}
		text, ok := result.(string)
		if !ok {
			t.Fatalf("discover answered %T, want the JSON text", result)
		}
		var decoded discoverResult
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			t.Fatalf("discover result %q does not decode: %v", text, err)
		}
		return decoded
	}

	webTool := &fakeTool{decl: &trpctool.Declaration{
		Name:        "web_search_prime",
		Description: "search the public web",
		InputSchema: &trpctool.Schema{Type: "object"},
	}}
	fileTool := &fakeTool{decl: &trpctool.Declaration{
		Name:        "file_read",
		Description: "read a local file",
	}}

	if result := discover(t, NewServerRegistry(), ""); result.Count != 0 || len(result.Tools) != 0 || result.Error != "" {
		t.Fatalf("empty registry discovery = %+v, want tools=[] count=0", result)
	}

	registry := NewServerRegistry()
	discoverTool := NewDiscoverTool(registry, nil)
	before, err := discoverTool.Call(ctx, nil)
	if err != nil {
		t.Fatalf("discovery before registration = %v", err)
	}
	if beforeText := decodeDiscoverText(t, before); beforeText.Count != 0 {
		t.Fatalf("discovery before registration = %+v, want count 0", beforeText)
	}
	if err := registry.Add(ctx, "alpha", &fakeToolSet{name: "alpha", tools: []trpctool.Tool{webTool, fileTool}}); err != nil {
		t.Fatalf("register alpha: %v", err)
	}
	after, err := discoverTool.Call(ctx, nil)
	if err != nil {
		t.Fatalf("discovery after registration = %v", err)
	}
	afterText := decodeDiscoverText(t, after)
	if afterText.Count != 2 || len(afterText.Tools) != 2 {
		t.Fatalf("discovery after registration = %+v, want the two registered tools", afterText)
	}

	cases := []struct {
		query string
		want  []string
	}{
		{query: "web", want: []string{"web_search_prime"}},
		{query: "web search", want: []string{"web_search_prime"}},
		{query: "WEB_SEARCH", want: []string{"web_search_prime"}},
		{query: "read", want: []string{"file_read"}},
		{query: "search file", want: nil},
	}
	for _, tc := range cases {
		result := discover(t, registry, tc.query)
		names := make([]string, 0, len(result.Tools))
		for _, entry := range result.Tools {
			names = append(names, entry.Tool)
		}
		if len(names) != len(tc.want) {
			t.Fatalf("query %q discovered %v, want %v", tc.query, names, tc.want)
		}
		for i := range names {
			if names[i] != tc.want[i] {
				t.Fatalf("query %q discovered %v, want %v", tc.query, names, tc.want)
			}
		}
	}

	brokenRegistry := NewServerRegistry()
	if err := brokenRegistry.Add(ctx, "broken", &fakeToolSet{name: "broken", initErr: errors.New("connection refused")}); err != nil {
		t.Fatalf("register broken: %v", err)
	}
	if err := brokenRegistry.Add(ctx, "healthy", &fakeToolSet{name: "healthy", tools: []trpctool.Tool{webTool}}); err != nil {
		t.Fatalf("register healthy: %v", err)
	}
	if err := brokenRegistry.Add(ctx, "empty", &fakeToolSet{name: "empty"}); err != nil {
		t.Fatalf("register empty: %v", err)
	}
	broken := discover(t, brokenRegistry, "")
	if len(broken.Tools) != 1 || broken.Tools[0].Server != "healthy" {
		t.Fatalf("discovery beside a broken server = %+v, want the healthy tool", broken)
	}
	if len(broken.Failures) != 1 || broken.Failures[0].Server != "broken" {
		t.Fatalf("failures = %+v, want only the broken server", broken.Failures)
	}
	if !strings.Contains(broken.Failures[0].Error, "connection refused") {
		t.Fatalf("failure = %+v, want the refresh cause", broken.Failures[0])
	}

	guided := discover(t, registry, "web_search")
	if len(guided.Tools) != 1 {
		t.Fatalf("guided discovery = %+v, want one entry", guided)
	}
	entry := guided.Tools[0]
	if want := `mcp_call(server="alpha", tool="web_search_prime", args={...})`; entry.Call != want {
		t.Fatalf("guidance = %q, want %q", entry.Call, want)
	}
	if entry.Source != "mcp:alpha" {
		t.Fatalf("source = %q, want mcp:alpha", entry.Source)
	}
	if entry.InputSchema == nil || entry.InputSchema.Type != "object" {
		t.Fatalf("entry carries %+v, want the tool input schema", entry.InputSchema)
	}

	gateways := NewGateways(registry, nil)
	declarations := gateways.ReloadDeclarations(ctx)
	if len(declarations) != 2 || declarations[0].Name != ToolNameMCPCall || declarations[1].Name != ToolNameMCPDiscover {
		t.Fatalf("reload order = %v, want call then discover", declarationNames(declarations))
	}
	if gateways.ReloadDeclarations(ctx)[0] != declarations[0] {
		t.Fatalf("reload answered a different call declaration")
	}
}

func decodeCallResult(t *testing.T, answer any, err error) callResult {
	t.Helper()
	if err != nil {
		t.Fatalf("gateway answered a Go error %v, want a named result", err)
	}
	text, ok := answer.(string)
	if !ok {
		t.Fatalf("gateway answered %T, want the JSON text", answer)
	}
	var decoded callResult
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("gateway result %q does not decode: %v", text, err)
	}
	return decoded
}

func decodeDiscoverText(t *testing.T, answer any) discoverResult {
	t.Helper()
	text, ok := answer.(string)
	if !ok {
		t.Fatalf("discovery answered %T, want the JSON text", answer)
	}
	var decoded discoverResult
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("discovery result %q does not decode: %v", text, err)
	}
	return decoded
}

func declarationNames(declarations []*trpctool.Declaration) []string {
	names := make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		names = append(names, declaration.Name)
	}
	return names
}

func strconvQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

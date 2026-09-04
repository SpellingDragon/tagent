package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// fakeTool is a minimal CallableTool with a fixed declaration.
type fakeTool struct {
	name    string
	callErr error
	result  any
	gotArgs []byte
}

func (f *fakeTool) Declaration() *trpctool.Declaration {
	return &trpctool.Declaration{
		Name:        f.name,
		Description: "fake tool " + f.name,
		InputSchema: &trpctool.Schema{
			Type: "object",
			Properties: map[string]*trpctool.Schema{
				"search_query": {Type: "string"},
			},
			Required: []string{"search_query"},
		},
	}
}

func (f *fakeTool) Call(_ context.Context, args []byte) (any, error) {
	f.gotArgs = args
	if f.callErr != nil {
		return nil, f.callErr
	}
	return f.result, nil
}

func newTestRegistry(t *testing.T, tools ...trpctool.Tool) *Registry {
	t.Helper()
	r := NewRegistry()
	r.Add("mock", &countingToolSet{name: "mock", tools: tools})
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func callJSON(t *testing.T, ct trpctool.CallableTool, payload string) (any, string) {
	t.Helper()
	res, err := ct.Call(context.Background(), []byte(payload))
	require.NoError(t, err)
	b, err := json.Marshal(res)
	require.NoError(t, err)
	return res, string(b)
}

func TestCallTool_Success(t *testing.T) {
	ft := &fakeTool{name: "webSearchPrime", result: map[string]any{"answer": 42}}
	ct := NewCallTool(newTestRegistry(t, ft))

	_, out := callJSON(t, ct, `{"server":"mock","tool":"webSearchPrime","args":{"search_query":"golang"}}`)
	assert.Contains(t, out, `"answer":42`)
	assert.JSONEq(t, `{"search_query":"golang"}`, string(ft.gotArgs), "args must pass through verbatim")
}

func TestCallTool_EmptyArgsDefaultsToObject(t *testing.T) {
	ft := &fakeTool{name: "noArgs", result: "ok"}
	ct := NewCallTool(newTestRegistry(t, ft))

	callJSON(t, ct, `{"server":"mock","tool":"noArgs"}`)
	assert.JSONEq(t, `{}`, string(ft.gotArgs))
}

func TestCallTool_UnknownServer_ListsAvailable(t *testing.T) {
	ct := NewCallTool(newTestRegistry(t, &fakeTool{name: "x"}))

	_, out := callJSON(t, ct, `{"server":"nope","tool":"x"}`)
	assert.Contains(t, out, "unknown MCP server")
	assert.Contains(t, out, `"available_servers":["mock"]`)
}

func TestCallTool_UnknownTool_ListsServerTools(t *testing.T) {
	ct := NewCallTool(newTestRegistry(t, &fakeTool{name: "alpha"}, &fakeTool{name: "beta"}))

	_, out := callJSON(t, ct, `{"server":"mock","tool":"gamma"}`)
	assert.Contains(t, out, "not found on MCP server")
	assert.Contains(t, out, `"available_tools":["alpha","beta"]`)
}

func TestCallTool_TargetError_EchoesInputSchema(t *testing.T) {
	ft := &fakeTool{name: "webSearchPrime", callErr: errors.New("missing search_query")}
	ct := NewCallTool(newTestRegistry(t, ft))

	_, out := callJSON(t, ct, `{"server":"mock","tool":"webSearchPrime","args":{}}`)
	assert.Contains(t, out, "failed")
	assert.Contains(t, out, "input_schema")
	assert.Contains(t, out, "search_query")
}

func TestCallTool_EmptyRegistry(t *testing.T) {
	r := NewRegistry()
	t.Cleanup(func() { _ = r.Close() })
	ct := NewCallTool(r)

	_, out := callJSON(t, ct, `{"server":"a","tool":"b"}`)
	assert.Contains(t, out, "no MCP servers are registered")

	// Nil registry falls back to the empty stub (factory must not fail).
	ctNil := NewCallTool(nil)
	_, out = callJSON(t, ctNil, `{"server":"a","tool":"b"}`)
	assert.Contains(t, out, "no MCP servers are registered")
}

func TestCallTool_MissingServerOrTool(t *testing.T) {
	ct := NewCallTool(newTestRegistry(t, &fakeTool{name: "x"}))

	_, out := callJSON(t, ct, `{"server":"","tool":""}`)
	assert.Contains(t, out, "requires both")
	assert.Contains(t, out, `"available_servers":["mock"]`)
}

func TestCallTool_DeclarationConstantAcrossRegistryMutations(t *testing.T) {
	r := NewRegistry()
	t.Cleanup(func() { _ = r.Close() })
	ct := NewCallTool(r)

	before, err := json.Marshal(ct.Declaration())
	require.NoError(t, err)

	r.Add("s1", &countingToolSet{name: "s1", tools: []trpctool.Tool{&fakeTool{name: "t"}}})
	r.Add("s2", &countingToolSet{name: "s2"})
	r.Remove("s1")

	after, err := json.Marshal(ct.Declaration())
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after),
		"mcp_call declaration must not change with registry content (prefix-cache invariant)")
}

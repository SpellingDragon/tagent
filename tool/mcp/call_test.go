package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/agent/reliability"
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

// TestMCPCall_CircuitBreak pins the degraded-MCP short-circuit and its probe cadence.
// - While DepMCP is degraded with probeEvery=N>0, every call but the Nth short-circuits with a readable result.
// - The failure permeates as a result rather than an error, so the agent turn survives a dead upstream.
// - The breaker is off by default: probeEvery=0 lets every call through.
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-gateway-injection
func TestMCPCall_CircuitBreak(t *testing.T) {
	reg := NewRegistry()
	ct := NewCallTool(reg)
	mgr := reliability.NewDegradationManager(nil)
	ct.SetDegradation(mgr)
	ct.SetMCPProbeEvery(2)

	for i := 0; i < 6; i++ {
		mgr.ReportFailure(reliability.DepMCP, context.DeadlineExceeded)
	}
	if !mgr.IsDegraded(reliability.DepMCP) {
		t.Fatal("setup: DepMCP should be degraded")
	}

	call := func() string {
		res, err := ct.Call(context.Background(), []byte(`{"server":"x","tool":"y","args":{}}`))
		if err != nil {
			t.Fatalf("breaker must not error: %v", err)
		}
		r, _ := res.(callErrorResult)
		return r.Error
	}
	first := call()
	second := call()
	if !strings.Contains(first, "熔断") {
		t.Fatalf("first call should be circuit-broken, got %q", first)
	}
	if strings.Contains(second, "熔断") {
		t.Fatalf("second call (probe) must pass through, got %q", second)
	}

	ct2 := NewCallTool(reg)
	ct2.SetDegradation(mgr)
	for i := 0; i < 6; i++ {
		mgr.ReportFailure(reliability.DepMCP, context.DeadlineExceeded)
	}
	res, _ := ct2.Call(context.Background(), []byte(`{"server":"x","tool":"y","args":{}}`))
	if strings.Contains(res.(callErrorResult).Error, "熔断") {
		t.Fatal("probeEvery=0 must disable the breaker (zero behavior change)")
	}
}

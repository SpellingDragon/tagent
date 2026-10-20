package capabilities

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	mcpgw "github.com/SpellingDragon/tagent/tool/mcp"
)

// TestMCPFaceBindsTheDeclaredServers drives the MCP gateways the declaration grants through the public assembly:
// - the declared mcp_call and mcp_discover ids reach the request as the two gateway tools
// - the server the declaration names is bound onto the live registry the gateway reads
// - the call gateway reaches the real wire of the declared server and returns its result untouched
// - a server no generation declared is refused by name instead of reaching anything
//
// 契约: docs/wiki/tool/tool-architecture.md#mcp-live-registry
func TestMCPFaceBindsTheDeclaredServers(t *testing.T) {
	t.Run("the declared server answers through the call gateway", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		alpha := newMCPFake(t, "alpha")
		alpha.registerEcho(t, "echo", "echo the text back")

		round := 0
		provider := &ntModel{name: "model-mcp", answer: func(req *model.Request) *model.Response {
			round++
			if round == 1 {
				return ntToolTurn(ntCall("call-mcp", mcpgw.ToolNameMCPCall, `{"server":"alpha","tool":"echo","args":{"text":"hi"}}`))
			}
			for _, name := range []string{mcpgw.ToolNameMCPCall, mcpgw.ToolNameMCPDiscover} {
				if req.Tools[name] == nil {
					t.Errorf("second request tools carry no %q, want the declared MCP gateway granted", name)
				}
			}
			checkObservedBody(t, req, "call-mcp", mcpgw.ToolNameMCPCall, "echo:hi")
			return ntFinalTurn("the declared server answered")
		}}

		run, err := tagent.Assemble(ctx, mcpFaceDeclaration(map[string]config.MCPServerSpec{
			"alpha": {Transport: "streamable-http", URL: alpha.url},
		}), map[event.AgentID]tagent.NativeAgent{
			"agent-mcp": {Model: provider},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		turn, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-mcp-face",
			Address:      event.Address{AgentID: "agent-mcp", SessionID: "session-mcp-face"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "search the web"}},
		})
		if err != nil {
			t.Fatalf("invoke the MCP face: %v", err)
		}
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		if got := alpha.callCount("echo"); got != 1 {
			t.Errorf("fake server calls = %d, want the gateway to reach the declared server exactly once", got)
		}
		if err := run.CloseMCPServers(); err != nil {
			t.Errorf("close the declared servers: %v, want the release to answer no error", err)
		}
	})

	t.Run("a server no generation declared is refused by name", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		round := 0
		provider := &ntModel{name: "model-mcp", answer: func(req *model.Request) *model.Response {
			round++
			if round == 1 {
				return ntToolTurn(ntCall("call-mcp", mcpgw.ToolNameMCPCall, `{"server":"alpha","tool":"echo","args":{"text":"hi"}}`))
			}
			checkObservedBody(t, req, "call-mcp", mcpgw.ToolNameMCPCall, "no MCP server is registered")
			return ntFinalTurn("the undeclared server was refused")
		}}

		run, err := tagent.Assemble(ctx, mcpFaceDeclaration(nil), map[event.AgentID]tagent.NativeAgent{
			"agent-mcp": {Model: provider},
		}, tagent.WithGovernance(passPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		if _, err := run.Invoke(ctx, tagent.InvokeRequest{
			InvocationID: "inv-mcp-face-undeclared",
			Address:      event.Address{AgentID: "agent-mcp", SessionID: "session-mcp-face-undeclared"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "search the web"}},
		}); err != nil {
			t.Fatalf("invoke the MCP face: %v", err)
		}
		if err := run.CloseMCPServers(); err != nil && !errors.Is(err, mcpgw.ErrRegistryClosed) {
			t.Errorf("close the empty registry: %v, want no error", err)
		}
	})
}

// mcpFaceDeclaration declares one agent that grants both MCP gateways over the
// declared server generation the deployment names.
func mcpFaceDeclaration(servers map[string]config.MCPServerSpec) *config.Declaration {
	return &config.Declaration{
		Entry:   "agent-mcp",
		Servers: servers,
		Agents: []config.AgentSpec{{
			ID:    "agent-mcp",
			Model: "model-mcp",
			Tools: []config.ToolRef{
				{Name: mcpgw.ToolNameMCPCall},
				{Name: mcpgw.ToolNameMCPDiscover},
			},
		}},
	}
}

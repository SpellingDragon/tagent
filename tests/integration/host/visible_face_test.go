// 契约: docs/wiki/agent/event-flow.md#e2e-turn-sequence
package host

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/state"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func TestTheNarrowedFaceNarrowsEveryFace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writePromptAt(t, filepath.Join(root, "prompts", "system.md"), "system prompt\n")
	writeSkillAt(t, root, "greet", "greet body v1\n")
	writeSkillAt(t, root, "extra", "extra body v1\n")
	echo := function.NewFunctionTool(
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return "fn-out:" + in.Text, nil
		},
		function.WithName("fn_tool"),
		function.WithDescription("echo the text back through a native function tool"),
	)
	other := function.NewFunctionTool(
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return "other:" + in.Text, nil
		},
		function.WithName("other_tool"),
		function.WithDescription("a second native function tool the subset omits"),
	)
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	spec := config.AgentSpec{
		ID:               "agent-a",
		Model:            "model-a",
		EnvironmentBlock: true,
		Files: []config.FileRef{
			{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
			{Path: filepath.Join(root, "skills", "greet", "SKILL.md"), Kind: config.FileKindSkill},
			{Path: filepath.Join(root, "skills", "extra", "SKILL.md"), Kind: config.FileKindSkill},
		},
		Tools: []config.ToolRef{
			{Name: "docs", Endpoint: "http://mcp.local"},
			{Name: "evaluation", Endpoint: "http://mcp2.local"},
		},
	}
	spec.File = config.FileSpec{BaseDir: root}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{spec}}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{echo, other}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if err := run.NarrowFace(ctx, "agent-a", state.VisibleSelection{
		Tools:  []string{"fn_tool"},
		MCP:    []string{"docs"},
		Skills: []string{"greet"},
	}); err != nil {
		t.Fatalf("narrow the face: %v", err)
	}
	invokeTurn(t, run, "inv-visible-1", event.Address{AgentID: "agent-a", SessionID: "session-visible"}, "看看可见面")

	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	if len(requests[0].Tools) != 1 || requests[0].Tools["fn_tool"] == nil {
		t.Fatalf("request tools = %v, want the declared subset (fn_tool) alone", requests[0].Tools)
	}
	block := trailingSystem(t, requests[0].Messages)
	for _, want := range []string{"- tools: fn_tool", "- skills: greet", "- mcp: docs@http://mcp.local"} {
		if !strings.Contains(block, want) {
			t.Fatalf("environment block = %q, want %q inside the narrowed face", block, want)
		}
	}
	for _, unwanted := range []string{"other_tool", "evaluation", "extra"} {
		if strings.Contains(block, unwanted) {
			t.Fatalf("environment block = %q, want %q absent from the narrowed face", block, unwanted)
		}
	}
	for _, message := range requests[0].Messages {
		if strings.Contains(message.Content, "extra body v1") {
			t.Fatalf("request carries the narrowed-out skill body %q: the prefix must match the visible list", message.Content)
		}
	}
}

func TestTheNarrowedFaceReachesTheNextRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	echo := function.NewFunctionTool(
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return "fn-out:" + in.Text, nil
		},
		function.WithName("fn_tool"),
		function.WithDescription("echo the text back through a native function tool"),
	)
	other := function.NewFunctionTool(
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return "other:" + in.Text, nil
		},
		function.WithName("other_tool"),
		function.WithDescription("a second native function tool"),
	)
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:               "agent-a",
			Model:            "model-a",
			EnvironmentBlock: true,
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{echo, other}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-visible-hot"}
	invokeTurn(t, run, "inv-hot-1", addr, "第一轮")
	if got := provider.recorded(); len(got[0].Tools) != 2 {
		t.Fatalf("first request tools = %v, want the full configured face by default", got[0].Tools)
	}
	if err := run.NarrowFace(ctx, "agent-a", state.VisibleSelection{Tools: []string{"fn_tool"}}); err != nil {
		t.Fatalf("narrow the face: %v", err)
	}
	invokeTurn(t, run, "inv-hot-2", addr, "第二轮")
	requests := provider.recorded()
	second := requests[len(requests)-1]
	if len(second.Tools) != 1 || second.Tools["fn_tool"] == nil {
		t.Fatalf("second request tools = %v, want the narrowed subset on the very next request", second.Tools)
	}
	block := trailingSystem(t, second.Messages)
	if !strings.Contains(block, "- tools: fn_tool") || strings.Contains(block, "other_tool") {
		t.Fatalf("environment block = %q, want the narrowed list in the same request", block)
	}
}

// trailingSystem reports the last system message of one request: the dynamic
// section the request closes with.
func trailingSystem(t *testing.T, messages []model.Message) string {
	t.Helper()
	for at := len(messages) - 1; at >= 0; at-- {
		if messages[at].Role == model.RoleSystem {
			return messages[at].Content
		}
	}
	t.Fatalf("request carries no system message: %v", messages)
	return ""
}

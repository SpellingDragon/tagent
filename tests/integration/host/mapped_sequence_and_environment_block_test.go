package host

import (
	"context"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// invokeTurn drives one invocation of the named session through the formal
// entry and fails the test when the turn is refused.
func invokeTurn(t *testing.T, run *assembly.Run, id string, addr event.Address, input string) {
	t.Helper()
	if _, err := run.Invoke(context.Background(), assembly.InvokeRequest{
		InvocationID: event.InvocationID(id),
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: input}},
	}); err != nil {
		t.Fatalf("invoke %s: %v", id, err)
	}
}

// TestProjectionCarriesTheCommittedSequence checks the mapped sequence an agent declares a replay budget for.
//   - the request after the first range presents that range's input and output from the sequence
//   - the session transcript carries none of them, so no fact reaches the model from two faces
//   - the sequence maps the frozen event types onto the user and assistant roles
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionCarriesTheCommittedSequence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	round := 0
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		round++
		return entryFinalTurn("回答第" + strconv.Itoa(round) + "轮")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:        "agent-a",
			Model:     "model-a",
			MaxTokens: 100,
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-mapped"}
	invokeTurn(t, run, "inv-mapped-1", addr, "第一轮输入")
	invokeTurn(t, run, "inv-mapped-2", addr, "第二轮输入")

	if history := run.SessionHistory(addr); len(history) != 0 {
		t.Fatalf("session transcript = %+v, want the mapped sequence to carry the ended range instead", history)
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want one request per invocation", len(requests))
	}
	got := requests[1].Messages
	if len(got) != 3 {
		t.Fatalf("second request carries %d messages, want the two mapped facts and the range input: %v", len(got), got)
	}
	if got[0].Role != model.RoleUser || got[0].Content != "第一轮输入" {
		t.Fatalf("message 0 = (%q, %q), want the earlier range's input mapped as a user message", got[0].Role, got[0].Content)
	}
	if got[1].Role != model.RoleAssistant || got[1].Content != "回答第1轮" {
		t.Fatalf("message 1 = (%q, %q), want the earlier range's output mapped as an assistant message", got[1].Role, got[1].Content)
	}
	if got[2].Role != model.RoleUser || got[2].Content != "第二轮输入" {
		t.Fatalf("message 2 = (%q, %q), want the range in flight carried by the transcript alone", got[2].Role, got[2].Content)
	}
}

// TestProjectionAbsentKeepsTheTranscript checks the assembly of an agent naming no replay budget.
//   - the no-budget window registers its mapped sequence like a budgeted one: the window is the budget and the fold measure, never the registration gate
//   - the ended range rides the mapped sequence and the transcript keeps none of it, so a fact reaches the model from one face
//   - the request carries each fact once and no environment block
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectionAbsentKeepsTheTranscript(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	round := 0
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		round++
		return entryFinalTurn("回答第" + strconv.Itoa(round) + "轮")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-unmapped"}
	invokeTurn(t, run, "inv-unmapped-1", addr, "第一轮输入")
	invokeTurn(t, run, "inv-unmapped-2", addr, "第二轮输入")

	history := run.SessionHistory(addr)
	if len(history) != 0 {
		t.Fatalf("session transcript = %+v, want the mapped sequence of the no-budget window to carry the ended range instead — one face per fact", history)
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want one request per invocation", len(requests))
	}
	got := requests[1].Messages
	if len(got) != 3 {
		t.Fatalf("second request carries %d messages, want the two mapped facts and the range input: %v", len(got), got)
	}
	wantCarried := []struct {
		role    model.Role
		content string
	}{
		{model.RoleUser, "第一轮输入"},
		{model.RoleAssistant, "回答第1轮"},
		{model.RoleUser, "第二轮输入"},
	}
	for i, want := range wantCarried {
		if got[i].Role != want.role || got[i].Content != want.content {
			t.Fatalf("message %d = (%q, %q), want (%q, %q): the ended range must reach the model through the mapped sequence", i, got[i].Role, got[i].Content, want.role, want.content)
		}
	}
	for _, message := range got {
		if strings.Contains(message.Content, environmentBlockOpen) {
			t.Fatalf("request carries an environment block the agent never declared: %v", got)
		}
	}
	carried := 0
	for _, message := range got {
		if message.Content == "第一轮输入" {
			carried++
		}
	}
	if carried != 1 {
		t.Fatalf("the earlier range input is carried %d times, want exactly once: %v", carried, got)
	}
}

// TestEnvironmentBlockClosesTheRequestAssembly checks where the declared environment block stands.
//   - the block is the last message of the request, behind the mapped sequence and the invocation's own input
//   - the block names the agent, the empty tool, skill and MCP faces and the empty task table
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestEnvironmentBlockClosesTheRequestAssembly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	round := 0
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		round++
		return entryFinalTurn("回答第" + strconv.Itoa(round) + "轮")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:               "agent-a",
			Model:            "model-a",
			MaxTokens:        100,
			EnvironmentBlock: true,
		}},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	addr := event.Address{AgentID: "agent-a", SessionID: "session-environment"}
	invokeTurn(t, run, "inv-environment-1", addr, "第一轮输入")
	invokeTurn(t, run, "inv-environment-2", addr, "第二轮输入")

	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want one request per invocation", len(requests))
	}
	got := requests[1].Messages
	if len(got) != 4 {
		t.Fatalf("second request carries %d messages, want the two mapped facts, the input and the block: %v", len(got), got)
	}
	for i := 0; i < 3; i++ {
		if strings.Contains(got[i].Content, environmentBlockOpen) {
			t.Fatalf("message %d carries the environment marker, want it behind the mapped sequence and the input", i)
		}
	}
	if got[2].Role != model.RoleUser || got[2].Content != "第二轮输入" {
		t.Fatalf("message 2 = (%q, %q), want the invocation input ahead of the block", got[2].Role, got[2].Content)
	}
	block := got[3]
	if block.Role != model.RoleUser || !strings.HasPrefix(block.Content, environmentBlockOpen+"\n") ||
		!strings.HasSuffix(block.Content, environmentBlockClose) {
		t.Fatalf("message 3 = (%q, %q), want the marked environment block trailing the request", block.Role, block.Content)
	}
	for _, want := range []string{
		"- agent: agent-a",
		"- model: model-a",
		"- tools: (none)",
		"- skills: (none)",
		"- mcp: (none)",
		"- tasks: 0 open",
		"  - (none)",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("environment block = %q, want it to name %q beside the empty task table", block.Content, want)
		}
	}
}

// TestEnvironmentBlockNamesTheAvailableFaces checks the dynamic faces the block reports.
//   - every callable tool the binding carries is named, in name order
//   - the MCP faces the declaration names are reported beside them
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestEnvironmentBlockNamesTheAvailableFaces(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writePromptAt(t, filepath.Join(root, "prompts", "system.md"), "system prompt\n")
	writeSkillAt(t, root, "greet", "greet body v1\n")
	echo := function.NewFunctionTool(
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return "fn-out:" + in.Text, nil
		},
		function.WithName("fn_tool"),
		function.WithDescription("echo the text back through a native function tool"),
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
			{Path: filepath.Join(root, "skills", "greet", skill.SkillFile), Kind: config.FileKindSkill},
		},
		Tools: []config.ToolRef{
			{Name: "evaluation"},
			{Name: "docs", Endpoint: "http://mcp.local"},
		},
	}
	spec.File = config.FileSpec{BaseDir: root}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{spec},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{echo}},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	invokeTurn(t, run, "inv-environment-faces", event.Address{AgentID: "agent-a", SessionID: "session-faces"}, "看看环境")

	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	got := requests[0].Messages
	if len(got) != 4 {
		t.Fatalf("request carries %d messages, want the prompt, the skill body, the input and the block: %v", len(got), got)
	}
	block := got[len(got)-1]
	if block.Role != model.RoleUser || !strings.HasPrefix(block.Content, environmentBlockOpen+"\n") ||
		!strings.HasSuffix(block.Content, environmentBlockClose) {
		t.Fatalf("message %d = (%q, %q), want the marked environment block trailing the request", len(got)-1, block.Role, block.Content)
	}
	if input := got[len(got)-2]; input.Role != model.RoleUser || input.Content != "看看环境" {
		t.Fatalf("message %d = (%q, %q), want the range input ahead of the block", len(got)-2, input.Role, input.Content)
	}
	if !strings.Contains(got[0].Content, "system prompt") || !strings.Contains(got[1].Content, "greet body v1") {
		t.Fatalf("request prefix = %v, want the fixed prompt and skill bodies in front of the block", got[:2])
	}
	for _, want := range []string{
		"- skills: greet",
		"- mcp: docs@http://mcp.local",
		"- tasks: 0 open",
	} {
		if !strings.Contains(block.Content, want) {
			t.Fatalf("environment block = %q, want it to name %q", block.Content, want)
		}
	}
	toolsLine := ""
	for _, line := range strings.Split(block.Content, "\n") {
		if strings.HasPrefix(line, "- tools: ") {
			toolsLine = strings.TrimPrefix(line, "- tools: ")
		}
	}
	if toolsLine == "" {
		t.Fatalf("environment block = %q, want the tool face named", block.Content)
	}
	named := strings.Split(toolsLine, ", ")
	for _, want := range []string{"fn_tool", "read_file", "save_file", "skill_load"} {
		if !slices.Contains(named, want) {
			t.Fatalf("tool face = %v, want it to name %q", named, want)
		}
	}
	if !sort.StringsAreSorted(named) {
		t.Fatalf("tool face = %v, want the names in name order", named)
	}
}

// TestEnvironmentBlockNamesTheOpenObligations checks the asynchronous work the block reports.
//   - a call another range left open is named as one open obligation under its task
//   - the block of a run holding no obligation still carries the empty task table
//
// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
func TestEnvironmentBlockNamesTheOpenObligations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	blocking := function.NewFunctionTool(
		func(inner context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			close(started)
			select {
			case <-release:
				return "released-out:" + in.Text, nil
			case <-inner.Done():
				return "", inner.Err()
			}
		},
		function.WithName("blocking_tool"),
		function.WithDescription("hold the call open until the test releases it"),
	)
	blockedRound := 0
	blocked := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		blockedRound++
		if blockedRound == 1 {
			return entryToolTurn(entryCall("call-blocking", "blocking_tool", `{"text":"hold"}`))
		}
		return entryFinalTurn("released")
	}}
	observer := &entryModel{name: "model-b", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("observed")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{
			{ID: "agent-a", Model: "model-a"},
			{ID: "agent-b", Model: "model-b", EnvironmentBlock: true},
		},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"agent-a": {Model: blocked, Tools: []trpctool.CallableTool{blocking}},
		"agent-b": {Model: observer},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := run.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-blocking",
			Address:      event.Address{AgentID: "agent-a", SessionID: "session-blocking"},
			Input:        []model.Message{{Role: model.RoleUser, Content: "先挂起一个调用"}},
		})
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("the blocking call never started")
	}
	invokeTurn(t, run, "inv-observer", event.Address{AgentID: "agent-b", SessionID: "session-observer"}, "看看任务面")
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("invoke the blocking range: %v", err)
	}

	requests := observer.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one request of the observing range", len(requests))
	}
	got := requests[0].Messages
	if len(got) != 2 || !strings.HasPrefix(got[1].Content, environmentBlockOpen+"\n") ||
		!strings.HasSuffix(got[1].Content, environmentBlockClose) {
		t.Fatalf("request = %v, want the environment block trailing the input", got)
	}
	block := got[1].Content
	if !strings.Contains(block, "- tasks: 1 open") {
		t.Fatalf("environment block = %q, want the one open obligation counted", block)
	}
	named := false
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "  - ") && strings.Contains(line, "task=") {
			named = true
		}
	}
	if !named {
		t.Fatalf("environment block = %q, want the open obligation named beside the task it belongs to", block)
	}
}

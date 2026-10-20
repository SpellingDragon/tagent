package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// TestFileChangesReachNextRequest checks the 01.6 boundary on the formal entry.
//   - a prompt file rewritten between two invocations enters the next request's messages at once
//   - the rewrite takes effect with no reload and no structural generation in between
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestFileChangesReachNextRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	promptPath := filepath.Join(root, "prompts", "system.md")
	writePromptAt(t, promptPath, "prompt body v1\n")

	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{{Path: promptPath, Kind: config.FileKindPrompt}},
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	address := event.Address{AgentID: "agent-a", SessionID: "session-file-change"}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-file-change-1",
		Address:      address,
		Input:        []model.Message{{Role: model.RoleUser, Content: "first"}},
	}); err != nil {
		t.Fatalf("invoke the first request: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one request of the first invocation", len(requests))
	}
	if !carriesSystemBody(requests[0], "prompt body v1") {
		t.Fatalf("first request = %+v, want the prompt body v1 fixed at its boundary", requests[0].Messages)
	}

	writePromptAt(t, promptPath, "prompt body v2\n")
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-file-change-2",
		Address:      address,
		Input:        []model.Message{{Role: model.RoleUser, Content: "second"}},
	}); err != nil {
		t.Fatalf("invoke the second request: %v", err)
	}
	requests = provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want the second invocation's request recorded too", len(requests))
	}
	if !carriesSystemBody(requests[1], "prompt body v2") {
		t.Fatalf("second request = %+v, want the rewritten prompt body v2 carried into the next request", requests[1].Messages)
	}
	for i, msg := range requests[1].Messages {
		if strings.Contains(msg.Content, "prompt body v1") {
			t.Fatalf("second request message %d = %+v, want no stale body from the earlier read", i, msg)
		}
	}
}

// TestSkillBodyReachesNextRequest checks the 04.1 boundary on the formal entry.
//   - a skill body loaded through the skill tool enters the next request's model context
//   - the declared skill bodies the fixed files already carry appear exactly once each
func TestSkillBodyReachesNextRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writePromptAt(t, filepath.Join(root, "prompts", "system.md"), "system prompt\n")
	writeSkillAt(t, root, "greet", "greet body v1\n")
	writeSkillAt(t, root, "extra", "extra body v1\n")

	round := 0
	provider := &entryModel{name: "model-a", answer: func(req *model.Request) *model.Response {
		round++
		if round == 1 {
			return entryToolTurn(
				entryCall("call-greet", "skill_load", `{"skill":"greet"}`),
				entryCall("call-extra", "skill_load", `{"skill":"extra"}`),
			)
		}
		return entryFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{
				{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
				{Path: filepath.Join(root, "skills", "greet", skill.SkillFile), Kind: config.FileKindSkill},
			},
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-skill-body-1",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-skill-body"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "load the skills"}},
	}); err != nil {
		t.Fatalf("invoke the skill loads: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want the load decision and the closing decision", len(requests))
	}
	if containsBody(requests[0], "extra body v1") {
		t.Fatal("first request carried the extra skill body before the load, want no unloaded body")
	}
	if !containsBody(requests[1], "extra body v1") {
		t.Fatalf("second request = %+v, want the loaded extra skill body in the next request's model context", requests[1].Messages)
	}
	if !carriesSystemBody(requests[1], "extra body v1") {
		t.Fatalf("second request = %+v, want the loaded skill body carried as boundary material", requests[1].Messages)
	}
	if got := countBody(requests[1], "greet body v1"); got != 1 {
		t.Fatalf("second request carries the greet body %d times, want exactly once beside its load", got)
	}
}

// TestRequestCarriesProjectedBudget checks the 03.4 boundary on the formal entry.
//   - the budget assembly of a registered replay projection rides in front of the request transcript
//   - each presented entry is mapped under the frozen event-type→role table with no mirrored projection entry
func TestRequestCarriesProjectedBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	proj := replay.NewReplayProjection()
	for _, entry := range []replay.ProjectionEntry{
		{Key: "p-0001", Type: event.TypeExternalInput, Summary: "in", Content: "投影原文一", Time: base},
		{Key: "p-0002", Type: event.TypeAgentOutput, Summary: "out", Content: "投影原文二", Time: base.Add(time.Minute)},
	} {
		proj.Append(ctx, entry)
	}
	presented, err := proj.AssembleBudget(ctx, 100)
	if err != nil {
		t.Fatalf("AssembleBudget: %v", err)
	}

	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry:  "agent-a",
		Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	chain := agent.RequestChain{PartitionID: 7, SessionID: event.SessionID("session-projection")}
	if err := run.AttachReplay(chain, 100, proj); err != nil {
		t.Fatalf("AttachReplay: %v", err)
	}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-projection-1",
		Address:      event.Address{AgentID: "agent-a", SessionID: chain.SessionID},
		Input:        []model.Message{{Role: model.RoleUser, Content: "带投影的请求"}},
	}); err != nil {
		t.Fatalf("invoke the projected request: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	got := requests[0].Messages
	if len(got) != len(presented)+1 {
		t.Fatalf("request carries %d messages, want the %d projected entries in front of the transcript", len(got), len(presented))
	}
	wantRoles := map[string]model.Role{
		event.TypeExternalInput: model.RoleUser,
		event.TypeAgentOutput:   model.RoleAssistant,
	}
	for i, entry := range presented {
		if got[i].Content != entry.Content {
			t.Fatalf("message %d content = %q, want the budget assembly view %q", i, got[i].Content, entry.Content)
		}
		if want := wantRoles[entry.Type]; got[i].Role != want {
			t.Fatalf("message %d role = %q, want the frozen type %s mapped onto %q", i, got[i].Role, entry.Type, want)
		}
	}
}

// TestNativeToolFamiliesAdmitted checks the composition-root admission of the native tool families beside the evaluation criterion reference.
//   - every native family enters the model tool face and answers with a real admission receipt
//   - the evaluation reference — a criterion carrier, not a callable tool — is never listed on that face
func TestNativeToolFamiliesAdmitted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writePromptAt(t, filepath.Join(root, "prompts", "system.md"), "system prompt\n")
	writeSkillAt(t, root, "greet", "greet body v1\n")

	fnTool := function.NewFunctionTool(
		func(_ context.Context, in struct {
			Text string `json:"text"`
		}) (string, error) {
			return "fn-out:" + in.Text, nil
		},
		function.WithName("fn_tool"),
		function.WithDescription("echo the text back through a native function tool"),
	)
	round := 0
	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		round++
		if round == 1 {
			return entryToolTurn(
				entryCall("call-fn", "fn_tool", `{"text":"hi"}`),
				entryCall("call-save", "save_file", `{"file_name":"probe.txt","contents":"probe\n","overwrite":true}`),
				entryCall("call-load", "skill_load", `{"skill":"greet"}`),
			)
		}
		return entryFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{
				{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
				{Path: filepath.Join(root, "skills", "greet", skill.SkillFile), Kind: config.FileKindSkill},
			},
			Tools: []config.ToolRef{{Name: "evaluation"}},
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider, Tools: []trpctool.CallableTool{fnTool}},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-tool-families",
		Address:      event.Address{AgentID: "agent-a", SessionID: "session-tool-families"},
		Input:        []model.Message{{Role: model.RoleUser, Content: "use the tools"}},
	})
	if err != nil {
		t.Fatalf("invoke the tool families: %v", err)
	}
	if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
		t.Fatalf("turn = (%v, %d decisions), want the completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
	}
	requests := provider.recorded()
	if len(requests) != 2 {
		t.Fatalf("recorded requests = %d, want the call decision and the closing decision", len(requests))
	}
	tools := requests[0].Tools
	for _, name := range []string{"fn_tool", "read_file", "save_file", "skill_load"} {
		if tools[name] == nil {
			t.Errorf("request tools carry no %q, want every registered native tool family visible to the model", name)
		}
	}
	if tools["evaluation"] != nil {
		t.Fatal("request tools carry the evaluation criterion reference, want it skipped and never on the model tool face")
	}
	receipts := 0
	for _, msg := range requests[1].Messages {
		if msg.Role == model.RoleTool && strings.Contains(msg.Content, "accepted task-") {
			receipts++
		}
	}
	if receipts != 3 {
		t.Fatalf("second request carries %d admission receipts, want one real receipt per admitted call", receipts)
	}
}

// TestProjectedContextKeepsItsFixedLayout checks the parts one decision is assembled from and the order they take.
//   - the fixed file contents lead the request as one system prefix, in the order the binding carries them
//   - the folded projection follows the prefix, its compaction summary first under the frozen event-type role table
//   - the invocation's own input closes the request, so no fourth part sits between them
//   - every callable face the binding carries sits on the request's tool field, not among the messages
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func TestProjectedContextKeepsItsFixedLayout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writePromptAt(t, filepath.Join(root, "prompts", "system.md"), "system prompt\n")
	writeSkillAt(t, root, "greet", "greet body v1\n")

	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	proj := replay.NewReplayProjection()
	for _, entry := range []replay.ProjectionEntry{
		{Key: "p-0001", Type: event.TypeExternalInput, Summary: "in", Content: "投影原文一", Time: base},
		{Key: "p-0002", Type: event.TypeAgentOutput, Summary: "out", Content: "投影原文二", Time: base.Add(time.Minute)},
	} {
		proj.Append(ctx, entry)
	}
	summary, err := proj.Fold(ctx)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if proj.Len() != 1 {
		t.Fatalf("folded projection holds %d entries, want the one compaction entry the fold answers with", proj.Len())
	}
	if !strings.HasPrefix(summary.Content, "compaction: ") {
		t.Fatalf("compaction entry content = %q, want the summary line the fold minted", summary.Content)
	}

	provider := &entryModel{name: "model-a", answer: func(*model.Request) *model.Response {
		return entryFinalTurn("settled")
	}}
	decl := &config.Declaration{
		Entry: "agent-a",
		Agents: []config.AgentSpec{{
			ID:    "agent-a",
			Model: "model-a",
			Files: []config.FileRef{
				{Path: filepath.Join(root, "prompts", "system.md"), Kind: config.FileKindPrompt},
				{Path: filepath.Join(root, "skills", "greet", skill.SkillFile), Kind: config.FileKindSkill},
			},
		}},
	}
	run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
		"agent-a": {Model: provider},
	}, tagent.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}
	chain := agent.RequestChain{PartitionID: 7, SessionID: event.SessionID("session-layout")}
	if err := run.AttachReplay(chain, 100, proj); err != nil {
		t.Fatalf("AttachReplay: %v", err)
	}
	if _, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: "inv-layout",
		Address:      event.Address{AgentID: "agent-a", SessionID: chain.SessionID},
		Input:        []model.Message{{Role: model.RoleUser, Content: "装配序的请求"}},
	}); err != nil {
		t.Fatalf("invoke the layout request: %v", err)
	}
	requests := provider.recorded()
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want exactly one formal request", len(requests))
	}
	got := requests[0].Messages
	if len(got) != 4 {
		t.Fatalf("request carries %d messages, want the two fixed files, the folded entry and the input and nothing else: %v", len(got), got)
	}
	if got[0].Role != model.RoleSystem || !strings.Contains(got[0].Content, "system prompt") {
		t.Fatalf("message 0 = (%q, %q), want the prompt file content leading the request", got[0].Role, got[0].Content)
	}
	if got[1].Role != model.RoleSystem || !strings.Contains(got[1].Content, "greet body v1") {
		t.Fatalf("message 1 = (%q, %q), want the skill file content beside the prompt in the static prefix", got[1].Role, got[1].Content)
	}
	want, err := agent.RoleOf(replay.CompactionEventType)
	if err != nil {
		t.Fatalf("map %s onto a role: %v", replay.CompactionEventType, err)
	}
	if got[2].Role != want {
		t.Fatalf("message 2 role = %q, want the frozen %s role %q", got[2].Role, replay.CompactionEventType, want)
	}
	if got[2].Content != summary.Content {
		t.Fatalf("message 2 content = %q, want the compaction summary %q the fold answers with", got[2].Content, summary.Content)
	}
	if got[3].Role != model.RoleUser || got[3].Content != "装配序的请求" {
		t.Fatalf("message 3 = (%q, %q), want the invocation input closing the request", got[3].Role, got[3].Content)
	}
	for _, name := range []string{"read_file", "save_file", "skill_load"} {
		if face := requests[0].Tools[name]; face == nil {
			t.Errorf("request tools carry no %q, want every registered native tool family on the tool field", name)
		}
	}
}

// writePromptAt writes one prompt file at the named path.
func writePromptAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the prompt directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write the prompt file: %v", err)
	}
}

// writeSkillAt writes one skill folder with its SKILL.md under the given root,
// in the shape the framework skill repository scans and parses.
func writeSkillAt(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: \"the named skill\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file: %v", err)
	}
}

// containsBody reports whether any message of the request carries the named
// body.
func containsBody(req model.Request, body string) bool {
	return countBody(req, body) > 0
}

// carriesSystemBody reports whether a system-role boundary message of the
// request carries the named body.
func carriesSystemBody(req model.Request, body string) bool {
	for _, msg := range req.Messages {
		if msg.Role == model.RoleSystem && strings.Contains(msg.Content, body) {
			return true
		}
	}
	return false
}

// countBody reports how many messages of the request carry the named body.
func countBody(req model.Request, body string) int {
	got := 0
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, body) {
			got++
		}
	}
	return got
}

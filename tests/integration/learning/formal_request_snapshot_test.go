package learning

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent"
	"github.com/SpellingDragon/tagent/agent"
	"github.com/SpellingDragon/tagent/config"
	"github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/model"
	"github.com/SpellingDragon/tagent/rl"

	trpcmodel "trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestFormalRequestSnapshotHasNoSecrets drives the formal entry and checks the safe request snapshot its request boundary keeps.
//   - the snapshot is fixed before each model request starts and holds a fixed copy of what the model actually saw
//   - no URL credential of the request reaches the kept snapshot while the request keeps its own credentials
//   - the fixed snapshot belongs to its decision request and never to a tool result recorded beside it
//
// 契约: docs/wiki/rl/rl-architecture.md#trajectory-capture
func TestFormalRequestSnapshotHasNoSecrets(t *testing.T) {
	t.Run("the snapshot is fixed before each model request and holds what the model saw", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		root := t.TempDir()
		writePromptAt(t, filepath.Join(root, "prompts", "system.md"), "prompt body v1\n")
		writeSkillAt(t, root, "greet", "greet body v1\n")
		base := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
		proj := replay.NewReplayProjection()
		proj.Append(ctx, replay.ProjectionEntry{Key: "p-0001", Type: event.TypeExternalInput, Summary: "in", Content: "投影原文一", Time: base})
		proj.Append(ctx, replay.ProjectionEntry{Key: "p-0002", Type: event.TypeAgentOutput, Summary: "out", Content: "投影原文二", Time: base.Add(time.Minute)})
		var run *tagent.Run
		seen := 0
		provider := &learnModel{name: "model-a", answer: func(req *model.Request) *model.Response {
			snaps := run.RequestSnapshots()
			if len(snaps) != seen+1 {
				t.Fatalf("snapshots at model call %d = %d, want the snapshot of this request fixed before the call", seen+1, len(snaps))
			}
			got := mustSnapshotJSON(t, snaps[len(snaps)-1].Snapshot.Messages)
			want := mustSnapshotJSON(t, req.Messages)
			if !bytes.Equal(got, want) {
				t.Fatalf("snapshot messages = %s, want the fixed copy of the model-visible request %s", got, want)
			}
			seen++
			return learnFinalTurn("settled")
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
		assembled, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&learnTool{name: "echo-a", output: "out-a"}}},
		}, tagent.WithGovernance(formalPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		run = assembled
		chain := agent.RequestChain{PartitionID: 3, SessionID: event.SessionID("session-snapshot")}
		if err := run.AttachReplay(chain, 100, proj); err != nil {
			t.Fatalf("AttachReplay: %v", err)
		}
		firstTurn := invokeSnapshot(t, ctx, run, "inv-snapshot-1", chain.SessionID, "first")
		secondTurn := invokeSnapshot(t, ctx, run, "inv-snapshot-2", chain.SessionID, "second")
		requests := learnRequests(provider)
		snaps := run.RequestSnapshots()
		if len(requests) != 2 || len(snaps) != 2 {
			t.Fatalf("recorded (%d requests, %d snapshots), want one snapshot beside each of the two model requests", len(requests), len(snaps))
		}
		first := snaps[0].Snapshot.Messages
		if len(first) != 5 {
			t.Fatalf("first request snapshot carries %d messages, want five: prompt, skill, two projected entries and the input", len(first))
		}
		if first[0].Role != model.RoleSystem || !strings.Contains(first[0].Content, "prompt body v1") {
			t.Fatalf("snapshot message 0 = %+v, want the fixed prompt body", first[0])
		}
		if first[1].Role != model.RoleSystem || !strings.Contains(first[1].Content, "greet body v1") {
			t.Fatalf("snapshot message 1 = %+v, want the fixed skill body", first[1])
		}
		if first[2].Role != agent.RoleOf(event.TypeExternalInput) || first[2].Content != "投影原文一" {
			t.Fatalf("snapshot message 2 = %+v, want the first projected entry under the frozen role table", first[2])
		}
		if first[3].Role != agent.RoleOf(event.TypeAgentOutput) || first[3].Content != "投影原文二" {
			t.Fatalf("snapshot message 3 = %+v, want the second projected entry under the frozen role table", first[3])
		}
		if first[4].Role != model.RoleUser || first[4].Content != "first" {
			t.Fatalf("snapshot message 4 = %+v, want the input the invocation carried", first[4])
		}
		following := snaps[1].Snapshot.Messages
		if len(following) != 7 {
			t.Fatalf("second request snapshot carries %d messages, want seven: the boundary material plus the whole transcript", len(following))
		}
		if following[2].Content != "投影原文一" || following[3].Content != "投影原文二" {
			t.Fatalf("snapshot messages 2-3 = (%q, %q), want both projected entries again", following[2].Content, following[3].Content)
		}
		if following[4].Role != model.RoleUser || following[4].Content != "first" {
			t.Fatalf("snapshot message 4 = %+v, want the settled input of the previous drive", following[4])
		}
		if following[5].Role != model.RoleAssistant || following[5].Content != "settled" {
			t.Fatalf("snapshot message 5 = %+v, want the settled answer of the previous drive", following[5])
		}
		if following[6].Role != model.RoleUser || following[6].Content != "second" {
			t.Fatalf("snapshot message 6 = %+v, want the input of this drive", following[6])
		}
		view := snaps[0].Snapshot
		if view.Model != "model-a" || view.PromptVersion == "" {
			t.Fatalf("snapshot view = (%q, prompt %q), want the model name beside a fixed prompt version", view.Model, view.PromptVersion)
		}
		if view.PromptVersion != snaps[1].Snapshot.PromptVersion {
			t.Fatalf("prompt versions = (%q, %q), want the same fixed version across the two requests", view.PromptVersion, snaps[1].Snapshot.PromptVersion)
		}
		if !namedHas(view.VisibleTools, "echo-a") {
			t.Fatalf("visible tools = %+v, want the echo-a tool pinned on the view", view.VisibleTools)
		}
		if !namedHas(view.VisibleSkills, "greet") {
			t.Fatalf("visible skills = %+v, want the greet skill pinned on the view", view.VisibleSkills)
		}
		if snaps[0].Decision != firstTurn.Decisions[0].DecisionID || snaps[1].Decision != secondTurn.Decisions[0].DecisionID {
			t.Fatalf("snapshot decisions = (%s, %s), want the decisions whose requests they were fixed for", snaps[0].Decision, snaps[1].Decision)
		}
		if seen != 2 {
			t.Fatalf("model calls answered = %d, want two real formal requests driven", seen)
		}
	})

	t.Run("no URL credential reaches the kept snapshot while the request keeps its own", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		const secretUserinfo = "USERINFO-4c5d"
		const secretQuery = "QUERY-7e8f"
		original := "https://alice:" + secretUserinfo + "@cdn.example.com/img.png?api_key=" + secretQuery + "&size=2"
		provider := &learnModel{name: "model-a", answer: func(*model.Request) *model.Response {
			return learnFinalTurn("settled")
		}}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}
		run, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&learnTool{name: "echo-a", output: "out-a"}}},
		}, tagent.WithGovernance(formalPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		invokeSnapshotWith(t, ctx, run, "inv-snapshot-secret", "session-snapshot-secret", []model.Message{{
			Role:         model.RoleUser,
			ContentParts: []model.ContentPart{{Type: "image", Image: &trpcmodel.Image{URL: original, Format: "png"}}},
		}})
		snaps := run.RequestSnapshots()
		if len(snaps) != 1 {
			t.Fatalf("snapshots = %d, want exactly one fixed beside the single model request", len(snaps))
		}
		kept, err := rl.MarshalSnapshot(snaps[0].Snapshot)
		if err != nil {
			t.Fatalf("marshal the kept snapshot: %v", err)
		}
		if bytes.Contains(kept, []byte(secretUserinfo)) || bytes.Contains(kept, []byte(secretQuery)) {
			t.Fatalf("kept snapshot bytes = %s, want no request credential carried", kept)
		}
		if !bytes.Contains(kept, []byte(rl.RedactedValue)) {
			t.Fatalf("kept snapshot bytes = %s, want the URL credential projected to the redacted value", kept)
		}
		wantURL := "https://cdn.example.com/img.png?api_key=" + rl.RedactedValue + "&size=2"
		if got := snaps[0].Snapshot.Messages[0].ContentParts[0].Image.URL; got != wantURL {
			t.Fatalf("snapshot image URL = %q, want %q", got, wantURL)
		}
		requests := learnRequests(provider)
		if len(requests) != 1 {
			t.Fatalf("recorded requests = %d, want one real formal request driven", len(requests))
		}
		if got := requests[0].Messages[0].ContentParts[0].Image.URL; got != original {
			t.Fatalf("model-visible request URL = %q, want the original %q kept", got, original)
		}
	})

	t.Run("the snapshot belongs to its decision request and never to a tool result", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var run *tagent.Run
		round := 0
		provider := &learnModel{name: "model-a", answer: func(*model.Request) *model.Response {
			round++
			snaps := run.RequestSnapshots()
			if len(snaps) != round {
				t.Fatalf("snapshots at model request %d = %d, want one snapshot per model request and none for a tool result", round, len(snaps))
			}
			if round == 1 {
				return learnToolTurn(learnCall("call-1", "echo-a", `{"x":1}`))
			}
			return learnFinalTurn("settled")
		}}
		decl := &config.Declaration{
			Entry:  "agent-a",
			Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}},
		}
		assembled, err := tagent.Assemble(ctx, decl, map[event.AgentID]tagent.NativeAgent{
			"agent-a": {Model: provider, Tools: []trpctool.CallableTool{&learnTool{name: "echo-a", output: "out-a"}}},
		}, tagent.WithGovernance(formalPolicy{}))
		if err != nil {
			t.Fatalf("assemble the runtime: %v", err)
		}
		run = assembled
		turn := invokeSnapshot(t, ctx, run, "inv-snapshot-tool", "session-snapshot-tool", "drive the tool")
		if turn.Terminal != model.KindCompleted || len(turn.Decisions) != 2 {
			t.Fatalf("turn = (%v, %d decisions), want one completed terminal beside two decisions", turn.Terminal, len(turn.Decisions))
		}
		snaps := run.RequestSnapshots()
		if len(snaps) != 2 {
			t.Fatalf("snapshots = %d, want exactly one per model request beside the tool result that ran between them", len(snaps))
		}
		for i := range snaps {
			if snaps[i].Decision != turn.Decisions[i].DecisionID {
				t.Fatalf("snapshot %d decision = %s, want the decision whose request it was fixed for %s", i, snaps[i].Decision, turn.Decisions[i].DecisionID)
			}
		}
		earlier := mustSnapshotJSON(t, snaps[0].Snapshot)
		if bytes.Contains(earlier, []byte("out-a")) {
			t.Fatalf("first request snapshot = %s, want no body of a tool result that had not run yet", earlier)
		}
		later := mustSnapshotJSON(t, snaps[1].Snapshot.Messages)
		if !bytes.Contains(later, []byte("out-a")) {
			t.Fatalf("second request snapshot = %s, want the settled tool body its transcript actually carried", later)
		}
		requests := learnRequests(provider)
		if !bytes.Equal(later, mustSnapshotJSON(t, requests[1].Messages)) {
			t.Fatalf("second request snapshot = %s, want the fixed copy of the second model request", later)
		}
	})
}

// invokeSnapshot drives one invocation of the formal entry carrying the named input beside the episode boundary it names.
func invokeSnapshot(t *testing.T, ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID, input string) agent.Turn {
	t.Helper()
	return invokeSnapshotWith(t, ctx, run, invocation, session, []model.Message{{Role: model.RoleUser, Content: input}})
}

// invokeSnapshotWith drives one invocation of the formal entry carrying the given native input beside the episode boundary it names.
func invokeSnapshotWith(t *testing.T, ctx context.Context, run *tagent.Run, invocation event.InvocationID, session event.SessionID, input []model.Message) agent.Turn {
	t.Helper()
	turn, err := run.Invoke(ctx, tagent.InvokeRequest{
		InvocationID: invocation,
		Episode:      "ep-snapshot",
		Address:      event.Address{AgentID: "agent-a", SessionID: session},
		Input:        input,
	})
	if err != nil {
		t.Fatalf("invoke the formal runtime: %v", err)
	}
	return turn
}

// learnRequests reports a copy of the native requests the scripted provider was driven with.
func learnRequests(m *learnModel) []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

// namedHas reports whether the pinned view members carry the named member.
func namedHas(members []rl.VersionedName, name string) bool {
	for _, member := range members {
		if member.Name == name {
			return true
		}
	}
	return false
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

// writeSkillAt writes one skill folder with its SKILL.md under the given root, in the shape the framework skill repository scans and parses.
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

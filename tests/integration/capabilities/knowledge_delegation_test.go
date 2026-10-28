package capabilities

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/SpellingDragon/tagent/tool/subagent"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestKnowledgeDelegationRunsTheKnowledgeSurface runs the knowledge surface a delegated turn reaches:
// - the delegated turn's request carries the three knowledge tools and none of the caller's own
// - skill_search answers the deployment's skill repository for real
// - read_file answers the file inside the declared read root for real
// - the model stage consumes both results in its second decision
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestKnowledgeDelegationRunsTheKnowledgeSurface(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	writeCapabilitySkill(t, root, "deploy-runbook", "the deployment runbook", "run the deploy\n")
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatalf("create the notes directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "deploy.md"), []byte("deploy notes\n"), 0o600); err != nil {
		t.Fatalf("write the notes file: %v", err)
	}
	repository, err := skill.NewFSRepository(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatalf("open the skill repository: %v", err)
	}
	surface, err := knowledge.New(ctx, knowledge.Options{Skills: repository, ReadRoot: root})
	if err != nil {
		t.Fatalf("assemble the knowledge surface: %v", err)
	}

	probe := &delegationProbeTool{name: "caller_probe"}
	settled := make(chan struct{})
	knowledgeRounds := 0
	delegated := &ntModel{name: "model-knowledge", answer: func(req *model.Request) *model.Response {
		knowledgeRounds++
		if knowledgeRounds == 1 {
			return ntToolTurn(
				ntCall("call-search", knowledge.SkillSearchToolName, `{"query":"deploy"}`),
				ntCall("call-read", knowledge.ReadFileToolName, `{"file_name":"notes/deploy.md"}`),
			)
		}
		close(settled)
		return ntFinalTurn("the runbook lives in deploy-runbook and the notes are read")
	}}
	callerRounds := 0
	caller := &ntModel{name: "model-caller", answer: func(req *model.Request) *model.Response {
		callerRounds++
		if callerRounds == 1 {
			return ntToolTurn(ntCall("call-knowledge", "knowledge", `{"input":"how do we deploy?"}`))
		}
		return ntFinalTurn("the knowledge turn was admitted")
	}}
	decl := &config.Declaration{
		Entry: "caller",
		Agents: []config.AgentSpec{
			{
				ID:    "caller",
				Model: "model-caller",
				Tools: []config.ToolRef{{
					Name:   "knowledge",
					Params: map[string]string{subagent.ParamAgent: "knowledge"},
				}},
			},
			{
				ID:    "knowledge",
				Model: "model-knowledge",
				Tools: []config.ToolRef{
					{Name: knowledge.SkillSearchToolName},
					{Name: knowledge.SkillLoadToolName},
					{Name: knowledge.ReadFileToolName},
				},
			},
		},
	}
	run, err := assembly.Assemble(ctx, decl, map[event.AgentID]assembly.NativeAgent{
		"caller":    {Model: caller, Tools: []trpctool.CallableTool{probe}},
		"knowledge": {Model: delegated, Tools: surface},
	}, assembly.WithGovernance(passPolicy{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	addr := event.Address{AgentID: "caller", SessionID: "session-knowledge-delegation"}
	if _, err := run.Invoke(ctx, assembly.InvokeRequest{
		InvocationID: "inv-knowledge-delegation",
		Address:      addr,
		Input:        []model.Message{{Role: model.RoleUser, Content: "how do we deploy?"}},
	}); err != nil {
		t.Fatalf("invoke the caller: %v", err)
	}
	select {
	case <-settled:
	case <-time.After(10 * time.Second):
		t.Fatal("the delegated knowledge turn never reached its second decision")
	}

	requests := delegated.recorded()
	if len(requests) != 2 {
		t.Fatalf("delegated requests = %d, want the tool decision beside the answer it feeds", len(requests))
	}
	for _, name := range []string{knowledge.SkillSearchToolName, knowledge.SkillLoadToolName, knowledge.ReadFileToolName} {
		if requests[0].Tools[name] == nil {
			t.Fatalf("the delegated request carries no %q, want the knowledge agent's own surface: %v", name, requests[0].Tools)
		}
	}
	if requests[0].Tools[probe.name] != nil {
		t.Fatalf("the delegated request carries the caller's %q tool: the surface is the target's own declaration", probe.name)
	}

	search := findToolResult(requests[1].Messages, "call-search")
	if search == nil || !strings.Contains(search.Content, "deploy-runbook") {
		t.Fatalf("the second request carries no skill search answer: %+v", requests[1].Messages)
	}
	read := findToolResult(requests[1].Messages, "call-read")
	if read == nil || !strings.Contains(read.Content, "deploy notes") {
		t.Fatalf("the second request carries no read file answer: %+v", requests[1].Messages)
	}
	if got := probe.calls(); got != 0 {
		t.Fatalf("the caller's probe tool ran %d times, want none", got)
	}
}

// writeCapabilitySkill writes one skill folder under the given root.
func writeCapabilitySkill(t *testing.T, root, name, description, body string) {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory %s: %v", dir, err)
	}
	content := "---\nname: " + name + "\ndescription: \"" + description + "\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file %s: %v", filepath.Join(dir, skill.SkillFile), err)
	}
}

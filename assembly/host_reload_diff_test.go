// The declaration diff of a configuration reload: every declaration dimension one
// published generation moves is judged under its own configuration dimension, so a
// reader of the chain reads which part of the organization moved instead of one
// undifferentiated binding entry. The judgment is a pure diff of two declarations,
// so it is checked here apart from the publication and the refusal that decide
// whether a generation carries it.
package assembly

import (
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// TestConfigChangesOfJudgesTheDeclarationDimensions pins the dimensions one
// published reload files:
//   - an entry move is the organization entry dimension, owned by the entry the
//     declaration now names
//   - an agent the set gained or lost is the organization agents dimension, owned
//     by that agent
//   - a config-declared agent reference move is the organization assembly
//     dimension, and is never restated as a binding tool move
//   - a preset reference move, a prompt source move and an organization section
//     move keep their own dimensions
//   - a declaration that did not move judges nothing
//
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestConfigChangesOfJudgesTheDeclarationDimensions(t *testing.T) {
	entryMove := configChangesOf(
		diffDeclaration("user", diffAgent("user"), diffAgent("worker")),
		diffDeclaration("worker", diffAgent("user"), diffAgent("worker")),
	)
	if len(entryMove) != 1 {
		t.Fatalf("entry move changes = %v, want the one organization entry dimension", changeLabels(entryMove))
	}
	if got := entryMove[0].Dimension; got != event.ConfigChangeDimensionOrgEntry {
		t.Fatalf("entry move dimension = %q, want the organization entry dimension", got)
	}
	if got := entryMove[0].Agent; got != "worker" {
		t.Fatalf("entry move owner = %q, want the entry the declaration now names", got)
	}
	if body := entryMove[0].description; !strings.Contains(body, "user") || !strings.Contains(body, "worker") {
		t.Fatalf("entry move description = %q, want both entries named", body)
	}

	agentAdded := configChangesOf(
		diffDeclaration("user", diffAgent("user")),
		diffDeclaration("user", diffAgent("user"), diffAgent("worker")),
	)
	if len(agentAdded) != 1 || agentAdded[0].Dimension != event.ConfigChangeDimensionOrgAgents {
		t.Fatalf("agent added changes = %v, want the one organization agents dimension", changeLabels(agentAdded))
	}
	if got := agentAdded[0].Agent; got != "worker" {
		t.Fatalf("agent added owner = %q, want the declared agent that was added", got)
	}
	if body := agentAdded[0].description; !strings.Contains(body, "added") {
		t.Fatalf("agent added description = %q, want the addition named", body)
	}

	agentRemoved := configChangesOf(
		diffDeclaration("user", diffAgent("user"), diffAgent("worker")),
		diffDeclaration("user", diffAgent("user")),
	)
	if len(agentRemoved) != 1 || agentRemoved[0].Dimension != event.ConfigChangeDimensionOrgAgents {
		t.Fatalf("agent removed changes = %v, want the one organization agents dimension", changeLabels(agentRemoved))
	}
	if got := agentRemoved[0].Agent; got != "worker" {
		t.Fatalf("agent removed owner = %q, want the declared agent that was removed", got)
	}
	if body := agentRemoved[0].description; !strings.Contains(body, "removed") {
		t.Fatalf("agent removed description = %q, want the removal named", body)
	}

	t.Run("an agent reference move is the assembly dimension and never a binding move", func(t *testing.T) {
		for name, change := range map[string]struct {
			before config.ToolRef
			after  config.ToolRef
		}{
			"the description file moves": {
				before: diffAgentReference("worker", "worker.md"),
				after:  diffAgentReference("worker", "worker-b.md"),
			},
			"the declared target moves": {
				before: diffAgentReference("worker", "worker.md"),
				after:  diffAgentReference("reviewer", "worker.md"),
			},
		} {
			previous := diffDeclaration("user", diffCaller(change.before))
			next := diffDeclaration("user", diffCaller(change.after))
			changes := configChangesOf(previous, next)
			if len(changes) != 1 || changes[0].Dimension != event.ConfigChangeDimensionOrgAssembly {
				t.Fatalf("%s: changes = %v, want the one organization assembly dimension", name, changeLabels(changes))
			}
			if got := changes[0].Agent; got != "user" {
				t.Fatalf("%s: assembly owner = %q, want the declaring agent", name, got)
			}
			if body := changes[0].description; !strings.Contains(body, "delegation assembly changed") {
				t.Fatalf("%s: assembly description = %q, want the moved assembly named", name, body)
			}
			for _, other := range changes {
				if other.Dimension == event.ConfigChangeDimensionBinding {
					t.Fatalf("%s: changes = %v, want the agent reference left out of the binding dimension", name, changeLabels(changes))
				}
			}
		}
	})

	t.Run("a preset reference move keeps the binding dimension", func(t *testing.T) {
		previous := diffDeclaration("user", config.AgentSpec{
			ID:    "user",
			Tools: []config.ToolRef{{Name: "knowledge_skill_search"}},
		})
		next := diffDeclaration("user", config.AgentSpec{
			ID:    "user",
			Tools: []config.ToolRef{{Name: "recall_query"}},
		})
		changes := configChangesOf(previous, next)
		if len(changes) != 1 || changes[0].Dimension != event.ConfigChangeDimensionBinding {
			t.Fatalf("preset move changes = %v, want the one binding dimension", changeLabels(changes))
		}
		if body := changes[0].description; !strings.Contains(body, "binding changed: tools") {
			t.Fatalf("preset move description = %q, want the tool move named as a binding diff", body)
		}
	})

	t.Run("a prompt source move keeps the prompt dimension", func(t *testing.T) {
		previous := diffDeclaration("user", config.AgentSpec{
			ID:    "user",
			Files: []config.FileRef{{Path: "boot.md", Kind: config.FileKindPrompt}},
		})
		next := diffDeclaration("user", config.AgentSpec{
			ID:    "user",
			Files: []config.FileRef{{Path: "next.md", Kind: config.FileKindPrompt}},
		})
		changes := configChangesOf(previous, next)
		if len(changes) != 1 || changes[0].Dimension != event.ConfigChangeDimensionPrompt {
			t.Fatalf("prompt move changes = %v, want the one prompt dimension", changeLabels(changes))
		}
	})

	t.Run("an organization section move keeps the binding dimension of the entry", func(t *testing.T) {
		previous := diffDeclaration("user", diffAgent("user"))
		next := diffDeclaration("user", diffAgent("user"))
		next.Servers = map[string]config.MCPServerSpec{"docs": {Transport: "stdio"}}
		changes := configChangesOf(previous, next)
		if len(changes) != 1 || changes[0].Dimension != event.ConfigChangeDimensionBinding {
			t.Fatalf("organization move changes = %v, want the one binding dimension", changeLabels(changes))
		}
		if got := changes[0].Agent; got != "user" {
			t.Fatalf("organization move owner = %q, want the entry agent", got)
		}
		if body := changes[0].description; !strings.Contains(body, "organization binding changed: mcp_servers") {
			t.Fatalf("organization move description = %q, want the moved section named", body)
		}
	})

	if changes := configChangesOf(diffDeclaration("user", diffAgent("user")), diffDeclaration("user", diffAgent("user"))); len(changes) != 0 {
		t.Fatalf("unchanged declaration changes = %v, want none", changeLabels(changes))
	}
}

// diffDeclaration builds one declaration over the given entry and agents.
func diffDeclaration(entry string, agents ...config.AgentSpec) *config.Declaration {
	return &config.Declaration{Entry: entry, Agents: agents}
}

// diffAgent builds one bare declared agent.
func diffAgent(id string) config.AgentSpec {
	return config.AgentSpec{ID: id}
}

// diffCaller builds one declared agent carrying the given tool reference.
func diffCaller(refs ...config.ToolRef) config.AgentSpec {
	return config.AgentSpec{ID: "user", Tools: refs}
}

// diffAgentReference builds one config-declared agent reference: the declared target,
// the tool name it is reached by and the description file it reads.
func diffAgentReference(target, file string) config.ToolRef {
	return config.ToolRef{Name: target, Params: map[string]string{
		subagent.ParamAgent:           target,
		subagent.ParamDescriptionFile: file,
	}}
}

// changeLabels renders the judged changes as dimension and owner pairs, the shape
// a failing check reads.
func changeLabels(changes []hostReloadConfigChange) []string {
	out := make([]string, 0, len(changes))
	for _, change := range changes {
		out = append(out, change.Dimension+"@"+change.Agent)
	}
	return out
}

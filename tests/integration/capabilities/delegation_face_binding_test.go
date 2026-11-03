package capabilities

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/tool/knowledge"
	"github.com/SpellingDragon/tagent/tool/subagent"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestDelegatedBindingCarriesTheDeclaredSkills reads the seam a delegated turn reads a declared skill face through:
//   - the binding a sub-agent declaring skills binds with carries a tool-set face
//   - that face offers the skill load tool the request assembly publishes
//   - a load fixes the body on the binding's resources, which the next decision reads
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestDelegatedBindingCarriesTheDeclaredSkills(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeCapabilitySkill(t, root, "deploy-runbook", "the deployment runbook", "run the deploy\n")
	roots := filepath.Join(root, "skills")
	builder := delegatedFaceBuilder(t, roots)
	bound, err := builder.Bind(ctx, delegatedSkillAssembly(roots))
	if err != nil {
		t.Fatalf("bind the sub-agent declaring skills: %v", err)
	}
	resources, ok := bound.Set.(*state.ToolResources)
	if !ok {
		t.Fatalf("the delegated binding carries no tool-set face: %T", bound.Set)
	}
	face, order := state.ToolFace(bound)
	load := face[knowledge.SkillLoadToolName]
	if load == nil {
		t.Fatalf("the delegated request offers no %q, want the declared skill face: %v",
			knowledge.SkillLoadToolName, order)
	}
	callable, ok := load.(trpctool.CallableTool)
	if !ok {
		t.Fatalf("the offered %q is not callable: %T", knowledge.SkillLoadToolName, load)
	}
	frame := task.WithCallFrame(ctx, task.CallFrame{
		Identity: task.CallIdentity{DecisionID: "decision-knowledge-1"},
		Address:  event.Address{AgentID: "knowledge", SessionID: "session-delegated-face"},
	})
	if _, err := callable.Call(frame, []byte(`{"skill":"deploy-runbook"}`)); err != nil {
		t.Fatalf("load the declared skill: %v", err)
	}
	loads := resources.SkillLoads()
	if len(loads) != 1 {
		t.Fatalf("the binding fixed %d skill bodies, want the one it loaded: %+v", len(loads), loads)
	}
	if loads[0].Skill != "deploy-runbook" || !strings.Contains(loads[0].Body, "run the deploy") {
		t.Fatalf("the binding fixed %+v, want the body of the skill it loaded", loads[0])
	}
	if loads[0].Agent != "knowledge" {
		t.Fatalf("the fixed load names agent %q, want the site the load ran on", loads[0].Agent)
	}
}

// TestDelegatedBindingRefusesADeclaredSkillBesideNoResolver keeps the refusal of a declared skill face named.
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestDelegatedBindingRefusesADeclaredSkillBesideNoResolver(t *testing.T) {
	ctx := context.Background()
	builder := subagent.Builder{Model: func(context.Context, string) (model.Model, error) {
		return &ntModel{name: "model-knowledge"}, nil
	}}
	if _, err := builder.Bind(ctx, delegatedSkillAssembly("/tmp/declared-skills")); !errors.Is(err, subagent.ErrResolverMissing) {
		t.Fatalf("bind a declared skill beside no resolver = %v, want the named refusal", err)
	}
}

// delegatedSkillAssembly is one sub-agent assembly declaring the skills of the
// repository under the given roots.
func delegatedSkillAssembly(roots string) *subagent.Assembly {
	return &subagent.Assembly{Definition: subagent.Definition{
		Name:   "knowledge",
		Model:  "model-knowledge",
		Skills: []string{roots},
	}}
}

// delegatedFaceBuilder is the builder one delegated binding resolves the
// declared skills through, over the scripted model the binding names.
func delegatedFaceBuilder(t *testing.T, roots string) subagent.Builder {
	t.Helper()
	return subagent.Builder{
		Model: func(context.Context, string) (model.Model, error) {
			return &ntModel{name: "model-knowledge"}, nil
		},
		Skills: func(context.Context, []string) (skill.Repository, error) {
			return skill.NewFSRepository(roots)
		},
	}
}

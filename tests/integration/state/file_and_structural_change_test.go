package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SpellingDragon/tagent/api/config"
	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"

	orgstate "github.com/SpellingDragon/tagent/runtime/state"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// gateProbe is the deterministic stand-in of the real governance policy beside
// the consultation count: the fixed answers it carries are mechanism inputs
// with no quality conclusion, and the count shows which face reached a gate at
// all.
type gateProbe struct {
	answers map[orgstate.CheckName]orgstate.CheckAnswer
	asked   int
}

// Check implements Governance beside the consultation it counts.
func (p *gateProbe) Check(ctx context.Context, plan orgstate.StatePlan) (map[orgstate.CheckName]orgstate.CheckAnswer, error) {
	p.asked++
	return p.answers, nil
}

// allPassing reports the passing disposition of every required check.
func allPassing() map[orgstate.CheckName]orgstate.CheckAnswer {
	return map[orgstate.CheckName]orgstate.CheckAnswer{
		"eval":       passing("eval"),
		"benchmark":  passing("benchmark"),
		"apply-gate": passing("apply-gate"),
	}
}

// writePromptFile writes one prompt file under the source root.
func writePromptFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the prompt directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write the prompt file: %v", err)
	}
}

// writeSkillFile writes one skill folder with its SKILL.md under the skill
// root, in the shape the framework skill repository scans and parses.
func writeSkillFile(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create the skill directory: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: \"greet the user\"\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, skill.SkillFile), []byte(content), 0o644); err != nil {
		t.Fatalf("write the skill file: %v", err)
	}
}

// fileCandidate prepares the candidate of one declared agent beside the
// structural file binding and the candidate-exclusive resources the caller
// registered.
func fileCandidate(t *testing.T, baseline, fingerprint string, m model.Model, tools []trpctool.Tool, plugins []plugin.Instance, files []config.FileRef, leases *orgstate.Leases) orgstate.Candidate {
	t.Helper()
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{{ID: "agent-a", Model: "model-a"}}}
	plan := orgstate.OrgPlan{Declaration: decl, Baseline: baseline, Fingerprint: fingerprint, NextPrompt: "prompt-a"}
	built, err := orgstate.NewStatePlan(plan, map[event.AgentID]orgstate.AgentBinding{
		"agent-a": {
			Model:        m,
			Tools:        tools,
			Plugins:      plugins,
			PromptSource: orgstate.PromptSource{Files: files},
		},
	}, leases.Refs())
	if err != nil {
		t.Fatalf("instantiate the candidate plan: %v", err)
	}
	return orgstate.Candidate{Plan: built, Resources: leases}
}

// fileRig is the rig one sub-case drives: the file source a request fixes its
// content through and the sole publication entry a structural change commits
// through, beside the binding holder a request borrows from.
type fileRig struct {
	source     *orgstate.FileSource
	pub        *orgstate.Publisher
	holder     *orgstate.Bindings
	probe      *gateProbe
	records    *recordSink
	files      []config.FileRef
	root       string
	skillsRoot string
}

// newFileRig builds the rig over a fresh source root and skill repository: one
// prompt file and one skill file in the structural binding of the declared
// agent.
func newFileRig(t *testing.T) *fileRig {
	t.Helper()
	root := t.TempDir()
	skillsRoot := t.TempDir()
	writePromptFile(t, root, "prompts/a.md", "prompt v1\n")
	writeSkillFile(t, skillsRoot, "greet", "body v1\n")
	repo, err := skill.NewFSRepository(skillsRoot)
	if err != nil {
		t.Fatalf("open the framework skill repository: %v", err)
	}
	files := []config.FileRef{
		{Path: "prompts/a.md", Kind: config.FileKindPrompt},
		{Path: filepath.Join("greet", skill.SkillFile), Kind: config.FileKindSkill},
	}
	probe := &gateProbe{answers: allPassing()}
	records := &recordSink{}
	holder := orgstate.NewBindings()
	pub := orgstate.NewPublisher(holder, orgstate.PublishContract{
		Required: []orgstate.CheckName{"eval", "benchmark", "apply-gate"},
	}, probe, records)
	return &fileRig{
		source:     orgstate.NewFileSource(root, repo),
		pub:        pub,
		holder:     holder,
		probe:      probe,
		records:    records,
		files:      files,
		root:       root,
		skillsRoot: skillsRoot,
	}
}

// TestFileAndStructuralChange checks how the file face and the structural face of a configuration change stay on their own paths:
// - a prompt or skill file rewritten between two requests is read and fixed anew at the next request boundary while the structural generation stays where it is
// - a replayed historical snapshot never pins the content a later request reads by source
// - a structural change commits only through the publish precondition gate and a refused one leaves the effective binding as it was
// - a request under way keeps one generation of model, tools and plugins while the file content it reads keeps updating
//
// The semantics the deleted routing face used to carry are anchored where they
// now live: the host reload refuses a structural move by name
// (tests/integration/host/reload_tiers_test.go), the sole publication entry
// rolls back as one more generation (TestGenerationPublication), and the strict
// decoder keeps the source and normalized line of a refused document
// (TestDecodeStrictRefusesKeysOutsideTheCallerSchema).
// 契约: docs/wiki/platform/org-hot-reload.md#generations
func TestFileAndStructuralChange(t *testing.T) {
	t.Run("a file change is read at the next request boundary while the structural generation stays", func(t *testing.T) {
		ctx := context.Background()
		rig := newFileRig(t)
		leases := orgstate.NewLeases()
		cand := fileCandidate(t, rig.pub.Baseline(), "fp-1", &stubModel{name: "gen-1"}, nil, nil, rig.files, leases)
		out, err := rig.pub.Publish(ctx, cand)
		if err != nil || out.Result != orgstate.PublishApplied || out.Publication.Generation != 1 {
			t.Fatalf("publish the structural generation = (%+v, %v), want one applied generation", out, err)
		}
		if rig.probe.asked != 1 {
			t.Fatalf("gate consultations = %d, want the structural publication to pass the precondition gate exactly once", rig.probe.asked)
		}

		snap1, err := rig.source.Fixate(ctx, orgstate.PromptSource{Files: rig.files})
		if err != nil {
			t.Fatalf("fixate the file content at the request boundary: %v", err)
		}
		if len(snap1.Files) != 2 || snap1.Files[0].Content != "prompt v1\n" {
			t.Fatalf("fixed prompt content = %+v, want the prompt body read by source", snap1.Files)
		}
		skillBody := snap1.Files[1].Content
		if !strings.Contains(skillBody, "body v1") || strings.Contains(skillBody, "description:") {
			t.Fatalf("fixed skill content = %q, want the body the framework repository parsed without its front matter", skillBody)
		}
		if snap1.Version == "" {
			t.Fatalf("fixed version = %q, want the content version of what this request fixed", snap1.Version)
		}
		if rig.probe.asked != 1 || len(rig.records.records) != 1 {
			t.Fatalf("gate consultations = %d beside %d publication records, want the file face to reach neither the register nor the gate", rig.probe.asked, len(rig.records.records))
		}

		writePromptFile(t, rig.root, "prompts/a.md", "prompt v2\n")
		writeSkillFile(t, rig.skillsRoot, "greet", "body v2\n")

		snap2, err := rig.source.Fixate(ctx, orgstate.PromptSource{Files: rig.files})
		if err != nil {
			t.Fatalf("fixate the rewritten content at the next request boundary: %v", err)
		}
		if snap2.Files[0].Content != "prompt v2\n" || !strings.Contains(snap2.Files[1].Content, "body v2") {
			t.Fatalf("fixed contents = %+v, want the rewritten bodies read by source", snap2.Files)
		}
		if snap2.Version == snap1.Version {
			t.Fatalf("fixed versions = (%s, %s), want a new content version beside the rewritten bodies", snap1.Version, snap2.Version)
		}
		if got := rig.pub.Current(); got.Generation != 1 || got.Fingerprint != "fp-1" {
			t.Fatalf("structural generation = %+v, want the file change to leave the generation where it was", got)
		}
		if snap1.Files[0].Content != "prompt v1\n" {
			t.Fatalf("earlier fixed content = %q, want the version one request fixed kept as it was", snap1.Files[0].Content)
		}
	})

	t.Run("a replayed historical snapshot never pins the content of a later request", func(t *testing.T) {
		ctx := context.Background()
		rig := newFileRig(t)
		snap1, err := rig.source.Fixate(ctx, orgstate.PromptSource{Files: rig.files})
		if err != nil {
			t.Fatalf("fixate the file content at the request boundary: %v", err)
		}
		historical := orgstate.PromptSource{Files: rig.files, Version: snap1.Version}

		writePromptFile(t, rig.root, "prompts/a.md", "prompt v2\n")

		replayed, err := rig.source.Fixate(ctx, historical)
		if err != nil {
			t.Fatalf("fixate beside the replayed historical snapshot: %v", err)
		}
		if replayed.Files[0].Content != "prompt v2\n" {
			t.Fatalf("content beside the replay = %q, want the current file content read by source", replayed.Files[0].Content)
		}
		if replayed.Version == historical.Version {
			t.Fatalf("fixed version = %s, want a version other than the one the historical snapshot carries", replayed.Version)
		}
		if historical.Version != snap1.Version || snap1.Files[0].Content != "prompt v1\n" {
			t.Fatalf("historical snapshot = (%s, %q), want the replayed snapshot kept as the version it recorded", historical.Version, snap1.Files[0].Content)
		}
	})

	t.Run("a structural change commits only through the precondition gate", func(t *testing.T) {
		ctx := context.Background()
		rig := newFileRig(t)
		leases := orgstate.NewLeases()
		cand := fileCandidate(t, rig.pub.Baseline(), "fp-1", &stubModel{name: "gen-1"}, nil, nil, rig.files, leases)
		out, err := rig.pub.Publish(ctx, cand)
		if err != nil || out.Publication.Generation != 1 {
			t.Fatalf("commit the structural change = (%+v, %v), want it admitted through the gate", out, err)
		}
		if rig.probe.asked != 1 {
			t.Fatalf("gate consultations = %d, want the structural change to pass the precondition gate", rig.probe.asked)
		}

		rig.probe.answers = map[orgstate.CheckName]orgstate.CheckAnswer{
			"eval":       {Name: "eval", Verdict: orgstate.CheckReject, Reason: "the tool declaration is not admissible", Scope: "org"},
			"benchmark":  passing("benchmark"),
			"apply-gate": passing("apply-gate"),
		}
		refused := fileCandidate(t, rig.pub.Baseline(), "fp-2", &stubModel{name: "gen-2"}, nil, nil, rig.files, orgstate.NewLeases())
		if _, err := rig.pub.Publish(ctx, refused); !errors.Is(err, orgstate.ErrCheckRefused) {
			t.Fatalf("commit beside a refusing check = %v, want %v beside the refusal that names the check", err, orgstate.ErrCheckRefused)
		}
		if got := rig.pub.Current(); got.Generation != 1 || got.Fingerprint != "fp-1" {
			t.Fatalf("structural generation = %+v, want the effective binding kept as it was beside the refusal", got)
		}
		if len(rig.records.records) != 1 {
			t.Fatalf("publication records = %d, want no record beside the refused structural change", len(rig.records.records))
		}
		handle, err := rig.holder.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow beside the refusal: %v", err)
		}
		defer handle.Release(ctx)
		if handle.Binding.Model.Info().Name != "gen-1" {
			t.Fatalf("borrowed model = %s, want the model of the binding still in effect", handle.Binding.Model.Info().Name)
		}
	})

	t.Run("an in-flight request keeps one generation of model tools and plugins while the file updates", func(t *testing.T) {
		ctx := context.Background()
		rig := newFileRig(t)
		tools := []trpctool.Tool{&stubTool{name: "tool-1"}}
		pipeline := []plugin.Instance{
			&stubPlugin{name: "enrich", port: plugin.PortCallTransform},
			&stubPlugin{name: "risk", port: plugin.PortCallRuling},
		}
		first := fileCandidate(t, rig.pub.Baseline(), "fp-1", &stubModel{name: "gen-1"}, tools, pipeline, rig.files, orgstate.NewLeases())
		if _, err := rig.pub.Publish(ctx, first); err != nil {
			t.Fatalf("publish the first generation: %v", err)
		}
		handle, err := rig.holder.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as the request begins executing: %v", err)
		}
		defer handle.Release(ctx)

		second := fileCandidate(t, rig.pub.Baseline(), "fp-2", &stubModel{name: "gen-2"},
			[]trpctool.Tool{&stubTool{name: "tool-2"}},
			[]plugin.Instance{&stubPlugin{name: "present", port: plugin.PortFactView}},
			rig.files, orgstate.NewLeases())
		if _, err := rig.pub.Publish(ctx, second); err != nil {
			t.Fatalf("publish the second generation beside the in-flight request: %v", err)
		}

		if handle.Binding.Model.Info().Name != "gen-1" {
			t.Fatalf("in-flight model = %s, want the model of the generation the request took", handle.Binding.Model.Info().Name)
		}
		if len(handle.Binding.Tools) != 1 || handle.Binding.Tools[0].Declaration().Name != "tool-1" {
			t.Fatalf("in-flight tools = %+v, want the tools of the same generation as its model", handle.Binding.Tools)
		}
		if len(handle.Binding.Plugins) != 2 || handle.Binding.Plugins[0].Name() != "enrich" || handle.Binding.Plugins[1].Name() != "risk" {
			t.Fatalf("in-flight plugins = %+v, want the ordered pipeline of the same generation, never spliced mid-request", handle.Binding.Plugins)
		}

		writePromptFile(t, rig.root, "prompts/a.md", "prompt v2\n")
		snap, err := rig.source.Fixate(ctx, handle.Binding.PromptSource)
		if err != nil {
			t.Fatalf("fixate the file content beside the in-flight request: %v", err)
		}
		if snap.Files[0].Content != "prompt v2\n" {
			t.Fatalf("fixed content = %q, want the file content free to update beside a pinned generation", snap.Files[0].Content)
		}

		fresh, err := rig.holder.Borrow(ctx, "agent-a")
		if err != nil {
			t.Fatalf("borrow as a later request begins executing: %v", err)
		}
		defer fresh.Release(ctx)
		if fresh.Binding.Model.Info().Name != "gen-2" {
			t.Fatalf("later model = %s, want the newer generation taken at the request boundary", fresh.Binding.Model.Info().Name)
		}
		later, err := rig.source.Fixate(ctx, fresh.Binding.PromptSource)
		if err != nil {
			t.Fatalf("fixate for the later request: %v", err)
		}
		if later.Files[0].Content != "prompt v2\n" {
			t.Fatalf("later fixed content = %q, want the same current file content read by source", later.Files[0].Content)
		}
	})
}

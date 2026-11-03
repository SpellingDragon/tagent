// The delegated assembly of one agent's prompt: the instruction a target tool or a
// generic delegated declaration inherits is read through the file source of the
// agent the prompt files belong to, so the text is the content the file change
// fact carries instead of a second read beside the chain.
package assembly

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
	"github.com/SpellingDragon/tagent/tool/subagent"
)

// TestDelegatedAssemblyReadsItsInstructionThroughTheFileFact pins the preset read of a delegated assembly through the file change fact.
//
// 契约: docs/wiki/agent/event-flow.md#event-pipeline-atomic
func TestDelegatedAssemblyReadsItsInstructionThroughTheFileFact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const (
		bootBody = "PRESET-FACT-BOOT-INSTRUCTION"
		nextBody = "PRESET-FACT-NEXT-INSTRUCTION"
	)
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "system.md")
	writePresetPrompt(t, promptPath, bootBody+"\n")

	spec := config.AgentSpec{
		ID:    "agent-a",
		Model: "model-a",
		Files: []config.FileRef{{Path: promptPath, Kind: config.FileKindPrompt}},
		Tools: []config.ToolRef{{Name: subagent.CallSubagentRef}},
	}
	decl := &config.Declaration{Entry: "agent-a", Agents: []config.AgentSpec{spec}}
	run, err := Assemble(ctx, decl, map[event.AgentID]NativeAgent{
		"agent-a": {Model: &snapshotTestModel{answer: func(*model.Request) *model.Response {
			return &model.Response{
				Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "settled"}}},
				Done:    true,
			}
		}}},
	}, WithGovernance(snapshotTestGovernance{}))
	if err != nil {
		t.Fatalf("assemble the runtime: %v", err)
	}

	filed := presetFileFact(t, ctx, run, promptPath, bootBody+"\n")
	if filed.Type != event.TypeConfigChange {
		t.Fatalf("preset file fact type = %q, want the configuration change family", filed.Type)
	}
	if got := filed.Meta[event.MetaKeyConfigChangeDimension]; got != event.ConfigChangeDimensionFile {
		t.Fatalf("preset file fact dimension = %q, want the file dimension", got)
	}
	if filed.Owner.AgentID != "agent-a" {
		t.Fatalf("preset file fact owner = %q, want the agent whose prompt files carry the instruction", filed.Owner.AgentID)
	}

	assembly, err := run.presetAssembly(ctx, "agent-a", spec, nil)
	if err != nil {
		t.Fatalf("resolve the delegated assembly: %v", err)
	}
	if assembly.Instruction != bootBody {
		t.Fatalf("delegated instruction = %q, want the %q the file fact carries", assembly.Instruction, bootBody)
	}
	if after := run.CommittedFacts(); after != 1 {
		t.Fatalf("committed facts = %d after re-reading the same content, want the one fact the content names", after)
	}

	writePresetPrompt(t, promptPath, nextBody+"\n")
	assembly, err = run.presetAssembly(ctx, "agent-a", spec, nil)
	if err != nil {
		t.Fatalf("resolve the delegated assembly of the rewritten prompt: %v", err)
	}
	if assembly.Instruction != nextBody {
		t.Fatalf("delegated instruction beside the rewrite = %q, want the rewritten body %q", assembly.Instruction, nextBody)
	}
	if after := run.CommittedFacts(); after != 2 {
		t.Fatalf("committed facts = %d after the rewrite, want one fact per content version the assembly read", after)
	}
	rewritten := presetFileFact(t, ctx, run, promptPath, nextBody+"\n")
	if body := rewritten.Message.Content; !strings.Contains(body, nextBody) {
		t.Fatalf("rewritten preset file fact body = %q, want the rewritten body", body)
	}
	if earlier, err := run.CommittedEvent(ctx, presetFileFactID(promptPath, bootBody+"\n")); err != nil {
		t.Fatalf("read the earlier preset file fact back: %v", err)
	} else if earlier.Message.Content != filed.Message.Content {
		t.Fatalf("earlier preset file fact body = %q, want the content it filed, %q", earlier.Message.Content, filed.Message.Content)
	}
}

// writePresetPrompt writes one prompt body under the given path.
func writePresetPrompt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create the prompt directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write the prompt file %s: %v", path, err)
	}
}

// presetFileFact reports the file change fact one preset read of the prompt files
// files: the identity is the one the boundary derives for the content, so a check
// reads the committed fact by it instead of scanning the chain.
func presetFileFact(t *testing.T, ctx context.Context, run *Run, path, content string) event.Event {
	t.Helper()
	fact, err := run.CommittedEvent(ctx, presetFileFactID(path, content))
	if err != nil {
		t.Fatalf("read the preset file fact of %s: %v", path, err)
	}
	return fact
}

// presetFileFactID derives the identity one preset read files for the content:
// the file reference the boundary reads the content under beside the content
// itself, exactly as the request boundary derives the file dimension identity.
func presetFileFactID(path, content string) event.EventID {
	ref := config.FileRef{Path: filepath.Base(filepath.FromSlash(path)), Kind: config.FileKindPrompt}
	return event.ConfigChangeFor("agent-a", event.ConfigChangeDimensionFile, fileFactGeneration,
		fileFactHeader(ref, content)+"\n"+content).ID
}

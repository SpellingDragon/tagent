// The knowledge surface over the deployment's own faces: the skill repository it
// holds and the root its reads are confined to.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/skill"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestNewSurface keeps the assembled surface shaped the way the declaration and the prompt name it:
// - a deployment wiring both faces gets the three tools in the fixed order
// - a deployment wiring no skill repository gets no skill tool
// - a deployment wiring no read root gets no read tool
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestNewSurface(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	repository := knowledgeRepository(t, dir)

	t.Run("both faces wired", func(t *testing.T) {
		surface, err := New(ctx, Options{Skills: repository, ReadRoot: dir})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		got := make([]string, 0, len(surface))
		for _, tool := range surface {
			got = append(got, tool.Declaration().Name)
		}
		want := []string{SkillSearchToolName, SkillLoadToolName, ReadFileToolName}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("surface = %v, want %v", got, want)
		}
	})

	t.Run("no skill repository", func(t *testing.T) {
		surface, err := New(ctx, Options{ReadRoot: dir})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		for _, tool := range surface {
			if name := tool.Declaration().Name; name == SkillSearchToolName || name == SkillLoadToolName {
				t.Fatalf("surface carries %q beside no skill repository", name)
			}
		}
	})

	t.Run("no read root", func(t *testing.T) {
		surface, err := New(ctx, Options{Skills: repository})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		for _, tool := range surface {
			if name := tool.Declaration().Name; name == ReadFileToolName {
				t.Fatalf("surface carries %q beside no read root", name)
			}
		}
	})

	t.Run("neither face", func(t *testing.T) {
		surface, err := New(ctx, Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if len(surface) != 0 {
			t.Fatalf("surface = %d tools, want none beside no face", len(surface))
		}
	})
}

// TestSkillSearch answers the shortlist the repository holds:
// - a query matches the skill names and descriptions case-insensitively
// - an empty query lists the repository in its own order
// - the page bound defaults and clamps
// - a surface assembled beside no repository refuses by name
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestSkillSearch(t *testing.T) {
	ctx := context.Background()
	surface := knowledgeSurface(t, ctx)
	search := surface[SkillSearchToolName]

	t.Run("a query matches name and description", func(t *testing.T) {
		answer := searchAnswerOf(t, search, `{"query":"DEPLOY"}`)
		if answer.Count != 1 || len(answer.Skills) != 1 || answer.Skills[0].Name != "deploy-runbook" {
			t.Fatalf("answer = %+v, want the deploy runbook the description matches", answer)
		}
		if answer.Tool != SkillSearchToolName {
			t.Fatalf("answer tool = %q, want %q", answer.Tool, SkillSearchToolName)
		}
	})

	t.Run("an empty query lists the repository", func(t *testing.T) {
		answer := searchAnswerOf(t, search, `{}`)
		if answer.Count != 2 {
			t.Fatalf("answer = %+v, want both skills of the repository", answer)
		}
	})

	t.Run("the page bound clamps", func(t *testing.T) {
		answer := searchAnswerOf(t, search, `{"limit":1}`)
		if answer.Count != 1 {
			t.Fatalf("answer = %+v, want the one skill the page bound names", answer)
		}
		answer = searchAnswerOf(t, search, `{"limit":500}`)
		if answer.Count != 2 {
			t.Fatalf("answer = %+v, want the ceiling to carry every match instead of refusing the call", answer)
		}
	})

	t.Run("a surface beside no repository refuses by name", func(t *testing.T) {
		_, err := (&skillSearchTool{}).Call(ctx, []byte(`{}`))
		if !errors.Is(err, ErrSkillsMissing) {
			t.Fatalf("search beside no repository = %v, want the named refusal", err)
		}
	})
}

// TestSkillLoad puts one skill's body in context:
// - the load answers the body of the skill the name selects
// - a name the repository does not hold is refused
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestSkillLoad(t *testing.T) {
	ctx := context.Background()
	load := knowledgeSurface(t, ctx)[SkillLoadToolName]

	answer, err := load.Call(ctx, []byte(`{"skill":"deploy-runbook"}`))
	if err != nil {
		t.Fatalf("skill_load(deploy-runbook): %v", err)
	}
	if text := renderAnswer(t, answer); !strings.Contains(text, "loaded: deploy-runbook") {
		t.Fatalf("skill_load answered %q, want the loaded skill named back", text)
	}

	if _, err := load.Call(ctx, []byte(`{"skill":"no-such-skill"}`)); err == nil {
		t.Fatal("skill_load of a name the repository does not hold answered without a refusal")
	}
}

// TestReadFile reads inside the declared root:
// - a relative path inside the root answers the file's text
// - a surface assembled beside no root refuses by name
//
// 契约: docs/wiki/tool/tool-architecture.md#recall-subtools
func TestReadFile(t *testing.T) {
	ctx := context.Background()
	read := knowledgeSurface(t, ctx)[ReadFileToolName]

	answer, err := read.Call(ctx, []byte(`{"file_name":"notes/deploy.md"}`))
	if err != nil {
		t.Fatalf("read_file(notes/deploy.md): %v", err)
	}
	if text := renderAnswer(t, answer); !strings.Contains(text, "deploy notes") {
		t.Fatalf("read_file answered %q, want the file's text", text)
	}
}

// knowledgeSurface assembles the three-tool surface over a temporary deployment:
// two skills and one readable file.
func knowledgeSurface(t *testing.T, ctx context.Context) map[string]trpctool.CallableTool {
	t.Helper()
	dir := t.TempDir()
	repository := knowledgeRepository(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatalf("create the notes directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes", "deploy.md"), []byte("deploy notes\n"), 0o600); err != nil {
		t.Fatalf("write the notes file: %v", err)
	}
	surface, err := New(ctx, Options{Skills: repository, ReadRoot: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	out := make(map[string]trpctool.CallableTool, len(surface))
	for _, tool := range surface {
		out[tool.Declaration().Name] = tool
	}
	return out
}

// knowledgeRepository writes two skills under the given root and returns the
// repository over them.
func knowledgeRepository(t *testing.T, root string) skill.Repository {
	t.Helper()
	writeKnowledgeSkill(t, root, "deploy-runbook", "the deployment runbook", "run the deploy\n")
	writeKnowledgeSkill(t, root, "greet", "greet the user", "say hello\n")
	repository, err := skill.NewFSRepository(filepath.Join(root, "skills"))
	if err != nil {
		t.Fatalf("open the skill repository: %v", err)
	}
	return repository
}

// writeKnowledgeSkill writes one skill folder under the root.
func writeKnowledgeSkill(t *testing.T, root, name, description, body string) {
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

// searchAnswerOf runs one search call and decodes its answer.
func searchAnswerOf(t *testing.T, tool trpctool.CallableTool, args string) searchAnswer {
	t.Helper()
	answer, err := tool.Call(context.Background(), []byte(args))
	if err != nil {
		t.Fatalf("skill_search(%s): %v", args, err)
	}
	text := renderAnswer(t, answer)
	var decoded searchAnswer
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("skill_search(%s) answered unparsable JSON %q: %v", args, text, err)
	}
	return decoded
}

// renderAnswer reports the text one tool answered with: the string it returned,
// or the JSON rendering of the structured answer the tRPC tools hand back.
func renderAnswer(t *testing.T, answer any) string {
	t.Helper()
	if text, ok := answer.(string); ok {
		return text
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatalf("encode the answer %T: %v", answer, err)
	}
	return string(encoded)
}

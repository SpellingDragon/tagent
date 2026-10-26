package subagent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAgentsFileParseMatrix pins the agents file form: every declared field is read and every undeclared shape is refused by name.
// - a full frontmatter yields every field, with the prompt file read into text
// - the markdown body supplies the prompt when the frontmatter declares none
// - both sources, an unreadable file, a missing name, a wrong type and an unknown key are refused
// 契约: docs/wiki/platform/platform-subsystems.md#strict-decode
func TestAgentsFileParseMatrix(t *testing.T) {
	ctx := context.Background()

	t.Run("a full frontmatter yields every field", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("You review changes.\n"), 0o600); err != nil {
			t.Fatalf("write prompt file = %v, want a readable file", err)
		}
		path := writeAgentsFile(t, dir, "reviewer.md", `---
name: reviewer
description: Reviews a change end to end.
model: glm-5.3-flash
prompt: ./prompt.md
skills:
  - go-service-ops
  - change-review
tools: exec_run, mcp_call
mcp: [mcp-lsp]
---
`)

		d, err := LoadDefinition(ctx, Source{AgentsFile: path})
		if err != nil {
			t.Fatalf("load %s = %v, want every field", path, err)
		}
		if d.Name != "reviewer" || d.Description != "Reviews a change end to end." || d.Model != "glm-5.3-flash" {
			t.Fatalf("definition = %+v, want the declared name, description and model", d)
		}
		if d.Prompt != "You review changes." {
			t.Fatalf("prompt = %q, want the prompt file text", d.Prompt)
		}
		requireNames(t, "skills", d.Skills, []string{"go-service-ops", "change-review"})
		requireNames(t, "tools", d.Tools, []string{"exec_run", "mcp_call"})
		requireNames(t, "mcp", d.MCP, []string{"mcp-lsp"})
	})

	t.Run("the body supplies the prompt", func(t *testing.T) {
		path := writeAgentsFile(t, t.TempDir(), "body.md", `---
name: body-prompt
---
# Reviewer

Review the change.
`)

		d, err := LoadDefinition(ctx, Source{AgentsFile: path})
		if err != nil {
			t.Fatalf("load %s = %v, want the body as prompt", path, err)
		}
		if d.Prompt != "# Reviewer\n\nReview the change." {
			t.Fatalf("prompt = %q, want the markdown body", d.Prompt)
		}
	})

	t.Run("both sources are refused", func(t *testing.T) {
		_, err := LoadDefinition(ctx, Source{
			AgentsFile: "reviewer.md",
			Explicit:   &Definition{Name: "reviewer"},
		})
		requireRefusal(t, err, ErrSourceConflict)
	})

	t.Run("an unreadable file is refused", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.md")
		d, err := LoadDefinition(ctx, Source{AgentsFile: missing})
		requireRefusal(t, err, ErrFileUnreadable)
		if d != nil {
			t.Fatalf("definition = %+v, want none from a refused file", d)
		}
	})

	t.Run("a missing name is refused", func(t *testing.T) {
		path := writeAgentsFile(t, t.TempDir(), "no-name.md", `---
description: Reviews a change.
---
`)
		d, err := LoadDefinition(ctx, Source{AgentsFile: path})
		requireRefusal(t, err, ErrNameMissing)
		if d != nil {
			t.Fatalf("definition = %+v, want none without a name", d)
		}
	})

	t.Run("a wrong field type is refused", func(t *testing.T) {
		path := writeAgentsFile(t, t.TempDir(), "type-error.md", `---
name: reviewer
model: [glm-5.3-flash]
---
`)
		d, err := LoadDefinition(ctx, Source{AgentsFile: path})
		requireRefusal(t, err, ErrFieldType)
		if d != nil {
			t.Fatalf("definition = %+v, want none from a mistyped field", d)
		}
	})

	t.Run("an unknown key is refused", func(t *testing.T) {
		path := writeAgentsFile(t, t.TempDir(), "unknown-key.md", `---
name: reviewer
temperatur: 0.2
---
`)
		d, err := LoadDefinition(ctx, Source{AgentsFile: path})
		requireRefusal(t, err, ErrUnknownField)
		if d != nil {
			t.Fatalf("definition = %+v, want none from an unknown key", d)
		}
	})
}

// writeAgentsFile writes one agents file into dir and returns its path.
func writeAgentsFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s = %v, want a readable file", name, err)
	}
	return path
}

// requireRefusal fails unless err unwraps to want.
func requireRefusal(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("refusal = %v, want %v", err, want)
	}
}

// requireNames fails unless got holds exactly the names want lists in order.
func requireNames(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", field, got, want)
		}
	}
}

package subagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestExplicitParamsAssembly pins inheritance: an undeclared field takes the parent's and a declared one overrides it.
// - description, model, instruction and name lists are inherited when undeclared
// - a declared field overrides the parent's field of the same name
// - an explicitly empty list clears the parent's list instead of inheriting it
// - a prompt file is read into the instruction and an unreadable one is refused by name
// - both sources, no source and a missing name are refused by name
// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
func TestExplicitParamsAssembly(t *testing.T) {
	ctx := context.Background()

	t.Run("undeclared fields inherit the parent", func(t *testing.T) {
		parent := parentAssembly()
		got, err := Assemble(ctx, parent, Source{Explicit: &Definition{Name: "child"}})
		if err != nil {
			t.Fatalf("assemble = %v, want the parent fields", err)
		}
		if got.Name != "child" {
			t.Fatalf("name = %q, want the declared child name", got.Name)
		}
		if got.Description != "parent description" || got.Model != "parent-model" {
			t.Fatalf("description/model = %q/%q, want the parent values", got.Description, got.Model)
		}
		if got.Instruction != "parent instruction" {
			t.Fatalf("instruction = %q, want the parent instruction", got.Instruction)
		}
		requireNames(t, "skills", got.Skills, []string{"p-skill"})
		requireNames(t, "tools", got.Tools, []string{"p-tool"})
		requireNames(t, "mcp", got.MCP, []string{"p-mcp"})
		got.Skills[0] = "changed"
		if parent.Skills[0] != "p-skill" {
			t.Fatalf("parent skills = %v, want the inherited list to be a copy", parent.Skills)
		}
	})

	t.Run("declared fields override the parent", func(t *testing.T) {
		src := Source{Explicit: &Definition{
			Name:        "child",
			Description: "child description",
			Model:       "child-model",
			Prompt:      "child instruction",
			Skills:      []string{"c-skill"},
			Tools:       []string{"c-tool"},
			MCP:         []string{"c-mcp"},
		}}
		got, err := Assemble(ctx, parentAssembly(), src)
		if err != nil {
			t.Fatalf("assemble = %v, want the declared fields", err)
		}
		if got.Description != "child description" || got.Model != "child-model" || got.Instruction != "child instruction" {
			t.Fatalf("description/model/instruction = %q/%q/%q, want the declared values", got.Description, got.Model, got.Instruction)
		}
		requireNames(t, "skills", got.Skills, []string{"c-skill"})
		requireNames(t, "tools", got.Tools, []string{"c-tool"})
		requireNames(t, "mcp", got.MCP, []string{"c-mcp"})
	})

	t.Run("an empty list clears the parent list", func(t *testing.T) {
		src := Source{Explicit: &Definition{Name: "child", Skills: []string{}, Tools: []string{}}}
		got, err := Assemble(ctx, parentAssembly(), src)
		if err != nil {
			t.Fatalf("assemble = %v, want the cleared lists", err)
		}
		if got.Skills == nil || len(got.Skills) != 0 {
			t.Fatalf("skills = %v, want an empty list rather than the parent's", got.Skills)
		}
		if got.Tools == nil || len(got.Tools) != 0 {
			t.Fatalf("tools = %v, want an empty list rather than the parent's", got.Tools)
		}
		requireNames(t, "mcp", got.MCP, []string{"p-mcp"})
	})

	t.Run("a prompt file is read into the instruction", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "prompt.md")
		if err := os.WriteFile(path, []byte("Review the change.\n"), 0o600); err != nil {
			t.Fatalf("write prompt file = %v, want a readable file", err)
		}
		got, err := Assemble(ctx, nil, Source{Explicit: &Definition{Name: "child", Prompt: path}})
		if err != nil {
			t.Fatalf("assemble = %v, want the prompt file text", err)
		}
		if got.Instruction != "Review the change." {
			t.Fatalf("instruction = %q, want the prompt file text", got.Instruction)
		}
	})

	t.Run("an unreadable prompt file is refused", func(t *testing.T) {
		src := Source{Explicit: &Definition{Name: "child", Prompt: filepath.Join(t.TempDir(), "missing.md")}}
		got, err := Assemble(ctx, parentAssembly(), src)
		requireRefusal(t, err, ErrFileUnreadable)
		if got != nil {
			t.Fatalf("assembly = %+v, want none from a refused prompt", got)
		}
	})

	t.Run("both sources are refused", func(t *testing.T) {
		src := Source{AgentsFile: "reviewer.md", Explicit: &Definition{Name: "child"}}
		got, err := Assemble(ctx, parentAssembly(), src)
		requireRefusal(t, err, ErrSourceConflict)
		if got != nil {
			t.Fatalf("assembly = %+v, want none from two sources", got)
		}
	})

	t.Run("no source is refused", func(t *testing.T) {
		got, err := Assemble(ctx, parentAssembly(), Source{})
		requireRefusal(t, err, ErrSourceMissing)
		if got != nil {
			t.Fatalf("assembly = %+v, want none without a source", got)
		}
	})

	t.Run("a missing name is refused and never inherited", func(t *testing.T) {
		got, err := Assemble(ctx, parentAssembly(), Source{Explicit: &Definition{Model: "child-model"}})
		requireRefusal(t, err, ErrNameMissing)
		if got != nil {
			t.Fatalf("assembly = %+v, want none without a name", got)
		}
	})
}

// parentAssembly returns the parent every inheritance case resolves against.
func parentAssembly() *Assembly {
	return &Assembly{
		Definition: Definition{
			Name:        "parent",
			Description: "parent description",
			Model:       "parent-model",
			Skills:      []string{"p-skill"},
			Tools:       []string{"p-tool"},
			MCP:         []string{"p-mcp"},
		},
		Instruction: "parent instruction",
	}
}

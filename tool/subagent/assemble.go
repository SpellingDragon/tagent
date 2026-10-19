// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// assemble resolves one sub-agent declaration against its parent into the plain
// data a trpc agent builder takes.
package subagent

import "context"

// Assembly is the resolved sub-agent definition the trpc agent builder consumes.
//
// Name, Description and Instruction feed the builder's name, description and
// instruction inputs; Model, Skills, Tools and MCP are the reference names it
// resolves into a model, a skill repository and tool values.
type Assembly struct {
	// Definition is the declaration every field was resolved from.
	Definition
	// Instruction is the prompt text the sub-agent runs on.
	Instruction string
}

// Assemble resolves one declaration against its parent: a declared field overrides
// the parent's field of the same name and an undeclared field inherits it.
//
// Name is never inherited, a definition without one is refused, and a refused
// declaration returns no assembly.
func Assemble(ctx context.Context, parent *Assembly, src Source) (*Assembly, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	def, err := LoadDefinition(ctx, src)
	if err != nil {
		return nil, err
	}
	a := &Assembly{Definition: *def}
	if err := resolveInstruction(a); err != nil {
		return nil, err
	}
	inherit(a, parent)
	return a, nil
}

// resolveInstruction settles the instruction a declared prompt carries: a resolved
// prompt is already text and every other one is read as text or as a file reference.
func resolveInstruction(a *Assembly) error {
	if a.Prompt == "" {
		return nil
	}
	if a.promptText {
		a.Instruction = a.Prompt
		return nil
	}
	text, err := readPrompt(explicitLabel, "", a.Prompt)
	if err != nil {
		return err
	}
	a.Instruction = text
	return nil
}

// inherit fills every field the declaration left unset from the parent assembly.
func inherit(a *Assembly, parent *Assembly) {
	if parent == nil {
		return
	}
	if a.Description == "" {
		a.Description = parent.Description
	}
	if a.Model == "" {
		a.Model = parent.Model
	}
	if a.Skills == nil {
		a.Skills = cloneStrings(parent.Skills)
	}
	if a.Tools == nil {
		a.Tools = cloneStrings(parent.Tools)
	}
	if a.MCP == nil {
		a.MCP = cloneStrings(parent.MCP)
	}
	if a.Instruction == "" {
		a.Instruction = parent.Instruction
	}
}

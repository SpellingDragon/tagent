// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
//
// Package subagent turns one sub-agent declaration - an agents file or explicit
// parameters - into the resolved assembly the trpc agent builder consumes. The
// package holds no live object: a refused declaration yields no assembly.
package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Definition is one sub-agent declaration in the form either source writes it.
//
// Prompt holds instruction text or a file reference (a leading ./, ../ or /),
// which is read into text before assembly. Skills, Tools and MCP hold the reference
// names the builder resolves into live instances; an empty string field is
// undeclared and inherits the parent's, while an empty list clears it.
type Definition struct {
	// Name identifies the sub-agent; a definition without one is refused.
	Name string
	// Description describes the sub-agent to its caller.
	Description string
	// Model names the model the sub-agent runs on.
	Model string
	// Prompt is the instruction text or a file reference holding that text.
	Prompt string
	// Skills lists the skill names the sub-agent may load.
	Skills []string
	// Tools lists the tool names the sub-agent may call.
	Tools []string
	// MCP lists the MCP server names the sub-agent may use.
	MCP []string
	// promptText marks a Prompt the loader already resolved to text.
	promptText bool
}

// Source names exactly one assembly source: an agents file or explicit parameters.
type Source struct {
	// AgentsFile is the path of the agents file to read.
	AgentsFile string
	// Explicit holds explicit parameters; nil when AgentsFile names the source.
	Explicit *Definition
}

// Error is one refused declaration: the source and line it was read at, the field
// it failed on and the reason, with the refusal kind readable through errors.Is.
type Error struct {
	// Source names the agents file or parameter set the declaration came from.
	Source string
	// Line is the one-based line of the failing field; zero without line information.
	Line int
	// Field is the dotted field path of the refusal; empty for a source-level refusal.
	Field string
	// Kind is the refusal sentinel errors.Is reads.
	Kind error
	// Reason states the refusal in words.
	Reason string
}

// Error renders the refusal as source, line, field and reason.
func (e *Error) Error() string {
	where := e.Source
	if e.Line > 0 {
		where = fmt.Sprintf("%s:%d", e.Source, e.Line)
	}
	switch {
	case where != "" && e.Field != "":
		return fmt.Sprintf("subagent: %s: %s: %s", where, e.Field, e.Reason)
	case where != "":
		return fmt.Sprintf("subagent: %s: %s", where, e.Reason)
	case e.Field != "":
		return fmt.Sprintf("subagent: %s: %s", e.Field, e.Reason)
	default:
		return "subagent: " + e.Reason
	}
}

// Unwrap exposes the refusal kind to errors.Is.
func (e *Error) Unwrap() error { return e.Kind }

// ErrSourceConflict reports a call naming both assembly sources at once.
var ErrSourceConflict = errors.New("both an agents file and explicit parameters were given")

// ErrSourceMissing reports a call naming no assembly source.
var ErrSourceMissing = errors.New("no assembly source was given")

// ErrFileUnreadable reports an agents file or prompt file that cannot be read.
var ErrFileUnreadable = errors.New("the file cannot be read")

// ErrNameMissing reports a definition that declares no name.
var ErrNameMissing = errors.New("the name is required")

// ErrFieldType reports a declared field whose shape is not the one the form names.
var ErrFieldType = errors.New("the field has the wrong shape")

// ErrUnknownField reports a declared key the definition form does not name.
var ErrUnknownField = errors.New("unknown field")

// ErrFileShape reports an agents file whose markdown or frontmatter shape is invalid.
var ErrFileShape = errors.New("the agents file shape is invalid")

// explicitLabel names the parameter set in a refusal that came from explicit input.
const explicitLabel = "params"

// LoadDefinition reads the definition named by exactly one source.
func LoadDefinition(ctx context.Context, src Source) (*Definition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case src.AgentsFile != "" && src.Explicit != nil:
		return nil, &Error{Source: src.AgentsFile, Kind: ErrSourceConflict, Reason: ErrSourceConflict.Error()}
	case src.AgentsFile != "":
		return loadAgentsFile(src.AgentsFile)
	case src.Explicit != nil:
		d := cloneDefinition(src.Explicit)
		if err := requireName(explicitLabel, d); err != nil {
			return nil, err
		}
		return d, nil
	default:
		return nil, &Error{Kind: ErrSourceMissing, Reason: ErrSourceMissing.Error()}
	}
}

// requireName refuses a definition that carries no name.
func requireName(source string, d *Definition) error {
	if strings.TrimSpace(d.Name) == "" {
		return &Error{Source: source, Field: "name", Kind: ErrNameMissing, Reason: "required field missing"}
	}
	return nil
}

// cloneDefinition copies a definition so later edits of the source cannot reach it.
func cloneDefinition(d *Definition) *Definition {
	out := *d
	out.Skills = cloneStrings(d.Skills)
	out.Tools = cloneStrings(d.Tools)
	out.MCP = cloneStrings(d.MCP)
	return &out
}

// cloneStrings copies a name list; nil stays nil and an empty list stays empty.
func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

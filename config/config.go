// 契约: docs/wiki/platform/platform-subsystems.md#config-surface
//
// Package config defines the pure declaration layer of the runtime: the
// organization declaration read from a configuration source, together with the
// agent, tool and file references that declaration names.
//
// The package is a leaf and depends only on the Go standard library; it never
// imports another tagent package. It describes data and holds no live objects
// (no model, tool or plugin instances, no connections, no locks, no
// generation). Turning a Declaration into live bindings belongs to the
// composition root and to the state owner, not here.
//
//   - config.go states the declaration vocabulary;
//   - decode.go maps a decoded source tree onto it, rejecting every key the surface does not name;
//   - load.go is the assembly entry that reads a source and applies defaults;
//   - source.go decodes the source syntax, while defaults.go, clone.go and fingerprint.go hold the default pass, the deep copy and the content fingerprint.
package config

// Declaration is the complete organization declaration parsed from a
// configuration source.
//
// A Declaration describes an organization in terms of data only: the entry
// agent, the declared agent graph, the references those agents use, and the
// version baseline the declaration was loaded against. Applying defaults and
// normalizing content is the job of Load.
type Declaration struct {
	// Entry is the name of the entry agent that receives application input.
	Entry string
	// Agents lists the declared agents in declaration order. The order is
	// significant: candidate resources are later constructed in it.
	Agents []AgentSpec
	// Refs lists organization level references (tools, sub-agent delegation
	// targets and remote endpoints) that are not owned by a single agent.
	Refs []ToolRef
	// Baseline records the version baseline the declaration was loaded against.
	Baseline string
	// Raw is the decoded source document before defaults and normalization are
	// applied.
	Raw map[string]any
}

// AgentSpec is the declaration of a single agent inside an organization.
type AgentSpec struct {
	// ID identifies the agent within its declaration.
	ID string
	// ParentID names the declaring parent agent and is empty for the entry agent.
	ParentID string
	// Model references the model the agent uses. The reference is a name that
	// the composition root resolves; this package never resolves it.
	Model string
	// Tools lists the agent's tool, sub-agent and remote references.
	Tools []ToolRef
	// Plugins lists the agent's plugin references in pipeline order.
	Plugins []string
	// TaskPolicy references the task policy applied to the agent's tasks.
	TaskPolicy string
	// Files lists the prompt and skill files the agent is assembled from, in
	// assembly order.
	Files []FileRef
	// Params holds the agent's declaration-time model parameters.
	Params map[string]float64
}

// ToolRef is a declaration-time reference to a tool, to a sub-agent delegation
// target, or to a remote endpoint.
type ToolRef struct {
	// Name is the referenced name as written in the declaration.
	Name string
	// Params holds the reference's declaration-time parameters.
	Params map[string]string
	// Endpoint is the remote target of a remote-only reference and is empty for
	// a locally resolvable reference.
	Endpoint string
}

// FileRef references a prompt or skill file whose content is read at request
// boundaries.
type FileRef struct {
	// Path is the source-relative path of the referenced file.
	Path string
	// Kind identifies the role the file plays in request assembly.
	Kind FileKind
}

// FileKind identifies the role a referenced file plays in request assembly.
type FileKind string

const (
	// FileKindPrompt marks a file whose content is the agent's system prompt.
	FileKindPrompt FileKind = "prompt"
	// FileKindSkill marks a file whose content is a skill made available to the
	// agent.
	FileKindSkill FileKind = "skill"
)

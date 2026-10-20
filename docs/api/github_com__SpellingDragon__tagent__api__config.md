package config // import "github.com/SpellingDragon/tagent/api/config"

Package config defines the pure declaration layer of the runtime: the
organization declaration read from a configuration source, together with the
agent, tool and file references that declaration names.

The package is a leaf and depends only on the Go standard library; it never
imports another tagent package. It describes data and holds no live objects
(no model, tool or plugin instances, no connections, no locks, no generation).
Turning a Declaration into live bindings belongs to the composition root and to
the state owner, not here.

  - config.go states the declaration vocabulary;
  - decode.go maps a decoded source tree onto it, rejecting every key the
    surface does not name;
  - load.go is the assembly entry that reads a source and applies defaults;
  - source.go decodes the source syntax, while defaults.go, clone.go and
    fingerprint.go hold the default pass, the deep copy and the content
    fingerprint.

The strict mapping face of the declaration layer: it maps a decoded source
tree onto the declaration vocabulary in config.go, rejecting every key the
surface does not name, every repeated key and every value of the wrong shape,
and it names the offending source, line and field path in the error. The field
readers below are the only place the source node shapes are inspected, so the
declaration types never carry syntax knowledge.

The assembly face of the declaration layer: the one entry that turns a source
file into a Declaration. Decoding the source syntax belongs to source.go,
mapping the decoded tree onto the declaration types belongs to decode.go and the
default pass belongs to defaults.go; this file only sequences them, so a caller
reads one door instead of three.

The source-syntax face of the declaration layer: it turns the bytes of a .yaml,
.yml or .json source into one sourceNode tree and rejects every other extension
by name. The node types are syntax only — they name no declaration word — so
decode.go maps this tree onto the vocabulary in config.go and this file never
decides what a field means.

The strict face for caller-owned document schemas: decode.go names one schema,
the declaration vocabulary, while a host composes its own document (connection
registry, governance section, application section) and owes its reader the same
rejection. DecodeStrict applies this package's policy — every key the schema
does not name is refused by source, line and field path — to the schema the
caller's struct type states in its yaml tags, so one rejection vocabulary covers
both kinds of document and no host spells its own.

VARIABLES

var ErrUnknownConfigKey = errors.New("document key outside the schema")
    ErrUnknownConfigKey reports a document key the target schema does not name.
    Errors wrapping it also carry the source, the line and the field path,
    so a caller can tell a key outside the schema from a value of the wrong
    shape with errors.Is alone.

FUNCTIONS

func DecodeStrict(source string, data []byte, v any) error
    DecodeStrict decodes the document in data into v and refuses every mapping
    key the type of v does not name. v must point at a struct whose yaml tags
    state the schema; nested structs, maps and sequences are walked, so a host
    document and its sections are checked to the same depth. The zero document
    decodes into the zero value of v, and a value of the wrong shape is reported
    by source and reason rather than by key.

func Fingerprint(d Declaration) (string, error)

    Fingerprint returns the canonical organization fingerprint of d: a stable
    string derived solely from the declaration surface, namely the entry agent,
    the declared agent graph with its model selection, prompt sources and tool
    declarations, the organization references and the version baseline. Live
    objects (model handles, tool instances, connections, locks and generation
    numbers) never contribute to the fingerprint, so reloading equivalent
    content yields the same fingerprint.

    The fingerprint is the SHA-256 of the canonical JSON encoding of the
    declaration with defaults applied: omitted collections encode as empty ones,
    map keys encode in order, negative zero normalizes to zero and the decoded
    source document is left out. The declaration d is not modified.

TYPES

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
	// FinalRound is the watchdog budget the invocations of this agent run
	// under. Every field of the zero budget is zero, which places no bound on
	// any invocation.
	FinalRound FinalRoundSpec
	// ReplayBudget is the token budget the mapped sequence of this agent's
	// session chain is assembled under at every request boundary: the committed
	// events of the chain are projected into the request context under it. Zero
	// attaches no replay, so every request runs on its fixed files and
	// transcript alone.
	ReplayBudget int
	// EnvironmentBlock states whether every request of this agent closes with
	// the dynamic environment block: the tool, skill and MCP faces the request
	// carries beside the open asynchronous task state. False keeps a request
	// exactly as it stands without the block.
	EnvironmentBlock bool
}
    AgentSpec is the declaration of a single agent inside an organization.

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
    Declaration is the complete organization declaration parsed from a
    configuration source.

    A Declaration describes an organization in terms of data only: the entry
    agent, the declared agent graph, the references those agents use, and the
    version baseline the declaration was loaded against. Applying defaults and
    normalizing content is the job of Load.

func Load(ctx context.Context, path string) (*Declaration, error)

    Load reads the declaration document at path, applies defaults to it and
    returns the normalized Declaration. Sources ending in .yaml or .yml decode
    as YAML, sources ending in .json decode as JSON, and every other extension
    is rejected by name. Failures are reported as errors that name the offending
    source, the line within it, the field path and the reason.

func (d *Declaration) AgentSpecs() []AgentSpec
    AgentSpecs returns the declared agents as a deep copy, so callers may mutate
    the result without touching the declaration.

func (d *Declaration) Clone() *Declaration

    Clone returns a deep copy of the declaration. Mutating the copy never
    changes the original, and the copy owns its own raw document.

func (d Declaration) FactKey() string
    FactKey returns the declaration's identity as a fact key: the canonical
    organization fingerprint in its hex spelling (see Fingerprint). That
    identity is synthesised from the declaration surface alone, so reloading
    equivalent content answers the same key and any change to the surface
    answers a different one. The raw source document and every live binding
    stay outside it, and a declaration the canonical encoding cannot represent
    answers no key: the empty string, which names no fact.

func (d Declaration) FactType() string
    FactType returns the frozen type a declaration is read as.

func (d *Declaration) RawDocument() map[string]any
    RawDocument returns the decoded source document as a deep copy, so callers
    may mutate the result without touching the declaration.

func (d *Declaration) ToolRefs() []ToolRef
    ToolRefs returns the organization level references as a deep copy,
    so callers may mutate the result without touching the declaration.

type FileKind string
    FileKind identifies the role a referenced file plays in request assembly.

const (
	// FileKindPrompt marks a file whose content is the agent's system prompt.
	FileKindPrompt FileKind = "prompt"
	// FileKindSkill marks a file whose content is a skill made available to the
	// agent.
	FileKindSkill FileKind = "skill"
)
type FileRef struct {
	// Path is the source-relative path of the referenced file.
	Path string
	// Kind identifies the role the file plays in request assembly.
	Kind FileKind
}
    FileRef references a prompt or skill file whose content is read at request
    boundaries.

type FinalRoundSpec struct {
	// MaxRounds bounds the tool rounds one invocation may run; 0 removes the
	// bound.
	MaxRounds int
	// BudgetSeconds bounds the time one invocation may spend, in seconds; 0
	// removes the bound.
	BudgetSeconds float64
	// TimeoutSeconds bounds the closing round itself, in seconds; 0 takes the
	// runtime default timeout.
	TimeoutSeconds float64
	// Prompt is the closing instruction handed to the model; "" takes the
	// runtime default instruction.
	Prompt string
}
    FinalRoundSpec is the declaration of one agent's watchdog budget: how many
    tool rounds one invocation may run and how long it may spend before its
    next decision becomes the closing round, the instruction that round carries
    and the timeout it runs under. The zero budget places no bound on any
    invocation, so a declaration that names no budget leaves every drive exactly
    as it runs beside no budget at all.

type ToolRef struct {
	// Name is the referenced name as written in the declaration.
	Name string
	// Params holds the reference's declaration-time parameters.
	Params map[string]string
	// Endpoint is the remote target of a remote-only reference and is empty for
	// a locally resolvable reference.
	Endpoint string
}
    ToolRef is a declaration-time reference to a tool, to a sub-agent delegation
    target, or to a remote endpoint.

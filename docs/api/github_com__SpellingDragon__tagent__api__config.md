package config // import "github.com/SpellingDragon/tagent/api/config"

Package config defines the pure declaration layer of the runtime: the
organization declaration read from a configuration source, together with the
agent, tool and file references that declaration names.

The package is a leaf and depends only on the Go standard library; it never
imports another tagent package. It describes data and holds no live objects
(no model, tool or plugin instances, no connections, no locks, no generation).
Turning a Declaration into live bindings belongs to the composition root and to
the state owner, not here.

  - config.go states the declaration vocabulary every host shares;
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

	// Memory is the memory section this agent declares: the storage tier its
	// facts persist through, the namespaces it may read across partitions and
	// the semantic engine its recall vocabulary embeds with. The zero value
	// declares no memory binding.
	Memory MemorySpec
	// Meditation is the scheduled reflection this agent declares: whether it
	// runs, the cadence it ticks at, the prompt its input carries, the
	// namespaces it observes and the declared agents its result flows back to.
	// The zero value enables no reflection.
	Meditation MeditationSpec
	// File is the file surface this agent declares: the root every relative
	// path of its file tools resolves under and the extra roots an absolute
	// read path may name. The zero value declares no file surface.
	File FileSpec
	// Exec is the execution face this agent declares: the monitor rhythm every
	// run of its execution tools starts from. The zero value declares no
	// rhythm, which is the runtime default.
	Exec ExecSpec
}
    AgentSpec is the declaration of a single agent inside an organization: the
    core every host shares and the four domain sections it may declare (memory,
    meditation, file and exec). Every section is a plain field, so one file
    names every key of an agent declaration and a reader reaches a section by
    reading the field instead of going through a registration mechanism.

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
	// Servers declares the MCP servers the organization binds by name: each
	// entry fixes the transport, the endpoint and the environment variable
	// holding the key. An empty map binds no server, so the MCP gateways
	// answer the named empty state instead of reaching anything.
	Servers map[string]MCPServerSpec
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

type EmbeddingSpec struct {
	// Provider names the embedding protocol provider.
	Provider string `yaml:"provider"`
	// Model names the embedding model of that provider.
	Model string `yaml:"model"`
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string `yaml:"api_key_env"`
	// Dimensions is the vector width the connection declares.
	Dimensions int `yaml:"dimensions"`
	// APIEndpoint overrides the provider default endpoint when it is set.
	APIEndpoint string `yaml:"api_endpoint"`
}
    EmbeddingSpec is one embedding connection: the protocol provider, the model,
    the environment variable holding the key, the vector width, and the optional
    endpoint that overrides the provider default.

type EngineSpec struct {
	// Backend names the retrieval backend.
	Backend string `yaml:"backend"`
	// Embedding is the connection the vector leg embeds through.
	Embedding EmbeddingSpec `yaml:"embedding"`
	// VectorTopK bounds the vector leg of one retrieval; 0 takes the runtime
	// default.
	VectorTopK int `yaml:"vector_top_k"`
	// KeywordTopK bounds the keyword leg of one retrieval; 0 takes the runtime
	// default.
	KeywordTopK int `yaml:"keyword_top_k"`
	// RRFK is the rank constant the reciprocal-rank fusion runs under; 0 takes
	// the runtime default.
	RRFK int `yaml:"rrf_k"`
}
    EngineSpec is the semantic engine behind one memory declaration: the
    retrieval backend, the embedding connection and the two leg bounds and rank
    constant the hybrid retrieval runs under.

type ExecSpec struct {
	// Monitor is the rhythm a run starts from; its zero fields take the
	// runtime defaults.
	Monitor MonitorSpec `yaml:"monitor"`
}
    ExecSpec is the execution face one agent declares: the monitor rhythm every
    run of its execution tools starts from.

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

type FileSpec struct {
	// BaseDir is the directory every relative path resolves under; empty takes
	// the working directory when the face is declared some other way.
	BaseDir string `yaml:"base_dir"`
	// ReadRoots lists the extra roots an absolute read path may name beside the
	// base directory itself.
	ReadRoots []string `yaml:"read_roots"`
}
    FileSpec is the file face one agent declares: the root every relative path
    of its file tools resolves under and the extra roots an absolute read path
    may name.

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

type MCPServerSpec struct {
	// Transport names the connection transport; empty takes the remote default.
	Transport string `yaml:"transport"`
	// URL is the endpoint the connection is opened against.
	URL string `yaml:"url"`
	// APIKeyEnv names the environment variable holding the call key.
	APIKeyEnv string `yaml:"api_key_env"`
}
    MCPServerSpec is one MCP server the declaration binds: the transport,
    the endpoint and the environment variable holding the call key.

type MeditationSpec struct {
	// Enabled states whether this agent runs the reflection loop.
	Enabled bool `yaml:"enabled"`
	// Interval is the tick cadence; 0 takes the engine default cadence.
	Interval time.Duration `yaml:"interval"`
	// MinGap is the floor between two consumed reflections; 0 places no floor.
	MinGap time.Duration `yaml:"min_gap"`
	// PromptFile is the file the static reflection prompt is read from.
	PromptFile string `yaml:"prompt_file"`
	// ObservedNamespaces declares the observed face; empty means the agent's
	// own partition.
	ObservedNamespaces []string `yaml:"observed_namespaces"`
	// DeliverTo lists the declared agents the reflection result flows back to.
	DeliverTo []string `yaml:"deliver_to"`
}
    MeditationSpec is the scheduled reflection one agent declares: whether it
    runs, the tick cadence and the floor between two consumed reflections,
    the prompt its input carries, the namespaces it observes and the declared
    agents its result flows back to.

type MemorySpec struct {
	// Type names the storage tier kind, or is empty for no tier.
	Type string `yaml:"type"`
	// Path is the root the storage tier opens, or is empty for no tier.
	Path string `yaml:"path"`
	// ReadNamespaces lists the namespaces this agent may read across
	// partitions, beside its own namespace.
	ReadNamespaces []string `yaml:"read_namespaces"`
	// Engine is the semantic engine the recall retrieval runs under; its zero
	// value names no engine, which is the keyword-only retrieval.
	Engine EngineSpec `yaml:"engine"`
}
    MemorySpec is the memory declaration of one agent: the storage tier its
    facts persist through, the namespaces it is authorized to read across
    partitions, and the semantic engine its recall vocabulary generates its
    embeddings with.

    The tier is a type and a root, both required together; an engine beside no
    tier names no place for its vectors to persist.

type MonitorSpec struct {
	// Interval is the observation cadence.
	Interval time.Duration `yaml:"interval"`
	// StableDuration is the span a run must stay unchanged to count as stable.
	StableDuration time.Duration `yaml:"stable_duration"`
	// InteractiveStableDuration is the span an interactive run must stay
	// unchanged to count as stable.
	InteractiveStableDuration time.Duration `yaml:"interactive_stable_duration"`
	// FakeDeadDuration is the span after which a silent run is suspected dead.
	FakeDeadDuration time.Duration `yaml:"fake_dead_duration"`
}
    MonitorSpec is one monitor rhythm: the cadence the run is observed at,
    the span it must stay unchanged to count as stable, the span an interactive
    run must stay unchanged to count as stable and the span after which a silent
    run is suspected dead. Every zero field takes the runtime default.

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

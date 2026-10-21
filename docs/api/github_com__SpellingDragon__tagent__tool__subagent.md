package subagent // import "github.com/SpellingDragon/tagent/tool/subagent"

agentsfile reads the markdown form one sub-agent declaration is written in:
a fenced YAML frontmatter block over a prompt body.

assemble resolves one sub-agent declaration against its parent into the plain
data a trpc agent builder takes.

builder maps one resolved assembly onto the tRPC agent builder and runs one
turn of the agent that builder returns. The provider, the stream and the tool
protocol stay the tRPC agent's own: this file resolves the reference names an
assembly carries and drains the events of one turn.

The structured handoff one call_subagent call declares beside its
natural-language request: the fields the call names, the grants the assembled
sub-agent actually holds, and the envelope the delegated turn receives.

The preset tools of the configuration face live beside the sub-agent tool
because they share one composition-root seam: a declared `knowledge`
reference resolves onto the trpc knowledge tool over a loaded source,
a declared `recall` reference wraps the run's own memory retrieval face,
and a declared `call_subagent` reference resolves onto the tool of this package.
This file holds no retrieval of its own: the knowledge stack is the trpc one,
the recall behaviour is the face the caller injects, and an unknown reference is
left to the host-contributed tools instead of being refused.

The sampling one sub-agent call names for the requests of its delegated turn:
the temperature, thinking switch and reasoning effort the call declares flow
onto every request that turn runs. The wrapper sits between the trpc agent
builder and the model the assembly resolves, so a parameter declared at the
call site reaches the provider even though the trpc model constructors accept no
per-call sampling of their own.

A parameter the call does not name is never written as a zero, and a value a
request already carries is never overridden.

Package subagent turns one sub-agent declaration - an agents file or explicit
parameters - into the resolved assembly the trpc agent builder consumes.
The package holds no live object: a refused declaration yields no assembly.

The target tool of one declared agent reference: the declaration names the
target agent, so the model sees a tool of that agent's own name instead of a
generic call it could point anywhere. The tool surface behind it is the target's
own declaration — the tools that agent declares, not the caller's assembled set
— so the reference grants exactly the declared target and nothing wider.

call_subagent is the model-visible face of one sub-agent call: the declaration
names the two assembly sources, and a call resolves one of them into an
assembly, admits the assembled call as a task through the delegation face and
answers with that admission's receipt.

CONSTANTS

const (
	// KnowledgeRef is the reference resolving onto the trpc knowledge tool.
	KnowledgeRef = "knowledge"
	// RecallRef is the reference resolving onto the run's memory retrieval.
	RecallRef = "recall"
	// CallSubagentRef is the reference resolving onto the call_subagent tool.
	CallSubagentRef = ToolName
)
    KnowledgeRef, RecallRef and CallSubagentRef are the reference names a
    declaration uses to ask for a preset tool.

const (
	// KnowledgeSearchToolName is the tool name the plain knowledge search answers under.
	KnowledgeSearchToolName = "knowledge_search"
	// CodeSearchToolName is the tool name the code search answers under.
	CodeSearchToolName = "code_search"
)
    CodeSearchToolName and KnowledgeSearchToolName are the tool names a
    knowledge reference may select through its "tool" parameter.

const (
	// ParamTool selects the knowledge tool of a knowledge reference.
	ParamTool = "tool"
	// ParamSource names the local document sources of a knowledge reference.
	ParamSource = "source"
	// ParamMaxResults bounds the hits one knowledge search answers.
	ParamMaxResults = "max_results"
	// ParamMinScore drops the knowledge hits below the score.
	ParamMinScore = "min_score"
)
    ParamMinScore, ParamMaxResults, ParamSource and ParamTool are the
    configuration parameter names a preset reference reads.

const (
	// ParamAgent names the declared agent one reference authorizes.
	ParamAgent = "agent"
	// ParamDescriptionFile names the file the target's description is read from.
	ParamDescriptionFile = "description_file"
	// ParamEventParams names the call arguments that carry event references.
	ParamEventParams = "event_params"
)
    ParamAgent, ParamDescriptionFile and ParamEventParams are the reference
    parameters one agent reference carries: the declared target it authorizes,
    the file its description is read from at every request boundary, and the
    call arguments whose values travel as the handoff's refs.

const RecallToolName = "recall"
    RecallToolName is the tool name the recall reference is assembled under.

const ToolName = "call_subagent"
    ToolName is the tool name the declaration and the configuration reference
    share.

VARIABLES

var ErrAddressIncomplete = errors.New("call_subagent: the call frame names no complete session")
    ErrAddressIncomplete reports a call whose frame names no complete agent
    session.

var ErrArguments = errors.New("call_subagent: the arguments do not match the declaration")
    ErrArguments reports arguments the declaration does not name.

var ErrAssemblyMissing = errors.New("subagent: no assembly was given")
    ErrAssemblyMissing reports a build or a turn asked for without an assembly.

var ErrBindingsMissing = errors.New("call_subagent: no borrowing face is attached")
    ErrBindingsMissing reports a tool constructed with no borrowing face.

var ErrDelegationMissing = errors.New("call_subagent: no delegation face is attached")
    ErrDelegationMissing reports a tool constructed with no delegation face.

var ErrFieldType = errors.New("the field has the wrong shape")
    ErrFieldType reports a declared field whose shape is not the one the form
    names.

var ErrFileShape = errors.New("the agents file shape is invalid")
    ErrFileShape reports an agents file whose markdown or frontmatter shape is
    invalid.

var ErrFileUnreadable = errors.New("the file cannot be read")
    ErrFileUnreadable reports an agents file or prompt file that cannot be read.

var ErrHandoffArguments = errors.New("call_subagent: the handoff names no declared field")
    ErrHandoffArguments reports a handoff object naming a key the declaration
    does not name.

var ErrKnowledgeEndpointUnwired = errors.New("knowledge: a remote endpoint is not wired")
    ErrKnowledgeEndpointUnwired reports a knowledge reference naming a remote
    endpoint.

var ErrKnowledgeSourceMissing = errors.New("knowledge: the reference names no local source")
    ErrKnowledgeSourceMissing reports a knowledge reference that names no
    source.

var ErrKnowledgeToolUnknown = errors.New("knowledge: the reference names no known tool")
    ErrKnowledgeToolUnknown reports a knowledge reference naming no known tool.

var ErrModelMissing = errors.New("subagent: the assembly names no model")
    ErrModelMissing reports an assembly that names no model a turn could run on.

var ErrModelUnresolved = errors.New("subagent: the model reference resolved to no model")
    ErrModelUnresolved reports a model reference a resolver resolved to no
    model.

var ErrNameMissing = errors.New("the name is required")
    ErrNameMissing reports a definition that declares no name.

var ErrPresetNotWired = errors.New("preset: the declared tool carries no wiring")
    ErrPresetNotWired reports a declared preset the assembly carries no face
    for.

var ErrPresetParam = errors.New("preset: the parameter value cannot be read")
    ErrPresetParam reports a preset parameter whose value cannot be read.

var ErrResolverMissing = errors.New("subagent: no resolver is attached for the declared references")
    ErrResolverMissing reports a declared reference list the builder holds no
    resolver for.

var ErrSourceConflict = errors.New("both an agents file and explicit parameters were given")
    ErrSourceConflict reports a call naming both assembly sources at once.

var ErrSourceMissing = errors.New("no assembly source was given")
    ErrSourceMissing reports a call naming no assembly source.

var ErrTargetArguments = errors.New("subagent: the call names no declared argument")
    ErrTargetArguments reports a call carrying a key the target's declaration
    does not name.

var ErrTargetNameMissing = errors.New("subagent: the target tool names no declared agent")
    ErrTargetNameMissing reports a target tool constructed with no name.

var ErrTurnFailed = errors.New("subagent: the delegated turn failed")
    ErrTurnFailed reports a turn whose stream reached a terminal error.

var ErrUnknownField = errors.New("unknown field")
    ErrUnknownField reports a declared key the definition form does not name.

FUNCTIONS

func NewRecallTool(recall RecallFunc) trpctool.Tool
    NewRecallTool returns the recall tool over the memory retrieval face the
    caller injects: the tool holds no retrieval of its own, so its behaviour is
    exactly the face it was assembled over.

func ResolvePresets(ctx context.Context, refs []config.ToolRef, opts PresetOptions) ([]trpctool.Tool, error)
    ResolvePresets resolves every preset reference of one declaration into the
    tool it names, in declaration order. A reference naming no preset is left to
    the host-contributed tools and resolves to nothing here, so a declaration
    that references a contributed tool never fails on this step.

TYPES

type Assembly struct {
	// Definition is the declaration every field was resolved from.
	Definition
	// Instruction is the prompt text the sub-agent runs on.
	Instruction string
}
    Assembly is the resolved sub-agent definition the trpc agent builder
    consumes.

    Name, Description and Instruction feed the builder's name, description and
    instruction inputs; Model, Skills, Tools and MCP are the reference names it
    resolves into a model, a skill repository and tool values.

func Assemble(ctx context.Context, parent *Assembly, src Source) (*Assembly, error)
    Assemble resolves one declaration against its parent: a declared field
    overrides the parent's field of the same name and an undeclared field
    inherits it.

    Name is never inherited, a definition without one is refused, and a refused
    declaration returns no assembly.

type Builder struct {
	// Model resolves the model reference an assembly names; an assembly that names a
	// model with no resolver attached is refused by name.
	Model ModelResolver
	// Skills resolves the skill references into the repository holding them.
	Skills SkillResolver
	// Tools resolves the tool references into the tools they name.
	Tools ToolResolver
	// MCP resolves the MCP server references into the tool sets they name.
	MCP ToolSetResolver
	// Sessions is the session service a delegated turn runs on; nil gives every turn
	// an in-memory session service of its own.
	Sessions session.Service
	// Facts reads the committed fact one handoff reference names, so the block a
	// delegated turn receives carries the content of each ref under the projection
	// budget instead of its bare identity. A builder beside no reader renders the
	// identities alone, exactly as it did before the reader existed.
	Facts FactReader
	// RefBudget is the token budget the handoff refs render under; zero keeps the
	// package default, the budget unit the request assembly packs under.
	RefBudget int
}
    Builder resolves one assembly and hands it to the tRPC agent builder.
    Every resolver is injected: this package holds no model registry, no skill
    repository and no tool store of its own.

func (b Builder) Build(ctx context.Context, a *Assembly) (trpcagent.Agent, error)
    Build returns the live agent one assembly describes: the model, the skill
    repository, the tools and the MCP tool sets the assembly names are resolved
    through the injected resolvers and handed to the tRPC builder, and a
    reference that cannot be resolved refuses the build with no agent beside it.

func (b Builder) Serve(ctx context.Context, a *Assembly, turn Turn) (TurnResult, error)
    Serve builds the agent of one assembly and runs one turn of it through the
    tRPC runner, reporting the assistant text the turn ended on. The runner
    owns the session append of the turn, so a turn that ends never waits on a
    completion no one signals.

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
	// Temperature is the sampling temperature every request of the sub-agent
	// carries. Nil declares none, so the model default stands; a declared zero
	// is the deterministic sampling zero names.
	Temperature *float64
	// ThinkingEnabled states whether the model's extended reasoning runs for
	// every request of the sub-agent. Nil declares no switch, so the model
	// default stands; a declared false is the explicit off.
	ThinkingEnabled *bool
	// ReasoningEffort names the reasoning effort every request of the sub-agent
	// carries. Empty declares none, so the model default stands.
	ReasoningEffort string

	// Has unexported fields.
}
    Definition is one sub-agent declaration in the form either source writes it.

    Prompt holds instruction text or a file reference (a leading ./, ../ or /),
    which is read into text before assembly. Skills, Tools and MCP hold the
    reference names the builder resolves into live instances; an empty string
    field is undeclared and inherits the parent's, while an empty list clears
    it.

func LoadDefinition(ctx context.Context, src Source) (*Definition, error)
    LoadDefinition reads the definition named by exactly one source.

type Delegation interface {
	// Delegate admits one sub-agent call as a task and reports its admission receipt.
	Delegate(ctx context.Context, req agent.Delegation) (task.AdmissionReceipt, error)
	// Complete reports the terminal of one admitted call, so its notification carries
	// the outcome to the session that issued the call.
	Complete(ctx context.Context, taskID event.TaskID, status task.Status, fact event.EventID) (task.Notice, error)
}
    Delegation is the task face one sub-agent call is admitted through: the
    admission that answers the caller with a receipt, and the terminal report of
    that same task once the delegated turn ends.

    红线清单（2 条）： MUST answer the caller with the admission receipt and report the
    terminal as a notice of the same task MUST NOT create a private writable
    history for the delegated turn, nor replace a notice identity with the fact
    it points at

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
    Error is one refused declaration: the source and line it was read at, the
    field it failed on and the reason, with the refusal kind readable through
    errors.Is.

func (e *Error) Error() string
    Error renders the refusal as source, line, field and reason.

func (e *Error) Unwrap() error
    Unwrap exposes the refusal kind to errors.Is.

type FactReader func(ctx context.Context, id event.EventID) (event.Event, error)
    FactReader reads the committed fact one handoff reference names. A run
    answers it through its own committed-fact read face (Run.CommittedEvent),
    so a ref renders the fact as it committed and never a reconstruction of it;
    a read that refuses the identity renders as the missing reference it is.

type ModelResolver func(ctx context.Context, name string) (trpcmodel.Model, error)
    ModelResolver resolves one model reference name into the model it names.

type PresetOptions struct {
	// Recall is the run's own memory retrieval face; a recall reference
	// declared beside no face is refused by name.
	Recall RecallFunc
	// Subagent is the call_subagent tool wired by the composition root; a
	// call_subagent reference declared beside no tool is refused by name.
	Subagent *Tool
}
    PresetOptions fixes the faces a preset reference is assembled over.

type RecallFunc func(ctx context.Context, q search.Query) (search.Result, error)
    RecallFunc is the memory retrieval face a recall tool answers through:
    the run's own recall over the committed store, never a retrieval of this
    package.

type SkillResolver func(ctx context.Context, names []string) (skill.Repository, error)
    SkillResolver resolves one skill reference list into the repository holding
    them.

type Source struct {
	// AgentsFile is the path of the agents file to read.
	AgentsFile string
	// Explicit holds explicit parameters; nil when AgentsFile names the source.
	Explicit *Definition
}
    Source names exactly one assembly source: an agents file or explicit
    parameters.

type Target struct {
	// Has unexported fields.
}
    Target is the model-visible face of one declared agent reference: a tool of
    the target's own name whose call admits one turn of that target as a task.

func NewTarget(opts TargetOptions) (*Target, error)
    NewTarget returns the target tool over the declared faces.

func (t *Target) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements the native callable tool port: it reads one call's declared
    arguments, builds the handoff the declared event params feed, admits one
    turn of the target as a task and answers with the receipt of that admission.

func (t *Target) Declaration() *trpctool.Declaration
    Declaration implements the native callable tool port. The description is
    read from the declared file at every read, so a rewritten file reaches the
    next request's tool declaration without a reload; a file that cannot be read
    answers with the declared description instead.

type TargetOptions struct {
	// Name is the tool name the model sees: the declared target agent identity.
	Name string
	// Description is the declared purpose read when no description file is
	// named, and the fallback of a file that cannot be read.
	Description string
	// DescriptionFile is the file the description is read from at every
	// declaration read, so a rewritten file takes effect with no reload.
	DescriptionFile string
	// Assembly is the resolved declaration of the target agent: its identity,
	// model, tool names and instruction.
	Assembly *Assembly
	// Builder runs the assembly into the live agent and one turn of it.
	Builder Builder
	// Delegation is the task face the call is admitted through.
	Delegation Delegation
	// Bindings is the state owner's borrowing face.
	Bindings state.Borrow
	// EventParams lists the call arguments whose values are handed over as the
	// envelope's refs, in declaration order.
	EventParams []string
}
    TargetOptions fixes what the composition root hands one target tool:
    the declared target's identity and purpose, the assembly its delegated turn
    runs on, the builder that runs it, the delegation and borrowing faces of the
    run and the call arguments that travel as the handoff's refs.

type Tool struct {
	// Has unexported fields.
}
    Tool is the call_subagent tool: one declaration naming both assembly
    sources, and a call that assembles one sub-agent, admits it as a task and
    answers with its receipt.

func NewTool(opts ToolOptions) (*Tool, error)
    NewTool returns the call_subagent tool over the faces the caller fixes.

func (t *Tool) Call(ctx context.Context, jsonArgs []byte) (any, error)
    Call implements the native callable tool port: it resolves exactly one
    assembly source, admits the assembled sub-agent as a task and answers with
    the receipt of that admission. The delegated turn runs beside the admission,
    and its outcome returns as the terminal notification of the task that the
    session which issued the call receives.

      - a refusal of the arguments, the assembly or the delegation answers as
        one named result text beside no task, so the turn continues;
      - err is reserved for the protocol fault of a call carrying no admission
        identity.

func (t *Tool) Declaration() *trpctool.Declaration
    Declaration implements the native callable tool port. The description states
    the two mutually exclusive assembly sources, which is the contract the model
    reads.

type ToolOptions struct {
	// Parent is the assembly an assembled sub-agent inherits undeclared fields from;
	// nil means no inheritance.
	Parent *Assembly
	// Builder turns one assembly into the live agent the delegated turn runs on.
	Builder Builder
	// Delegation is the task face the call is admitted through.
	Delegation Delegation
	// Bindings is the state owner's borrowing face: the initiating generation a
	// delegated turn inherits is taken here when the call is issued.
	Bindings state.Borrow
}
    ToolOptions is what the composition root fixes when it registers the tool:
    the parent assembly inheritance reads, the builder the delegated turn runs
    on, the delegation face the call is admitted through and the state owner's
    borrowing face.

type ToolResolver func(ctx context.Context, names []string) ([]trpctool.Tool, error)
    ToolResolver resolves one tool reference list into the tools they name.

type ToolSetResolver func(ctx context.Context, names []string) ([]trpctool.ToolSet, error)
    ToolSetResolver resolves one MCP server reference list into the tool sets
    they name.

type Turn struct {
	// UserID is the user the selected session belongs to.
	UserID string
	// SessionID is the session the delegated turn runs in.
	SessionID string
	// Input is the request the turn starts from; empty runs on the instruction alone.
	Input string
	// Handoff is the structured handoff the turn receives beside that request; nil
	// when the call declared none.
	Handoff *agent.HandoffEnvelope
}
    Turn names one delegated turn: the session the application selected for it,
    the request it starts from and the structured handoff that request carries.

type TurnResult struct {
	// Content is the assistant text of the turn; empty when the turn produced none.
	Content string
}
    TurnResult is the outcome of one turn: the assistant text the turn ended on.

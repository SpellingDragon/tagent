package knowledge // import "github.com/SpellingDragon/tagent/tool/knowledge"

Package knowledge is the model-facing tool surface of a knowledge agent:
the skill search over the deployment's skill repository, the skill load that
puts one skill's body in context, and the bounded file read inside one root.
The three tools own no repository and no file system of their own: the skill
face is the tRPC skill repository and the file face is the tRPC file tool set,
so this package only adapts them onto the declaration the model reads.

CONSTANTS

const (
	// SkillSearchToolName is the name of the skill search tool.
	SkillSearchToolName = "skill_search"
	// SkillLoadToolName is the name of the skill load tool.
	SkillLoadToolName = "skill_load"
	// ReadFileToolName is the name of the bounded file read tool.
	ReadFileToolName = "read_file"
)
    SkillSearchToolName, SkillLoadToolName and ReadFileToolName are the tool
    names the knowledge prompt and the declaration share. They are frozen:
    a rename is a prompt change and a declaration change in one breath.

VARIABLES

var ErrFileToolMissing = errors.New("knowledge: the file tool set carries no read file tool")
    ErrFileToolMissing reports a tRPC file tool set that carries no read_file
    tool, so the declared name would answer nothing.

var ErrReadRootMissing = errors.New("knowledge: the deployment wires no read root")
    ErrReadRootMissing reports a surface assembled beside no read root.

var ErrSearchArguments = errors.New("knowledge: the search arguments do not decode")
    ErrSearchArguments reports a search argument object that does not decode.

var ErrSkillsMissing = errors.New("knowledge: the deployment wires no skill repository")
    ErrSkillsMissing reports a surface assembled beside no skill repository:
    the search and the load refuse by name instead of answering an empty list.

FUNCTIONS

func New(ctx context.Context, opts Options) ([]trpctool.CallableTool, error)
    New returns the knowledge surface over the faces the caller fixes,
    in a fixed order: skill_search, skill_load, read_file. A face the deployment
    leaves unwired contributes no tool, and a surface beside neither face
    returns none.

TYPES

type LoadSink func(name, body string)
    LoadSink receives every skill body a load tool loads: the name the call
    answered beside the body fixed with it, in load order. A surface assembled
    beside no sink loads without publishing its bodies, so the request boundary
    has nothing to inject.

type LoadedSkill struct {
	// Name is the skill name the load was asked for.
	Name string
	// Body is the body the load fixed at that boundary.
	Body string
}
    LoadedSkill is one skill body a load tool fixed at a request boundary:
    the name the load answered beside the body fixed with it.

type Options struct {
	// Skills is the deployment's skill repository; a surface beside none
	// refuses the two skill tools by name.
	Skills skill.Repository
	// ReadRoot is the directory every file read is confined to; a surface
	// beside none refuses the read by name.
	ReadRoot string
	// Sink receives the body of every skill the load tool loads. The host
	// mounts the request-side ledger here; nil publishes no body, so a load
	// answers its name-level receipt exactly as before.
	Sink LoadSink
}
    Options fixes the faces one knowledge surface is assembled over: the skill
    repository the deployment holds, the root the file read is confined to and
    the sink the load publishes its bodies to.

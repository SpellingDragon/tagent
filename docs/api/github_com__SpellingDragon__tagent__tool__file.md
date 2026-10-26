package file // import "github.com/SpellingDragon/tagent/tool/file"

Package file is the model-facing file surface of an action agent: the file tools
one declaration grants, confined to the base directory and the read roots the
same declaration fixes. The package owns no file system of its own: every tool
is the tRPC file tool set, so this package only selects the granted tools and
keeps their request prefix in declaration order.

CONSTANTS

const (
	// SaveFileToolName is the name of the file write tool.
	SaveFileToolName = "save_file"
	// ReadFileToolName is the name of the single file read tool.
	ReadFileToolName = "read_file"
	// ReadMultipleFilesToolName is the name of the multi-file read tool.
	ReadMultipleFilesToolName = "read_multiple_files"
	// ListFileToolName is the name of the directory listing tool.
	ListFileToolName = "list_file"
	// SearchFileToolName is the name of the file name search tool.
	SearchFileToolName = "search_file"
	// SearchContentToolName is the name of the file content search tool.
	SearchContentToolName = "search_content"
	// ReplaceContentToolName is the name of the in-place edit tool.
	ReplaceContentToolName = "replace_content"
)
    SaveFileToolName and the six sibling names are the seven names the
    declaration and the action prompt share. They are frozen: a rename is a
    prompt change and a declaration change in one breath.

VARIABLES

var ErrDuplicateTool = errors.New("file: the declaration names the same file tool twice")
    ErrDuplicateTool reports a declared tool id named twice: one grant is one
    entry of the request prefix.

var ErrUnknownTool = errors.New("file: the declaration names no such file tool")
    ErrUnknownTool reports a declared tool id the file surface does not carry,
    so a declaration that drifts from the frozen seven stops the assembly
    instead of exposing a name nothing answers.

FUNCTIONS

func Names() []string
    Names returns the seven names in the frozen order the tool set carries them.

TYPES

type Options struct {
	// BaseDir is the directory every relative path resolves under; an empty
	// directory means the current directory.
	BaseDir string
	// ReadRoots are the additional roots an absolute read may name; writes
	// never reach outside BaseDir.
	ReadRoots []string
	// Tools selects the granted tools in declaration order; an empty list
	// grants all seven in the frozen order.
	Tools []string
}
    Options fixes the face one file surface is assembled over: the directory
    every path resolves under, the roots a read may name absolutely, and the
    tools the declaration grants.

type Surface struct {
	// Has unexported fields.
}
    Surface is the file tool set of one agent: the granted tools over the
    declared base directory, in the order the declaration names them.

func New(opts Options) (*Surface, error)
    New assembles the file surface the options fix. The base directory must
    exist when the surface is built, so a deployment whose declared directory is
    missing is refused at assembly rather than at the first call.

func (s *Surface) BaseDir() string
    BaseDir reports the directory every relative path resolves under.

func (s *Surface) Close() error
    Close implements the tool set interface: the surface holds no resource the
    native tool set does not already release.

func (s *Surface) Name() string
    Name implements the tool set interface.

func (s *Surface) Tools(context.Context) []trpctool.Tool
    Tools implements the tool set interface: the granted tools in declaration
    order.

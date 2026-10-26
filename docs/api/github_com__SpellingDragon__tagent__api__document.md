package document // import "github.com/SpellingDragon/tagent/api/document"

Package document is the host document dialect adaptation every host shares:
a host document carries the providers, governance and application sections its
own deployment needs over the core declaration, and the host maps that whole
document onto a Document — the declaration-shaped section every host has in
common. The dialect shifts from host to host, the declaration underneath stays
fixed: Declaration derives the organization declaration from a Document — the
entry agent validation, the assembly order and the agent sections — so a host
never spells the declaration rules a second time.

VARIABLES

var ErrDeclarationEntry = errors.New("config: the document names no declared entry agent")
    ErrDeclarationEntry reports a document that names no entry agent, or names
    one it does not declare: the entry is the one agent the declaration admits
    application input to, so a document without it assembles nothing.

FUNCTIONS

func PromptFiles(dir string, names []string) ([]config.FileRef, []string, error)
    PromptFiles resolves the prompt files one declared agent is assembled from,
    relative to the prompt directory the document names. A declared file the
    checkout does not carry is reported as absent instead of failing the read,
    so the personal context a deployment keeps beside the checkout stays an
    input rather than a startup precondition; any other read failure stops the
    read.

TYPES

type Document struct {
	// Entry is the name of the entry agent that receives application input.
	Entry string
	// Agents holds the declared agent sections keyed by identity. The entry
	// is resolved against this map, so an identity the map does not carry is
	// outside the document.
	Agents map[string]config.AgentSpec
	// Servers declares the MCP servers the organization binds by name.
	Servers map[string]config.MCPServerSpec
}
    Document is the declaration-shaped section of a host document: the entry
    agent, the agent sections keyed by identity and the MCP servers the
    organization binds. A host whose own document carries further sections maps
    those onto the document schema it decodes and hands this section over.

func (d Document) Declaration() (*config.Declaration, error)
    Declaration derives the organization declaration of this document: the entry
    agent is required and must be declared, and the agent sections are assembled
    in Order. Every other section of a host document stays the host's own,
    so a document that carries a connection registry or an application section
    keeps it beside the declaration instead of inside it.

func (d Document) Order() []string
    Order returns the declared agent identities in assembly order: the entry
    first, then the remaining identities sorted, so one document assembles in
    one order across restarts.

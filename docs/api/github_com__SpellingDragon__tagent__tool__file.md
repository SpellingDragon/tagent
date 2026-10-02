package file // import "github.com/SpellingDragon/tagent/tool/file"

Package file wraps trpc-agent-go's built-in file operation tools for tagent.

- read_file, save_file, list_file and friends register as plain tools
for agent YAML. - base_dir falls back to the agent working root,

FUNCTIONS

func RegisterTools()
    RegisterTools registers all built-in file operation tools as plain tools.
    Should be called once during tagent's built-in tool registration. Uses
    sync.Once for idempotency — safe to call multiple times.

package file // import "github.com/SpellingDragon/tagent/tool/file"

Package file wraps trpc-agent-go's built-in file operation tools for tagent.

It registers individual file tools (read_file, save_file, list_file, etc.) as
plain tools so they can be referenced directly from agent YAML configs.

Configuration (via ToolRef.Properties): - base_dir: root directory for file
operations. Falls back to the agent-level working root (config.working_dir
/ $TAGENT_WORKING_DIR), then to the process working directory "." — the same
precedence the exec tool uses for its command cwd, so both always share one base
(a single filesystem view for the model).

Example YAML:

    tools:

- kind: tool id: read_file description_file: read_file_tool_desc.md properties:
base_dir: "./workspace"

FUNCTIONS

func RegisterTools()

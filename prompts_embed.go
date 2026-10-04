// 契约: docs/wiki/prompt/prompt-architecture.md#file-layout
package tagent

import "embed"

// defaultPromptsFS embeds the framework's default prompt set so it ships with
// the binary as a location-independent fallback. Consumers override individual
// prompts on disk (via prompt_dir); anything they don't provide resolves from
// here; prompt.WithFallback is the consumer-side entry for that override.
//
//go:embed resources/prompts
var defaultPromptsFS embed.FS

// DefaultPromptsFS returns the embedded framework default prompts. The tree is rooted
// at DefaultPromptsPrefix (a prompt file is e.g. recall_tool_desc.md).
func DefaultPromptsFS() embed.FS { return defaultPromptsFS }

// DefaultPromptsPrefix is the path prefix under which the embedded defaults live.
const DefaultPromptsPrefix = "resources/prompts"

package tagent

import "embed"

// defaultPromptsFS embeds the framework's default prompt set so it ships with
// the binary as a location-independent fallback. Consumers override individual
// prompts on disk (via prompt_dir); anything they don't provide resolves from
// here. See prompt.WithFallback / the prompt-loader-fallback capability.
//
//go:embed resources/prompts
var defaultPromptsFS embed.FS

// DefaultPromptsFS returns the embedded framework default prompts. The tree is
// rooted at "resources/prompts" (e.g. "resources/prompts/recall_tool_desc.md").
func DefaultPromptsFS() embed.FS { return defaultPromptsFS }

// DefaultPromptsPrefix is the path prefix under which the embedded defaults live.
const DefaultPromptsPrefix = "resources/prompts"

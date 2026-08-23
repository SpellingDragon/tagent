package tagent

import (
	"testing"

	"github.com/SpellingDragon/tagent/prompt"
)

// TestEmbeddedPrompts_ResolveViaFallback verifies the framework's shared prompts
// are embedded and resolve through the loader fallback from an empty on-disk
// dir — the mechanism that lets examples drop their duplicate copies.
func TestEmbeddedPrompts_ResolveViaFallback(t *testing.T) {
	l := prompt.NewLoader(t.TempDir(), prompt.WithFallback(defaultPromptsFS, DefaultPromptsPrefix))

	shared := []string{
		"action_agent.md", "action_tool_desc.md",
		"exec_tool_desc.md",
		"knowledge_agent.md", "knowledge_tool_desc.md",
		"meditation.md",
		"recall_agent.md", "recall_tool_desc.md",
	}
	for _, f := range shared {
		content, err := l.LoadFromFile(f)
		if err != nil {
			t.Errorf("%s: embedded fallback resolve failed: %v", f, err)
			continue
		}
		if content == "" {
			t.Errorf("%s: embedded content is empty", f)
		}
	}
}
